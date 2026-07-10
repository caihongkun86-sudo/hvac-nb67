import { db } from '../config/db.js';
import { sql } from 'kysely';
import { config } from '../config/index.js';
import { formatTime } from '../utils/time.js';

/**
 * 实时状态与聚合查询
 */
export class StatusRepository {
    /**
     * 根据当前 RUNTIME 环境返回用于时序分析的列名
     * DEV: 使用解析时间 (ingest_time)
     * PRD: 使用设备时间 (event_time)
     */
    private static get timeCol() {
        return config.runtime === 'DEV' ? 'ingest_time' : 'event_time';
    }
    /**
     * 模拟原有的 /api/rest/AirSystem 结构
     * 返回各列车的报警统计摘要
     */
    /**
     * 实现客户要求的“删掉告警，直到修好再弹出来”的自动复位逻辑
     */
    private static async syncMasks() {
        // [自动复位逻辑]: 寻找那些已被屏蔽、但当前物理状态已变回 false (即修好了) 的位
        // 只有修好了，我们才把屏蔽记作删除，确保下次再坏时能重新提醒
        await sql`
            DELETE FROM hvac.dim_alarm_mask m
            USING (
                SELECT DISTINCT ON (device_id) device_id, payload_json->'raw' as raw
                FROM hvac.fact_raw
                ORDER BY device_id, ${sql.raw(this.timeCol)} DESC
            ) latest
            WHERE m.device_id = latest.device_id
            AND (latest.raw->>m.fault_code)::boolean = false
        `.execute(db);
    }

    static async getTrainAlarmSummary() {
        // 在聚合前同步屏蔽状态
        await this.syncMasks();

        // alarm_count 仍来自 fact_raw 最新帧的物理比特位（语义：当前帧瞬时报警状态）
        // warning_count 改为来自 hvac.warning_lifecycle 活跃行数（Bug B 修复 2026-06-04）
        // → 与 /api/rest/RealtimeWarning 列表数据源一致，
        //   前端"预警数"和"实时预警"表格不会再出现一边 0 一边 1 的诡异不一致。
        const [records, lifecycle] = await Promise.all([
            db
                .with('latest_status', (db) => db
                    .selectFrom('hvac.fact_raw')
                    .select(['train_id', 'device_id', 'payload_json'])
                    .distinctOn('device_id')
                    .orderBy('device_id')
                    .orderBy(this.timeCol as any, 'desc')
                )
                .selectFrom('latest_status')
                .select([
                    'train_id as train_no',
                    // 告警 (Alarm): 统计物理比特位，但排除掉"已被删除(屏蔽)"的位点
                    sql<number>`SUM(
                        (SELECT count(*) FROM jsonb_each_text(payload_json->'raw')
                         WHERE (key ILIKE 'Bflt%' OR key ILIKE 'Blp%' OR key ILIKE 'Bsc%' OR key ILIKE 'Boc%')
                         AND value = 'true'
                         AND NOT EXISTS (
                             SELECT 1 FROM hvac.dim_alarm_mask m
                             WHERE m.device_id = latest_status.device_id AND m.fault_code = key
                         ))
                    )`.as('alarm_count'),
                ])
                .groupBy('train_id')
                .execute(),
            // 预警 (Warning): 活跃 lifecycle 行数，per train
            db
                .selectFrom('hvac.warning_lifecycle' as any)
                .select(['train_id', sql<number>`count(*)`.as('warn_count')])
                .where('end_time', 'is', null)
                .groupBy('train_id' as any)
                .execute() as Promise<Array<{ train_id: number; warn_count: number }>>,
        ]);

        const warnByTrain: Record<number, number> = {};
        lifecycle.forEach((r) => {
            warnByTrain[Number(r.train_id)] = Number(r.warn_count);
        });

        return {
            vw_train_alarm_count: records.map(r => {
                const a = Number(r.alarm_count);
                const w = warnByTrain[Number(r.train_no)] ?? 0;
                return {
                    train_no: r.train_no,
                    alarm_count: a,
                    warning_count: w,
                    total_number: a + w
                };
            })
        };
    }

    /**
     * 屏蔽告警 API
     */
    static async maskAlarm(deviceId: string, faultCode: string) {
        return await db
            .insertInto('hvac.dim_alarm_mask')
            .values({ device_id: deviceId, fault_code: faultCode })
            .onConflict((oc) => oc.doNothing())
            .execute();
    }

