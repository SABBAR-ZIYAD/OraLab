package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// composeManager is the browser-facing control boundary. It exposes only the
// fixed OraLab lifecycle actions and never accepts arbitrary Docker or shell
// commands from HTTP requests.
type composeManager struct {
	app  app
	root string
	opMu sync.Mutex
	busy atomic.Bool
}

func newComposeManager(a app, root string) *composeManager {
	return &composeManager{app: a, root: root}
}

func (m *composeManager) state(ctx context.Context) managerState {
	state := managerState{
		Host:      hostState{OS: hostOSName(runtime.GOOS), Architecture: runtime.GOARCH},
		Docker:    dockerState{APIAvailable: false, Reachable: true, LocalContext: true, Context: "Docker Compose"},
		Oracle:    oracleState{Service: composeService, Status: "not created", Health: "unknown", Readiness: "unknown"},
		CheckedAt: time.Now().UTC(),
	}
	if err := m.app.checkCompose(ctx, m.root); err != nil {
		state.Docker.Reachable = false
		state.Docker.Error = err.Error()
		return state
	}
	legacy, err := legacyContainerExists(ctx, m.app, m.root)
	if err != nil {
		state.Oracle.Error = err.Error()
		return state
	}
	if legacy {
		state.Oracle.Status = "legacy migration blocked"
		state.Oracle.Error = "Legacy oralab-oracle container detected; follow docs/migration.md before using V2."
		return state
	}
	image, err := m.app.configuredImage(ctx, m.root)
	if err != nil {
		state.Oracle.Error = err.Error()
		return state
	}
	state.Oracle.Image = image
	if config, err := readLabConfig(filepath.Join(m.root, environmentFile)); err == nil {
		state.Resources.ConfiguredCPUs = config.CPUs
		state.Resources.ConfiguredMemoryBytes = config.MemoryBytes
		state.Oracle.SID = config.SID
		state.Oracle.PDB = config.PDB
	} else {
		state.Resources.Error = err.Error()
	}
	service, exists, err := m.app.composeServiceState(ctx, m.root)
	if err != nil {
		state.Oracle.Error = err.Error()
		return state
	}
	if !exists {
		return state
	}
	state.Oracle.Exists = true
	state.Oracle.ContainerID = shortContainerID(service.ID)
	state.Oracle.Status = service.State
	state.Oracle.Health = service.Health
	state.Oracle.Running = service.State == "running"
	if service.State == "running" && service.Health == "healthy" {
		state.Oracle.Readiness = "ready"
	} else if service.State == "running" && service.Health == "" {
		state.Oracle.Readiness = "not verified"
	} else if service.State == "running" {
		state.Oracle.Readiness = "initializing"
	} else {
		state.Oracle.Readiness = "stopped"
	}
	state.Storage = []storageMount{{Type: "volume", Name: oracleVolumeName, Destination: oracleDataPath}}
	state.Resources.AppliedCPUs = float64(state.Resources.ConfiguredCPUs)
	state.Resources.AppliedMemoryBytes = state.Resources.ConfiguredMemoryBytes
	if state.Oracle.Running {
		cpu, memory, statsErr := m.containerStats(ctx, service.ID)
		if statsErr != nil {
			state.Resources.Error = statsErr.Error()
		} else {
			state.Resources.CPUPercent = cpu
			state.Resources.MemoryUsageBytes = memory
			state.Resources.StatsAvailable = true
		}
	}
	return state
}

func (m *composeManager) containerStats(ctx context.Context, containerID string) (float64, int64, error) {
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	if err := m.app.runDocker(ctx, m.root, nil, &output, &errorOutput,
		"stats", "--no-stream", "--format", "{{json .}}", containerID); err != nil {
		return 0, 0, fmt.Errorf("read container metrics: %s", strings.TrimSpace(errorOutput.String()))
	}
	var stats struct {
		CPUPercent  string `json:"CPUPerc"`
		MemoryUsage string `json:"MemUsage"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &stats); err != nil {
		return 0, 0, fmt.Errorf("decode container metrics: %w", err)
	}
	cpu, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(stats.CPUPercent, "%")), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("decode CPU metric %q", stats.CPUPercent)
	}
	used, _, _ := strings.Cut(stats.MemoryUsage, "/")
	memory, err := parseByteSize(strings.TrimSpace(used))
	if err != nil {
		return 0, 0, fmt.Errorf("decode memory metric %q", stats.MemoryUsage)
	}
	return cpu, memory, nil
}

func (m *composeManager) logs(ctx context.Context) (string, error) {
	output := boundedBuffer{remaining: 1024 * 1024}
	var errorOutput bytes.Buffer
	err := m.app.runCompose(ctx, m.root, nil, &output, &errorOutput,
		"logs", "--no-color", "--tail", defaultLogTail, composeService)
	if err != nil {
		return "", fmt.Errorf("read bounded Oracle logs: %s", errorOutput.String())
	}
	contents := output.String()
	if config, err := readLabConfig(filepath.Join(m.root, environmentFile)); err == nil && config.Password != "" {
		contents = strings.ReplaceAll(contents, config.Password, "[REDACTED]")
	}
	return contents, nil
}

type boundedBuffer struct {
	bytes.Buffer
	remaining int
}

func (b *boundedBuffer) Write(contents []byte) (int, error) {
	originalLength := len(contents)
	if b.remaining > 0 {
		amount := len(contents)
		if amount > b.remaining {
			amount = b.remaining
		}
		_, _ = b.Buffer.Write(contents[:amount])
		b.remaining -= amount
	}
	return originalLength, nil
}

func (m *composeManager) lifecycle(ctx context.Context, request lifecycleRequest) operationResult {
	if !m.opMu.TryLock() {
		return operationResult{Message: "Another OraLab operation is already in progress.", State: m.state(ctx)}
	}
	defer m.opMu.Unlock()
	m.busy.Store(true)
	defer m.busy.Store(false)
	quiet := m.app
	quiet.stdout = io.Discard
	quiet.stderr = io.Discard
	code := 1
	switch request.Action {
	case "start":
		code = quiet.v2Up(m.root, defaultWait)
	case "stop":
		code = quiet.v2Stop(m.root, defaultStop)
	case "restart":
		code = quiet.v2Restart(m.root, defaultStop)
	default:
		return operationResult{Message: "Unsupported lifecycle action.", State: m.state(ctx)}
	}
	state := m.state(ctx)
	if code != 0 {
		return operationResult{Message: "Compose could not complete the requested Oracle lifecycle action. Use oralab status or oralab logs for details.", State: state}
	}
	return operationResult{OK: true, Applied: true, Message: "Oracle lifecycle operation completed through Docker Compose.", State: state}
}
