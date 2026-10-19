package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cpuSettingKey         = "ORACLE_CPUS"
	memorySettingKey      = "ORACLE_MEMORY"
	shmSettingKey         = "ORACLE_SHM_SIZE"
	passwordSettingKey    = "ORACLE_PWD"
	defaultOracleRegistry = "container-registry.oracle.com"
	defaultOracleImage    = defaultOracleRegistry + "/database/enterprise:19.3.0.0"
	managedLabel          = "dev.oralab.managed"
	oracleContainerName   = "oralab-oracle"
	oracleVolumeName      = "oralab-oradata"
	oracleDataPath        = "/opt/oracle/oradata"
	oracleListenerPort    = "1521"
	operationTimeout      = 31 * time.Minute
	stateTimeout          = 12 * time.Second
)

type hostState struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type dockerState struct {
	APIAvailable bool   `json:"api_available"`
	Reachable    bool   `json:"reachable"`
	LocalContext bool   `json:"local_context"`
	Context      string `json:"context,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	EngineName   string `json:"engine_name,omitempty"`
	EngineOS     string `json:"engine_os,omitempty"`
	Version      string `json:"version,omitempty"`
	CPUs         int    `json:"cpus,omitempty"`
	MemoryBytes  int64  `json:"memory_bytes,omitempty"`
	Error        string `json:"error,omitempty"`
}

type oracleState struct {
	Service     string `json:"service,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	Exists      bool   `json:"exists"`
	Running     bool   `json:"running"`
	Status      string `json:"status"`
	Health      string `json:"health"`
	Readiness   string `json:"readiness"`
	Error       string `json:"error,omitempty"`
}

type resourceState struct {
	ConfiguredCPUs        int     `json:"configured_cpus,omitempty"`
	ConfiguredMemoryBytes int64   `json:"configured_memory_bytes,omitempty"`
	AppliedCPUs           float64 `json:"applied_cpus,omitempty"`
	AppliedMemoryBytes    int64   `json:"applied_memory_bytes,omitempty"`
	CPUPercent            float64 `json:"cpu_percent,omitempty"`
	MemoryUsageBytes      int64   `json:"memory_usage_bytes,omitempty"`
	StatsAvailable        bool    `json:"stats_available"`
	Error                 string  `json:"error,omitempty"`
}

type storageMount struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
}

type managerState struct {
	Host      hostState      `json:"host"`
	Docker    dockerState    `json:"docker"`
	Oracle    oracleState    `json:"oracle"`
	Resources resourceState  `json:"resources"`
	Storage   []storageMount `json:"storage"`
	CheckedAt time.Time      `json:"checked_at"`
}

type applyRequest struct {
	CPUs      int `json:"cpus"`
	MemoryGiB int `json:"memory_gib"`
}

type operationResult struct {
	OK      bool         `json:"ok"`
	Saved   bool         `json:"saved,omitempty"`
	Applied bool         `json:"applied,omitempty"`
	Message string       `json:"message"`
	State   managerState `json:"state"`
}

type lifecycleRequest struct {
	Action string `json:"action"`
}

type labConfig struct {
	Password    string
	CPUs        int
	MemoryBytes int64
	ShmBytes    int64
}

type resourceManager struct {
	root      string
	engine    dockerEngine
	engineErr error
	opMu      sync.Mutex
}

func newResourceManager(root string, engine dockerEngine, engineErr error) *resourceManager {
	return &resourceManager{root: root, engine: engine, engineErr: engineErr}
}

