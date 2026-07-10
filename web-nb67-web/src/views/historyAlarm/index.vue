<template>
    <div class="warp2">
        <div class="header-monitor">
            <div class="header-left">
                <el-button type="primary" class="nav-btn" icon="ArrowLeft" @click="goBack">返回</el-button>
            </div>
            <div class="header-right">
                <el-form :inline="true" :model="filterForm" class="filter-form">
                    <el-form-item label="车号">
                        <el-select v-model="filterForm.trainNo" placeholder="请选择车号" class="train-select">
                            <el-option v-for="item in trainList" :key="item.train_id" :label="item.label" :value="item.train_id" />
                        </el-select>
                    </el-form-item>
                    <el-form-item label="车厢">
                        <el-select v-model="filterForm.carriageNos" placeholder="全部车厢" multiple collapse-tags clearable class="carriage-select" style="width:200px">
                            <el-option v-for="item in carriageList" :key="item.carriage_no" :label="item.label" :value="item.carriage_no" />
                        </el-select>
                    </el-form-item>
                    <el-form-item label="机组">
                        <el-select v-model="filterForm.unitName" style="width:110px">
                            <el-option label="全部机组" value="" />
                            <el-option label="机组一" value="机组一" />
                            <el-option label="机组二" value="机组二" />
                        </el-select>
                    </el-form-item>
                    <el-form-item label="时间范围">
                        <el-date-picker
                            v-model="filterForm.timeRange"
                            type="datetimerange"
                            range-separator="至"
                            start-placeholder="开始时间"
                            end-placeholder="结束时间"
                            format="YYYY-MM-DD HH:mm:ss"
                            value-format="YYYY-MM-DD HH:mm:ss"
                            :shortcuts="shortcuts"
                            :default-time="[new Date(2000, 1, 1, 0, 0, 0), new Date(2000, 1, 1, 23, 59, 59)]"
                        />
                    </el-form-item>
                    <el-form-item>
                        <el-button type="primary" class="nav-btn" icon="Search" @click="handleQuery">查询</el-button>
                        <el-button type="primary" class="nav-btn" icon="Download" @click="handleExport">导出</el-button>
                    </el-form-item>
                </el-form>
            </div>
        </div>

        <div class="monitor-container">
            <div class="section-box">
                <div class="section-header">
                    <span class="title-icon"></span>
                    <span class="title-text">历史报警</span>
                </div>
                <el-table :data="tableData" border stripe style="width: 100%" v-loading="loading">
                    <el-table-column prop="train_id" label="列车号" align="center" width="100">
                        <template #default="scope">{{ scope.row.train_id ? `0${scope.row.train_id}` : '-' }}</template>
                    </el-table-column>
                    <el-table-column prop="carriage_no" label="车厢" align="center" width="90">
                        <template #default="scope">{{ getCarriageName(scope.row.carriage_no) }}</template>
                    </el-table-column>
                    <el-table-column prop="unit_name" label="机组" align="center" width="90" />
                    <el-table-column prop="fault_level" label="严重级别" align="center" width="100">
                        <template #default="scope">
                            <span :class="['severity-text', scope.row.fault_level === '严重' ? 'danger' : scope.row.fault_level === '轻微' ? 'info' : 'warning']">
                                {{ scope.row.fault_level }}
                            </span>
                        </template>
                    </el-table-column>
                    <el-table-column prop="fault_desc" label="故障详情" header-align="center" min-width="200" show-overflow-tooltip />
                    <el-table-column label="开始时间" align="center" width="175" show-overflow-tooltip>
                        <template #default="scope">{{ scope.row.occurrence_time }}</template>
                    </el-table-column>
                    <el-table-column label="结束时间" align="center" width="175" show-overflow-tooltip>
                        <template #default="scope">{{ scope.row.recovery_time || '-' }}</template>
                    </el-table-column>
                </el-table>

                <div class="pagination-container">
                    <el-pagination
                        v-model:current-page="currentPage"
                        v-model:page-size="pageSize"
                        :page-sizes="[10, 20, 50]"
                        layout="total, sizes, prev, pager, next, jumper"
                        :total="total"
                        @size-change="handleSizeChange"
                        @current-change="handleCurrentChange"
                    />
                </div>
            </div>
        </div>
    </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { getAlarmInformation, getAlarmExportUrl } from '@/api/api'
