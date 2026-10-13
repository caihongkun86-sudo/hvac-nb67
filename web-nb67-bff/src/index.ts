import Fastify from 'fastify';
import swagger from '@fastify/swagger';
import swaggerUi from '@fastify/swagger-ui';
import websocket from '@fastify/websocket';
import cors from '@fastify/cors';
import { config } from './config/index.js';
import { db } from './config/db.js';
import { checkDbConnection } from './config/db.js';
import { sql } from 'kysely';
import { KafkaManager } from './config/kafka.js';
import { HistoryRepository, TREND_PARAM_DEFS } from './repository/history.repository.js';
import { StatusRepository } from './repository/status.repository.js';
import { formatTime } from './utils/time.js';
import { getModelName, getStationName } from './utils/labels.js';

const fastify = Fastify({
    logger: {
        level: config.logLevel,
        timestamp: () => `,"time":"${formatTime(new Date())}"`
    },
});

/**
 * 启动服务
 */
async function bootstrap() {
    try {
        // 1. 注册插件
        await fastify.register(cors, { origin: '*' });
        await fastify.register(websocket);

        // 注册 Swagger
        await fastify.register(swagger, {
            openapi: {
                info: {
                    title: 'Macda-Connector BFF API',
                    description: '车载空调网关 BFF 接口文档',
                    version: '1.0.0'
                },
                servers: [{ url: `http://localhost:${config.port}` }]
            }
        });

        await fastify.register(swaggerUi, {
            routePrefix: '/docs',
            uiConfig: {
                docExpansion: 'list',
                deepLinking: false
            }
        });

        // 2. 数据库健康检查
        await checkDbConnection();

        // 3. 注册 WebSocket 路由
        fastify.register(async (instance) => {
            instance.get('/ws/signals', { websocket: true }, (connection: any, req) => {
                instance.log.info('[WebSocket] Client connected');

                connection.socket.on('message', (message: Buffer) => {
                    instance.log.info(`[WebSocket] Received: ${message.toString()}`);
                });

                connection.socket.on('close', () => {
                    instance.log.info('[WebSocket] Client disconnected');
                });
            });
        });

        // 4. 注册 HTTP 路由 (REST)
        fastify.get('/api/rest/AirSystem', {
            schema: {
                description: '获取在线列车空调运行状态汇总',
                tags: ['Status'],
                response: {
                    200: {
                        type: 'object',
                        properties: {
                            vw_train_alarm_count: {
                                type: 'array',
                                items: {
                                    type: 'object',
                                    properties: {
                                        train_no: { type: 'number' },
                                        alarm_count: { type: 'number' },
                                        warning_count: { type: 'number' },
                                        total_number: { type: 'number' }
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }, async () => {
            return await StatusRepository.getTrainAlarmSummary();
        });

        fastify.post('/api/rest/v2/train/RealtimeAlarm', {
            schema: {
                description: '获取指定列车的详细实时信号',
                tags: ['Status'],
                body: {
                    type: 'object',
                    properties: {
                        trainNo: { type: 'array', items: { type: 'string' } }
                    }
                }
            }
        }, async (request: any) => {
            const { trainNo } = request.body;
            // parse to array of numbers
            const trainIds = Array.isArray(trainNo) ? trainNo.map(n => parseInt(n)) : [];
            const data = await StatusRepository.getTrainDetails(trainIds);

            return {
                vw_train_alarm_info: data
            };
        });

        fastify.get('/api/history/events', {
            // ... (keep existing historical events route)
        }, async (request: any) => {
            return await StatusRepository.getHistoricalEvents(request.query);
        });

        /**
         * 业务接口：删除（屏蔽）具体告警
         */
        fastify.post('/api/rest/v2/alarm/mask', {
            schema: {
                description: '删除(屏蔽)指定的故障位。被屏蔽的故障将不再计入实时统计，直到该故障物理消失后再次发生。',
                tags: ['Alarm'],
                body: {
                    type: 'object',
                    required: ['deviceId', 'faultCode'],
                    properties: {
                        deviceId: { type: 'string', description: '设备唯一标识 (如 HVAC-67-07002-01)' },
                        faultCode: { type: 'string', description: '故障代码位名称 (如 Bflt_FadU11)' }
                    }
                },
                response: {
                    200: {
                        type: 'object',
                        properties: {
                            success: { type: 'boolean' },
                            message: { type: 'string' }
                        }
                    }
                }
            }
        }, async (request: any) => {
            const { deviceId, faultCode } = request.body;
            await StatusRepository.maskAlarm(deviceId, faultCode);
            return { success: true, message: `Alarm ${faultCode} for ${deviceId} masked.` };
        });

        fastify.get('/api/history/temperature', async (request: any, reply) => {
            const { deviceId, type = 'hour' } = request.query;
            if (!deviceId) return reply.status(400).send({ error: 'deviceId is required' });

            // 假设 deviceId 格式为 700203
            const trainId = parseInt(String(deviceId).slice(0, 4));
            const carriageId = parseInt(String(deviceId).slice(-2));

            const data = await HistoryRepository.getTemperatureTrend(trainId, carriageId, type as string);
            return { success: true, data };
        });

        // ==========================
        // 兼容前端页面的统计与预警接口
        // ==========================
        fastify.post('/api/rest/v2/FaultStatistics', {
            schema: { tags: ['Status'] }
        }, async () => {
            return await StatusRepository.getFaultStatistics();
        });

        fastify.get('/api/rest/RealtimeWarning', {
            schema: { tags: ['Status'] }
        }, async () => {
            return await StatusRepository.getRealtimeWarnings();
        });

        fastify.get('/api/status/latest', async (request: any, reply) => {
            const { deviceId } = request.query;
            if (!deviceId) return reply.status(400).send({ error: 'deviceId is required' });

            const data = await HistoryRepository.getLatestStatus(deviceId);
            return { success: true, data };
        });

        // ── 健康检查端点（Docker healthcheck 专用）────────────────
        // 此端点不访问数据库，保证容器启动阶段能快速通过健康检查
        fastify.get('/health', async () => {
            return { status: 'ok', time: formatTime(new Date()) };
        });

        fastify.get('/api/test', async () => {
            return { status: 'ok', time: formatTime(new Date()) };
        });

        // 调试接口：获取最新一条原始报文的所有字段名和示例值，方便前端对齐字段名
        fastify.get('/api/debug/raw-fields', async (request: any) => {
            const { trainId, carriageId } = request.query;
            const row = await db
                .selectFrom('hvac.fact_raw')
                .selectAll()
                .where('train_id', '=', parseInt(trainId || '7002'))
                .where('carriage_id', '=', parseInt(carriageId || '1'))
                .orderBy('ingest_time', 'desc')
                .limit(1)
                .executeTakeFirst();
            if (!row) return { error: 'no data' };
            const raw = (row as any).payload_json?.raw || {};
            return {
                // 顶层 DB 列（排除大字段）
                db_columns: Object.keys(row).filter(k => k !== 'payload_json'),
                db_sample: Object.fromEntries(Object.entries(row).filter(([k]) => k !== 'payload_json')),
                // payload_json.raw 内的信号字段
                raw_keys: Object.keys(raw),
                raw_sample: raw
            };
        });

        // ==========================
        // 列车/车厢 详细 REST 接口 (适配原有前端)
        // ==========================
        fastify.get('/api/rest/train/StatusAlert/:trainId', async (request: any) => {
            const trainId = parseInt(request.params.trainId);
            return await StatusRepository.getRealtimeWarnings(trainId);
        });

        fastify.get('/api/rest/train/TrainSelection/:trainId', async (request: any) => {
            return await StatusRepository.getTrainSelection(parseInt(request.params.trainId));
        });

        fastify.get('/api/rest/train/RunningState/:trainId', async (request: any) => {
            return await StatusRepository.getTrainRunningState(parseInt(request.params.trainId));
        });

        // B1: 历史报警接口（支持多车厢/机组筛选，返回真实 recovery_time）
        fastify.post('/api/rest/train/AlarmInformation', async (request: any) => {
            const { state, trainNo, carriageNos, unitNames, startTime, endTime, page, limit } = request.body;

            // 兼容旧版单车厢 state 格式（700203）和新版多选格式
            let trainId: number | undefined;
            let carriageIds: number[] | undefined;
            if (state) {
                // state 格式：{trainId}{carriageNo(2位)}，trainId 可能是 4 或 5 位
                const s = String(state);
                trainId = parseInt(s.slice(0, -2));
                const cid = parseInt(s.slice(-2));
                if (!isNaN(cid)) carriageIds = [cid];
            }
            if (trainNo) trainId = parseInt(String(trainNo));
            if (carriageNos && Array.isArray(carriageNos) && carriageNos.length > 0) {
                carriageIds = carriageNos.map((n: any) => parseInt(String(n))).filter((n: number) => !isNaN(n));
            }

            const safeStartTime = startTime || '';
            const safeEndTime = endTime || '';
            const safePage = Math.max(1, parseInt(page as string) || 1);
            const safeLimit = Math.max(1, parseInt(limit as string) || 10);

            try {
                const result = await StatusRepository.getHistoricalEvents({
                    trainId,
                    carriageIds,
                    unitNames: unitNames || [],
                    startTime: safeStartTime,
                    endTime: safeEndTime,
                    eventType: 'alarm',
                    page: safePage,
                    limit: safeLimit
                });

                const formatUnit = (faultCode: string) => {
                    if (!faultCode) return '-';
                    const c = faultCode.toLowerCase();
                    if (c.startsWith('hvac')) {
                        const seq = parseInt(faultCode.replace(/[^0-9]/g, '')) % 100;
                        if (seq >= 27 && seq <= 46) return '机组一';
                        if (seq >= 47 && seq <= 66) return '机组二';
                        if (seq >= 67 && seq <= 75) return '公共';
                    }
                    if (c.includes('u1')) return '机组一';
                    if (c.includes('u2')) return '机组二';
                    return '-';
                };

                const list = (result.list || []).map((row: any) => {
                    // 报警严重级别：参考 alertcode level 列，1→正常 2→中等 3→严重
                    let level = '正常';
                    if (row.severity === 3) level = '严重';
                    else if (row.severity === 2) level = '中等';
                    else if (row.severity === 1) level = '正常';

                    return {
                        train_id: row.train_id,
                        carriage_no: row.carriage_id,
                        unit_name: formatUnit(row.fault_code || ''),
                        fault_code: row.fault_code,
                        fault_desc: row.fault_name,
                        fault_level: level,
                        occurrence_time: formatTime(row['event_time']),
                        recovery_time: row.recovery_time ? formatTime(row.recovery_time) : null,
                        status: row.recovery_time ? '已恢复' : '活动'
                    };
                });

                const timeline = (result.list || []).map((row: any) => ({
                    train_id: row.train_id,
                    carriage_no: row.carriage_id,
                    fault_code: row.fault_code,
                    name: row.fault_name,
                    start_time: formatTime(row['event_time']),
                    end_time: row.recovery_time ? formatTime(row.recovery_time) : null,
                    state: `${row.train_id}${String(row.carriage_id).padStart(2, '0')}1`
                }));

                return {
                    code: 200,
                    data: {
                        list: list,
                        total: result.total,
                        alarm_timeline: timeline
                    }
                };
            } catch (err: any) {
                fastify.log.error(err);
                return {
                    code: 500,
                    message: err.message,
                    data: { list: [], total: 0, alarm_timeline: [] }
                };
            }
        });

        // 历史数据接口 (适配 HistoryData 页面提取存在数据的日期)
        fastify.get('/api/getTrainDataDates', async (request: any) => {
            const { state, startTime, endTime } = request.query;
            const trainId = state ? parseInt(String(state).slice(0, -2)) : undefined;
            const carriageId = state ? parseInt(String(state).slice(-2)) : undefined;

            try {
                const queryStartTime = new Date(String(startTime || ''));
                const queryEndTime = new Date(String(endTime || ''));
                const timeCol = config.runtime === 'DEV' ? 'ingest_time' : 'event_time';

                let q = db.selectFrom('hvac.fact_raw')
                    .select(sql<string>`TO_CHAR(${sql.raw(timeCol)}, 'YYYY-MM-DD')`.as('date'))
                    .distinct()
                    .where(timeCol as any, '>=', queryStartTime)
                    .where(timeCol as any, '<=', queryEndTime);

                if (trainId) q = q.where('train_id', '=', trainId);
                if (carriageId) q = q.where('carriage_id', '=', carriageId);

                const records = await q.execute();
                const dates = records.map((r: any) => r.date).filter(Boolean).sort((a: string, b: string) => b.localeCompare(a));
                return { code: 200, data: dates };
            } catch (err: any) {
                fastify.log.error(err);
                return { code: 500, message: err.message, data: [] };
            }
        });

        // 历史数据接口 (适配 HistoryData 页面)
        // 每条 DB 记录按机组一/机组二拆成两行输出
        fastify.get('/api/getTrainData', async (request: any) => {
            const { state, carriageIds, startTime, endTime, page = 1, limit = 50, unitName = '' } = request.query;

            // 解析 trainId（从 state 参数或默认）
            const trainId = state ? parseInt(String(state).slice(0, -2)) : undefined;

            // 解析 carriageId（兼容旧的 state 参数）
            const carriageId = state ? parseInt(String(state).slice(-2)) : undefined;

            // 解析 carriageIds 数组参数（支持多车厢查询）
            let parsedCarriageIds: number[] | undefined;
            if (carriageIds) {
                if (Array.isArray(carriageIds)) {
                    parsedCarriageIds = carriageIds
                        .map((c: any) => parseInt(String(c)))
                        .filter((c: number) => !isNaN(c));
                } else {
                    // 处理逗号分隔的字符串
                    const ids = String(carriageIds).split(',');
                    parsedCarriageIds = ids
                        .map((c: string) => parseInt(c.trim()))
                        .filter((c: number) => !isNaN(c));
                }
            }

            // 关键修复：判断是否是"全部机组"
            const isAllUnits = !unitName;
            // 全部机组时，每条原始记录展开成2条展示记录，
            // 所以数据库查询的limit需要减半，才能保证返回的展示记录数 = 前端pageSize
            const repoLimit = isAllUnits ? Math.ceil(limit / 2) : limit;

            const result = await StatusRepository.getRawHistory({
                trainId,
                carriageId,
                carriageIds: parsedCarriageIds.length > 0 ? parsedCarriageIds : undefined,
                startTime: String(startTime || ''),
                endTime: String(endTime || ''),
                page: parseInt(page as string),
                limit: repoLimit  // ← 修复：全部机组时limit减半
            });

            // ms 转小时，保留 1 位小数
            const ms2h = (ms: any) =>
                ms !== undefined && ms !== null && ms !== ''
                    ? (Number(ms) / 3600000).toFixed(1) + 'h'
                    : '';

            // 展开后的数据
            const expandedList = result.list.flatMap((row: any) => {
                const raw = row.payload_json?.raw || {};

                        const unit1 = {
                            train_id: row.train_id,
                            carriage_id: row.carriage_id,
                            ingest_time: formatTime(row.ingest_time),
                            event_time: formatTime(row.event_time),
                            unit_name: '机组一',

                            i_inner_temp: raw.Tveh1 !== undefined ? (Number(raw.Tveh1) / 10).toFixed(1) : '',
                            i_back_temp: raw.RasU1 !== undefined ? (Number(raw.RasU1) / 10).toFixed(1) : '',
                            i_outer_temp: raw.FasU1 !== undefined ? (Number(raw.FasU1) / 10).toFixed(1) : '',
                            i_set_temp: raw.Tic !== undefined ? (Number(raw.Tic) / 10).toFixed(1) : '',
                            i_hum: raw.AqHU1 !== undefined ? (Number(raw.AqHU1) / 10).toFixed(1) : '',
                            i_sp_u1: raw.SpU11 !== undefined ? (Number(raw.SpU11) / 10).toFixed(1) : '',
                            i_sp_u2: raw.SpU12 !== undefined ? (Number(raw.SpU12) / 10).toFixed(1) : '', 
                            i_co2: raw.AqCo2U1 ?? '',
                            i_pm25: raw.AqPm25U1 ?? '',
                            i_pm10: raw.AqPm10U1 ?? '',
                            i_tvoc: raw.AqTvocU1 ?? '',
                            work_status: raw.WmodeU1 ?? '',

                            dwef_op_tm: raw.DwefOpTmU11 ?? '',
                            w_crnt_cf1: raw.ICfU11 !== undefined ? (Number(raw.ICfU11) / 10).toFixed(1) : '',
                            w_crnt_cf2: raw.ICfU12 !== undefined ? (Number(raw.ICfU12) / 10).toFixed(1) : '',
                            w_crnt_cp1: raw.ICpU11 !== undefined ? (Number(raw.ICpU11) / 10).toFixed(1) : '',
                            w_crnt_cp2: raw.ICpU12 !== undefined ? (Number(raw.ICpU12) / 10).toFixed(1) : '',
                            w_freq_cp1: raw.FCpU11 !== undefined ? (Number(raw.FCpU11) / 10).toFixed(1) : '',
                            w_freq_cp2: raw.FCpU12 !== undefined ? (Number(raw.FCpU12) / 10).toFixed(1) : '',
                            w_crnt_ef1: raw.IEfU11 !== undefined ? (Number(raw.IEfU11) / 10).toFixed(1) : '',
                            w_crnt_ef2: raw.IEfU12 !== undefined ? (Number(raw.IEfU12) / 10).toFixed(1) : '',

                            i_high_pres1: raw.HighpressU11 !== undefined ? (Number(raw.HighpressU11) / 10).toFixed(1) : '',
                            i_low_pres1: raw.SuckpU11 !== undefined ? (Number(raw.SuckpU11) / 10).toFixed(1) : '',
                            i_high_pres2: raw.HighpressU12 !== undefined ? (Number(raw.HighpressU12) / 10).toFixed(1) : '',
                            i_low_pres2: raw.SuckpU12 !== undefined ? (Number(raw.SuckpU12) / 10).toFixed(1) : '',

                            i_sat1: raw.SasU11 !== undefined ? (Number(raw.SasU11) / 10).toFixed(1) : '',
                            i_sat2: raw.SasU12 !== undefined ? (Number(raw.SasU12) / 10).toFixed(1) : '',
                            i_suckt1: raw.SucktU11 !== undefined ? (Number(raw.SucktU11) / 10).toFixed(1) : '',
                            i_suckt2: raw.SucktU12 !== undefined ? (Number(raw.SucktU12) / 10).toFixed(1) : '',
                            i_pres_diff: raw.PresdiffU1 !== undefined ? (Number(raw.PresdiffU1) / 10).toFixed(1) : '',
                            i_exufan: raw.IExufan !== undefined ? (Number(raw.IExufan) / 10).toFixed(1) : '',

                            dwcf_op_tm1: raw.DwcfOpTmU11 ?? '',
                            dwcf_op_tm2: raw.DwcfOpTmU12 ?? '',
                            dwcp_op_tm1: raw.DwcpOpTmU11 ?? '',
                            dwcp_op_tm2: raw.DwcpOpTmU12 ?? '',
                            dwexufan_op_tm: raw.DwexufanOpTm ?? '',

                            dwfad_op_cnt: raw.FadposU1 ?? '',
                            dwrad_op_cnt: raw.RadposU1 ?? '',
                            dmp_exu_pos: raw.DmpExuPos ?? '',
                            start_station: raw.StartStation ?? '',
                            terminal_station: raw.TerminalStation ?? '',
                            cur_station: raw.CurStation ?? '',
                            next_station: raw.NextStation ?? '',
                            line_id: row.line_id ?? '',
                        };

                        // ── 机组二 (U2x) ─────────────────────────────────
                        const unit2 = {
                            train_id: row.train_id,
                            carriage_id: row.carriage_id,
                            ingest_time: formatTime(row.ingest_time),
                            event_time: formatTime(row.event_time),
                            unit_name: '机组二',

                            i_inner_temp: raw.Tveh2 !== undefined ? (Number(raw.Tveh2) / 10).toFixed(1) : '',
                            i_back_temp: raw.RasU2 !== undefined ? (Number(raw.RasU2) / 10).toFixed(1) : '',
                            i_outer_temp: raw.FasU2 !== undefined ? (Number(raw.FasU2) / 10).toFixed(1) : '',
                            i_set_temp: raw.Tic !== undefined ? (Number(raw.Tic) / 10).toFixed(1) : '',
                            i_sp_u1: raw.SpU21 !== undefined ? (Number(raw.SpU21) / 10).toFixed(1) : '',
                            i_sp_u2: raw.SpU22 !== undefined ? (Number(raw.SpU22) / 10).toFixed(1) : '', 
                            i_hum: raw.AqHU2 !== undefined ? (Number(raw.AqHU2) / 10).toFixed(1) : '',
                            i_co2: raw.AqCo2U2 ?? '',
                            i_pm25: raw.AqPm25U2 ?? '',
                            i_pm10: raw.AqPm10U2 ?? '',
                            i_tvoc: raw.AqTvocU2 ?? '',
                            work_status: raw.WmodeU2 ?? '',

                            dwef_op_tm: raw.DwefOpTmU21 ?? '',
                            w_crnt_cf1: raw.ICfU21 !== undefined ? (Number(raw.ICfU21) / 10).toFixed(1) : '',
                            w_crnt_cf2: raw.ICfU22 !== undefined ? (Number(raw.ICfU22) / 10).toFixed(1) : '',
                            w_crnt_cp1: raw.ICpU21 !== undefined ? (Number(raw.ICpU21) / 10).toFixed(1) : '',
                            w_crnt_cp2: raw.ICpU22 !== undefined ? (Number(raw.ICpU22) / 10).toFixed(1) : '',
                            w_freq_cp1: raw.FCpU21 !== undefined ? (Number(raw.FCpU21) / 10).toFixed(1) : '',
                            w_freq_cp2: raw.FCpU22 !== undefined ? (Number(raw.FCpU22) / 10).toFixed(1) : '',
                            w_crnt_ef1: raw.IEfU21 !== undefined ? (Number(raw.IEfU21) / 10).toFixed(1) : '',
                            w_crnt_ef2: raw.IEfU22 !== undefined ? (Number(raw.IEfU22) / 10).toFixed(1) : '',

                            i_high_pres1: raw.HighpressU21 !== undefined ? (Number(raw.HighpressU21) / 10).toFixed(1) : '',
                            i_low_pres1: raw.SuckpU21 !== undefined ? (Number(raw.SuckpU21) / 10).toFixed(1) : '',
                            i_high_pres2: raw.HighpressU22 !== undefined ? (Number(raw.HighpressU22) / 10).toFixed(1) : '',
                            i_low_pres2: raw.SuckpU22 !== undefined ? (Number(raw.SuckpU22) / 10).toFixed(1) : '',

                            i_sat1: raw.SasU21 !== undefined ? (Number(raw.SasU21) / 10).toFixed(1) : '',
                            i_sat2: raw.SasU22 !== undefined ? (Number(raw.SasU22) / 10).toFixed(1) : '',
                            i_suckt1: raw.SucktU21 !== undefined ? (Number(raw.SucktU21) / 10).toFixed(1) : '',
                            i_suckt2: raw.SucktU22 !== undefined ? (Number(raw.SucktU22) / 10).toFixed(1) : '',
                            i_pres_diff: raw.PresdiffU2 !== undefined ? (Number(raw.PresdiffU2) / 10).toFixed(1) : '',
                            i_exufan: raw.IExufan !== undefined ? (Number(raw.IExufan) / 10).toFixed(1) : '',

                            dwcf_op_tm1: raw.DwcfOpTmU21 ?? '',
                            dwcf_op_tm2: raw.DwcfOpTmU22 ?? '',
                            dwcp_op_tm1: raw.DwcpOpTmU21 ?? '',
                            dwcp_op_tm2: raw.DwcpOpTmU22 ?? '',
                            dwexufan_op_tm: '', // 仅在机组一展示，避免重复

                            dwfad_op_cnt: raw.FadposU2 ?? '',
                            dwrad_op_cnt: raw.RadposU2 ?? '',
                            dmp_exu_pos: raw.DmpExuPos ?? '',
                            start_station: raw.StartStation ?? '',
                            terminal_station: raw.TerminalStation ?? '',
                            cur_station: raw.CurStation ?? '',
                            next_station: raw.NextStation ?? '',
                            line_id: row.line_id ?? '',
                        };

                const unitFilter = String(unitName || '');
                const units = [unit1, unit2];
                return unitFilter ? units.filter(u => u.unit_name === unitFilter) : units;
            });

            // 安全截断：防止奇数条原始记录导致返回数据超过前端pageSize（如19条原始→38条展示，没问题；
            // 但如果数据库返回了20条原始，展开40条，前端limit=39时就会多1条，这里做兜底截断）
            const finalList = expandedList.slice(0, limit);
            return {
                code: 200,
                data: {
                    list: finalList,
                    // 全部机组时总数翻倍，单选时保持原总数
                    total: isAllUnits ? result.total * 2 : result.total
                },
            };
        });

        fastify.get('/api/rest/carriage/SystemInfo/:carriageId', async (request: any) => {
            return await StatusRepository.getCarriageSystemInfo(request.params.carriageId);
        });

        fastify.get('/api/rest/carriage/HealthAssessment/:carriageId', async (request: any) => {
            return await StatusRepository.getCarriageHealthAssessment(request.params.carriageId);
        });

        // B6: 多参数趋势分析（params[] 为参数 key 数组，空则默认温度三线）
        fastify.post('/api/rest/carriage/TemperatureTrend', async (request: any) => {
            const { carriageNo, type = 'hour', params } = request.body;
            const trainId = parseInt(String(carriageNo).slice(0, 4));
            const carriageId = parseInt(String(carriageNo).slice(-2));
            const paramList = Array.isArray(params) && params.length > 0
                ? params
                : ['ras_u1', 'fas_u1', 'tic'];
            const data = await HistoryRepository.getParamTrend(trainId, carriageId, type as string, paramList);
            return { [type]: data };
        });

        // B6: 获取可选参数列表（供前端多选下拉使用）
        fastify.get('/api/rest/carriage/TrendParams', async () => {
            return {
                code: 200,
                data: Object.entries(TREND_PARAM_DEFS).map(([key, def]) => ({
                    key,
                    label: def.label,
                    unit: def.unit,
                }))
            };
        });

        // 历史数据流式导出 CSV（适配 historyData/index.vue 的"下载 ZIP"按钮）
        // 改进：单次请求流式输出，避免前端循环分页 + DB 重复 COUNT；响应头 X-Total-Rows 供前端进度展示
        //
        // 连接管理：streamRawHistory 在 transaction scope 内执行（含 SET LOCAL statement_timeout），
        // consumer 回调必须把 stream 消费完，回调返回后事务才 commit、连接才归还。
        fastify.get('/api/getTrainData/export', async (request: any, reply: any) => {
            const { state, carriageIds, startTime, endTime, unitName = '' } = request.query;

            // 参数解析与 /api/getTrainData 一致
            const trainId = state ? parseInt(String(state).slice(0, -2)) : undefined;
            const carriageId = state ? parseInt(String(state).slice(-2)) : undefined;

            let parsedCarriageIds: number[] | undefined;
            if (carriageIds) {
                parsedCarriageIds = String(carriageIds)
                    .split(',')
                    .map((c: string) => parseInt(c.trim()))
                    .filter((c: number) => !isNaN(c));
            }

            const isAllUnits = !unitName;

            // 时间范围校验：最多 7 天，与前端一致
            const start = new Date(startTime);
            const end = new Date(endTime);
            if (isNaN(start.getTime()) || isNaN(end.getTime())) {
                reply.code(400);
                return { code: 400, message: 'startTime/endTime 参数无效' };
            }
            const dayDiff = (end.getTime() - start.getTime()) / 86400000;
            if (dayDiff > 7) {
                reply.code(400);
                return { code: 400, message: '时间范围最多 7 天' };
            }

            // 车厢号 → 车型名映射（与 /api/getTrainData 路由一致）
            const CARRIAGE_MAP: Record<string, string> = { '1': 'TC1', '2': 'MP1', '3': 'M1', '4': 'M2', '5': 'MP2', '6': 'TC2' };

            // 文件名（车厢范围标签）
            const carriageLabel = parsedCarriageIds && parsedCarriageIds.length > 0
                ? parsedCarriageIds.map((c: number) => CARRIAGE_MAP[String(c)] || c).join('-')
                : 'all';
            const fname = `history_0${trainId ?? '0000'}_${carriageLabel}_${new Date().toISOString().slice(0, 10)}.csv`;

            // CSV 表头（与前端 CSV_HEADERS 一致）
            const CSV_HEADERS = [
                '序号', '列车号', '车厢', '机组',
                '车厢温度(℃)', '回风温度(℃)', '新风温度(℃)', '设定温度(℃)', '湿度(%)',
                '工作状态', '通风机运行时间(秒)',
                '冷凝风机电流1(A)', '冷凝风机电流2(A)',
                '压缩机电流1(A)', '压缩机电流2(A)',
                '压缩机频率1(Hz)', '压缩机频率2(Hz)',
                '送风机电流1(A)', '送风机电流2(A)',
                '高压1(bar)', '低压1(bar)', '高压2(bar)', '低压2(bar)', '压力差(bar)',
                '吸风温度1(℃)', '吸风温度2(℃)', '送风温度1(℃)', '送风温度2(℃)',
                '冷凝运行时间(秒)',
                '压缩机时间1(秒)', '压缩机时间2(秒)',
                '废排风机时间(秒)', '废排风机电流(A)', '新风阀开度(%)', '回风阀开度(%)',
                '废排风阀开度(%)', '过热度1(K)', '过热度2(K)',
                'CO₂(ppm)', 'PM2.5(ug/m³)', 'PM10(ug/m³)', 'TVOC(ppb)',
                '起始站', '终点站', '当前站', '下一站',
                '设备上报时间'
            ];

            // CSV 字段转义：null→空，含双引号/逗号/换行→双引号包裹并 "" 转义
            const esc = (v: any): string => {
                if (v === null || v === undefined) return '';
                const s = String(v);
                return /["\n,]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
            };

            const rowToLine = (row: any, idx: number): string => {
                const fields = [
                    idx + 1,
                    row.train_id ? `0${row.train_id}` : '',
                    CARRIAGE_MAP[String(row.carriage_id)] || row.carriage_id,
                    row.unit_name,
                    row.i_inner_temp, row.i_back_temp, row.i_outer_temp, row.i_set_temp, row.i_hum,
                    getModelName(row.work_status), row.dwef_op_tm,
                    row.w_crnt_cf1, row.w_crnt_cf2, row.w_crnt_cp1, row.w_crnt_cp2,
                    row.w_freq_cp1, row.w_freq_cp2, row.w_crnt_ef1, row.w_crnt_ef2,
                    row.i_high_pres1, row.i_low_pres1, row.i_high_pres2, row.i_low_pres2, row.i_pres_diff,
                    row.i_suckt1, row.i_suckt2, row.i_sat1, row.i_sat2,
                    row.dwcf_op_tm1, row.dwcp_op_tm1, row.dwcp_op_tm2,
                    row.dwexufan_op_tm, row.i_exufan, row.dwfad_op_cnt, row.dwrad_op_cnt,
                    row.dmp_exu_pos, row.i_sp_u1, row.i_sp_u2,
                    row.i_co2, row.i_pm25, row.i_pm10, row.i_tvoc,
                    getStationName(row.line_id, row.start_station),
                    getStationName(row.line_id, row.terminal_station),
                    getStationName(row.line_id, row.cur_station),
                    getStationName(row.line_id, row.next_station),
                    row.event_time
                ];
                return fields.map(esc).join(',');
            };

            // 调用 streamRawHistory：在 transaction scope 内执行
            // consumer 回调必须把 stream 消费完，返回后事务才 commit
            try {
                await StatusRepository.streamRawHistory(
                    {
                        trainId,
                        carriageId,
                        carriageIds: parsedCarriageIds && parsedCarriageIds.length > 0 ? parsedCarriageIds : undefined,
                        startTime: String(startTime || ''),
                        endTime: String(endTime || ''),
                    },
                    async (stream, factTotal) => {
                        // 全部机组时总数翻倍（与 /api/getTrainData 语义一致）
                        const displayTotal = isAllUnits ? factTotal * 2 : factTotal;

                        // 在 consumer 内设置响应头：此时已拿到 total，可以填 X-Total-Rows
                        reply.header('Content-Type', 'text/csv; charset=utf-8');
                        reply.header('Content-Disposition', `attachment; filename="${fname}"`);
                        reply.header('X-Total-Rows', String(displayTotal));
                        reply.header('Cache-Control', 'no-store');

                        // 写入 BOM + CSV 表头
                        reply.raw.write('\uFEFF' + CSV_HEADERS.join(',') + '\n');

                        let idx = 0;
                        try {
                            for await (const row of stream) {
                                const raw = row.payload_json?.raw || {};
                                const eventTimeStr = formatTime(row.event_time);

                                const unit1 = {
                                    train_id: row.train_id,
                                    carriage_id: row.carriage_id,
                                    event_time: eventTimeStr,
                                    unit_name: '机组一',
                                    i_inner_temp: raw.Tveh1 !== undefined ? (Number(raw.Tveh1) / 10).toFixed(1) : '',
                                    i_back_temp: raw.RasU1 !== undefined ? (Number(raw.RasU1) / 10).toFixed(1) : '',
                                    i_outer_temp: raw.FasU1 !== undefined ? (Number(raw.FasU1) / 10).toFixed(1) : '',
                                    i_set_temp: raw.Tic !== undefined ? (Number(raw.Tic) / 10).toFixed(1) : '',
                                    i_hum: raw.AqHU1 !== undefined ? (Number(raw.AqHU1) / 10).toFixed(1) : '',
                                    i_sp_u1: raw.SpU11 !== undefined ? (Number(raw.SpU11) / 10).toFixed(1) : '',
                                    i_sp_u2: raw.SpU12 !== undefined ? (Number(raw.SpU12) / 10).toFixed(1) : '',
                                    i_co2: raw.AqCo2U1 ?? '',
                                    i_pm25: raw.AqPm25U1 ?? '',
                                    i_pm10: raw.AqPm10U1 ?? '',
                                    i_tvoc: raw.AqTvocU1 ?? '',
                                    work_status: raw.WmodeU1 ?? '',
                                    dwef_op_tm: raw.DwefOpTmU11 ?? '',
                                    w_crnt_cf1: raw.ICfU11 !== undefined ? (Number(raw.ICfU11) / 10).toFixed(1) : '',
                                    w_crnt_cf2: raw.ICfU12 !== undefined ? (Number(raw.ICfU12) / 10).toFixed(1) : '',
                                    w_crnt_cp1: raw.ICpU11 !== undefined ? (Number(raw.ICpU11) / 10).toFixed(1) : '',
                                    w_crnt_cp2: raw.ICpU12 !== undefined ? (Number(raw.ICpU12) / 10).toFixed(1) : '',
                                    w_freq_cp1: raw.FCpU11 !== undefined ? (Number(raw.FCpU11) / 10).toFixed(1) : '',
                                    w_freq_cp2: raw.FCpU12 !== undefined ? (Number(raw.FCpU12) / 10).toFixed(1) : '',
                                    w_crnt_ef1: raw.IEfU11 !== undefined ? (Number(raw.IEfU11) / 10).toFixed(1) : '',
                                    w_crnt_ef2: raw.IEfU12 !== undefined ? (Number(raw.IEfU12) / 10).toFixed(1) : '',
                                    i_high_pres1: raw.HighpressU11 !== undefined ? (Number(raw.HighpressU11) / 10).toFixed(1) : '',
                                    i_low_pres1: raw.SuckpU11 !== undefined ? (Number(raw.SuckpU11) / 10).toFixed(1) : '',
                                    i_high_pres2: raw.HighpressU12 !== undefined ? (Number(raw.HighpressU12) / 10).toFixed(1) : '',
                                    i_low_pres2: raw.SuckpU12 !== undefined ? (Number(raw.SuckpU12) / 10).toFixed(1) : '',
                                    i_sat1: raw.SasU11 !== undefined ? (Number(raw.SasU11) / 10).toFixed(1) : '',
                                    i_sat2: raw.SasU12 !== undefined ? (Number(raw.SasU12) / 10).toFixed(1) : '',
                                    i_suckt1: raw.SucktU11 !== undefined ? (Number(raw.SucktU11) / 10).toFixed(1) : '',
                                    i_suckt2: raw.SucktU12 !== undefined ? (Number(raw.SucktU12) / 10).toFixed(1) : '',
                                    i_pres_diff: raw.PresdiffU1 !== undefined ? (Number(raw.PresdiffU1) / 10).toFixed(1) : '',
                                    i_exufan: raw.IExufan !== undefined ? (Number(raw.IExufan) / 10).toFixed(1) : '',
                                    dwcf_op_tm1: raw.DwcfOpTmU11 ?? '',
                                    dwcp_op_tm1: raw.DwcpOpTmU11 ?? '',
                                    dwcp_op_tm2: raw.DwcpOpTmU12 ?? '',
                                    dwexufan_op_tm: raw.DwexufanOpTm ?? '',
                                    dwfad_op_cnt: raw.FadposU1 ?? '',
                                    dwrad_op_cnt: raw.RadposU1 ?? '',
                                    dmp_exu_pos: raw.DmpExuPos ?? '',
                                    start_station: raw.StartStation ?? '',
                                    terminal_station: raw.TerminalStation ?? '',
                                    cur_station: raw.CurStation ?? '',
                                    next_station: raw.NextStation ?? '',
                                    line_id: row.line_id ?? '',
                                };
                                const unit2 = {
                                    train_id: row.train_id,
                                    carriage_id: row.carriage_id,
                                    event_time: eventTimeStr,
                                    unit_name: '机组二',
                                    i_inner_temp: raw.Tveh2 !== undefined ? (Number(raw.Tveh2) / 10).toFixed(1) : '',
                                    i_back_temp: raw.RasU2 !== undefined ? (Number(raw.RasU2) / 10).toFixed(1) : '',
                                    i_outer_temp: raw.FasU2 !== undefined ? (Number(raw.FasU2) / 10).toFixed(1) : '',
                                    i_set_temp: raw.Tic !== undefined ? (Number(raw.Tic) / 10).toFixed(1) : '',
                                    i_sp_u1: raw.SpU21 !== undefined ? (Number(raw.SpU21) / 10).toFixed(1) : '',
                                    i_sp_u2: raw.SpU22 !== undefined ? (Number(raw.SpU22) / 10).toFixed(1) : '',
                                    i_hum: raw.AqHU2 !== undefined ? (Number(raw.AqHU2) / 10).toFixed(1) : '',
                                    i_co2: raw.AqCo2U2 ?? '',
                                    i_pm25: raw.AqPm25U2 ?? '',
                                    i_pm10: raw.AqPm10U2 ?? '',
                                    i_tvoc: raw.AqTvocU2 ?? '',
                                    work_status: raw.WmodeU2 ?? '',
                                    dwef_op_tm: raw.DwefOpTmU21 ?? '',
                                    w_crnt_cf1: raw.ICfU21 !== undefined ? (Number(raw.ICfU21) / 10).toFixed(1) : '',
                                    w_crnt_cf2: raw.ICfU22 !== undefined ? (Number(raw.ICfU22) / 10).toFixed(1) : '',
                                    w_crnt_cp1: raw.ICpU21 !== undefined ? (Number(raw.ICpU21) / 10).toFixed(1) : '',
                                    w_crnt_cp2: raw.ICpU22 !== undefined ? (Number(raw.ICpU22) / 10).toFixed(1) : '',
                                    w_freq_cp1: raw.FCpU21 !== undefined ? (Number(raw.FCpU21) / 10).toFixed(1) : '',
                                    w_freq_cp2: raw.FCpU22 !== undefined ? (Number(raw.FCpU22) / 10).toFixed(1) : '',
                                    w_crnt_ef1: raw.IEfU21 !== undefined ? (Number(raw.IEfU21) / 10).toFixed(1) : '',
                                    w_crnt_ef2: raw.IEfU22 !== undefined ? (Number(raw.IEfU22) / 10).toFixed(1) : '',
                                    i_high_pres1: raw.HighpressU21 !== undefined ? (Number(raw.HighpressU21) / 10).toFixed(1) : '',
                                    i_low_pres1: raw.SuckpU21 !== undefined ? (Number(raw.SuckpU21) / 10).toFixed(1) : '',
                                    i_high_pres2: raw.HighpressU22 !== undefined ? (Number(raw.HighpressU22) / 10).toFixed(1) : '',
                                    i_low_pres2: raw.SuckpU22 !== undefined ? (Number(raw.SuckpU22) / 10).toFixed(1) : '',
                                    i_sat1: raw.SasU21 !== undefined ? (Number(raw.SasU21) / 10).toFixed(1) : '',
                                    i_sat2: raw.SasU22 !== undefined ? (Number(raw.SasU22) / 10).toFixed(1) : '',
                                    i_suckt1: raw.SucktU21 !== undefined ? (Number(raw.SucktU21) / 10).toFixed(1) : '',
                                    i_suckt2: raw.SucktU22 !== undefined ? (Number(raw.SucktU22) / 10).toFixed(1) : '',
                                    i_pres_diff: raw.PresdiffU2 !== undefined ? (Number(raw.PresdiffU2) / 10).toFixed(1) : '',
                                    i_exufan: raw.IExufan !== undefined ? (Number(raw.IExufan) / 10).toFixed(1) : '',
                                    dwcf_op_tm1: raw.DwcfOpTmU21 ?? '',
                                    dwcp_op_tm1: raw.DwcpOpTmU21 ?? '',
                                    dwcp_op_tm2: raw.DwcpOpTmU22 ?? '',
                                    dwexufan_op_tm: '',
                                    dwfad_op_cnt: raw.FadposU2 ?? '',
                                    dwrad_op_cnt: raw.RadposU2 ?? '',
                                    dmp_exu_pos: raw.DmpExuPos ?? '',
                                    start_station: raw.StartStation ?? '',
                                    terminal_station: raw.TerminalStation ?? '',
                                    cur_station: raw.CurStation ?? '',
                                    next_station: raw.NextStation ?? '',
                                    line_id: row.line_id ?? '',
                                };

                                const units = isAllUnits ? [unit1, unit2] : [String(unitName) === '机组二' ? unit2 : unit1];
                                for (const u of units) {
                                    reply.raw.write(rowToLine(u, idx) + '\n');
                                    idx++;
                                }
                            }
                            reply.raw.end();
                        } catch (streamErr: any) {
                            request.log.error(streamErr, 'history export stream write failed');
                            // 流已经写出过部分数据，无法切换为 JSON 错误响应；写入末尾错误注释后结束
                            reply.raw.write(`\n# ERROR: ${streamErr?.message || 'stream failed'}\n`);
                            reply.raw.end();
                            // 抛回上层让 transaction 也 rollback
                            throw streamErr;
                        }
                    }
                );
            } catch (err: any) {
                request.log.error(err, 'history export stream failed');
                // 若 reply 已开始写入（headers sent），无法切换为 JSON 错误响应
                if (!reply.sent) {
                    reply.code(500);
                    return { code: 500, message: err?.message || 'export failed' };
                }
            }
        });

        // B2: 历史报警导出 CSV
        fastify.get('/api/export/alarm', async (request: any, reply: any) => {
            const { trainNo, carriageNos, unitNames, startTime, endTime } = request.query;
            const trainId = trainNo ? parseInt(String(trainNo)) : undefined;
            const cids = carriageNos ? String(carriageNos).split(',').map(Number).filter(n => !isNaN(n)) : undefined;
            const units = unitNames ? String(unitNames).split(',') : [];

            const result = await StatusRepository.getHistoricalEvents({
                trainId, carriageIds: cids, unitNames: units,
                startTime: startTime || '', endTime: endTime || '',
                eventType: 'alarm', page: 1, limit: 10000
            });

            const formatUnit = (code: string) => code?.toLowerCase().includes('u2') ? '机组二' : code?.toLowerCase().includes('u1') ? '机组一' : '-';
            const CARRIAGE_MAP: Record<string, string> = { '1':'TC1','2':'MP1','3':'M1','4':'M2','5':'MP2','6':'TC2' };

            const headers = ['列车号','车厢','机组','严重级别','故障详情','开始时间','结束时间','状态'];
            const rows = result.list.map((r: any) => {
                // 报警导出 severity：1→正常 2→中等 3→严重
                const level = r.severity === 3 ? '严重' : r.severity === 2 ? '中等' : '正常';
                return [
                    `0${r.train_id}`,
                    CARRIAGE_MAP[String(r.carriage_id)] || r.carriage_id,
                    formatUnit(r.fault_code),
                    level,
                    r.fault_name || '',
                    r.event_time ? formatTime(r.event_time) : '',
                    r.recovery_time ? formatTime(r.recovery_time) : '',
                    r.recovery_time ? '已恢复' : '活动'
                ].map((v: any) => `"${String(v).replace(/"/g, '""')}"`).join(',');
            });

            const bom = '﻿';
            const csv = bom + headers.join(',') + '\n' + rows.join('\n');
            reply.header('Content-Type', 'text/csv; charset=utf-8');
            reply.header('Content-Disposition', `attachment; filename="alarm_export_${Date.now()}.csv"`);
            return reply.send(csv);
        });

        // B3: 历史预警查询
        fastify.post('/api/rest/train/HistoryWarning', async (request: any) => {
            const { trainNo, carriageNos, unitNames, startTime, endTime, page, limit } = request.body;
            const trainId = trainNo ? parseInt(String(trainNo)) : undefined;
            const cids = carriageNos && Array.isArray(carriageNos) && carriageNos.length > 0
                ? carriageNos.map((n: any) => parseInt(n)).filter((n: number) => !isNaN(n))
                : undefined;

            try {
                const result = await StatusRepository.getHistoricalWarnings({
                    trainId, carriageIds: cids, unitNames: unitNames || [],
                    startTime: startTime || '', endTime: endTime || '',
                    page: Math.max(1, parseInt(page) || 1),
                    limit: Math.max(1, parseInt(limit) || 10)
                });

                const CARRIAGE_MAP: Record<string, string> = { '1':'TC1','2':'MP1','3':'M1','4':'M2','5':'MP2','6':'TC2' };
                const formatUnit = (code: string) => {
                    if (!code) return '-';
                    const c = code.toLowerCase();
                    // 数字后缀型（HVAC编码）：奇数seq对应机组1，偶数对应机组2
                    if (c.startsWith('hvac')) {
                        const seq = parseInt(code.replace(/[^0-9]/g, '')) % 100;
                        // 车厢级预警（不属于任何机组）
                        const carriageLevelSeqs = new Set([7,8,9,20,76]);
                        if (carriageLevelSeqs.has(seq)) return '-';
                        if ([1,2,5,10,12,13,16,17,21,22,25].includes(seq)) return '机组一';
                        if ([3,4,6,11,14,15,18,19,23,24,26].includes(seq)) return '机组二';
                    }
                    if (c.includes('u2')) return '机组二';
                    if (c.includes('u1')) return '机组一';
                    return '-';
                };
                // HVAC seq → 触发条件描述（当warning_config无匹配时回填）
                const HVAC_SEQ_STRATEGY: Record<string, string> = {
                    '1':'制冷模式吸气压力<2.0bar持续5分钟，或通风模式高压<5bar持续15分钟',
                    '2':'制冷模式吸气压力<2.0bar持续5分钟，或通风模式高压<5bar持续15分钟',
                    '3':'制冷模式吸气压力<2.0bar持续5分钟，或通风模式高压<5bar持续15分钟',
                    '4':'制冷模式吸气压力<2.0bar持续5分钟，或通风模式高压<5bar持续15分钟',
                    '5':'同机组压缩机电流差>2A持续3分钟，或过热度>20℃/<-8℃持续10分钟',
                    '6':'同机组压缩机电流差>2A持续3分钟，或过热度>20℃/<-8℃持续10分钟',
                    '7':'两机组新风温度差值>8℃持续8分钟',
                    '8':'两机组回风温度差值>8℃持续8分钟',
                    '9':'制冷运行>20分钟且无故障，回风温度>目标温度+4℃持续2分钟',
                    '10':'通风机运行中滤网压差>300pa持续30分钟',
                    '11':'通风机运行中滤网压差>300pa持续30分钟',
                    '12':'通风机运行中电流>1.8A持续10分钟',
                    '13':'通风机运行中电流>1.8A持续10分钟',
                    '14':'通风机运行中电流>1.8A持续10分钟',
                    '15':'通风机运行中电流>1.8A持续10分钟',
                    '16':'冷凝风机运行中电流>2.3A持续10分钟',
                    '17':'冷凝风机运行中电流>2.3A持续10分钟',
                    '18':'冷凝风机运行中电流>2.3A持续10分钟',
                    '19':'冷凝风机运行中电流>2.3A持续10分钟',
                    '20':'废排风机运行中电流>2.3A持续10分钟',
                    '21':'新风温度<35℃且压缩机电流>18A持续10分钟',
                    '22':'新风温度<35℃且压缩机电流>18A持续10分钟',
                    '23':'新风温度<35℃且压缩机电流>18A持续10分钟',
                    '24':'新风温度<35℃且压缩机电流>18A持续10分钟',
                    '25':'通风机运行>20分钟，CO₂>4500ppm持续15分钟或PM/TVOC超标持续20分钟',
                    '26':'通风机运行>20分钟，CO₂>4500ppm持续15分钟或PM/TVOC超标持续20分钟',
                    '76':'两机组车厢温度差值>8℃持续8分钟',
                };
                const getTriggerCondition = (row: any): string => {
                    if (row.trigger_condition) return row.trigger_condition;
                    const code = String(row.fault_code || '');
                    if (code.toUpperCase().startsWith('HVAC')) {
                        const seq = String(parseInt(code.replace(/[^0-9]/g, '')) % 100);
                        return HVAC_SEQ_STRATEGY[seq] || row.fault_name || '';
                    }
                    return row.fault_name || '';
                };

                const list = result.list.map((row: any) => ({
                    train_id: row.train_id,
                    carriage_no: CARRIAGE_MAP[String(row.carriage_id)] || row.carriage_id,
                    unit_name: formatUnit(row.fault_code || ''),
                    severity: '轻微',  // 预警严重级别固定为"轻微"
                    warn_name: row.fault_name,
                    fault_code: row.fault_code,
                    fault_desc: row.fault_name,  // 完整预警描述（来自 alertcode_v2 fault_desc）
                    trigger_condition: getTriggerCondition(row),
                    start_time: formatTime(row.occurrence_start ?? row[config.runtime === 'DEV' ? 'ingest_time' : 'event_time']),
                    end_time: row.recovery_time ? formatTime(row.recovery_time) : null,
                }));

                return { code: 200, data: { list, total: result.total } };
            } catch (err: any) {
                fastify.log.error(err);
                return { code: 500, message: err.message, data: { list: [], total: 0 } };
            }
        });

        // B4: 历史预警导出 CSV
        fastify.get('/api/export/warning', async (request: any, reply: any) => {
            const { trainNo, carriageNos, unitNames, startTime, endTime } = request.query;
            const trainId = trainNo ? parseInt(String(trainNo)) : undefined;
            const cids = carriageNos ? String(carriageNos).split(',').map(Number).filter(n => !isNaN(n)) : undefined;
            const units = unitNames ? String(unitNames).split(',') : [];

            const result = await StatusRepository.getHistoricalWarnings({
                trainId, carriageIds: cids, unitNames: units,
                startTime: startTime || '', endTime: endTime || '',
                page: 1, limit: 10000
            });

            const CARRIAGE_MAP: Record<string, string> = { '1':'TC1','2':'MP1','3':'M1','4':'M2','5':'MP2','6':'TC2' };
            const formatUnit = (code: string) => {
                if (!code) return '-';
                const c = code.toLowerCase();
                if (c.startsWith('hvac')) {
                    const seq = parseInt(code.replace(/[^0-9]/g, '')) % 100;
                    // 车厢级预警（不属于任何机组）
                    const carriageLevelSeqs = new Set([7,8,9,20,76]);
                    if (carriageLevelSeqs.has(seq)) return '';
                    if ([1,3,5,10,12,13,16,17,21,22,25].includes(seq)) return '机组一';
                    if ([2,4,6,11,14,15,18,19,23,24,26].includes(seq)) return '机组二';
                }
                if (c.includes('u2')) return '机组二';
                if (c.includes('u1')) return '机组一';
                return '-';
            };
            const headers = ['列车号','车厢','机组','严重级别','预警名称','触发条件','开始时间','结束时间'];
            const rows = result.list.map((r: any) => {
                return [
                    `0${r.train_id}`,
                    CARRIAGE_MAP[String(r.carriage_id)] || r.carriage_id,
                    formatUnit(r.fault_code),
                    '轻微',  // 预警严重级别固定为"轻微"
                    r.fault_name || '',
                    r.trigger_condition || '',
                    formatTime(r.occurrence_start ?? r[config.runtime === 'DEV' ? 'ingest_time' : 'event_time']),
                    r.recovery_time ? formatTime(r.recovery_time) : ''
                ].map((v: any) => `"${String(v).replace(/"/g, '""')}"`).join(',');
            });

            const csv = '﻿' + headers.join(',') + '\n' + rows.join('\n');
            reply.header('Content-Type', 'text/csv; charset=utf-8');
            reply.header('Content-Disposition', `attachment; filename="warning_export_${Date.now()}.csv"`);
            return reply.send(csv);
        });

        // B5: 预警详情——触发前30分钟参数曲线
        fastify.get('/api/rest/predict/detail', async (request: any) => {
            const { trainId, carriageId, triggerTime, warnCode } = request.query;
            if (!trainId || !carriageId || !triggerTime) {
                return { code: 400, message: 'trainId/carriageId/triggerTime 必填' };
            }
            try {
                // triggerTime 由 formatTime 格式化为上海时区，需用 dayjs.tz 解析避免偏差8小时
                const { dayjs: djs } = await import('./utils/time.js');
                const triggerDate = djs.tz(triggerTime, 'Asia/Shanghai').toDate();
                const data = await HistoryRepository.getPredictDetailCurve(
                    parseInt(trainId),
                    parseInt(carriageId),
                    triggerDate,
                    String(warnCode || '')
                );
                return { code: 200, data };
            } catch (err: any) {
                return { code: 500, message: err.message };
            }
        });

        // B7: 预警条件配置 CRUD
        fastify.get('/api/rest/warning/config', async () => {
            const list = await StatusRepository.getWarningConfigs();
            return { code: 200, data: list };
        });

        fastify.get('/api/rest/warning/config/:id', async (request: any) => {
            const item = await StatusRepository.getWarningConfig(parseInt(request.params.id));
            return item ? { code: 200, data: item } : { code: 404, message: '未找到' };
        });

        fastify.put('/api/rest/warning/config/:id', async (request: any) => {
            try {
                await StatusRepository.updateWarningConfig(parseInt(request.params.id), request.body);
                return { code: 200, message: '更新成功' };
            } catch (err: any) {
                return { code: 500, message: err.message };
            }
        });

        fastify.post('/api/rest/warning/config/:id/reset', async (request: any) => {
            try {
                await StatusRepository.resetWarningConfig(parseInt(request.params.id));
                return { code: 200, message: '已重置为出厂设置' };
            } catch (err: any) {
                return { code: 500, message: err.message };
            }
        });

        // B8: 最新数据时间戳（前端离线检测用）
        fastify.get('/api/rest/train/LatestDataTime', async (request: any) => {
            const { trainId, carriageId } = request.query;
            if (!trainId || !carriageId) return { code: 400, message: 'trainId/carriageId 必填' };
            try {
                const latest = await StatusRepository.getLatestDataTime(
                    parseInt(String(trainId)),
                    parseInt(String(carriageId))
                );
                return { code: 200, data: { latest_time: latest ? formatTime(latest) : null } };
            } catch (err: any) {
                return { code: 500, message: err.message };
            }
        });

        // 5. 启动 Kafka 消费者并广播
        const kafka = KafkaManager.getInstance();
        await kafka.start((topic, data) => {
            // 广播给所有 WebSocket 客户端
            const payload = JSON.stringify({ topic, data });
            fastify.websocketServer.clients.forEach((client) => {
                if (client.readyState === 1) { // OPEN
                    client.send(payload);
                }
            });
        });

        // 6. 运行监听
        await fastify.ready();
        console.log('[BFF] All Routes registered:');
        console.log(fastify.printRoutes());

        await fastify.listen({ port: config.port, host: config.host });
        console.log(`[BFF] Server V2-DISTINCT is running at http://${config.host}:${config.port}`);
    } catch (err) {
        fastify.log.error(err);
        process.exit(1);
    }
}

bootstrap();
