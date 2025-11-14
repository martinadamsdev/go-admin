package main

import (
	"sync"
	"time"
)

// RateLimiter 限流器接口
type RateLimiter interface {
	Allow(key string) bool
	Cleanup()
}

// TokenBucketLimiter 令牌桶限流器
type TokenBucketLimiter struct {
	rate       float64           // 每秒生成的令牌数
	burst      int               // 桶容量
	perIP      bool              // 是否按 IP 限流
	buckets    map[string]*bucket
	mu         sync.RWMutex
	stopCleanup chan struct{}
}

type bucket struct {
	tokens    float64
	lastCheck time.Time
	mu        sync.Mutex
}

// NewRateLimiter 创建限流器
func NewRateLimiter(config RateLimitConfig) *TokenBucketLimiter {
	if !config.Enabled {
		return nil
	}

	limiter := &TokenBucketLimiter{
		rate:        float64(config.RequestsPerSecond),
		burst:       config.BurstSize,
		perIP:       config.PerIP,
		buckets:     make(map[string]*bucket),
		stopCleanup: make(chan struct{}),
	}

	// 启动清理协程
	go limiter.cleanupRoutine(config.CleanupInterval)

	return limiter
}

// Allow 检查是否允许请求
func (rl *TokenBucketLimiter) Allow(key string) bool {
	if rl == nil {
		return true
	}

	// 如果不是按 IP 限流，使用全局限流
	if !rl.perIP {
		key = "global"
	}

	// 获取或创建桶
	rl.mu.RLock()
	b, exists := rl.buckets[key]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		// 双重检查
		b, exists = rl.buckets[key]
		if !exists {
			b = &bucket{
				tokens:    float64(rl.burst),
				lastCheck: time.Now(),
			}
			rl.buckets[key] = b
		}
		rl.mu.Unlock()
	}

	// 令牌桶算法
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastCheck).Seconds()

	// 添加新令牌
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastCheck = now

	// 检查是否有可用令牌
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}

	return false
}

// Cleanup 手动清理
func (rl *TokenBucketLimiter) Cleanup() {
	if rl == nil {
		return
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, b := range rl.buckets {
		b.mu.Lock()
		// 删除超过 5 分钟未使用的桶
		if now.Sub(b.lastCheck) > 5*time.Minute {
			delete(rl.buckets, key)
		}
		b.mu.Unlock()
	}
}

// cleanupRoutine 定期清理协程
func (rl *TokenBucketLimiter) cleanupRoutine(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.Cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

// Stop 停止限流器
func (rl *TokenBucketLimiter) Stop() {
	if rl != nil {
		close(rl.stopCleanup)
	}
}