import { formatTime, dayjs } from '@/utils/time'

const router = useRouter()
const route = useRoute()

const shortcuts = [
    { text: '最近1小时',  value: () => [dayjs().subtract(1, 'hour').format('YYYY-MM-DD HH:mm:ss'), dayjs().format('YYYY-MM-DD HH:mm:ss')] },
    { text: '最近24小时', value: () => [dayjs().subtract(24, 'hour').format('YYYY-MM-DD HH:mm:ss'), dayjs().format('YYYY-MM-DD HH:mm:ss')] },
    { text: '最近7天',    value: () => [dayjs().subtract(7, 'day').format('YYYY-MM-DD HH:mm:ss'), dayjs().format('YYYY-MM-DD HH:mm:ss')] },
]

const STORAGE_KEY = 'macda_history_alarm_filter'

// 初始化：优先从缓存恢复，否则使用稳定默认值
const getInitialFilter = () => {
    let cached = null
    const saved = sessionStorage.getItem(STORAGE_KEY)
    if (saved) {
        try { cached = JSON.parse(saved) } catch(e) {}
    }
    
    // 强制优先使用 URL query 传过来的参数（例如从列车详情跳转过来）
    const qTrain = route.query.trainNo
    const qCoach = route.query.trainCoach

    return {
        trainNo:    String(qTrain || cached?.trainNo || '7001'),
        carriageNos: Array.isArray(qCoach) ? qCoach : (qCoach ? [String(qCoach)] : (cached?.carriageNos || [])),
        unitName:   '',
        timeRange: [
            dayjs().subtract(7, 'day').startOf('day').format('YYYY-MM-DD HH:mm:ss'),
            dayjs().endOf('day').format('YYYY-MM-DD HH:mm:ss')
        ]
    }
}

const filterForm = reactive(getInitialFilter())

// 实时持久化：防止重挂载导致状态丢失
watch(filterForm, (newVal) => {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(newVal))
}, { deep: true })

// 路由参数变化时（从其他页面跳转过来），刷新筛选条件并重新查询
watch(
    () => [route.query.trainNo, route.query.trainCoach],
    (newVals, oldVals) => {
        if (!oldVals) return
        if (newVals[0] !== oldVals[0] || newVals[1] !== oldVals[1]) {
            if (newVals[0]) filterForm.trainNo = String(newVals[0])
            if (newVals[1]) {
                filterForm.carriageNos = Array.isArray(newVals[1]) ? newVals[1] : [String(newVals[1])]
            }
            handleQuery()
        }
    }
)

const formatUnit = (faultCode) => {
    if (!faultCode) return '-'
    const c = faultCode.toLowerCase()
    if (c.includes('u1')) return '机组一'
    if (c.includes('u2')) return '机组二'
    return '-'
}

const getCarriageName = (no) => {
    const map = {
        '1': 'TC1',
        '2': 'MP1',
        '3': 'M1',
        '4': 'M2',
        '5': 'MP2',
        '6': 'TC2'
    }
    return map[String(no)] || `${no}车厢`
}

const CARRIAGE_MAP = { '1':'TC1','2':'MP1','3':'M1','4':'M2','5':'MP2','6':'TC2' }
const trainList    = ref(Array.from({ length: 37 }, (_, i) => ({ train_id: (6001 + i).toString(), label: `0${6001 + i}` })))
const carriageList = ref(Array.from({ length: 6  }, (_, i) => ({ carriage_no: (i + 1).toString(), label: CARRIAGE_MAP[String(i+1)] })))

