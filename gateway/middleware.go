package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// RequestIDKey 请求 ID 的 context key
type contextKey string

const RequestIDKey contextKey = "request_id"

// ResponseWriter 包装的响应写入器
type ResponseWriter struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func NewResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

func (rw *ResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *ResponseWriter) Write(b []byte) (int, error) {
	rw.body.Write(b)
	return rw.ResponseWriter.Write(b)
}

func (rw *ResponseWriter) StatusCode() int {
	return rw.statusCode
}

func (rw *ResponseWriter) Body() []byte {
	return rw.body.Bytes()
}

// RequestIDMiddleware 请求 ID 中间件
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查请求头中是否已有 Request ID
		requestID := r.Header.Get("X-Request-ID")

		// 如果没有，生成新的 Request ID
		if requestID == "" {
			requestID = generateRequestID()
		}

		// 将 Request ID 添加到 context
		ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
		r = r.WithContext(ctx)

		// 将 Request ID 添加到响应头
		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r)
	})
}

// generateRequestID 生成唯一的请求 ID
func generateRequestID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// LoggingMiddlewareNew 改进的日志中间件
func LoggingMiddlewareNew(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// 获取 Request ID
			requestID := r.Context().Value(RequestIDKey).(string)

			// 包装 ResponseWriter 以捕获状态码
			rw := NewResponseWriter(w)

			// 记录请求开始
			logger.InfoWithRequestID(requestID, "Request started", map[string]interface{}{
				"method":     r.Method,
				"path":       r.URL.Path,
				"remote_ip":  getClientIP(r),
				"user_agent": r.UserAgent(),
			})

			// 处理请求
			next.ServeHTTP(rw, r)

			// 记录请求完成
			duration := time.Since(start)
			GetMetrics().RecordLatency(duration)
			GetMetrics().RecordStatusCode(rw.StatusCode())

			logger.InfoWithRequestID(requestID, "Request completed", map[string]interface{}{
				"method":      r.Method,
				"path":        r.URL.Path,
				"status_code": rw.StatusCode(),
				"duration_ms": duration.Milliseconds(),
				"remote_ip":   getClientIP(r),
			})
		})
	}
}

// MetricsMiddleware 指标收集中间件
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		GetMetrics().RecordRequest()

		rw := NewResponseWriter(w)
		next.ServeHTTP(rw, r)

		if rw.StatusCode() >= 200 && rw.StatusCode() < 400 {
			GetMetrics().RecordSuccess()
		} else {
			GetMetrics().RecordError()
		}
	})
}

