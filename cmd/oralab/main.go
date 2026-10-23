package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultWait       = 1800
	defaultStop       = 600
	environmentSample = ".env.example"
	environmentFile   = ".env"
)

var version = "dev"

type engineFactory func() (dockerEngine, error)

type app struct {
	stdin        io.Reader
	stdout       io.Writer
	stderr       io.Writer
	getwd        func() (string, error)
	getenv       func(string) string
	newEngine    engineFactory
	readPassword func() (string, error)
}

func main() {
	a := app{
		stdin:        os.Stdin,
		stdout:       os.Stdout,
		stderr:       os.Stderr,
		getwd:        os.Getwd,
		getenv:       os.Getenv,
		newEngine:    newMobyEngine,
		readPassword: readPasswordFromTerminal,
	}
	os.Exit(a.run(os.Args[1:]))
}

func (a app) run(args []string) int {
	projectDir, args, err := a.parseGlobalOptions(args)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 2
	}
	if len(args) == 0 {
		a.printHelp(a.stdout)
		return 0
	}
	command := args[0]
	commandArgs := args[1:]
	switch command {
	case "help", "-h", "--help":
		a.printHelp(a.stdout)
		return 0
	case "version", "--version":
		fmt.Fprintf(a.stdout, "oralab %s\n", version)
		return 0
	}
	if command == "init" || command == "setup" {
		if command == "setup" {
			fmt.Fprintln(a.stderr, "oralab: \"setup\" is deprecated; use \"init\"")
		}
		root, err := a.resolveInitDir(projectDir)
		if err != nil {
			fmt.Fprintf(a.stderr, "oralab: %v\n", err)
			return 1
		}
		return a.init(root, commandArgs)
	}
	root, err := a.resolveProjectDir(projectDir)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}

	switch command {
	case "doctor":
		return a.doctor(root, commandArgs)
	case "ui":
		return a.ui(root, commandArgs)
	case "up":
		seconds, ok := parseTimeout(commandArgs, defaultWait)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab up [--timeout SECONDS]")
			return 2
		}
		return a.up(root, seconds)
	case "status":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab status")
			return 2
		}
		return a.status(root)
	case "logs":
		options, ok := parseLogOptions(commandArgs)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab logs [--no-follow] [--tail LINES] [--since VALUE] [--timestamps]")
			return 2
		}
		return a.logs(options)
	case "shell":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab shell")
			return 2
		}
		return a.exec([]string{"bash"}, true)
	case "exec":
		if len(commandArgs) > 0 && commandArgs[0] == "--" {
			commandArgs = commandArgs[1:]
		}
		if len(commandArgs) == 0 {
			fmt.Fprintln(a.stderr, "usage: oralab exec -- <native-command> [arguments...]")
			return 2
		}
		return a.exec(commandArgs, false)
	case "start":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab start")
			return 2
		}
		fmt.Fprintln(a.stderr, "oralab: \"start\" is deprecated; use \"up\"")
		return a.up(root, defaultWait)
	case "stop":
		seconds, ok := parseTimeout(commandArgs, defaultStop)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab stop [--timeout SECONDS]")
			return 2
		}
		return a.stop(seconds)
	case "restart":
		seconds, ok := parseTimeout(commandArgs, defaultStop)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab restart [--timeout SECONDS]")
			return 2
		}
		fmt.Fprintln(a.stderr, "oralab: \"restart\" is deprecated; use \"stop\" followed by \"up\"")
		return a.restart(seconds)
	default:
		fmt.Fprintf(a.stderr, "oralab: unknown command %q\n\n", command)
		a.printHelp(a.stderr)
		return 2
	}
}

func (a app) resolveInitDir(explicit string) (string, error) {
	if explicit == "" {
		explicit = a.getenv("ORALAB_PROJECT_DIR")
	}
	if explicit == "" {
		workingDirectory, err := a.getwd()
		if err != nil {
			return "", fmt.Errorf("determine current directory: %w", err)
		}
		explicit = workingDirectory
	}
	absolute, err := filepath.Abs(explicit)
	if err != nil {
		return "", fmt.Errorf("resolve project directory: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("open project directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project path is not a directory: %s", absolute)
	}
	return absolute, nil
}

func (a app) parseGlobalOptions(args []string) (string, []string, error) {
	var projectDir string
	for len(args) > 0 {
		switch args[0] {
		case "--project-dir":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return "", nil, errors.New("--project-dir requires a path")
			}
			projectDir = args[1]
			args = args[2:]
		case "--":
			return projectDir, args[1:], nil
		default:
			return projectDir, args, nil
		}
	}
	return projectDir, args, nil
}

