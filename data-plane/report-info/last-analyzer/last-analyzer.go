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
	Ts        time.Time
	ClientIP  string
	ConnRT    int
	Continent string
	Country   string // 国家
	Province  string // 省份
	City      string // 城市
	ISP       string // 运营商
	//Node     string // 当前节点
}

type LastStatsKey struct {
	Continent string `json:"continent"`
	Country   string `json:"country"`
	Province  string `json:"province"`
	City      string `json:"city"`
	ISP       string `json:"isp"`
}

// LastStatsValue 时延统计值
type LastStatsValue struct {
	Count int     `json:"count"`
	SumRT float64 `json:"sum_rt"`
	AvgRT float64 `json:"avg_rt"`
	P95RT int     `json:"p95_rt"`
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
		delayStats := calculate(pre, logger)
		SendLastStats(delayStats, pre, logger)
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

func calculate(pre string, logger *slog.Logger) map[LastStatsKey]*LastStatsValue {
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

	agg := make(map[LastStatsKey]*LastStatsValue) // 改成存指针！
	rts := make(map[LastStatsKey][]int)

	for _, r := range valid {
		key := LastStatsKey{
			Continent: util.GetContinentByCountry(r.Country),
			Country:   r.Country,
			Province:  r.Province,
			City:      r.City,
			ISP:       r.ISP,
		}

		// 不存在就初始化
		if agg[key] == nil {
			agg[key] = &LastStatsValue{}
		}

		s := agg[key] // 取指针，直接修改原数据
		s.Count++
		s.SumRT += float64(r.ConnRT)
		rts[key] = append(rts[key], r.ConnRT)
	}

	// 计算平均 & P95
	for k, s := range agg {
		// 平均
		s.AvgRT = float64(s.SumRT) / float64(s.Count)

		// P95 正确计算
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