func (m *resourceManager) state(ctx context.Context) managerState {
	state := managerState{
		Host: hostState{OS: hostOSName(runtime.GOOS), Architecture: runtime.GOARCH},
		Oracle: oracleState{
			Service: oracleContainerName, Status: "unknown", Health: "unknown", Readiness: "unknown",
		},
		CheckedAt: time.Now().UTC(),
	}
	configuredCPUs, configuredMemory, configErr := readResourceSettings(filepath.Join(m.root, environmentFile))
	state.Resources.ConfiguredCPUs = configuredCPUs
	state.Resources.ConfiguredMemoryBytes = configuredMemory
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		state.Resources.Error = configErr.Error()
	}
	if m.engineErr != nil || m.engine == nil {
		state.Docker.Error = friendlyEngineError("Docker Engine client is unavailable", m.engineErr)
		state.Oracle.Error = "Oracle state requires the Docker Engine API."
		return state
	}
	state.Docker.APIAvailable = true
	state.Docker.Context = "Engine API"
	state.Docker.Endpoint = m.engine.Endpoint()
	state.Docker.LocalContext = isLocalDockerEndpoint(state.Docker.Endpoint)
	info, err := m.engine.Info(ctx)
	if err != nil {
		state.Docker.Error = friendlyEngineError("Docker Engine is unavailable", err)
		state.Oracle.Error = "Oracle state is unavailable until Docker Engine is running."
		return state
	}
	state.Docker.Reachable = true
	state.Docker.EngineName = info.Name
	state.Docker.EngineOS = info.OperatingSystem
	state.Docker.Version = info.ServerVersion
	state.Docker.CPUs = info.CPUs
	state.Docker.MemoryBytes = info.MemoryBytes

	inspected, err := m.engine.InspectContainer(ctx, oracleContainerName)
	if errors.Is(err, errDockerObjectNotFound) {
		state.Oracle.Status = "not-created"
		state.Oracle.Health = "not-applicable"
		state.Oracle.Readiness = "not-ready"
		return state
	}
	if err != nil {
		state.Oracle.Error = friendlyEngineError("Oracle container inspection failed", err)
		return state
	}
	if !inspected.Managed {
		state.Oracle.Status = "name-conflict"
		state.Oracle.Error = "A container named oralab-oracle exists but is not managed by OraLab."
		return state
	}
	state.Oracle.Exists = true
	state.Oracle.ContainerID = shortContainerID(inspected.ID)
	state.Oracle.Running = inspected.Running
	state.Oracle.Status = inspected.Status
	if inspected.Restarting {
		state.Oracle.Status = "restarting"
	}
	if inspected.HealthConfigured {
		state.Oracle.Health = inspected.Health
		switch inspected.Health {
		case "healthy":
			state.Oracle.Readiness = "ready"
		case "starting":
			state.Oracle.Readiness = "initializing"
		default:
			state.Oracle.Readiness = "not-ready"
		}
	} else {
		state.Oracle.Health = "not-configured"
		if inspected.Running {
			state.Oracle.Readiness = "ready"
		} else {
			state.Oracle.Readiness = "not-ready"
		}
	}
	state.Resources.AppliedCPUs = inspected.CPUs
	state.Resources.AppliedMemoryBytes = inspected.MemoryBytes
	for _, item := range inspected.Mounts {
		state.Storage = append(state.Storage, storageMount{
			Type: item.Type, Name: item.Name, Source: item.Source, Destination: item.Destination,
		})
	}
	if inspected.Running {
		stats, statsErr := m.engine.Stats(ctx, oracleContainerName)
		if statsErr != nil {
			state.Resources.Error = friendlyEngineError("Resource statistics unavailable", statsErr)
		} else {
			state.Resources.CPUPercent = stats.CPUPercent
			state.Resources.MemoryUsageBytes = stats.MemoryUsedBytes
			state.Resources.StatsAvailable = true
		}
	}
	return state
}

