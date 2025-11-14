package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics 指标收集器
type Metrics struct {
	// 请求计数
	TotalRequests   uint64
	SuccessRequests uint64
	ErrorRequests   uint64

	// 按状态码分类
	StatusCodes map[int]uint64
	mu          sync.RWMutex

	// 延迟统计
	TotalLatency   uint64 // 纳秒
	RequestLatency []time.Duration
	latencyMu      sync.Mutex

	// 后端状态
	BackendStatus map[string]bool
	backendMu     sync.RWMutex

	// 限流统计
	RateLimitedRequests uint64

	// 缓存统计
	CacheHits   uint64
	CacheMisses uint64
}

var globalMetrics *Metrics

// InitMetrics 初始化指标收集器
func InitMetrics() *Metrics {
	globalMetrics = &Metrics{
		StatusCodes:    make(map[int]uint64),
		BackendStatus:  make(map[string]bool),
		RequestLatency: make([]time.Duration, 0, 1000),
	}
	return globalMetrics
}

// GetMetrics 获取全局指标收集器
func GetMetrics() *Metrics {
	if globalMetrics == nil {
		globalMetrics = InitMetrics()
	}
	return globalMetrics
}

// RecordRequest 记录请求
func (m *Metrics) RecordRequest() {
	atomic.AddUint64(&m.TotalRequests, 1)
}

// RecordSuccess 记录成功请求
func (m *Metrics) RecordSuccess() {
	atomic.AddUint64(&m.SuccessRequests, 1)
}

// RecordError 记录错误请求
func (m *Metrics) RecordError() {
	atomic.AddUint64(&m.ErrorRequests, 1)
}

// RecordStatusCode 记录状态码
func (m *Metrics) RecordStatusCode(code int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StatusCodes[code]++
}

// RecordLatency 记录延迟
func (m *Metrics) RecordLatency(duration time.Duration) {
	atomic.AddUint64(&m.TotalLatency, uint64(duration.Nanoseconds()))

	m.latencyMu.Lock()
	defer m.latencyMu.Unlock()

	// 保留最近 1000 个请求的延迟数据
	if len(m.RequestLatency) >= 1000 {
		m.RequestLatency = m.RequestLatency[1:]
	}
	m.RequestLatency = append(m.RequestLatency, duration)
}

// RecordRateLimited 记录被限流的请求
func (m *Metrics) RecordRateLimited() {
	atomic.AddUint64(&m.RateLimitedRequests, 1)
}

// RecordCacheHit 记录缓存命中
func (m *Metrics) RecordCacheHit() {
	atomic.AddUint64(&m.CacheHits, 1)
}

// RecordCacheMiss 记录缓存未命中
func (m *Metrics) RecordCacheMiss() {
	atomic.AddUint64(&m.CacheMisses, 1)
}

// UpdateBackendStatus 更新后端状态
func (m *Metrics) UpdateBackendStatus(backend string, alive bool) {
	m.backendMu.Lock()
	defer m.backendMu.Unlock()
	m.BackendStatus[backend] = alive
}

// GetStats 获取统计数据
func (m *Metrics) GetStats() map[string]interface{} {
	m.mu.RLock()
	m.backendMu.RLock()
	m.latencyMu.Lock()

	defer m.mu.RUnlock()
	defer m.backendMu.RUnlock()
	defer m.latencyMu.Unlock()

	totalRequests := atomic.LoadUint64(&m.TotalRequests)
	avgLatency := float64(0)
	if totalRequests > 0 {
		avgLatency = float64(atomic.LoadUint64(&m.TotalLatency)) / float64(totalRequests) / 1e6 // 转换为毫秒
	}

	// 计算 P95 延迟
	p95Latency := float64(0)
	if len(m.RequestLatency) > 0 {
		idx := int(float64(len(m.RequestLatency)) * 0.95)
		if idx >= len(m.RequestLatency) {
			idx = len(m.RequestLatency) - 1
		}
		p95Latency = float64(m.RequestLatency[idx].Milliseconds())
	}

	// 计算错误率
	errorRate := float64(0)
	if totalRequests > 0 {
		errorRate = float64(atomic.LoadUint64(&m.ErrorRequests)) / float64(totalRequests) * 100
	}

	// 计算缓存命中率
	cacheHitRate := float64(0)
	cacheHits := atomic.LoadUint64(&m.CacheHits)
	cacheMisses := atomic.LoadUint64(&m.CacheMisses)
	if cacheHits+cacheMisses > 0 {
		cacheHitRate = float64(cacheHits) / float64(cacheHits+cacheMisses) * 100
	}

	// 复制 map 避免锁竞争
	statusCodes := make(map[int]uint64)
	for k, v := range m.StatusCodes {
		statusCodes[k] = v
	}

	backendStatus := make(map[string]bool)
	for k, v := range m.BackendStatus {
		backendStatus[k] = v
	}

	return map[string]interface{}{
		"total_requests":        totalRequests,
		"success_requests":      atomic.LoadUint64(&m.SuccessRequests),
		"error_requests":        atomic.LoadUint64(&m.ErrorRequests),
		"error_rate":            errorRate,
		"rate_limited_requests": atomic.LoadUint64(&m.RateLimitedRequests),
		"avg_latency_ms":        avgLatency,
		"p95_latency_ms":        p95Latency,
		"status_codes":          statusCodes,
		"cache_hits":            cacheHits,
		"cache_misses":          cacheMisses,
		"cache_hit_rate":        cacheHitRate,
		"backend_status":        backendStatus,
	}
}

// MetricsHandler 指标端点处理器
func MetricsHandler(w http.ResponseWriter, r *http.Request) {
	stats := GetMetrics().GetStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// StartMetricsServer 启动指标服务器
func StartMetricsServer(config MetricsConfig) {
	if !config.Enabled {
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc(config.Path, MetricsHandler)

	server := &http.Server{
		Addr:    ":" + config.Port,
		Handler: mux,
	}

	go func() {
		GetLogger().Info("Metrics server started", map[string]interface{}{
			"port": config.Port,
			"path": config.Path,
		})

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			GetLogger().Error("Metrics server error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()
}
