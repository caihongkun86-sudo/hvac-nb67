import type { Generated } from 'kysely';

/**
 * TimescaleDB 中的 hvac.fact_raw 表结构定义
 * 与 baseEnv/init-db/01-init.sql 严格对齐
 */
export interface FactRawTable {
    event_time: Date;
    ingest_time: Date;
    line_id: number;
    train_id: number;
    carriage_id: number;
    device_id: string;

    // 核心环境参数与传感器数据
    ras_u1: number | null;
    ras_u2: number | null;
    fas_u1: number | null;
    fas_u2: number | null;
    presdiff_u1: number | null;
    presdiff_u2: number | null;

    // 空气质量 (AQ)
    aq_co2_u1: number | null;
    aq_tvoc_u1: number | null;
    aq_pm2_5_u1: number | null;
    aq_pm10_u1: number | null;
    aq_co2_u2: number | null;
    aq_tvoc_u2: number | null;
    aq_pm2_5_u2: number | null;
    aq_pm10_u2: number | null;

    // 机组压缩机数据 (样例仅展示部分)
    f_cp_u11: number | null;
    i_cp_u11: number | null;
    highpress_u11: number | null;

    // 故障状态 (Boolean Flags)
    // 过流保护
    bocflt_ef_u11: boolean | null; bocflt_ef_u12: boolean | null;
    bocflt_ef_u21: boolean | null; bocflt_ef_u22: boolean | null;
    bocflt_cf_u11: boolean | null; bocflt_cf_u12: boolean | null;
    bocflt_cf_u21: boolean | null; bocflt_cf_u22: boolean | null;

    // 压缩机故障
    blpflt_comp_u11: boolean | null; blpflt_comp_u12: boolean | null;
    blpflt_comp_u21: boolean | null; blpflt_comp_u22: boolean | null;
    bscflt_comp_u11: boolean | null; bscflt_comp_u12: boolean | null;
    bscflt_comp_u21: boolean | null; bscflt_comp_u22: boolean | null;

    // 其他系统故障
    bflt_tempover: boolean | null;
    bflt_diffpres_u1: boolean | null; bflt_diffpres_u2: boolean | null;
    bflt_emergivt: boolean | null;
    bflt_powersupply_u1: boolean | null; bflt_powersupply_u2: boolean | null;
    bflt_tcms: boolean | null;
    bflt_fad_u11: boolean | null; bflt_fad_u12: boolean | null;
    bflt_rad_u11: boolean | null; bflt_rad_u12: boolean | null;
    bflt_airmon_u1: boolean | null; bflt_airmon_u2: boolean | null;
    bflt_currentmon: boolean | null;
    bflt_exhaustval: boolean | null;

    // 元数据
    parser_version: string | null;
    quality_code: number | null;
    payload_json: any;
}

export interface FactEventTable {
    event_time: Date;
    ingest_time: Date;
    line_id: number | null;
    train_id: number | null;
    carriage_id: number | null;
    device_id: string;
    event_type: string | null;
    fault_code: string;
    fault_name: string | null;
    severity: number | null;
    status: string | null;
    recovery_time: Date | null;
    payload_json: any | null;
}

export interface DimAlarmMaskTable {
    device_id: string;
    fault_code: string;
    masked_at: Generated<Date>;
}

/**
 * alarm_lifecycle 表：告警生命周期（对齐 dist/init-db/09-migration-alarm-lifecycle.sql）
 * 一条告警从开始到结束只有一行，end_time IS NULL 表示活跃中。
 */
export interface AlarmLifecycleTable {
    id: Generated<string>;
    device_id: string;
    line_id: number | null;
    train_id: number | null;
    carriage_id: number | null;
    unit_id: number | null;
    fault_code: string;
    fault_name: string | null;
    severity: number | null;
    start_time: Date;
    end_time: Date | null;
    last_seen_time: Date;
    trigger_snapshot: any | null;
    source: string | null;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/**
 * warning_lifecycle 表：预警生命周期（对齐 dist/init-db/08-migration-20260603.sql）
 * 一条预警从开始到结束只有一行，end_time IS NULL 表示活跃中。
 */
export interface WarningLifecycleTable {
    id: Generated<string>;
    device_id: string;
    line_id: number | null;
    train_id: number | null;
    carriage_id: number | null;
    unit_id: number | null;
    fault_code: string;
    fault_name: string | null;
    warn_code: string | null;
    severity: number | null;
    start_time: Date;
    end_time: Date | null;
    last_seen_time: Date;
    trigger_snapshot: any | null;
    source: string | null;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/**
 * warning_config 表：预警条件配置（对齐 dist/init-db/02-migration-20260504.sql）
 */
export interface WarningConfigTable {
    id: Generated<number>;
    warn_code: string;
    component_name: string;
    category: string | null;
    threshold_good: string | null;
    threshold_normal: string | null;
    threshold_bad: string | null;
    trigger_operator: string;
    trigger_value: number;
    clear_value: number | null;
    duration_seconds: number;
    unit: string | null;
    strategy: string | null;
    params: any | null;
    enabled: boolean;
    created_at: Generated<Date>;
    updated_at: Generated<Date>;
}

/**
 * 数据库 Schema 统一入口
 */
export interface Database {
    'hvac.fact_raw': FactRawTable;
    'hvac.fact_event': FactEventTable;
    'hvac.dim_alarm_mask': DimAlarmMaskTable;
    'hvac.alarm_lifecycle': AlarmLifecycleTable;
    'hvac.warning_lifecycle': WarningLifecycleTable;
    'hvac.warning_config': WarningConfigTable;
}
