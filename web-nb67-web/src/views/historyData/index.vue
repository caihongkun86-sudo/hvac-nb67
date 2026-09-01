<template>
    <div class="warp2">
        <!-- 顶部筛选栏 -->
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
                    <el-form-item label="车厢号">
                        <el-select v-model="filterForm.carriageNos" placeholder="请选择车厢号" multiple collapse-tags clearable class="carriage-select" style="width:200px">
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
                            format="YYYY-MM-DD HH:mm"
                            value-format="YYYY-MM-DD HH:mm:ss"
                            :disabled-date="disabledDate"
                            @calendar-change="onCalendarChange"
                            :shortcuts="shortcuts"
                            :default-time="[new Date(2000, 1, 1, 0, 0, 0), new Date(2000, 1, 1, 23, 59, 59)]"
                            teleported
                            popper-class="history-data-picker"
                        />
                    </el-form-item>
                    <el-form-item>
                        <el-button type="primary" class="nav-btn" icon="Search" @click="handleQuery">查询</el-button>
                    </el-form-item>
                </el-form>
            </div>
        </div>

        <div class="monitor-container">
            <div class="section-box">
                <div class="section-header">
                    <span class="title-icon"></span>
                    <span class="title-text">历史数据</span>
                    <span class="record-count" v-if="detailTotal > 0">共 {{ detailTotal }} 条</span>
                    <div style="flex:1"></div>
                    <el-button type="primary" size="small" icon="Download" :loading="downloading" @click="handleDownload">下载 ZIP</el-button>
                </div>

                <el-table
                    :data="detailList"
                    border stripe
                    style="width:100%"
                    height="65vh"
                    v-loading="loading"
                    element-loading-background="rgba(14,22,40,0.92)"
                    :scrollbar-always-on="true"
                    class="no-ghost-table"
                >
                    <el-table-column type="index"           label="序号"              width="60"  fixed="left" align="center" />
                    <el-table-column prop="train_id"        label="列车号"             width="90"  fixed="left" show-overflow-tooltip>
                        <template #default="s">{{ s.row.train_id ? `0${s.row.train_id}` : '-' }}</template>
                    </el-table-column>
                    <el-table-column prop="carriage_id"     label="车厢"               width="80"  fixed="left" show-overflow-tooltip>
                        <template #default="s">{{ CARRIAGE_MAP[String(s.row.carriage_id)] || s.row.carriage_id }}</template>
                    </el-table-column>
                    <el-table-column prop="unit_name"       label="机组"               width="90"  fixed="left" show-overflow-tooltip />
                    <el-table-column prop="i_inner_temp"    label="车厢温度(℃)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="i_back_temp"     label="回风温度(℃)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="i_outer_temp"    label="新风温度(℃)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="i_set_temp"      label="设定温度(℃)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="i_hum"           label="湿度(%)"            width="90"  show-overflow-tooltip />
                    <el-table-column prop="work_status"     label="工作状态"            width="100" show-overflow-tooltip >
                        <template #default="s">{{ getModelName(s.row.work_status) }}</template>
                    </el-table-column>
                    <el-table-column prop="dwef_op_tm"      label="通风机运行时间(秒)"  width="155" show-overflow-tooltip />
                    <el-table-column prop="w_crnt_cf1"      label="冷凝风机电流1(A)"    width="140" show-overflow-tooltip />
                    <el-table-column prop="w_crnt_cf2"      label="冷凝风机电流2(A)"    width="140" show-overflow-tooltip />
                    <el-table-column prop="w_crnt_cp1"      label="压缩机电流1(A)"      width="130" show-overflow-tooltip />
                    <el-table-column prop="w_crnt_cp2"      label="压缩机电流2(A)"      width="130" show-overflow-tooltip />
                    <el-table-column prop="w_freq_cp1"      label="压缩机频率1(Hz)"     width="130" show-overflow-tooltip />
                    <el-table-column prop="w_freq_cp2"      label="压缩机频率2(Hz)"     width="130" show-overflow-tooltip />
                    <el-table-column prop="w_crnt_ef1"      label="送风机电流1(A)"      width="130" show-overflow-tooltip />
                    <el-table-column prop="w_crnt_ef2"      label="送风机电流2(A)"      width="130" show-overflow-tooltip />
                    <el-table-column prop="i_high_pres1"    label="高压1(bar)"          width="110" show-overflow-tooltip />
                    <el-table-column prop="i_low_pres1"     label="低压1(bar)"          width="110" show-overflow-tooltip />
                    <el-table-column prop="i_high_pres2"    label="高压2(bar)"          width="110" show-overflow-tooltip />
                    <el-table-column prop="i_low_pres2"     label="低压2(bar)"          width="110" show-overflow-tooltip />
                    <el-table-column prop="i_pres_diff"     label="压力差(bar)"         width="110" show-overflow-tooltip />
                    <el-table-column prop="i_suckt1"        label="吸风温度1(℃)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="i_suckt2"        label="吸风温度2(℃)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="i_sat1"          label="送风温度1(℃)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="i_sat2"          label="送风温度2(℃)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="dwcf_op_tm1"     label="冷凝运行时间(秒)"   width="150" show-overflow-tooltip />
                    <el-table-column prop="dwcp_op_tm1"     label="压缩机时间1(秒)"     width="135" show-overflow-tooltip />
                    <el-table-column prop="dwcp_op_tm2"     label="压缩机时间2(秒)"     width="135" show-overflow-tooltip />
                    <el-table-column prop="dwexufan_op_tm"  label="废排风机时间(秒)"    width="135" show-overflow-tooltip />
                    <el-table-column prop="i_exufan"        label="废排风机电流(A)"    width="135" show-overflow-tooltip />
                    <el-table-column prop="dwfad_op_cnt"    width="140" show-overflow-tooltip>
                        <template #header>
                            <HeaderTip tip="时间开度值(s)=百分比开度值(%)*35(s)(整数位取整)">新风阀开度(%)</HeaderTip>
                        </template>
                    </el-table-column>
                    <el-table-column prop="dwrad_op_cnt"    width="140" show-overflow-tooltip>
                        <template #header>
                            <HeaderTip tip="时间开度值(s)=百分比开度值(%)*35(s)(整数位取整)">回风阀开度(%)</HeaderTip>
                        </template>
                    </el-table-column>
                    <el-table-column prop="dmp_exu_pos"     width="150" show-overflow-tooltip>
                        <template #header>
                            <HeaderTip tip="时间开度值(s)=百分比开度值(%)*35(s)(整数位取整)">废排风阀开度(%)</HeaderTip>
                        </template>
                    </el-table-column>
                    <el-table-column prop="i_sp_u1"     label="过热度1(K)"     width="110" show-overflow-tooltip />
                    <el-table-column prop="i_sp_u2"     label="过热度2(K)"     width="110" show-overflow-tooltip />
                    <el-table-column prop="i_co2"           label="CO₂(ppm)"           width="110" show-overflow-tooltip />
                    <el-table-column prop="i_pm25"          label="PM2.5(ug/m³)"        width="130" show-overflow-tooltip />
                    <el-table-column prop="i_pm10"          label="PM10(ug/m³)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="i_tvoc"          label="TVOC(ppb)"        width="120" show-overflow-tooltip />
                    <el-table-column prop="start_station"   label="起始站"              width="110" show-overflow-tooltip>
                        <template #default="s">{{ getStationName(s.row.line_id, s.row.start_station) }}</template>
                    </el-table-column>
                    <el-table-column prop="terminal_station" label="终点站"             width="110" show-overflow-tooltip>
                        <template #default="s">{{ getStationName(s.row.line_id, s.row.terminal_station) }}</template>
                    </el-table-column>
                    <el-table-column prop="cur_station"     label="当前站"              width="110" show-overflow-tooltip>
                        <template #default="s">{{ getStationName(s.row.line_id, s.row.cur_station) }}</template>
                    </el-table-column>
                    <el-table-column prop="next_station"    label="下一站"              width="110" show-overflow-tooltip>
                        <template #default="s">{{ getStationName(s.row.line_id, s.row.next_station) }}</template>
                    </el-table-column>
                    <el-table-column prop="event_time"      label="设备上报时间"        width="185" fixed="right" show-overflow-tooltip />
                </el-table>

                <div class="detail-pagination">
                    <el-pagination
                        v-model:current-page="detailPage"
                        v-model:page-size="detailPageSize"
                        :page-sizes="[20, 50, 100]"
                        layout="total, sizes, prev, pager, next, jumper"
                        :total="detailTotal"
                        @size-change="(v) => { detailPageSize = v; detailPage = 1; fetchDetailData() }"
                        @current-change="fetchDetailData"
                    />
                </div>
            </div>
        </div>
    </div>
