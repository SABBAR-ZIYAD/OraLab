package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func testApp(root string, engine dockerEngine) (app, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return app{
		stdin:  bytes.NewBuffer(nil),
		stdout: stdout,
		stderr: stderr,
		getwd:  func() (string, error) { return root, nil },
		getenv: func(string) string { return "" },
		newEngine: func() (dockerEngine, error) {
			return engine, nil
		},
	}, stdout, stderr
}

func projectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, environmentSample), []byte("ORACLE_IMAGE=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestExecPreservesNativeOracleArguments(t *testing.T) {
	root := projectFixture(t)
	engine := &fakeEngine{}
	a, _, stderr := testApp(root, engine)

	if code := a.run([]string{"exec", "--", "sqlplus", "/", "as", "sysdba"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	want := []string{"sqlplus", "/", "as", "sysdba"}
	if !reflect.DeepEqual(engine.execCommand, want) {
		t.Fatalf("command = %#v, want %#v", engine.execCommand, want)
	}
}

func TestUpCreatesContainerThroughEngineAPI(t *testing.T) {
	root := projectFixture(t)
	contents := "ORACLE_IMAGE=registry.example/oracle:19c\nORACLE_PWD=secret\nORACLE_CPUS=4\nORACLE_MEMORY=6g\nORACLE_SHM_SIZE=2g\n"
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := &fakeEngine{
		info:       engineInfo{CPUs: 8, MemoryBytes: 10 * 1024 * 1024 * 1024},
		inspectErr: errDockerObjectNotFound,
	}
	a, _, stderr := testApp(root, engine)

	if code := a.run([]string{"up", "--timeout", "900"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	if engine.ensureImage != "registry.example/oracle:19c" {
		t.Fatalf("image = %q", engine.ensureImage)
	}
	if engine.createSpec.Name != oracleContainerName || engine.createSpec.HostIP != "127.0.0.1" {
		t.Fatalf("container spec = %#v", engine.createSpec)
	}
	if engine.createSpec.VolumeName != oracleVolumeName || engine.createSpec.VolumePath != oracleDataPath {
		t.Fatalf("volume spec = %#v", engine.createSpec)
	}
	if engine.startCalls != 1 {
		t.Fatalf("start calls = %d", engine.startCalls)
	}
}

func TestSetupNeverOverwritesEnvironment(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("KEEP=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, _ := testApp(root, &fakeEngine{})
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

func TestProjectDoesNotRequireComposeFile(t *testing.T) {
	root := projectFixture(t)
	if !isProjectDirectory(root) {
		t.Fatal("project with .env.example was not detected")
	}
}

func TestRemoteEngineMutationIsRejected(t *testing.T) {
	root := projectFixture(t)
	engine := &fakeEngine{endpoint: "tcp://remote.example:2376"}
	a, _, _ := testApp(root, engine)
	if code := a.run([]string{"stop"}); code == 0 {
		t.Fatal("remote Docker mutation was accepted")
	}
	if engine.stopTimeout != 0 {
		t.Fatal("remote engine received a stop request")
	}
}

func TestParseLogOptions(t *testing.T) {
	options, ok := parseLogOptions([]string{"--no-follow", "--tail", "200", "--since", "10m", "--timestamps"})
	if !ok || options.Follow || options.Tail != "200" || options.Since != "10m" || !options.Timestamps {
		t.Fatalf("options = %#v, ok=%v", options, ok)
	}
}
