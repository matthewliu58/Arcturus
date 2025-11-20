package metrics_evaluating

import (
	log "github.com/sirupsen/logrus"
	pb "scheduling/controller/metrics_processing/protocol"
	"scheduling/data_compression"
)

type Analyzer struct{}

func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

func (a *Analyzer) AnalyzeOutliersAndNormalizeValues(ipPairs []*pb.IPPairAssessment) ([]*pb.IPPairAssessment, error) {

	if len(ipPairs) < 5 {
		return ipPairs, nil
	}

	values := make([]float64, len(ipPairs))
	for i, pair := range ipPairs {
		values[i] = float64(pair.Assessment)
	}

	k := 3
	outliers := data_compression.DetectOutliersAdaptive(values, k, 1.5)

	outlierIndices := make(map[int]bool)
	for _, outlier := range outliers {
		outlierIndices[outlier.Index] = true
	}

	var sum float64 = 0
	var count int = 0
	for i, value := range values {
		if !outlierIndices[i] {
			sum += value
			count++
		}
	}

	if count == 0 || len(outlierIndices) == 0 {
		return ipPairs, nil
	}

	var mean float64
	if count > 0 {
		mean = sum / float64(count)
	}

	var result []*pb.IPPairAssessment

	for i, pair := range ipPairs {
		if outlierIndices[i] {

			result = append(result, pair)
		}
	}

	if count > 0 {

		defaultPair := &pb.IPPairAssessment{
			Ip1:        "default",
			Ip2:        "default",
			Assessment: float32(mean),
		}

		result = append(result, defaultPair)
	}

	log.Infof("AnalyzeOutliersAndNormalizeValues, lenipPairs=%d, lenresult=%d, lenoutlierIndices=%d, mean=%.2f",
		len(ipPairs), len(result), len(outlierIndices), mean)

	return result, nil
}
