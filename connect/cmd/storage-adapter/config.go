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

	// WarnSweepTimeoutSec 预警超时消除阈值（秒）。
	// end_time IS NULL 且 last_seen_time 距今超过此值的活跃行由 SweepTimeouts 兜底关闭。
	// 0 = 禁用清扫器。默认 60s（storage-adapter 每帧刷新 last_seen_time，正常 1s 间隔）。
	WarnSweepTimeoutSec int

	// WarnSweepIntervalSec SweepTimeouts 执行间隔（秒）。默认 30s。
	WarnSweepIntervalSec int

	// AlarmSweepTimeoutSec 告警超时消除阈值（秒）。
	// alarm_lifecycle 中 end_time IS NULL 且 last_seen_time 距今超过此值的活跃行
	// 由 AlarmLifecycleWriter.SweepTimeouts 兜底关闭（DB close + 内存 active 同步清理）。
	// 0 = 禁用。默认 300s（约定：连续 300s 未刷新视为设备断流，告警消除）。
	AlarmSweepTimeoutSec int

	// AlarmSweepIntervalSec alarm SweepTimeouts 执行间隔（秒）。默认 300s。
	AlarmSweepIntervalSec int
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

		WarnSweepTimeoutSec:  getEnvInt("WARN_SWEEP_TIMEOUT_SEC", 43200),
		WarnSweepIntervalSec: getEnvInt("WARN_SWEEP_INTERVAL_SEC", 300),

		AlarmSweepTimeoutSec:  getEnvInt("ALARM_SWEEP_TIMEOUT_SEC", 300),
		AlarmSweepIntervalSec: getEnvInt("ALARM_SWEEP_INTERVAL_SEC", 300),
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
