package main

import (
	"context"
	"log"
	"time"
)

// StateSyncer 状态同步器
// 定期将数据库中的预警状态同步到 ground-reporter 的内存中
// 解决 storage-adapter 通过 ExpireScanner 消除预警后，ground-reporter 内存状态不同步的问题
type StateSyncer struct {
	tracker  *AlarmTracker
	dbChecker *DBChecker
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewStateSyncer 创建状态同步器
func NewStateSyncer(tracker *AlarmTracker, dbChecker *DBChecker, interval time.Duration) *StateSyncer {
	if dbChecker == nil || !dbChecker.IsAvailable() {
		log.Printf("[INFO] state syncer disabled: no DB checker available")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &StateSyncer{
		tracker:  tracker,
		dbChecker: dbChecker,
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}

	log.Printf("[INFO] state syncer initialized: interval=%v", interval)
	return s
}

// Start 启动后台同步
func (s *StateSyncer) Start() {
	if s == nil {
		return
	}

	go s.run()
	log.Printf("[INFO] state syncer started")
}

// Stop 停止同步
func (s *StateSyncer) Stop() {
	if s == nil {
		return
	}
	s.cancel()
	log.Printf("[INFO] state syncer stopped")
}

// run 后台同步循环
func (s *StateSyncer) run() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.syncAllDevices()
		}
	}
}

// syncAllDevices 同步所有设备的状态
func (s *StateSyncer) syncAllDevices() {
	devices := s.tracker.GetDevices()
	if len(devices) == 0 {
		return
	}

	totalRemoved := 0
	totalAdded := 0
	nowTs := nowMs()

	for _, deviceID := range devices {
		// 跳过已取消的设备
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		dbActiveCodes, err := s.dbChecker.SyncAlarmState(s.ctx, deviceID)
		if err != nil {
			log.Printf("[WARN] state syncer: sync device %s failed: %v", deviceID, err)
			continue
		}

		// 获取 predict 设备的数据库状态
		predictKey := "predict:" + deviceID
		dbRemoved := s.tracker.SyncWithDB(predictKey, dbActiveCodes, nowTs)
		if dbRemoved > 0 {
			totalRemoved += dbRemoved
		}

		// 同时同步 alarm 设备（如果存在）
		alarmKey := "alarm:" + deviceID
		if s.tracker.HasDevice(alarmKey) {
			dbRemoved2 := s.tracker.SyncWithDB(alarmKey, dbActiveCodes, nowTs)
			if dbRemoved2 > 0 {
				totalRemoved += dbRemoved2
			}
		}
	}

	if totalRemoved > 0 || totalAdded > 0 {
		log.Printf("[STATE-SYNC] completed: checked %d devices, removed %d, added %d",
			len(devices), totalRemoved, totalAdded)
	}
}

// StateSyncConfig 状态同步配置
type StateSyncConfig struct {
	Enabled           bool
	SyncInterval      time.Duration
}

// DefaultStateSyncConfig 默认配置
func DefaultStateSyncConfig() StateSyncConfig {
	return StateSyncConfig{
		Enabled:      true,
		SyncInterval: 30 * time.Second, // 每 30 秒同步一次
	}
}
