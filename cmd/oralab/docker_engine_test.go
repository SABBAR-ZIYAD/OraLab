package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type fakeEngine struct {
	endpoint       string
	info           engineInfo
	infoErr        error
	container      engineContainer
	inspectErr     error
	stats          engineStats
	statsErr       error
	ensureImage    string
	ensureImageErr error
	createSpec     engineContainerSpec
	createErr      error
	startCalls     int
	stopTimeout    int
	restartTimeout int
	updateCPUs     int
	updateMemory   int64
	execCommand    []string
	execTTY        bool
	execExit       int
	execErr        error
	logsOptions    logOptions
}

func (f *fakeEngine) Close() error { return nil }
func (f *fakeEngine) Endpoint() string {
	if f.endpoint == "" {
		return "unix:///var/run/docker.sock"
	}
	return f.endpoint
}
func (f *fakeEngine) Info(context.Context) (engineInfo, error) {
	return f.info, f.infoErr
}
func (f *fakeEngine) InspectContainer(context.Context, string) (engineContainer, error) {
	return f.container, f.inspectErr
}
func (f *fakeEngine) EnsureImage(_ context.Context, image string) error {
	f.ensureImage = image
	return f.ensureImageErr
}
func (f *fakeEngine) CreateContainer(_ context.Context, spec engineContainerSpec) (string, error) {
	f.createSpec = spec
	if f.createErr != nil {
		return "", f.createErr
	}
	f.inspectErr = nil
	f.container = engineContainer{
		ID: "1234567890abcdef", Name: spec.Name, Image: spec.Image, Status: "created",
		Managed: true, CPUs: float64(spec.CPUs), MemoryBytes: spec.MemoryBytes,
		Mounts: []engineMount{{Type: "volume", Name: spec.VolumeName, Destination: spec.VolumePath}},
	}
	return f.container.ID, nil
}
func (f *fakeEngine) StartContainer(context.Context, string) error {
	f.startCalls++
	f.container.Running = true
	f.container.Status = "running"
	f.container.HealthConfigured = true
	f.container.Health = "healthy"
	return nil
}
func (f *fakeEngine) StopContainer(_ context.Context, _ string, timeout int) error {
	f.stopTimeout = timeout
	f.container.Running = false
	f.container.Status = "exited"
	return nil
}
func (f *fakeEngine) RestartContainer(_ context.Context, _ string, timeout int) error {
	f.restartTimeout = timeout
	f.container.Running = true
	f.container.Status = "running"
	f.container.HealthConfigured = true
	f.container.Health = "healthy"
	return nil
}
func (f *fakeEngine) UpdateResources(_ context.Context, _ string, cpus int, memory int64) error {
	f.updateCPUs = cpus
	f.updateMemory = memory
	f.container.CPUs = float64(cpus)
	f.container.MemoryBytes = memory
	return nil
}
func (f *fakeEngine) Stats(context.Context, string) (engineStats, error) {
	return f.stats, f.statsErr
}
func (f *fakeEngine) Logs(_ context.Context, _ string, options logOptions, _, _ io.Writer) error {
	f.logsOptions = options
	return nil
}
func (f *fakeEngine) Exec(_ context.Context, _ string, command []string, tty bool, _ io.Reader, _, _ io.Writer) (int, error) {
	f.execCommand = append([]string(nil), command...)
	f.execTTY = tty
	return f.execExit, f.execErr
}

func multiplex(stream byte, payload string) []byte {
	var buffer bytes.Buffer
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(payload)))
	buffer.Write(header)
	buffer.WriteString(payload)
	return buffer.Bytes()
}

func TestRegistryCredentialsAreEncodedFromProcessEnvironment(t *testing.T) {
	values := map[string]string{
		"ORALAB_REGISTRY_USERNAME": "student",
		"ORALAB_REGISTRY_TOKEN":    "temporary-token",
		"ORALAB_REGISTRY_SERVER":   "container-registry.oracle.com",
	}
	encoded, err := registryAuthFromEnvironment(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "" || strings.Contains(encoded, "temporary-token") {
		t.Fatalf("credentials were not safely encoded: %q", encoded)
	}
	payload, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var auth struct {
		ServerAddress string `json:"serveraddress"`
	}
	if err := json.Unmarshal(payload, &auth); err != nil {
		t.Fatal(err)
	}
	if auth.ServerAddress != defaultOracleRegistry {
		t.Fatalf("registry server = %q", auth.ServerAddress)
	}
}

func TestRegistryCredentialsMustBeComplete(t *testing.T) {
	_, err := registryAuthFromEnvironment(func(key string) string {
		if key == "ORALAB_REGISTRY_USERNAME" {
			return "student"
		}
		return ""
	})
	if err == nil {
		t.Fatal("partial registry credentials were accepted")
	}
}
