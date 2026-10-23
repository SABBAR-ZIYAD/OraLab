package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedComposeMatchesRepositoryAndSafetyInvariants(t *testing.T) {
	repository, err := os.ReadFile(filepath.Join("..", "..", composeFileName))
	if err != nil {
		t.Fatal(err)
	}
	normalize := func(value []byte) []byte { return bytes.ReplaceAll(value, []byte("\r\n"), []byte("\n")) }
	if !bytes.Equal(normalize(repository), normalize(embeddedComposeFile)) {
		t.Fatal("embedded compose.yaml differs from the repository manifest")
	}
	manifest := string(normalize(repository))
	for _, required := range []string{
		"services:\n  oracle:",
		"image: container-registry.oracle.com/database/enterprise:19.3.0.0",
		`"127.0.0.1:1521:1521"`,
		`"127.0.0.1:5500:5500"`,
		"oralab-oradata:/opt/oracle/oradata",
		"external: true",
	} {
		if !strings.Contains(manifest, required) {
			t.Errorf("compose.yaml is missing %q", required)
		}
	}
	for _, forbidden := range []string{"docker.sock", "privileged:", "network_mode: host", "0.0.0.0:"} {
		if strings.Contains(manifest, forbidden) {
			t.Errorf("compose.yaml contains unsafe setting %q", forbidden)
		}
	}
}

func TestComposeArgumentsKeepWindowsPathsAndNativeArgumentsDistinct(t *testing.T) {
	root := `Z:\Projects\Ora test\my lab`
	arguments := composeArguments(root, "exec", "--no-tty", composeService, "sqlplus", `@C:\scripts\check file.sql`)
	if !containsArgument(arguments, filepath.Join(root, environmentFile)) || !containsArgument(arguments, filepath.Join(root, composeFileName)) {
		t.Fatalf("project paths were not preserved as distinct arguments: %#v", arguments)
	}
	if arguments[len(arguments)-1] != `@C:\scripts\check file.sql` {
		t.Fatalf("native argument changed: %#v", arguments)
	}
}

func TestEnsureComposeFileNeverOverwritesUserModification(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, composeFileName)
	custom := []byte("services:\n  oracle:\n    image: example.invalid/custom:1\n")
	if err := os.WriteFile(path, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureComposeFile(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, custom) {
		t.Fatalf("user compose.yaml was changed: %q", got)
	}
}

func TestImageChangeIsRejectedAfterSuccessfulImageWasRecorded(t *testing.T) {
	root := t.TempDir()
	if err := writeLabState(root, "registry.example/oracle:old"); err != nil {
		t.Fatal(err)
	}
	if err := ensureImageIsSafe(root, "registry.example/oracle:new"); err == nil || !strings.Contains(err.Error(), "not treat an image change") {
		t.Fatalf("image mismatch error = %v", err)
	}
	if err := ensureImageIsSafe(root, "registry.example/oracle:old"); err != nil {
		t.Fatalf("same image was rejected: %v", err)
	}
}

