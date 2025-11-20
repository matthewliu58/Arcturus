package data_compression

import (
	"math"
	"sort"
)

type OutlierType int

const (
	NormalValue OutlierType = iota
	SmallOutlier
	LargeOutlier
)

type Outlier struct {
	Index  int
	Value  float64
	Type   OutlierType
	Score  float64
	IPAddr string
}

func DetectOutliersAdaptive(data []float64, k int, sensitivity float64) []Outlier {
	n := len(data)
	if n <= k {
		return []Outlier{} //
	}

	type indexedValue struct {
		index int
		value float64
	}

	indexedData := make([]indexedValue, n)
	for i, val := range data {
		indexedData[i] = indexedValue{i, val}
	}

	sort.Slice(indexedData, func(i, j int) bool {
		return indexedData[i].value < indexedData[j].value
	})

	sortedData := make([]float64, n)
	for i, pair := range indexedData {
		sortedData[i] = pair.value
	}

	sum := 0.0
	for _, val := range sortedData {
		sum += val
	}
	mean := sum / float64(n)

	sumSq := 0.0
	for _, val := range sortedData {
		diff := val - mean
		sumSq += diff * diff
	}
	stdDev := math.Sqrt(sumSq / float64(n))

	gaps := make([]float64, n-1)
	for i := 0; i < n-1; i++ {
		gaps[i] = sortedData[i+1] - sortedData[i]
	}

	sortedGaps := make([]float64, len(gaps))
	copy(sortedGaps, gaps)
	sort.Float64s(sortedGaps)

	gapMedian := 0.0
	if len(sortedGaps)%2 == 0 {
		gapMedian = (sortedGaps[len(sortedGaps)/2-1] + sortedGaps[len(sortedGaps)/2]) / 2
	} else {
		gapMedian = sortedGaps[len(sortedGaps)/2]
	}

	gapDeviations := make([]float64, len(gaps))
	for i, gap := range gaps {
		gapDeviations[i] = math.Abs(gap - gapMedian)
	}
	sort.Float64s(gapDeviations)

	gapMAD := 0.0
	if len(gapDeviations)%2 == 0 {
		gapMAD = (gapDeviations[len(gapDeviations)/2-1] + gapDeviations[len(gapDeviations)/2]) / 2
	} else {
		gapMAD = gapDeviations[len(gapDeviations)/2]
	}

	if gapMAD < 0.0001 {

		gapMAD = math.Max(0.0001, stdDev*0.6745)
	}

	significantGapThreshold := gapMedian + 2.5*gapMAD/0.6745

	significantGapPositions := []int{}
	for i, gap := range gaps {
		if gap > significantGapThreshold {
			significantGapPositions = append(significantGapPositions, i)
		}
	}

	sort.Ints(significantGapPositions)

	clusters := [][]int{}
	if len(significantGapPositions) > 0 {
		startIdx := 0
		for _, gapPos := range significantGapPositions {

			cluster := []int{}
			for i := startIdx; i <= gapPos; i++ {
				cluster = append(cluster, i)
			}
			if len(cluster) > 0 {
				clusters = append(clusters, cluster)
			}
			startIdx = gapPos + 1
		}

		if startIdx < n {
			lastCluster := []int{}
			for i := startIdx; i < n; i++ {
				lastCluster = append(lastCluster, i)
			}
			if len(lastCluster) > 0 {
				clusters = append(clusters, lastCluster)
			}
		}
	} else {

		allCluster := []int{}
		for i := 0; i < n; i++ {
			allCluster = append(allCluster, i)
		}
		clusters = append(clusters, allCluster)
	}

	mainClusterIdx := 0
	mainClusterScore := -1.0

	for i, cluster := range clusters {
		if len(cluster) == 0 {
			continue
		}

		clusterValues := []float64{}
		for _, idx := range cluster {
			clusterValues = append(clusterValues, sortedData[idx])
		}

		clusterMean := 0.0
		for _, v := range clusterValues {
			clusterMean += v
		}
		clusterMean /= float64(len(clusterValues))

		sizeScore := float64(len(cluster)) / float64(n)

		positionScore := 1.0 - math.Abs((clusterMean-mean)/mean)
		positionScore = math.Max(0, math.Min(1, positionScore))

		clusterRange := 0.0
		if len(cluster) > 1 {
			clusterRange = sortedData[cluster[len(cluster)-1]] - sortedData[cluster[0]]
		}

		densityScore := 0.0
		if clusterRange > 0 {

			overallDensity := float64(n) / (sortedData[n-1] - sortedData[0] + 0.0001)
			clusterDensity := float64(len(cluster)) / (clusterRange + 0.0001)
			densityScore = math.Min(clusterDensity/overallDensity, 2.0)
		}

		totalScore := sizeScore*0.6 + positionScore*0.25 + densityScore*0.15

		if totalScore > mainClusterScore {
			mainClusterScore = totalScore
			mainClusterIdx = i
		}
	}

	if mainClusterIdx >= len(clusters) || len(clusters[mainClusterIdx]) == 0 {
		mainClusterIdx = 0
		clusters = [][]int{}
		allCluster := []int{}
		for i := 0; i < n; i++ {
			allCluster = append(allCluster, i)
		}
		clusters = append(clusters, allCluster)
	}

	mainClusterValues := []float64{}
	mainClusterIndices := make(map[int]bool)
	for _, idx := range clusters[mainClusterIdx] {
		mainClusterValues = append(mainClusterValues, sortedData[idx])
		mainClusterIndices[idx] = true
	}

	mainClusterSum := 0.0
	for _, val := range mainClusterValues {
		mainClusterSum += val
	}
	mainClusterMean := mainClusterSum / float64(len(mainClusterValues))

	mainClusterSumSq := 0.0
	for _, val := range mainClusterValues {
		diff := val - mainClusterMean
		mainClusterSumSq += diff * diff
	}
	mainClusterStdDev := math.Sqrt(mainClusterSumSq / float64(len(mainClusterValues)))

	mainClusterSorted := make([]float64, len(mainClusterValues))
	copy(mainClusterSorted, mainClusterValues)
	sort.Float64s(mainClusterSorted)

	mainClusterQ1Pos := int(float64(len(mainClusterSorted)) * 0.25)
	mainClusterQ3Pos := int(float64(len(mainClusterSorted)) * 0.75)
	if mainClusterQ1Pos >= len(mainClusterSorted) {
		mainClusterQ1Pos = len(mainClusterSorted) - 1
	}
	if mainClusterQ3Pos >= len(mainClusterSorted) {
		mainClusterQ3Pos = len(mainClusterSorted) - 1
	}

	mainClusterQ1 := mainClusterSorted[mainClusterQ1Pos]
	mainClusterQ3 := mainClusterSorted[mainClusterQ3Pos]
	mainClusterIQR := mainClusterQ3 - mainClusterQ1

	smallOutlierThresholdByStdDev := mainClusterMean - 2.0*mainClusterStdDev
	largeOutlierThresholdByStdDev := mainClusterMean + 2.0*mainClusterStdDev

	smallOutlierThresholdByIQR := mainClusterQ1 - 1.5*mainClusterIQR
	largeOutlierThresholdByIQR := mainClusterQ3 + 1.5*mainClusterIQR

	smallOutlierThreshold := math.Min(smallOutlierThresholdByStdDev, smallOutlierThresholdByIQR)

	largeValueAdjustment := 1.7 // 1.5
	largeOutlierAdjustedThreshold := mainClusterMean + largeValueAdjustment*mainClusterStdDev

	largeOutlierThreshold := math.Max(largeOutlierThresholdByStdDev,
		math.Max(largeOutlierThresholdByIQR, largeOutlierAdjustedThreshold))

	mainClusterMinVal := mainClusterSorted[0]
	mainClusterMaxVal := mainClusterSorted[len(mainClusterSorted)-1]
	mainClusterRange := mainClusterMaxVal - mainClusterMinVal

	outliers := []Outlier{}
	processedIndices := make(map[int]bool)

	for i, idxVal := range indexedData {
		if processedIndices[idxVal.index] {
			continue
		}

		val := idxVal.value
		idx := idxVal.index

		var score float64 = 0
		var outlierType OutlierType
		var isOutlier bool = false

		isInMainCluster := mainClusterIndices[i]

		if val >= 100 && val > mainClusterMean*1.8 {
			relativeVal := val / mainClusterMean
			score = math.Log10(relativeVal) * 3.0
			outlierType = LargeOutlier
			isOutlier = true
		} else if val < smallOutlierThreshold {

			deviation := (smallOutlierThreshold - val) / mainClusterStdDev

			score = math.Max(1.0, math.Pow(deviation, 1.2))
			outlierType = SmallOutlier
			isOutlier = true
		} else if val > largeOutlierThreshold {

			relativeVal := val / mainClusterMean
			deviation := (val - largeOutlierThreshold) / mainClusterStdDev

			if relativeVal < 1.5 && deviation < 0.8 {
				isOutlier = false
			} else {
				score = math.Max(1.0, math.Pow(deviation, 1.3))

				if val > mainClusterMaxVal+mainClusterRange*0.5 {
					score *= 1.8
				}

				outlierType = LargeOutlier
				isOutlier = true
			}
		}

		if !isOutlier {
			continue
		}

		if i > 0 {
			prevGap := sortedData[i] - sortedData[i-1]
			if prevGap > significantGapThreshold && outlierType == LargeOutlier {
				score *= 1.3
			}
		}
		if i < n-1 {
			nextGap := sortedData[i+1] - sortedData[i]
			if nextGap > significantGapThreshold && outlierType == SmallOutlier {
				score *= 1.3
			}
		}

		if !isInMainCluster {
			score *= 1.2
		}

		if score < 1.0 {
			score = 1.0
		}

		outliers = append(outliers, Outlier{
			Index: idx,
			Value: val,
			Type:  outlierType,
			Score: score,
		})

		processedIndices[idx] = true
	}

	sort.Slice(outliers, func(i, j int) bool {
		if outliers[i].Type != outliers[j].Type {
			return outliers[i].Type == SmallOutlier
		}
		return outliers[i].Score > outliers[j].Score
	})

	return outliers
}
