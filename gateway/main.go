package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	healthy int32
	cfg     *Config
	logger  *Logger
)

func init() {
	// 加载配置
	cfg = LoadConfig()

	// 初始化日志
	logger = InitLogger(cfg.Logging)

	// 初始化指标
	InitMetrics()
}

func main() {
	logger.Info("Starting API Gateway", map[string]interface{}{
		"version": "2.0.0",
		"port":    cfg.Server.Port,
	})

	// 创建路由
	mux := http.NewServeMux()

	// 注册路由
	mux.HandleFunc("/api/v1/resource", ResourceHandler)
	mux.HandleFunc("/health", HealthCheckHandler)

	// 定义路径白名单（这些路径不需要代理到后端）
	pathWhitelist := map[string]bool{
		"/api/v1/resource": true,
		"/health":          true,
		"/metrics":         true,
	}

	// 创建限流器
	rateLimiter := NewRateLimiter(cfg.RateLimit)
	if rateLimiter != nil {
		defer rateLimiter.Stop()
	}

	// 创建缓存
	cache := NewCache(cfg.Cache)
	if cache != nil {
		defer cache.Stop()
	}

	// 创建熔断器
	circuitBreaker := NewCircuitBreaker(cfg.CircuitBreaker)

	// 创建负载均衡器和后端列表
	loadBalancer, backends := NewLoadBalancer(cfg.Backend, cfg.Backend.LoadBalanceStrategy)

	// 启动健康检查
	healthChecker := NewHealthChecker(backends, loadBalancer, cfg.Backend)
	go healthChecker.Start()
	defer healthChecker.Stop()

	// 构建中间件链（注意顺序很重要！）
	handler := buildMiddlewareChain(
		mux,
		rateLimiter,
		cache,
		circuitBreaker,
		loadBalancer,
		pathWhitelist,
	)

	// 创建 HTTP 服务器
	srv := &http.Server{
		Handler:        handler,
		Addr:           cfg.Server.Host + ":" + cfg.Server.Port,
		ReadTimeout:    cfg.Server.ReadTimeout,
		WriteTimeout:   cfg.Server.WriteTimeout,
		IdleTimeout:    cfg.Server.IdleTimeout,
		MaxHeaderBytes: cfg.Server.MaxHeaderBytes,
	}

	// 启动指标服务器
	StartMetricsServer(cfg.Metrics)

	// 启动 HTTP 服务器
	go func() {
		atomic.StoreInt32(&healthy, 1)

		logger.Info("HTTP server started", map[string]interface{}{
			"address": srv.Addr,
		})

		var err error
		if cfg.Server.EnableTLS {
			err = srv.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile)
		} else {
			err = srv.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			logger.Error("Server error", map[string]interface{}{
				"error": err.Error(),
			})
			os.Exit(1)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit

	logger.Info("Shutting down server gracefully...", nil)

	// 标记为不健康
	atomic.StoreInt32(&healthy, 0)

	// 创建关闭超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	// 优雅关闭服务器
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", map[string]interface{}{
			"error": err.Error(),
		})
	}

	logger.Info("Server stopped", nil)
}

// buildMiddlewareChain 构建中间件链
func buildMiddlewareChain(
	handler http.Handler,
	rateLimiter *TokenBucketLimiter,
	cache *LRUCache,
	circuitBreaker *CircuitBreaker,
	loadBalancer LoadBalancer,
	pathWhitelist map[string]bool,
) http.Handler {
	// 中间件执行顺序（从外到内）：
	// 1. Recovery - 捕获 panic
	// 2. RequestID - 生成请求 ID
	// 3. Logging - 记录日志
	// 4. Metrics - 收集指标
	// 5. SecurityHeaders - 设置安全头
	// 6. CORS - 处理跨域
	// 7. IPFilter - IP 过滤
	// 8. RequestSizeLimit - 请求大小限制
	// 9. Timeout - 超时控制
	// 10. Compression - 压缩
	// 11. RateLimit - 限流
	// 12. Authentication - 认证
	// 13. Cache - 缓存
	// 14. Proxy - 代理（负载均衡 + 熔断 + 重试）
	// 15. Handler - 最终处理器

	// 从内到外包装中间件
	h := handler

	// 14. 代理中间件（只对非白名单路径生效）
	h = ProxyMiddleware(loadBalancer, circuitBreaker, cfg.Backend, pathWhitelist)(h)

	// 13. 缓存中间件
	h = CacheMiddlewareNew(cache, pathWhitelist)(h)

	// 12. 认证中间件
	h = AuthenticationMiddlewareNew(cfg.Security, pathWhitelist)(h)

	// 11. 限流中间件
	h = RateLimitMiddlewareNew(rateLimiter, pathWhitelist)(h)

	// 10. 压缩中间件
	h = CompressionMiddleware(h)

	// 9. 超时中间件
	h = TimeoutMiddleware(30 * time.Second)(h)

	// 8. 请求大小限制中间件
	h = RequestSizeLimitMiddleware(cfg.Security.MaxRequestSize)(h)

	// 7. IP 过滤中间件
	h = IPFilterMiddleware(cfg.Security)(h)

	// 6. CORS 中间件
	h = CORSMiddleware(cfg.Security)(h)

	// 5. 安全头中间件
	h = SecurityHeadersMiddleware(h)

	// 4. 指标中间件
	h = MetricsMiddleware(h)

	// 3. 日志中间件
	h = LoggingMiddlewareNew(logger)(h)

	// 2. 请求 ID 中间件
	h = RequestIDMiddleware(h)

	// 1. 恢复中间件（最外层）
	h = RecoveryMiddleware(h)

	return h
}

// printBanner 打印启动横幅
func printBanner() {
	banner := `
  ____                       _      _           _
 / ___| ___     / \   __  __| |_ _| |__  _   _(_)_ __
| |  _ / _ \   / _ \  \ \/ /| __/ _ '_ \| | | | | '_ \
| |_| | (_) | / ___ \  >  < | ||  __/ | | | |_| | | | |
 \____|\___/ /_/   \_\/_/\_\ \__\___|_| |_|\__,_|_|_| |_|

High-Performance API Gateway v2.0.0
`
	fmt.Println(banner)
}

// WarnWithRequestID 日志辅助方法
func (l *Logger) WarnWithRequestID(requestID, message string, fields map[string]interface{}) {
	if l.level <= WARN {
		l.log(WARN, message, requestID, fields)
	}
}

// DebugWithRequestID 日志辅助方法
func (l *Logger) DebugWithRequestID(requestID, message string, fields map[string]interface{}) {
	if l.level <= DEBUG {
		l.log(DEBUG, message, requestID, fields)
	}
}