const loading     = ref(false)
const tableData   = ref([])
const total       = ref(0)
const currentPage = ref(1)
const pageSize    = ref(10)

// 请求序号：用于丢弃过期的并发响应，防止"后发先至"覆盖最新结果
let latestReqId = 0

const fetchData = async (p, l) => {
    const reqId = ++latestReqId
    loading.value = true

    // 优先使用传入参数，否则兜底使用 ref 值
    const page      = p || currentPage.value
    const limit     = l || pageSize.value
    
    // 直接读取 DatePicker 当前值，不做任何修改
    const startTime = filterForm.timeRange?.[0] || ''
    const endTime   = filterForm.timeRange?.[1] || ''
    // 多选车厢：如果数组为空，则不传该参数（查询全部车厢）
    console.log(`[HistoryAlarm] req#${reqId} start`, { trainNo: filterForm.trainNo, carriageNos: filterForm.carriageNos, startTime, endTime, page, limit })

    try {
        const res = await getAlarmInformation({
            trainNo: filterForm.trainNo,
            carriageNos: filterForm.carriageNos && filterForm.carriageNos.length > 0 ? filterForm.carriageNos : undefined,
            unitNames: filterForm.unitName ? [filterForm.unitName] : [],
            startTime, endTime, page, limit
        })
        console.log(`[HistoryAlarm] req#${reqId} API Raw Response:`, res)

        // 竞态处理：如果已有更新的请求，丢弃本次响应
        if (reqId !== latestReqId) {
            console.log(`[HistoryAlarm] req#${reqId} discarded (stale)`)
            return
        }

        if (res?.code === 200 && res.data) {
            const remoteList = res.data.list || []
            const remoteTotal = Number(res.data.total) || 0
            
            // 重要：在这里强制打印一次，确认 total 是否为 0
            if (remoteTotal === 0) {
                console.warn(`[HistoryAlarm] req#${reqId} WARNING: total is 0. params were:`, { trainNo: filterForm.trainNo, carriageNo: filterForm.carriageNo, startTime, endTime, page, limit })
            } else {
                console.log(`[HistoryAlarm] req#${reqId} success: total=${remoteTotal}, list=${remoteList.length}`)
            }
            
            tableData.value = remoteList
            total.value     = remoteTotal
        } else {
            console.error(`[HistoryAlarm] req#${reqId} error: API responded with code ${res?.code}`, res)
            tableData.value = []
            total.value     = 0
        }
    } catch (err) {
        console.error(`[HistoryAlarm] req#${reqId} failed:`, err)
        if (reqId === latestReqId) {
            tableData.value = []
            total.value     = 0
        }
    } finally {
        // 只有最新的请求才能关闭 loading 状态
        if (reqId === latestReqId) {
            loading.value = false
        }
    }
}

// 点「查询」：重置第1页，直接用 DatePicker 当前值查询
const handleQuery = () => {
    currentPage.value = 1
    fetchData()
}

// 改变每页条数：重置页码后请求
const handleSizeChange = (val) => {
    console.log(`[HistoryAlarm] handleSizeChange: ${val}`)
    pageSize.value = val
    currentPage.value = 1
    fetchData(1, val)
}

// 切换页码：直接请求
const handleCurrentChange = (val) => {
    console.log(`[HistoryAlarm] handleCurrentChange: ${val}`)
    currentPage.value = val
    fetchData(val, pageSize.value)
}

const handleExport = () => {
    const params = {
        trainNo: filterForm.trainNo,
        carriageNos: filterForm.carriageNos && filterForm.carriageNos.length > 0 ? filterForm.carriageNos.join(',') : '',
        unitNames: filterForm.unitName || '',
        startTime: filterForm.timeRange?.[0] || '',
        endTime:   filterForm.timeRange?.[1] || '',
    }
    const url = getAlarmExportUrl(params)
    window.open(url, '_blank')
}

