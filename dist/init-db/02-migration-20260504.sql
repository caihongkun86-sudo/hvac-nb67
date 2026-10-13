-- =============================================================================
-- Migration: 2026-05-04
-- 变更内容：
--   D1. hvac.fact_event 增加 recovery_time 字段（报警/预警结束时间）
--   D2. 新建 hvac.warning_config 表（PHM 预警条件配置，支持前端编辑阈值）
-- 说明：所有语句幂等，可重复执行
-- =============================================================================

-- ----------------------------------------------------------------------------
-- D1. hvac.fact_event: 增加结束时间字段
--     BFF 现有代码已预留 recovery_time: null 占位，字段名与此一致
-- ----------------------------------------------------------------------------
ALTER TABLE hvac.fact_event
    ADD COLUMN IF NOT EXISTS recovery_time TIMESTAMPTZ;

COMMENT ON COLUMN hvac.fact_event.recovery_time IS '预警/报警消除时间（NULL 表示仍处于活动状态）';

-- ----------------------------------------------------------------------------
-- D2. 新建 hvac.warning_config：PHM 预警条件配置表
--
-- 设计说明：
--   - 标准字段存共性属性（编码、名称、单位、持续时间、启用状态）
--   - threshold_good/normal/bad 存前端显示用的范围描述（如 "0~75"）
--   - trigger_value / clear_value 存机器可读的数值阈值，event-builder 直接使用
--   - params JSONB 存各预警类型的专属参数（多条件预警用）
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS hvac.warning_config (
    id               SERIAL PRIMARY KEY,
    warn_code        VARCHAR(64)   NOT NULL,            -- 预警唯一编码
    component_name   VARCHAR(128)  NOT NULL,            -- 前端显示名称
    category         VARCHAR(64),                       -- 分类: air_quality / current / pressure / temperature / life
    threshold_good   VARCHAR(64),                       -- 良好范围（显示用，如 "0~75"）
    threshold_normal VARCHAR(64),                       -- 一般范围（显示用）
    threshold_bad    VARCHAR(64),                       -- 差阈值（显示用，如 "≥150"）
    trigger_operator VARCHAR(8)    NOT NULL DEFAULT '>', -- 触发比较符: > >= < <=
    trigger_value    NUMERIC(12,3) NOT NULL,            -- 触发阈值（event-builder 使用）
    clear_value      NUMERIC(12,3),                     -- 消除阈值（NULL 则与 trigger_value 相同）
    duration_seconds INTEGER       NOT NULL DEFAULT 0,  -- 持续时间门槛（秒），0=立即触发
    unit             VARCHAR(32),                       -- 单位，如 ppm / A / pa / ug/m³
    strategy         TEXT,                              -- 预警策略/指导意见（前端展示）
    params           JSONB,                             -- 复杂预警的专属参数
    enabled          BOOLEAN       NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_warning_config_warn_code
    ON hvac.warning_config (warn_code);

CREATE INDEX IF NOT EXISTS ix_warning_config_category
    ON hvac.warning_config (category);

COMMENT ON TABLE hvac.warning_config IS 'PHM 预警条件配置表，存储各预警项阈值，支持前端动态修改';
COMMENT ON COLUMN hvac.warning_config.trigger_value    IS '触发阈值，原始数据单位（如压差0.1pa，1.8A×10=18）';
COMMENT ON COLUMN hvac.warning_config.duration_seconds IS '需要持续满足条件的秒数，0=立即触发';
COMMENT ON COLUMN hvac.warning_config.params           IS '复杂预警的额外参数（JSON），优先级高于 trigger_value';

-- updated_at 自动维护触发器
CREATE OR REPLACE FUNCTION hvac.set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_warning_config_updated_at ON hvac.warning_config;
CREATE TRIGGER trg_warning_config_updated_at
    BEFORE UPDATE ON hvac.warning_config
    FOR EACH ROW EXECUTE FUNCTION hvac.set_updated_at();

-- ----------------------------------------------------------------------------
-- D2. 预警配置初始数据（PHM 文档 10 个预警，阈值来自 v02）
--     使用 WHERE NOT EXISTS 保证幂等
-- ----------------------------------------------------------------------------

-- 3.1 车厢超温预警（三条件同时满足，params 存专属参数）
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
VALUES
    ('WARN_CABIN_OVERHEAT', '车厢超温预警', 'temperature',
     '目标温度±2℃', '目标温度+2~4℃', '>目标温度+4℃',
     '>', 4.0, 3.0, 120,
     '℃', '车厢制冷效果异常，可能为车门持续开启、蒸发器脏堵、冷媒少量泄漏或控制逻辑异常，请检修',
     '{"raw_scale": 10, "target_temp": 26.0, "trigger_duration_min": 2, "min_cooling_runtime_min": 20, "trigger_duration_min": 2, "precondition": "no_alarm AND cooling_mode"}'::jsonb)
ON CONFLICT (warn_code) 
DO UPDATE SET
    component_name = EXCLUDED.component_name,
    category = EXCLUDED.category,
    threshold_good = EXCLUDED.threshold_good,
    threshold_normal = EXCLUDED.threshold_normal,
    threshold_bad = EXCLUDED.threshold_bad,
    trigger_operator = EXCLUDED.trigger_operator,
    trigger_value = EXCLUDED.trigger_value,
    clear_value = EXCLUDED.clear_value,
    duration_seconds = EXCLUDED.duration_seconds,
    unit = EXCLUDED.unit,
    strategy = EXCLUDED.strategy,
    params = EXCLUDED.params;

-- 3.2 冷媒泄露预警（两个OR条件，params 存子条件）
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
SELECT 'WARN_REFRIGERANT_LEAK', '冷媒泄露预警', 'pressure',
       '吸气≥2.0bar', '—', '吸气<2.0bar',
       '<', 2.0, 2.0, 300,
       'bar', '冷媒管路可能泄漏，请采用手持式卤素仪检测漏点位置并安排检修',
       '{"condition1": {"mode": "cooling", "fcp_gt": 300, "suckp_lt": 20, "duration_min": 5},
         "condition2": {"mode": "ventilation", "highp_lt": 50, "duration_min": 15}}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_REFRIGERANT_LEAK');