</template>


<script setup>
import { ref, reactive, onMounted, watch, h } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { getTrainDataApi } from '@/api/api'
import dayjs from 'dayjs'
import { ElMessage, ElTooltip, ElIcon } from 'element-plus'
import { InfoFilled } from '@element-plus/icons-vue'
import JSZip from 'jszip'
import { saveAs } from 'file-saver'
import { getStationName } from '@/config/stationMap.js'
import { getModelName } from '@/config/modelMap.js'

// 表头提示组件：在表头文字后追加 i 图标，hover 显示说明
const HeaderTip = (props, { slots }) => h(
    ElTooltip,
    { content: props.tip, placement: 'top', effect: 'dark', 'show-after': 200 },
    () => h('span', { class: 'header-with-tip' }, [
        slots.default?.(),
        h(ElIcon, { class: 'tip-icon' }, () => h(InfoFilled))
    ])
)
HeaderTip.props = ['tip']

const router = useRouter()
const route = useRoute()

const CARRIAGE_MAP = { '1':'TC1','2':'MP1','3':'M1','4':'M2','5':'MP2','6':'TC2' }

const shortcuts = [
    { text: '最近1小时',  value: () => [dayjs().subtract(1,'hour').format('YYYY-MM-DD HH:mm:ss'), dayjs().format('YYYY-MM-DD HH:mm:ss')] },
    { text: '最近24小时', value: () => [dayjs().subtract(24,'hour').format('YYYY-MM-DD HH:mm:ss'), dayjs().format('YYYY-MM-DD HH:mm:ss')] },
    { text: '最近7天',    value: () => [dayjs().subtract(7,'day').format('YYYY-MM-DD HH:mm:ss'), dayjs().format('YYYY-MM-DD HH:mm:ss')] },
]