    /**
     * 获取列车实时告警详情 (适配前端遍历逻辑)
     */
    static async getTrainDetails(trainIds: number[]) {
        let query = db
            .selectFrom('hvac.fact_event')
            .selectAll()
            // 实时故障去重：同一车厢同一编码只保留最新触发的一条（降序取第一条）
            .distinctOn(['train_id', 'carriage_id' as any, 'fault_code'])
            .where('event_type', '=', 'alarm')
            .where('recovery_time', 'is', null)
            .orderBy('train_id')
            .orderBy('carriage_id' as any)
            .orderBy('fault_code')
            .orderBy(this.timeCol as any, 'desc')
            .limit(200);

        if (trainIds && trainIds.length > 0 && !isNaN(Number(trainIds[0]))) {
            const validIds = trainIds.filter(id => id !== undefined && id !== null) as number[];
            if (validIds.length > 0) {
                query = query.where('train_id', 'in', validIds as any);
            }
        }

        const data = await query.execute();

        return data.map(row => ({
            [row.fault_code as string]: 1,
            [`${row.fault_code}_name`]: row.fault_name,
            alarm_time: formatTime((row as any)[this.timeCol]),
            carriage_no: row.carriage_id || 0,
            line_no: row.line_id || 0,
            train_no: row.train_id || 0
        }));
    }

    /**
     * 获取实时预警详情 (适配前端 Object.entries 逻辑)
     */
    // HVAC fault_code → warning_config.warn_code 映射
    // 冷媒泄露预警通过 _c（制冷模式）/_v（通风模式）后缀区分两个独立配置条目
    private static hvacSeqToWarnCode(seq: number, faultCode?: string): string | null {
        if (seq >= 1  && seq <= 4) {
            if (faultCode && faultCode.endsWith('_v')) return 'WARN_REFRIGERANT_LEAK_VENT';
            return 'WARN_REFRIGERANT_LEAK_COOLING';
        }
        if (seq >= 5  && seq <= 6)  return 'WARN_COOLING_SYSTEM';
        if (seq >= 7  && seq <= 8)  return 'WARN_TEMP_SENSOR';
        if (seq === 9)               return 'WARN_CABIN_OVERHEAT';
        if (seq >= 10 && seq <= 11) return 'WARN_FILTER_CLOG';
        if (seq >= 12 && seq <= 15) return 'WARN_EF_CURRENT';
        if (seq >= 16 && seq <= 19) return 'WARN_CF_CURRENT';
        if (seq === 20)              return 'WARN_EXUF_CURRENT';
        if (seq >= 21 && seq <= 24) return 'WARN_CP_CURRENT';
        if (seq === 25 || seq === 26) return 'WARN_AQ_CO2';
        return null;
    }

    static async getRealtimeWarnings(trainId?: number) {
        // Phase 4: 直接查 warning_lifecycle，end_time IS NULL = 活跃中。
        // 数据库部分唯一索引 UNIQUE(device_id, fault_code) WHERE end_time IS NULL 保证
        // 同设备同 fault_code 不可能有 2 行，去重在写入端，**这里不需要 distinctOn**。
        let query = db
            .selectFrom('hvac.warning_lifecycle' as any)
            .selectAll()
            .where('end_time', 'is', null)
            .orderBy('train_id' as any)
            .orderBy('carriage_id' as any)
            .orderBy('fault_code' as any)
            .limit(500);

        if (trainId && !isNaN(trainId)) {
            query = query.where('train_id' as any, '=', trainId);
        }

        const [data, configs] = await Promise.all([
            query.execute() as Promise<any[]>,
            db.selectFrom('hvac.warning_config' as any).selectAll().execute() as Promise<any[]>
        ]);

        // 构建 warn_code → strategy 查找表
        const strategyMap: Record<string, string> = {};
        configs.forEach((c: any) => {
            if (c.warn_code && c.strategy) strategyMap[c.warn_code] = c.strategy;
        });

        const grouped: Record<string, any[]> = {};
        data.forEach((row: any) => {
            const code = row.fault_code;
            if (!code) return;
            // warn_code 优先用 lifecycle.warn_code（storage-adapter 写入时已映射）；
            // 缺失时回退用 BFF 端推断，兼容老数据。
            let warnCode: string | null = row.warn_code || null;
            if (!warnCode) {
                const num = parseInt(String(code).replace(/[^0-9]/g, ''), 10);
                const seq = isNaN(num) ? -1 : num % 100;
                warnCode = this.hvacSeqToWarnCode(seq, code);
            }
            const strategy = warnCode ? (strategyMap[warnCode] || '') : '';
            if (!grouped[code]) grouped[code] = [];
            grouped[code].push({
                name: row.fault_name,
                warning_time: formatTime(row.start_time),
                carriage_no: row.carriage_id || 0,
                train_no: row.train_id || 0,
                line_no: row.line_id || 0,
                strategy
            });
        });
        return grouped;
    }

