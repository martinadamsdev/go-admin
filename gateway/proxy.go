package main

import (
	"context"
	"io"
	"net/http"
	"time"
)

// ProxyMiddleware 代理中间件（整合负载均衡、熔断器、重试）
func ProxyMiddleware(lb LoadBalancer, breaker *CircuitBreaker, config BackendConfig, whitelist map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 检查是否在白名单中（白名单路径直接转发到 next）
			if whitelist[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// 获取请求 ID
			requestID, _ := r.Context().Value(RequestIDKey).(string)

			// 获取后端服务器
			backend := lb.NextBackend()
			if backend == nil {
				GetLogger().ErrorWithRequestID(requestID, "No available backend", map[string]interface{}{
					"path": r.URL.Path,
				})
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				return
			}

			// 使用熔断器执行请求
			err := breaker.Call(func() error {
				return proxyRequestWithRetry(w, r, backend, config, requestID)
			})

			if err != nil {
				if err == ErrCircuitOpen {
					GetLogger().WarnWithRequestID(requestID, "Circuit breaker open", map[string]interface{}{
						"backend": backend.URL.String(),
					})
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				} else if err == ErrTooManyRequests {
					GetLogger().WarnWithRequestID(requestID, "Too many requests to half-open circuit", map[string]interface{}{
						"backend": backend.URL.String(),
					})
					http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				}
				// 其他错误已经在 proxyRequestWithRetry 中处理
			}
		})
	}
}

// proxyRequestWithRetry 带重试的代理请求
func proxyRequestWithRetry(w http.ResponseWriter, r *http.Request, backend *Backend, config BackendConfig, requestID string) error {
	var lastErr error

	// 增加连接数
	backend.IncrementConnections()
	defer backend.DecrementConnections()

	// 重试逻辑
	for attempt := 0; attempt <= config.RetryAttempts; attempt++ {
		if attempt > 0 {
			// 重试延迟
			time.Sleep(config.RetryDelay * time.Duration(attempt))

			GetLogger().InfoWithRequestID(requestID, "Retrying request", map[string]interface{}{
				"attempt": attempt,
				"backend": backend.URL.String(),
			})
		}

		// 执行代理请求
		err := proxyRequest(w, r, backend, requestID)
		if err == nil {
			return nil
		}

		lastErr = err

		// 如果是客户端错误（4xx），不重试
		if isClientError(err) {
			break
		}
	}

	return lastErr
}

// proxyRequest 执行代理请求
func proxyRequest(w http.ResponseWriter, r *http.Request, backend *Backend, requestID string) error {
	// 创建超时上下文
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// 构建后端 URL
	targetURL := backend.URL.String() + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// 创建新请求
	proxyReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, r.Body)
	if err != nil {
		GetLogger().ErrorWithRequestID(requestID, "Failed to create proxy request", map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL.String(),
		})
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return err
	}

	// 复制请求头
	for key, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// 添加 X-Forwarded-* 头
	proxyReq.Header.Set("X-Forwarded-For", getClientIP(r))
	proxyReq.Header.Set("X-Forwarded-Proto", r.URL.Scheme)
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	proxyReq.Header.Set("X-Request-ID", requestID)

	// 发送请求
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		GetLogger().ErrorWithRequestID(requestID, "Proxy request failed", map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL.String(),
		})
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return err
	}
	defer resp.Body.Close()

	// 复制响应头
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// 设置状态码
	w.WriteHeader(resp.StatusCode)

	// 复制响应体
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		GetLogger().ErrorWithRequestID(requestID, "Failed to copy response body", map[string]interface{}{
			"error":   err.Error(),
			"backend": backend.URL.String(),
		})
		return err
	}

	GetLogger().InfoWithRequestID(requestID, "Proxy request succeeded", map[string]interface{}{
		"backend":     backend.URL.String(),
		"status_code": resp.StatusCode,
	})

	return nil
}

// isClientError 判断是否为客户端错误
func isClientError(err error) bool {
	// 这里可以根据错误类型判断
	// 简单起见，我们假设所有错误都可以重试
	return false
}