const filterForm = reactive({
    trainNo: String(parseInt(String(route.query.trainNo || '7001')) || 7001),
    carriageNos: route.query.trainCoach ? [String(route.query.trainCoach)] : ['1'],
    unitName: '',
    timeRange: [
        dayjs().subtract(24, 'hour').format('YYYY-MM-DD HH:mm:ss'),
        dayjs().format('YYYY-MM-DD HH:mm:ss')
    ]
})

watch(() => route.query, (newQuery) => {
    if (newQuery.trainNo) filterForm.trainNo = String(parseInt(String(newQuery.trainNo)) || 7001)
    if (newQuery.trainCoach) filterForm.carriageNos = [String(newQuery.trainCoach)]
    handleQuery()
}, { deep: true })

const trainList = ref(Array.from({ length: 37 }, (_, i) => ({
    train_id: (6001 + i).toString(), label: `0${6001 + i}`
})))
const carriageList = ref(Array.from({ length: 6 }, (_, i) => ({
    carriage_no: (i + 1).toString(),
    label: CARRIAGE_MAP[String(i + 1)] || `${i + 1}车厢`
})))

// 时间范围最多 7 天：记录用户点击的第一个日期，用于限制另一侧
const pickerFirstDate = ref(null)
const disabledDate = (time) => {
    if (!pickerFirstDate.value) return false
    const first = dayjs(pickerFirstDate.value)
    const min = first.subtract(6, 'day').startOf('day')
    const max = first.add(6, 'day').endOf('day')
    return time.getTime() < min.valueOf() || time.getTime() > max.valueOf()
}
const onCalendarChange = ([start]) => {
    pickerFirstDate.value = start || null
}

const loading = ref(false)
const downloading = ref(false)
const detailList = ref([])
const detailPage = ref(1)
const detailPageSize = ref(20)
const detailTotal = ref(0)

const handleQuery = () => {
    detailPage.value = 1
    fetchDetailData()
}

