package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type scriptedDockerRunner struct {
	outputs map[string]string
	errors  map[string]error
	lookErr error
}

func (r *scriptedDockerRunner) LookPath(string) (string, error) {
	return "docker", r.lookErr
}

func (r *scriptedDockerRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	return []byte(r.outputs[key]), r.errors[key]
}

func TestUpdateResourceSettingsPreservesUnrelatedConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, environmentFile)
	original := "# local settings\nORACLE_IMAGE=registry.example/oracle:19c\nORACLE_PWD=do-not-touch\nORACLE_CPUS=2\nCUSTOM=value\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := updateResourceSettings(path, 4, "8g"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, expected := range []string{
		"ORACLE_IMAGE=registry.example/oracle:19c",
		"ORACLE_PWD=do-not-touch",
		"CUSTOM=value",
		"ORACLE_CPUS=4",
		"ORACLE_MEMORY=8g",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("updated file is missing %q:\n%s", expected, text)
		}
	}
}

func TestUpdateResourceSettingsRejectsDuplicateManagedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), environmentFile)
	original := "ORACLE_CPUS=2\nORACLE_CPUS=3\nORACLE_MEMORY=4g\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := updateResourceSettings(path, 4, "8g"); err == nil {
		t.Fatal("duplicate managed key was accepted")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != original {
		t.Fatalf("configuration changed after rejection:\n%s", contents)
	}
}

func TestParseByteSize(t *testing.T) {
	tests := map[string]int64{
		"6g":     6 * 1024 * 1024 * 1024,
		"3.5GiB": int64(3.5 * 1024 * 1024 * 1024),
		"512MiB": 512 * 1024 * 1024,
		"1000MB": 1000 * 1000 * 1000,
	}
	for input, expected := range tests {
		actual, err := parseByteSize(input)
		if err != nil {
			t.Fatalf("parseByteSize(%q): %v", input, err)
		}
		if actual != expected {
			t.Errorf("parseByteSize(%q) = %d, want %d", input, actual, expected)
		}
	}
}

func TestManagerReadsRealDockerResponses(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("ORACLE_CPUS=4\nORACLE_MEMORY=6g\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	containerID := "1234567890abcdef"
	composePrefix := "docker compose --project-directory " + root + " --file " + filepath.Join(root, composeFileName) + " "
	runner := &scriptedDockerRunner{outputs: map[string]string{
		"docker context show":                                         "default\n",
		"docker context inspect default":                              `[{"Endpoints":{"docker":{"Host":"npipe:////./pipe/docker_engine"}}}]`,
		"docker info --format {{json .}}":                             `{"Name":"desktop","OperatingSystem":"Docker Desktop","ServerVersion":"29.0","NCPU":8,"MemTotal":10737418240}`,
		composePrefix + "config --services":                           "oracle\n",
		composePrefix + "ps -q oracle":                                containerID + "\n",
		"docker inspect " + containerID:                               `[{"State":{"Status":"running","Running":true,"Restarting":false,"ExitCode":0,"Health":{"Status":"healthy"}},"HostConfig":{"NanoCpus":4000000000,"Memory":6442450944},"Mounts":[{"Type":"volume","Name":"oradata","Source":"/var/lib/docker/volumes/oradata","Destination":"/opt/oracle/oradata"}]}]`,
		"docker stats --no-stream --format {{json .}} " + containerID: `{"CPUPerc":"23.5%","MemUsage":"3.5GiB / 6GiB"}`,
	}, errors: map[string]error{}}

	state := newResourceManager(root, runner).state(context.Background())
	if !state.Docker.Reachable || !state.Docker.LocalContext {
		t.Fatalf("Docker state = %#v", state.Docker)
	}
	if state.Oracle.Readiness != "ready" || !state.Oracle.Running {
		t.Fatalf("Oracle state = %#v", state.Oracle)
	}
	if state.Resources.AppliedCPUs != 4 || state.Resources.AppliedMemoryBytes != 6*1024*1024*1024 {
		t.Fatalf("applied resources = %#v", state.Resources)
	}
	if !state.Resources.StatsAvailable || state.Resources.CPUPercent != 23.5 {
		t.Fatalf("statistics = %#v", state.Resources)
	}
	if len(state.Storage) != 1 || state.Storage[0].Destination != "/opt/oracle/oradata" {
		t.Fatalf("storage = %#v", state.Storage)
	}
}

func TestRemoteDockerContextIsNotLocal(t *testing.T) {
	if isLocalDockerEndpoint("tcp://remote.example:2376") {
		t.Fatal("remote TCP Docker endpoint was treated as local")
	}
	if !isLocalDockerEndpoint("unix:///var/run/docker.sock") {
		t.Fatal("local Unix Docker endpoint was rejected")
	}
	if !isLocalDockerEndpoint("npipe:////./pipe/docker_engine") {
		t.Fatal("local Windows Docker endpoint was rejected")
	}
}

func TestMissingDockerIsReportedWithoutCommands(t *testing.T) {
	runner := &scriptedDockerRunner{outputs: map[string]string{}, errors: map[string]error{}, lookErr: errors.New("not found")}
	state := newResourceManager(t.TempDir(), runner).state(context.Background())
	if state.Docker.CLIAvailable || state.Docker.Error == "" {
		t.Fatalf("Docker state = %#v", state.Docker)
	}
}
