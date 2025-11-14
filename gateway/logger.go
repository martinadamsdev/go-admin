package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// LogLevel 日志级别
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

var logLevelNames = map[LogLevel]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
}

// Logger 结构化日志器
type Logger struct {
	level  LogLevel
	format string // "json" or "text"
	output io.Writer
}

// LogEntry 日志条目
type LogEntry struct {
	Timestamp string                 `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	RequestID string                 `json:"request_id,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

var globalLogger *Logger

// InitLogger 初始化日志器
func InitLogger(config LoggingConfig) *Logger {
	level := parseLogLevel(config.Level)
	output := getLogOutput(config.Output, config.FilePath)

	globalLogger = &Logger{
		level:  level,
		format: config.Format,
		output: output,
	}

	return globalLogger
}

// GetLogger 获取全局日志器
func GetLogger() *Logger {
	if globalLogger == nil {
		// 默认配置
		globalLogger = &Logger{
			level:  INFO,
			format: "json",
			output: os.Stdout,
		}
	}
	return globalLogger
}

func parseLogLevel(level string) LogLevel {
	switch level {
	case "debug":
		return DEBUG
	case "info":
		return INFO
	case "warn":
		return WARN
	case "error":
		return ERROR
	default:
		return INFO
	}
}

func getLogOutput(output, filePath string) io.Writer {
	switch output {
	case "stdout":
		return os.Stdout
	case "stderr":
		return os.Stderr
	case "file":
		file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open log file: %v, falling back to stdout\n", err)
			return os.Stdout
		}
		return file
	default:
		return os.Stdout
	}
}

// Debug 调试级别日志
func (l *Logger) Debug(message string, fields map[string]interface{}) {
	if l.level <= DEBUG {
		l.log(DEBUG, message, "", fields)
	}
}

// Info 信息级别日志
func (l *Logger) Info(message string, fields map[string]interface{}) {
	if l.level <= INFO {
		l.log(INFO, message, "", fields)
	}
}

// Warn 警告级别日志
func (l *Logger) Warn(message string, fields map[string]interface{}) {
	if l.level <= WARN {
		l.log(WARN, message, "", fields)
	}
}

// Error 错误级别日志
func (l *Logger) Error(message string, fields map[string]interface{}) {
	if l.level <= ERROR {
		l.log(ERROR, message, "", fields)
	}
}

// InfoWithRequestID 带请求ID的信息日志
func (l *Logger) InfoWithRequestID(requestID, message string, fields map[string]interface{}) {
	if l.level <= INFO {
		l.log(INFO, message, requestID, fields)
	}
}

// ErrorWithRequestID 带请求ID的错误日志
func (l *Logger) ErrorWithRequestID(requestID, message string, fields map[string]interface{}) {
	if l.level <= ERROR {
		l.log(ERROR, message, requestID, fields)
	}
}

func (l *Logger) log(level LogLevel, message, requestID string, fields map[string]interface{}) {
	entry := LogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     logLevelNames[level],
		Message:   message,
		RequestID: requestID,
		Fields:    fields,
	}

	var output string
	if l.format == "json" {
		data, _ := json.Marshal(entry)
		output = string(data) + "\n"
	} else {
		// 文本格式
		output = fmt.Sprintf("[%s] %s - %s", entry.Timestamp, entry.Level, entry.Message)
		if requestID != "" {
			output += fmt.Sprintf(" [RequestID: %s]", requestID)
		}
		if len(fields) > 0 {
			fieldsJSON, _ := json.Marshal(fields)
			output += fmt.Sprintf(" %s", fieldsJSON)
		}
		output += "\n"
	}

	l.output.Write([]byte(output))
}

// 全局日志函数（便捷使用）

func Debug(message string, fields map[string]interface{}) {
	GetLogger().Debug(message, fields)
}

func Info(message string, fields map[string]interface{}) {
	GetLogger().Info(message, fields)
}

func Warn(message string, fields map[string]interface{}) {
	GetLogger().Warn(message, fields)
}

func Error(message string, fields map[string]interface{}) {
	GetLogger().Error(message, fields)
}