const fetchDetailData = async () => {
    if (!filterForm.timeRange || filterForm.timeRange.length < 2) return

    // 一周限制校验
    const diff = dayjs(filterForm.timeRange[1]).diff(dayjs(filterForm.timeRange[0]), 'day', true)
    if (diff > 7) {
        ElMessage.warning('时间范围最多 7 天，请重新选择')
        return
    }

    loading.value = true
    try {
        const carriageNos = filterForm.carriageNos || []
        
        // 如果没有选择车厢，查询所有车厢
        const targetCarriages = carriageNos.length > 0 ? carriageNos : ['1', '2', '3', '4', '5', '6']
        
        const res = await getTrainDataApi({
            state: `${filterForm.trainNo}01`, // 用于解析 trainId
            carriageIds: targetCarriages.join(','), // 传递多车厢参数（逗号分隔）
            startTime: filterForm.timeRange[0],
            endTime: filterForm.timeRange[1],
            unitName: filterForm.unitName || '',
            page: detailPage.value,
            limit: detailPageSize.value
        })
        
        if (res?.data) {
            detailList.value = res.data.list || []
            detailTotal.value = res.data.total || 0
        } else {
            detailList.value = []; detailTotal.value = 0
        }
    } catch (err) {
        console.error('获取明细数据失败:', err)
        detailList.value = []; detailTotal.value = 0
    } finally {
        loading.value = false
    }
}

const CSV_HEADERS = [
    '序号','列车号','车厢','机组',
    '车厢温度(℃)','回风温度(℃)','新风温度(℃)','设定温度(℃)','湿度(%)',
    '工作状态','通风机运行时间(秒)',
    '冷凝风机电流1(A)','冷凝风机电流2(A)',
    '压缩机电流1(A)','压缩机电流2(A)',
    '压缩机频率1(Hz)','压缩机频率2(Hz)',
    '送风机电流1(A)','送风机电流2(A)',
    '高压1(bar)','低压1(bar)','高压2(bar)','低压2(bar)','压力差(bar)',
    '吸风温度1(℃)','吸风温度2(℃)','送风温度1(℃)','送风温度2(℃)',
    '冷凝运行时间(秒)',
    '压缩机时间1(秒)','压缩机时间2(秒)',
    '废排风机时间(秒)','废排风机电流(A)','新风阀开度(%)','回风阀开度(%)',
    '废排风阀开度(%)','过热度1(K)','过热度2(K)',
    'CO₂(ppm)','PM2.5(ug/m³)','PM10(ug/m³)','TVOC(ppb)',
    '起始站','终点站','当前站','下一站',
    '设备上报时间'
]

const rowToCsv = (row, idx) => {
    const fields = [
        idx+1, row.train_id ? `0${row.train_id}` : '',
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
    ]
    return fields.map(v => (v == null ? '' : `"${String(v).replace(/"/g, '""')}"`)).join(',')
}

const handleDownload = async () => {
    if (!filterForm.timeRange || filterForm.timeRange.length < 2) return

    const diff = dayjs(filterForm.timeRange[1]).diff(dayjs(filterForm.timeRange[0]), 'day', true)
    if (diff > 7) {
        ElMessage.warning('时间范围最多 7 天，请重新选择')
        return
    }

    downloading.value = true
    try {
        const carriageNos = filterForm.carriageNos || []
        const targetCarriages = carriageNos.length > 0 ? carriageNos : ['1', '2', '3', '4', '5', '6']
        
        let page = 1; let allRows = []
        let serverTotal = 0
        
        while (true) {
            const res = await getTrainDataApi({
                state: `${filterForm.trainNo}01`,
                carriageIds: targetCarriages.join(','),
                startTime: filterForm.timeRange[0],
                endTime: filterForm.timeRange[1],
                unitName: filterForm.unitName || '',
                page, limit: 500
            })
            
            const batch = res?.data?.list || []
            allRows = allRows.concat(batch)
            
            if (res?.data?.total) serverTotal = res.data.total

            // 终止条件：已取完服务端总数 / 当前批不足 500 / 安全上限 2000 页（100 万条）
            const done = (serverTotal > 0 && allRows.length >= serverTotal)
                || batch.length < 500
                || page >= 2000
            if (done) break
            page++
        }
        
        if (allRows.length === 0) { ElMessage.warning('暂无数据'); return }
        
        const csv = '﻿' + CSV_HEADERS.join(',') + '\n' + allRows.map(rowToCsv).join('\n')
        const zip = new JSZip()
        
        // 文件名显示选择的车厢范围
        const carriageLabel = carriageNos.length > 0 
            ? carriageNos.map(c => CARRIAGE_MAP[c] || c).join('-')
            : '全部车厢'
        const fname = `历史数据_0${filterForm.trainNo}_${carriageLabel}_${dayjs().format('YYYYMMDD')}.csv`
        
        zip.file(fname, csv)
        const blob = await zip.generateAsync({ type:'blob', compression:'DEFLATE', compressionOptions:{level:6} })
        saveAs(blob, fname.replace('.csv', '.zip'))
        ElMessage.success(`已下载 ${allRows.length} 条`)
    } catch (err) {
        console.error('下载失败:', err); ElMessage.error('下载失败')
    } finally {
        downloading.value = false
    }
}

