import request from '../utils/request.js'

//获取指定列车空调运行状态
export const getAirStartDataApi = (params) => request('/api/airStartData', 'get', params)

//取在线列车
// export const getActiveCarApi = () => request('/api/rest/train', 'get')
export const getActiveCarApi = () => request('/api/getActiveCar', 'get')
// 空调系统
export const getAirSystemApi = () => request('/api/rest/AirSystem', 'get')
// 实时报警
export const getRealtimeAlarm = (params) => request('/api/rest/v2/train/RealtimeAlarm', 'post', params)
// 实时预警
export const getRealtimeWarning = () => request('/api/rest/RealtimeWarning', 'get')
// 空调故障统计
export const getFaultStatistics = (params) => request('/api/rest/v2/FaultStatistics', 'post', params)
// 列车详情-车厢实时报警
export const getRealtimeAlarmDetail = (trainID) => request('/api/rest/train/RealtimeAlarm/' + trainID, 'get')
// 列车详情-车厢状态预警
export const getStatusAlert = (trainID) => request('/api/rest/train/StatusAlert/' + trainID, 'get')
// 列车详情-车厢选择
export const getTrainSelection = (trainId) => request('/api/rest/train/TrainSelection/' + trainId, 'get')
// 列车详情-报警信息
// export const getAlarmInformation = (trainId) => request('/api/rest/train/AlarmInformation/' + trainId, 'get')
export const getAlarmInformation = (params) => request('/api/rest/train/AlarmInformation', 'post', params)

// export const getAlarmInformation = (params) => request('/v1/graphql', 'post', params)
// 列车详情-所有机组运行状态
export const getRunningState = (trainID) => request('/api/rest/train/RunningState/' + trainID, 'get')
// 列车详情-车厢温度信息
export const getTrainTemperature = (trainID) => request('/api/rest/train/TrainTemperature/' + trainID, 'get')
// 列车详情-车厢详情-机组详情-健康状态
export const getHealthAssessment = (carriageID) => request('/api/rest/carriage/HealthAssessment/' + carriageID, 'get')
// 列车详情-车厢详情-机组详情-系统温度1
export const gettemperature = (carriageID) => request('/api/rest/carriage/SystemInfo/' + carriageID, 'get')
// 列车详情-车厢详情-机组详情-系统温度1
export const getSystemFirst = (carriageID) => request('/api/rest/carriage/SystemInfo/' + carriageID, 'get')
// 列车详情-车厢详情-机组详情-系统温度2
export const getSystemSecond = (carriageID) => request('/api/rest/carriage/SystemSecond/' + carriageID, 'get')
//获取在线列车数据
export const getActiveCarNumApi = () => request('/api/getActiveCarNum', 'get')

//获取在线列车告警数据
export const getCarEaDataApi = () => request('/api/getActiveCarEaData', 'get')

//获取在线列车预警数据
export const getCarPaDataApi = () => request('/api/getActiveCarPaData', 'get')

//获取告警统计数据
export const getEaDataApi = (params) => request('/api/getEaData', 'get', params)

// 获取指定列车的告警或预警数据 
export const getEaDataBydvcAddrApi = (params) => request('/api/getEaDataBydvcAddr', 'get', { restore: true, ...params })

//获取指定列车具体车厢最近10分钟内最新一条数据的健康评估信息数据
export const getHealthDataApi = (id) => request('/api/getHealthData', 'get', { FL_dvcAddr: id })

// 获取指定列车具体车厢最近10分钟内最新一条数据
export const getNewDataApi = (id) => request('/api/getNewData', 'get', { FL_dvcAddr: id })

//获取在线列车指定列车预警数据
export const getPaDataByDvcAddrApi = () => request('/api/getPaDataByDvcAddr', 'get')

// 获取在线列车指定列车实时统计数据
export const getStatisDataByDvcAddrApi = () => request('/api/getStatisDataByDvcAddr', 'get')

// 获取在线列车指定列车实时数据
export const getThDataByDvcAddrApi = (params) => request('/api/rest/carriage/TemperatureTrend', 'post', params)

// 获取指定列车最近10分钟的各节车厢数据
export const getTrainDataApi = (params) => request('/api/getTrainData', 'get', params)

// 流式导出历史数据 CSV（适配 historyData/index.vue 的"下载 ZIP"按钮）
// 返回完整 URL，由前端 downloadStream 工具消费（fetch + ReadableStream + 连接重试）
export const getTrainDataExportUrl = (params) => {
    const qs = new URLSearchParams(params).toString()
    return `/api/getTrainData/export?${qs}`
}

// 获取指定时间范围内存有数据的对应日期列表
export const getTrainDataDatesApi = (params) => request('/api/getTrainDataDates', 'get', params)

//根据id获取数据
export const getEaOrPaById = (params) => request('/api/getEaOrPaById', 'get', params)

// B3: 历史预警查询
export const getHistoryWarning = (params) => request('/api/rest/train/HistoryWarning', 'post', params)

// B2/B4: 报警/预警导出（直接拼URL跳转下载）
export const getAlarmExportUrl = (params) => {
    const qs = new URLSearchParams(params).toString()
    return `/api/export/alarm?${qs}`
}
export const getWarningExportUrl = (params) => {
    const qs = new URLSearchParams(params).toString()
    return `/api/export/warning?${qs}`
}

// B5: 预警详情曲线
export const getPredictDetail = (params) => request('/api/rest/predict/detail', 'get', params)

// B6: 参数列表
export const getTrendParamsApi = () => request('/api/rest/carriage/TrendParams', 'get')

// B7: 预警条件配置 CRUD
export const getWarningConfigs = () => request('/api/rest/warning/config', 'get')
export const updateWarningConfig = (id, data) => request(`/api/rest/warning/config/${id}`, 'put', data)
export const resetWarningConfig = (id) => request(`/api/rest/warning/config/${id}/reset`, 'post')

// B8: 最新数据时间（离线检测）
export const getLatestDataTime = (params) => request('/api/rest/train/LatestDataTime', 'get', params)