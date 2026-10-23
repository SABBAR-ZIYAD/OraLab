package main

import "time"

type hostState struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type dockerState struct {
	APIAvailable bool   `json:"api_available"`
	Reachable    bool   `json:"reachable"`
	LocalContext bool   `json:"local_context"`
	Context      string `json:"context,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	EngineName   string `json:"engine_name,omitempty"`
	EngineOS     string `json:"engine_os,omitempty"`
	Version      string `json:"version,omitempty"`
	CPUs         int    `json:"cpus,omitempty"`
	MemoryBytes  int64  `json:"memory_bytes,omitempty"`
	Error        string `json:"error,omitempty"`
}

type oracleState struct {
	Service     string `json:"service,omitempty"`
	Image       string `json:"image,omitempty"`
	SID         string `json:"sid,omitempty"`
	PDB         string `json:"pdb,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	Exists      bool   `json:"exists"`
	Running     bool   `json:"running"`
	Status      string `json:"status"`
	Health      string `json:"health"`
	Readiness   string `json:"readiness"`
	Error       string `json:"error,omitempty"`
}

type resourceState struct {
	ConfiguredCPUs        int     `json:"configured_cpus,omitempty"`
	ConfiguredMemoryBytes int64   `json:"configured_memory_bytes,omitempty"`
	AppliedCPUs           float64 `json:"applied_cpus,omitempty"`
	AppliedMemoryBytes    int64   `json:"applied_memory_bytes,omitempty"`
	CPUPercent            float64 `json:"cpu_percent,omitempty"`
	MemoryUsageBytes      int64   `json:"memory_usage_bytes,omitempty"`
	StatsAvailable        bool    `json:"stats_available"`
	Error                 string  `json:"error,omitempty"`
}

type storageMount struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination"`
}

type managerState struct {
	Host      hostState      `json:"host"`
	Docker    dockerState    `json:"docker"`
	Oracle    oracleState    `json:"oracle"`
	Resources resourceState  `json:"resources"`
	Storage   []storageMount `json:"storage"`
	CheckedAt time.Time      `json:"checked_at"`
}

type operationResult struct {
	OK      bool         `json:"ok"`
	Applied bool         `json:"applied,omitempty"`
	Message string       `json:"message"`
	State   managerState `json:"state"`
}

type lifecycleRequest struct {
	Action string `json:"action"`
}
