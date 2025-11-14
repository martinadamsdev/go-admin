package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config 网关配置
type Config struct {
	// 服务器配置
	Server ServerConfig

	// 安全配置
	Security SecurityConfig

	// 中间件配置
	RateLimit   RateLimitConfig
	Cache       CacheConfig
	CircuitBreaker CircuitBreakerConfig

	// 后端配置
	Backend BackendConfig

	// 可观测性配置
	Logging LoggingConfig
	Metrics MetricsConfig
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port            string
	Host            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	MaxHeaderBytes  int
	EnableTLS       bool
	CertFile        string
	KeyFile         string
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	APIKeys         []string
	EnableCORS      bool
	AllowedOrigins  []string
	AllowedMethods  []string
	AllowedHeaders  []string
	IPWhitelist     []string
	IPBlacklist     []string
	MaxRequestSize  int64
}

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	Enabled       bool
	RequestsPerSecond int
	BurstSize     int
	PerIP         bool
	CleanupInterval time.Duration
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled     bool
	MaxSize     int
	TTL         time.Duration
	CleanupInterval time.Duration
}

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	Enabled          bool
	Threshold        int       // 失败次数阈值
	Timeout          time.Duration // 熔断超时时间
	MaxRequests      int       // 半开状态最大请求数
}

// BackendConfig 后端配置
type BackendConfig struct {
	URLs            []string
	HealthCheckInterval time.Duration
	HealthCheckTimeout  time.Duration
	HealthCheckPath     string
	LoadBalanceStrategy string // "round-robin", "weighted", "least-conn", "random"
	MaxIdleConns        int
	MaxConnsPerHost     int
	IdleConnTimeout     time.Duration
	RetryAttempts       int
	RetryDelay          time.Duration
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level      string // "debug", "info", "warn", "error"
	Format     string // "json", "text"
	Output     string // "stdout", "stderr", "file"
	FilePath   string
	MaxSize    int // MB
	MaxBackups int
	MaxAge     int // days
}

// MetricsConfig 指标配置
type MetricsConfig struct {
	Enabled bool
	Port    string
	Path    string
}

// LoadConfig 加载配置
func LoadConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Port:            getEnv("SERVER_PORT", "8081"),
			Host:            getEnv("SERVER_HOST", "0.0.0.0"),
			ReadTimeout:     getDurationEnv("SERVER_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getDurationEnv("SERVER_WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:     getDurationEnv("SERVER_IDLE_TIMEOUT", 60*time.Second),
			ShutdownTimeout: getDurationEnv("SERVER_SHUTDOWN_TIMEOUT", 5*time.Second),
			MaxHeaderBytes:  getIntEnv("SERVER_MAX_HEADER_BYTES", 1<<20), // 1MB
			EnableTLS:       getBoolEnv("SERVER_ENABLE_TLS", false),
			CertFile:        getEnv("SERVER_CERT_FILE", "server.crt"),
			KeyFile:         getEnv("SERVER_KEY_FILE", "server.key"),
		},
		Security: SecurityConfig{
			APIKeys:        getSliceEnv("SECURITY_API_KEYS", []string{"default-api-key"}),
			EnableCORS:     getBoolEnv("SECURITY_ENABLE_CORS", true),
			AllowedOrigins: getSliceEnv("SECURITY_ALLOWED_ORIGINS", []string{"*"}),
			AllowedMethods: getSliceEnv("SECURITY_ALLOWED_METHODS", []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}),
			AllowedHeaders: getSliceEnv("SECURITY_ALLOWED_HEADERS", []string{"Content-Type", "Authorization", "X-API-Key", "X-Request-ID"}),
			IPWhitelist:    getSliceEnv("SECURITY_IP_WHITELIST", []string{}),
			IPBlacklist:    getSliceEnv("SECURITY_IP_BLACKLIST", []string{}),
			MaxRequestSize: getInt64Env("SECURITY_MAX_REQUEST_SIZE", 10<<20), // 10MB
		},
		RateLimit: RateLimitConfig{
			Enabled:         getBoolEnv("RATELIMIT_ENABLED", true),
			RequestsPerSecond: getIntEnv("RATELIMIT_REQUESTS_PER_SECOND", 100),
			BurstSize:       getIntEnv("RATELIMIT_BURST_SIZE", 50),
			PerIP:           getBoolEnv("RATELIMIT_PER_IP", true),
			CleanupInterval: getDurationEnv("RATELIMIT_CLEANUP_INTERVAL", 1*time.Minute),
		},
		Cache: CacheConfig{
			Enabled:         getBoolEnv("CACHE_ENABLED", true),
			MaxSize:         getIntEnv("CACHE_MAX_SIZE", 1000),
			TTL:             getDurationEnv("CACHE_TTL", 5*time.Minute),
			CleanupInterval: getDurationEnv("CACHE_CLEANUP_INTERVAL", 1*time.Minute),
		},
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:     getBoolEnv("CIRCUIT_BREAKER_ENABLED", true),
			Threshold:   getIntEnv("CIRCUIT_BREAKER_THRESHOLD", 5),
			Timeout:     getDurationEnv("CIRCUIT_BREAKER_TIMEOUT", 60*time.Second),
			MaxRequests: getIntEnv("CIRCUIT_BREAKER_MAX_REQUESTS", 1),
		},
		Backend: BackendConfig{
			URLs:                getSliceEnv("BACKEND_URLS", []string{"http://localhost:8082", "http://localhost:8083"}),
			HealthCheckInterval: getDurationEnv("BACKEND_HEALTH_CHECK_INTERVAL", 10*time.Second),
			HealthCheckTimeout:  getDurationEnv("BACKEND_HEALTH_CHECK_TIMEOUT", 2*time.Second),
			HealthCheckPath:     getEnv("BACKEND_HEALTH_CHECK_PATH", "/health"),
			LoadBalanceStrategy: getEnv("BACKEND_LOAD_BALANCE_STRATEGY", "round-robin"),
			MaxIdleConns:        getIntEnv("BACKEND_MAX_IDLE_CONNS", 100),
			MaxConnsPerHost:     getIntEnv("BACKEND_MAX_CONNS_PER_HOST", 100),
			IdleConnTimeout:     getDurationEnv("BACKEND_IDLE_CONN_TIMEOUT", 90*time.Second),
			RetryAttempts:       getIntEnv("BACKEND_RETRY_ATTEMPTS", 3),
			RetryDelay:          getDurationEnv("BACKEND_RETRY_DELAY", 100*time.Millisecond),
		},
		Logging: LoggingConfig{
			Level:      getEnv("LOG_LEVEL", "info"),
			Format:     getEnv("LOG_FORMAT", "json"),
			Output:     getEnv("LOG_OUTPUT", "stdout"),
			FilePath:   getEnv("LOG_FILE_PATH", "/var/log/gateway.log"),
			MaxSize:    getIntEnv("LOG_MAX_SIZE", 100),
			MaxBackups: getIntEnv("LOG_MAX_BACKUPS", 3),
			MaxAge:     getIntEnv("LOG_MAX_AGE", 7),
		},
		Metrics: MetricsConfig{
			Enabled: getBoolEnv("METRICS_ENABLED", true),
			Port:    getEnv("METRICS_PORT", "9090"),
			Path:    getEnv("METRICS_PATH", "/metrics"),
		},
	}
}

// 辅助函数

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getInt64Env(key string, fallback int64) int64 {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return fallback
}

func getSliceEnv(key string, fallback []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return fallback
}
