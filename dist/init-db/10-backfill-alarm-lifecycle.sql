-- ============================================================================
-- 10-backfill-alarm-lifecycle.sql
-- 历史告警回填：hvac.fact_event 存量 alarm 数据 → hvac.alarm_lifecycle
--
-- 逻辑：按 300s 发作岛（gap island）去重
--   同一 (device_id, fault_code) 的连续 alarm 事件，相邻间隔 <= 300s 视为同一次发作；
--   间隔 > 300s 切分新岛。每岛生成一行 alarm_lifecycle：
--     start_time      = 岛内最早的 event_time
--     last_seen_time  = 岛内最晚的 event_time
--     end_time        = last_seen_time（已结束）；
--                       若 last_seen_time 距 now() <= 300s 则留 NULL，
--                       由 alarm-lifecycle-writer 启动时 Recover 接管（或被清扫器兜底关闭）
--     trigger_snapshot= 岛内首帧的 payload_json
--     source          = 'backfill-fact-event'（与 writer 写入的 'connect-rule-v2' 区分）
--   created_at / updated_at 使用表默认 now()。
--
-- 幂等性：可重复执行。脚本先删除上一次回填行（source='backfill-fact-event'）再重灌；
--   writer 已接管的活跃行（source='connect-rule-v2'）不受影响，open 行冲突时 DO NOTHING。
--
-- 执行方式：
--   docker exec -i timescaledb psql -U postgres -d postgres < 10-backfill-alarm-lifecycle.sql
-- ============================================================================

BEGIN;

-- 1) 清理上一次回填结果（幂等重跑）
DELETE FROM hvac.alarm_lifecycle WHERE source = 'backfill-fact-event';

-- 2) 300s 发作岛聚合回填
WITH alarm_rows AS (
    SELECT device_id, fault_code, event_time, ingest_time,
           line_id, train_id, carriage_id,
           fault_name, severity, payload_json
    FROM hvac.fact_event
    WHERE event_type = 'alarm'
),
gapped AS (
    SELECT r.*,
           CASE
               WHEN event_time - LAG(event_time) OVER w <= interval '300 seconds'
               THEN 0 ELSE 1
           END AS new_island
    FROM alarm_rows r
    WINDOW w AS (PARTITION BY device_id, fault_code ORDER BY event_time)
),
islands AS (
    SELECT g.*,
           SUM(new_island) OVER (PARTITION BY device_id, fault_code ORDER BY event_time) AS island_no
    FROM gapped g
),
agg AS (
    SELECT
        device_id,
        fault_code,
        MIN(event_time)                                   AS start_time,
        MAX(event_time)                                   AS last_seen_time,
        (ARRAY_AGG(line_id      ORDER BY event_time))[1]  AS line_id,
        (ARRAY_AGG(train_id     ORDER BY event_time))[1]  AS train_id,
        (ARRAY_AGG(carriage_id  ORDER BY event_time))[1]  AS carriage_id,
        (ARRAY_AGG(fault_name   ORDER BY event_time))[1]  AS fault_name,
        (ARRAY_AGG(severity     ORDER BY event_time))[1]  AS severity,
        (ARRAY_AGG(payload_json ORDER BY event_time))[1]  AS trigger_snapshot
    FROM islands
    GROUP BY device_id, fault_code, island_no
)
INSERT INTO hvac.alarm_lifecycle (
    device_id, line_id, train_id, carriage_id, unit_id,
    fault_code, fault_name, severity,
    start_time, end_time, last_seen_time,
    trigger_snapshot, source
)
SELECT
    device_id,
    line_id,
    train_id,
    carriage_id,
    NULL,                       -- fact_event 无 unit 信息
    fault_code,
    fault_name,
    severity,
    start_time,
    CASE
        WHEN last_seen_time >= now() - interval '300 seconds' THEN NULL  -- 可能仍活跃，交由 writer Recover/清扫器接管
        ELSE last_seen_time                                              -- 历史岛：last_seen_time 即结束时刻
    END,
    last_seen_time,
    trigger_snapshot,
    'backfill-fact-event'
FROM agg
ON CONFLICT (device_id, fault_code) WHERE end_time IS NULL DO NOTHING;

COMMIT;
