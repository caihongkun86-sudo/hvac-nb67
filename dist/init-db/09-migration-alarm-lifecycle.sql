-- =============================================================================
-- Migration: 2026-08-28  alarm_lifecycle 告警生命周期表
--
-- 背景：
--   现状 alarm 路径每帧写入 hvac.fact_event（每帧每 hit 一行 = 重复源），
--   BFF getHistoricalEvents 的 alarm 分支需在 JS 层做 60s "发作岛" 去重。
--   本表对齐 warning_lifecycle 的设计：一条告警从开始到结束只有一行，
--   由 storage-adapter 的 AlarmLifecycleWriter 做 per-device diff 后写入
--   (open/touch/close 三态)，BFF 改为直接查本表，删除 JS 去重逻辑。
--
--   双轨过渡：本 migration 不动现有 fact_event 表，alarm 仍同时写两张表，
--   待 BFF 切换查询 + 历史回填验证一致后再下线 fact_event 的 alarm 写入。
--
-- 时间字段约定：
--   start_time      = 告警首次命中的 event_time（设备时间，应用层传入）
--   last_seen_time  = 最近一次仍命中的 event_time（应用层传入）
--   end_time        = 告警消除时刻，取 last_seen_time（清扫器兜底或 diff close 时填）
--   created_at      = DB now()（DDL DEFAULT，INSERT 时不显式传）
--   updated_at      = DB now()（由 tr_alarm_lifecycle_updated_at 触发器在 BEFORE UPDATE 时维护）
--
-- 所有语句幂等，可重复执行。
-- =============================================================================

CREATE TABLE IF NOT EXISTS hvac.alarm_lifecycle (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id       VARCHAR(64)  NOT NULL,
    line_id         INTEGER,
    train_id        INTEGER,
    carriage_id     INTEGER,
    unit_id         SMALLINT,                 -- 机组号 1 / 2 / NULL（由 fault_code 序号推断）
    fault_code      VARCHAR(128) NOT NULL,    -- 例：HVAC112
    fault_name      TEXT,
    severity        SMALLINT,
    start_time      TIMESTAMPTZ  NOT NULL,    -- 首次命中时刻（取 event_time）
    end_time        TIMESTAMPTZ,              -- NULL 表示仍处于活动状态
    last_seen_time  TIMESTAMPTZ  NOT NULL,    -- 最近一次仍命中的时刻（取 event_time）
    trigger_snapshot JSONB,                   -- 触发时上下文快照（对应 fact_event.payload_json）
    source          VARCHAR(32) DEFAULT 'connect-rule-v2',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE  hvac.alarm_lifecycle                IS '告警生命周期表：一条告警从开始到结束只有一行（end_time IS NULL 表示活跃中）';
COMMENT ON COLUMN hvac.alarm_lifecycle.start_time     IS '告警首次命中时刻（取 event_time，设备时间）';
COMMENT ON COLUMN hvac.alarm_lifecycle.end_time       IS 'NULL 表示活跃中；消除时填写 = last_seen_time（清扫器兜底或 diff close）';
COMMENT ON COLUMN hvac.alarm_lifecycle.last_seen_time IS '最近一次仍命中的 event_time；连续 300s 未刷新由清扫器兜底填 end_time';
COMMENT ON COLUMN hvac.alarm_lifecycle.trigger_snapshot IS '触发时刻上下文快照，对应 fact_event.payload_json';
COMMENT ON COLUMN hvac.alarm_lifecycle.created_at     IS '记录创建时刻（DB now()）';
COMMENT ON COLUMN hvac.alarm_lifecycle.updated_at     IS '记录更新时刻（DB now()，由触发器维护）';

-- 部分唯一索引（核心约束）：同一 (device_id, fault_code) 活跃时只能 1 行
-- 从数据库层根除"同时刻同条告警重复"问题，应用层判定失误也有兜底
CREATE UNIQUE INDEX IF NOT EXISTS uq_alarm_lifecycle_active
    ON hvac.alarm_lifecycle (device_id, fault_code)
    WHERE end_time IS NULL;

-- 查询性能索引（对齐 BFF getHistoricalEvents 的查询模式）
CREATE INDEX IF NOT EXISTS ix_alarm_lifecycle_active
    ON hvac.alarm_lifecycle (train_id, carriage_id, fault_code)
    WHERE end_time IS NULL;

CREATE INDEX IF NOT EXISTS ix_alarm_lifecycle_history
    ON hvac.alarm_lifecycle (train_id, start_time DESC);

CREATE INDEX IF NOT EXISTS ix_alarm_lifecycle_device_history
    ON hvac.alarm_lifecycle (device_id, fault_code, start_time DESC);

-- updated_at 自动维护（对齐 warning_lifecycle：BEFORE UPDATE 触发器写 now()）
CREATE OR REPLACE FUNCTION hvac.tg_alarm_lifecycle_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tr_alarm_lifecycle_updated_at ON hvac.alarm_lifecycle;
CREATE TRIGGER tr_alarm_lifecycle_updated_at
    BEFORE UPDATE ON hvac.alarm_lifecycle
    FOR EACH ROW
    EXECUTE FUNCTION hvac.tg_alarm_lifecycle_updated_at();