    /**
     * 获取列车车厢选择列表及其状态 (vw_carriage_status)
     */
    static async getTrainSelection(trainId: number) {
        const statuses = await db
            .with('latest_status', (db) => db
                .selectFrom('hvac.fact_raw')
                .select(['train_id', 'carriage_id', 'device_id', 'payload_json'])
                .where('train_id', '=', trainId)
                .distinctOn('device_id')
                .orderBy('device_id')
                .orderBy(this.timeCol as any, 'desc')
            )
            .selectFrom('latest_status')
            .select([
                'carriage_id as carriage_no',
                sql<number>`SUM(
                    (SELECT count(*) FROM jsonb_each_text(payload_json->'raw') 
                     WHERE (key ILIKE 'Bflt%' OR key ILIKE 'Blp%' OR key ILIKE 'Bsc%' OR key ILIKE 'Boc%') 
                     AND value = 'true'
                     AND NOT EXISTS (
                         SELECT 1 FROM hvac.dim_alarm_mask m 
                         WHERE m.device_id = latest_status.device_id AND m.fault_code = key
                     ))
                )`.as('alarm_count')
            ])
            .groupBy('carriage_id')
            .execute();

        return {
            vw_carriage_status: statuses.map(s => ({
                carriage_no: `${trainId}0${s.carriage_no}`,
                alarm_count: Number(s.alarm_count)
            })),
            vw_carriage_predict_status: [] // 预警先返回空
        };
    }

    /**
     * 获取全车各车厢最新的细节运行状态 (vw_running_state_info)
     */
    static async getTrainRunningState(trainId: number) {
        const records = await db
            .selectFrom('hvac.fact_raw')
            .selectAll()
            .where('train_id', '=', trainId)
            .distinctOn('carriage_id')
            .orderBy('carriage_id')
            .orderBy(this.timeCol as any, 'desc')
            .execute();

        return {
            vw_running_state_info: records.map(r => ({
                ...r,
                carriage_no: r.carriage_id,
                ...(r.payload_json?.raw || {}) // 将原始信号平铺
            }))
        };
    }

    /**
     * 获取车厢系统细节 (vw_system_info)
     */
    static async getCarriageSystemInfo(carriageId: string) {
        const trainId = parseInt(carriageId.slice(0, 4));
        const carNo = parseInt(carriageId.slice(-2));

        const record = await db
            .selectFrom('hvac.fact_raw')
            .selectAll()
            .where((eb) => eb.or([
                eb('carriage_id', '=', carNo),
                eb('carriage_id', '=', parseInt(carriageId))
            ]))
            .where('train_id', '=', trainId)
            .orderBy(this.timeCol as any, 'desc')
            .limit(1)
            .executeTakeFirst();

        return {
            vw_system_info: record ? [{
                ...record,
                ...(record.payload_json?.raw || {})
            }] : []
        };
    }

    /**
     * 获取车厢健康评估 (读取真实的设备寿命数据)
     * 根据 NB67 协议映射：风机/压缩机读取累计运行秒数，阀门读取开关次数
     */
    static async getCarriageHealthAssessment(carriageId: string) {
        const trainId = parseInt(carriageId.slice(0, 4));
        const carNo = parseInt(carriageId.slice(-2));

        // 1. 获取该车厢最新的原始报文快照
        const latestRaw = await db
            .selectFrom('hvac.fact_raw')
            .select(['payload_json'])
            .where('train_id', '=', trainId)
            .where((eb) => eb.or([
                eb('carriage_id', '=', carNo),
                eb('carriage_id', '=', parseInt(carriageId))
            ]))
            .orderBy(this.timeCol as any, 'desc')
            .limit(1)
            .executeTakeFirst();

        // 如果没有数据，返回空列表
        if (!latestRaw || !latestRaw.payload_json || !latestRaw.payload_json.raw) {
            return { vw_health_assessment: [] };
        }

        const raw = latestRaw.payload_json.raw;

        /**
         * 2. 定义部件寿命映射表 (与 nb67_event_processor.go 逻辑对齐)
         * 额定寿命参考：风机 25000h, 压缩机 50000h, 阀门 100万次
         */
        const componentSchema = [
            { name: '机组1通风机', timeField: 'DwefOpTmU11', ratedTime: 25000 },
            { name: '机组1冷凝风机', timeField: 'DwcfOpTmU11', ratedTime: 25000 },
            { name: '机组1压缩机1', timeField: 'DwcpOpTmU11', ratedTime: 50000 },
            { name: '机组1压缩机2', timeField: 'DwcpOpTmU12', ratedTime: 50000 },
            { name: '机组1新风阀', actionField: 'DwfadOpCntU1', ratedActions: 1000000 },
            { name: '机组1回风阀', actionField: 'DwradOpCntU1', ratedActions: 1000000 },

            { name: '机组2通风机', timeField: 'DwefOpTmU21', ratedTime: 25000 },
            { name: '机组2冷凝风机', timeField: 'DwcfOpTmU21', ratedTime: 25000 },
            { name: '机组2压缩机1', timeField: 'DwcpOpTmU21', ratedTime: 50000 },
            { name: '机组2压缩机2', timeField: 'DwcpOpTmU22', ratedTime: 50000 },
            { name: '机组2新风阀', actionField: 'DwfadOpCntU2', ratedActions: 1000000 },
            { name: '机组2回风阀', actionField: 'DwradOpCntU2', ratedActions: 1000000 },

            { name: '废排风机', timeField: 'DwexufanOpTm', ratedTime: 25000 },
            { name: '废排风阀', actionField: 'DwdmpexuOpCnt', ratedActions: 1000000 },
        ];

        // 3. 转换并计算健康度
        const assessment = componentSchema.map(c => {
            let actualTime = 0;
            let actualActions = 0;
            let ratio = 0;

            if (c.timeField) {
                // 原始数据为秒，转换为小时
                const seconds = Number(raw[c.timeField]) || 0;
                actualTime = Math.floor(seconds / 3600);
                ratio = actualTime / c.ratedTime;
            } else if (c.actionField) {
                // 原始数据即为次数
                actualActions = Number(raw[c.actionField]) || 0;
                ratio = actualActions / c.ratedActions;
            }

            // 判定逻辑：≥85% 非健康，≥70% 亚健康，否则健康 — PHM 2.2节
            let healthStatus = '健康';
            let suggestion = '设备运行良好，继续保持';

            if (ratio >= 0.85) {
                healthStatus = '非健康';
                suggestion = '设备寿命已耗尽，请立即安排更换';
            } else if (ratio >= 0.70) {
                healthStatus = '亚健康';
                suggestion = '部件进入磨损后期，请加强关注';
            }

            return {
                equipment_name: c.name,
                health_status: healthStatus,
                cumulative_running_time: actualTime,
                rated_running_time: c.ratedTime || 0,
                number_actions: actualActions,
                rated_actions: c.ratedActions || 0,
                suggestion: suggestion
            };
        });

        return {
            vw_health_assessment: assessment
        };
    }

