package metrics_evaluating

import (
	"context"
	"database/sql"
	"fmt"
	log "github.com/sirupsen/logrus"
	"runtime"
	pb "scheduling/controller/metrics_processing/protocol"
	"scheduling/db_models"
	"scheduling/goroutine_pool"
	linkevaluate "scheduling/link_evaluating"
	"scheduling/structs"
	"sync"
	"time"
)

type Calculator struct {
	db           *sql.DB
	mutex        sync.Mutex
	lastCalcTime time.Time
	interval     time.Duration
	assessments  []*pb.RegionPairAssessment
	assessmutex  sync.RWMutex
}

func NewAssessmentCalculator(db *sql.DB, interval time.Duration) *Calculator {
	return &Calculator{
		db:          db,
		interval:    interval,
		assessments: make([]*pb.RegionPairAssessment, 0),
	}
}

func (ac *Calculator) CalculateAssessmentsIfNeeded() bool {
	log.Infof("AssessmentCalculator: CalculateAssessmentsIfNeeded called.")

	ac.mutex.Lock()
	defer ac.mutex.Unlock()

	if time.Since(ac.lastCalcTime) < ac.interval {
		return false //
	}

	startTime := time.Now()
	ac.lastCalcTime = time.Now()

	regionAssessments, err := ac.calculateRegionAssessments()
	if err != nil {
		log.Errorf("calculateRegionAssessments faield, err=%v", err)
		return false
	}

	ac.assessmutex.Lock()
	ac.assessments = regionAssessments
	ac.assessmutex.Unlock()

	elapsed := time.Since(startTime)
	log.Infof("AssessmentCalculator: Assessments calculated and cached. Count: %d. Elapsed: %v", len(ac.assessments), elapsed)
	return true
}

func (ac *Calculator) GetCachedAssessments() []*pb.RegionPairAssessment {
	ac.assessmutex.RLock()
	defer ac.assessmutex.RUnlock()

	result := make([]*pb.RegionPairAssessment, len(ac.assessments))
	copy(result, ac.assessments)
	return result
}