// AuthenticationMiddlewareNew 改进的认证中间件
func AuthenticationMiddlewareNew(config SecurityConfig, whitelist map[string]bool) func(http.Handler) http.Handler {
	// 将 API keys 转换为 map 以加速查找
	apiKeyMap := make(map[string]bool)
	for _, key := range config.APIKeys {
		apiKeyMap[key] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 检查是否在白名单中
			if whitelist[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// 获取 API Key
			apiKey := r.Header.Get("X-API-Key")

			// 验证 API Key
			if apiKey == "" || !apiKeyMap[apiKey] {
				requestID := r.Context().Value(RequestIDKey).(string)
				GetLogger().WarnWithRequestID(requestID, "Authentication failed", map[string]interface{}{
					"path":      r.URL.Path,
					"remote_ip": getClientIP(r),
				})

				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitMiddlewareNew 改进的限流中间件
func RateLimitMiddlewareNew(limiter *TokenBucketLimiter, whitelist map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 检查是否在白名单中
			if whitelist[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// 获取客户端 IP
			clientIP := getClientIP(r)

			// 检查限流
			if limiter != nil && !limiter.Allow(clientIP) {
				GetMetrics().RecordRateLimited()

				requestID := r.Context().Value(RequestIDKey).(string)
				GetLogger().WarnWithRequestID(requestID, "Rate limit exceeded", map[string]interface{}{
					"remote_ip": clientIP,
				})

				w.Header().Set("X-RateLimit-Limit", "100")
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("Retry-After", "1")
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CacheMiddlewareNew 改进的缓存中间件
func CacheMiddlewareNew(cache *LRUCache, whitelist map[string]bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 只缓存 GET 请求
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			// 检查是否在白名单中（白名单中的路径可以被缓存）
			if !whitelist[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// 生成缓存键
			cacheKey := r.Method + ":" + r.URL.String()

			// 检查缓存
			if cache != nil {
				if data, found := cache.Get(cacheKey); found {
					GetMetrics().RecordCacheHit()

					requestID := r.Context().Value(RequestIDKey).(string)
					GetLogger().DebugWithRequestID(requestID, "Cache hit", map[string]interface{}{
						"cache_key": cacheKey,
					})

					w.Header().Set("X-Cache", "HIT")
					w.Write(data)
					return
				}

				GetMetrics().RecordCacheMiss()
			}

			// 缓存未命中，执行请求
			rw := NewResponseWriter(w)
			next.ServeHTTP(rw, r)

			// 缓存响应（只缓存成功响应）
			if cache != nil && rw.StatusCode() == http.StatusOK {
				cache.Set(cacheKey, rw.Body())
			}

			rw.Header().Set("X-Cache", "MISS")
		})
	}
}

// CORSMiddleware CORS 中间件
func CORSMiddleware(config SecurityConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !config.EnableCORS {
				next.ServeHTTP(w, r)
				return
			}

			// 设置 CORS 头
			origin := r.Header.Get("Origin")
			if origin != "" {
				// 检查是否在允许的源列表中
				allowed := false
				for _, allowedOrigin := range config.AllowedOrigins {
					if allowedOrigin == "*" || allowedOrigin == origin {
						allowed = true
						break
					}
				}

				if allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
					w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Max-Age", "3600")
				}
			}

			// 处理 OPTIONS 预检请求
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeadersMiddleware 安全头中间件
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 设置安全头
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		next.ServeHTTP(w, r)
	})
}

// IPFilterMiddleware IP 过滤中间件
func IPFilterMiddleware(config SecurityConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := getClientIP(r)

			// 检查黑名单
			for _, blockedIP := range config.IPBlacklist {
				if clientIP == blockedIP {
					requestID := r.Context().Value(RequestIDKey).(string)
					GetLogger().WarnWithRequestID(requestID, "IP blocked", map[string]interface{}{
						"remote_ip": clientIP,
					})

					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}

			// 检查白名单（如果配置了白名单，只允许白名单中的 IP）
			if len(config.IPWhitelist) > 0 {
				allowed := false
				for _, allowedIP := range config.IPWhitelist {
					if clientIP == allowedIP {
						allowed = true
						break
					}
				}

				if !allowed {
					requestID := r.Context().Value(RequestIDKey).(string)
					GetLogger().WarnWithRequestID(requestID, "IP not in whitelist", map[string]interface{}{
						"remote_ip": clientIP,
					})

					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestSizeLimitMiddleware 请求大小限制中间件
func RequestSizeLimitMiddleware(maxSize int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			next.ServeHTTP(w, r)
		})
	}
}

// CompressionMiddleware 压缩中间件
func CompressionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查客户端是否支持 gzip
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		// 创建 gzip writer
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()

		gzw := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next.ServeHTTP(gzw, r)
	})
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

// RecoveryMiddleware 恢复中间件（捕获 panic）
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				requestID := r.Context().Value(RequestIDKey).(string)
				GetLogger().ErrorWithRequestID(requestID, "Panic recovered", map[string]interface{}{
					"error": err,
					"path":  r.URL.Path,
				})

				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// TimeoutMiddleware 超时中间件
func TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			r = r.WithContext(ctx)

			done := make(chan struct{})
			go func() {
				next.ServeHTTP(w, r)
				close(done)
			}()

			select {
			case <-done:
				// 请求完成
			case <-ctx.Done():
				// 超时
				requestID := r.Context().Value(RequestIDKey).(string)
				GetLogger().WarnWithRequestID(requestID, "Request timeout", map[string]interface{}{
					"timeout": timeout.String(),
				})

				http.Error(w, "Request Timeout", http.StatusRequestTimeout)
			}
		})
	}
}

// getClientIP 获取客户端 IP
func getClientIP(r *http.Request) string {
	// 尝试从 X-Forwarded-For 头获取
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// 尝试从 X-Real-IP 头获取
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// 从 RemoteAddr 获取
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}