    // B1: 支持机组筛选（unitNames: ['机组一','机组二'] 或空=全部）
    static async getHistoricalEvents(params: {
        trainId?: number | undefined;
        carriageId?: number | undefined;
        carriageIds?: number[] | undefined;
        eventType?: string | undefined;
        unitNames?: string[] | undefined;
        startTime: string;
        endTime: string;
        page?: number;
        limit?: number;
    }) {
        // 分页参数处理，确保类型正确
        const page = Math.max(1, Number(params.page) || 1);
        const limit = Math.min(500, Math.max(1, Number(params.limit) || 500));
        const offset = (page - 1) * limit;

        // 【关键修复】count 查询和 list 查询必须分别独立构建
        // Kysely 的 QB 对象在调用 .select()/.selectAll() 后返回新对象，
        // 但共享同一个 QB 实例在某些版本中会导致 select 子句污染。
        // 这里通过工厂函数每次重新构建，彻底隔离两路查询。
        const buildBase = () => {
            let q = db.selectFrom('hvac.fact_event');

            // 只有当时间参数有效时才应用过滤器
            if (params.startTime) {
                const start = new Date(params.startTime);
                if (!isNaN(start.getTime())) {
                    q = q.where(this.timeCol as any, '>=', start);
                }
            }
            if (params.endTime) {
                const end = new Date(params.endTime);
                if (!isNaN(end.getTime())) {
                    q = q.where(this.timeCol as any, '<=', end);
                }
            }

            if (params.trainId) {
                q = q.where('train_id', '=', params.trainId);
            }
            if (params.carriageId) {
                q = q.where('carriage_id', '=', params.carriageId);
            }
            if (params.carriageIds && params.carriageIds.length > 0) {
                q = q.where('carriage_id', 'in', params.carriageIds as any);
            }
            if (params.eventType) {
                q = q.where('event_type', '=', params.eventType);
            }
            // 机组筛选：告警码已改为 HVAC{carriage*100+seq} 格式（alertcode_v2.xlsx）
            // seq 1-26：预警码（predict），seq 27-46：机组1告警，seq 47-66：机组2告警，seq 67-75：公共告警
            // 通过提取 seq = fault_code数字部分 % 100 判断所属机组
            if (params.unitNames && params.unitNames.length > 0 && params.unitNames.length < 2) {
                if (params.unitNames.includes('机组一')) {
                    // seq 1-26（预警，含机组1部分）+ seq 27-46（机组1告警）
                    q = q.where(sql`
                        fault_code ~ '^HVAC[0-9]+$' AND (
                            (CAST(substring(fault_code from 5) AS INT) % 100 BETWEEN 1 AND 26
                             AND lower(fault_name) LIKE '%u1%' OR lower(fault_name) LIKE '%1%')
                            OR
                            (CAST(substring(fault_code from 5) AS INT) % 100 BETWEEN 27 AND 46)
                        )
                    ` as any);
                } else if (params.unitNames.includes('机组二')) {
                    // seq 3-6,11,14-15,18-19,23-24,26（预警，机组2部分）+ seq 47-66（机组2告警）
                    q = q.where(sql`
                        fault_code ~ '^HVAC[0-9]+$' AND (
                            (CAST(substring(fault_code from 5) AS INT) % 100 BETWEEN 1 AND 26
                             AND (lower(fault_name) LIKE '%u2%' OR lower(fault_name) LIKE '%机组2%'))
                            OR
                            (CAST(substring(fault_code from 5) AS INT) % 100 BETWEEN 47 AND 66)
                        )
                    ` as any);
                }
            }
            return q;
        };

        // alarm 路径：fact_event 每帧入一行 → 历史报警页天然重复。
        // 这里在 JS 层按 (train_id, carriage_id, fault_code) + 60s "发作岛" 去重，
        // 同一岛取 min(event_time/ingest_time) 作触发时刻，max(timeCol) 作 recovery_time。
        // 不动 DB schema，不影响 predict 路径（predict 走 warning_lifecycle）。
        if (params.eventType === 'alarm') {
            const rawAll = await buildBase()
                .selectAll()
                .orderBy(this.timeCol as any, 'desc')
                .orderBy('event_time', 'desc')
                .orderBy('fault_code', 'asc')
                .limit(5000)
                .execute();

            const GAP_MS = 60_000;
            const tCol = this.timeCol;
            // 按 key + 时间正序构造发作岛
            const asc = [...rawAll].sort((a: any, b: any) => {
                const ka = `${a.train_id}:${a.carriage_id}:${a.fault_code}`;
                const kb = `${b.train_id}:${b.carriage_id}:${b.fault_code}`;
                if (ka !== kb) return ka.localeCompare(kb);
                return new Date(a[tCol]).getTime() - new Date(b[tCol]).getTime();
            });
            type Island = {
                key: string;
                row: any;          // 第一帧（带完整字段）
                start: Date;
                lastSeen: Date;
            };
            const islands: Island[] = [];
            let cur: Island | null = null;
            for (const r of asc) {
                const k = `${(r as any).train_id}:${(r as any).carriage_id}:${(r as any).fault_code}`;
                const t = new Date((r as any)[tCol]).getTime();
                if (cur && cur.key === k && t - cur.lastSeen.getTime() <= GAP_MS) {
                    cur.lastSeen = new Date(t);
                } else {
                    cur = { key: k, row: r, start: new Date(t), lastSeen: new Date(t) };
                    islands.push(cur);
                }
            }
            // 每岛 = 一条 "alarm" 记录：start 用 islandStart；recovery_time 推断：
            //   - 原 row.recovery_time 不为 null → 直接用
            //   - 同 key 后面还有更新的岛（说明此岛已结束） → recovery_time = lastSeen
            //   - 是该 key 最新一岛 且 lastSeen 距 now < 60s → 视为仍活跃，recovery_time = null
            //   - 是该 key 最新一岛 但 lastSeen 已经超过 60s → 视为已结束，recovery_time = lastSeen
            const nowMs = Date.now();
            const lastIslandIdxByKey = new Map<string, number>();
            islands.forEach((isl, idx) => lastIslandIdxByKey.set(isl.key, idx));
            const merged = islands.map((i, idx) => {
                let recovery: Date | null;
                if (i.row.recovery_time != null) {
                    recovery = i.row.recovery_time;
                } else if (lastIslandIdxByKey.get(i.key) !== idx) {
                    recovery = i.lastSeen;        // 此岛后面还有同 key 新岛，必定已结束
                } else if (nowMs - i.lastSeen.getTime() > 60_000) {
                    recovery = i.lastSeen;        // 最新岛但已超 60s 无新帧 → 视为结束
                } else {
                    recovery = null;              // 仍活跃
                }
                return {
                    ...i.row,
                    [tCol]: i.start,
                    event_time: i.start,
                    ingest_time: i.start,
                    recovery_time: recovery,
                    occurrence_start: i.start,
                };
            });
            // 按 start_time desc 重排，分页
            merged.sort((a: any, b: any) =>
                new Date(b.event_time).getTime() - new Date(a.event_time).getTime());
            const total = merged.length;
            const list = merged.slice(offset, offset + limit);
            console.log(`[Repository] Query Alarms (deduped): total=${total}, returned=${list.length}, page=${page}, offset=${offset}, range=[${params.startTime} - ${params.endTime}]`);
            return { list, total };
        }

        // 非 alarm 路径（保留原行为）
        const totalResult = await buildBase()
            .select(sql<number>`count(*)`.as('count'))
            .executeTakeFirst();
        const total = Number((totalResult as any)?.count || 0);

        const list = await buildBase()
            .selectAll()
            .orderBy(this.timeCol as any, 'desc')
            .orderBy('event_time', 'desc')
            .orderBy('fault_code', 'asc')
            .limit(limit)
            .offset(offset)
            .execute();

        console.log(`[Repository] Query Alarms: total=${total}, returned=${list.length}, page=${page}, offset=${offset}, range=[${params.startTime} - ${params.endTime}]`);

        return { list, total };
    }

