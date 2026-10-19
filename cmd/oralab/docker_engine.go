package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/registry"
	"github.com/moby/moby/client"
)

var errDockerObjectNotFound = errors.New("Docker object not found")

type engineInfo struct {
	Name            string
	OperatingSystem string
	ServerVersion   string
	CPUs            int
	MemoryBytes     int64
}

type engineMount struct {
	Type        string
	Name        string
	Source      string
	Destination string
}

type engineContainer struct {
	ID               string
	Name             string
	Image            string
	Status           string
	Running          bool
	Restarting       bool
	ExitCode         int
	HealthConfigured bool
	Health           string
	CPUs             float64
	MemoryBytes      int64
	TTY              bool
	Managed          bool
	Mounts           []engineMount
}

type engineStats struct {
	CPUPercent      float64
	MemoryUsedBytes int64
}

type engineContainerSpec struct {
	Name        string
	Image       string
	Environment []string
	CPUs        int
	MemoryBytes int64
	ShmBytes    int64
	StopTimeout int
	VolumeName  string
	VolumePath  string
	HostIP      string
	HostPort    string
	TargetPort  string
	Labels      map[string]string
}

type logOptions struct {
	Follow     bool
	Tail       string
	Since      string
	Timestamps bool
}

type dockerEngine interface {
	Close() error
	Endpoint() string
	Info(context.Context) (engineInfo, error)
	InspectContainer(context.Context, string) (engineContainer, error)
	EnsureImage(context.Context, string) error
	CreateContainer(context.Context, engineContainerSpec) (string, error)
	StartContainer(context.Context, string) error
	StopContainer(context.Context, string, int) error
	RestartContainer(context.Context, string, int) error
	UpdateResources(context.Context, string, int, int64) error
	Stats(context.Context, string) (engineStats, error)
	Logs(context.Context, string, logOptions, io.Writer, io.Writer) error
	Exec(context.Context, string, []string, bool, io.Reader, io.Writer, io.Writer) (int, error)
}

type mobyEngine struct {
	client       *client.Client
	registryAuth string
}

func newMobyEngine() (dockerEngine, error) {
	registryAuth, err := registryAuthFromEnvironment(os.Getenv)
	if err != nil {
		return nil, err
	}
	apiClient, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	return &mobyEngine{client: apiClient, registryAuth: registryAuth}, nil
}

func (e *mobyEngine) Close() error {
	return e.client.Close()
}

func (e *mobyEngine) Endpoint() string {
	return e.client.DaemonHost()
}

func (e *mobyEngine) Info(ctx context.Context) (engineInfo, error) {
	result, err := e.client.Info(ctx, client.InfoOptions{})
	if err != nil {
		return engineInfo{}, err
	}
	return engineInfo{
		Name:            result.Info.Name,
		OperatingSystem: result.Info.OperatingSystem,
		ServerVersion:   result.Info.ServerVersion,
		CPUs:            result.Info.NCPU,
		MemoryBytes:     result.Info.MemTotal,
	}, nil
}

func (e *mobyEngine) InspectContainer(ctx context.Context, name string) (engineContainer, error) {
	result, err := e.client.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		if errdefs.IsNotFound(err) {
			return engineContainer{}, errDockerObjectNotFound
		}
		return engineContainer{}, err
	}
	inspected := result.Container
	details := engineContainer{
		ID:      inspected.ID,
		Name:    strings.TrimPrefix(inspected.Name, "/"),
		Image:   inspected.Image,
		Managed: inspected.Config != nil && inspected.Config.Labels[managedLabel] == "true",
	}
	if inspected.Config != nil {
		details.TTY = inspected.Config.Tty
	}
	if inspected.State != nil {
		details.Status = string(inspected.State.Status)
		details.Running = inspected.State.Running
		details.Restarting = inspected.State.Restarting
		details.ExitCode = inspected.State.ExitCode
		if inspected.State.Health != nil {
			details.HealthConfigured = true
			details.Health = string(inspected.State.Health.Status)
		}
	}
	if inspected.HostConfig != nil {
		details.CPUs = float64(inspected.HostConfig.NanoCPUs) / 1_000_000_000
		details.MemoryBytes = inspected.HostConfig.Memory
	}
	for _, item := range inspected.Mounts {
		details.Mounts = append(details.Mounts, engineMount{
			Type:        string(item.Type),
			Name:        item.Name,
			Source:      item.Source,
			Destination: item.Destination,
		})
	}
	return details, nil
}

func (e *mobyEngine) EnsureImage(ctx context.Context, image string) error {
	if _, err := e.client.ImageInspect(ctx, image); err == nil {
		return nil
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	response, err := e.client.ImagePull(ctx, image, client.ImagePullOptions{RegistryAuth: e.registryAuth})
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	defer response.Close()
	decoder := json.NewDecoder(response)
	for {
		var event struct {
			Error string `json:"error"`
		}
		if err := decoder.Decode(&event); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("read image pull response: %w", err)
		}
		if event.Error != "" {
			return errors.New(event.Error)
		}
	}
}

func registryAuthFromEnvironment(getenv func(string) string) (string, error) {
	username := strings.TrimSpace(getenv("ORALAB_REGISTRY_USERNAME"))
	token := getenv("ORALAB_REGISTRY_TOKEN")
	server := strings.TrimSpace(getenv("ORALAB_REGISTRY_SERVER"))
	if username == "" && token == "" && server == "" {
		return "", nil
	}
	if username == "" || token == "" {
		return "", errors.New("ORALAB_REGISTRY_USERNAME and ORALAB_REGISTRY_TOKEN must be set together")
	}
	if server == "" {
		server = defaultOracleRegistry
	}
	payload, err := json.Marshal(registry.AuthConfig{
		Username:      username,
		Password:      token,
		ServerAddress: server,
	})
	if err != nil {
		return "", fmt.Errorf("encode registry credentials: %w", err)
	}
	return base64.URLEncoding.EncodeToString(payload), nil
}

