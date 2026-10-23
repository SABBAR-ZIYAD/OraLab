package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type fakeComposeRunner struct {
	calls [][]string
	run   func([]string, io.Writer, io.Writer) error
}

func (f *fakeComposeRunner) Run(_ context.Context, _ string, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.run != nil {
		return f.run(args, stdout, stderr)
	}
	if len(args) >= 3 && args[0] == "container" && args[1] == "inspect" {
		_, _ = io.WriteString(stderr, "Error: No such object\n")
		return errors.New("not found")
	}
	if len(args) >= 3 && args[0] == "volume" && args[1] == "inspect" {
		_, _ = io.WriteString(stderr, "Error: no such volume\n")
		return errors.New("not found")
	}
	if containsArgument(args, "config") {
		_, _ = io.WriteString(stdout, `{"services":{"oracle":{"image":"container-registry.oracle.com/database/enterprise:19.3.0.0"}}}`)
	}
	if containsArgument(args, "ps") {
		_, _ = io.WriteString(stdout, `{"ID":"1234567890abcdef","Name":"oralab-oracle-1","Service":"oracle","State":"running","Health":"healthy","ExitCode":0}`+"\n")
	}
	return nil
}

func containsArgument(args []string, expected string) bool {
	for _, value := range args {
		if value == expected {
			return true
		}
	}
	return false
}

func testApp(root string) (app, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return app{
		stdin:        bytes.NewBuffer(nil),
		stdout:       stdout,
		stderr:       stderr,
		getwd:        func() (string, error) { return root, nil },
		getenv:       func(string) string { return "" },
		runner:       &fakeComposeRunner{},
		interactive:  func() bool { return false },
		readPassword: func() (string, error) { return "TestPassword1", nil },
	}, stdout, stderr
}

func projectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, environmentSample), []byte("ORACLE_PWD=\nORACLE_CPUS=\nORACLE_MEMORY=\nORACLE_SHM_SIZE=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, composeFileName), embeddedComposeFile, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestExecPreservesNativeOracleArguments(t *testing.T) {
	root := projectFixture(t)
	a, _, stderr := testApp(root)

	if code := a.run([]string{"exec", "--", "sqlplus", "/", "as", "sysdba"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	runner := a.runner.(*fakeComposeRunner)
	execCall := findComposeCall(runner.calls, "exec")
	if execCall == nil || !containsArgument(execCall, "--no-tty") || !reflect.DeepEqual(execCall[len(execCall)-5:], []string{"oracle", "sqlplus", "/", "as", "sysdba"}) {
		t.Fatalf("compose exec calls = %#v", runner.calls)
	}
}

func TestShellDisablesTTYWhenStreamsArePiped(t *testing.T) {
	root := projectFixture(t)
	a, _, stderr := testApp(root)

	if code := a.run([]string{"shell"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	runner := a.runner.(*fakeComposeRunner)
	execCall := findComposeCall(runner.calls, "exec")
	if execCall == nil || !containsArgument(execCall, "--no-tty") || !slices.Contains(execCall, "sqlplus") {
		t.Fatalf("shell did not invoke piped SQL*Plus correctly: %#v", runner.calls)
	}
}

func TestNativeUtilityUsesTTYForInteractiveTerminal(t *testing.T) {
	root := projectFixture(t)
	a, _, stderr := testApp(root)
	a.interactive = func() bool { return true }
	if code := a.run([]string{"sqlplus", "/", "as", "sysdba"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	runner := a.runner.(*fakeComposeRunner)
	execCall := findComposeCall(runner.calls, "exec")
	if execCall == nil || containsArgument(execCall, "--no-tty") {
		t.Fatalf("interactive call unexpectedly disabled TTY: %#v", runner.calls)
	}
}

func TestUpUsesComposeLifecycle(t *testing.T) {
	root := projectFixture(t)
	contents := "ORACLE_PWD=secret\nORACLE_CPUS=4\nORACLE_MEMORY=6g\nORACLE_SHM_SIZE=2g\n"
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	a, stdout, stderr := testApp(root)

	if code := a.run([]string{"up", "--timeout", "900"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	runner := a.runner.(*fakeComposeRunner)
	if !containsComposeCall(runner.calls, "up") {
		t.Fatalf("compose calls = %#v", runner.calls)
	}
	for _, expected := range []string{
		"Starting Oracle through Docker Compose",
		"Waiting for Oracle readiness",
		"Oracle Database is running and ready",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("progress output is missing %q:\n%s", expected, stdout.String())
		}
	}
}

func containsComposeCall(calls [][]string, command string) bool {
	return findComposeCall(calls, command) != nil
}

func findComposeCall(calls [][]string, command string) []string {
	for _, call := range calls {
		if containsArgument(call, command) && containsArgument(call, "compose") {
			return call
		}
	}
	return nil
}

func TestLifecycleCommandsReportSafeOutcomes(t *testing.T) {
	root := projectFixture(t)
	contents := "ORACLE_PWD=secret\nORACLE_CPUS=4\nORACLE_MEMORY=6g\nORACLE_SHM_SIZE=1g\n"
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	a, stdout, stderr := testApp(root)

	if code := a.run([]string{"up"}); code != 0 {
		t.Fatalf("up exit code %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Oracle Database is running and ready") {
		t.Fatalf("up progress output:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := a.run([]string{"stop"}); code != 0 {
		t.Fatalf("stop exit code %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "volume was preserved") {
		t.Fatalf("stop progress output:\n%s", stdout.String())
	}

	runner := a.runner.(*fakeComposeRunner)
	if !containsComposeCall(runner.calls, "stop") {
		t.Fatalf("Compose stop was not invoked: %#v", runner.calls)
	}
}

func TestDeprecatedAliasesRemainAvailable(t *testing.T) {
	root := projectFixture(t)
	contents := "ORACLE_PWD=secret\nORACLE_CPUS=4\nORACLE_MEMORY=6g\nORACLE_SHM_SIZE=1g\n"
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, stderr := testApp(root)

	if code := a.run([]string{"start"}); code != 0 {
		t.Fatalf("start alias exit code %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `"start" is deprecated; use "up"`) {
		t.Fatalf("start deprecation warning = %q", stderr.String())
	}

	stderr.Reset()
	if code := a.run([]string{"restart"}); code != 0 {
		t.Fatalf("restart alias exit code %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `"restart" is deprecated; use "stop" followed by "up"`) {
		t.Fatalf("restart deprecation warning = %q", stderr.String())
	}
}

func TestInitWritesOnlyUserConfigurationAndNeverOverwritesEnvironment(t *testing.T) {
	root := projectFixture(t)
	a, _, stderr := testApp(root)
	if code := a.run([]string{"init", "--cpus", "4", "--memory", "6g", "--shm", "1g"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	contents, err := os.ReadFile(filepath.Join(root, environmentFile))
	if err != nil {
		t.Fatal(err)
	}
	want := "# Created by OraLab. Change the Oracle image only in compose.yaml.\nORACLE_PWD='TestPassword1'\nORACLE_SID=ORCLCDB\nORACLE_PDB=ORCLPDB1\nORACLE_CPUS=4\nORACLE_MEMORY=6g\nORACLE_SHM_SIZE=1g\nORALAB_STOP_GRACE_PERIOD=10m\n"
	if string(contents) != want {
		t.Fatalf(".env = %q, want %q", contents, want)
	}
	if strings.Contains(string(contents), "ORACLE_IMAGE") {
		t.Fatal(".env exposes the managed image")
	}
	if code := a.run([]string{"init", "--cpus", "4", "--memory", "6g", "--shm", "1g"}); code == 0 {
		t.Fatal("init unexpectedly overwrote .env")
	}
	if !strings.Contains(stderr.String(), "refusing to overwrite") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestInitReadsPasswordFromStdin(t *testing.T) {
	root := projectFixture(t)
	a, _, stderr := testApp(root)
	a.stdin = strings.NewReader("\ufeffPipedPassword1\n")
	if code := a.run([]string{"init", "--cpus", "2", "--memory", "4g", "--shm", "1g", "--password-stdin"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	contents, err := os.ReadFile(filepath.Join(root, environmentFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "ORACLE_PWD='PipedPassword1'\n") {
		t.Fatalf(".env does not contain piped password: %q", contents)
	}
}

func TestInitRejectsInvalidResourceArguments(t *testing.T) {
	root := projectFixture(t)
	a, _, _ := testApp(root)
	if code := a.run([]string{"init", "--cpus", "4", "--memory", "2g", "--shm", "3g"}); code == 0 {
		t.Fatal("init accepted shared memory larger than memory")
	}
	if _, err := os.Stat(filepath.Join(root, environmentFile)); !os.IsNotExist(err) {
		t.Fatalf(".env was created after invalid init: %v", err)
	}
}

func TestInitNeverOverwritesExistingEnvironment(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("KEEP=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, _ := testApp(root)
	if code := a.run([]string{"init", "--cpus", "4", "--memory", "6g", "--shm", "1g"}); code == 0 {
		t.Fatal("init unexpectedly overwrote .env")
	}
	contents, err := os.ReadFile(filepath.Join(root, environmentFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "KEEP=yes\n" {
		t.Fatalf(".env changed to %q", contents)
	}
}

func TestInitInitializesEmptyDirectoryFromEmbeddedSample(t *testing.T) {
	root := t.TempDir()
	a, _, stderr := testApp(root)

	if code := a.run([]string{"init", "--cpus", "4", "--memory", "6g", "--shm", "1g"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	contents, err := os.ReadFile(filepath.Join(root, environmentSample))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(contents, embeddedEnvironmentSample) {
		t.Fatal(".env.example does not contain the embedded environment sample")
	}
	if _, err := os.Stat(filepath.Join(root, environmentFile)); err != nil {
		t.Fatalf("standalone init did not create .env: %v", err)
	}
	if !regularFile(filepath.Join(root, composeFileName)) {
		t.Fatal("standalone init did not create compose.yaml")
	}
	if !isProjectDirectory(root) {
		t.Fatal("standalone setup did not create an OraLab project directory")
	}
}

func TestSetupAliasInitializesProjectWithDeprecationWarning(t *testing.T) {
	root := t.TempDir()
	a, _, stderr := testApp(root)

	if code := a.run([]string{"setup", "--cpus", "4", "--memory", "6g", "--shm", "1g"}); code != 0 {
		t.Fatalf("setup alias exit code %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `"setup" is deprecated; use "init"`) {
		t.Fatalf("setup deprecation warning = %q", stderr.String())
	}
	if !isProjectDirectory(root) {
		t.Fatal("setup alias did not create an OraLab project directory")
	}
}

func TestEmbeddedEnvironmentSampleMatchesRepository(t *testing.T) {
	repositorySample, err := os.ReadFile(filepath.Join("..", "..", environmentSample))
	if err != nil {
		t.Fatal(err)
	}
	normalizeLines := func(contents []byte) []byte {
		return bytes.ReplaceAll(contents, []byte("\r\n"), []byte("\n"))
	}
	if !bytes.Equal(normalizeLines(repositorySample), normalizeLines(embeddedEnvironmentSample)) {
		t.Fatal("cmd/oralab/env.example must match the repository .env.example")
	}
}

func TestProjectDoesNotRequireComposeFile(t *testing.T) {
	root := projectFixture(t)
	if !isProjectDirectory(root) {
		t.Fatal("project with .env.example was not detected")
	}
}

func TestParseLogOptions(t *testing.T) {
	options, ok := parseLogOptions([]string{"--follow", "--tail", "50", "--since", "10m", "--timestamps"})
	if !ok || !options.Follow || options.Tail != "50" || options.Since != "10m" || !options.Timestamps {
		t.Fatalf("options = %#v, ok=%v", options, ok)
	}
	defaults, ok := parseLogOptions(nil)
	if !ok || defaults.Follow || defaults.Tail != defaultLogTail {
		t.Fatalf("defaults = %#v, ok=%v", defaults, ok)
	}
}