func (a app) resolveProjectDir(explicit string) (string, error) {
	if explicit == "" {
		explicit = a.getenv("ORALAB_PROJECT_DIR")
	}
	if explicit != "" {
		absolute, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve project directory: %w", err)
		}
		if !isProjectDirectory(absolute) {
			return "", fmt.Errorf("%s is not an OraLab project directory", absolute)
		}
		return absolute, nil
	}
	workingDirectory, err := a.getwd()
	if err != nil {
		return "", fmt.Errorf("determine current directory: %w", err)
	}
	return findProjectDirectory(workingDirectory)
}

func findProjectDirectory(start string) (string, error) {
	current, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if isProjectDirectory(current) {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", errors.New("no OraLab project found; run this command inside the project or use --project-dir")
}

func isProjectDirectory(path string) bool {
	return regularFile(filepath.Join(path, environmentSample))
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (a app) openEngine() (dockerEngine, bool) {
	engine, err := a.newEngine()
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: connect to Docker Engine API: %v\n", err)
		return nil, false
	}
	return engine, true
}

func (a app) openLifecycleEngine(ctx context.Context, progress *cliProgress) (dockerEngine, bool) {
	progress.waiting("Connecting to the Docker Engine")
	engine, err := a.newEngine()
	if err != nil {
		progress.failed("Docker Engine client could not be created")
		fmt.Fprintf(a.stderr, "oralab: connect to Docker Engine API: %v\n", err)
		return nil, false
	}
	if !isLocalDockerEndpoint(engine.Endpoint()) {
		progress.failed("Docker Engine endpoint is not local")
		fmt.Fprintln(a.stderr, "oralab: refusing lifecycle changes through a remote Docker endpoint")
		_ = engine.Close()
		return nil, false
	}
	checkContext, cancel := context.WithTimeout(ctx, stateTimeout)
	defer cancel()
	info, err := engine.Info(checkContext)
	if err != nil {
		progress.failed("Docker Engine is unavailable")
		fmt.Fprintf(a.stderr, "oralab: connect to Docker Engine API: %v\n", err)
		_ = engine.Close()
		return nil, false
	}
	if info.ServerVersion != "" {
		progress.success("Docker Engine %s is available", info.ServerVersion)
	} else {
		progress.success("Docker Engine is available")
	}
	return engine, true
}

func (a app) up(root string, seconds int) int {
	progress := newCLIProgress(a.stdout)
	progress.heading()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()
	engine, ok := a.openLifecycleEngine(ctx, progress)
	if !ok {
		return 1
	}
	defer engine.Close()
	inspected, err := ensureOracleContainer(ctx, engine, root, progress)
	if err != nil {
		progress.failed("Oracle container preparation failed")
		fmt.Fprintf(a.stderr, "oralab: prepare Oracle container: %v\n", err)
		return 1
	}
	if !inspected.Running {
		progress.waiting("Starting the Oracle container")
		if err := engine.StartContainer(ctx, oracleContainerName); err != nil {
			progress.failed("Oracle container could not be started")
			fmt.Fprintf(a.stderr, "oralab: start Oracle container: %v\n", err)
			return 1
		}
		progress.success("Oracle container started")
	} else {
		progress.success("Oracle container is already running")
	}
	progress.waiting("Waiting for Oracle readiness")
	if err := waitForReadinessWithProgress(ctx, engine, oracleContainerName, progress.readinessUpdate); err != nil {
		progress.failed("Oracle did not become ready")
		fmt.Fprintf(a.stderr, "oralab: Oracle did not become ready: %v\n", err)
		return 1
	}
	progress.success("Oracle Database 19c is running and ready")
	return 0
}

func (a app) status(root string) int {
	engine, err := a.newEngine()
	manager := newResourceManager(root, engine, err)
	if engine != nil {
		defer engine.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), stateTimeout)
	defer cancel()
	state := manager.state(ctx)
	if !state.Docker.Reachable {
		fmt.Fprintf(a.stderr, "oralab: %s\n", state.Docker.Error)
		return 1
	}
	fmt.Fprintf(a.stdout, "Docker: %s %s (%s)\n", state.Docker.EngineOS, state.Docker.Version, state.Docker.Endpoint)
	fmt.Fprintf(a.stdout, "Oracle: %s; readiness=%s; health=%s\n", state.Oracle.Status, state.Oracle.Readiness, state.Oracle.Health)
	if state.Oracle.ContainerID != "" {
		fmt.Fprintf(a.stdout, "Container: %s (%s)\n", oracleContainerName, state.Oracle.ContainerID)
	}
	if state.Oracle.Error != "" {
		fmt.Fprintf(a.stderr, "oralab: %s\n", state.Oracle.Error)
		return 1
	}
	return 0
}

