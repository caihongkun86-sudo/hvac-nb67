/**
 * 实时监控全局配置中心
 * 用于统一管理全站的刷新频率、超时时间等关键参数
 */
export const MONITOR_CONFIG = {
    // 数据自动刷新间隔 (单位: 毫秒)
    // 默认 5000ms (5秒)，可根据服务器负载灵活调整
    refreshInterval: Number(import.meta.env.VITE_REFRESH_INTERVAL) || 20000,

    // 图表自适应防抖延迟
    resizeDebounce: 300,

    // WebSocket 重连心跳间隔
    wsHeartbeatInterval: 10000
}
