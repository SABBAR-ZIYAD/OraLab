package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	cpuSettingKey       = "ORACLE_CPUS"
	memorySettingKey    = "ORACLE_MEMORY"
	shmSettingKey       = "ORACLE_SHM_SIZE"
	passwordSettingKey  = "ORACLE_PWD"
	managedLabel        = "dev.oralab.managed"
	oracleContainerName = "oralab-oracle"
	oracleVolumeName    = "oralab-oradata"
	oracleDataPath      = "/opt/oracle/oradata"
	oracleRegistryURL   = "https://container-registry.oracle.com/"
	operationTimeout    = 31 * time.Minute
	stateTimeout        = 12 * time.Second
	defaultLogTail      = "200"
)

type logOptions struct {
	Follow     bool
	Tail       string
	Since      string
	Timestamps bool
}

type labConfig struct {
	Password    string
	SID         string
	PDB         string
	CPUs        int
	MemoryBytes int64
	ShmBytes    int64
}

func readLabConfig(path string) (labConfig, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return labConfig{}, errors.New(".env is missing; run oralab init first")
		}
		return labConfig{}, err
	}
	values, err := parseEnvironment(contents)
	if err != nil {
		return labConfig{}, err
	}
	config := labConfig{Password: values[passwordSettingKey], SID: strings.TrimSpace(values["ORACLE_SID"]), PDB: strings.TrimSpace(values["ORACLE_PDB"])}
	if config.SID == "" {
		config.SID = "ORCLCDB"
	}
	if config.PDB == "" {
		config.PDB = "ORCLPDB1"
	}
	if value := strings.TrimSpace(values[cpuSettingKey]); value != "" {
		config.CPUs, err = strconv.Atoi(value)
		if err != nil || config.CPUs <= 0 {
			return labConfig{}, fmt.Errorf("%s must be a positive whole number", cpuSettingKey)
		}
	}
	if value := strings.TrimSpace(values[memorySettingKey]); value != "" {
		config.MemoryBytes, err = parseByteSize(value)
		if err != nil || config.MemoryBytes <= 0 {
			return labConfig{}, fmt.Errorf("%s is invalid", memorySettingKey)
		}
	}
	config.ShmBytes = 1024 * 1024 * 1024
	if value := strings.TrimSpace(values[shmSettingKey]); value != "" {
		config.ShmBytes, err = parseByteSize(value)
		if err != nil || config.ShmBytes <= 0 {
			return labConfig{}, fmt.Errorf("%s is invalid", shmSettingKey)
		}
	}
	if config.MemoryBytes > 0 && config.ShmBytes > config.MemoryBytes {
		return labConfig{}, fmt.Errorf("%s cannot exceed %s", shmSettingKey, memorySettingKey)
	}
	return config, nil
}

func createLabConfig(path, password string, options initOptions) error {
	quotedPassword := "'" + strings.ReplaceAll(password, "'", "\\'") + "'"
	contents := fmt.Sprintf("# Created by OraLab. Change the Oracle image only in compose.yaml.\nORACLE_PWD=%s\nORACLE_SID=ORCLCDB\nORACLE_PDB=ORCLPDB1\nORACLE_CPUS=%d\nORACLE_MEMORY=%s\nORACLE_SHM_SIZE=%s\nORALAB_STOP_GRACE_PERIOD=10m\n", quotedPassword, options.CPUs, options.Memory, options.Shm)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return errors.New("refusing to overwrite existing .env")
	}
	if err != nil {
		return err
	}
	if _, err := file.WriteString(contents); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func parseEnvironment(contents []byte) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("duplicate setting %s", key)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = strings.ReplaceAll(value[1:len(value)-1], `\'`, `'`)
		} else if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	return values, scanner.Err()
}

func parseByteSize(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty size")
	}
	lower := strings.ToLower(value)
	units := []struct {
		suffix     string
		multiplier float64
	}{
		{"gib", 1024 * 1024 * 1024}, {"gb", 1000 * 1000 * 1000}, {"g", 1024 * 1024 * 1024},
		{"mib", 1024 * 1024}, {"mb", 1000 * 1000}, {"m", 1024 * 1024},
		{"kib", 1024}, {"kb", 1000}, {"k", 1024}, {"b", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(lower, unit.suffix) {
			number := strings.TrimSpace(strings.TrimSuffix(lower, unit.suffix))
			parsed, err := strconv.ParseFloat(number, 64)
			if err != nil || parsed < 0 {
				return 0, errors.New("invalid size")
			}
			return int64(parsed * unit.multiplier), nil
		}
	}
	parsed, err := strconv.ParseInt(lower, 10, 64)
	if err != nil || parsed < 0 {
		return 0, errors.New("invalid size")
	}
	return parsed, nil
}

func isLocalDockerEndpoint(endpoint string) bool {
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	if endpoint == "" || strings.HasPrefix(endpoint, "unix://") || strings.HasPrefix(endpoint, "npipe://") {
		return true
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "tcp" && parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func hostOSName(goos string) string {
	switch goos {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	default:
		return goos
	}
}

func shortContainerID(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
