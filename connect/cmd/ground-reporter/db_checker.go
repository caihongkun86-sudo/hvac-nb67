package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBChecker 数据库状态检查器
// 用于在发送预警前查询数据库，确认预警是否已经处于活跃状态
// 作为 ground-reporter 的"统一状态源"，避免与 storage-adapter 的内存状态不同步
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

// IsAlarmActive 检查指定设备的指定预警是否在数据库中处于活跃状态
// 返回 true 表示预警已活跃（end_time IS NULL），应跳过发送
func (c *DBChecker) IsAlarmActive(ctx context.Context, deviceID, faultCode string) (bool, error) {
	if c == nil || c.pool == nil {
		return false, nil // 无数据库连接时，不检查
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var exists bool
	err := c.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM hvac.warning_lifecycle
			 WHERE device_id = $1
			   AND fault_code = $2
			   AND end_time IS NULL
		)
	`, deviceID, faultCode).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("check alarm active failed: %w", err)
	}

	return exists, nil
}

// FilterActiveAlarms 批量过滤已在数据库中活跃的预警
// 输入: 需要检查的预警列表 (deviceID + faultCode)
// 输出: 需要发送的预警列表（排除已活跃的）
func (c *DBChecker) FilterActiveAlarms(ctx context.Context, records []Record61, deviceID string) []Record61 {
	if c == nil || c.pool == nil || len(records) == 0 {
		return records // 无数据库连接时，不过滤
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// 收集所有需要检查的 fault_code
	codes := make([]string, 0, len(records))
	for _, r := range records {
		codes = append(codes, r.Code)
	}

	// 批量查询数据库中活跃的预警
	activeCodes, err := c.getActiveCodes(ctx, deviceID, codes)
	if err != nil {
		log.Printf("[WARN] db checker batch query failed, sending all records: %v", err)
		return records // 查询失败时，保守处理：发送所有记录
	}

	if len(activeCodes) == 0 {
		return records // 没有活跃的，全部发送
	}

	// 过滤掉已活跃的预警
	var filtered []Record61
	skipped := 0
	for _, r := range records {
		if _, exists := activeCodes[r.Code]; exists {
			skipped++
			if skipped <= 3 { // 只打印前几个跳过的
				log.Printf("[DB-SKIP] device=%s code=%s already active in DB", deviceID, r.Code)
			}
			continue
		}
		filtered = append(filtered, r)
	}

	if skipped > 0 {
		log.Printf("[DB-FILTER] filtered %d already-active alarms for device=%s", skipped, deviceID)
	}

	return filtered
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

// getActiveCodes 批量查询数据库中指定设备的活跃预警代码
func (c *DBChecker) getActiveCodes(ctx context.Context, deviceID string, codes []string) (map[string]bool, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// 构建 IN 子句（防止 SQL 注入，使用参数化查询）
	placeholders := make([]string, len(codes))
	args := make([]interface{}, len(codes)+1)
	args[0] = deviceID
	for i, code := range codes {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = code
	}

	query := fmt.Sprintf(`
		SELECT fault_code
		  FROM hvac.warning_lifecycle
		 WHERE device_id = $1
		   AND fault_code IN (%s)
		   AND end_time IS NULL
	`, strings.Join(placeholders, ","))

	rows, err := c.pool.Query(ctx, query, args...)
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
