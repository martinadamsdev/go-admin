package main

import (
	"crypto/tls"
	"log"
	"net/http"
)

// 启用 SSL/TLS
func startTLSServer(handler http.Handler) {
	srv := &http.Server{
		Addr:    ":8443",
		Handler: handler,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// 可以配置更多的 TLS 选项，如证书等
		},
	}
	log.Fatal(srv.ListenAndServeTLS("server.crt", "server.key")) // 启动 HTTPS 服务器
}
