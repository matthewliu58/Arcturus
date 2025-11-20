package bpr

import (
	"math"
	"testing"
)

func TestBPR(t *testing.T) {
}

func TestBPRAlgorithm_Simple3Nodes(t *testing.T) {
	// Construct 3 nodes: A (1-core), B (1-core), C (2-core)
	nodes := []*Node{
		{
			id:           0,
			ip:           "10.0.0.1",
			onsetReq:     10,
			reqRate:      10,
			cpuUsage:     30.0,
			queueBacklog: 5.0,
			delay:        20.0,
			CoreNum:      1,
		},
		{
			id:           1,
			ip:           "10.0.0.2",
			onsetReq:     20,
			reqRate:      20,
			cpuUsage:     50.0,
			queueBacklog: 3.0,
			delay:        30.0,
			CoreNum:      1,
		},
		{
			id:           2,
			ip:           "10.0.0.3",
			onsetReq:     30,
			reqRate:      30,
			cpuUsage:     40.0,
			queueBacklog: 2.0,
			delay:        25.0,
			CoreNum:      2,
		},
	}

	// total increment to distribute
	R := 30
	p := 0.2

	// Run algorithm
	dist := BPRAlgorithm(nodes, R, p)

	// Print distribution and final node states for debugging/inspection
	t.Logf("BPR distribution result:")
	for ip, v := range dist {
		t.Logf("  %s => %d", ip, v)
	}
	t.Logf("Final node states:")
	for _, n := range nodes {
		t.Logf("  node %s(id=%d): reqRate=%d cpuUsage=%.2f queueBacklog=%.2f dpp=%.4f",
			n.ip, n.id, n.reqRate, n.cpuUsage, n.queueBacklog, n.dppValue)
	}

	// Basic checks: keys exist and total allocated equals onset sum + R
	if len(dist) != 3 {
		t.Fatalf("expected 3 nodes in distribution, got %d", len(dist))
	}

	totalAllocated := 0
	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		v, ok := dist[ip]
		if !ok {
			t.Fatalf("expected ip %s in distribution", ip)
		}
		if v < 0 {
			t.Fatalf("allocated requests for %s is negative: %d", ip, v)
		}
		totalAllocated += v
	}

	onsetSum := 10 + 20 + 30
	if totalAllocated != onsetSum+R {
		t.Fatalf("total allocated mismatch: expected %d, got %d", onsetSum+R, totalAllocated)
	}
}

// This test configures node 0 with a large queueBacklog and low cpuUsage
// and uses a larger redistribution proportion so that a redistribution
// attempt is more likely to be accepted. We assert that the final
// distribution differs from the initial proportional allocation.
func TestBPRAlgorithm_AcceptedRedistribution(t *testing.T) {
	// Construct 3 nodes: A (1-core, low cpu, high backlog), B (1-core), C (2-core)
	nodes := []*Node{
		{
			id:           0,
			ip:           "10.0.0.1",
			onsetReq:     950,
			reqRate:      950,
			cpuUsage:     1.0,  // small onset CPU so deltaCPU likely positive
			queueBacklog: 50.0, // large backlog to increase stabilityComponent
			delay:        20.0,
			CoreNum:      1,
		},
		{
			id:           1,
			ip:           "10.0.0.2",
			onsetReq:     1000,
			reqRate:      1000,
			cpuUsage:     5.0,
			queueBacklog: 3.0,
			delay:        30.0,
			CoreNum:      1,
		},
		{
			id:           2,
			ip:           "10.0.0.3",
			onsetReq:     1050,
			reqRate:      1050,
			cpuUsage:     4.0,
			queueBacklog: 2.0,
			delay:        25.0,
			CoreNum:      2,
		},
	}

	R := 300
	p := 0.3 // larger proportion to increase redistribution impact

	// Compute initial proportional distribution for comparison
	onsetSum := 0
	for _, n := range nodes {
		onsetSum += n.onsetReq
	}
	remaining := R
	initDist := map[string]int{}
	for i, n := range nodes {
		var inc int
		if i == len(nodes)-1 {
			inc = remaining
		} else {
			inc = int(math.Round(float64(n.onsetReq) / float64(onsetSum) * float64(R)))
			remaining -= inc
		}
		initDist[n.ip] = n.onsetReq + inc
	}

	dist := BPRAlgorithm(nodes, R, p)

	t.Logf("Initial proportional distribution: %v", initDist)
	t.Logf("BPR distribution result: %v", dist)

	// Print final node states
	for _, n := range nodes {
		t.Logf("node %s(id=%d): reqRate=%d cpuUsage=%.2f queueBacklog=%.2f dpp=%.4f",
			n.ip, n.id, n.reqRate, n.cpuUsage, n.queueBacklog, n.dppValue)
	}

	// Check that the distribution changed (i.e., some redistribution was accepted)
	same := true
	for ip, v := range initDist {
		if dist[ip] != v {
			same = false
			break
		}
	}
	if same {
		t.Fatalf("expected redistribution to be accepted (distribution changed), but got same as initial: %v", dist)
	}
}

// Grid search for parameter combinations where redistribution is accepted.
// This test logs the first found (backlog, p) combination that changes the distribution.
func TestBPRAlgorithm_GridSearchAccepted(t *testing.T) {
	baseNodes := func(backlog float64) []*Node {
		return []*Node{
			{id: 0, ip: "10.0.0.1", onsetReq: 10, reqRate: 10, cpuUsage: 1.0, queueBacklog: backlog, delay: 20.0, CoreNum: 1},
			{id: 1, ip: "10.0.0.2", onsetReq: 20, reqRate: 20, cpuUsage: 5.0, queueBacklog: 3.0, delay: 30.0, CoreNum: 1},
			{id: 2, ip: "10.0.0.3", onsetReq: 30, reqRate: 30, cpuUsage: 4.0, queueBacklog: 2.0, delay: 25.0, CoreNum: 2},
		}
	}

	backlogs := []float64{50, 100, 200, 500}
	ps := []float64{0.1, 0.2, 0.5, 0.8}

	onsetSum := 10 + 20 + 30

	found := false
	for _, b := range backlogs {
		for _, p := range ps {
			nodes := baseNodes(b)
			// compute initial dist
			remaining := 30
			initDist := map[string]int{}
			for i, n := range nodes {
				var inc int
				if i == len(nodes)-1 {
					inc = remaining
				} else {
					inc = int(math.Round(float64(n.onsetReq) / float64(onsetSum) * float64(30)))
					remaining -= inc
				}
				initDist[n.ip] = n.onsetReq + inc
			}

			dist := BPRAlgorithm(nodes, 30, p)
			same := true
			for ip, v := range initDist {
				if dist[ip] != v {
					same = false
					break
				}
			}
			if !same {
				t.Logf("Found accepted redistribution for backlog=%.0f p=%.2f => dist=%v", b, p, dist)
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Logf("No accepted redistribution found in grid search")
	}
}
