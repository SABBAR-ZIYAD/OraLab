package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateResourceSettingsPreservesUnrelatedConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, environmentFile)
	original := "# local settings\nLEGACY_NOTE=keep-this-value\nORACLE_PWD=do-not-touch\nORACLE_CPUS=2\nCUSTOM=value\n"
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
		"LEGACY_NOTE=keep-this-value",
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

func TestParseEnvironmentPreservesPasswordQuoteCharacters(t *testing.T) {
	values, err := parseEnvironment([]byte("ORACLE_PWD='starts-with-a-quote\n"))
	if err != nil {
		t.Fatal(err)
	}
	if values[passwordSettingKey] != "'starts-with-a-quote" {
		t.Fatalf("password = %q", values[passwordSettingKey])
	}
}

func TestManagerReadsTypedEngineResponses(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("ORACLE_CPUS=4\nORACLE_MEMORY=6g\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := &fakeEngine{
		info: engineInfo{
			Name: "desktop", OperatingSystem: "Docker Desktop", ServerVersion: "29.0",
			CPUs: 8, MemoryBytes: 10 * 1024 * 1024 * 1024,
		},
		container: engineContainer{
			ID: "1234567890abcdef", Managed: true, Running: true, Status: "running",
			HealthConfigured: true, Health: "healthy", CPUs: 4, MemoryBytes: 6 * 1024 * 1024 * 1024,
			Mounts: []engineMount{{Type: "volume", Name: oracleVolumeName, Destination: oracleDataPath}},
		},
		stats: engineStats{CPUPercent: 23.5, MemoryUsedBytes: 3 * 1024 * 1024 * 1024},
	}
	state := newResourceManager(root, engine, nil).state(context.Background())
	if !state.Docker.Reachable || !state.Docker.LocalContext || !state.Docker.APIAvailable {
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
	if len(state.Storage) != 1 || state.Storage[0].Destination != oracleDataPath {
		t.Fatalf("storage = %#v", state.Storage)
	}
}

func TestResourceChangeUsesLiveEngineUpdate(t *testing.T) {
	root := projectFixture(t)
	if err := os.WriteFile(filepath.Join(root, environmentFile), []byte("ORACLE_CPUS=2\nORACLE_MEMORY=4g\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := &fakeEngine{
		info: engineInfo{CPUs: 8, MemoryBytes: 16 * 1024 * 1024 * 1024},
		container: engineContainer{
			ID: "id", Managed: true, Running: true, Status: "running",
			HealthConfigured: true, Health: "healthy", CPUs: 2, MemoryBytes: 4 * 1024 * 1024 * 1024,
		},
	}
	result := newResourceManager(root, engine, nil).apply(context.Background(), applyRequest{CPUs: 4, MemoryGiB: 6})
	if !result.OK || !result.Applied {
		t.Fatalf("result = %#v", result)
	}
	if engine.updateCPUs != 4 || engine.updateMemory != 6*1024*1024*1024 {
		t.Fatalf("update = %d CPUs, %d bytes", engine.updateCPUs, engine.updateMemory)
	}
}

func TestRemoteDockerEndpointIsNotLocal(t *testing.T) {
	if isLocalDockerEndpoint("tcp://remote.example:2376") {
		t.Fatal("remote TCP Docker endpoint was treated as local")
	}
	if !isLocalDockerEndpoint("unix:///var/run/docker.sock") {
		t.Fatal("local Unix Docker endpoint was rejected")
	}
	if !isLocalDockerEndpoint("npipe:////./pipe/docker_engine") {
		t.Fatal("local Windows Docker endpoint was rejected")
	}
	if !isLocalDockerEndpoint("tcp://127.0.0.1:2375") {
		t.Fatal("loopback TCP Docker endpoint was rejected")
	}
}

func TestMissingEngineIsReported(t *testing.T) {
	state := newResourceManager(t.TempDir(), nil, errors.New("unavailable")).state(context.Background())
	if state.Docker.APIAvailable || state.Docker.Error == "" {
		t.Fatalf("Docker state = %#v", state.Docker)
	}
}

func TestCopyDockerStreamDemultiplexesOutput(t *testing.T) {
	input := append(multiplex(1, "out"), multiplex(2, "err")...)
	var stdout, stderr strings.Builder
	if err := copyDockerStream(&stdout, &stderr, strings.NewReader(string(input)), false); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "out" || stderr.String() != "err" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
