import { db } from '../config/db.js';
import { sql } from 'kysely';
import { config } from '../config/index.js';

// 参数趋势分析支持的字段列表及其映射配置
// label: 前端显示名, col: DB列名, scale: 原始值÷scale=真实值
export const TREND_PARAM_DEFS: Record<string, { label: string; col: string; scale: number; unit: string }> = {
    ras_u1:      { label: '回风温度(机组1)', col: 'ras_u1',      scale: 10, unit: '℃' },
    ras_u2:      { label: '回风温度(机组2)', col: 'ras_u2',      scale: 10, unit: '℃' },
    fas_u1:      { label: '新风温度(机组1)', col: 'fas_u1',      scale: 10, unit: '℃' },
    fas_u2:      { label: '新风温度(机组2)', col: 'fas_u2',      scale: 10, unit: '℃' },
    tic:         { label: '客室温度',        col: "payload_json->'raw'->>'Tic'", scale: 10, unit: '℃' },
    // 压缩机电流 (4个系统)
    i_cp_u11:    { label: '压缩机1电流(机组1)', col: 'i_cp_u11', scale: 10, unit: 'A' },
    i_cp_u12:    { label: '压缩机2电流(机组1)', col: 'i_cp_u12', scale: 10, unit: 'A' },
    i_cp_u21:    { label: '压缩机1电流(机组2)', col: 'i_cp_u21', scale: 10, unit: 'A' },
    i_cp_u22:    { label: '压缩机2电流(机组2)', col: 'i_cp_u22', scale: 10, unit: 'A' },
    // 压缩机频率 (4个系统)
    f_cp_u11:    { label: '压缩机1频率(机组1)', col: 'f_cp_u11', scale: 10, unit: 'Hz' },
    f_cp_u12:    { label: '压缩机2频率(机组1)', col: 'f_cp_u12', scale: 10, unit: 'Hz' },
    f_cp_u21:    { label: '压缩机1频率(机组2)', col: 'f_cp_u21', scale: 10, unit: 'Hz' },
    f_cp_u22:    { label: '压缩机2频率(机组2)', col: 'f_cp_u22', scale: 10, unit: 'Hz' },
    // 吸气压力 (4个系统)
    suckp_u11:   { label: '吸气压力(机组1系统1)', col: 'suckp_u11', scale: 10, unit: 'bar' },
    suckp_u12:   { label: '吸气压力(机组1系统2)', col: 'suckp_u12', scale: 10, unit: 'bar' },
    suckp_u21:   { label: '吸气压力(机组2系统1)', col: 'suckp_u21', scale: 10, unit: 'bar' },
    suckp_u22:   { label: '吸气压力(机组2系统2)', col: 'suckp_u22', scale: 10, unit: 'bar' },
    // 高压 (4个系统)
    highpress_u11: { label: '高压(机组1系统1)', col: 'highpress_u11', scale: 10, unit: 'bar' },
    highpress_u12: { label: '高压(机组1系统2)', col: 'highpress_u12', scale: 10, unit: 'bar' },
    highpress_u21: { label: '高压(机组2系统1)', col: 'highpress_u21', scale: 10, unit: 'bar' },
    highpress_u22: { label: '高压(机组2系统2)', col: 'highpress_u22', scale: 10, unit: 'bar' },
    // 滤网压差
    presdiff_u1: { label: '滤网压差(机组1)', col: 'presdiff_u1', scale: 10, unit: 'pa' },
    presdiff_u2: { label: '滤网压差(机组2)', col: 'presdiff_u2', scale: 10, unit: 'pa' },
    // 送风温度 (payload_json，无独立DB列)
    sas_u11:     { label: '送风温度1(机组1)', col: "payload_json->'raw'->>'SasU11'", scale: 10, unit: '℃' },
    sas_u12:     { label: '送风温度2(机组1)', col: "payload_json->'raw'->>'SasU12'", scale: 10, unit: '℃' },
    sas_u21:     { label: '送风温度1(机组2)', col: "payload_json->'raw'->>'SasU21'", scale: 10, unit: '℃' },
    sas_u22:     { label: '送风温度2(机组2)', col: "payload_json->'raw'->>'SasU22'", scale: 10, unit: '℃' },
    // 吸气温度 (payload_json)
    suckt_u11:   { label: '吸气温度(机组1系统1)', col: "payload_json->'raw'->>'SuckTU11'", scale: 10, unit: '℃' },
    suckt_u12:   { label: '吸气温度(机组1系统2)', col: "payload_json->'raw'->>'SuckTU12'", scale: 10, unit: '℃' },
    suckt_u21:   { label: '吸气温度(机组2系统1)', col: "payload_json->'raw'->>'SuckTU21'", scale: 10, unit: '℃' },
    suckt_u22:   { label: '吸气温度(机组2系统2)', col: "payload_json->'raw'->>'SuckTU22'", scale: 10, unit: '℃' },
    // 过热度 (payload_json)
    sp_u11:      { label: '过热度(机组1系统1)', col: "payload_json->'raw'->>'SpU11'", scale: 10, unit: '℃' },
    sp_u12:      { label: '过热度(机组1系统2)', col: "payload_json->'raw'->>'SpU12'", scale: 10, unit: '℃' },
    sp_u21:      { label: '过热度(机组2系统1)', col: "payload_json->'raw'->>'SpU21'", scale: 10, unit: '℃' },
    sp_u22:      { label: '过热度(机组2系统2)', col: "payload_json->'raw'->>'SpU22'", scale: 10, unit: '℃' },
    // 压缩机电压 (payload_json)
    v_cp_u11:    { label: '压缩机1电压(机组1)', col: "payload_json->'raw'->>'VCpU11'", scale: 10, unit: 'V' },
    v_cp_u12:    { label: '压缩机2电压(机组1)', col: "payload_json->'raw'->>'VCpU12'", scale: 10, unit: 'V' },
    v_cp_u21:    { label: '压缩机1电压(机组2)', col: "payload_json->'raw'->>'VCpU21'", scale: 10, unit: 'V' },
    v_cp_u22:    { label: '压缩机2电压(机组2)', col: "payload_json->'raw'->>'VCpU22'", scale: 10, unit: 'V' },
    // 冷凝风机电流 (payload_json)
    i_cf_u11:    { label: '冷凝风机1电流(机组1)', col: "payload_json->'raw'->>'ICfU11'", scale: 10, unit: 'A' },
    i_cf_u12:    { label: '冷凝风机2电流(机组1)', col: "payload_json->'raw'->>'ICfU12'", scale: 10, unit: 'A' },
    i_cf_u21:    { label: '冷凝风机1电流(机组2)', col: "payload_json->'raw'->>'ICfU21'", scale: 10, unit: 'A' },
    i_cf_u22:    { label: '冷凝风机2电流(机组2)', col: "payload_json->'raw'->>'ICfU22'", scale: 10, unit: 'A' },
    // 通风机电流 (payload_json)
    i_ef_u11:    { label: '通风机1电流(机组1)', col: "payload_json->'raw'->>'IEfU11'", scale: 10, unit: 'A' },
    i_ef_u12:    { label: '通风机2电流(机组1)', col: "payload_json->'raw'->>'IEfU12'", scale: 10, unit: 'A' },
    i_ef_u21:    { label: '通风机1电流(机组2)', col: "payload_json->'raw'->>'IEfU21'", scale: 10, unit: 'A' },
    i_ef_u22:    { label: '通风机2电流(机组2)', col: "payload_json->'raw'->>'IEfU22'", scale: 10, unit: 'A' },
    // 废排风机电流 (payload_json)
    i_exufan:    { label: '废排风机电流', col: "payload_json->'raw'->>'IEXUFan'", scale: 10, unit: 'A' },
    // 空气质量 (无需缩放)
    aq_co2_u1:   { label: 'CO₂浓度(机组1)',  col: 'aq_co2_u1',  scale: 1,  unit: 'ppm' },
    aq_co2_u2:   { label: 'CO₂浓度(机组2)',  col: 'aq_co2_u2',  scale: 1,  unit: 'ppm' },
    aq_pm2_5_u1: { label: 'PM2.5(机组1)',    col: 'aq_pm2_5_u1', scale: 1, unit: 'ug/m³' },
    aq_pm2_5_u2: { label: 'PM2.5(机组2)',    col: 'aq_pm2_5_u2', scale: 1, unit: 'ug/m³' },
    aq_pm10_u1:  { label: 'PM10(机组1)',     col: 'aq_pm10_u1',  scale: 1, unit: 'ug/m³' },
    aq_pm10_u2:  { label: 'PM10(机组2)',     col: 'aq_pm10_u2',  scale: 1, unit: 'ug/m³' },
    aq_tvoc_u1:  { label: 'TVOC(机组1)',     col: 'aq_tvoc_u1',  scale: 1, unit: 'ppb' },
    aq_tvoc_u2:  { label: 'TVOC(机组2)',     col: 'aq_tvoc_u2',  scale: 1, unit: 'ppb' },
};

