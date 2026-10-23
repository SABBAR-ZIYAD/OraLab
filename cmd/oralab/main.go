package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultWait       = 1800
	defaultStop       = 600
	environmentSample = ".env.example"
	environmentFile   = ".env"
)

var version = "dev"

type app struct {
	stdin        io.Reader
	stdout       io.Writer
	stderr       io.Writer
	getwd        func() (string, error)
	getenv       func(string) string
	runner       composeRunner
	readPassword func() (string, error)
	interactive  func() bool
}

func main() {
	a := app{
		stdin:        os.Stdin,
		stdout:       os.Stdout,
		stderr:       os.Stderr,
		getwd:        os.Getwd,
		getenv:       os.Getenv,
		readPassword: readPasswordFromTerminal,
		interactive: func() bool {
			return terminalAttached(os.Stdin, os.Stdout)
		},
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
		return a.v2Doctor(root, commandArgs)
	case "ui":
		return a.ui(root, commandArgs)
	case "serve":
		return a.ui(root, commandArgs)
	case "up":
		seconds, ok := parseTimeout(commandArgs, defaultWait)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab up [--timeout SECONDS]")
			return 2
		}
		return a.v2Up(root, seconds)
	case "status":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab status")
			return 2
		}
		return a.v2Status(root)
	case "logs":
		options, ok := parseLogOptions(commandArgs)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab logs [--follow] [--tail LINES] [--since VALUE] [--timestamps]")
			return 2
		}
		return a.v2Logs(root, options)
	case "shell":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab shell")
			return 2
		}
		return a.v2Exec(root, []string{"sqlplus", "/", "as", "sysdba"}, a.useTTY())
	case "sqlplus", "rman", "lsnrctl", "expdp", "impdp":
		return a.v2Exec(root, append([]string{command}, commandArgs...), a.useTTY())
	case "exec":
		if len(commandArgs) > 0 && commandArgs[0] == "--" {
			commandArgs = commandArgs[1:]
		}
		if len(commandArgs) == 0 {
			fmt.Fprintln(a.stderr, "usage: oralab exec -- <native-command> [arguments...]")
			return 2
		}
		return a.v2Exec(root, commandArgs, a.useTTY())
	case "start":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab start")
			return 2
		}
		fmt.Fprintln(a.stderr, "oralab: \"start\" is deprecated; use \"up\"")
		return a.v2Up(root, defaultWait)
	case "stop":
		seconds, ok := parseTimeout(commandArgs, defaultStop)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab stop [--timeout SECONDS]")
			return 2
		}
		return a.v2Stop(root, seconds)
	case "restart":
		seconds, ok := parseTimeout(commandArgs, defaultStop)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab restart [--timeout SECONDS]")
			return 2
		}
		fmt.Fprintln(a.stderr, "oralab: \"restart\" is deprecated; use \"stop\" followed by \"up\"")
		return a.v2Restart(root, seconds)
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

type initOptions struct {
	CPUs          int
	Memory        string
	MemoryBytes   int64
	Shm           string
	ShmBytes      int64
	PasswordStdin bool
}

