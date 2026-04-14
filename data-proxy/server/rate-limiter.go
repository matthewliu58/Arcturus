package server

import (
	"data-proxy/config"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type IPLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type PortLimiters struct {
	limiters map[string]*IPLimiter
	mu       sync.RWMutex
	config   config.RateLimit
	stopCh   chan struct{}
}

type GlobalRateLimiter struct {
	portLimiters map[int]*PortLimiters
	mu           sync.RWMutex
	config       config.RateLimit
}

var globalRL *GlobalRateLimiter

func InitRateLimiter(cfg config.RateLimit) {
	globalRL = &GlobalRateLimiter{
		portLimiters: make(map[int]*PortLimiters),
		config:       cfg,
	}
}

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

func (p *PortLimiters) cleanupLoop() {
	ticker := time.NewTicker(time.Duration(p.config.CleanInterval) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			now := time.Now()
			for ip, il := range p.limiters {
				if now.Sub(il.lastSeen) > time.Duration(p.config.CleanInterval)*time.Minute {
					delete(p.limiters, ip)
				}
			}
			p.mu.Unlock()
		case <-p.stopCh:
			return
		}
	}
}

func (g *GlobalRateLimiter) Allow(port int, ip string) bool {
	limiter := g.GetLimiter(port, ip)
	return limiter.Allow()
}

func (g *GlobalRateLimiter) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, pl := range g.portLimiters {
		close(pl.stopCh)
	}
}
