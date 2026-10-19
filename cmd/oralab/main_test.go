package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type recordedCommand struct {
	name string
	args []string
}

type fakeRunner struct {
	commands []recordedCommand
	lookErr  error
	runErr   error
}

func (f *fakeRunner) LookPath(string) (string, error) {
	return "docker", f.lookErr
}

func (f *fakeRunner) Run(name string, args []string, _ io.Reader, _, _ io.Writer) error {
	copyArgs := append([]string(nil), args...)
	f.commands = append(f.commands, recordedCommand{name: name, args: copyArgs})
	return f.runErr
}

func testApp(root string, runner commandRunner) (app, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return app{
		runner: runner,
		stdin:  bytes.NewBuffer(nil),
		stdout: stdout,
		stderr: stderr,
		getwd:  func() (string, error) { return root, nil },
		getenv: func(string) string { return "" },
	}, stdout, stderr
}

func projectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, composeFileName), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestExecPreservesNativeOracleArguments(t *testing.T) {
	root := projectFixture(t)
	runner := &fakeRunner{}
	a, _, stderr := testApp(root, runner)

	if code := a.run([]string{"exec", "--", "sqlplus", "/", "as", "sysdba"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	if len(runner.commands) != 1 {
		t.Fatalf("got %d commands", len(runner.commands))
	}
	wantSuffix := []string{"exec", serviceName, "sqlplus", "/", "as", "sysdba"}
	got := runner.commands[0].args
	if !reflect.DeepEqual(got[len(got)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("command suffix = %#v, want %#v", got, wantSuffix)
	}
}

func TestUpUsesReadinessWait(t *testing.T) {
	root := projectFixture(t)
	runner := &fakeRunner{}
	a, _, stderr := testApp(root, runner)

	if code := a.run([]string{"up", "--timeout", "900"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	wantSuffix := []string{"up", "-d", "--wait", "--wait-timeout", "900", serviceName}
	got := runner.commands[0].args
	if !reflect.DeepEqual(got[len(got)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("command suffix = %#v, want %#v", got, wantSuffix)
	}
}

func TestSetupNeverOverwritesEnvironment(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentSample), []byte("EXAMPLE=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("KEEP=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, _ := testApp(root, &fakeRunner{})

	if code := a.run([]string{"setup"}); code == 0 {
		t.Fatal("setup unexpectedly overwrote .env")
	}
	contents, err := os.ReadFile(filepath.Join(root, environmentFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "KEEP=yes\n" {
		t.Fatalf(".env changed to %q", contents)
	}
}

func TestMissingComposeFailsWithoutCallingDocker(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "PROJECT_PLAN.md"), []byte("# plan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	a, _, _ := testApp(root, runner)

	if code := a.run([]string{"status"}); code == 0 {
		t.Fatal("status succeeded without compose.yaml")
	}
	if len(runner.commands) != 0 {
		t.Fatalf("Docker was called: %#v", runner.commands)
	}
}

func TestRunnerFailureProducesFailureExitCode(t *testing.T) {
	root := projectFixture(t)
	runner := &fakeRunner{runErr: errors.New("unavailable")}
	a, _, _ := testApp(root, runner)
	if code := a.run([]string{"status"}); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}