func TestUpRefusesLegacyContainerBeforeComposeStart(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("ORACLE_PWD=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, stderr := testApp(root)
	runner := a.runner.(*fakeComposeRunner)
	runner.run = func(args []string, stdout, stderr io.Writer) error {
		if len(args) >= 2 && args[0] == "container" && args[1] == "inspect" {
			_, _ = io.WriteString(stdout, "{}")
		}
		return nil
	}
	if code := a.run([]string{"up"}); code == 0 {
		t.Fatal("up accepted a legacy container")
	}
	if !strings.Contains(stderr.String(), "legacy oralab-oracle") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if containsComposeCall(runner.calls, "up") {
		t.Fatalf("Compose up was called after legacy detection: %#v", runner.calls)
	}
}

func TestComposeCheckRejectsRemoteEndpoint(t *testing.T) {
	a, _, _ := testApp(t.TempDir())
	runner := a.runner.(*fakeComposeRunner)
	runner.run = func(args []string, stdout, _ io.Writer) error {
		if len(args) >= 2 && args[0] == "context" && args[1] == "inspect" {
			_, _ = io.WriteString(stdout, `"tcp://remote.example:2376"`)
		}
		return nil
	}
	err := a.checkCompose(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "remote") {
		t.Fatalf("remote endpoint error = %v", err)
	}
}

func TestUpReportsComposePortCollisionWithoutChangingData(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("ORACLE_PWD=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, stderr := testApp(root)
	runner := a.runner.(*fakeComposeRunner)
	runner.run = func(args []string, stdout, errorOutput io.Writer) error {
		if len(args) >= 2 && args[0] == "container" && args[1] == "inspect" {
			_, _ = io.WriteString(errorOutput, "No such object")
			return errors.New("not found")
		}
		if containsArgument(args, "config") {
			_, _ = io.WriteString(stdout, `{"services":{"oracle":{"image":"example.invalid/oracle:compatible"}}}`)
		}
		if containsArgument(args, "up") {
			_, _ = io.WriteString(errorOutput, "Bind for 127.0.0.1:1521 failed: port is already allocated")
			return errors.New("compose up failed")
		}
		return nil
	}
	if code := a.run([]string{"up"}); code == 0 {
		t.Fatal("up succeeded despite a port collision")
	}
	if !strings.Contains(stderr.String(), "port is already allocated") && !strings.Contains(stderr.String(), "Compose") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, stateFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("image state was recorded after failed start: %v", err)
	}
}

func TestComposeManagerReadsContainerMetricsThroughFixedDockerStats(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("ORACLE_PWD='Secret123'\nORACLE_CPUS=4\nORACLE_MEMORY=6g\nORACLE_SHM_SIZE=1g\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, _ := testApp(root)
	runner := a.runner.(*fakeComposeRunner)
	runner.run = func(args []string, stdout, stderr io.Writer) error {
		switch {
		case len(args) >= 2 && args[0] == "container" && args[1] == "inspect":
			_, _ = io.WriteString(stderr, "No such object")
			return errors.New("not found")
		case containsArgument(args, "config"):
			_, _ = io.WriteString(stdout, `{"services":{"oracle":{"image":"example.invalid/oracle:compatible"}}}`)
		case containsArgument(args, "ps"):
			_, _ = io.WriteString(stdout, `{"ID":"1234567890abcdef","Service":"oracle","State":"running","Health":"healthy"}`+"\n")
		case len(args) > 0 && args[0] == "stats":
			_, _ = io.WriteString(stdout, `{"CPUPerc":"2.50%","MemUsage":"1.5GiB / 6GiB"}`)
		}
		return nil
	}
	state := newComposeManager(a, root).state(context.Background())
	if !state.Resources.StatsAvailable || state.Resources.CPUPercent != 2.5 || state.Resources.MemoryUsageBytes != 1610612736 {
		t.Fatalf("resource state = %#v", state.Resources)
	}
	if state.Oracle.Image != "example.invalid/oracle:compatible" {
		t.Fatalf("manager image = %q", state.Oracle.Image)
	}
	statsCallFound := false
	for _, call := range runner.calls {
		if len(call) > 0 && call[0] == "stats" && containsArgument(call, "--no-stream") {
			statsCallFound = true
		}
	}
	if !statsCallFound {
		t.Fatalf("bounded stats call missing: %#v", runner.calls)
	}
}

type fakeProcessExitError struct{ code int }

func (e fakeProcessExitError) Error() string { return "process exited" }
func (e fakeProcessExitError) ExitCode() int { return e.code }

func TestNativeUtilityExitCodeIsPreserved(t *testing.T) {
	root := projectFixture(t)
	a, _, _ := testApp(root)
	runner := a.runner.(*fakeComposeRunner)
	runner.run = func(args []string, _, errorOutput io.Writer) error {
		if len(args) >= 2 && args[0] == "container" && args[1] == "inspect" {
			_, _ = io.WriteString(errorOutput, "No such object")
			return errors.New("not found")
		}
		if containsArgument(args, "exec") {
			return fakeProcessExitError{code: 37}
		}
		return nil
	}
	if code := a.run([]string{"exec", "false"}); code != 37 {
		t.Fatalf("exit code = %d, want 37", code)
	}
}

func TestStopUsesComposeStopAndNeverDestructiveCommands(t *testing.T) {
	root := projectFixture(t)
	a, _, stderr := testApp(root)
	if code := a.run([]string{"stop"}); code != 0 {
		t.Fatalf("stop exit code %d: %s", code, stderr.String())
	}
	runner := a.runner.(*fakeComposeRunner)
	if !containsComposeCall(runner.calls, "stop") {
		t.Fatalf("Compose stop missing: %#v", runner.calls)
	}
	for _, call := range runner.calls {
		for _, forbidden := range []string{"down", "rm", "volume"} {
			if containsArgument(call, forbidden) {
				t.Fatalf("stop used destructive argument %q: %#v", forbidden, call)
			}
		}
	}
}

func TestInitPerformsGuardedV1HandoverWithoutChangingEnvironmentOrVolume(t *testing.T) {
	root := t.TempDir()
	environment := []byte("ORACLE_PWD='ExistingSecret123'\nORACLE_CPUS=4\nORACLE_MEMORY=6g\nORACLE_SHM_SIZE=1g\n")
	if err := os.WriteFile(filepath.Join(root, environmentFile), environment, 0o600); err != nil {
		t.Fatal(err)
	}
	a, stdout, stderr := testApp(root)
	runner := a.runner.(*fakeComposeRunner)
	runner.run = func(args []string, output, errorOutput io.Writer) error {
		switch {
		case len(args) >= 2 && args[0] == "container" && args[1] == "inspect":
			_, _ = io.WriteString(errorOutput, "No such object")
			return errors.New("not found")
		case len(args) >= 2 && args[0] == "volume" && args[1] == "inspect":
			_, _ = io.WriteString(output, `{"dev.oralab.managed":"true"}`)
		case containsArgument(args, "config"):
			_, _ = io.WriteString(output, `{"services":{"oracle":{"image":"container-registry.oracle.com/database/enterprise:19.3.0.0"}}}`)
		}
		return nil
	}
	if code := a.run([]string{"init"}); code != 0 {
		t.Fatalf("migration init exit code %d, stderr: %s", code, stderr.String())
	}
	got, err := os.ReadFile(filepath.Join(root, environmentFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, environment) {
		t.Fatalf("existing .env changed: %q", got)
	}
	if !regularFile(filepath.Join(root, composeFileName)) || !regularFile(filepath.Join(root, environmentSample)) {
		t.Fatal("V2 files were not installed")
	}
	state, err := readLabState(root)
	if err != nil || state.Image != "container-registry.oracle.com/database/enterprise:19.3.0.0" {
		t.Fatalf("migration image state = %#v, err=%v", state, err)
	}
	if !strings.Contains(stdout.String(), "preserved unchanged") {
		t.Fatalf("migration output = %q", stdout.String())
	}
	for _, call := range runner.calls {
		if len(call) >= 2 && call[0] == "volume" && call[1] == "create" {
			t.Fatalf("migration tried to create/replace the volume: %#v", call)
		}
	}
}

func TestCompatibleCustomImageFlowsFromComposeWithoutRebuildingCLI(t *testing.T) {
	root := projectFixture(t)
	customCompose := []byte("services:\n  oracle:\n    image: container-registry.oracle.com/database/enterprise:19.99.0-test\n")
	if err := os.WriteFile(filepath.Join(root, composeFileName), customCompose, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("ORACLE_PWD='Secret123'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _, stderr := testApp(root)
	runner := a.runner.(*fakeComposeRunner)
	runner.run = func(args []string, output, errorOutput io.Writer) error {
		switch {
		case len(args) >= 2 && args[0] == "container" && args[1] == "inspect":
			_, _ = io.WriteString(errorOutput, "No such object")
			return errors.New("not found")
		case containsArgument(args, "config"):
			_, _ = io.WriteString(output, `{"services":{"oracle":{"image":"container-registry.oracle.com/database/enterprise:19.99.0-test"}}}`)
		case containsArgument(args, "ps"):
			_, _ = io.WriteString(output, `{"ID":"abcdef123456","Service":"oracle","State":"running","Health":"healthy"}`+"\n")
		}
		return nil
	}
	if code := a.run([]string{"up"}); code != 0 {
		t.Fatalf("custom image up exit code %d: %s", code, stderr.String())
	}
	state, err := readLabState(root)
	if err != nil || state.Image != "container-registry.oracle.com/database/enterprise:19.99.0-test" {
		t.Fatalf("recorded state = %#v, err=%v", state, err)
	}
	after, err := os.ReadFile(filepath.Join(root, composeFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, customCompose) {
		t.Fatalf("custom compose.yaml was overwritten: %q", after)
	}
}

func TestLogsRedactPasswordAcrossSubprocessWriteBoundaries(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("ORACLE_PWD='Secret123'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, stdout, stderr := testApp(root)
	runner := a.runner.(*fakeComposeRunner)
	runner.run = func(args []string, output, errorOutput io.Writer) error {
		if len(args) >= 2 && args[0] == "container" && args[1] == "inspect" {
			_, _ = io.WriteString(errorOutput, "No such object")
			return errors.New("not found")
		}
		if containsArgument(args, "logs") {
			_, _ = io.WriteString(output, "prefix Secr")
			_, _ = io.WriteString(output, "et123 suffix")
		}
		return nil
	}
	if code := a.run([]string{"logs"}); code != 0 {
		t.Fatalf("logs exit code %d: %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Secret123") || !strings.Contains(stdout.String(), "[REDACTED]") {
		t.Fatalf("redacted logs = %q", stdout.String())
	}
}

func TestEnvironmentRoundTripsSingleQuoteInPassword(t *testing.T) {
	root := t.TempDir()
	options, err := parseInitOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, environmentFile)
	if err := createLabConfig(path, "Valid'Pass123", options); err != nil {
		t.Fatal(err)
	}
	config, err := readLabConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Password != "Valid'Pass123" {
		t.Fatalf("password round-trip = %q", config.Password)
	}
}
