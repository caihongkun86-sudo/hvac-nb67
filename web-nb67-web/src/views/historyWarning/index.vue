<template>
    <div class="warp2">
        <div class="header-monitor">
            <div class="header-left">
                <el-button type="primary" class="nav-btn" icon="ArrowLeft" @click="goBack">返回</el-button>
                <el-button type="primary" class="nav-btn" icon="Setting" @click="gotoWarningConfig">预警条件设置</el-button>
            </div>
            <div class="header-right">
                <el-form :inline="true" :model="filterForm" class="filter-form">
                    <el-form-item label="车号">
                        <el-select v-model="filterForm.trainNo" placeholder="请选择车号" class="train-select">
                            <el-option v-for="item in trainList" :key="item.train_id" :label="item.label" :value="item.train_id" />
                        </el-select>
                    </el-form-item>
                    <el-form-item label="车厢">
                        <el-select v-model="filterForm.carriageNos" placeholder="全部" multiple collapse-tags clearable style="width:200px">
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
                        <el-date-picker v-model="filterForm.timeRange" type="datetimerange"
                            range-separator="至" start-placeholder="开始时间" end-placeholder="结束时间"
                            format="YYYY-MM-DD HH:mm:ss" value-format="YYYY-MM-DD HH:mm:ss"
                            :shortcuts="shortcuts" />
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
                    <span class="title-text">历史预警</span>
                </div>
                <el-table :data="tableData" border stripe style="width:100%" v-loading="loading">
                    <el-table-column prop="train_id" label="列车号" align="center" width="100">
                        <template #default="s">{{ s.row.train_id ? `0${s.row.train_id}` : '-' }}</template>
                    </el-table-column>
                    <el-table-column prop="carriage_no" label="车厢" align="center" width="90" />
                    <el-table-column prop="unit_name"   label="机组"   align="center" width="90" />
                    <el-table-column prop="severity"    label="严重级别" align="center" width="100">
                        <template #default="s">
                            <span :class="['severity-text', s.row.severity==='严重'?'danger':s.row.severity==='轻微'?'info':'warning']">
                                {{ s.row.severity }}
                            </span>
                        </template>
                    </el-table-column>
                    <el-table-column prop="warn_name"        label="预警名称"   min-width="200" show-overflow-tooltip />
                    <el-table-column prop="trigger_condition" label="触发条件"  min-width="200" show-overflow-tooltip />
                    <el-table-column prop="start_time"       label="开始时间"  width="175" show-overflow-tooltip />
                    <el-table-column prop="end_time"         label="结束时间"  width="175" show-overflow-tooltip>
                        <template #default="s">{{ s.row.end_time || '-' }}</template>
                    </el-table-column>
                    <el-table-column label="详情" align="center" width="80" fixed="right">
                        <template #default="s">
                            <el-link type="primary" @click="showDetail(s.row)">详情</el-link>
                        </template>
                    </el-table-column>
                </el-table>
                <div class="pagination-container">
                    <el-pagination
                        v-model:current-page="currentPage"
                        v-model:page-size="pageSize"
                        :page-sizes="[10, 20, 50]"
                        layout="total, sizes, prev, pager, next, jumper"
                        :total="total"
                        @size-change="(v)=>{ pageSize=v; currentPage=1; fetchData() }"
                        @current-change="fetchData"
                    />
                </div>
            </div>
        </div>

        <!-- F14: 预警详情弹窗 — 触发前30分钟参数曲线 -->
        <el-dialog v-model="detailVisible" title="预警详情" width="80%" top="5vh" :append-to-body="true" class="dark-dialog">
            <template #header>
                <div class="dialog-header">
                    <span class="dialog-title">预警数据趋势图</span>
                    <span class="dialog-sub" v-if="currentRow">
                        {{ currentRow.warn_name }} | {{ currentRow.start_time }}
                    </span>
                </div>
            </template>
            <!-- 完整预警描述 -->
            <div v-if="currentRow && currentRow.fault_desc" class="detail-desc-box">
                <span class="detail-desc-label">完整预警描述：</span>
                <span class="detail-desc-text">{{ currentRow.fault_desc }}</span>
            </div>
            <div v-loading="detailLoading" style="height:360px;position:relative">
                <div ref="detailChartRef" style="width:100%;height:100%"></div>
                <div v-if="!detailHasData" style="position:absolute;top:0;left:0;width:100%;height:100%;display:flex;flex-direction:column;align-items:center;justify-content:center;color:#676e82">
                    <img src="/img/no-data.svg" width="50" /><p style="margin-top:10px">该时间段（预警前后各1小时）暂无数据</p>
                </div>
            </div>
        </el-dialog>
    </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { getHistoryWarning, getPredictDetail, getWarningExportUrl } from '@/api/api'
