package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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
		readPassword: func() (string, error) { return "TestPassword1", nil },
	}, stdout, stderr
}

func projectFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, environmentSample), []byte("ORACLE_PWD=\nORACLE_CPUS=\nORACLE_MEMORY=\nORACLE_SHM_SIZE=\n"), 0o600); err != nil {
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
	if engine.execTTY {
		t.Fatal("non-interactive exec unexpectedly allocated a TTY")
	}
}

func TestShellAllocatesTTY(t *testing.T) {
	root := projectFixture(t)
	engine := &fakeEngine{}
	a, _, stderr := testApp(root, engine)

	if code := a.run([]string{"shell"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	if !engine.execTTY {
		t.Fatal("shell did not allocate a TTY")
	}
}

func TestUpCreatesContainerThroughEngineAPI(t *testing.T) {
	root := projectFixture(t)
	contents := "ORACLE_PWD=secret\nORACLE_CPUS=4\nORACLE_MEMORY=6g\nORACLE_SHM_SIZE=2g\n"
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
	if engine.ensureImage != defaultOracleImage {
		t.Fatalf("image = %q", engine.ensureImage)
	}
	if engine.createSpec.Name != oracleContainerName || engine.createSpec.HostIP != "127.0.0.1" || engine.createSpec.Image != defaultOracleImage {
		t.Fatalf("container spec = %#v", engine.createSpec)
	}
	if engine.createSpec.VolumeName != oracleVolumeName || engine.createSpec.VolumePath != oracleDataPath {
		t.Fatalf("volume spec = %#v", engine.createSpec)
	}
	for _, expected := range []string{"ORACLE_SID=" + oracleSID, "ORACLE_PDB=" + oraclePDB} {
		if !slices.Contains(engine.createSpec.Environment, expected) {
			t.Errorf("container environment is missing %q: %#v", expected, engine.createSpec.Environment)
		}
	}
	if engine.startCalls != 1 {
		t.Fatalf("start calls = %d", engine.startCalls)
	}
}

func TestInitWritesOnlyUserConfigurationAndNeverOverwritesEnvironment(t *testing.T) {
	root := projectFixture(t)
	a, _, stderr := testApp(root, &fakeEngine{})
	if code := a.run([]string{"init", "--cpus", "4", "--memory", "6g", "--shm", "1g"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	contents, err := os.ReadFile(filepath.Join(root, environmentFile))
	if err != nil {
		t.Fatal(err)
	}
	want := "# Created by OraLab. The Oracle 19c Enterprise image is managed by OraLab.\nORACLE_PWD=TestPassword1\nORACLE_CPUS=4\nORACLE_MEMORY=6g\nORACLE_SHM_SIZE=1g\n"
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
	a, _, stderr := testApp(root, &fakeEngine{})
	a.stdin = strings.NewReader("\ufeffPipedPassword1\n")
	if code := a.run([]string{"init", "--cpus", "2", "--memory", "4g", "--shm", "1g", "--password-stdin"}); code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr.String())
	}
	contents, err := os.ReadFile(filepath.Join(root, environmentFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "ORACLE_PWD=PipedPassword1\n") {
		t.Fatalf(".env does not contain piped password: %q", contents)
	}
}

func TestInitRejectsInvalidResourceArguments(t *testing.T) {
	root := projectFixture(t)
	a, _, _ := testApp(root, &fakeEngine{})
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
	a, _, _ := testApp(root, &fakeEngine{})
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
	a, _, stderr := testApp(root, &fakeEngine{})

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
	if !isProjectDirectory(root) {
		t.Fatal("standalone setup did not create an OraLab project directory")
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
