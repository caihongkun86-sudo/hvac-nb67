-- ============================================================================
-- 11-migration-fact-raw-1min.sql
-- 1分钟粒度连续聚合视图：服务于 getPredictDetailCurve（预警前后2小时参数曲线）
--
-- 物化视图列已预聚合并除过 scale，查询时只需 AVG(viewCol) 再 ROUND，
-- 无需实时 JSONB 提取和 scale 除法，显著降低查询延迟。
-- 列名与 BFF TREND_PARAM_DEFS.viewCol 对齐。
--
-- TimescaleDB 连续聚合特性：
--   - materialized_only 默认 false，查询时合并实时 + 已聚合数据
--   - 自动按 time_bucket('1 minute') 分组，无需手动处理
--   - NULLIF 防止空字符串 ::numeric 报错（JSONB 字段可能返回 ''）
-- ============================================================================

BEGIN;

CREATE MATERIALIZED VIEW IF NOT EXISTS hvac.fact_raw_1min
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 minute', event_time) AS bucket,
    train_id,
    carriage_id,

    -- 顶级列 scale=10
    AVG(ras_u1 / 10)      AS ras_u1,
    AVG(ras_u2 / 10)      AS ras_u2,
    AVG(fas_u1 / 10)      AS fas_u1,
    AVG(fas_u2 / 10)      AS fas_u2,
    AVG(i_cp_u11 / 10)    AS i_cp_u11,
    AVG(i_cp_u12 / 10)    AS i_cp_u12,
    AVG(i_cp_u21 / 10)    AS i_cp_u21,
    AVG(i_cp_u22 / 10)    AS i_cp_u22,
    AVG(f_cp_u11 / 10)    AS f_cp_u11,
    AVG(f_cp_u12 / 10)    AS f_cp_u12,
    AVG(f_cp_u21 / 10)    AS f_cp_u21,
    AVG(f_cp_u22 / 10)    AS f_cp_u22,
    AVG(suckp_u11 / 10)   AS suckp_u11,
    AVG(suckp_u12 / 10)   AS suckp_u12,
    AVG(suckp_u21 / 10)   AS suckp_u21,
    AVG(suckp_u22 / 10)   AS suckp_u22,
    AVG(highpress_u11 / 10) AS highpress_u11,
    AVG(highpress_u12 / 10) AS highpress_u12,
    AVG(highpress_u21 / 10) AS highpress_u21,
    AVG(highpress_u22 / 10) AS highpress_u22,
    AVG(presdiff_u1 / 10) AS presdiff_u1,
    AVG(presdiff_u2 / 10) AS presdiff_u2,

    -- 顶级列 scale=1（无需除 scale）
    AVG(aq_co2_u1)   AS aq_co2_u1,
    AVG(aq_co2_u2)   AS aq_co2_u2,
    AVG(aq_pm2_5_u1) AS aq_pm2_5_u1,
    AVG(aq_pm2_5_u2) AS aq_pm2_5_u2,
    AVG(aq_pm10_u1)  AS aq_pm10_u1,
    AVG(aq_pm10_u2)  AS aq_pm10_u2,
    AVG(aq_tvoc_u1)  AS aq_tvoc_u1,
    AVG(aq_tvoc_u2)  AS aq_tvoc_u2,

    -- JSONB 嵌套列 scale=10（NULLIF 防空字符串 ::numeric 报错）
    AVG(NULLIF(payload_json->'raw'->>'Tic', '')::numeric / 10)     AS tic,
    AVG(NULLIF(payload_json->'raw'->>'Tveh1', '')::numeric / 10)   AS tveh1,
    AVG(NULLIF(payload_json->'raw'->>'Tveh2', '')::numeric / 10)   AS tveh2,
    AVG(NULLIF(payload_json->'raw'->>'SasU11', '')::numeric / 10)  AS sas_u11,
    AVG(NULLIF(payload_json->'raw'->>'SasU12', '')::numeric / 10)  AS sas_u12,
    AVG(NULLIF(payload_json->'raw'->>'SasU21', '')::numeric / 10)  AS sas_u21,
    AVG(NULLIF(payload_json->'raw'->>'SasU22', '')::numeric / 10)  AS sas_u22,
    AVG(NULLIF(payload_json->'raw'->>'SucktU11', '')::numeric / 10) AS suckt_u11,
    AVG(NULLIF(payload_json->'raw'->>'SucktU12', '')::numeric / 10) AS suckt_u12,
    AVG(NULLIF(payload_json->'raw'->>'SucktU21', '')::numeric / 10) AS suckt_u21,
    AVG(NULLIF(payload_json->'raw'->>'SucktU22', '')::numeric / 10) AS suckt_u22,
    AVG(NULLIF(payload_json->'raw'->>'SpU11', '')::numeric / 10)   AS sp_u11,
    AVG(NULLIF(payload_json->'raw'->>'SpU12', '')::numeric / 10)   AS sp_u12,
    AVG(NULLIF(payload_json->'raw'->>'SpU21', '')::numeric / 10)   AS sp_u21,
    AVG(NULLIF(payload_json->'raw'->>'SpU22', '')::numeric / 10)   AS sp_u22,
    AVG(NULLIF(payload_json->'raw'->>'VCpU11', '')::numeric / 10)  AS v_cp_u11,
    AVG(NULLIF(payload_json->'raw'->>'VCpU12', '')::numeric / 10)  AS v_cp_u12,
    AVG(NULLIF(payload_json->'raw'->>'VCpU21', '')::numeric / 10)  AS v_cp_u21,
    AVG(NULLIF(payload_json->'raw'->>'VCpU22', '')::numeric / 10)  AS v_cp_u22,
    AVG(NULLIF(payload_json->'raw'->>'ICfU11', '')::numeric / 10)  AS i_cf_u11,
    AVG(NULLIF(payload_json->'raw'->>'ICfU12', '')::numeric / 10)  AS i_cf_u12,
    AVG(NULLIF(payload_json->'raw'->>'ICfU21', '')::numeric / 10)  AS i_cf_u21,
    AVG(NULLIF(payload_json->'raw'->>'ICfU22', '')::numeric / 10)  AS i_cf_u22,
    AVG(NULLIF(payload_json->'raw'->>'IEfU11', '')::numeric / 10)  AS i_ef_u11,
    AVG(NULLIF(payload_json->'raw'->>'IEfU12', '')::numeric / 10)  AS i_ef_u12,
    AVG(NULLIF(payload_json->'raw'->>'IEfU21', '')::numeric / 10)  AS i_ef_u21,
    AVG(NULLIF(payload_json->'raw'->>'IEfU22', '')::numeric / 10)  AS i_ef_u22,
    AVG(NULLIF(payload_json->'raw'->>'IExufan', '')::numeric / 10) AS i_exufan
FROM hvac.fact_raw
WHERE event_time_valid = true
GROUP BY bucket, train_id, carriage_id
WITH NO DATA;

-- 添加连续聚合刷新策略：每5分钟刷新最近2小时的数据
SELECT add_continuous_aggregate_policy('hvac.fact_raw_1min',
  start_offset => INTERVAL '2 hours',
  end_offset   => INTERVAL '1 minute',
  schedule_interval => INTERVAL '5 minutes',
  if_not_exists => TRUE);

COMMIT;