const goBack = () => router.push({
    name: 'trainInfo',
    query: { trainNo: filterForm.trainNo, trainCoach: filterForm.carriageNos?.[0] || '1' }
})

onMounted(() => handleQuery())
</script>

<style scoped lang="scss">
.warp2 {
    background: #0a0f1d;
    min-height: 100vh;
    padding: 0;
    color: #fff;
}
.header-monitor {
    height: 50px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0 20px;
    background: linear-gradient(180deg, #181f30 0%, #0a0f1d 100%);
    position: sticky;
    top: 0;
    z-index: 1000;
    border-bottom: 1px solid rgba(33, 134, 207, 0.2);
    .header-left { display: flex; align-items: center; gap: 15px; }
    .header-right {
        .filter-form {
            display: flex;
            align-items: center;
            :deep(.el-form-item) { margin-bottom: 0 !important; margin-right: 12px !important; display: flex; align-items: center; }
            :deep(.el-form-item__label) { color: #2186cf; font-weight: bold; padding-right: 6px; white-space: nowrap; font-size: 13px; }
        }
    }
}
.monitor-container { padding: 15px; display: flex; flex-direction: column; gap: 12px; }
.section-box { background: #141a2e; border-radius: 8px; border: 1px solid #262e45; padding: 15px; box-shadow: 0 6px 15px rgba(0,0,0,0.3); }
.section-header { display: flex; align-items: center; gap: 8px; margin-bottom: 15px; padding: 0;
    .title-icon { width: 4px; height: 16px; background: #2186cf; border-radius: 2px; }
    .title-text { color: #ffffff; font-size: 15px; font-weight: bold; }
}
.nav-btn {
    background: #0a0f1d !important; border: 1px solid #2186cf !important; color: #ffffff !important;
    font-size: 13px !important; font-weight: 500 !important; height: 32px !important;
    padding: 0 15px !important; border-radius: 4px !important;
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1) !important;
    display: flex; align-items: center; justify-content: center;
    &:hover, &:focus { background: rgba(33,134,207,0.2) !important; border-color: #409eff !important; color: #ffffff !important; box-shadow: 0 0 10px rgba(33,134,207,0.3) !important; }
    :deep(.el-icon) { font-size: 14px !important; margin-right: 5px !important; color: #2186cf !important; }
}
:deep(.el-table) {
    background: transparent !important; color: #d1d9e7;
    --el-table-border-color: #262e45; --el-table-header-bg-color: #1a2234;
    --el-table-tr-bg-color: transparent; --el-table-row-hover-bg-color: rgba(33,134,207,0.1);
    th.el-table__cell { background: #1a2234 !important; color: #2186cf; font-weight: bold; border-bottom: 2px solid #2186cf; }
    td.el-table__cell { border-bottom: 1px solid #262e45; }
    .el-table__inner-wrapper::before { display: none; }
}
.pagination-container { margin-top: 20px; display: flex; justify-content: flex-end; }
.train-select, .carriage-select { width: 110px !important; }

.severity-text {
    font-weight: bold;
    color: #ffffff;
}
</style>

<style lang="scss">
.filter-form {
    --el-fill-color-blank: #0a0f1d !important; --el-border-color: #2186cf !important;
    --el-text-color-regular: #ffffff !important; --el-border-color-hover: #409eff !important;
    .el-input__wrapper {
        background-color: #0a0f1d !important; box-shadow: 0 0 0 1px #2186cf inset !important;
        border-radius: 4px !important; height: 32px !important;
        &:hover { box-shadow: 0 0 0 1px #409eff inset, 0 0 10px rgba(33,134,207,0.4) !important; }
        &.is-focus { box-shadow: 0 0 0 1px #409eff inset, 0 0 15px rgba(33,134,207,0.6) !important; }
        .el-input__inner { color: #ffffff !important; font-size: 13px !important; }
    }
    .el-range-editor.el-input__wrapper { width: 350px !important; }
    .el-range-input { color: #fff !important; }
    .el-range-separator { color: #2186cf !important; }
}
.el-select__popper.el-popper, .el-picker__popper.el-popper {
    background-color: #141a2e !important; border: 1px solid #2186cf !important;
    box-shadow: 0 4px 12px rgba(0,0,0,0.5) !important; color: #fff !important;
    .el-select-dropdown__item {
        color: #d1d9e7 !important;
        &:hover, &.hover { background-color: rgba(33,134,207,0.2) !important; color: #ffffff !important; }
        &.selected { color: #2186cf !important; font-weight: bold; background-color: rgba(33,134,207,0.1) !important; }
    }
    .el-picker-panel { background-color: #141a2e !important; color: #fff !important; border: none !important; }
    .el-date-range-picker__time-header, .el-date-range-picker__header {
        border-bottom: 1px solid #262e45 !important;
        .el-input__wrapper { background-color: #0a0f1d !important; box-shadow: 0 0 0 1px #2186cf inset !important; .el-input__inner { color: #fff !important; } }
    }
    .el-date-range-picker__content.is-left { border-right: 1px solid #262e45 !important; }
    .el-date-table th { color: #2186cf !important; border-bottom: 1px solid #262e45 !important; }
    .el-date-table td.next-month, .el-date-table td.prev-month { color: #4e5969 !important; }
    .el-date-table td.available:hover { color: #2186cf !important; }
    .el-date-table td.in-range .el-date-table-cell { background-color: rgba(33,134,207,0.1) !important; }
    .el-time-panel { background-color: #141a2e !important; border: 1px solid #2186cf !important; }
    .el-time-spinner__item { color: #d1d9e7 !important; &:hover { background: rgba(33,134,207,0.2) !important; } &.is-active { color: #2186cf !important; font-weight: bold; } }
    .el-picker-panel__footer {
        background-color: #141a2e !important; border-top: 1px solid #262e45 !important; padding: 10px !important;
        .el-button {
            background: #0a0f1d !important; border: 1px solid #2186cf !important; color: #ffffff !important;
            height: 28px !important; padding: 0 12px !important; font-size: 12px !important;
            &.is-text { border: 1px solid #2186cf !important; }
            &--primary { background: #2186cf !important; border-color: #2186cf !important; &:hover { background: #409eff !important; border-color: #409eff !important; } }
            &:hover { background: rgba(33,134,207,0.2) !important; border-color: #409eff !important; color: #ffffff !important; }
        }
    }
}
.el-pagination {
    --el-pagination-bg-color: transparent !important; --el-pagination-button-bg-color: transparent !important;
    --el-pagination-button-color: #ffffff !important; --el-pagination-button-disabled-bg-color: transparent !important;
    .el-select, .el-input {
        --el-fill-color-blank: #0a0f1d !important; --el-border-color: #2186cf !important; --el-text-color-regular: #ffffff !important;
        .el-input__wrapper {
            background-color: #0a0f1d !important; box-shadow: 0 0 0 1px #2186cf inset !important; background: #0a0f1d !important; border: none !important;
            &:hover { box-shadow: 0 0 0 1px #409eff inset !important; }
            &.is-focus { box-shadow: 0 0 0 1px #409eff inset !important; }
        }
        .el-input__inner { color: #ffffff !important; background-color: transparent !important; border: none !important; box-shadow: none !important; text-align: center; }
    }
    .el-pagination__total, .el-pagination__jump { color: #d1d9e7 !important; }
    .el-pager li { background: transparent !important; color: #d1d9e7 !important; &.is-active { color: #2186cf !important; font-weight: bold; } }
}
/* 多选下拉 tag 统一暗色风格 */
.el-select__selection .el-tag {
    background-color: rgba(33,134,207,0.2) !important;
    border-color: #2186cf !important;
    color: #ffffff !important;
    .el-tag__close { color: #ffffff !important; }
}
</style>
