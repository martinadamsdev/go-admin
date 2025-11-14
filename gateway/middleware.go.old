package main

import (
	"log"
	"net/http"
	"time"
)

// 日志记录中间件
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()                                                                 // 记录请求开始时间
		next.ServeHTTP(w, r)                                                                // 处理请求
		log.Printf("Request: %s %s, Duration: %s", r.Method, r.URL.Path, time.Since(start)) // 打印日志
	})
}

// 认证中间件
func AuthenticationMiddleware(next http.Handler, whitelist map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查请求路径是否在白名单中
		if _, ok := whitelist[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}

		apiKey := r.Header.Get("X-API-Key") // 获取 API 密钥
		if apiKey != "secret-key" {         // 检查密钥是否匹配
			http.Error(w, "Forbidden", http.StatusForbidden) // 不匹配则返回 403 禁止访问
			return
		}
		next.ServeHTTP(w, r) // 密钥匹配则继续处理请求
	})
}

// 限流中间件
func RateLimitMiddleware(next http.Handler) http.Handler {
	limiter := time.Tick(time.Second / 5) // 每秒限制 5 个请求
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-limiter            // 等待令牌
		next.ServeHTTP(w, r) // 继续处理请求
	})
}

// 缓存中间件（简化示例）
func CacheMiddleware(next http.Handler) http.Handler {
	cache := make(map[string][]byte) // 简单的内存缓存
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if data, ok := cache[r.RequestURI]; ok { // 检查缓存
			w.Write(data) // 返回缓存内容
			return
		}
		rw := &responseWriter{ResponseWriter: w}
		next.ServeHTTP(rw, r)
		cache[r.RequestURI] = rw.body // 缓存响应内容
	})
}

type responseWriter struct {
	http.ResponseWriter
	body []byte
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.body = append(rw.body, b...) // 记录响应内容
	return rw.ResponseWriter.Write(b)
}
