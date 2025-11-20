package common

// Network represents the network topology with nodes and links
type Network struct {
	Nodes []Node  `json:"nodes"`
	Links [][]int `json:"links"` // latency of every link, -1 means that there is no link between two nodes
}

// Node represents a network node
type Node struct {
}

// Flow represents a traffic flow between source and destination
type Flow struct {
	Source      int `json:"source"`
	Destination int `json:"destination"`
}

// Path represents a routing path with node indices
type Path struct {
	Nodes   []int `json:"nodes"`   // nodes on a route
	Latency int   `json:"latency"` // total latency of a route
}

// PathWithIP represents a routing path with IP addresses
// This is the output format used by PathManager and routing algorithms
type PathWithIP struct {
	IPList  []string
	Latency int
	Weight  int
}

// RoutingResult represents the complete routing result
type RoutingResult struct {
	Net   Network `json:"net"`
	Flows []Flow  `json:"flows"`
}

// Result represents a link result with IP addresses and value
type Result struct {
	Ip1   string
	Ip2   string
	Value float64
}

// Edge represents a directed edge in a graph
type Edge struct {
	From, To, Capacity int
}

// Graph represents a flow network graph
type Graph struct {
	Capacity [][]int
	Flow     [][]int
	Nodes    int
}

// PathCalculator defines the interface for routing algorithms
type PathCalculator interface {
	// ComputePaths computes paths between source and destination
	// Parameters:
	//   - network: the network topology
	//   - source: source node index
	//   - dest: destination node index
	//   - params: algorithm-specific parameters as map (e.g., {"k": 3} for K-Shortest)
	//   - ipToIndex: mapping from IP to node index
	//   - indexToIP: mapping from node index to IP
	// Returns:
	//   - list of PathWithIP objects sorted by some criteria (e.g., latency)
	ComputePaths(
		network *Network,
		source int,
		dest int,
		params map[string]interface{},
		ipToIndex map[string]int,
		indexToIP map[int]string,
	) []PathWithIP
}
