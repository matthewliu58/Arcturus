package main

// Config represents the application configuration
type Config struct {
	ControllerAddr string `toml:"controller_addr"` // Controller API address, e.g., "http://controller:8080"
	ProbeInterval  int    `toml:"probe_interval"`  // Probe interval in seconds
	ProbeTimeout   int    `toml:"probe_timeout"`   // TCP probing_report timeout in milliseconds
	LocalIP        string `toml:"local_ip"`        // Optional: manually specify local IP
	LogLevel       string `toml:"log_level"`       // Log level: debug, info, warn, error
}

// NodeInfo represents a node returned by the controller
type NodeInfo struct {
	IP   string `json:"ip"`   // Node IP address
	Port int    `json:"port"` // Target port for TCP delay probing_report
}

// DelayInfo represents a single delay measurement
type DelayInfo struct {
	TargetIP  string  `json:"destIP"`    // Target node IP
	DelayMs   float64 `json:"delayMS"`   // Measured delay in milliseconds
	Timestamp int64   `json:"timestamp"` // Unix timestamp in seconds when probing_report occurred
}

// DelayReportPayload is the payload sent to the controller
type DelayReportPayload struct {
	SourceIP string      `json:"sourceIP"` // Source IP (this Traefik server)
	Delays   []DelayInfo `json:"delays"`   // List of delay measurements
}