-- 3.3 制冷系统预警（电流差/过热度异常，params 存两个子类型）
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
SELECT 'WARN_COOLING_SYSTEM', '制冷系统预警', 'current',
       '电流差<2A', '—', '电流差≥2A或过热度异常',
       '>', 2.0, 2.0, 180,
       'A', '压缩机或电子膨胀阀异常，触发后需专业检修人员比对排查，定位故障部件',
       '{"current_diff_threshold_a": 2.0, "superheat_high": 20.0, "superheat_low": -8.0,
         "superheat_duration_min": 10}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_COOLING_SYSTEM');

-- 3.4 新/回风温度传感器预警
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
SELECT 'WARN_TEMP_SENSOR', '车厢/新/回风温度传感器预警', 'temperature',
       '两机组差值<5℃', '5~8℃', '≥8℃',
       '>', 8.0, 5.0, 480,
       '℃', '温度传感器可能故障，请现场排查并采用相邻车厢温度值作参考进行异常部件定位',
       '{"raw_scale": 10, "fas_threshold_c": 8.0, "ras_threshold_c": 8.0, "tveh_threshold_c": 8.0}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_TEMP_SENSOR');

-- 3.5 空调滤网脏堵预警
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
SELECT 'WARN_FILTER_CLOG', '空调滤网脏堵预警', 'pressure',
       '100~180pa', '180~300pa', '>300pa',
       '>', 300.0, 200.0, 1800,
       'pa', '空调滤网压差超标，滤网可能严重脏堵，请尽快安排清洗',
       '{"raw_unit_factor": 10}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_FILTER_CLOG');

-- 3.6 通风机电流预警
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy)
SELECT 'WARN_EF_CURRENT', '通风机电流预警', 'current',
       '≤1.6A', '1.6~1.8A', '>1.8A',
       '>', 1.8, 1.6, 600,
       'A', '通风机运行电流偏大，可能为轴承磨损或叶轮失衡，请安排检修'
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_EF_CURRENT');

-- 3.7 冷凝风机电流预警
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy)
SELECT 'WARN_CF_CURRENT', '冷凝风机电流预警', 'current',
       '≤2.0A', '2.0~2.3A', '>2.3A',
       '>', 2.3, 2.0, 600,
       'A', '冷凝风机运行电流偏大，可能为轴承磨损或叶轮失衡，请安排检修'
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_CF_CURRENT');

-- 3.8 废排风机电流预警
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy)
SELECT 'WARN_EXUF_CURRENT', '废排风机电流预警', 'current',
       '≤2.0A', '2.0~2.3A', '>2.3A',
       '>', 2.3, 2.0, 600,
       'A', '废排风机运行电流偏大，可能为轴承磨损或叶轮失衡，请安排检修'
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_EXUF_CURRENT');

-- 3.9 压缩机电流预警（新风温度<35℃前置条件）
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
SELECT 'WARN_CP_CURRENT', '压缩机电流预警', 'current',
       '≤18A', '—', '>18A',
       '>', 18.0, 18.0, 600,
       'A', '压缩机电流过大，可能为润滑油泄露、冷凝风机反转或冷凝器脏堵严重，请专业检修',
       '{"precondition": "fas_lt_35c"}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_CP_CURRENT');

-- 3.10 空气质量预警（CO₂/PM2.5/PM10/TVOC，前置：通风机运行>20min）
INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
SELECT 'WARN_AQ_CO2', '空气质量-二氧化碳浓度预警', 'air_quality',
       '<4500ppm', '—', '≥4500ppm',
       '>', 4500.0, 4500.0, 900,
       'ppm', 'CO₂浓度超标，请检查通风系统运行状态及车厢密封情况',
       '{"precondition_ventilation_min": 20}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_AQ_CO2');

INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
SELECT 'WARN_AQ_PM25', '空气质量-PM2.5颗粒物浓度预警', 'air_quality',
       '<75ug/m³', '—', '≥75ug/m³',
       '>', 75.0, 75.0, 1200,
       'ug/m³', 'PM2.5颗粒物浓度超标，请检查空调过滤系统',
       '{"precondition_ventilation_min": 20}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_AQ_PM25');

INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
SELECT 'WARN_AQ_PM10', '空气质量-PM10颗粒物浓度预警', 'air_quality',
       '<150ug/m³', '—', '≥150ug/m³',
       '>', 150.0, 150.0, 1200,
       'ug/m³', 'PM10颗粒物浓度超标，请检查空调过滤系统及车厢密封',
       '{"precondition_ventilation_min": 20}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_AQ_PM10');

INSERT INTO hvac.warning_config
    (warn_code, component_name, category, threshold_good, threshold_normal, threshold_bad,
     trigger_operator, trigger_value, clear_value, duration_seconds, unit, strategy, params)
SELECT 'WARN_AQ_TVOC', '空气质量-TVOC浓度预警', 'air_quality',
       '<600ug/m³', '—', '≥600ug/m³',
       '>', 600.0, 600.0, 1200,
       'ug/m³', 'TVOC浓度超标，请检查车厢内污染源及通风换气状况',
       '{"precondition_ventilation_min": 20}'::jsonb
WHERE NOT EXISTS (SELECT 1 FROM hvac.warning_config WHERE warn_code = 'WARN_AQ_TVOC');