import * as echarts from 'echarts'
import { dayjs } from '@/utils/time'

const router = useRouter()
const route = useRoute()

const CARRIAGE_MAP = { '1':'TC1','2':'MP1','3':'M1','4':'M2','5':'MP2','6':'TC2' }

const shortcuts = [
    { text: '最近1小时',  value: () => [dayjs().subtract(1,'hour').format('YYYY-MM-DD HH:mm:ss'), dayjs().format('YYYY-MM-DD HH:mm:ss')] },
    { text: '最近24小时', value: () => [dayjs().subtract(24,'hour').format('YYYY-MM-DD HH:mm:ss'), dayjs().format('YYYY-MM-DD HH:mm:ss')] },
    { text: '最近7天',    value: () => [dayjs().subtract(7,'day').format('YYYY-MM-DD HH:mm:ss'), dayjs().format('YYYY-MM-DD HH:mm:ss')] },
]

const filterForm = reactive({
    trainNo:    String(route.query.trainNo || '7001'),
    carriageNos: route.query.trainCoach ? [String(route.query.trainCoach)] : [],
    unitName:   '',
    timeRange: [
        dayjs().subtract(7,'day').startOf('day').format('YYYY-MM-DD HH:mm:ss'),
        dayjs().endOf('day').format('YYYY-MM-DD HH:mm:ss')
    ]
})

const trainList    = ref(Array.from({ length: 37 }, (_, i) => ({ train_id:(6001+i).toString(), label:`0${6001+i}` })))
const carriageList = ref(Array.from({ length: 6  }, (_, i) => ({ carriage_no:(i+1).toString(), label: CARRIAGE_MAP[String(i+1)] })))

const loading     = ref(false)
const tableData   = ref([])
const total       = ref(0)
const currentPage = ref(1)
const pageSize    = ref(10)

const handleQuery = () => { currentPage.value = 1; fetchData() }

const fetchData = async () => {
    loading.value = true
    try {
        const carriageNos = filterForm.carriageNos || []
        const res = await getHistoryWarning({
            trainNo: filterForm.trainNo,
            carriageNos: carriageNos.length > 0 ? carriageNos : undefined,
            unitNames: filterForm.unitName ? [filterForm.unitName] : [],
            startTime: filterForm.timeRange?.[0] || '',
            endTime:   filterForm.timeRange?.[1] || '',
            page: currentPage.value,
            limit: pageSize.value
        })
        if (res?.code === 200) {
            tableData.value = res.data.list || []
            total.value = res.data.total || 0
        } else {
            tableData.value = []; total.value = 0
        }
    } catch (e) {
        tableData.value = []; total.value = 0
    } finally {
        loading.value = false
    }
}

const handleExport = () => {
    const carriageNos = filterForm.carriageNos || []
    const url = getWarningExportUrl({
        trainNo: filterForm.trainNo,
        carriageNos: carriageNos.join(',') || '',
        unitNames: filterForm.unitName || '',
        startTime: filterForm.timeRange?.[0] || '',
        endTime:   filterForm.timeRange?.[1] || '',
    })
    window.open(url, '_blank')
}

// F14: 详情弹窗
const detailVisible = ref(false)
const detailLoading = ref(false)
const detailHasData = ref(false)
const detailChartRef = ref(null)
const currentRow = ref(null)
let detailChart = null

