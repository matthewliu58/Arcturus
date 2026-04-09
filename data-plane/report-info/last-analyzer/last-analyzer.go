package last_analyzer

import (
	"bufio"
	"data-plane/util"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
)

// 日志记录结构 —— 只保留你要的：国家、省份、城市、ISP、节点
type Record struct {
	Ts       time.Time
	ClientIP string
	ConnRT   int
	Country  string // 国家
	Province string // 省份
	City     string // 城市
	ISP      string // 运营商
	//Node     string // 当前节点
}

// 调度统计 KEY —— 省份保留，无 Slot
type Key struct {
	Country  string
	Province string
	City     string
	ISP      string
	//Node     string
}

type Stats struct {
	Count int
	SumRT int
	AvgRT float64
	P95RT int
}

var (
	mu           sync.RWMutex
	records      []Record
	window       = 30 * time.Second
	tickInterval = 30 * time.Second
)

func AccessAnalyzer(pre string, logger *slog.Logger) {
	go tailFile(util.Config_.AccessLog, pre, logger)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for range ticker.C {
		calculate(pre, logger)
	}
}

func tailFile(path string, pre string, logger *slog.Logger) {
	openFile := func() *os.File {
		for {
			f, err := os.Open(path)
			if err != nil {
				logger.Error("Open file failed", slog.String("pre", pre), slog.Any("err", err))
				time.Sleep(1 * time.Second)
				continue
			}
			_, err = f.Seek(0, os.SEEK_END)
			if err != nil {
				f.Close()
				continue
			}
			return f
		}
	}

	f := openFile()
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1<<20)
	scanner.Buffer(buf, 1<<20)

	for {
		if scanner.Scan() {
			line := scanner.Text()

			tsStr := extract(line, `time=(\S+)`)
			ip := extract(line, `client_ip=(\S+)`)
			rtStr := extract(line, `conn_rt_ms=(\d+)`)

			if tsStr == "" || ip == "" || rtStr == "" {
				continue
			}

			ts, err := time.Parse(time.RFC3339Nano, tsStr)
			if err != nil {
				continue
			}

			if time.Since(ts) > window {
				continue
			}

			rt, _ := strconv.Atoi(rtStr)
			ipInfo, err := util.GetIPInfo(ip, pre, logger)
			if err != nil {
				continue
			}

			mu.Lock()
			records = append(records, Record{
				Ts:       ts,
				ClientIP: ip,
				ConnRT:   rt,
				Country:  ipInfo.Country,
				Province: ipInfo.Province, // 省份保留
				City:     ipInfo.City,
				ISP:      ipInfo.ISP,
				//Node:     util.Config_.NodeName, // 节点名
			})
			mu.Unlock()

		} else {
			logger.Error("log rotated, reconnecting", slog.String("pre", pre))
			f.Close()
			time.Sleep(500 * time.Millisecond)
			f = openFile()
			scanner = bufio.NewScanner(f)
			scanner.Buffer(buf, 1<<20)
		}
	}
}

func calculate(pre string, logger *slog.Logger) map[Key]*Stats {
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()

	valid := make([]Record, 0, len(records))
	for _, r := range records {
		if now.Sub(r.Ts) <= window {
			valid = append(valid, r)
		}
	}
	records = valid

	// 聚合维度：国家 + 省份 + ISP + 节点
	agg := make(map[Key]*Stats)
	var rts []int
	for _, r := range valid {
		key := Key{
			Country:  r.Country,
			Province: r.Province,
			City:     r.City,
			ISP:      r.ISP,
			//Node:     r.Node,
		}
		if agg[key] == nil {
			agg[key] = &Stats{}
		}
		s := agg[key]
		s.Count++
		s.SumRT += r.ConnRT
		rts = append(rts, r.ConnRT)
	}

	// 计算平均 & P95
	for _, s := range agg {
		s.AvgRT = float64(s.SumRT) / float64(s.Count)
		sort.Ints(rts)
		p95Idx := int(float64(len(rts)) * 0.95)
		if p95Idx >= len(rts) {
			p95Idx = len(rts) - 1
		}
		s.P95RT = rts[p95Idx]
	}

	logger.Info("调度统计完成", slog.String("pre", pre), slog.Int("有效记录数", len(valid)))
	for key, s := range agg {
		logger.Info("用户时延统计",
			slog.String("pre", pre),
			"国家", key.Country,
			"省份", key.Province,
			"city", key.City,
			"运营商", key.ISP,
			//"节点", key.Node,
			"平均RT(ms)", s.AvgRT,
			"P95RT(ms)", s.P95RT,
			"请求数", s.Count,
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
