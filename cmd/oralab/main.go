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
	root, err := a.resolveProjectDir(projectDir)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		return 1
	}

	switch command {
	case "doctor":
		return a.doctor(root, commandArgs)
	case "init", "setup":
		return a.init(root, commandArgs)
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
		return a.exec([]string{"bash"})
	case "exec":
		if len(commandArgs) > 0 && commandArgs[0] == "--" {
			commandArgs = commandArgs[1:]
		}
		if len(commandArgs) == 0 {
			fmt.Fprintln(a.stderr, "usage: oralab exec -- <native-command> [arguments...]")
			return 2
		}
		return a.exec(commandArgs)
	case "start":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab start")
			return 2
		}
		return a.start()
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
		return a.restart(seconds)
	default:
		fmt.Fprintf(a.stderr, "oralab: unknown command %q\n\n", command)
		a.printHelp(a.stderr)
		return 2
	}
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

func (a app) up(root string, seconds int) int {
	engine, ok := a.openEngine()
	if !ok {
		return 1
	}
	defer engine.Close()
	if !isLocalDockerEndpoint(engine.Endpoint()) {
		fmt.Fprintln(a.stderr, "oralab: refusing lifecycle changes through a remote Docker endpoint")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
	defer cancel()
	inspected, err := ensureOracleContainer(ctx, engine, root)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: prepare Oracle container: %v\n", err)
		return 1
	}
	if !inspected.Running {
		if err := engine.StartContainer(ctx, oracleContainerName); err != nil {
			fmt.Fprintf(a.stderr, "oralab: start Oracle container: %v\n", err)
			return 1
		}
	}
	if err := waitForReadiness(ctx, engine, oracleContainerName); err != nil {
		fmt.Fprintf(a.stderr, "oralab: Oracle did not become ready: %v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, "Oracle container is running and ready.")
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
	if err := engine.Logs(ctx, oracleContainerName, options, a.stdout, a.stderr); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(a.stderr, "oralab: read Oracle logs: %v\n", err)
		return 1
	}
	return 0
}

func (a app) exec(command []string) int {
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
	exitCode, err := engine.Exec(ctx, oracleContainerName, command, true, a.stdin, a.stdout, a.stderr)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: execute in Oracle container: %v\n", err)
		return 1
	}
	return exitCode
}

func (a app) start() int {
	engine, ok := a.openEngine()
	if !ok {
		return 1
	}
	defer engine.Close()
	if !isLocalDockerEndpoint(engine.Endpoint()) {
		fmt.Fprintln(a.stderr, "oralab: refusing lifecycle changes through a remote Docker endpoint")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(defaultWait)*time.Second)
	defer cancel()
	inspected, err := engine.InspectContainer(ctx, oracleContainerName)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: inspect Oracle container: %v\n", err)
		return 1
	}
	if !inspected.Managed {
		fmt.Fprintln(a.stderr, "oralab: the oralab-oracle container is not managed by OraLab")
		return 1
	}
	if !inspected.Running {
		if err := engine.StartContainer(ctx, oracleContainerName); err != nil {
			fmt.Fprintf(a.stderr, "oralab: start Oracle container: %v\n", err)
			return 1
		}
	}
	if err := waitForReadiness(ctx, engine, oracleContainerName); err != nil {
		fmt.Fprintf(a.stderr, "oralab: Oracle did not become ready: %v\n", err)
		return 1
	}
	return 0
}

func (a app) stop(seconds int) int {
	engine, ok := a.openEngine()
	if !ok {
		return 1
	}
	defer engine.Close()
	if !isLocalDockerEndpoint(engine.Endpoint()) {
		fmt.Fprintln(a.stderr, "oralab: refusing lifecycle changes through a remote Docker endpoint")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds+60)*time.Second)
	defer cancel()
	if err := engine.StopContainer(ctx, oracleContainerName, seconds); err != nil {
		fmt.Fprintf(a.stderr, "oralab: stop Oracle container: %v\n", err)
		return 1
	}
	return 0
}

func (a app) restart(seconds int) int {
	engine, ok := a.openEngine()
	if !ok {
		return 1
	}
	defer engine.Close()
	if !isLocalDockerEndpoint(engine.Endpoint()) {
		fmt.Fprintln(a.stderr, "oralab: refusing lifecycle changes through a remote Docker endpoint")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	if err := engine.RestartContainer(ctx, oracleContainerName, seconds); err != nil {
		fmt.Fprintf(a.stderr, "oralab: restart Oracle container: %v\n", err)
		return 1
	}
	if err := waitForReadiness(ctx, engine, oracleContainerName); err != nil {
		fmt.Fprintf(a.stderr, "oralab: Oracle did not become ready: %v\n", err)
		return 1
	}
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
	if err := createLabConfig(filepath.Join(root, environmentFile), password, options); err != nil {
		fmt.Fprintf(a.stderr, "oralab: create local configuration: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.stdout, "Created %s for Oracle Database 19c Enterprise.\n", filepath.Join(root, environmentFile))
	fmt.Fprintln(a.stdout, "Before the first pull, accept Oracle Container Registry terms and provide your registry auth token if requested.")
	fmt.Fprintln(a.stdout, "Next: oralab up")
	return 0
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
  doctor                     Check Docker Engine and project configuration
	init --cpus N --memory S --shm S [--password-stdin]
	                           Configure this lab and securely collect its database password
  ui [--port PORT] [--no-open]
                              Open the local CPU/RAM resource manager
  up [--timeout SECONDS]     Create/start Oracle and wait for readiness
  status                     Show Docker and Oracle container status
  logs [OPTIONS]             Follow Oracle logs through the Engine API
  shell                      Enter the Oracle container shell
  exec -- COMMAND [ARGS...]  Run a native command unchanged in the container
  start                      Start the existing Oracle container
  stop [--timeout SECONDS]   Gracefully stop Oracle (default: 600 seconds)
  restart [--timeout SECONDS] Restart Oracle gracefully
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