const goBack = () => {
    router.push({ name: 'trainInfo', query: { trainNo: String(filterForm.trainNo).padStart(5, '0'), trainCoach: filterForm.carriageNos?.[0] || '1' } })
}

onMounted(() => {
    handleQuery()
})

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

    .header-left {
        display: flex;
        align-items: center;
        gap: 15px;
    }

    .header-right {
        .filter-form {
            display: flex;
            align-items: center;
            :deep(.el-form-item) {
                margin-bottom: 0 !important;
                margin-right: 24px !important;
                display: flex;
                align-items: center;
            }
            :deep(.el-form-item__label) {
                color: #2186cf;
                font-weight: bold;
                padding-right: 6px;
                white-space: nowrap;
                font-size: 13px;
            }
        }
    }
}

.monitor-container {
    padding: 20px;
}

.section-box {
    background: #141a2e;
    border-radius: 8px;
    border: 1px solid #262e45;
    padding: 15px;
    box-shadow: 0 6px 15px rgba(0,0,0,0.3);
}

.section-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 15px;

    .title-icon {
        width: 4px;
        height: 16px;
        background: #2186cf;
        border-radius: 2px;
    }

    .title-text {
        color: #ffffff;
        font-size: 15px;
        font-weight: bold;
    }

    .record-count {
        color: #676e82;
        font-size: 12px;
    }
}

// 修复 Element Plus 固定列拖动时产生的重影（双重 box-shadow 叠加）
.no-ghost-table {
    :deep(.el-table__fixed),
    :deep(.el-table__fixed-right) {
        box-shadow: none !important;
    }
    :deep(.el-table__fixed-right-patch) {
        background: #141a2e !important;
    }
    :deep(td.el-table-fixed-column--right),
    :deep(td.el-table-fixed-column--left) {
        background: #141a2e !important;
    }
}

.nav-btn {
    background: #0a0f1d !important;
    border: 1px solid #2186cf !important;
    color: #ffffff !important;
    font-size: 13px !important;
    font-weight: 500 !important;
    height: 32px !important;
    padding: 0 15px !important;
    border-radius: 4px !important;
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1) !important;

    &:hover, &:focus {
        background: rgba(33, 134, 207, 0.2) !important;
        border-color: #409eff !important;
        color: #ffffff !important;
        box-shadow: 0 0 10px rgba(33, 134, 207, 0.3) !important;
    }

    :deep(.el-icon) {
        font-size: 14px !important;
        margin-right: 5px !important;
        color: #2186cf !important;
    }
}

:deep(.el-table) {
    background: transparent !important;
    color: #d1d9e7;
    --el-table-border-color: #262e45;
    --el-table-header-bg-color: #1a2234;
    --el-table-tr-bg-color: transparent;
    --el-table-row-hover-bg-color: rgba(33, 134, 207, 0.1);

    th.el-table__cell {
        background: #1a2234 !important;
        color: #2186cf;
        font-weight: bold;
        border-bottom: 2px solid #2186cf;
    }

    td.el-table__cell {
        border-bottom: 1px solid #262e45;
    }

    .el-table__inner-wrapper::before {
        display: none;
    }
}

.detail-pagination {
    margin-top: 20px;
    display: flex;
    justify-content: center;
}

.train-select, .carriage-select {
    width: 110px !important;
}

/* 对话框头部自定义布局 */
.dialog-header {
    display: flex;
    align-items: baseline;
    gap: 16px;

    .dialog-title {
        color: #2186cf;
        font-size: 16px;
        font-weight: bold;
    }

    .dialog-sub {
        color: #8899bb;
        font-size: 13px;
    }
}
</style>

