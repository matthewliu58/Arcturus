package edge_domain

import (
	"bufio"
	"bytes"
	"data-plane/util"
	"log/slog"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
)

type Record struct {
	Ts        time.Time
	ClientIP  string
	ConnRT    int
	Continent string
	Country   string
	Province  string
	City      string
}

type LastKey struct {
	Continent string `json:"continent"`
	Country   string `json:"country"`
	City      string `json:"city"`
}

type LastCongestion struct {
	Count int     `json:"count"`
	SumRT float64 `json:"sum_rt"`
	AvgRT float64 `json:"avg_rt"`
	P95RT int     `json:"p95_rt"`
}

var (
	mu      sync.RWMutex
	records []Record
	window  = 10 * time.Second // 5s report interval + 5s buffer
)

func LastTelemetryReporter(pre string, logger *slog.Logger) {
	go tailFile(util.Config_.AccessLog, pre, logger)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		lastsCongestion := calculateLastCongestion(pre, logger)
		if len(lastsCongestion) > 0 {
			_ = SendLastTelemetry(lastsCongestion, pre, logger)
		} else {
			logger.Info("No valid telemetry data to report", slog.String("pre", pre))
		}
	}
}

func tailFile(path string, pre string, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		readLatestLogs(path, pre, logger)
	}
}

func readLatestLogs(path string, pre string, logger *slog.Logger) {
	f, err := os.Open(path)
	if err != nil {
		logger.Error("Open file failed", slog.String("pre", pre), slog.Any("err", err))
		return
	}
	defer f.Close()

	// Read only the last ~64KB to find recent logs
	const readSize = 64 * 1024
	fileInfo, err := f.Stat()
	if err != nil {
		logger.Error("Stat file failed", slog.String("pre", pre), slog.Any("err", err))
		return
	}

	var startPos int64 = 0
	if fileInfo.Size() > readSize {
		startPos = fileInfo.Size() - readSize
	}

	_, err = f.Seek(startPos, os.SEEK_SET)
	if err != nil {
		logger.Error("Seek file failed", slog.String("pre", pre), slog.Any("err", err))
		return
	}

	// Skip partial first line
	if startPos > 0 {
		buf := make([]byte, 1024)
		n, _ := f.Read(buf)
		if idx := bytes.Index(buf[:n], []byte("\n")); idx != -1 {
			_, _ = f.Seek(startPos+int64(idx)+1, os.SEEK_SET)
		}
	}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1<<20)
	scanner.Buffer(buf, 1<<20)

	for scanner.Scan() {
		line := scanner.Text()

		tsStr := extract(line, `time=(\S+)`)
		ip := extract(line, `client_ip=(\S+)`)
		rtStr := extract(line, `conn_rt_ms=([\d.]+)`)

		if tsStr == "" || ip == "" || rtStr == "" {
			continue
		}

		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			continue
		}

		rtMs, _ := strconv.ParseFloat(rtStr, 64)
		rt := int(math.Round(rtMs))
		ipInfo, err := util.GetIPInfo(ip, pre, logger)
		if err != nil {
			continue
		}

		rec := Record{
			Ts:       ts,
			ClientIP: ip,
			ConnRT:   rt,
			Country:  ipInfo.Country,
			Province: ipInfo.Province,
			City:     ipInfo.City,
		}

		mu.Lock()
		records = append(records, rec)
		mu.Unlock()
	}

	if err := scanner.Err(); err != nil {
		logger.Error("Read log file failed", slog.String("pre", pre), slog.Any("err", err))
	}
}

func calculateLastCongestion(pre string, logger *slog.Logger) map[string]*LastCongestion {
	now := time.Now()

	mu.Lock()
	// First pass: filter records within window
	valid := make([]Record, 0, len(records))
	for _, r := range records {
		if now.Sub(r.Ts) <= window {
			valid = append(valid, r)
		}
	}

	// If no data within window, use all records as fallback
	if len(valid) == 0 && len(records) > 0 {
		valid = records
		logger.Warn("No recent data, using all records as fallback", slog.String("pre", pre), slog.Int("count", len(records)))
	}
	records = valid

	mu.Unlock()

	agg := make(map[string]*LastCongestion)
	rts := make(map[string][]int)

	for _, r := range valid {
		key := r.City

		if agg[key] == nil {
			agg[key] = &LastCongestion{}
		}

		s := agg[key]
		s.Count++
		s.SumRT += float64(r.ConnRT)
		rts[key] = append(rts[key], r.ConnRT)
	}

	for k, s := range agg {

		s.AvgRT = s.SumRT / float64(s.Count)

		rtList := rts[k]
		if len(rtList) == 0 {
			continue
		}
		sort.Ints(rtList)
		p95Idx := int(float64(len(rtList)) * 0.95)
		if p95Idx >= len(rtList) {
			p95Idx = len(rtList) - 1
		}
		s.P95RT = rtList[p95Idx]
	}

	logger.Info("calculateLastCongestion completed", slog.String("pre", pre), slog.Int("valid_records", len(valid)))
	for key, s := range agg {
		logger.Info("User latency statistics",
			slog.String("pre", pre),
			slog.String("city", key),
			slog.Float64("avg_rt_ms", s.AvgRT),
			slog.Any("p95_rt_ms", s.P95RT),
			slog.Any("count", s.Count),
		)
	}
	return agg
}

func extract(line, pattern string) string {
	re := regexp.MustCompile(pattern)
	m := re.FindStringSubmatch(line)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}