func (a app) logs(options logOptions) int {
	engine, ok := a.openEngine()
	if !ok {
		return 1
	}
	defer engine.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if options.Follow {
		fmt.Fprintln(a.stderr, "[..] Following Oracle logs; press Ctrl+C to stop")
	} else {
		fmt.Fprintln(a.stderr, "[..] Reading Oracle logs")
	}
	if err := engine.Logs(ctx, oracleContainerName, options, a.stdout, a.stderr); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(a.stderr, "oralab: read Oracle logs: %v\n", err)
		return 1
	}
	if !options.Follow {
		fmt.Fprintln(a.stderr, "[OK] Oracle log read completed")
	}
	return 0
}

func (a app) exec(command []string, tty bool) int {
	engine, ok := a.openEngine()
	if !ok {
		return 1
	}
	defer engine.Close()
	if !isLocalDockerEndpoint(engine.Endpoint()) {
		fmt.Fprintln(a.stderr, "oralab: refusing container execution through a remote Docker endpoint")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	exitCode, err := engine.Exec(ctx, oracleContainerName, command, tty, a.stdin, a.stdout, a.stderr)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: execute in Oracle container: %v\n", err)
		return 1
	}
	return exitCode
}

func (a app) stop(seconds int) int {
	progress := newCLIProgress(a.stdout)
	progress.heading()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds+60)*time.Second)
	defer cancel()
	engine, ok := a.openLifecycleEngine(ctx, progress)
	if !ok {
		return 1
	}
	defer engine.Close()
	progress.waiting("Inspecting the Oracle container")
	inspected, err := engine.InspectContainer(ctx, oracleContainerName)
	if errors.Is(err, errDockerObjectNotFound) {
		progress.success("Oracle container does not exist; nothing to stop")
		return 0
	}
	if err != nil {
		progress.failed("Oracle container inspection failed")
		fmt.Fprintf(a.stderr, "oralab: inspect Oracle container: %v\n", err)
		return 1
	}
	if !inspected.Managed {
		progress.failed("The existing container is not managed by OraLab")
		fmt.Fprintln(a.stderr, "oralab: the oralab-oracle container is not managed by OraLab")
		return 1
	}
	if !inspected.Running {
		progress.success("Oracle is already stopped; database files are preserved")
		return 0
	}
	progress.waiting("Stopping Oracle gracefully")
	if err := engine.StopContainer(ctx, oracleContainerName, seconds); err != nil {
		progress.failed("Oracle container could not be stopped")
		fmt.Fprintf(a.stderr, "oralab: stop Oracle container: %v\n", err)
		return 1
	}
	progress.success("Oracle stopped; database files are preserved")
	return 0
}

func (a app) restart(seconds int) int {
	progress := newCLIProgress(a.stdout)
	progress.heading()
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	engine, ok := a.openLifecycleEngine(ctx, progress)
	if !ok {
		return 1
	}
	defer engine.Close()
	progress.waiting("Inspecting the Oracle container")
	inspected, err := engine.InspectContainer(ctx, oracleContainerName)
	if errors.Is(err, errDockerObjectNotFound) {
		progress.failed("Oracle container has not been created")
		fmt.Fprintln(a.stderr, "oralab: Oracle container does not exist; run oralab up first")
		return 1
	}
	if err != nil {
		progress.failed("Oracle container inspection failed")
		fmt.Fprintf(a.stderr, "oralab: inspect Oracle container: %v\n", err)
		return 1
	}
	if !inspected.Managed {
		progress.failed("The existing container is not managed by OraLab")
		fmt.Fprintln(a.stderr, "oralab: the oralab-oracle container is not managed by OraLab")
		return 1
	}
	progress.waiting("Restarting the Oracle container")
	if err := engine.RestartContainer(ctx, oracleContainerName, seconds); err != nil {
		progress.failed("Oracle container could not be restarted")
		fmt.Fprintf(a.stderr, "oralab: restart Oracle container: %v\n", err)
		return 1
	}
	progress.success("Oracle container restarted")
	progress.waiting("Waiting for Oracle readiness")
	if err := waitForReadinessWithProgress(ctx, engine, oracleContainerName, progress.readinessUpdate); err != nil {
		progress.failed("Oracle did not become ready")
		fmt.Fprintf(a.stderr, "oralab: Oracle did not become ready: %v\n", err)
		return 1
	}
	progress.success("Oracle Database 19c restarted and ready")
	return 0
}

