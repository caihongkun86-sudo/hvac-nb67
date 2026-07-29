package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBChecker 数据库状态检查器
// 作为 ground-reporter 的"统一状态源"，从数据库同步预警状态到内存
// 与 storage-adapter 配合，确保两者状态一致
type DBChecker struct {
	pool *pgxpool.Pool
}

// NewDBChecker 创建数据库检查器
func NewDBChecker(ctx context.Context, dsn string) (*DBChecker, error) {
	if dsn == "" {
		return nil, nil
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db checker pool init failed: %w", err)
	}

	// 验证连接
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db checker ping failed: %w", err)
	}

	log.Printf("[INFO] db checker initialized")
	return &DBChecker{pool: pool}, nil
}

// Close 关闭数据库连接
func (c *DBChecker) Close() {
	if c != nil && c.pool != nil {
		c.pool.Close()
		log.Printf("[INFO] db checker closed")
	}
}

// IsAvailable 检查数据库检查器是否可用
func (c *DBChecker) IsAvailable() bool {
	return c != nil && c.pool != nil
}

// SyncAlarmState 同步指定设备的预警状态
// 根据数据库中活跃的预警列表，更新 ground-reporter 的内存状态
// 返回: DB 中活跃的 code 集合
func (c *DBChecker) SyncAlarmState(ctx context.Context, deviceID string) (map[string]bool, error) {
	if c == nil || c.pool == nil {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	activeCodes, err := c.getAllActiveCodes(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	return activeCodes, nil
}

// getAllActiveCodes 查询指定设备在数据库中所有活跃的预警代码
func (c *DBChecker) getAllActiveCodes(ctx context.Context, deviceID string) (map[string]bool, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT fault_code
		  FROM hvac.warning_lifecycle
		 WHERE device_id = $1
		   AND end_time IS NULL
	`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activeCodes := make(map[string]bool)
	for rows.Next() {
		var faultCode string
		if err := rows.Scan(&faultCode); err != nil {
			return nil, err
		}
		activeCodes[faultCode] = true
	}

	return activeCodes, nil
}
