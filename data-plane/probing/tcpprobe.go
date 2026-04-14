package probing

import (
	"context"
	"data-plane/util"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"
)

type Result struct {
	Target   ProbeTask     `json:"target"`
	Attempts int           `json:"attempts"`
	Failures int           `json:"failures"`
	LossRate float64       `json:"loss_rate"`
	AvgRTT   time.Duration `json:"avg_rtt"`
}

type Config struct {
	Concurrency int
	Timeout     time.Duration
	Interval    time.Duration
	Attempts    int
	BufferSize  int
}

var (
	mu            sync.RWMutex
	latestResults = make(map[string]Result)
)

func updateLatestResults(results []Result) {
	mu.Lock()
	defer mu.Unlock()
	for _, r := range results {
		latestResults[r.Target.IP] = r
	}
}

func GetLatestResults() map[string]Result {
	mu.RLock()
	defer mu.RUnlock()

	copied := make(map[string]Result, len(latestResults))
	for k, v := range latestResults {
		copied[k] = v
	}
	return copied
}

func StartProbePeriodically(ctx context.Context, controlHost string, cfg Config, pre string, logger *slog.Logger) {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.Attempts <= 0 {
		cfg.Attempts = 5
	}

	logger.Info("StartProbePeriodically", slog.String("pre", pre))

	go func() {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("periodic probe stopped", slog.String("pre", pre))
				return
			default:
			}

			pre_ := util.GenerateRandomLetters(5)

			targets, err := GetProbeTasks(controlHost, pre_)
			if err != nil {
				logger.Error("get probe task failed", slog.Any("err", err))
				time.Sleep(time.Second)
				continue
			}
			logger.Info("get probing tasks", slog.String("pre", pre_), slog.Any("targets", targets))

			doProbeLossRTT(targets, cfg, pre_, logger)

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func doProbeLossRTT(targets []ProbeTask, cfg Config, pre string, logger *slog.Logger) {
	jobs := make(chan ProbeTask)
	var wg sync.WaitGroup
	roundResults := make([]Result, 0, len(targets))
	var roundMu sync.Mutex

	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range jobs {
				failures := 0
				var totalRTT time.Duration
				successes := 0

				dialer := net.Dialer{
					Timeout: cfg.Timeout,
				}

				for a := 0; a < cfg.Attempts; a++ {
					start := time.Now()
					conn, err := dialer.Dial("tcp", target.IP+":"+strconv.Itoa(target.Port))
					rtt := time.Since(start)

					if err != nil {
						var err_ net.Error
						if errors.As(err, &err_) && err_.Timeout() {
							failures++
							continue
						}

						successes++
						totalRTT += rtt
						continue
					}

					successes++
					totalRTT += rtt
					conn.Close()
				}

				avgRTT := time.Duration(0)
				if successes > 0 {
					avgRTT = totalRTT / time.Duration(successes)
				}

				result := Result{
					Target:   target,
					Attempts: cfg.Attempts,
					Failures: failures,
					LossRate: float64(failures) / float64(cfg.Attempts),
					AvgRTT:   avgRTT,
				}

				logger.Info("probe result", slog.String("pre", pre),
					slog.String("ip", result.Target.IP),
					slog.Int("port", result.Target.Port),
					slog.String("provider", result.Target.Provider),
					slog.String("target_type", result.Target.TargetType),
					slog.Any("result", result),
				)

				roundMu.Lock()
				roundResults = append(roundResults, result)
				roundMu.Unlock()
			}
		}()
	}

	go func() {
		for _, t := range targets {
			jobs <- t
		}
		close(jobs)
	}()

	wg.Wait()

	updateLatestResults(roundResults)
}