func (a app) doctor(root string, args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(a.stderr, "usage: oralab doctor")
		return 2
	}
	fmt.Fprintln(a.stdout, "OraLab environment check")
	failed := false
	engine, err := a.newEngine()
	if err != nil {
		fmt.Fprintf(a.stderr, "[FAIL] Docker Engine API client: %v\n", err)
		failed = true
	} else {
		defer engine.Close()
		ctx, cancel := context.WithTimeout(context.Background(), stateTimeout)
		defer cancel()
		info, infoErr := engine.Info(ctx)
		if infoErr != nil {
			fmt.Fprintf(a.stderr, "[FAIL] Docker Engine API: %v\n", infoErr)
			failed = true
		} else {
			fmt.Fprintf(a.stdout, "[PASS] Docker Engine %s at %s\n", info.ServerVersion, engine.Endpoint())
			if isLocalDockerEndpoint(engine.Endpoint()) {
				fmt.Fprintln(a.stdout, "[PASS] Docker endpoint is local")
			} else {
				fmt.Fprintln(a.stderr, "[WARN] Docker endpoint is remote; OraLab mutations are disabled")
			}
		}
	}
	envPath := filepath.Join(root, environmentFile)
	if regularFile(envPath) {
		fmt.Fprintln(a.stdout, "[PASS] Local .env exists")
		config, configErr := readLabConfig(envPath)
		if configErr != nil {
			fmt.Fprintf(a.stderr, "[FAIL] Local .env: %v\n", configErr)
			failed = true
		} else {
			fmt.Fprintf(a.stdout, "[PASS] Oracle image: %s\n", defaultOracleImage)
			if config.Password == "" {
				fmt.Fprintln(a.stderr, "[WARN] ORACLE_PWD is not configured")
			}
			if config.CPUs == 0 || config.MemoryBytes == 0 {
				fmt.Fprintln(a.stderr, "[WARN] Oracle CPU/RAM limits are not configured")
			}
		}
	} else if regularFile(filepath.Join(root, environmentSample)) {
		fmt.Fprintln(a.stderr, "[WARN] Local .env is missing; run: oralab init --cpus 4 --memory 6g --shm 1g")
	} else {
		fmt.Fprintln(a.stderr, "[FAIL] Both .env and .env.example are missing")
		failed = true
	}
	if failed {
		return 1
	}
	return 0
}

type initOptions struct {
	CPUs          int
	Memory        string
	MemoryBytes   int64
	Shm           string
	ShmBytes      int64
	PasswordStdin bool
}

