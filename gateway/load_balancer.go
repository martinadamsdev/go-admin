package main

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// LoadBalancer 负载均衡器接口
type LoadBalancer interface {
	NextBackend() *Backend
	MarkBackendDown(backend *Backend)
	MarkBackendUp(backend *Backend)
}

// Backend 后端服务器
type Backend struct {
	URL          *url.URL
	Alive        bool
	mu           sync.RWMutex
	ReverseProxy *httputil.ReverseProxy
	Connections  int64 // 当前连接数（用于最小连接数策略）
}

// IsAlive 检查后端是否存活
func (b *Backend) IsAlive() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.Alive
}

// SetAlive 设置后端存活状态
func (b *Backend) SetAlive(alive bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Alive = alive
}

// IncrementConnections 增加连接数
func (b *Backend) IncrementConnections() {
	atomic.AddInt64(&b.Connections, 1)
}

// DecrementConnections 减少连接数
func (b *Backend) DecrementConnections() {
	atomic.AddInt64(&b.Connections, -1)
}

// GetConnections 获取连接数
func (b *Backend) GetConnections() int64 {
	return atomic.LoadInt64(&b.Connections)
}

// RoundRobinBalancer 轮询负载均衡器
type RoundRobinBalancer struct {
	backends []*Backend
	current  uint64
	mu       sync.RWMutex
}

// NewLoadBalancer 创建负载均衡器
func NewLoadBalancer(config BackendConfig, strategy string) (LoadBalancer, []*Backend) {
	var backends []*Backend

	for _, backendURL := range config.URLs {
		parsedURL, err := url.Parse(backendURL)
		if err != nil {
			GetLogger().Error("Failed to parse backend URL", map[string]interface{}{
				"url":   backendURL,
				"error": err.Error(),
			})
			continue
		}

		proxy := httputil.NewSingleHostReverseProxy(parsedURL)

		// 自定义错误处理
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			GetLogger().Error("Proxy error", map[string]interface{}{
				"backend": parsedURL.String(),
				"error":   err.Error(),
				"path":    r.URL.Path,
			})
			w.WriteHeader(http.StatusBadGateway)
		}

		backend := &Backend{
			URL:          parsedURL,
			Alive:        true,
			ReverseProxy: proxy,
		}

		backends = append(backends, backend)
	}

	var lb LoadBalancer

	switch strategy {
	case "round-robin":
		lb = &RoundRobinBalancer{
			backends: backends,
		}
	case "least-conn":
		lb = &LeastConnectionBalancer{
			backends: backends,
		}
	case "random":
		lb = &RandomBalancer{
			backends: backends,
		}
	default:
		lb = &RoundRobinBalancer{
			backends: backends,
		}
	}

	return lb, backends
}

// NextBackend 获取下一个后端（轮询）
func (rb *RoundRobinBalancer) NextBackend() *Backend {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if len(rb.backends) == 0 {
		return nil
	}

	// 找到下一个存活的后端
	start := atomic.AddUint64(&rb.current, 1) % uint64(len(rb.backends))

	for i := 0; i < len(rb.backends); i++ {
		idx := (start + uint64(i)) % uint64(len(rb.backends))
		backend := rb.backends[idx]

		if backend.IsAlive() {
			return backend
		}
	}

	return nil
}

// MarkBackendDown 标记后端为下线
func (rb *RoundRobinBalancer) MarkBackendDown(backend *Backend) {
	backend.SetAlive(false)
}

// MarkBackendUp 标记后端为上线
func (rb *RoundRobinBalancer) MarkBackendUp(backend *Backend) {
	backend.SetAlive(true)
}

// LeastConnectionBalancer 最小连接数负载均衡器
type LeastConnectionBalancer struct {
	backends []*Backend
	mu       sync.RWMutex
}

