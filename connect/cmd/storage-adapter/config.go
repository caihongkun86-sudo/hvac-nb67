package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	KafkaBrokers  []string
	KafkaTopics   []string
	KafkaGroup    string
	PGDSN         string
	BatchSize     int
	FlushInterval time.Duration
	LogLevel      string
	// WriteFactEvent 控制是否把 event 消息写入 hvac.fact_event。
	// 默认 true（向后兼容 Plan B 全量替代场景）；在 lifecycle-only 部署里设为 false，
	// 以避免与 connect-event-writer（YAML Plan A）双重写入 fact_event。
	WriteFactEvent bool
	// WriteFactRaw 控制是否把 storage 消息写入 hvac.fact_raw。
	// 默认 true；lifecycle-only 部署通常只订阅 signal-predict，不会走此路径。
	WriteFactRaw bool
	
	// 预警超时配置
	// AlarmExpireMinutes: 预警最后更新时间超过此分钟数后，自动判定为消除（0 表示禁用）
	// ExpireScanIntervalMinutes: 扫描间隔（分钟）
	AlarmExpireMinutes        int
	ExpireScanIntervalMinutes int
}

func loadConfig() Config {
	return Config{
		KafkaBrokers:   splitCSV(getEnv("KAFKA_BROKERS", "redpanda-1:9092,redpanda-2:9092,redpanda-3:9092")),
		KafkaTopics:    splitCSV(getEnv("KAFKA_TOPICS", "signal-storage,signal-alarm,signal-predict,signal-life")),
		KafkaGroup:     getEnv("KAFKA_GROUP", "macda-storage-adapter-v1"),
		PGDSN:          getEnv("PG_DSN", "postgres://postgres:passw0rd@timescaledb:5432/postgres?sslmode=disable"),
		BatchSize:      getEnvInt("BATCH_SIZE", 300),
		FlushInterval:  time.Duration(getEnvInt("FLUSH_INTERVAL_MS", 300)) * time.Millisecond,
		LogLevel:       getEnv("LOG_LEVEL", "INFO"),
		WriteFactEvent: getEnvBool("WRITE_FACT_EVENT", true),
		WriteFactRaw:   getEnvBool("WRITE_FACT_RAW", true),
		// 预警超时配置：默认 30 分钟无更新则自动消除，每 5 分钟扫描一次
		AlarmExpireMinutes:        getEnvInt("ALARM_EXPIRE_MINUTES", 30),
		ExpireScanIntervalMinutes: getEnvInt("EXPIRE_SCAN_INTERVAL_MINUTES", 5),
	}
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return number
}
