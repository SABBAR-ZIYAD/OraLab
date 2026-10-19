package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cpuSettingKey    = "ORACLE_CPUS"
	memorySettingKey = "ORACLE_MEMORY"
	operationTimeout = 31 * time.Minute
	stateTimeout     = 12 * time.Second
)

type dockerCommandRunner interface {
	LookPath(file string) (string, error)
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type systemDockerRunner struct{}

func (systemDockerRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (systemDockerRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil && stderr.Len() > 0 {
		output = append(output, stderr.Bytes()...)
	}
	return output, err
}

type hostState struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type dockerState struct {
	CLIAvailable bool   `json:"cli_available"`
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
	CPUs           int  `json:"cpus"`
	MemoryGiB      int  `json:"memory_gib"`
	ConfirmRestart bool `json:"confirm_restart"`
}

type operationResult struct {
	OK              bool         `json:"ok"`
	Saved           bool         `json:"saved,omitempty"`
	Applied         bool         `json:"applied,omitempty"`
	RequiresRestart bool         `json:"requires_restart,omitempty"`
	Message         string       `json:"message"`
	State           managerState `json:"state"`
}

type lifecycleRequest struct {
	Action string `json:"action"`
}

type resourceManager struct {
	root   string
	runner dockerCommandRunner
	opMu   sync.Mutex
}

func newResourceManager(root string, runner dockerCommandRunner) *resourceManager {
	return &resourceManager{root: root, runner: runner}
}

func (m *resourceManager) state(ctx context.Context) managerState {
	state := managerState{
		Host: hostState{
			OS:           hostOSName(runtime.GOOS),
			Architecture: runtime.GOARCH,
		},
		Oracle:    oracleState{Status: "unknown", Health: "unknown", Readiness: "unknown"},
		CheckedAt: time.Now().UTC(),
	}

	configuredCPUs, configuredMemory, configErr := readResourceSettings(filepath.Join(m.root, environmentFile))
	state.Resources.ConfiguredCPUs = configuredCPUs
	state.Resources.ConfiguredMemoryBytes = configuredMemory
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		state.Resources.Error = configErr.Error()
	}

	if _, err := m.runner.LookPath("docker"); err != nil {
		state.Docker.Error = "Docker CLI is not installed or is not available in PATH."
		state.Oracle.Error = "Oracle state requires Docker."
		return state
	}
	state.Docker.CLIAvailable = true

	contextName, endpoint, err := m.dockerContext(ctx)
	if err != nil {
		state.Docker.Error = friendlyCommandError("Docker context is unavailable", err)
		state.Oracle.Error = "Oracle state is unavailable until Docker is accessible."
		return state
	}
	state.Docker.Context = contextName
	state.Docker.Endpoint = endpoint
	state.Docker.LocalContext = isLocalDockerEndpoint(endpoint)

	infoOutput, err := m.runner.Output(ctx, "docker", "info", "--format", "{{json .}}")
	if err != nil {
		state.Docker.Error = friendlyCommandOutputError("Docker Engine is unavailable", infoOutput, err)
		state.Oracle.Error = "Oracle state is unavailable until Docker Engine is running."
		return state
	}
	var info struct {
		Name            string `json:"Name"`
		OperatingSystem string `json:"OperatingSystem"`
		ServerVersion   string `json:"ServerVersion"`
		NCPU            int    `json:"NCPU"`
		MemTotal        int64  `json:"MemTotal"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(infoOutput), &info); err != nil {
		state.Docker.Error = "Docker returned an unreadable capability response."
		return state
	}
	state.Docker.Reachable = true
	state.Docker.EngineName = info.Name
	state.Docker.EngineOS = info.OperatingSystem
	state.Docker.Version = info.ServerVersion
	state.Docker.CPUs = info.NCPU
	state.Docker.MemoryBytes = info.MemTotal

	service, err := m.discoverOracleService(ctx)
	if err != nil {
		state.Oracle.Error = err.Error()
		if strings.Contains(err.Error(), composeFileName) {
			state.Oracle.Status = "not-configured"
		}
		return state
	}
	state.Oracle.Service = service

	containerOutput, err := m.composeOutput(ctx, "ps", "-q", service)
	if err != nil {
		state.Oracle.Error = friendlyCommandOutputError("Oracle container detection failed", containerOutput, err)
		return state
	}
	containerID := strings.TrimSpace(string(containerOutput))
	if containerID == "" {
		state.Oracle.Status = "not-created"
		state.Oracle.Health = "not-applicable"
		state.Oracle.Readiness = "not-ready"
		return state
	}

	state.Oracle.Exists = true
	state.Oracle.ContainerID = shortContainerID(containerID)
	inspectOutput, err := m.runner.Output(ctx, "docker", "inspect", containerID)
	if err != nil {
		state.Oracle.Error = friendlyCommandOutputError("Oracle container inspection failed", inspectOutput, err)
		return state
	}
	var inspected []struct {
		State struct {
			Status     string `json:"Status"`
			Running    bool   `json:"Running"`
			Restarting bool   `json:"Restarting"`
			ExitCode   int    `json:"ExitCode"`
			Health     *struct {
				Status string `json:"Status"`
			} `json:"Health"`
		} `json:"State"`
		HostConfig struct {
			NanoCPUs int64 `json:"NanoCpus"`
			Memory   int64 `json:"Memory"`
		} `json:"HostConfig"`
		Mounts []struct {
			Type        string `json:"Type"`
			Name        string `json:"Name"`
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
		} `json:"Mounts"`
	}
	if err := json.Unmarshal(inspectOutput, &inspected); err != nil || len(inspected) != 1 {
		state.Oracle.Error = "Docker returned unreadable Oracle container details."
		return state
	}
	container := inspected[0]
	state.Oracle.Running = container.State.Running
	state.Oracle.Status = container.State.Status
	if container.State.Restarting {
		state.Oracle.Status = "restarting"
	}
	if container.State.Health != nil {
		state.Oracle.Health = container.State.Health.Status
		switch container.State.Health.Status {
		case "healthy":
			state.Oracle.Readiness = "ready"
		case "starting":
			state.Oracle.Readiness = "initializing"
		default:
			state.Oracle.Readiness = "not-ready"
		}
	} else {
		state.Oracle.Health = "not-configured"
		state.Oracle.Readiness = "unknown"
	}
	state.Resources.AppliedCPUs = float64(container.HostConfig.NanoCPUs) / 1_000_000_000
	state.Resources.AppliedMemoryBytes = container.HostConfig.Memory
	for _, mount := range container.Mounts {
		state.Storage = append(state.Storage, storageMount{
			Type:        mount.Type,
			Name:        mount.Name,
			Source:      mount.Source,
			Destination: mount.Destination,
		})
	}

	if state.Oracle.Running {
		statsOutput, statsErr := m.runner.Output(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}", containerID)
		if statsErr != nil {
			state.Resources.Error = friendlyCommandOutputError("Resource statistics unavailable", statsOutput, statsErr)
		} else if err := parseContainerStats(statsOutput, &state.Resources); err != nil {
			state.Resources.Error = err.Error()
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
		return operationResult{Message: "Resource changes are disabled because the active Docker context is not local.", State: before}
	}
	if before.Oracle.Running && !request.ConfirmRestart {
		return operationResult{
			RequiresRestart: true,
			Message:         "Applying these limits requires a graceful Oracle container restart.",
			State:           before,
		}
	}

	memoryValue := fmt.Sprintf("%dg", request.MemoryGiB)
	if err := updateResourceSettings(filepath.Join(m.root, environmentFile), request.CPUs, memoryValue); err != nil {
		return operationResult{Message: fmt.Sprintf("Resource settings were not saved: %v", err), State: before}
	}
	result := operationResult{Saved: true}

	service := before.Oracle.Service
	if service == "" {
		result.Message = "Settings were saved, but no Oracle Compose service is available to apply them."
		result.State = m.state(operationContext)
		return result
	}

	var output []byte
	var err error
	if before.Oracle.Running {
		output, err = m.composeOutput(operationContext, "up", "-d", "--force-recreate", "--wait", "--wait-timeout", strconv.Itoa(defaultWait), service)
	} else {
		output, err = m.composeOutput(operationContext, "create", "--force-recreate", service)
	}
	if err != nil {
		result.Message = friendlyCommandOutputError("Settings were saved, but Docker could not apply them", output, err)
		result.State = m.state(operationContext)
		return result
	}

	after := m.state(operationContext)
	result.State = after
	expectedMemory := int64(request.MemoryGiB) * 1024 * 1024 * 1024
	if nearlyEqual(after.Resources.AppliedCPUs, float64(request.CPUs)) && after.Resources.AppliedMemoryBytes == expectedMemory {
		result.OK = true
		result.Applied = true
		result.Message = "CPU and memory limits were saved and verified by Docker."
		return result
	}
	result.Message = "Settings were saved, but Docker's effective limits do not match the requested values. Check that compose.yaml uses ORACLE_CPUS and ORACLE_MEMORY."
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
		return operationResult{Message: "Lifecycle operations are disabled because the active Docker context is not local.", State: before}
	}
	service := before.Oracle.Service
	if service == "" {
		return operationResult{Message: "No Oracle Compose service was found.", State: before}
	}

	var output []byte
	var err error
	switch request.Action {
	case "start":
		output, err = m.composeOutput(operationContext, "up", "-d", "--wait", "--wait-timeout", strconv.Itoa(defaultWait), service)
	case "stop":
		output, err = m.composeOutput(operationContext, "stop", "--timeout", strconv.Itoa(defaultStop), service)
	case "restart":
		output, err = m.composeOutput(operationContext, "stop", "--timeout", strconv.Itoa(defaultStop), service)
		if err == nil {
			output, err = m.composeOutput(operationContext, "up", "-d", "--wait", "--wait-timeout", strconv.Itoa(defaultWait), service)
		}
	default:
		return operationResult{Message: "Unsupported lifecycle action.", State: before}
	}
	if err != nil {
		return operationResult{Message: friendlyCommandOutputError("Oracle lifecycle operation failed", output, err), State: m.state(operationContext)}
	}

	after := m.state(operationContext)
	succeeded := request.Action == "stop" && !after.Oracle.Running || request.Action != "stop" && after.Oracle.Running
	if !succeeded {
		return operationResult{Message: "Docker completed the operation, but the requested Oracle state was not verified.", State: after}
	}
	return operationResult{OK: true, Applied: true, Message: "Oracle lifecycle operation completed and its container state was verified.", State: after}
}

func (m *resourceManager) dockerContext(ctx context.Context) (string, string, error) {
	nameOutput, err := m.runner.Output(ctx, "docker", "context", "show")
	if err != nil {
		return "", "", errors.New(friendlyCommandOutputError("cannot read active Docker context", nameOutput, err))
	}
	name := strings.TrimSpace(string(nameOutput))
	inspectOutput, err := m.runner.Output(ctx, "docker", "context", "inspect", name)
	if err != nil {
		return name, "", errors.New(friendlyCommandOutputError("cannot inspect active Docker context", inspectOutput, err))
	}
	var contexts []struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if err := json.Unmarshal(inspectOutput, &contexts); err != nil || len(contexts) != 1 {
		return name, "", errors.New("Docker returned unreadable context information")
	}
	return name, contexts[0].Endpoints["docker"].Host, nil
}

func (m *resourceManager) discoverOracleService(ctx context.Context) (string, error) {
	if !regularFile(filepath.Join(m.root, composeFileName)) {
		return "", fmt.Errorf("%s is missing", composeFileName)
	}
	output, err := m.composeOutput(ctx, "config", "--services")
	if err != nil {
		return "", errors.New(friendlyCommandOutputError("Compose service discovery failed", output, err))
	}
	var services []string
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		if service := strings.TrimSpace(scanner.Text()); service != "" {
			services = append(services, service)
		}
	}
	for _, service := range services {
		if service == serviceName {
			return service, nil
		}
	}
	if len(services) == 1 {
		return services[0], nil
	}
	if len(services) == 0 {
		return "", errors.New("Compose defines no services")
	}
	return "", errors.New("Compose defines multiple services and none is named oracle")
}

func (m *resourceManager) composeOutput(ctx context.Context, args ...string) ([]byte, error) {
	composePath := filepath.Join(m.root, composeFileName)
	fullArgs := []string{"compose", "--project-directory", m.root, "--file", composePath}
	fullArgs = append(fullArgs, args...)
	return m.runner.Output(ctx, "docker", fullArgs...)
}

func readResourceSettings(path string) (int, int64, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	values, err := parseEnvironment(contents)
	if err != nil {
		return 0, 0, err
	}
	var cpus int
	if value := strings.TrimSpace(values[cpuSettingKey]); value != "" {
		cpus, err = strconv.Atoi(value)
		if err != nil || cpus <= 0 {
			return 0, 0, fmt.Errorf("%s must be a positive whole number", cpuSettingKey)
		}
	}
	var memory int64
	if value := strings.TrimSpace(values[memorySettingKey]); value != "" {
		memory, err = parseByteSize(value)
		if err != nil || memory <= 0 {
			return 0, 0, fmt.Errorf("%s is invalid", memorySettingKey)
		}
	}
	return cpus, memory, nil
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
		values[key] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	return values, scanner.Err()
}

func updateResourceSettings(path string, cpus int, memory string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New(".env is missing; run oralab setup first")
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

func parseContainerStats(output []byte, state *resourceState) error {
	var stats struct {
		CPUPercent string `json:"CPUPerc"`
		Memory     string `json:"MemUsage"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &stats); err != nil {
		return errors.New("Docker returned unreadable resource statistics")
	}
	cpuText := strings.TrimSuffix(strings.TrimSpace(stats.CPUPercent), "%")
	cpu, err := strconv.ParseFloat(cpuText, 64)
	if err != nil {
		return errors.New("Docker returned an unreadable CPU statistic")
	}
	memoryUsed, _, found := strings.Cut(stats.Memory, "/")
	if !found {
		return errors.New("Docker returned an unreadable memory statistic")
	}
	memory, err := parseByteSize(strings.TrimSpace(memoryUsed))
	if err != nil {
		return errors.New("Docker returned an unreadable memory statistic")
	}
	state.CPUPercent = cpu
	state.MemoryUsageBytes = memory
	state.StatsAvailable = true
	return nil
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
	return endpoint == "" || strings.HasPrefix(endpoint, "unix://") || strings.HasPrefix(endpoint, "npipe://")
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

func friendlyCommandError(prefix string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return prefix + ": operation timed out"
	}
	return prefix + "."
}

func friendlyCommandOutputError(prefix string, output []byte, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return prefix + ": operation timed out."
	}
	message := strings.TrimSpace(string(output))
	if len(message) > 400 {
		message = message[:400] + "…"
	}
	if message == "" {
		return prefix + "."
	}
	return prefix + ": " + message
}
