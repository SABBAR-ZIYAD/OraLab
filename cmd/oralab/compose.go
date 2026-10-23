package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	composeFileName = "compose.yaml"
	composeProject  = "oralab"
	composeService  = "oracle"
	stateFileName   = ".oralab-state"
	legacyContainer = "oralab-oracle"
)

type composeRunner interface {
	Run(context.Context, string, []string, io.Reader, io.Writer, io.Writer) error
}

type systemComposeRunner struct{}

func (systemComposeRunner) Run(ctx context.Context, directory string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := exec.CommandContext(ctx, "docker", args...)
	command.Dir = directory
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func (a app) composeRunner() composeRunner {
	if a.runner != nil {
		return a.runner
	}
	return systemComposeRunner{}
}

func composeArguments(root string, args ...string) []string {
	prefix := []string{
		"compose", "--project-name", composeProject,
		"--project-directory", root,
		"--env-file", filepath.Join(root, environmentFile),
		"--file", filepath.Join(root, composeFileName),
	}
	return append(prefix, args...)
}

func (a app) runCompose(ctx context.Context, root string, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	return a.composeRunner().Run(ctx, root, composeArguments(root, args...), stdin, stdout, stderr)
}

func (a app) runDocker(ctx context.Context, root string, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	return a.composeRunner().Run(ctx, root, args, stdin, stdout, stderr)
}

func (a app) checkCompose(ctx context.Context, root string) error {
	var stdout, stderr bytes.Buffer
	if err := a.runDocker(ctx, root, nil, &stdout, &stderr, "compose", "version"); err != nil {
		return commandError("Docker Compose V2 is required", err, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := a.runDocker(ctx, root, nil, &stdout, &stderr, "context", "inspect", "--format", "{{json .Endpoints.docker.Host}}"); err != nil {
		return commandError("inspect the active Docker context", err, stderr.String())
	}
	endpoint := strings.Trim(strings.TrimSpace(stdout.String()), "\"")
	if !isLocalDockerEndpoint(endpoint) {
		return fmt.Errorf("the active Docker endpoint %q is remote; OraLab only manages a local Docker Engine", endpoint)
	}
	if a.getenv != nil {
		if override := strings.TrimSpace(a.getenv("DOCKER_HOST")); override != "" && !isLocalDockerEndpoint(override) {
			return fmt.Errorf("DOCKER_HOST selects remote endpoint %q; OraLab only manages a local Docker Engine", override)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if err := a.runDocker(ctx, root, nil, &stdout, &stderr, "info", "--format", "{{json .ServerVersion}}"); err != nil {
		return commandError("Docker Engine is unavailable", err, stderr.String())
	}
	return nil
}

func commandError(prefix string, err error, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return fmt.Errorf("%s: %s", prefix, detail)
}

func ensureComposeFile(root string) error {
	path := filepath.Join(root, composeFileName)
	if regularFile(path) {
		return nil
	}
	if _, err := os.Stat(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(embeddedComposeFile); err != nil {
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

func requireComposeFile(root string) error {
	if regularFile(filepath.Join(root, composeFileName)) {
		return nil
	}
	return errors.New("compose.yaml is missing; this looks like an OraLab v1 lab. Read docs/migration.md before creating or copying a Compose file so the existing oralab-oradata volume is not attached to two Oracle containers")
}

func legacyContainerExists(ctx context.Context, a app, root string) (bool, error) {
	var stdout, stderr bytes.Buffer
	err := a.runDocker(ctx, root, nil, &stdout, &stderr, "container", "inspect", legacyContainer)
	if err == nil {
		return true, nil
	}
	if strings.Contains(strings.ToLower(stderr.String()), "no such object") || strings.Contains(strings.ToLower(stderr.String()), "not found") {
		return false, nil
	}
	return false, commandError("inspect existing OraLab container", err, stderr.String())
}

func createExternalVolume(ctx context.Context, a app, root string) error {
	var stdout, stderr bytes.Buffer
	err := a.runDocker(ctx, root, nil, &stdout, &stderr, "volume", "inspect", oracleVolumeName)
	if err == nil {
		return errors.New("the persistent volume oralab-oradata already exists; OraLab will not attach an unknown existing volume to a new Compose lab. Read docs/migration.md")
	}
	if !strings.Contains(strings.ToLower(stderr.String()), "no such volume") && !strings.Contains(strings.ToLower(stderr.String()), "not found") {
		return commandError("inspect Oracle data volume", err, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if err := a.runDocker(ctx, root, nil, &stdout, &stderr, "volume", "create", "--label", managedLabel+"=true", oracleVolumeName); err != nil {
		return commandError("create persistent Oracle data volume", err, stderr.String())
	}
	return nil
}

func inspectExternalVolume(ctx context.Context, a app, root string) (bool, bool, error) {
	var stdout, stderr bytes.Buffer
	err := a.runDocker(ctx, root, nil, &stdout, &stderr,
		"volume", "inspect", "--format", "{{json .Labels}}", oracleVolumeName)
	if err != nil {
		detail := strings.ToLower(stderr.String())
		if strings.Contains(detail, "no such volume") || strings.Contains(detail, "not found") {
			return false, false, nil
		}
		return false, false, commandError("inspect Oracle data volume", err, stderr.String())
	}
	labels := map[string]string{}
	if strings.TrimSpace(stdout.String()) != "null" {
		if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &labels); err != nil {
			return true, false, fmt.Errorf("read Oracle data volume labels: %w", err)
		}
	}
	return true, labels[managedLabel] == "true", nil
}

type composeConfig struct {
	Services map[string]struct {
		Image string `json:"image"`
	} `json:"services"`
}

func (a app) configuredImage(ctx context.Context, root string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := a.runCompose(ctx, root, nil, &stdout, &stderr, "config", "--format", "json"); err != nil {
		return "", commandError("validate compose.yaml", err, stderr.String())
	}
	var config composeConfig
	if err := json.Unmarshal(stdout.Bytes(), &config); err != nil {
		return "", fmt.Errorf("read rendered Compose configuration: %w", err)
	}
	service, ok := config.Services[composeService]
	if !ok || strings.TrimSpace(service.Image) == "" {
		return "", errors.New("compose.yaml must define services.oracle.image")
	}
	return strings.TrimSpace(service.Image), nil
}

type labState struct {
	Image string
}

func readLabState(root string) (labState, error) {
	contents, err := os.ReadFile(filepath.Join(root, stateFileName))
	if errors.Is(err, os.ErrNotExist) {
		return labState{}, nil
	}
	if err != nil {
		return labState{}, err
	}
	values, err := parseEnvironment(contents)
	if err != nil {
		return labState{}, err
	}
	return labState{Image: strings.TrimSpace(values["IMAGE"])}, nil
}

func writeLabState(root, image string) error {
	contents := []byte("# Created by OraLab. Do not edit this file to bypass image safety checks.\nIMAGE=" + image + "\n")
	path := filepath.Join(root, stateFileName)
	return os.WriteFile(path, contents, 0o600)
}

func ensureImageIsSafe(root, image string) error {
	state, err := readLabState(root)
	if err != nil {
		return fmt.Errorf("read OraLab state: %w", err)
	}
	if state.Image != "" && state.Image != image {
		return fmt.Errorf("compose.yaml now selects %q, but the persistent volume was previously started with %q. OraLab will not treat an image change as an automatic database upgrade. Restore the prior image or follow the explicit backup and migration procedure in docs/migration.md", image, state.Image)
	}
	return nil
}

type composePS struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	Service  string `json:"Service"`
	State    string `json:"State"`
	Health   string `json:"Health"`
	ExitCode int    `json:"ExitCode"`
}

func (a app) composeServiceState(ctx context.Context, root string) (composePS, bool, error) {
	var stdout, stderr bytes.Buffer
	if err := a.runCompose(ctx, root, nil, &stdout, &stderr, "ps", "--all", "--format", "json", composeService); err != nil {
		return composePS{}, false, commandError("inspect Oracle Compose service", err, stderr.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var state composePS
		if err := json.Unmarshal([]byte(line), &state); err != nil {
			return composePS{}, false, fmt.Errorf("read Compose service state: %w", err)
		}
		if state.Service == composeService || state.Service == "" {
			return state, true, nil
		}
	}
	return composePS{}, false, nil
}

func (a app) waitForComposeReadiness(ctx context.Context, root string, report func(composePS)) (composePS, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var previous string
	for {
		state, exists, err := a.composeServiceState(ctx, root)
		if err != nil {
			return composePS{}, err
		}
		if !exists {
			return composePS{}, errors.New("Compose did not create the Oracle service")
		}
		if state.State == "exited" || state.State == "dead" {
			return state, fmt.Errorf("Oracle container stopped with exit code %d", state.ExitCode)
		}
		status := state.State + "/" + state.Health
		if report != nil && status != previous {
			report(state)
			previous = status
		}
		if state.State == "running" && (state.Health == "healthy" || state.Health == "") {
			return state, nil
		}
		select {
		case <-ctx.Done():
			return state, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a app) v2Up(root string, seconds int) int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()
	if err := requireComposeFile(root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if err := a.checkCompose(ctx, root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	legacy, err := legacyContainerExists(ctx, a, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if legacy {
		fmt.Fprintln(a.stderr, "oralab: found the legacy oralab-oracle container. OraLab V2 refuses to attach Compose to oralab-oradata while that container exists. Read docs/migration.md; no data was changed.")
		return 1
	}
	image, err := a.configuredImage(ctx, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if err := ensureImageIsSafe(root, image); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, "[..] Starting Oracle through Docker Compose")
	if err := a.runCompose(ctx, root, a.stdin, a.stdout, a.stderr, "up", "--detach", composeService); err != nil {
		fmt.Fprintf(a.stderr, "oralab: start Oracle with Docker Compose: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, "[..] Waiting for Oracle readiness")
	state, err := a.waitForComposeReadiness(ctx, root, func(update composePS) {
		if update.Health != "" {
			fmt.Fprintf(a.stdout, "[..] Oracle is %s (health=%s)\n", update.State, update.Health)
		}
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: Oracle did not become ready: %v\n", err)
		return 1
	}
	if err := writeLabState(root, image); err != nil {
		fmt.Fprintf(a.stderr, "oralab: record image safety state: %v\n", err)
		return 1
	}
	if state.Health == "" {
		fmt.Fprintln(a.stdout, "[WARN] The configured image has no Compose-visible health check; the container is running but database readiness was not independently verified.")
	} else {
		fmt.Fprintln(a.stdout, "[OK] Oracle Database is running and ready")
	}
	if err := a.startManager(root); err != nil {
		fmt.Fprintf(a.stderr, "[WARN] OraLab Manager did not start automatically: %v\nRun: oralab ui\n", err)
	} else {
		fmt.Fprintln(a.stdout, "OraLab Manager: http://127.0.0.1:8765")
	}
	fmt.Fprintln(a.stdout, "SQL listener: 127.0.0.1:1521")
	fmt.Fprintln(a.stdout, "EM Express: https://127.0.0.1:5500/em/ (availability depends on the selected image and database configuration)")
	return 0
}

func (a app) startManager(root string) error {
	if a.runner != nil {
		return nil // Unit tests and embedded callers do not launch child processes.
	}
	if managerAvailable() {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, "--project-dir", root, "serve", "--no-open")
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	configureDetachedProcess(command)
	if err := command.Start(); err != nil {
		return err
	}
	_ = command.Process.Release()
	for attempt := 0; attempt < 30; attempt++ {
		if managerAvailable() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("manager process did not begin listening on 127.0.0.1:8765")
}

func managerAvailable() bool {
	client := http.Client{Timeout: 250 * time.Millisecond}
	response, err := client.Get("http://127.0.0.1:8765/")
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode == http.StatusOK && response.Header.Get("X-OraLab-Manager") == "2"
}

func (a app) v2Stop(root string, seconds int) int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds+60)*time.Second)
	defer cancel()
	if err := requireComposeFile(root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if err := a.checkCompose(ctx, root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	legacy, err := legacyContainerExists(ctx, a, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if legacy {
		fmt.Fprintln(a.stderr, "oralab: legacy oralab-oracle container detected; V2 will not operate on it. Use the V1 binary to stop it and follow docs/migration.md.")
		return 1
	}
	if err := a.runCompose(ctx, root, nil, a.stdout, a.stderr, "stop", "--timeout", fmt.Sprint(seconds), composeService); err != nil {
		fmt.Fprintf(a.stderr, "oralab: stop Oracle with Docker Compose: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, "[OK] Oracle stopped; the oralab-oradata volume was preserved")
	return 0
}

func (a app) v2Restart(root string, seconds int) int {
	if code := a.v2Stop(root, seconds); code != 0 {
		return code
	}
	return a.v2Up(root, defaultWait)
}

func (a app) v2Doctor(root string, args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(a.stderr, "usage: oralab doctor")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), stateTimeout)
	defer cancel()
	fmt.Fprintln(a.stdout, "OraLab V2 environment check")
	if err := a.checkCompose(ctx, root); err != nil {
		fmt.Fprintf(a.stderr, "[FAIL] %v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, "[PASS] Docker Compose V2 is available")
	fmt.Fprintf(a.stdout, "[PASS] OraLab CLI platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if runtime.GOARCH != "amd64" {
		fmt.Fprintln(a.stderr, "[WARN] The default Oracle 19c Enterprise image targets AMD64; this host may require unsupported or slow emulation.")
	}
	if err := requireComposeFile(root); err != nil {
		fmt.Fprintf(a.stderr, "[FAIL] %v\n", err)
		return 1
	}
	if !regularFile(filepath.Join(root, environmentFile)) {
		fmt.Fprintln(a.stderr, "[FAIL] Local .env is missing; run oralab init")
		return 1
	}
	image, err := a.configuredImage(ctx, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "[FAIL] %v\n", err)
		return 1
	}
	fmt.Fprintf(a.stdout, "[PASS] compose.yaml selects: %s\n", image)
	legacy, err := legacyContainerExists(ctx, a, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "[FAIL] %v\n", err)
		return 1
	}
	if legacy {
		fmt.Fprintln(a.stderr, "[WARN] Legacy oralab-oracle container detected. Compose migration is blocked until manual handover; see docs/migration.md.")
		return 1
	}
	volumeExists, volumeManaged, err := inspectExternalVolume(ctx, a, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "[FAIL] %v\n", err)
		return 1
	}
	if !volumeExists {
		fmt.Fprintln(a.stderr, "[FAIL] External volume oralab-oradata is missing; run oralab init for a new lab.")
		return 1
	}
	if !volumeManaged {
		fmt.Fprintln(a.stderr, "[FAIL] External volume oralab-oradata does not carry OraLab's ownership label; refusing automatic use.")
		return 1
	}
	fmt.Fprintln(a.stdout, "[PASS] External volume oralab-oradata is present and OraLab-managed")
	if err := ensureImageIsSafe(root, image); err != nil {
		fmt.Fprintf(a.stderr, "[WARN] %v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, "[PASS] No image-safety conflict was detected")
	service, exists, err := a.composeServiceState(ctx, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "[FAIL] %v\n", err)
		return 1
	}
	if !exists {
		fmt.Fprintln(a.stdout, "[PASS] Oracle service is not created yet; runtime health and utilities will be checked after oralab up")
		return 0
	}
	if service.State != "running" {
		fmt.Fprintf(a.stderr, "[WARN] Oracle service exists but is %s\n", service.State)
		return 0
	}
	if service.Health == "healthy" {
		fmt.Fprintln(a.stdout, "[PASS] Oracle image health check reports healthy")
	} else if service.Health == "" {
		fmt.Fprintln(a.stderr, "[WARN] The selected image exposes no Compose-visible health check; database readiness cannot be independently verified")
	} else {
		fmt.Fprintf(a.stderr, "[WARN] Oracle image health is %s\n", service.Health)
	}
	if err := a.checkOracleUtilities(ctx, root); err != nil {
		fmt.Fprintf(a.stderr, "[WARN] %v\n", err)
	} else {
		fmt.Fprintln(a.stdout, "[PASS] sqlplus, rman, lsnrctl, expdp, impdp, and dbca are available")
	}
	if connection, err := net.DialTimeout("tcp4", "127.0.0.1:5500", 500*time.Millisecond); err == nil {
		_ = connection.Close()
		fmt.Fprintln(a.stdout, "[PASS] Local port 5500 accepts TCP; verify EM Express HTTPS and database-side configuration separately")
	} else {
		fmt.Fprintln(a.stderr, "[WARN] Local port 5500 is not accepting TCP; EM Express is unavailable or still initializing")
	}
	return 0
}

func (a app) checkOracleUtilities(ctx context.Context, root string) error {
	var stdout, stderr bytes.Buffer
	script := `missing=""; for utility in sqlplus rman lsnrctl expdp impdp dbca; do command -v "$utility" >/dev/null 2>&1 || missing="$missing $utility"; done; test -z "$missing" || { echo "missing:$missing" >&2; exit 1; }`
	if err := a.runCompose(ctx, root, nil, &stdout, &stderr,
		"exec", "--no-tty", "--user", "oracle", composeService, "sh", "-c", script); err != nil {
		return commandError("selected image does not provide the expected Oracle utility environment", err, stderr.String())
	}
	return nil
}

func (a app) v2Status(root string) int {
	ctx, cancel := context.WithTimeout(context.Background(), stateTimeout)
	defer cancel()
	if err := requireComposeFile(root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if err := a.checkCompose(ctx, root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	legacy, err := legacyContainerExists(ctx, a, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if legacy {
		fmt.Fprintln(a.stderr, "oralab: legacy oralab-oracle container detected; V2 status is blocked to avoid presenting it as a Compose-managed lab. See docs/migration.md.")
		return 1
	}
	image, err := a.configuredImage(ctx, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	state, exists, err := a.composeServiceState(ctx, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.stdout, "Compose project: %s\nOracle image: %s\n", composeProject, image)
	if err := ensureImageIsSafe(root, image); err != nil {
		fmt.Fprintf(a.stderr, "[WARN] %v\n", err)
	}
	if !exists {
		fmt.Fprintln(a.stdout, "Oracle: not created; run oralab up")
		return 0
	}
	readiness := "unknown"
	if state.State == "running" && state.Health == "healthy" {
		readiness = "ready"
	} else if state.State == "running" && state.Health == "" {
		readiness = "not verified (no health check)"
	} else if state.State == "running" {
		readiness = "initializing"
	}
	fmt.Fprintf(a.stdout, "Oracle: %s; readiness=%s; health=%s\n", state.State, readiness, state.Health)
	serviceName := "ORCLPDB1"
	if config, err := readLabConfig(filepath.Join(root, environmentFile)); err == nil {
		serviceName = config.PDB
	}
	fmt.Fprintf(a.stdout, "Listener: 127.0.0.1:1521  Service: %s\n", serviceName)
	fmt.Fprintln(a.stdout, "EM Express: https://127.0.0.1:5500/em/ (verify availability in the selected image)")
	return 0
}

func (a app) v2Logs(root string, options logOptions) int {
	ctx, stop := signalContext()
	defer stop()
	if err := requireComposeFile(root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if err := a.checkCompose(ctx, root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	legacy, err := legacyContainerExists(ctx, a, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if legacy {
		fmt.Fprintln(a.stderr, "oralab: legacy oralab-oracle container detected; V2 will not access a competing Compose service. See docs/migration.md.")
		return 1
	}
	args := []string{"logs", "--no-color", "--tail", options.Tail}
	if options.Follow {
		args = append(args, "--follow")
	}
	if options.Since != "" {
		args = append(args, "--since", options.Since)
	}
	if options.Timestamps {
		args = append(args, "--timestamps")
	}
	args = append(args, composeService)
	stdout := io.Writer(a.stdout)
	stderr := io.Writer(a.stderr)
	var stdoutRedactor, stderrRedactor *redactingWriter
	if config, err := readLabConfig(filepath.Join(root, environmentFile)); err == nil && config.Password != "" {
		stdoutRedactor = &redactingWriter{destination: a.stdout, secret: config.Password}
		stderrRedactor = &redactingWriter{destination: a.stderr, secret: config.Password}
		stdout, stderr = stdoutRedactor, stderrRedactor
	}
	err = a.runCompose(ctx, root, a.stdin, stdout, stderr, args...)
	if stdoutRedactor != nil {
		_ = stdoutRedactor.Flush()
		_ = stderrRedactor.Flush()
	}
	if err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		fmt.Fprintf(a.stderr, "oralab: read Oracle logs: %v\n", err)
		return 1
	}
	return 0
}

type redactingWriter struct {
	destination io.Writer
	secret      string
	pending     string
}

func (w *redactingWriter) Write(contents []byte) (int, error) {
	originalLength := len(contents)
	w.pending += string(contents)
	for {
		if index := strings.Index(w.pending, w.secret); index >= 0 {
			if _, err := io.WriteString(w.destination, w.pending[:index]+"[REDACTED]"); err != nil {
				return 0, err
			}
			w.pending = w.pending[index+len(w.secret):]
			continue
		}
		keep := len(w.secret) - 1
		if keep < 0 {
			keep = 0
		}
		if len(w.pending) <= keep {
			return originalLength, nil
		}
		emit := len(w.pending) - keep
		if _, err := io.WriteString(w.destination, w.pending[:emit]); err != nil {
			return 0, err
		}
		w.pending = w.pending[emit:]
		return originalLength, nil
	}
}

func (w *redactingWriter) Flush() error {
	if w.pending == "" {
		return nil
	}
	_, err := io.WriteString(w.destination, strings.ReplaceAll(w.pending, w.secret, "[REDACTED]"))
	w.pending = ""
	return err
}

func (a app) v2Exec(root string, command []string, tty bool) int {
	ctx, stop := signalContext()
	defer stop()
	if err := requireComposeFile(root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if err := a.checkCompose(ctx, root); err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	legacy, err := legacyContainerExists(ctx, a, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}
	if legacy {
		fmt.Fprintln(a.stderr, "oralab: legacy oralab-oracle container detected; V2 will not execute against a competing Compose service. See docs/migration.md.")
		return 1
	}
	args := []string{"exec", "--user", "oracle"}
	if !tty {
		args = append(args, "--no-tty")
	}
	args = append(args, composeService)
	args = append(args, command...)
	if err := a.runCompose(ctx, root, a.stdin, a.stdout, a.stderr, args...); err != nil {
		fmt.Fprintf(a.stderr, "oralab: execute in Oracle Compose service: %v\n", err)
		return exitCode(err)
	}
	return 0
}

func exitCode(err error) int {
	var exitErr interface{ ExitCode() int }
	if errors.As(err, &exitErr) && exitErr.ExitCode() != 0 {
		return exitErr.ExitCode()
	}
	return 1
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