    /**
     * 获取原始历史数据 (适配全量数据查询)
     */
    static async getRawHistory(params: {
        trainId?: number | undefined;
        carriageId?: number | undefined;
        carriageIds?: number[] | undefined;
        startTime: string;
        endTime: string;
        page: number;
        limit: number;
    }) {
        const offset = (params.page - 1) * params.limit;

        let baseQuery = db
            .selectFrom('hvac.fact_raw')
            .where(this.timeCol as any, '>=', new Date(params.startTime))
            .where(this.timeCol as any, '<=', new Date(params.endTime));

        if (params.trainId) {
            baseQuery = baseQuery.where('train_id', '=', params.trainId);
        }

        // 优先使用 carriageIds 数组（多车厢查询），兼容旧的 carriageId 单值参数
        if (params.carriageIds && params.carriageIds.length > 0) {
            baseQuery = baseQuery.where('carriage_id', 'in', params.carriageIds);
        } else if (params.carriageId) {
            baseQuery = baseQuery.where('carriage_id', '=', params.carriageId);
        }

        // 获取总数
        const totalResult = await baseQuery
            .select(sql`count(*)`.as('count'))
            .executeTakeFirst();
        const total = Number((totalResult as any)?.count || 0);

        // 获取列表
        const list = await baseQuery
            .selectAll()
            .orderBy(this.timeCol as any, 'desc')
            .limit(params.limit)
            .offset(offset)
            .execute();

        return { list, total };
    }

