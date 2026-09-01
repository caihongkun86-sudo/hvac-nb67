import { db } from '../config/db.js';
import { sql } from 'kysely';
import { config } from '../config/index.js';

// ============================================================
// 参数趋势分析支持的字段列表及其映射配置
// label: 前端显示名, col: DB列名, scale: 原始值÷scale=真实值
// viewCol: 连续聚合视图中的列名（物化视图已预聚合除过scale）
// ============================================================
export const TREND_PARAM_DEFS: Record<string, {
    label: string;
    col: string;
    scale: number;
    unit: string;
    viewCol: string;  // 物化视图中的列名
}> = {
    ras_u1:      { label: '回风温度(机组1)', col: 'ras_u1',      scale: 10, unit: '℃', viewCol: 'ras_u1' },
    ras_u2:      { label: '回风温度(机组2)', col: 'ras_u2',      scale: 10, unit: '℃', viewCol: 'ras_u2' },
    fas_u1:      { label: '新风温度(机组1)', col: 'fas_u1',      scale: 10, unit: '℃', viewCol: 'fas_u1' },
    fas_u2:      { label: '新风温度(机组2)', col: 'fas_u2',      scale: 10, unit: '℃', viewCol: 'fas_u2' },
    tic:         { label: '设定温度',        col: "payload_json->'raw'->>'Tic'", scale: 10, unit: '℃', viewCol: 'tic' },
    tveh1:       { label: '车厢温度-1',      col: "payload_json->'raw'->>'Tveh1'", scale: 10, unit: '℃', viewCol: 'tveh1' },
    tveh2:       { label: '车厢温度-2',      col: "payload_json->'raw'->>'Tveh2'", scale: 10, unit: '℃', viewCol: 'tveh2' },
    // 压缩机电流
    i_cp_u11:    { label: '压缩机1电流(机组1)', col: 'i_cp_u11', scale: 10, unit: 'A', viewCol: 'i_cp_u11' },
    i_cp_u12:    { label: '压缩机2电流(机组1)', col: 'i_cp_u12', scale: 10, unit: 'A', viewCol: 'i_cp_u12' },
    i_cp_u21:    { label: '压缩机1电流(机组2)', col: 'i_cp_u21', scale: 10, unit: 'A', viewCol: 'i_cp_u21' },
    i_cp_u22:    { label: '压缩机2电流(机组2)', col: 'i_cp_u22', scale: 10, unit: 'A', viewCol: 'i_cp_u22' },
    // 压缩机频率
    f_cp_u11:    { label: '压缩机1频率(机组1)', col: 'f_cp_u11', scale: 10, unit: 'Hz', viewCol: 'f_cp_u11' },
    f_cp_u12:    { label: '压缩机2频率(机组1)', col: 'f_cp_u12', scale: 10, unit: 'Hz', viewCol: 'f_cp_u12' },
    f_cp_u21:    { label: '压缩机1频率(机组2)', col: 'f_cp_u21', scale: 10, unit: 'Hz', viewCol: 'f_cp_u21' },
    f_cp_u22:    { label: '压缩机2频率(机组2)', col: 'f_cp_u22', scale: 10, unit: 'Hz', viewCol: 'f_cp_u22' },
    // 吸气压力
    suckp_u11:   { label: '吸气压力(机组1系统1)', col: 'suckp_u11', scale: 10, unit: 'bar', viewCol: 'suckp_u11' },
    suckp_u12:   { label: '吸气压力(机组1系统2)', col: 'suckp_u12', scale: 10, unit: 'bar', viewCol: 'suckp_u12' },
    suckp_u21:   { label: '吸气压力(机组2系统1)', col: 'suckp_u21', scale: 10, unit: 'bar', viewCol: 'suckp_u21' },
    suckp_u22:   { label: '吸气压力(机组2系统2)', col: 'suckp_u22', scale: 10, unit: 'bar', viewCol: 'suckp_u22' },
    // 高压
    highpress_u11: { label: '高压(机组1系统1)', col: 'highpress_u11', scale: 10, unit: 'bar', viewCol: 'highpress_u11' },
    highpress_u12: { label: '高压(机组1系统2)', col: 'highpress_u12', scale: 10, unit: 'bar', viewCol: 'highpress_u12' },
    highpress_u21: { label: '高压(机组2系统1)', col: 'highpress_u21', scale: 10, unit: 'bar', viewCol: 'highpress_u21' },
    highpress_u22: { label: '高压(机组2系统2)', col: 'highpress_u22', scale: 10, unit: 'bar', viewCol: 'highpress_u22' },
    // 滤网压差
    presdiff_u1: { label: '滤网压差(机组1)', col: 'presdiff_u1', scale: 10, unit: 'pa', viewCol: 'presdiff_u1' },
    presdiff_u2: { label: '滤网压差(机组2)', col: 'presdiff_u2', scale: 10, unit: 'pa', viewCol: 'presdiff_u2' },
    // 送风温度（JSONB）
    sas_u11:     { label: '送风温度1(机组1)', col: "payload_json->'raw'->>'SasU11'", scale: 10, unit: '℃', viewCol: 'sas_u11' },
    sas_u12:     { label: '送风温度2(机组1)', col: "payload_json->'raw'->>'SasU12'", scale: 10, unit: '℃', viewCol: 'sas_u12' },
    sas_u21:     { label: '送风温度1(机组2)', col: "payload_json->'raw'->>'SasU21'", scale: 10, unit: '℃', viewCol: 'sas_u21' },
    sas_u22:     { label: '送风温度2(机组2)', col: "payload_json->'raw'->>'SasU22'", scale: 10, unit: '℃', viewCol: 'sas_u22' },
    // 吸气温度（JSONB）
    suckt_u11:   { label: '吸气温度(机组1系统1)', col: "payload_json->'raw'->>'SucktU11'", scale: 10, unit: '℃', viewCol: 'suckt_u11' },
    suckt_u12:   { label: '吸气温度(机组1系统2)', col: "payload_json->'raw'->>'SucktU12'", scale: 10, unit: '℃', viewCol: 'suckt_u12' },
    suckt_u21:   { label: '吸气温度(机组2系统1)', col: "payload_json->'raw'->>'SucktU21'", scale: 10, unit: '℃', viewCol: 'suckt_u21' },
    suckt_u22:   { label: '吸气温度(机组2系统2)', col: "payload_json->'raw'->>'SucktU22'", scale: 10, unit: '℃', viewCol: 'suckt_u22' },
    // 过热度（JSONB）
    sp_u11:      { label: '过热度(机组1系统1)', col: "payload_json->'raw'->>'SpU11'", scale: 10, unit: '℃', viewCol: 'sp_u11' },
    sp_u12:      { label: '过热度(机组1系统2)', col: "payload_json->'raw'->>'SpU12'", scale: 10, unit: '℃', viewCol: 'sp_u12' },
    sp_u21:      { label: '过热度(机组2系统1)', col: "payload_json->'raw'->>'SpU21'", scale: 10, unit: '℃', viewCol: 'sp_u21' },
    sp_u22:      { label: '过热度(机组2系统2)', col: "payload_json->'raw'->>'SpU22'", scale: 10, unit: '℃', viewCol: 'sp_u22' },
    // 压缩机电压（JSONB）
    v_cp_u11:    { label: '压缩机1电压(机组1)', col: "payload_json->'raw'->>'VCpU11'", scale: 10, unit: 'V', viewCol: 'v_cp_u11' },
    v_cp_u12:    { label: '压缩机2电压(机组1)', col: "payload_json->'raw'->>'VCpU12'", scale: 10, unit: 'V', viewCol: 'v_cp_u12' },
    v_cp_u21:    { label: '压缩机1电压(机组2)', col: "payload_json->'raw'->>'VCpU21'", scale: 10, unit: 'V', viewCol: 'v_cp_u21' },
    v_cp_u22:    { label: '压缩机2电压(机组2)', col: "payload_json->'raw'->>'VCpU22'", scale: 10, unit: 'V', viewCol: 'v_cp_u22' },
    // 冷凝风机电流（JSONB）
    i_cf_u11:    { label: '冷凝风机1电流(机组1)', col: "payload_json->'raw'->>'ICfU11'", scale: 10, unit: 'A', viewCol: 'i_cf_u11' },
    i_cf_u12:    { label: '冷凝风机2电流(机组1)', col: "payload_json->'raw'->>'ICfU12'", scale: 10, unit: 'A', viewCol: 'i_cf_u12' },
    i_cf_u21:    { label: '冷凝风机1电流(机组2)', col: "payload_json->'raw'->>'ICfU21'", scale: 10, unit: 'A', viewCol: 'i_cf_u21' },
    i_cf_u22:    { label: '冷凝风机2电流(机组2)', col: "payload_json->'raw'->>'ICfU22'", scale: 10, unit: 'A', viewCol: 'i_cf_u22' },
    // 通风机电流（JSONB）
    i_ef_u11:    { label: '通风机1电流(机组1)', col: "payload_json->'raw'->>'IEfU11'", scale: 10, unit: 'A', viewCol: 'i_ef_u11' },
    i_ef_u12:    { label: '通风机2电流(机组1)', col: "payload_json->'raw'->>'IEfU12'", scale: 10, unit: 'A', viewCol: 'i_ef_u12' },
    i_ef_u21:    { label: '通风机1电流(机组2)', col: "payload_json->'raw'->>'IEfU21'", scale: 10, unit: 'A', viewCol: 'i_ef_u21' },
    i_ef_u22:    { label: '通风机2电流(机组2)', col: "payload_json->'raw'->>'IEfU22'", scale: 10, unit: 'A', viewCol: 'i_ef_u22' },
    // 废排风机电流（JSONB）
    i_exufan:    { label: '废排风机电流', col: "payload_json->'raw'->>'IExufan'", scale: 10, unit: 'A', viewCol: 'i_exufan' },
    // 空气质量（无需缩放）
    aq_co2_u1:   { label: 'CO₂浓度(机组1)',  col: 'aq_co2_u1',  scale: 1,  unit: 'ppm', viewCol: 'aq_co2_u1' },
    aq_co2_u2:   { label: 'CO₂浓度(机组2)',  col: 'aq_co2_u2',  scale: 1,  unit: 'ppm', viewCol: 'aq_co2_u2' },
    aq_pm2_5_u1: { label: 'PM2.5(机组1)',    col: 'aq_pm2_5_u1', scale: 1, unit: 'ug/m³', viewCol: 'aq_pm2_5_u1' },
    aq_pm2_5_u2: { label: 'PM2.5(机组2)',    col: 'aq_pm2_5_u2', scale: 1, unit: 'ug/m³', viewCol: 'aq_pm2_5_u2' },
    aq_pm10_u1:  { label: 'PM10(机组1)',     col: 'aq_pm10_u1',  scale: 1, unit: 'ug/m³', viewCol: 'aq_pm10_u1' },
    aq_pm10_u2:  { label: 'PM10(机组2)',     col: 'aq_pm10_u2',  scale: 1, unit: 'ug/m³', viewCol: 'aq_pm10_u2' },
    aq_tvoc_u1:  { label: 'TVOC(机组1)',     col: 'aq_tvoc_u1',  scale: 1, unit: 'ppb', viewCol: 'aq_tvoc_u1' },
    aq_tvoc_u2:  { label: 'TVOC(机组2)',     col: 'aq_tvoc_u2',  scale: 1, unit: 'ppb', viewCol: 'aq_tvoc_u2' },
};

