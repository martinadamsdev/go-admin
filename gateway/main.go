package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

var healthy int32

func main() {
	port := "8081"
	log.SetPrefix("[Gateway] ")

	mux := http.NewServeMux()

	// 路由
	mux.HandleFunc("/api/v1/resource", ResourceHandler)
	mux.HandleFunc("/health", HealthCheckHandler)

	// 定义白名单
	whitelist := map[string]bool{
		"/api/v1/resource": true,
		"/health":          true,
		// 可以在这里添加其他不需要认证的路径
	}

	// 中间件
	handler := LoggingMiddleware(mux)
	handler = AuthenticationMiddleware(handler, whitelist)
	handler = APIVersioningMiddleware(handler, whitelist)
	handler = RateLimitMiddleware(handler)
	handler = CacheMiddleware(handler)
	handler = LoadBalancerMiddleware(handler, whitelist)

	srv := &http.Server{
		Handler:      handler,
		Addr:         "0.0.0.0:" + port,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	// 运行服务器
	go func() {
		atomic.StoreInt32(&healthy, 1)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Could not listen on %s: %v\n", port, err)
		}
	}()
	log.Println("API Gateway is running on port " + port)

	// 捕获信号
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	<-stop

	log.Println("Shutting down gracefully...")

	atomic.StoreInt32(&healthy, 0)

	// 创建上下文，设置5秒超时
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 平滑关闭服务器
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")

	// 重启服务器
	go func() {
		log.Println("Restarting server...")
		main()
	}()
}
