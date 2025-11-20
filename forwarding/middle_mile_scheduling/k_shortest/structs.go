package k_shortest

import (
	"forwarding/middle_mile_scheduling/common"
)

// Type aliases for backward compatibility
// These types are now defined in the common package

// the numbers of nodes in a network start from 0, e.g., 10 nodes are 0-9.
// Deprecated: Use common.Network instead
type Network = common.Network

// Deprecated: Use common.Node instead
type Node = common.Node

// Deprecated: Use common.Flow instead
type Flow = common.Flow

// Deprecated: Use common.Path instead
type Path = common.Path

// Deprecated: Use common.PathWithIP instead
type PathWithIP = common.PathWithIP

// Deprecated: Use common.Result instead
type Result = common.Result

// RoutingResult represents the complete routing result
type RoutingResult struct {
	Net   Network `json:"net"`
	Flows []Flow  `json:"flows"`
}

// Edge represents a directed edge in a graph
// Deprecated: Use common.Edge instead
type Edge = common.Edge

// Graph represents a flow network graph
// Deprecated: Use common.Graph instead
type Graph = common.Graph