const COLORS = ['#2186cf','#42ad5d','#ffa55c','#e65355','#9b59b6','#1abc9c']

const showDetail = async (row) => {
    currentRow.value = row
    detailVisible.value = true
    detailLoading.value = true
    detailHasData.value = false
    try {
        const res = await getPredictDetail({
            trainId: row.train_id,
            carriageId: Object.keys(CARRIAGE_MAP).find(k => CARRIAGE_MAP[k] === row.carriage_no) || 1,
            triggerTime: row.start_time,
            warnCode: row.fault_code || ''
        })
        if (res?.code === 200 && res.data?.data?.length > 0) {
            detailHasData.value = true
            const { params, data, triggerTime } = res.data
            const timeAxis = data.map(d => {
                const t = new Date(d.bucket)
                return `${String(t.getHours()).padStart(2,'0')}:${String(t.getMinutes()).padStart(2,'0')}:${String(t.getSeconds()).padStart(2,'0')}`
            })
            // 找最接近预警时刻的 x 轴索引，用于标注竖线
            const triggerLabel = (() => {
                const t = new Date(triggerTime || row.start_time)
                return `${String(t.getHours()).padStart(2,'0')}:${String(t.getMinutes()).padStart(2,'0')}:${String(t.getSeconds()).padStart(2,'0')}`
            })()
            let triggerIdx = timeAxis.findIndex(l => l >= triggerLabel)
            if (triggerIdx === -1) triggerIdx = timeAxis.length - 1
            const series = params.map((p, i) => ({
                name: p.label,
                type: 'line',
                smooth: true,
                showSymbol: false,
                data: data.map(d => d[p.key] ?? null),
                lineStyle: { color: COLORS[i % COLORS.length], width: 2 },
                ...(i === 0 ? {
                    markLine: {
                        silent: true,
                        data: [{ xAxis: triggerIdx }],
                        lineStyle: { color: '#e65355', type: 'dashed', width: 2 },
                        label: { show: true, formatter: '预警时刻', color: '#e65355', position: 'insideEndTop', fontSize: 11 },
                        symbol: ['none', 'none']
                    }
                } : {})
            }))
            nextTick(() => {
                if (!detailChart && detailChartRef.value) detailChart = echarts.init(detailChartRef.value)
                if (detailChart) {
                    detailChart.setOption({
                        backgroundColor: 'transparent',
                        tooltip: { trigger:'axis', backgroundColor:'rgba(10,15,29,0.9)', borderColor:'#2186cf', textStyle:{color:'#fff'} },
                        legend: { data: params.map(p=>p.label), top:10, textStyle:{color:'#676e82',fontSize:11} },
                        grid: { left:'3%', right:'4%', bottom:'3%', containLabel:true },
                        xAxis: { type:'category', data:timeAxis, axisLabel:{color:'#676e82',fontSize:10} },
                        yAxis: { type:'value', axisLabel:{color:'#676e82',fontSize:10}, splitLine:{lineStyle:{color:'#1a2234',type:'dashed'}} },
                        series
                    }, true)
                }
            })
        }
    } catch (e) {
        console.error(e)
    } finally {
        detailLoading.value = false
    }
}

// dialog关闭时释放ECharts实例，防止内存泄漏
watch(detailVisible, (v) => {
    if (!v && detailChart) { detailChart.dispose(); detailChart = null }
})

onUnmounted(() => {
    if (detailChart) { detailChart.dispose(); detailChart = null }
})

const goBack = () => router.push({ name:'trainInfo', query:{ trainNo: filterForm.trainNo, trainCoach: filterForm.carriageNos?.[0] || '1' } })

const gotoWarningConfig = () => router.push({
    name: 'warningConfig',
    query: { trainNo: filterForm.trainNo, trainCoach: filterForm.carriageNos?.[0] || '1' }
})
onMounted(() => fetchData())
</script>

