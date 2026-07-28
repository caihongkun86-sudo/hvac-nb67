package main

import (
	"context"
	"log"
	"sync"
	"time"
)

// BatchSender 批量发送器：累积多条预警记录后一次性发送到平台
type BatchSender struct {
	client         *PlatformClient
	cfg            Config
	batch          []Record61
	mu             sync.Mutex
	ticker         *time.Ticker
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	isBatchEnabled bool
}

// newBatchSender 创建批量发送器
// 如果 BatchSize <= 1，返回 nil（不启用批量模式）
func newBatchSender(client *PlatformClient, cfg Config) *BatchSender {
	if cfg.BatchSize <= 1 {
		log.Printf("[INFO] batch sender disabled (BATCH_SIZE=%d)", cfg.BatchSize)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &BatchSender{
		client:         client,
		cfg:            cfg,
		batch:          make([]Record61, 0, cfg.BatchSize),
		ctx:            ctx,
		cancel:         cancel,
		isBatchEnabled: true,
	}

	log.Printf("[INFO] batch sender enabled: size=%d, flush=%dms", cfg.BatchSize, cfg.FlushIntervalMs)

	return s
}

// Start 启动批量发送器的定时刷盘协程
func (s *BatchSender) Start() {
	if !s.isBatchEnabled {
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		if s.cfg.FlushIntervalMs > 0 {
			s.ticker = time.NewTicker(time.Duration(s.cfg.FlushIntervalMs) * time.Millisecond)
			defer s.ticker.Stop()
		}

		for {
			select {
			case <-s.ctx.Done():
				s.flush()
				return
			case <-s.ticker.C:
				s.flush()
			}
		}
	}()
}

// Stop 停止批量发送器，等待剩余记录发送完成
func (s *BatchSender) Stop() {
	if !s.isBatchEnabled {
		return
	}

	s.cancel()
	s.wg.Wait()
	log.Printf("[INFO] batch sender stopped")
}

// Add 添加记录到批量队列
// 如果队列达到 BatchSize，立即发送
func (s *BatchSender) Add(records []Record61) {
	if !s.isBatchEnabled {
		// 不启用批量时，直接发送
		s.sendRecords(records)
		return
	}

	if len(records) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.batch = append(s.batch, records...)

	if len(s.batch) >= s.cfg.BatchSize {
		s.flushLocked()
	}
}

// flush 刷盘（线程安全）
func (s *BatchSender) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
}

// flushLocked 刷盘（必须持有锁）
func (s *BatchSender) flushLocked() {
	if len(s.batch) == 0 {
		return
	}

	// 复制并清空队列
	toSend := make([]Record61, len(s.batch))
	copy(toSend, s.batch)
	s.batch = s.batch[:0]

	// 在 goroutine 中发送，不阻塞消费
	go s.sendRecords(toSend)
}

// sendRecords 发送记录到平台
func (s *BatchSender) sendRecords(records []Record61) {
	if len(records) == 0 {
		return
	}

	if s.cfg.LogLevel == "DEBUG" {
		log.Printf("[BATCH-SEND] sending %d records to %s", len(records), s.cfg.FaultRecordURL)
	}

	if err := s.client.PostJSON(s.ctx, s.cfg.FaultRecordURL, records); err != nil {
		log.Printf("[ERROR] batch POST failed: count=%d, error=%v", len(records), err)
	} else {
		log.Printf("[SUCCESS] batch POST success: count=%d", len(records))
	}
}

// IsEnabled 返回是否启用了批量模式
func (s *BatchSender) IsEnabled() bool {
	return s.isBatchEnabled
}