    /**
     * 获取故障统计供 Echart 渲染 (返回 vw_alarm_info_all_date)
     */
    static async getFaultStatistics() {
        // 2026-06-09 修复：fact_event 每帧入一行，原查询取 last 5000 行让 Echart 拿到
        // 大量重复（一条 30s 报警 = 30 行）→ 占比统计严重失真。
        // 这里按 (fault_code, device_id, minute) 去重——同分钟内同设备同故障算 1 次——
        // 既保留前端 "value === 1" 累加契约（每个数组项形如 {HVAC301:1}），
        // 又把"同一报警重复出现 30 次"的伪信号过滤掉。
        const rows = await db
            .selectFrom('hvac.fact_event')
            .select([
                'fault_code',
                sql<Date>`date_trunc('minute', ${sql.ref(this.timeCol)})`.as('minute'),
                'device_id' as any,
            ])
            .where('event_type', '=', 'alarm')
            .where(this.timeCol as any, '>=', sql<Date>`now() - interval '7 days'` as any)
            .groupBy(['fault_code', 'device_id' as any, sql`date_trunc('minute', ${sql.ref(this.timeCol)})`])
            .orderBy(sql`date_trunc('minute', ${sql.ref(this.timeCol)})`, 'desc')
            .limit(5000)
            .execute();

        return {
            vw_alarm_info_all_date: rows.map((r: any) => ({
                [r.fault_code as string]: 1,
            })),
        };
    }

