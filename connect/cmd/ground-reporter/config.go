package main

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Platform endpoint URLs (full URLs, one per protocol chapter)
	FaultRecordURL string // 6.1 预警/报警写入
	SysStatusURL   string // 6.6 运行状态心跳
	LifeRecordURL  string // 6.7 寿命状态数据

	// Common HTTP settings
	PlatformApiKey         string // X-Api-Key header; empty = no auth
	PlatformXToken         string // x-token header; empty = no auth
	PlatformTimeoutSec     int
	PlatformRetryMax       int
	PlatformRetryBackoffMs int

	// HVAC subsystem identity
	SubsystemCode string // fixed "5"
	TrainType     string // e.g. "B"

	// Heartbeat / daily batch schedule
	HeartbeatIntervalMin int
	DailyBatchHour       int // 0-23, default 0 (midnight)

	// Kafka
	KafkaBrokers []string

	// Lifecycle 状态恢复：启动时从 hvac.warning_lifecycle 拉活跃集合，
	// 避免 ground-reporter 重启后把已 active 的预警当作新 open 重发给平台。
	// 空字符串 = 不启用 DB 恢复（保留原 in-memory 行为）。
	PGDSN string

	// 批量发送配置（减少 HTTP 调用频率）
	// BatchSize: 累积多少条记录后发送，0 或 1 表示不启用批量
	// FlushIntervalMs: 超时自动发送（毫秒），0 表示不启用超时
	BatchSize         int
	FlushIntervalMs   int

	// 状态同步配置
	// StateSyncEnabled: 是否启用内存与数据库状态同步
	// StateSyncIntervalSec: 同步间隔（秒）
	StateSyncEnabled    bool
	StateSyncIntervalSec int

	LogLevel string
}

func loadConfig() Config {
	return Config{
		FaultRecordURL: getEnv("FAULT_RECORD_URL", "https://clznyw7.nbmetro.com/gate/METRO-PHM/api/faultRecordsSubsystem/saveRecord"),
		SysStatusURL:   getEnv("SYS_STATUS_URL", "https://clznyw7.nbmetro.com/gate/METRO-SELFCHECK-SUBSYSTEM/api/faultRecordsSubsystem/saveStatus"),
		LifeRecordURL:  getEnv("LIFE_RECORD_URL", "https://clznyw7.nbmetro.com/gate/METRO-PHM/api/devices/status/train/saveOrUpdate"),

		PlatformApiKey:         getEnv("PLATFORM_API_KEY", ""),
		PlatformXToken:         getEnv("PLATFORM_X_TOKEN", ""),
		PlatformTimeoutSec:     getEnvInt("PLATFORM_TIMEOUT_SEC", 10),
		PlatformRetryMax:       getEnvInt("PLATFORM_RETRY_MAX", 3),
		PlatformRetryBackoffMs: getEnvInt("PLATFORM_RETRY_BACKOFF_MS", 1000),

		SubsystemCode: getEnv("SUBSYSTEM_CODE", "5"),
		TrainType:     getEnv("TRAIN_TYPE", "B"),

		HeartbeatIntervalMin: getEnvInt("HEARTBEAT_INTERVAL_MIN", 10),
		DailyBatchHour:       getEnvInt("DAILY_BATCH_HOUR", 0),

		KafkaBrokers: splitCSV(getEnv("KAFKA_BROKERS", "redpanda-1:9092,redpanda-2:9092,redpanda-3:9092")),

		PGDSN: getEnv("PG_DSN", ""),

		// 批量发送配置
		BatchSize:       getEnvInt("BATCH_SIZE", 50),
		FlushIntervalMs: getEnvInt("FLUSH_INTERVAL_MS", 5000),

		// 状态同步配置
		StateSyncEnabled:     getEnvBool("STATE_SYNC_ENABLED", true),
		StateSyncIntervalSec: getEnvInt("STATE_SYNC_INTERVAL_SEC", 30),

		LogLevel: getEnv("LOG_LEVEL", "INFO"),
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func getEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