export class HistoryRepository {
    private static get timeCol() {
        return config.runtime === 'DEV' ? 'ingest_time' : 'event_time';
    }

    // B6: 多参数趋势查询（params 为 TREND_PARAM_DEFS 的 key 数组）
    static async getParamTrend(
        trainId: number,
        carriageId: number,
        type: string,
        params: string[]
    ) {
        const typeMap: Record<string, { bucketSize: string; lookback: string }> = {
            hour:  { bucketSize: '10 seconds', lookback: '1 hour' },
            day:   { bucketSize: '1 minute',   lookback: '24 hours' },
            week:  { bucketSize: '15 minutes', lookback: '168 hours' },
            month: { bucketSize: '30 minutes', lookback: '720 hours' },
        };
        const { bucketSize, lookback } = typeMap[type] || typeMap.hour;

        // 只保留白名单内的参数，防止 SQL 注入
        const validParams = params.filter(p => TREND_PARAM_DEFS[p]);
        if (validParams.length === 0) validParams.push('ras_u1', 'fas_u1', 'tic');

        const selects: any[] = [
            sql.raw(`time_bucket('${bucketSize}', ${this.timeCol})`).as('bucket'),
        ];
        for (const key of validParams) {
            const def = TREND_PARAM_DEFS[key];
            // ROUND(x, n) 要求 x 为 numeric，不接受 double precision
            const colExpr = def.col.includes("->")
                ? `(${def.col})::numeric`
                : `${def.col}::numeric`;
            selects.push(
                sql.raw(`ROUND(AVG(${colExpr}) / ${def.scale}, 1)`).as(key)
            );
        }

        return await db
            .selectFrom('hvac.fact_raw')
            .select(selects)
            .where('train_id', '=', trainId)
            .where('carriage_id', '=', carriageId)
            .where(sql.raw(this.timeCol), '>', sql`NOW() - INTERVAL '${sql.raw(lookback)}'`)
            .groupBy('bucket')
            .orderBy('bucket', 'asc')
            .execute();
    }