func (e *mobyEngine) CreateContainer(ctx context.Context, spec engineContainerSpec) (string, error) {
	if _, err := e.client.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name:   spec.VolumeName,
		Labels: spec.Labels,
	}); err != nil {
		return "", fmt.Errorf("create data volume: %w", err)
	}
	targetPort, err := network.ParsePort(spec.TargetPort)
	if err != nil {
		return "", fmt.Errorf("parse listener port: %w", err)
	}
	hostIP, err := netip.ParseAddr(spec.HostIP)
	if err != nil {
		return "", fmt.Errorf("parse listener address: %w", err)
	}
	result, err := e.client.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: spec.Name,
		Config: &container.Config{
			Image:        spec.Image,
			Env:          spec.Environment,
			ExposedPorts: network.PortSet{targetPort: {}},
			Labels:       spec.Labels,
			StopTimeout:  &spec.StopTimeout,
		},
		HostConfig: &container.HostConfig{
			PortBindings: network.PortMap{
				targetPort: {{HostIP: hostIP, HostPort: spec.HostPort}},
			},
			ShmSize: spec.ShmBytes,
			Resources: container.Resources{
				NanoCPUs: int64(spec.CPUs) * 1_000_000_000,
				Memory:   spec.MemoryBytes,
			},
			Mounts: []mount.Mount{{
				Type:   mount.TypeVolume,
				Source: spec.VolumeName,
				Target: spec.VolumePath,
			}},
		},
	})
	if err != nil {
		return "", err
	}
	return result.ID, nil
}

func (e *mobyEngine) StartContainer(ctx context.Context, name string) error {
	_, err := e.client.ContainerStart(ctx, name, client.ContainerStartOptions{})
	return err
}

func (e *mobyEngine) StopContainer(ctx context.Context, name string, timeout int) error {
	_, err := e.client.ContainerStop(ctx, name, client.ContainerStopOptions{Timeout: &timeout})
	return err
}

func (e *mobyEngine) RestartContainer(ctx context.Context, name string, timeout int) error {
	_, err := e.client.ContainerRestart(ctx, name, client.ContainerRestartOptions{Timeout: &timeout})
	return err
}

func (e *mobyEngine) UpdateResources(ctx context.Context, name string, cpus int, memory int64) error {
	_, err := e.client.ContainerUpdate(ctx, name, client.ContainerUpdateOptions{
		Resources: &container.Resources{
			NanoCPUs: int64(cpus) * 1_000_000_000,
			Memory:   memory,
		},
	})
	return err
}

func (e *mobyEngine) Stats(ctx context.Context, name string) (engineStats, error) {
	result, err := e.client.ContainerStats(ctx, name, client.ContainerStatsOptions{
		IncludePreviousSample: true,
	})
	if err != nil {
		return engineStats{}, err
	}
	defer result.Body.Close()
	var stats container.StatsResponse
	if err := json.NewDecoder(result.Body).Decode(&stats); err != nil {
		return engineStats{}, err
	}
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)
	onlineCPUs := stats.CPUStats.OnlineCPUs
	if onlineCPUs == 0 {
		onlineCPUs = uint32(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	var cpuPercent float64
	if cpuDelta > 0 && systemDelta > 0 && onlineCPUs > 0 {
		cpuPercent = cpuDelta / systemDelta * float64(onlineCPUs) * 100
	}
	return engineStats{
		CPUPercent:      cpuPercent,
		MemoryUsedBytes: int64(stats.MemoryStats.Usage),
	}, nil
}

func (e *mobyEngine) Logs(ctx context.Context, name string, options logOptions, stdout, stderr io.Writer) error {
	inspected, err := e.InspectContainer(ctx, name)
	if err != nil {
		return err
	}
	stream, err := e.client.ContainerLogs(ctx, name, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     options.Follow,
		Tail:       options.Tail,
		Since:      options.Since,
		Timestamps: options.Timestamps,
	})
	if err != nil {
		return err
	}
	defer stream.Close()
	return copyDockerStream(stdout, stderr, stream, inspected.TTY)
}

func (e *mobyEngine) Exec(ctx context.Context, name string, command []string, tty bool, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	created, err := e.client.ExecCreate(ctx, name, client.ExecCreateOptions{
		TTY:          tty,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          append([]string(nil), command...),
	})
	if err != nil {
		return 1, err
	}
	attached, err := e.client.ExecAttach(ctx, created.ID, client.ExecAttachOptions{TTY: tty})
	if err != nil {
		return 1, err
	}
	defer attached.Close()
	if stdin != nil {
		go func() {
			_, _ = io.Copy(attached.Conn, stdin)
			_ = attached.CloseWrite()
		}()
	}
	if err := copyDockerStream(stdout, stderr, attached.Reader, tty); err != nil {
		return 1, err
	}
	inspected, err := e.client.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return 1, err
	}
	return inspected.ExitCode, nil
}

func copyDockerStream(stdout, stderr io.Writer, reader io.Reader, tty bool) error {
	if tty {
		_, err := io.Copy(stdout, reader)
		return err
	}
	buffered := bufio.NewReader(reader)
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(buffered, header); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}
		length := int64(binary.BigEndian.Uint32(header[4:]))
		destination := stdout
		if header[0] == 2 {
			destination = stderr
		}
		if _, err := io.CopyN(destination, buffered, length); err != nil {
			return err
		}
	}
}
