import { Pool } from 'pg';
import { Kysely, PostgresDialect } from 'kysely';
import Cursor from 'pg-cursor';
import type { Database } from '../repository/types.js';
import { config } from './index.js';

/**
 * 初始化 TimescaleDB 连接池
 */
const pool = new Pool({
    ...config.db,
    connectionTimeoutMillis: 5000,
    idleTimeoutMillis: 30000,
});

pool.on('error', (err) => {
    console.error('[Postgres Pool] Unexpected error on idle client', err);
});

/**
 * Kysely 强类型查询构造器实例
 *
 * cursor：pg-cursor 模块构造器，Kysely 用它实现 .stream() 流式查询。
 * 未配置时调用 .stream() 会抛错 "cursor is not present in your postgres dialect config"。
 */
export const db = new Kysely<Database>({
    dialect: new PostgresDialect({
        pool,
        cursor: Cursor,
    }),
    log: (event) => {
        if (event.level === 'query') {
            console.log('[SQL Query]:', event.query.sql);
            console.log('[SQL Params]:', event.query.parameters);
        }
    }
});

/**
 * 测试数据库连接
 */
export async function checkDbConnection() {
    try {
        const result = await db.selectFrom('hvac.fact_raw' as any)
            .select(({ fn }) => [fn.countAll().as('total')])
            .executeTakeFirst();
        console.log(`[Database] Connected to TimescaleDB. Sample count check: ${JSON.stringify(result)}`);
    } catch (err) {
        console.error('[Database] Failed to connect to TimescaleDB:', err);
    }
}