    // B3: 历史预警查询 — 改查 hvac.warning_lifecycle（2026-06-05 followup）
    //
    // 原实现查 fact_event 会出现"一条预警显示多行"——因为 predict 每帧入库一行。
    // 改查 warning_lifecycle：一条预警 = 一行，start_time = 触发时刻，end_time =
    // 消除时刻（NULL 表示仍活跃），与 #20 #21 整改后的实时页面共享一张事实表。
    //
    // 时间过滤语义：返回**生命周期与查询窗口有重叠**的预警，即
    //   start_time <= window_end AND (end_time IS NULL OR end_time >= window_start)
    // 这样窗口内"已经触发但还没结束"的活跃预警也会被列出来。
    static async getHistoricalWarnings(params: {
        trainId?: number;
        carriageIds?: number[];
        unitNames?: string[];
        startTime: string;
        endTime: string;
        page: number;
        limit: number;
    }) {
        const page = Math.max(1, params.page);
        const limit = Math.min(500, Math.max(1, params.limit));
        const offset = (page - 1) * limit;

        const buildBase = () => {
            let q = db.selectFrom('hvac.warning_lifecycle as e' as any);

            if (params.startTime) {
                const start = new Date(params.startTime);
                if (!isNaN(start.getTime())) {
                    // 生命周期与窗口有重叠：end_time IS NULL 或 end_time >= window_start
                    q = q.where((eb: any) => eb.or([
                        eb('e.end_time' as any, 'is', null),
                        eb('e.end_time' as any, '>=', start),
                    ]));
                }
            }
            if (params.endTime) {
                const end = new Date(params.endTime);
                if (!isNaN(end.getTime())) q = q.where('e.start_time' as any, '<=', end);
            }
            if (params.trainId) q = q.where('e.train_id' as any, '=', params.trainId);
            if (params.carriageIds && params.carriageIds.length > 0) {
                q = q.where('e.carriage_id' as any, 'in', params.carriageIds as any);
            }
            // 注意：unit 过滤在 JS 层处理（sql raw 在此版本 Kysely 的 where 中不生效）
            return q;
        };

        const totalResult = await buildBase()
            .select(sql<number>`count(*)`.as('count'))
            .executeTakeFirst();
        const total = Number((totalResult as any)?.count || 0);

        // 并行拉取生命周期列表和预警配置（用于触发条件描述）
        const [rawLifecycle, warnConfigs] = await Promise.all([
            buildBase()
                .select([
                    'e.id' as any,
                    'e.device_id' as any,
                    'e.line_id' as any,
                    'e.train_id' as any,
                    'e.carriage_id' as any,
                    'e.unit_id' as any,
                    'e.fault_code' as any,
                    'e.fault_name' as any,
                    'e.warn_code' as any,
                    'e.severity' as any,
                    'e.start_time' as any,
                    'e.end_time' as any,
                    'e.last_seen_time' as any,
                    'e.trigger_snapshot' as any,
                ])
                .orderBy('e.start_time' as any, 'desc')
                .limit(1000)   // 宽松拉取，JS 过滤后再分页
                .execute(),
            // 同 getRealtimeWarnings 的已验证模式：selectAll() as any[]
            (db.selectFrom('hvac.warning_config' as any).selectAll().execute() as Promise<any[]>),
        ]);

        // 映射 warning_lifecycle 行 → 兼容 fact_event 老 API 的字段名
        // 让前端无需改动即可消费；缺失列（如 ingest_time/payload_json）按需补 null/空对象。
        const rawList = (rawLifecycle as any[]).map((r: any) => ({
            event_time: r.start_time,       // 历史页用作"触发时刻"
            ingest_time: r.start_time,      // 老 API 兼容
            train_id: r.train_id,
            carriage_id: r.carriage_id,
            fault_code: r.fault_code,
            fault_name: r.fault_name,
            severity: r.severity,
            // status: 'active' = 仍活跃；'recovered' = 已消除
            status: r.end_time == null ? 'active' : 'recovered',
            recovery_time: r.end_time,
            // 老 API 的 payload_json 里有 trigger_condition_snapshot，
            // 新链路用 trigger_snapshot 列承载；包成兼容形状给下游降级逻辑用。
            payload_json: r.trigger_snapshot
                ? { trigger_condition_snapshot:
                    typeof r.trigger_snapshot === 'string'
                        ? r.trigger_snapshot
                        : (r.trigger_snapshot?.trigger_condition_snapshot
                            || r.trigger_snapshot?.condition
                            || null) }
                : {},
        }));

        // 构建 warn_code → trigger_condition（由设置页可见的 threshold_bad + duration_seconds 动态生成）
        // 与设置页保持一致：用户修改阈值后，历史页触发条件自动更新
        const triggerCondByWarnCode: Record<string, string> = {};
        warnConfigs.forEach((c: any) => {
            if (c.warn_code) {
                const parts: string[] = [];
                if (c.threshold_bad) parts.push(String(c.threshold_bad));
                const secs = Number(c.duration_seconds);
                if (secs > 0) {
                    if (secs >= 60 && secs % 60 === 0) {
                        parts.push(`持续${secs / 60}分钟`);
                    } else {
                        parts.push(`持续${secs}秒`);
                    }
                }
                if (parts.length) triggerCondByWarnCode[c.warn_code] = parts.join(' ');
            }
        });

        // 将 HVAC 编码映射到触发条件：优先读 payload_json 中的快照（触发时刻记录），
        // 快照缺失时降级到当前配置（向后兼容旧数据）
        let list = rawList.map((row: any) => {
            const code = String(row.fault_code || '');
            let trigger_condition: string | null = null;

            // 1. 尝试从 payload_json 读取快照（新数据路径）
            try {
                const payload = typeof row.payload_json === 'string'
                    ? JSON.parse(row.payload_json)
                    : (row.payload_json || {});
                if (payload.trigger_condition_snapshot) {
                    trigger_condition = String(payload.trigger_condition_snapshot);
                }
            } catch {
                // JSON 解析失败时忽略，继续走降级逻辑
            }

            // 2. 降级：从当前配置动态生成（旧数据无快照时的兼容路径）
            if (!trigger_condition && code.toUpperCase().startsWith('HVAC')) {
                const num = parseInt(code.replace(/[^0-9]/g, ''), 10);
                const seq = isNaN(num) ? -1 : num % 100;
                const warnCode = StatusRepository.hvacSeqToWarnCode(seq, code);
                if (warnCode) trigger_condition = triggerCondByWarnCode[warnCode] ?? null;
            }
            return { ...row, trigger_condition };
        });

        // JS 层机组过滤（sql raw 在此版本 Kysely where 中无法生效）
        if (params.unitNames && params.unitNames.length === 1) {
            const unit = params.unitNames[0];
            const u1Seqs = new Set([1,3,5,7,9,10,12,13,16,17,20,21,22,25]);
            const u2Seqs = new Set([2,4,6,8,11,14,15,18,19,23,24,26]);
            list = list.filter((row: any) => {
                const code: string = (row.fault_code || '').toLowerCase();
                if (code.startsWith('hvac')) {
                    const seq = parseInt(code.replace(/[^0-9]/g, '')) % 100;
                    return unit === '机组一' ? u1Seqs.has(seq) : u2Seqs.has(seq);
                }
                // u1/u2 风格编码（alarm 类）
                return unit === '机组一' ? code.includes('u1') : code.includes('u2');
            });
        }

        const filteredTotal = list.length;

        // 计算每行的"发作起始时间"：同一故障连续触发共享同一 start_time，60s 无触发视为新的发作序列
        const GAP_MS = 60_000;
        const tCol = this.timeCol;
        const asc = [...list].sort((a: any, b: any) => {
            const ka = `${a.train_id}:${a.carriage_id}:${a.fault_code}`;
            const kb = `${b.train_id}:${b.carriage_id}:${b.fault_code}`;
            if (ka !== kb) return ka.localeCompare(kb);
            return new Date(a[tCol]).getTime() - new Date(b[tCol]).getTime();
        });
        const startByRow = new Map<any, Date>();
        const islandStart: Record<string, Date> = {};
        const lastSeen: Record<string, number> = {};
        for (const row of asc) {
            const k = `${(row as any).train_id}:${(row as any).carriage_id}:${(row as any).fault_code}`;
            const t = new Date((row as any)[tCol]).getTime();
            const prev = lastSeen[k];
            if (prev === undefined || t - prev > GAP_MS) {
                islandStart[k] = new Date(t);
            }
            lastSeen[k] = t;
            startByRow.set(row, islandStart[k]!);
        }

        const paginated = list.slice(offset, offset + limit).map((row: any) => ({
            ...row,
            occurrence_start: startByRow.get(row) ?? new Date((row as any)[tCol]),
        }));

        return { list: paginated, total: filteredTotal };
    }

