package main

import (
	"net/http"
)

// API 版本控制中间件
func APIVersioningMiddleware(next http.Handler, whitelist map[string]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 跳过白名单路径
		if _, ok := whitelist[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}

		version := r.Header.Get("API-Version")
		if version == "" {
			version = "v1" // 默认版本
		}
		r.URL.Path = "/" + version + r.URL.Path // 根据版本修改路径
		next.ServeHTTP(w, r)
	})
}
