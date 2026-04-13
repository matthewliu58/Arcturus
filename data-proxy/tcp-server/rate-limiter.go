package tcp_server

import (
	"data-proxy/config"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IP 限流器
type IPLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// port 维度的限流器管理
type PortLimiters struct {
	limiters map[string]*IPLimiter
	mu       sync.RWMutex
	config   config.RateLimitConfig
	stopCh   chan struct{}
}

// 全局限流器管理
type GlobalRateLimiter struct {
	portLimiters map[int]*PortLimiters
	mu           sync.RWMutex
	config       config.RateLimitConfig
}

var globalRL *GlobalRateLimiter

// InitRateLimiter 初始化全局限流器
func InitRateLimiter(cfg config.RateLimitConfig) {
	globalRL = &GlobalRateLimiter{
		portLimiters: make(map[int]*PortLimiters),
		config:       cfg,
	}
}

// GetLimiter 获取 IP 限流器
func (g *GlobalRateLimiter) GetLimiter(port int, ip string) *rate.Limiter {
	g.mu.Lock()
	defer g.mu.Unlock()

	pl, exists := g.portLimiters[port]
	if !exists {
		pl = &PortLimiters{
			limiters: make(map[string]*IPLimiter),
			config:   g.config,
			stopCh:   make(chan struct{}),
		}
		g.portLimiters[port] = pl
		go pl.cleanupLoop()
	}

	pl.mu.Lock()
	defer pl.mu.Unlock()

	il, exists := pl.limiters[ip]
	if !exists {
		il = &IPLimiter{
			limiter:  rate.NewLimiter(rate.Limit(g.config.QPS), g.config.Burst),
			lastSeen: time.Now(),
		}
		pl.limiters[ip] = il
	}
	il.lastSeen = time.Now()

	return il.limiter
}

// cleanupLoop 定期清理过期的 limiter
func (p *PortLimiters) cleanupLoop() {
	ticker := time.NewTicker(time.Duration(p.config.CleanInt) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			now := time.Now()
			for ip, il := range p.limiters {
				if now.Sub(il.lastSeen) > time.Duration(p.config.CleanInt)*time.Minute {
					delete(p.limiters, ip)
				}
			}
			p.mu.Unlock()
		case <-p.stopCh:
			return
		}
	}
}

// Allow 检查是否允许请求
func (g *GlobalRateLimiter) Allow(port int, ip string) bool {
	limiter := g.GetLimiter(port, ip)
	return limiter.Allow()
}

// Stop 停止限流器
func (g *GlobalRateLimiter) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, pl := range g.portLimiters {
		close(pl.stopCh)
	}
}