    // B7: 预警条件配置 CRUD
    static async getWarningConfigs() {
        return await db.selectFrom('hvac.warning_config').selectAll().orderBy('id', 'asc').execute();
    }

    static async getWarningConfig(id: number) {
        return await db.selectFrom('hvac.warning_config').selectAll().where('id', '=', id).executeTakeFirst();
    }

    static async updateWarningConfig(id: number, data: {
        threshold_good?: string;
        threshold_normal?: string;
        threshold_bad?: string;
        trigger_operator?: string;
        trigger_value?: number;
        clear_value?: number;
        duration_seconds?: number;
        unit?: string;
        strategy?: string;
        enabled?: boolean;
    }) {
        return await db
            .updateTable('hvac.warning_config' as any)
            .set(data as any)
            .where('id', '=', id)
            .execute();
    }

    static async resetWarningConfig(id: number) {
        return await db.executeQuery(
            sql`UPDATE hvac.warning_config
                SET trigger_value    = default_trigger_value,
                    clear_value      = default_clear_value,
                    duration_seconds = default_duration_seconds,
                    threshold_good   = default_threshold_good,
                    threshold_normal = default_threshold_normal,
                    threshold_bad    = default_threshold_bad,
                    strategy         = default_strategy
                WHERE id = ${sql.lit(id)}
                  AND default_trigger_value IS NOT NULL`.compile(db)
        );
    }

    // B8: 获取指定车厢最新数据时间（供前端离线检测）
    static async getLatestDataTime(trainId: number, carriageId: number): Promise<Date | null> {
        const result = await db
            .selectFrom('hvac.fact_raw')
            .select(sql<Date>`MAX(${sql.raw(this.timeCol)})`.as('latest'))
            .where('train_id', '=', trainId)
            .where('carriage_id', '=', carriageId)
            .executeTakeFirst();
        return (result as any)?.latest || null;
    }
}