<style scoped lang="scss">
.warp2 { background:#0a0f1d; min-height:100vh; padding:0; color:#fff; }
.header-monitor {
    height:50px; display:flex; justify-content:space-between; align-items:center;
    padding:0 20px;
    background: linear-gradient(180deg, #181f30 0%, #0a0f1d 100%);
    position:sticky; top:0; z-index:1000;
    border-bottom:1px solid rgba(33,134,207,0.2);
    .header-left { display:flex; align-items:center; gap:8px; }
    .header-right .filter-form {
        display:flex; align-items:center;
        :deep(.el-form-item) { margin-bottom:0!important; margin-right:10px!important; }
        :deep(.el-form-item__label) { color:#2186cf; font-weight:bold; font-size:13px; }
    }
}
.monitor-container { padding:15px; }
.section-box { background:#141a2e; border-radius:8px; border:1px solid #262e45; padding:15px; }
.section-header { display:flex; align-items:center; gap:8px; margin-bottom:15px;
    .title-icon { width:4px; height:16px; background:#2186cf; border-radius:2px; }
    .title-text { color:#fff; font-size:15px; font-weight:bold; }
}
.pagination-container { margin-top:15px; display:flex; justify-content:flex-end; }
.nav-btn {
    background:#0a0f1d!important; border:1px solid #2186cf!important; color:#fff!important;
    font-size:13px!important; height:32px!important; padding:0 12px!important; border-radius:4px!important;
    &:hover { background:rgba(33,134,207,0.2)!important; }
}
.severity-text { font-weight:bold; color:#ffffff; }
.dialog-header { display:flex; flex-direction:column; gap:4px;
    .dialog-title { font-size:15px; font-weight:bold; color:#fff; }
    .dialog-sub   { font-size:12px; color:#676e82; }
}
.detail-desc-box {
    padding: 8px 12px; margin-bottom: 10px;
    background: rgba(33,134,207,0.08); border-left: 3px solid #2186cf; border-radius: 4px;
    .detail-desc-label { color: #2186cf; font-size: 13px; font-weight: bold; }
    .detail-desc-text  { color: #d1d9e7; font-size: 13px; }
}
:deep(.el-table) {
    background:transparent!important; color:#d1d9e7;
    --el-table-border-color:#262e45; --el-table-header-bg-color:#1a2234;
    --el-table-tr-bg-color:transparent; --el-table-row-hover-bg-color:rgba(33,134,207,0.1);
    th.el-table__cell { background:#1a2234!important; color:#2186cf; font-weight:bold; }
}
.train-select { width:110px!important; }
.el-select__selection .el-tag {
    background-color: rgba(33,134,207,0.2) !important;
    border-color: #2186cf !important;
    color: #ffffff !important;
    .el-tag__close { color: #ffffff !important; }
}
</style>

<style lang="scss">
/* 全局暗色输入框样式，与 historyAlarm 保持一致，避免硬刷新时变白 */
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
            &--primary { background: #2186cf !important; border-color: #2186cf !important; }
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
            background-color: #0a0f1d !important; box-shadow: 0 0 0 1px #2186cf inset !important; border: none !important;
            &:hover, &.is-focus { box-shadow: 0 0 0 1px #409eff inset !important; }
        }
        .el-input__inner { color: #ffffff !important; background-color: transparent !important; border: none !important; box-shadow: none !important; text-align: center; }
    }
    .el-pagination__total, .el-pagination__jump { color: #d1d9e7 !important; }
    .el-pager li { background: transparent !important; color: #d1d9e7 !important; &.is-active { color: #2186cf !important; font-weight: bold; } }
}

/* 预警详情弹窗暗色（teleport 到 body，必须全局） */
.dark-dialog.el-dialog {
    background: #0e1628 !important;
    border: 1px solid #2186cf !important;
    border-radius: 8px;
    .el-dialog__header {
        padding: 12px 20px; border-bottom: 1px solid rgba(33,134,207,0.25);
        background: #141b2e; border-radius: 8px 8px 0 0;
        .el-dialog__title { color: #fff; font-size: 14px; font-weight: bold; }
        .el-dialog__headerbtn .el-dialog__close { color: #2186cf; &:hover { color: #fff; } }
    }
    .el-dialog__body { padding: 20px; background: #0e1628; }
}
</style>