// NextBackend 获取连接数最少的后端
func (lb *LeastConnectionBalancer) NextBackend() *Backend {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if len(lb.backends) == 0 {
		return nil
	}

	var selected *Backend
	var minConns int64 = -1

	for _, backend := range lb.backends {
		if !backend.IsAlive() {
			continue
		}

		conns := backend.GetConnections()
		if minConns == -1 || conns < minConns {
			minConns = conns
			selected = backend
		}
	}

	return selected
}

// MarkBackendDown 标记后端为下线
func (lb *LeastConnectionBalancer) MarkBackendDown(backend *Backend) {
	backend.SetAlive(false)
}

// MarkBackendUp 标记后端为上线
func (lb *LeastConnectionBalancer) MarkBackendUp(backend *Backend) {
	backend.SetAlive(true)
}

// RandomBalancer 随机负载均衡器
type RandomBalancer struct {
	backends []*Backend
	mu       sync.RWMutex
}

// NextBackend 随机选择后端
func (rb *RandomBalancer) NextBackend() *Backend {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if len(rb.backends) == 0 {
		return nil
	}

	var aliveBackends []*Backend
	for _, backend := range rb.backends {
		if backend.IsAlive() {
			aliveBackends = append(aliveBackends, backend)
		}
	}

	if len(aliveBackends) == 0 {
		return nil
	}

	// 使用时间戳作为随机种子
	idx := time.Now().UnixNano() % int64(len(aliveBackends))
	return aliveBackends[idx]
}

// MarkBackendDown 标记后端为下线
func (rb *RandomBalancer) MarkBackendDown(backend *Backend) {
	backend.SetAlive(false)
}

// MarkBackendUp 标记后端为上线
func (rb *RandomBalancer) MarkBackendUp(backend *Backend) {
	backend.SetAlive(true)
}

// HealthChecker 健康检查器
type HealthChecker struct {
	backends []*Backend
	lb       LoadBalancer
	config   BackendConfig
	stopChan chan struct{}
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(backends []*Backend, lb LoadBalancer, config BackendConfig) *HealthChecker {
	return &HealthChecker{
		backends: backends,
		lb:       lb,
		config:   config,
		stopChan: make(chan struct{}),
	}
}

// Start 启动健康检查
func (hc *HealthChecker) Start() {
	ticker := time.NewTicker(hc.config.HealthCheckInterval)
	defer ticker.Stop()

	// 立即执行一次健康检查
	hc.checkAll()

	for {
		select {
		case <-ticker.C:
			hc.checkAll()
		case <-hc.stopChan:
			return
		}
	}
}

// Stop 停止健康检查
func (hc *HealthChecker) Stop() {
	close(hc.stopChan)
}

// checkAll 检查所有后端
func (hc *HealthChecker) checkAll() {
	for _, backend := range hc.backends {
		go hc.checkBackend(backend)
	}
}

// checkBackend 检查单个后端
func (hc *HealthChecker) checkBackend(backend *Backend) {
	ctx, cancel := context.WithTimeout(context.Background(), hc.config.HealthCheckTimeout)
	defer cancel()

	healthURL := backend.URL.String() + hc.config.HealthCheckPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		hc.lb.MarkBackendDown(backend)
		GetLogger().Warn("Health check failed to create request", map[string]interface{}{
			"backend": backend.URL.String(),
			"error":   err.Error(),
		})
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		wasAlive := backend.IsAlive()
		hc.lb.MarkBackendDown(backend)

		if wasAlive {
			GetLogger().Warn("Backend marked as down", map[string]interface{}{
				"backend": backend.URL.String(),
				"error":   err.Error(),
			})
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		wasDown := !backend.IsAlive()
		hc.lb.MarkBackendUp(backend)

		if wasDown {
			GetLogger().Info("Backend marked as up", map[string]interface{}{
				"backend": backend.URL.String(),
			})
		}
	} else {
		wasAlive := backend.IsAlive()
		hc.lb.MarkBackendDown(backend)

		if wasAlive {
			GetLogger().Warn("Backend health check failed", map[string]interface{}{
				"backend":     backend.URL.String(),
				"status_code": resp.StatusCode,
			})
		}
	}
}