func (a app) init(root string, args []string) int {
	hasExplicitOptions := len(args) != 0
	options, err := parseInitOptions(args)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: %v\n", err)
		fmt.Fprintln(a.stderr, "usage: oralab init [--cpus COUNT] [--memory SIZE] [--shm SIZE] [--password-stdin]")
		return 2
	}
	existingEnvironment := regularFile(filepath.Join(root, environmentFile))
	if existingEnvironment && regularFile(filepath.Join(root, composeFileName)) {
		fmt.Fprintln(a.stderr, "oralab: create local configuration: refusing to overwrite existing .env or compose.yaml")
		return 1
	}
	if existingEnvironment && hasExplicitOptions {
		fmt.Fprintln(a.stderr, "oralab: migration preserves the existing .env; run plain 'oralab init' without resource or password options")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), stateTimeout)
	defer cancel()
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
		fmt.Fprintln(a.stderr, "oralab: legacy oralab-oracle container detected. OraLab V2 will not create a second container against its persistent data. Read docs/migration.md; no data was changed.")
		return 1
	}
	if existingEnvironment {
		config, err := readLabConfig(filepath.Join(root, environmentFile))
		if err != nil || config.Password == "" {
			fmt.Fprintf(a.stderr, "oralab: existing .env is not a usable V1 configuration: %v\n", err)
			return 1
		}
		exists, managed, err := inspectExternalVolume(ctx, a, root)
		if err != nil {
			fmt.Fprintf(a.stderr, "oralab: inspect migration volume: %v\n", err)
			return 1
		}
		if !exists || !managed {
			fmt.Fprintln(a.stderr, "oralab: migration requires the existing OraLab-managed oralab-oradata volume; no files were changed")
			return 1
		}
		if err := ensureEnvironmentSample(root); err != nil {
			fmt.Fprintf(a.stderr, "oralab: create project marker: %v\n", err)
			return 1
		}
		if err := ensureComposeFile(root); err != nil {
			fmt.Fprintf(a.stderr, "oralab: create compose.yaml: %v\n", err)
			return 1
		}
		image, err := a.configuredImage(ctx, root)
		if err != nil {
			_ = os.Remove(filepath.Join(root, composeFileName))
			fmt.Fprintf(a.stderr, "oralab: validate migrated compose.yaml: %v\n", err)
			return 1
		}
		if err := writeLabState(root, image); err != nil {
			_ = os.Remove(filepath.Join(root, composeFileName))
			fmt.Fprintf(a.stderr, "oralab: record migration image safety state: %v\n", err)
			return 1
		}
		fmt.Fprintln(a.stdout, "Prepared OraLab V2 Compose files. Existing .env and oralab-oradata data were preserved unchanged.")
		fmt.Fprintln(a.stdout, "Next: oralab doctor, then oralab up")
		return 0
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
	if err := ensureComposeFile(root); err != nil {
		_ = os.Remove(configPath)
		fmt.Fprintf(a.stderr, "oralab: create compose.yaml: %v\n", err)
		return 1
	}
	if err := createExternalVolume(ctx, a, root); err != nil {
		_ = os.Remove(configPath)
		fmt.Fprintf(a.stderr, "oralab: prepare persistent storage: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.stdout, "Created %s for Oracle Database 19c Enterprise.\n", configPath)
	fmt.Fprintf(a.stdout, `Before the first pull, complete Oracle's one-time registry setup:
  %s
Sign in, open Database > enterprise, and accept the terms. Then use Docker's own login flow for registry credentials.
Once downloaded, Docker Compose reuses the local image without signing in on every use.
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
	if len(password) < 8 {
		return "", errors.New("password must contain at least 8 characters")
	}
	var upper, lower, digit bool
	for _, character := range password {
		switch {
		case character >= 'A' && character <= 'Z':
			upper = true
		case character >= 'a' && character <= 'z':
			lower = true
		case character >= '0' && character <= '9':
			digit = true
		}
	}
	if !upper || !lower || !digit {
		return "", errors.New("password must include an uppercase letter, a lowercase letter, and a digit")
	}
	return password, nil
}

func parseInitOptions(args []string) (initOptions, error) {
	options := initOptions{CPUs: 4, Memory: "6g", Shm: "1g"}
	var err error
	options.MemoryBytes, err = parseByteSize(options.Memory)
	if err != nil {
		return initOptions{}, err
	}
	options.ShmBytes, err = parseByteSize(options.Shm)
	if err != nil {
		return initOptions{}, err
	}
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
	options := logOptions{Tail: defaultLogTail}
	for len(args) > 0 {
		switch args[0] {
		case "--follow":
			options.Follow = true
			args = args[1:]
		case "--no-follow": // V1 compatibility; bounded output is already the default.
			args = args[1:]
		case "--timestamps":
			options.Timestamps = true
			args = args[1:]
		case "--tail":
			if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
				return logOptions{}, false
			}
			lines, err := strconv.Atoi(args[1])
			if err != nil || lines <= 0 || lines > 100000 {
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
	fmt.Fprintln(writer, `OraLab uses Docker Compose for lab lifecycle; Oracle commands remain unchanged.

Usage:
  oralab [--project-dir PATH] <command>

Commands:
  init [--cpus N] [--memory S] [--shm S] [--password-stdin]
                              Configure this lab and securely collect its database password
  up [--timeout SECONDS]     Create/start Oracle and wait for readiness
  status                     Show Docker and Oracle container status
  stop [--timeout SECONDS]   Gracefully stop Oracle (default: 600 seconds)
  logs [OPTIONS]             Read or follow Oracle Compose logs
  shell                      Open SQL*Plus as SYSDBA inside the Oracle service
  sqlplus [ARGS...]          Run SQL*Plus inside the Oracle service
  rman [ARGS...]             Run RMAN inside the Oracle service
  lsnrctl [ARGS...]          Run Listener Control inside the Oracle service
  expdp [ARGS...]            Run Data Pump Export inside the Oracle service
  impdp [ARGS...]            Run Data Pump Import inside the Oracle service
  exec -- COMMAND [ARGS...]  Run a native command unchanged in the container
  ui [--port PORT] [--no-open]
                              Open the local Compose status manager
  doctor                     Check Docker Compose and project configuration
  version                    Show the OraLab version
  help                       Show this help

Log options:
  --follow  --tail LINES  --since VALUE  --timestamps

Native Oracle examples:
  oralab sqlplus / as sysdba
  oralab rman target /
  oralab lsnrctl status

OraLab intentionally provides no reset, destroy, or volume-deletion command.`)
}

func (a app) useTTY() bool {
	return a.interactive != nil && a.interactive()
}
