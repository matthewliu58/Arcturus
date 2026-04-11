package routing

type PathInfo struct {
	Hops []string `json:"hops"`
	Rtt  float64  `json:"rtt"`
	//Weight int64  `json:"weight"`
}

type RoutingInfo struct {
	Routing []PathInfo `json:"routing"`
}

type EndPoints struct {
	Source EndPoint `json:"source"`
	Dest   EndPoint `json:"dest"`
}

type EndPoint struct {
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Provider  string `json:"provider"`
	Continent string `json:"continent"`
	Country   string `json:"country"`
	City      string `json:"city"`
}