func (m *resourceManager) apply(ctx context.Context, request applyRequest) operationResult {
	if !m.opMu.TryLock() {
		return operationResult{Message: "Another OraLab operation is already in progress."}
	}
	defer m.opMu.Unlock()
	operationContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), operationTimeout)
	defer cancel()
	before := m.state(operationContext)
	if validationError := validateResourceRequest(request, before); validationError != "" {
		return operationResult{Message: validationError, State: before}
	}
	if !before.Docker.LocalContext {
		return operationResult{Message: "Resource changes are disabled because the Docker Engine endpoint is not local.", State: before}
	}
	memoryBytes := int64(request.MemoryGiB) * 1024 * 1024 * 1024
	if err := updateResourceSettings(filepath.Join(m.root, environmentFile), request.CPUs, fmt.Sprintf("%dg", request.MemoryGiB)); err != nil {
		return operationResult{Message: fmt.Sprintf("Resource settings were not saved: %v", err), State: before}
	}
	result := operationResult{Saved: true}
	if !before.Oracle.Exists {
		result.OK = true
		result.Message = "CPU and memory limits were saved and will be used when OraLab creates the Oracle container."
		result.State = m.state(operationContext)
		return result
	}
	if err := m.engine.UpdateResources(operationContext, oracleContainerName, request.CPUs, memoryBytes); err != nil {
		result.Message = friendlyEngineError("Settings were saved, but Docker could not apply them", err)
		result.State = m.state(operationContext)
		return result
	}
	after := m.state(operationContext)
	result.State = after
	if nearlyEqual(after.Resources.AppliedCPUs, float64(request.CPUs)) && after.Resources.AppliedMemoryBytes == memoryBytes {
		result.OK = true
		result.Applied = true
		result.Message = "CPU and memory limits were saved and verified through the Docker Engine API."
		return result
	}
	result.Message = "Settings were saved, but Docker's effective limits do not match the requested values."
	return result
}

func (m *resourceManager) lifecycle(ctx context.Context, request lifecycleRequest) operationResult {
	if !m.opMu.TryLock() {
		return operationResult{Message: "Another OraLab operation is already in progress."}
	}
	defer m.opMu.Unlock()
	operationContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), operationTimeout)
	defer cancel()
	before := m.state(operationContext)
	if !before.Docker.Reachable {
		return operationResult{Message: "Docker Engine is unavailable.", State: before}
	}
	if !before.Docker.LocalContext {
		return operationResult{Message: "Lifecycle operations are disabled because the Docker Engine endpoint is not local.", State: before}
	}
	var err error
	switch request.Action {
	case "start":
		_, err = ensureOracleContainer(operationContext, m.engine, m.root)
		if err == nil && !before.Oracle.Running {
			err = m.engine.StartContainer(operationContext, oracleContainerName)
		}
		if err == nil {
			err = waitForReadiness(operationContext, m.engine, oracleContainerName)
		}
	case "stop":
		if !before.Oracle.Exists || !before.Oracle.Running {
			return operationResult{OK: true, Message: "Oracle is already stopped.", State: before}
		}
		err = m.engine.StopContainer(operationContext, oracleContainerName, defaultStop)
	case "restart":
		if !before.Oracle.Exists {
			return operationResult{Message: "The Oracle container has not been created; use start first.", State: before}
		}
		err = m.engine.RestartContainer(operationContext, oracleContainerName, defaultStop)
		if err == nil {
			err = waitForReadiness(operationContext, m.engine, oracleContainerName)
		}
	default:
		return operationResult{Message: "Unsupported lifecycle action.", State: before}
	}
	if err != nil {
		return operationResult{Message: friendlyEngineError("Oracle lifecycle operation failed", err), State: m.state(operationContext)}
	}
	after := m.state(operationContext)
	succeeded := request.Action == "stop" && !after.Oracle.Running || request.Action != "stop" && after.Oracle.Running
	if !succeeded {
		return operationResult{Message: "Docker completed the operation, but the requested Oracle state was not verified.", State: after}
	}
	return operationResult{OK: true, Applied: true, Message: "Oracle lifecycle operation completed and its state was verified.", State: after}
}