// 查询类型到物化视图的映射
const VIEW_MAP: Record<string, { view: string; bucketSize: string; lookback: string; useView: boolean }> = {
    hour:  { view: 'hvac.fact_raw',       bucketSize: '10 seconds', lookback: '1 hour',   useView: false },
    day:   { view: 'hvac.fact_raw',       bucketSize: '1 minute',   lookback: '24 hours', useView: false },
    week:  { view: 'hvac.fact_raw_15min', bucketSize: '15 minutes', lookback: '168 hours', useView: true },
    month: { view: 'hvac.fact_raw_15min', bucketSize: '30 minutes', lookback: '720 hours', useView: true },
};

// 查询超时配置（毫秒）
const QUERY_TIMEOUT_MS = 30000;

// fact_raw_1min 物化视图可用性缓存：
// 42P01（视图不存在）时缓存 60s，期间直接走原始表，避免每次请求都白打一条必失败的查询
let factRaw1minState: { available: boolean; checkedAt: number } | null = null;
const VIEW_CHECK_TTL_MS = 60_000;

export class HistoryRepository {
    private static get timeCol() {
        return config.runtime === 'DEV' ? 'ingest_time' : 'event_time';
    }

    /**
     * B6: 多参数趋势查询（使用连续聚合视图优化）
     *
     * 路由策略：
     * - hour:  原始表（需要10秒精度）
     * - day:   1min 物化视图
     * - week:  15min 物化视图
     * - month: 15min 物化视图
     *
     * 物化视图查询失败时自动降级到原始表
     */
    static async getParamTrend(
        trainId: number,
        carriageId: number,
        type: string,
        params: string[]
    ) {
        const viewConfig = VIEW_MAP[type] || VIEW_MAP.hour;
        const { bucketSize, lookback, useView } = viewConfig;

        // 只保留白名单内的参数，防止 SQL 注入
        const validParams = params.filter(p => TREND_PARAM_DEFS[p]);
        if (validParams.length === 0) validParams.push('ras_u1', 'fas_u1', 'tic');

        // 限制最大参数数量
        const limitedParams = validParams.slice(0, 8);

        // 构建查询
        const buildQuery = (tableName: string, isView: boolean) => {
            const selects: any[] = [
                sql.raw(`time_bucket('${bucketSize}', ${isView ? 'bucket' : this.timeCol})`).as('bucket'),
            ];

            for (const key of limitedParams) {
                const def = TREND_PARAM_DEFS[key];
                if (isView) {
                    // 物化视图：列已预聚合且除过 scale
                    selects.push(sql.raw(`ROUND(AVG(${def.viewCol}), 1)`).as(key));
                } else {
                    // 原始表：需要实时聚合
                    const colExpr = def.col.includes("->")
                        ? `(${def.col})::numeric`
                        : `${def.col}::numeric`;
                    selects.push(sql.raw(`ROUND(AVG(${colExpr}) / ${def.scale}, 1)`).as(key));
                }
            }

            const timeFilterCol = isView ? 'bucket' : this.timeCol;
            return db
                .selectFrom(tableName as any)
                .select(selects)
                .where('train_id', '=', trainId)
                .where('carriage_id', '=', carriageId)
                .where(sql.raw(timeFilterCol), '>', sql`NOW() - INTERVAL '${sql.raw(lookback)}'`)
                .groupBy('bucket')
                .orderBy('bucket', 'asc');
        };

        // 优先使用物化视图，失败时降级到原始表
        let rows: any[];
        if (useView) {
            try {
                const query = buildQuery(viewConfig.view, true);
                rows = await this.executeWithTimeout(query.execute(), QUERY_TIMEOUT_MS);
            } catch (err: any) {
                console.warn(`[HistoryRepository] 物化视图查询失败，降级到原始表: ${err.message}`);
                const fallbackQuery = buildQuery('hvac.fact_raw', false);
                rows = await this.executeWithTimeout(fallbackQuery.execute(), QUERY_TIMEOUT_MS);
            }
        } else {
            // hour 模式直接查原始表
            const query = buildQuery('hvac.fact_raw', false);
            rows = await this.executeWithTimeout(query.execute(), QUERY_TIMEOUT_MS);
        }

        return rows;
    }

