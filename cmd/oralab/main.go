package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	serviceName       = "oracle"
	defaultWait       = 1800
	defaultStop       = 600
	composeFileName   = "compose.yaml"
	environmentSample = ".env.example"
	environmentFile   = ".env"
)

var version = "dev"

type commandRunner interface {
	LookPath(file string) (string, error)
	Run(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

type systemRunner struct{}

func (systemRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (systemRunner) Run(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := exec.Command(name, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

type app struct {
	runner commandRunner
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	getwd  func() (string, error)
	getenv func(string) string
}

func main() {
	a := app{
		runner: systemRunner{},
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		getwd:  os.Getwd,
		getenv: os.Getenv,
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
	case "setup":
		return a.setup(root, commandArgs)
	case "ui":
		return a.ui(root, commandArgs)
	case "up":
		seconds, ok := parseTimeout(commandArgs, defaultWait)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab up [--timeout SECONDS]")
			return 2
		}
		return a.compose(root, "up", "-d", "--wait", "--wait-timeout", strconv.Itoa(seconds), serviceName)
	case "status":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab status")
			return 2
		}
		return a.compose(root, "ps", serviceName)
	case "logs":
		composeArgs := []string{"logs", "--follow"}
		composeArgs = append(composeArgs, commandArgs...)
		composeArgs = append(composeArgs, serviceName)
		return a.compose(root, composeArgs...)
	case "shell":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab shell")
			return 2
		}
		return a.compose(root, "exec", serviceName, "bash")
	case "exec":
		if len(commandArgs) > 0 && commandArgs[0] == "--" {
			commandArgs = commandArgs[1:]
		}
		if len(commandArgs) == 0 {
			fmt.Fprintln(a.stderr, "usage: oralab exec -- <native-command> [arguments...]")
			return 2
		}
		composeArgs := []string{"exec", serviceName}
		composeArgs = append(composeArgs, commandArgs...)
		return a.compose(root, composeArgs...)
	case "start":
		if len(commandArgs) != 0 {
			fmt.Fprintln(a.stderr, "usage: oralab start")
			return 2
		}
		return a.compose(root, "start", serviceName)
	case "stop":
		seconds, ok := parseTimeout(commandArgs, defaultStop)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab stop [--timeout SECONDS]")
			return 2
		}
		return a.compose(root, "stop", "--timeout", strconv.Itoa(seconds), serviceName)
	case "restart":
		seconds, ok := parseTimeout(commandArgs, defaultStop)
		if !ok {
			fmt.Fprintln(a.stderr, "usage: oralab restart [--timeout SECONDS]")
			return 2
		}
		return a.compose(root, "restart", "--timeout", strconv.Itoa(seconds), serviceName)
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
	return regularFile(filepath.Join(path, composeFileName)) || regularFile(filepath.Join(path, "PROJECT_PLAN.md"))
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (a app) compose(root string, args ...string) int {
	composePath := filepath.Join(root, composeFileName)
	if !regularFile(composePath) {
		fmt.Fprintf(a.stderr, "oralab: %s does not exist; the Oracle environment has not been implemented yet\n", composePath)
		return 1
	}
	fullArgs := []string{"compose", "--project-directory", root, "--file", composePath}
	fullArgs = append(fullArgs, args...)
	return a.execute("docker", fullArgs...)
}

func (a app) execute(name string, args ...string) int {
	err := a.runner.Run(name, args, a.stdin, a.stdout, a.stderr)
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	fmt.Fprintf(a.stderr, "oralab: cannot run %s: %v\n", name, err)
	return 1
}

func (a app) doctor(root string, args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(a.stderr, "usage: oralab doctor")
		return 2
	}
	fmt.Fprintln(a.stdout, "OraLab environment check")
	failed := false
	dockerAvailable := true

	if _, err := a.runner.LookPath("docker"); err != nil {
		fmt.Fprintln(a.stderr, "[FAIL] Docker CLI was not found in PATH")
		failed = true
		dockerAvailable = false
	} else {
		fmt.Fprintln(a.stdout, "[PASS] Docker CLI found")
		if a.execute("docker", "--version") != 0 {
			failed = true
		}
		if a.execute("docker", "compose", "version") != 0 {
			failed = true
		}
	}

	composePath := filepath.Join(root, composeFileName)
	if !regularFile(composePath) {
		fmt.Fprintf(a.stderr, "[FAIL] Missing %s\n", composePath)
		failed = true
	} else {
		fmt.Fprintf(a.stdout, "[PASS] Found %s\n", composePath)
		if !dockerAvailable {
			fmt.Fprintln(a.stderr, "[SKIP] Compose validation requires Docker")
		} else if a.compose(root, "config", "--quiet") != 0 {
			failed = true
		} else {
			fmt.Fprintln(a.stdout, "[PASS] Compose configuration is valid")
		}
	}

	if regularFile(filepath.Join(root, environmentFile)) {
		fmt.Fprintln(a.stdout, "[PASS] Local .env exists")
	} else if regularFile(filepath.Join(root, environmentSample)) {
		fmt.Fprintln(a.stderr, "[WARN] Local .env is missing; run: oralab setup")
	} else {
		fmt.Fprintln(a.stderr, "[FAIL] Both .env and .env.example are missing")
		failed = true
	}

	if failed {
		return 1
	}
	return 0
}

func (a app) setup(root string, args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(a.stderr, "usage: oralab setup")
		return 2
	}
	source := filepath.Join(root, environmentSample)
	destination := filepath.Join(root, environmentFile)
	if !regularFile(source) {
		fmt.Fprintf(a.stderr, "oralab: %s does not exist\n", source)
		return 1
	}
	input, err := os.Open(source)
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: open %s: %v\n", source, err)
		return 1
	}
	defer input.Close()

	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		fmt.Fprintf(a.stderr, "oralab: refusing to overwrite existing %s\n", destination)
		return 1
	}
	if err != nil {
		fmt.Fprintf(a.stderr, "oralab: create %s: %v\n", destination, err)
		return 1
	}

	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(destination)
		if copyErr != nil {
			fmt.Fprintf(a.stderr, "oralab: write %s: %v\n", destination, copyErr)
		} else {
			fmt.Fprintf(a.stderr, "oralab: close %s: %v\n", destination, closeErr)
		}
		return 1
	}
	fmt.Fprintf(a.stdout, "Created %s\nReview its values before running: oralab up\n", destination)
	return 0
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

func (a app) printHelp(writer io.Writer) {
	fmt.Fprintln(writer, `OraLab manages the Docker environment; Oracle commands remain unchanged.

Usage:
  oralab [--project-dir PATH] <command>

Commands:
  doctor                     Check Docker and project configuration
  setup                      Create .env safely from .env.example
  ui [--port PORT] [--no-open]
                              Open the local CPU/RAM resource manager
  up [--timeout SECONDS]     Start Oracle and wait for readiness
  status                     Show the Oracle service status
  logs [COMPOSE LOG OPTIONS] Follow Oracle container logs
  shell                      Enter the Oracle container shell
  exec -- COMMAND [ARGS...]  Run a native command unchanged in the container
  start                      Start the existing Oracle container
  stop [--timeout SECONDS]   Gracefully stop Oracle (default: 600 seconds)
  restart [--timeout SECONDS] Restart Oracle gracefully
  version                    Show the OraLab version
  help                       Show this help

Native Oracle examples:
  oralab exec -- sqlplus / as sysdba
  oralab exec -- rman target /
  oralab exec -- lsnrctl status

OraLab intentionally provides no reset, destroy, or volume-deletion command.`)
}
