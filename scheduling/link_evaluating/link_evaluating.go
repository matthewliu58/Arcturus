package linkevaluate

import (
	"math"
	"scheduling/structs"
)

type Evaluation struct {
	Delay         float64          //
	NormalCpuMean float64          // cpu
	NormalCpuVar  float64          // cpu
	QMean         float64          // CPU
	QVar          float64          // CPU
	Params        SystemParams     //
	State         structs.NetState //
}

type SystemParams struct {
	ThresholdCpuMean float64 // CPU
	ThresholdCpuVar  float64 // CPU
	Weight           float64 //
}

func (s *SystemParams) Normalize(node *structs.NodeState, net *structs.NetState) (float64, float64) {

	var CpuMeanNormalization, CpuVarNormalization float64

	isAboveThresholdCpuMean := func(x float64) bool {
		return x > s.ThresholdCpuMean
	}(node.CpuMean)

	if isAboveThresholdCpuMean {

		rank := 1
		for _, cpu := range net.AboveThresholdCpuMeans {
			if cpu < node.CpuMean {
				rank++
			}
		}

		lenAbove := len(net.AboveThresholdCpuMeans)
		if lenAbove > 1 {
			CpuMeanNormalization = float64(rank-1) / float64(lenAbove-1)
		} else {
			CpuMeanNormalization = 0
		}
	} else {

		rank := 1
		for _, cpu := range net.BelowThresholdCpuMeans {
			if cpu < node.CpuMean {
				rank++
			}
		}

		lenBelow := len(net.BelowThresholdCpuMeans)
		if lenBelow > 1 {
			CpuMeanNormalization = -(1 - float64(rank-1)/float64(lenBelow-1))
		} else {
			CpuMeanNormalization = 0
		}
	}

	isAboveThresholdCpuVar := func(x float64) bool {
		return x > s.ThresholdCpuVar
	}(node.CpuVar)

	if isAboveThresholdCpuVar {

		rank := 1
		for _, cpu := range net.AboveThresholdCpuVars {
			if cpu < node.CpuVar {
				rank++
			}
		}

		lenAbove := len(net.AboveThresholdCpuVars)
		if lenAbove > 1 {
			CpuVarNormalization = float64(rank-1) / float64(lenAbove-1)
		} else {
			CpuVarNormalization = 0
		}
	} else {

		rank := 1
		for _, cpu := range net.BelowThresholdCpuVars {
			if cpu < node.CpuVar {
				rank++
			}
		}

		lenBelow := len(net.BelowThresholdCpuVars)
		if lenBelow > 1 {
			CpuVarNormalization = -(1 - float64(rank-1)/float64(lenBelow-1))
		} else {
			CpuVarNormalization = 0
		}
	}

	return CpuMeanNormalization, CpuVarNormalization
}

func (e *Evaluation) UpdateQMean() float64 {
	return math.Max(e.QMean+e.NormalCpuMean, 0)
}

func (e *Evaluation) UpdateQVar() float64 {
	return math.Max(e.QVar+e.NormalCpuVar, 0)
}

func (e *Evaluation) DriftPlusPenalty() float64 {
	delayPart := e.Params.Weight * e.Delay
	meanPart := e.QMean * e.NormalCpuMean
	varPart := e.QVar * e.NormalCpuVar

	return delayPart + meanPart + varPart
}