func ensureOracleContainer(ctx context.Context, engine dockerEngine, root string) (engineContainer, error) {
	existing, err := engine.InspectContainer(ctx, oracleContainerName)
	if err == nil {
		if !existing.Managed {
			return engineContainer{}, errors.New("a container named oralab-oracle exists but is not managed by OraLab")
		}
		return existing, nil
	}
	if !errors.Is(err, errDockerObjectNotFound) {
		return engineContainer{}, err
	}
	config, err := readLabConfig(filepath.Join(root, environmentFile))
	if err != nil {
		return engineContainer{}, err
	}
	if config.Password == "" {
		return engineContainer{}, errors.New("ORACLE_PWD is empty")
	}
	if config.CPUs <= 0 || config.MemoryBytes <= 0 {
		return engineContainer{}, errors.New("ORACLE_CPUS and ORACLE_MEMORY must be configured before creating Oracle")
	}
	if err := engine.EnsureImage(ctx, defaultOracleImage); err != nil {
		return engineContainer{}, fmt.Errorf("make Oracle image available: %w", err)
	}
	_, err = engine.CreateContainer(ctx, engineContainerSpec{
		Name: oracleContainerName, Image: defaultOracleImage,
		Environment: []string{passwordSettingKey + "=" + config.Password, "ORACLE_EDITION=enterprise"},
		CPUs:        config.CPUs, MemoryBytes: config.MemoryBytes, ShmBytes: config.ShmBytes,
		StopTimeout: defaultStop, VolumeName: oracleVolumeName, VolumePath: oracleDataPath,
		HostIP: "127.0.0.1", HostPort: oracleListenerPort, TargetPort: oracleListenerPort + "/tcp",
		Labels: map[string]string{managedLabel: "true"},
	})
	if err != nil {
		return engineContainer{}, err
	}
	return engine.InspectContainer(ctx, oracleContainerName)
}

func waitForReadiness(ctx context.Context, engine dockerEngine, name string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		inspected, err := engine.InspectContainer(ctx, name)
		if err != nil {
			return err
		}
		if !inspected.Running {
			return fmt.Errorf("container stopped with exit code %d", inspected.ExitCode)
		}
		if !inspected.HealthConfigured || inspected.Health == "healthy" {
			return nil
		}
		if inspected.Health == "unhealthy" {
			return errors.New("container healthcheck reports unhealthy")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func readLabConfig(path string) (labConfig, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return labConfig{}, errors.New(".env is missing; run oralab init first")
		}
		return labConfig{}, err
	}
	values, err := parseEnvironment(contents)
	if err != nil {
		return labConfig{}, err
	}
	config := labConfig{Password: values[passwordSettingKey]}
	if value := strings.TrimSpace(values[cpuSettingKey]); value != "" {
		config.CPUs, err = strconv.Atoi(value)
		if err != nil || config.CPUs <= 0 {
			return labConfig{}, fmt.Errorf("%s must be a positive whole number", cpuSettingKey)
		}
	}
	if value := strings.TrimSpace(values[memorySettingKey]); value != "" {
		config.MemoryBytes, err = parseByteSize(value)
		if err != nil || config.MemoryBytes <= 0 {
			return labConfig{}, fmt.Errorf("%s is invalid", memorySettingKey)
		}
	}
	config.ShmBytes = 1024 * 1024 * 1024
	if value := strings.TrimSpace(values[shmSettingKey]); value != "" {
		config.ShmBytes, err = parseByteSize(value)
		if err != nil || config.ShmBytes <= 0 {
			return labConfig{}, fmt.Errorf("%s is invalid", shmSettingKey)
		}
	}
	if config.MemoryBytes > 0 && config.ShmBytes > config.MemoryBytes {
		return labConfig{}, fmt.Errorf("%s cannot exceed %s", shmSettingKey, memorySettingKey)
	}
	return config, nil
}

func readResourceSettings(path string) (int, int64, error) {
	config, err := readLabConfig(path)
	return config.CPUs, config.MemoryBytes, err
}

func createLabConfig(path, password string, options initOptions) error {
	contents := fmt.Sprintf("# Created by OraLab. The Oracle 19c Enterprise image is managed by OraLab.\nORACLE_PWD=%s\nORACLE_CPUS=%d\nORACLE_MEMORY=%s\nORACLE_SHM_SIZE=%s\n", password, options.CPUs, options.Memory, options.Shm)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return errors.New("refusing to overwrite existing .env")
	}
	if err != nil {
		return err
	}
	if _, err := file.WriteString(contents); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func parseEnvironment(contents []byte) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("duplicate setting %s", key)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values, scanner.Err()
}