    // B5: 预警发生时间前后各1小时参数曲线（固定查与该预警相关的参数）
    static async getPredictDetailCurve(
        trainId: number,
        carriageId: number,
        triggerTime: Date,
        warnCode: string
    ) {
        const startTime = new Date(triggerTime.getTime() - 60 * 60 * 1000);
        const endTime   = new Date(triggerTime.getTime() + 60 * 60 * 1000);

        // HVAC 预警码格式：HVAC{carriage_id*100+seq}，seq%100 还原类型号
        // 根据 seq 决定显示哪些相关参数，让趋势图与预警判断逻辑对应
        const paramsBySeq: Record<number, string[]> = {
            1:  ['suckp_u11', 'suckp_u12', 'highpress_u11', 'highpress_u12'],  // 机组1冷媒泄漏
            2:  ['suckp_u11', 'suckp_u12', 'highpress_u11', 'highpress_u12'],
            3:  ['suckp_u21', 'suckp_u22', 'highpress_u21', 'highpress_u22'],  // 机组2冷媒泄漏
            4:  ['suckp_u21', 'suckp_u22', 'highpress_u21', 'highpress_u22'],
            5:  ['i_cp_u11', 'i_cp_u12', 'f_cp_u11', 'f_cp_u12'],              // 机组1制冷系统
            6:  ['i_cp_u21', 'i_cp_u22', 'f_cp_u21', 'f_cp_u22'],              // 机组2制冷系统
            7:  ['fas_u1', 'fas_u2'],                                           // 新风温度差传感器
            8:  ['ras_u1', 'ras_u2'],                                           // 回风温度差传感器
            9:  ['ras_u1', 'ras_u2', 'tic'],                                   // 车厢超温
            10: ['presdiff_u1'],                                                // 机组1滤网脏堵
            11: ['presdiff_u2'],                                                // 机组2滤网脏堵
            12: ['i_ef_u11', 'i_ef_u12'],                                       // 机组1通风机电流
            13: ['i_ef_u11', 'i_ef_u12'],
            14: ['i_ef_u21', 'i_ef_u22'],                                       // 机组2通风机电流
            15: ['i_ef_u21', 'i_ef_u22'],
            16: ['i_cf_u11', 'i_cf_u12'],                                       // 机组1冷凝风机电流
            17: ['i_cf_u11', 'i_cf_u12'],
            18: ['i_cf_u21', 'i_cf_u22'],                                       // 机组2冷凝风机电流
            19: ['i_cf_u21', 'i_cf_u22'],
            20: ['i_exufan'],                                                   // 废排风机电流
            21: ['i_cp_u11', 'i_cp_u12', 'fas_u1'],                            // 机组1压缩机电流
            22: ['i_cp_u11', 'i_cp_u12', 'fas_u1'],
            23: ['i_cp_u21', 'i_cp_u22', 'fas_u2'],                            // 机组2压缩机电流
            24: ['i_cp_u21', 'i_cp_u22', 'fas_u2'],
            25: ['aq_co2_u1', 'aq_pm2_5_u1', 'aq_pm10_u1', 'aq_sp_u1', 'aq_tvoc_u1'],                   // 机组1空气质量
            26: ['aq_co2_u2', 'aq_pm2_5_u2', 'aq_pm10_u2', 'aq_sp_u2', 'aq_tvoc_u2'],                   // 机组2空气质量
        };

        let params: string[];
        const hvacMatch = warnCode.match(/^HVAC(\d+)$/i);
        if (hvacMatch && hvacMatch[1]) {
            const seq = parseInt(hvacMatch[1]) % 100;
            params = paramsBySeq[seq] || ['ras_u1', 'fas_u1', 'tic'];
        } else {
            params = ['ras_u1', 'fas_u1', 'tic'];
        }
        const validParams = params.filter(p => TREND_PARAM_DEFS[p]);

        // 使用 time_bucket 聚合，确保2小时窗口完整展示
        // 10秒桶 → 2小时最多720个点，不受 LIMIT 截断影响
        const bucketSize = '10 seconds';
        const selects: any[] = [
            sql.raw(`time_bucket('${bucketSize}', ${this.timeCol})`).as('bucket'),
        ];
        for (const key of validParams) {
            const def = TREND_PARAM_DEFS[key]!;
            const colExpr = def.col.includes("->")
                ? `(${def.col})::numeric`
                : `${def.col}::numeric`;
            selects.push(sql.raw(`ROUND(AVG(${colExpr}) / ${def.scale}, 1)`).as(key));
        }

        const rows = await db
            .selectFrom('hvac.fact_raw')
            .select(selects)
            .where('train_id', '=', trainId)
            .where('carriage_id', '=', carriageId)
            .where(this.timeCol as any, '>=', startTime)
            .where(this.timeCol as any, '<=', endTime)
            .groupBy('bucket')
            .orderBy('bucket', 'asc')
            .execute();

        return {
            params: validParams.map(k => ({ key: k, ...TREND_PARAM_DEFS[k] })),
            triggerTime: triggerTime.toISOString(),
            data: rows,
        };
    }

    static async getLatestStatus(deviceId: string) {
        return await db
            .selectFrom('hvac.fact_raw')
            .selectAll()
            .where('device_id', '=', deviceId)
            .orderBy(this.timeCol as any, 'desc')
            .limit(1)
            .executeTakeFirst();
    }
}
