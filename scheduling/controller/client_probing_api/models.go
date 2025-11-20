package client_probing_api

// NodeInfo defines the node information returned to the probing_client.
type NodeInfo struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// DelayData contains a single latency measurement.
// Field names match the expected JSON payload from the probing_client.
type DelayData struct {
	TargetIP  string  `json:"destIP"`  // The destination node IP for the probing_report
	DelayMS   float64 `json:"delayMS"` // Latency in milliseconds
	Timestamp int64   `json:"timestamp"`
}

// DelayReportPayload is the request body structure for reporting delays.
type DelayReportPayload struct {
	SourceIP string      `json:"sourceIP"` // The source IP of the probing_client reporting the data
	Delays   []DelayData `json:"delays"`   // A list of latency measurements
}