func updateResourceSettings(path string, cpus int, memory string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New(".env is missing; run oralab init first")
		}
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n")
	updates := map[string]string{cpuSettingKey: strconv.Itoa(cpus), memorySettingKey: memory}
	found := map[string]bool{}
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, hasValue := strings.Cut(trimmed, "=")
		key = strings.TrimSpace(key)
		value, managed := updates[key]
		if !hasValue || !managed {
			continue
		}
		if found[key] {
			return fmt.Errorf("refusing to update duplicate setting %s", key)
		}
		lines[index] = key + "=" + value
		found[key] = true
	}
	for _, key := range []string{cpuSettingKey, memorySettingKey} {
		if !found[key] {
			lines = append(lines, key+"="+updates[key])
		}
	}
	updated := []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n")
	return replaceFileSafely(path, updated)
}

func replaceFileSafely(path string, contents []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".oralab-env-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err == nil {
		return nil
	}
	backupPath := path + ".oralab-backup"
	if _, err := os.Stat(backupPath); err == nil {
		return fmt.Errorf("backup file already exists: %s", backupPath)
	}
	if err := os.Rename(path, backupPath); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Rename(backupPath, path)
		return err
	}
	_ = os.Remove(backupPath)
	return nil
}

func validateResourceRequest(request applyRequest, state managerState) string {
	if request.CPUs <= 0 || request.CPUs > 1024 {
		return "CPU limit must be a positive whole number."
	}
	if request.MemoryGiB <= 0 || request.MemoryGiB > 4096 {
		return "Memory limit must be a positive whole number of GiB."
	}
	if !state.Docker.Reachable {
		return "Docker Engine must be running before resource limits can be validated."
	}
	if state.Docker.CPUs > 0 && request.CPUs > state.Docker.CPUs {
		return fmt.Sprintf("CPU limit cannot exceed Docker's reported capacity of %d CPUs.", state.Docker.CPUs)
	}
	requestedMemory := int64(request.MemoryGiB) * 1024 * 1024 * 1024
	if state.Docker.MemoryBytes > 0 && requestedMemory > state.Docker.MemoryBytes {
		return fmt.Sprintf("Memory limit cannot exceed Docker's reported capacity of %s.", formatBytes(state.Docker.MemoryBytes))
	}
	return ""
}

func parseByteSize(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty size")
	}
	lower := strings.ToLower(value)
	units := []struct {
		suffix     string
		multiplier float64
	}{
		{"gib", 1024 * 1024 * 1024}, {"gb", 1000 * 1000 * 1000}, {"g", 1024 * 1024 * 1024},
		{"mib", 1024 * 1024}, {"mb", 1000 * 1000}, {"m", 1024 * 1024},
		{"kib", 1024}, {"kb", 1000}, {"k", 1024}, {"b", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(lower, unit.suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(lower, unit.suffix))
			parsed, err := strconv.ParseFloat(number, 64)
			if err != nil || parsed < 0 {
				return 0, errors.New("invalid size")
			}
			return int64(parsed * unit.multiplier), nil
		}
	}
	parsed, err := strconv.ParseInt(lower, 10, 64)
	if err != nil || parsed < 0 {
		return 0, errors.New("invalid size")
	}
	return parsed, nil
}

func isLocalDockerEndpoint(endpoint string) bool {
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	if endpoint == "" || strings.HasPrefix(endpoint, "unix://") || strings.HasPrefix(endpoint, "npipe://") {
		return true
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "tcp" && parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func hostOSName(goos string) string {
	switch goos {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return goos
	}
}

func shortContainerID(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func nearlyEqual(left, right float64) bool {
	difference := left - right
	if difference < 0 {
		difference = -difference
	}
	return difference < 0.001
}

func formatBytes(value int64) string {
	const gib = 1024 * 1024 * 1024
	if value >= gib {
		return fmt.Sprintf("%.1f GiB", float64(value)/gib)
	}
	return fmt.Sprintf("%d MiB", value/(1024*1024))
}

func friendlyEngineError(prefix string, err error) string {
	if err == nil {
		return prefix + "."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return prefix + ": operation timed out."
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 400 {
		message = message[:400] + "…"
	}
	if message == "" {
		return prefix + "."
	}
	return prefix + ": " + message
}