<style lang="scss">
/* 暗色对话框：全局覆盖（el-dialog appended to body，必须用非 scoped） */
.dark-dialog.el-dialog {
    background: #0e1628 !important;
    border: 1px solid #2186cf !important;
    box-shadow: 0 0 40px rgba(0, 0, 0, 0.9), 0 0 0 1px rgba(33, 134, 207, 0.3);
    border-radius: 10px;

    .el-dialog__header {
        margin: 0;
        padding: 14px 20px;
        border-bottom: 1px solid rgba(33, 134, 207, 0.25);
        background: #141b2e;
        border-radius: 10px 10px 0 0;
    }

    .el-dialog__headerbtn {
        top: 14px;
        .el-dialog__close {
            color: #2186cf;
            font-size: 18px;
            &:hover { color: #fff; }
        }
    }

    .el-dialog__body {
        padding: 20px;
        background: #0e1628;
    }

    /* 内部表格继承暗色 */
    .el-table {
        background: transparent !important;
        color: #d1d9e7 !important;
        --el-table-border-color: #262e45;
        --el-table-header-bg-color: #1a2234;
        --el-table-tr-bg-color: transparent;
        --el-table-row-hover-bg-color: rgba(33, 134, 207, 0.1);
        --el-fill-color-lighter: #1e2840;

        th.el-table__cell {
            background: #1a2234 !important;
            color: #2186cf !important;
            font-weight: bold;
            border-bottom: 2px solid #2186cf;
        }

        td.el-table__cell {
            border-bottom: 1px solid #262e45;
            background: transparent !important;
        }

        .el-table__body-wrapper {
            background: transparent !important;
        }

        .el-table__inner-wrapper::before { display: none; }
    }

    /* 内部分页 */
    .el-pagination {
        --el-pagination-bg-color: transparent;
        --el-pagination-button-bg-color: transparent;
        --el-pagination-button-color: #ffffff;
        color: #d1d9e7;

        .el-pager li {
            background: transparent !important;
            color: #d1d9e7 !important;
            &.is-active { color: #2186cf !important; font-weight: bold; }
        }

        .el-input__wrapper {
            background-color: #0a0f1d !important;
            box-shadow: 0 0 0 1px #2186cf inset !important;
            .el-input__inner { color: #fff !important; }
        }

        .el-pagination__total, .el-pagination__jump { color: #d1d9e7 !important; }
    }
}

/* 全局翻页组件暗色（刷新后也生效） */
.el-pagination {
    --el-pagination-bg-color: transparent !important;
    --el-pagination-button-bg-color: transparent !important;
    --el-pagination-button-color: #ffffff !important;
    --el-pagination-button-disabled-bg-color: transparent !important;
    color: #d1d9e7;
    .el-pager li { background: transparent !important; color: #d1d9e7 !important; &.is-active { color: #2186cf !important; font-weight: bold; } }
    .el-select, .el-input {
        --el-fill-color-blank: #0a0f1d !important; --el-border-color: #2186cf !important; --el-text-color-regular: #fff !important;
        .el-input__wrapper { background-color: #0a0f1d !important; box-shadow: 0 0 0 1px #2186cf inset !important; border: none !important; .el-input__inner { color: #fff !important; background: transparent !important; } }
    }
    .el-pagination__total, .el-pagination__jump { color: #d1d9e7 !important; }
}

/* 全局筛选框暗色 */
.filter-form {
    --el-fill-color-blank: #0a0f1d !important;
    --el-border-color: #2186cf !important;
    --el-text-color-regular: #ffffff !important;
    --el-border-color-hover: #409eff !important;

    .el-input__wrapper {
        background-color: #0a0f1d !important;
        box-shadow: 0 0 0 1px #2186cf inset !important;
        border-radius: 4px !important;
        height: 32px !important;

        &:hover { box-shadow: 0 0 0 1px #409eff inset, 0 0 10px rgba(33, 134, 207, 0.4) !important; }
        &.is-focus { box-shadow: 0 0 0 1px #409eff inset, 0 0 15px rgba(33, 134, 207, 0.6) !important; }

        .el-input__inner { color: #ffffff !important; font-size: 13px !important; }
    }

    .el-range-editor.el-input__wrapper { width: 350px !important; }
    .el-range-input { color: #fff !important; }
    .el-range-separator { color: #2186cf !important; }
}

/* 下拉菜单 & 日期弹窗 */
.el-select__popper.el-popper,
.el-picker__popper.el-popper,
.history-data-picker.el-popper {
    background-color: #141a2e !important;
    border: 1px solid #2186cf !important;
    box-shadow: 0 4px 12px rgba(0,0,0,0.5) !important;
    color: #fff !important;

    .el-select-dropdown__item {
        color: #d1d9e7 !important;
        &:hover, &.hover { background-color: rgba(33, 134, 207, 0.2) !important; color: #ffffff !important; }
        &.selected { color: #2186cf !important; font-weight: bold; background-color: rgba(33, 134, 207, 0.1) !important; }
    }

    .el-picker-panel {
        background-color: #141a2e !important;
        color: #fff !important;
        border: none !important;
    }

    .el-date-range-picker__time-header,
    .el-date-range-picker__header {
        border-bottom: 1px solid #262e45 !important;
        .el-input__wrapper { background-color: #0a0f1d !important; box-shadow: 0 0 0 1px #2186cf inset !important; .el-input__inner { color: #fff !important; } }
    }

    .el-date-range-picker__content.is-left { border-right: 1px solid #262e45 !important; }
    .el-date-table th { color: #2186cf !important; border-bottom: 1px solid #262e45 !important; }
    .el-date-table td.next-month, .el-date-table td.prev-month { color: #4e5969 !important; }
    .el-date-table td.available:hover { color: #2186cf !important; }
    .el-date-table td.in-range .el-date-table-cell { background-color: rgba(33, 134, 207, 0.1) !important; }
    /* 禁用日期：字体灰色、背景与正常日期一致（保持透明）、不可点击 */
    .el-date-table td.disabled,
    .el-date-table td.disabled:hover {
        background-color: transparent !important;
        cursor: not-allowed !important;
        .el-date-table-cell,
        .cell {
            color: #4e5969 !important;
            background-color: transparent !important;
        }
    }
    /* 时间面板：紧凑布局，避免超出视口右侧 */
    .el-time-panel {
        background-color: #141a2e !important;
        border: 1px solid #2186cf !important;
        width: 150px !important;
        max-width: 150px !important;
        box-sizing: border-box !important;
        padding: 4px 0 !important;
    }
    .el-time-panel__content {
        padding: 0 !important;
        display: flex !important;
        justify-content: center !important;
    }
    .el-time-spinner {
        display: flex !important;
        justify-content: center !important;
    }
    /* 三列均匀分布，整体居中 */
    .el-time-spinner__wrapper {
        flex: 1 1 0 !important;
        width: auto !important;
        max-width: 48px !important;
        box-sizing: border-box !important;
        text-align: center !important;
        & + .el-time-spinner__wrapper { margin-left: 0 !important; }
    }
    .el-time-spinner__item {
        height: 28px !important;
        line-height: 28px !important;
        padding: 0 !important;
        font-size: 12px !important;
        text-align: center !important;
        color: #d1d9e7 !important;
        &:hover { background: rgba(33, 134, 207, 0.2) !important; }
        &.is-active { color: #2186cf !important; font-weight: bold; }
    }
    .el-time-spinner__arrow {
        font-size: 12px !important;
        left: 50% !important;
        transform: translateX(-50%) !important;
    }
    .el-picker-panel__footer {
        background-color: #141a2e !important;
        border-top: 1px solid #262e45 !important;
        padding: 10px !important;
        .el-button {
            background: #0a0f1d !important;
            border: 1px solid #2186cf !important;
            color: #ffffff !important;
            height: 28px !important;
            padding: 0 12px !important;
            font-size: 12px !important;
            &--primary { background: #2186cf !important; border-color: #2186cf !important; &:hover { background: #409eff !important; } }
            &:hover { background: rgba(33, 134, 207, 0.2) !important; border-color: #409eff !important; }
        }
    }
}

/* 强制多选框选中选项为透明背景 */
.el-select__selected-item,
.el-tag.el-tag--info {
    background-color: rgba(33,134,207,0.2) !important;
    border-color: #2186cf !important;
    color: #ffffff !important;
    .el-tag__close { color: #ffffff !important; }
}

/* 表头说明图标：i 图标 + hover 提示 */
.header-with-tip {
    display: inline-flex !important;
    align-items: center;
    gap: 4px;
    cursor: help;

    .tip-icon {
        color: #2186cf;
        font-size: 14px;
        &:hover { color: #409eff; }
    }
}
</style>
