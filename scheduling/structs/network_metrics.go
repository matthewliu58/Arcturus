package structs

type NodeState struct {
	CpuMean float64
	CpuVar  float64
}

type NetState struct {
	AboveThresholdCpuMeans []float64
	BelowThresholdCpuMeans []float64
	AboveThresholdCpuVars  []float64
	BelowThresholdCpuVars  []float64
}
