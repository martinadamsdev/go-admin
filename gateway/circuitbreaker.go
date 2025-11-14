package main

import (
	"errors"
	"sync"
	"time"
)

// CircuitState 熔断器状态
type CircuitState int

const (
	StateClosed   CircuitState = iota // 关闭状态（正常）
	StateOpen                          // 打开状态（熔断）
	StateHalfOpen                      // 半开状态（尝试恢复）
)

var (
	ErrCircuitOpen     = errors.New("circuit breaker is open")
	ErrTooManyRequests = errors.New("too many requests in half-open state")
)

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	config       CircuitBreakerConfig
	state        CircuitState
	failures     int
	lastFailTime time.Time
	requests     int
	mu           sync.RWMutex
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if !config.Enabled {
		return nil
	}

	return &CircuitBreaker{
		config: config,
		state:  StateClosed,
	}
}

// Call 执行函数调用
func (cb *CircuitBreaker) Call(fn func() error) error {
	if cb == nil {
		return fn()
	}

	// 检查是否可以执行
	if err := cb.beforeRequest(); err != nil {
		return err
	}

	// 执行函数
	err := fn()

	// 记录结果
	cb.afterRequest(err)

	return err
}

// beforeRequest 请求前检查
func (cb *CircuitBreaker) beforeRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		// 正常状态，允许请求
		return nil

	case StateOpen:
		// 检查是否应该切换到半开状态
		if time.Since(cb.lastFailTime) > cb.config.Timeout {
			cb.state = StateHalfOpen
			cb.requests = 0
			return nil
		}
		return ErrCircuitOpen

	case StateHalfOpen:
		// 半开状态，限制请求数量
		if cb.requests >= cb.config.MaxRequests {
			return ErrTooManyRequests
		}
		cb.requests++
		return nil

	default:
		return ErrCircuitOpen
	}
}

// afterRequest 请求后记录
func (cb *CircuitBreaker) afterRequest(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.onFailure()
	} else {
		cb.onSuccess()
	}
}

// onSuccess 成功处理
func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateClosed:
		// 正常状态，重置失败计数
		cb.failures = 0

	case StateHalfOpen:
		// 半开状态，如果成功则切换到关闭状态
		cb.state = StateClosed
		cb.failures = 0
		cb.requests = 0
	}
}

// onFailure 失败处理
func (cb *CircuitBreaker) onFailure() {
	cb.failures++
	cb.lastFailTime = time.Now()

	switch cb.state {
	case StateClosed:
		// 正常状态，检查是否达到阈值
		if cb.failures >= cb.config.Threshold {
			cb.state = StateOpen
			GetLogger().Warn("Circuit breaker opened", map[string]interface{}{
				"failures":  cb.failures,
				"threshold": cb.config.Threshold,
			})
		}

	case StateHalfOpen:
		// 半开状态，失败则切换回打开状态
		cb.state = StateOpen
		cb.requests = 0
		GetLogger().Warn("Circuit breaker re-opened from half-open state", map[string]interface{}{
			"failures": cb.failures,
		})
	}
}

// State 获取当前状态
func (cb *CircuitBreaker) State() CircuitState {
	if cb == nil {
		return StateClosed
	}

	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return cb.state
}

// Reset 重置熔断器
func (cb *CircuitBreaker) Reset() {
	if cb == nil {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failures = 0
	cb.requests = 0
}
