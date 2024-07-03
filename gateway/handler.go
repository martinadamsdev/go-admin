package main

import (
	"encoding/json"
	"net/http"
)

type Response struct {
	Message string `json:"message"`
}

// 资源处理函数
func ResourceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet { // 只允许 GET 方法
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := Response{Message: "Hello, this is your resource!"} // 响应内容
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response) // 返回 JSON 响应
}