    /**
     * B5: 预警发生时间前后各1小时参数曲线
     *
     * 2小时窗口，bucket=60s。
     * 优先从物化视图 fact_raw_1min 查询（已预聚合，避免实时 JSONB 提取），
     * 失败时降级到原始表 fact_raw 实时聚合。
     */
    static async getPredictDetailCurve(
        trainId: number,
        carriageId: number,
        triggerTime: Date,
        warnCode: string
    ) {
        const startTime = new Date(triggerTime.getTime() - 60 * 60 * 1000);
        const endTime   = new Date(triggerTime.getTime() + 60 * 60 * 1000);

        // HVAC 预警码格式：HVAC{carriage_id*100+seq}
        const paramsBySeq: Record<number, string[]> = {
            1:  ['suckp_u11', 'suckp_u12', 'highpress_u11', 'highpress_u12'], // 机组1冷媒泄漏
            2:  ['suckp_u11', 'suckp_u12', 'highpress_u11', 'highpress_u12'],
            3:  ['suckp_u21', 'suckp_u22', 'highpress_u21', 'highpress_u22'], // 机组2冷媒泄漏
            4:  ['suckp_u21', 'suckp_u22', 'highpress_u21', 'highpress_u22'],
            5:  ['i_cp_u11', 'i_cp_u12', 'f_cp_u11', 'f_cp_u12'],             // 机组1制冷系统
            6:  ['i_cp_u21', 'i_cp_u22', 'f_cp_u21', 'f_cp_u22'],             // 机组2制冷系统
            7:  ['fas_u1', 'fas_u2'],                                         // 新风温度差传感器
            8:  ['ras_u1', 'ras_u2'],                                         // 回风温度差传感器
            9:  ['ras_u1', 'ras_u2', 'tveh1', 'tveh2', 'tic'],                // 车厢超温
            10: ['presdiff_u1'],                                              // 机组1滤网脏堵
            11: ['presdiff_u2'],                                              // 机组2滤网脏堵
            12: ['i_ef_u11', 'i_ef_u12'],                                     // 机组1通风机电流
            13: ['i_ef_u11', 'i_ef_u12'],
            14: ['i_ef_u21', 'i_ef_u22'],                                     // 机组2通风机电流
            15: ['i_ef_u21', 'i_ef_u22'],
            16: ['i_cf_u11', 'i_cf_u12'],                                     // 机组1冷凝风机电流
            17: ['i_cf_u11', 'i_cf_u12'],
            18: ['i_cf_u21', 'i_cf_u22'],                                     // 机组2冷凝风机电流
            19: ['i_cf_u21', 'i_cf_u22'],
            20: ['i_exufan'],                                                 // 废排风机电流
            21: ['i_cp_u11', 'i_cp_u12', 'fas_u1'],                           // 机组1压缩机电流
            22: ['i_cp_u11', 'i_cp_u12', 'fas_u1'],
            23: ['i_cp_u21', 'i_cp_u22', 'fas_u2'],                           // 机组2压缩机电流
            24: ['i_cp_u21', 'i_cp_u22', 'fas_u2'],
            // 注意：aq_sp_u1/u2 不在 TREND_PARAM_DEFS 中，已移除
            25: ['aq_co2_u1', 'aq_pm2_5_u1', 'aq_pm10_u1', 'aq_tvoc_u1'],     // 机组1空气质量
            26: ['aq_co2_u2', 'aq_pm2_5_u2', 'aq_pm10_u2', 'aq_tvoc_u2'],     // 机组2空气质量
            76: ['tveh1', 'tveh2'],                                           // 车厢温度传感器
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

        const bucketSize = '60 seconds';

        const buildQuery = (tableName: string, isView: boolean) => {
            const selects: any[] = [
                sql.raw(`time_bucket('${bucketSize}', ${isView ? 'bucket' : 'event_time'})`).as('bucket'),
            ];

            for (const key of validParams) {
                const def = TREND_PARAM_DEFS[key]!;
                if (isView) {
                    selects.push(sql.raw(`ROUND(AVG(${def.viewCol}), 1)`).as(key));
                } else {
                    const colExpr = def.col.includes("->")
                        ? `(${def.col})::numeric`
                        : `${def.col}::numeric`;
                    selects.push(sql.raw(`ROUND(AVG(${colExpr}) / ${def.scale}, 1)`).as(key));
                }
            }

            const timeFilterCol = isView ? 'bucket' : 'event_time';
            return db
                .selectFrom(tableName as any)
                .select(selects)
                .where('train_id', '=', trainId)
                .where('carriage_id', '=', carriageId)
                .where(sql.raw(timeFilterCol), '>=', startTime)
                .where(sql.raw(timeFilterCol), '<=', endTime)
                .groupBy('bucket')
                .orderBy('bucket', 'asc');
        };

        // 优先使用物化视图，失败或返回空时降级到原始表
        let rows: any[];
        const viewUsable =
            !factRaw1minState ||
            factRaw1minState.available ||
            Date.now() - factRaw1minState.checkedAt > VIEW_CHECK_TTL_MS;

        if (viewUsable) {
            try {
                const query = buildQuery('hvac.fact_raw_1min' as any, true);
                rows = await this.executeWithTimeout(query.execute(), QUERY_TIMEOUT_MS);
                if (rows.length === 0) {
                    throw new Error('empty result (not yet refreshed for this time range)');
                }
                factRaw1minState = { available: true, checkedAt: Date.now() };
            } catch (err: any) {
                if (err?.code === '42P01') {
                    factRaw1minState = { available: false, checkedAt: Date.now() };
                }
                console.warn(`[HistoryRepository] fact_raw_1min 物化视图查询失败，降级到原始表: ${err.message}`);
                const fallbackQuery = buildQuery('hvac.fact_raw', false);
                rows = await this.executeWithTimeout(fallbackQuery.execute(), QUERY_TIMEOUT_MS);
            }
        } else {
            const fallbackQuery = buildQuery('hvac.fact_raw', false);
            rows = await this.executeWithTimeout(fallbackQuery.execute(), QUERY_TIMEOUT_MS);
        }

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

    /**
     * 带超时的查询执行器
     */
    private static async executeWithTimeout<T>(
        promise: Promise<T>,
        timeoutMs: number
    ): Promise<T> {
        return Promise.race([
            promise,
            new Promise<T>((_, reject) =>
                setTimeout(() => reject(new Error(`QUERY_TIMEOUT_AFTER_${timeoutMs}ms`)), timeoutMs)
            ),
        ]);
    }
}
