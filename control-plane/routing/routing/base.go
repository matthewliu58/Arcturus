package routing

type PathInfo struct {
	Hops []string `json:"hops"`
	Rtt  float64  `json:"rtt"`
	//Weight int64  `json:"weight"`
}

type RoutingInfo struct {
	Routing []PathInfo `json:"routing"`
}