func (ac *Calculator) calculateRegionAssessments() ([]*pb.RegionPairAssessment, error) {

	regions, err := db_models.GetAllRegions(ac.db)
	if err != nil {
		return nil, err
	}

	netState, err := ac.getCurrentNetState()
	if err != nil {
		return nil, err
	}

	var assessments []*pb.RegionPairAssessment
	var assessmentsMutex sync.Mutex
	var wg sync.WaitGroup

	getIPPoolName := func(region1, region2 string) string {
		return fmt.Sprintf("IP_TASKS_POOL_%s_%s", region1, region2)
	}

	regionTaskFunc := func(payload interface{}) {
		defer wg.Done()

		task := payload.(*RegionPairTask)
		region1 := task.region1
		region2 := task.region2
		log.Infof("RegionTaskFunc: Processing region pair %s -> %s", region1, region2)

		ipPoolName := getIPPoolName(region1, region2)

		assessment := &pb.RegionPairAssessment{
			Region1: region1,
			Region2: region2,
			IpPairs: []*pb.IPPairAssessment{},
		}

		sourceIPs, err := db_models.GetRegionIPs(ac.db, region1)
		if err != nil {
			log.Errorf("RegionTaskFunc: Error getting source IPs for %s: %v", region1, err)
			return
		}
		log.Infof("RegionTaskFunc: Source IPs for %s: %v", region1, sourceIPs)

		targetIPs, err := db_models.GetRegionIPs(ac.db, region2)
		if err != nil {
			log.Errorf("RegionTaskFunc: Error getting target IPs for %s: %v", region2, err)
			return
		}
		log.Infof("RegionTaskFunc: Target IPs for %s: %v", region2, targetIPs)

		var ipPairs []*pb.IPPairAssessment
		var ipPairsMutex sync.Mutex
		var ipWg sync.WaitGroup

		ipTaskFunc := func(payload interface{}) {
			defer ipWg.Done()

			ipTask := payload.(*IpPairTask)
			sourceIP := ipTask.sourceIP
			targetIP := ipTask.targetIP

			if sourceIP == targetIP {
				log.Infof("IpTaskFunc: Skipping self-metrics_evaluating for IP %s -> %s", sourceIP, targetIP)
				return
			}

			result := linkevaluate.CalculateLinkWeight(ac.db, sourceIP, targetIP, netState)
			log.Infof("IpTaskFunc: Processing IP pair %s -> %s", sourceIP, targetIP)
			log.Infof("IpTaskFunc: LinkWeight for %s -> %s is %.2f", sourceIP, targetIP, result.Value)
			if result.Value > 0 {
				ipPair := &pb.IPPairAssessment{
					Ip1:        sourceIP,
					Ip2:        targetIP,
					Assessment: float32(result.Value),
				}

				ipPairsMutex.Lock()
				ipPairs = append(ipPairs, ipPair)
				ipPairsMutex.Unlock()
			}
		}

		ipPoolSize := runtime.NumCPU()
		goroutine_pool.InitPool(ipPoolName, ipPoolSize, ipTaskFunc)
		defer goroutine_pool.ReleasePool(ipPoolName)

		safeSubmitTask := func(task *IpPairTask) {
			p := goroutine_pool.GetPool(ipPoolName)
			if p == nil {
				log.Infof("GetPool, ipPoolName=%s", ipPoolName)
				ipWg.Done()
				return
			}

			if err := p.Invoke(task); err != nil {
				log.Errorf("Invoke failed, err=%v", err)
				ipWg.Done()
			}
		}

		for _, sourceIP := range sourceIPs {
			for _, targetIP := range targetIPs {
				ipWg.Add(1)

				ipTask := &IpPairTask{
					sourceIP: sourceIP,
					targetIP: targetIP,
				}

				safeSubmitTask(ipTask)
			}
		}

		ipWg.Wait()
		log.Infof("RegionTaskFunc: Finished IP pair metrics_tasks for %s -> %s. Raw ipPairs count: %d", region1, region2, len(ipPairs))

		if len(ipPairs) > 0 {
			analyzer := NewAnalyzer()
			log.Infof("RegionTaskFunc: Calling AnalyzeOutliersAndNormalizeValues for %s -> %s with %d ipPairs.", region1, region2, len(ipPairs))
			processedPairs, err := analyzer.AnalyzeOutliersAndNormalizeValues(ipPairs)
			if err != nil {
				log.Infof("RegionTaskFunc: Error from AnalyzeOutliersAndNormalizeValues for %s->%s : %v. Using raw ipPairs.", region1, region2, err)
				assessment.IpPairs = ipPairs
			} else {
				log.Infof("RegionTaskFunc: Processed IP pairs for %s -> %s. Count: %d", region1, region2, len(processedPairs))
				assessment.IpPairs = processedPairs
			}

			assessmentsMutex.Lock()
			assessments = append(assessments, assessment)
			assessmentsMutex.Unlock()
		} else {
			log.Infof("RegionTaskFunc: No valid IP pairs generated for %s -> %s after link weight calculation.", region1, region2)
		}
	}

	poolSize := runtime.NumCPU() * 2
	goroutine_pool.InitPool(goroutine_pool.RegionTasksPool, poolSize, regionTaskFunc)
	defer goroutine_pool.ReleasePool(goroutine_pool.RegionTasksPool)

	safeSubmitRegionTask := func(task *RegionPairTask) {
		p := goroutine_pool.GetPool(goroutine_pool.RegionTasksPool)
		if p == nil {
			log.Infof("GetPool success")
			wg.Done()
			return
		}

		if err := p.Invoke(task); err != nil {
			log.Errorf("Invoke failed, err=%v", err)
			wg.Done()
		}
	}

	regionPairCount := 0

	for _, region1 := range regions {
		for _, region2 := range regions {

			regionPairCount++
			wg.Add(1)

			task := &RegionPairTask{
				region1: region1,
				region2: region2,
			}

			safeSubmitRegionTask(task)
		}
	}

	log.Infof("calculateRegionAssessments, regionPairCount=%d", regionPairCount)

	wg.Wait()

	log.Infof("calculateRegionAssessments, lenassessments=%d", len(assessments))
	return assessments, nil
}

func (ac *Calculator) getCurrentNetState() (structs.NetState, error) {

	aboveCpuMeans, belowCpuMeans, aboveCpuVars, belowCpuVars, err := db_models.GetCPUPerformanceList(
		ac.db, linkevaluate.ThresholdCpuMean, linkevaluate.ThresholdCpuVar)

	if err != nil {
		return structs.NetState{}, err
	}

	return structs.NetState{
		AboveThresholdCpuMeans: aboveCpuMeans,
		AboveThresholdCpuVars:  aboveCpuVars,
		BelowThresholdCpuMeans: belowCpuMeans,
		BelowThresholdCpuVars:  belowCpuVars,
	}, nil
}

func (ac *Calculator) StartAssessmentCalculator(ctx context.Context) {
	initialDelay := 10 * time.Second
	log.Infof("AssessmentCalculator: Goroutine started")
	initialTimer := time.NewTimer(initialDelay)

	select {
	case <-ctx.Done():
		initialTimer.Stop()
		return
	case <-initialTimer.C:
		log.Infof("AssessmentCalculator: Initial delay passed, performing first calculation.")
		if ac.CalculateAssessmentsIfNeeded() {
			log.Infof("CalculateAssessmentsIfNeeded done")
		}
	}

	ticker := time.NewTicker(ac.interval / 4)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ac.CalculateAssessmentsIfNeeded() {
				log.Infof("CalculateAssessmentsIfNeeded done")
			}
		}
	}
}