func (a app) init(root string, args []string) int {
	options, err := parseInitOptions(args)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		fmt.Fprintln(a.stderr, "usage: oralab init --cpus COUNT --memory SIZE --shm SIZE [--password-stdin]")
		return 2
	}
	password, err := a.readInitialPassword(options.PasswordStdin)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: read database password: %v\n", err)
		return 1
	}
	configPath := filepath.Join(root, environmentFile)
	if err := createLabConfig(configPath, password, options); err != nil {
		fmt.Fprintf(a.stderr, "oralab: create local configuration: %v\n", err)
		return 1
	}
	if err := ensureEnvironmentSample(root); err != nil {
		_ = os.Remove(configPath)
		fmt.Fprintf(a.stderr, "oralab: create project marker: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.stdout, "Created %s for Oracle Database 19c Enterprise.\n", configPath)
	fmt.Fprintf(a.stdout, `Before the first pull, complete Oracle's one-time registry setup:
  %s
Sign in, open Database > enterprise, accept the terms, and create an auth token.
Once downloaded, OraLab reuses the local image without signing in on every use.
`, oracleRegistryURL)
	fmt.Fprintln(a.stdout, "Next: oralab up")
	return 0
}

func ensureEnvironmentSample(root string) error {
	path := filepath.Join(root, environmentSample)
	if regularFile(path) {
		return nil
	}
	if _, err := os.Stat(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) && regularFile(path) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(embeddedEnvironmentSample); err != nil {
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

func (a app) readInitialPassword(fromStdin bool) (string, error) {
	if fromStdin {
		contents, err := io.ReadAll(a.stdin)
		if err != nil {
			return "", err
		}
		password := strings.TrimPrefix(strings.TrimRight(string(contents), "\r\n"), "\ufeff")
		return validateInitialPassword(password)
	}
	if a.readPassword == nil {
		return "", errors.New("interactive password input is unavailable; use --password-stdin")
	}
	fmt.Fprint(a.stderr, "Oracle database password: ")
	password, err := a.readPassword()
	if err != nil {
		return "", err
	}
	fmt.Fprint(a.stderr, "Confirm Oracle database password: ")
	confirmation, err := a.readPassword()
	if err != nil {
		return "", err
	}
	if password != confirmation {
		return "", errors.New("passwords do not match")
	}
	return validateInitialPassword(password)
}

func validateInitialPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("password cannot be empty")
	}
	if strings.ContainsAny(password, "\r\n") {
		return "", errors.New("password cannot contain a newline")
	}
	return password, nil
}

func parseInitOptions(args []string) (initOptions, error) {
	var options initOptions
	seen := map[string]bool{}
	for len(args) > 0 {
		flag := args[0]
		if flag == "--password-stdin" {
			if seen[flag] {
				return initOptions{}, fmt.Errorf("%s cannot be repeated", flag)
			}
			options.PasswordStdin = true
			seen[flag] = true
			args = args[1:]
			continue
		}
		if len(args) < 2 || seen[flag] {
			return initOptions{}, fmt.Errorf("invalid init option %q", flag)
		}
		value := strings.TrimSpace(args[1])
		if value == "" {
			return initOptions{}, fmt.Errorf("%s requires a value", flag)
		}
		switch flag {
		case "--cpus":
			cpus, err := strconv.Atoi(value)
			if err != nil || cpus <= 0 || cpus > 1024 {
				return initOptions{}, errors.New("--cpus must be a positive whole number")
			}
			options.CPUs = cpus
		case "--memory":
			bytes, err := parseByteSize(value)
			if err != nil || bytes <= 0 {
				return initOptions{}, errors.New("--memory must be a positive size such as 6g")
			}
			options.Memory, options.MemoryBytes = value, bytes
		case "--shm":
			bytes, err := parseByteSize(value)
			if err != nil || bytes <= 0 {
				return initOptions{}, errors.New("--shm must be a positive size such as 1g")
			}
			options.Shm, options.ShmBytes = value, bytes
		default:
			return initOptions{}, fmt.Errorf("unknown init option %q", flag)
		}
		seen[flag] = true
		args = args[2:]
	}
	if options.CPUs == 0 || options.Memory == "" || options.Shm == "" {
		return initOptions{}, errors.New("--cpus, --memory, and --shm are required")
	}
	if options.ShmBytes > options.MemoryBytes {
		return initOptions{}, errors.New("--shm cannot exceed --memory")
	}
	return options, nil
}

func parseTimeout(args []string, fallback int) (int, bool) {
	if len(args) == 0 {
		return fallback, true
	}
	if len(args) != 2 || args[0] != "--timeout" {
		return 0, false
	}
	seconds, err := strconv.Atoi(args[1])
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return seconds, true
}

func parseLogOptions(args []string) (logOptions, bool) {
	options := logOptions{Follow: true, Tail: "all"}
	for len(args) > 0 {
		switch args[0] {
		case "--no-follow":
			options.Follow = false
			args = args[1:]
		case "--timestamps":
			options.Timestamps = true
			args = args[1:]
		case "--tail":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return logOptions{}, false
			}
			options.Tail = args[1]
			args = args[2:]
		case "--since":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return logOptions{}, false
			}
			options.Since = args[1]
			args = args[2:]
		default:
			return logOptions{}, false
		}
	}
	return options, true
}

func (a app) printHelp(writer io.Writer) {
	fmt.Fprintln(writer, `OraLab talks directly to the Docker Engine API; Oracle commands remain unchanged.

Usage:
  oralab [--project-dir PATH] <command>

Commands:
  init --cpus N --memory S --shm S [--password-stdin]
                              Configure this lab and securely collect its database password
  up [--timeout SECONDS]     Create/start Oracle and wait for readiness
  status                     Show Docker and Oracle container status
  stop [--timeout SECONDS]   Gracefully stop Oracle (default: 600 seconds)
  logs [OPTIONS]             Follow Oracle logs through the Engine API
  shell                      Enter the Oracle container shell
  exec -- COMMAND [ARGS...]  Run a native command unchanged in the container
  ui [--port PORT] [--no-open]
                              Open the local CPU/RAM resource manager
  doctor                     Check Docker Engine and project configuration
  version                    Show the OraLab version
  help                       Show this help

Log options:
  --no-follow  --tail LINES  --since VALUE  --timestamps

Native Oracle examples:
  oralab exec -- sqlplus / as sysdba
  oralab exec -- rman target /
  oralab exec -- lsnrctl status

OraLab intentionally provides no reset, destroy, or volume-deletion command.`)
}
