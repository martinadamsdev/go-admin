package main

import (
	"math/rand"
	"net/http"
)

// 简单的负载均衡中间件
func LoadBalancerMiddleware(next http.Handler, whitelist map[string]bool) http.Handler {
	backends := []string{"http://backend1", "http://backend2", "http://backend3"}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 跳过白名单路径
		if _, ok := whitelist[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}

		// 负载均衡处理
		backend := backends[rand.Intn(len(backends))] // 随机选择后端
		proxyReq, err := http.NewRequest(r.Method, backend+r.URL.Path, r.Body)
		if err != nil {
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
			return
		}
		proxyReq.Header = r.Header
		resp, err := http.DefaultClient.Do(proxyReq)
		if err != nil {
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		w.Write([]byte("Proxied to " + backend)) // 简单响应
	})
}
