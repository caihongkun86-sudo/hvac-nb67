<template>
    <div class="healthy-container">
        <div class="section-header">
            <span class="title-icon"></span>
            <span class="title-text">健康评估信息</span>
            <div class="health-legend">
                <span class="legend-item healthy"><i></i>健康</span>
                <span class="legend-item sub-healthy"><i></i>亚健康</span>
                <span class="legend-item unhealthy"><i></i>非健康</span>
            </div>
        </div>
        <div class="content">
            <el-table 
                ref="tableRef"
                :data="assessmentData" 
                stripe 
                style="width: 100%"
                height="350px"
                :row-style="{ height: '50px' }"
                :header-row-style="{ height: '50px' }"
                class="scrollable-table"
            >
                <el-table-column prop="equipment_name" label="设备名称" min-width="150" />
                
                <el-table-column prop="health_status" label="健康状态" min-width="120">
                    <template #default="scope">
                        <el-tag :type="getStatusType(scope.row.health_status)" effect="dark" round>
                            {{ scope.row.health_status }}
                        </el-tag>
                    </template>
                </el-table-column>

                <el-table-column label="累计运行时间 / 额定寿命" min-width="220">
                    <template #default="scope">
                        <div class="life-compare" v-if="scope.row.rated_running_time">
                            <span :class="['actual', getLifeClass(scope.row.cumulative_running_time, scope.row.rated_running_time)]">
                                {{ scope.row.cumulative_running_time }}h
                            </span>
                            <span class="divider">/</span>
                            <span class="rated">{{ scope.row.rated_running_time }}h</span>
                        </div>
                        <span v-else class="placeholder">--</span>
                    </template>
                </el-table-column>

                <el-table-column label="动作次数 / 额定寿命" min-width="200">
                    <template #default="scope">
                        <div class="life-compare" v-if="scope.row.rated_actions">
                            <span :class="['actual', getLifeClass(scope.row.number_actions, scope.row.rated_actions)]">
                                {{ scope.row.number_actions }}次
                            </span>
                            <span class="divider">/</span>
                            <span class="rated">{{ scope.row.rated_actions }}次</span>
                        </div>
                        <span v-else class="placeholder">--</span>
                    </template>
                </el-table-column>

                <el-table-column label="健康建议" min-width="200">
                    <template #default="scope">
                        <div :class="['suggestion-box', getStatusClass(scope.row.health_status)]">
                            <el-icon class="sug-icon">
                                <InfoFilled v-if="scope.row.health_status === '健康'" />
                                <WarnTriangleFilled v-else-if="scope.row.health_status === '亚健康'" />
                                <CircleCloseFilled v-else />
                            </el-icon>
                            <span>{{ scope.row.suggestion || getSuggestion(scope.row.health_status) }}</span>
                        </div>
                    </template>
                </el-table-column>

                <template #empty>
                    <div class="no-data-box">
                        <img src="/img/no-data.svg" width="60" />
                        <p>暂无评估数据</p>
                    </div>
                </template>
            </el-table>
        </div>
    </div>
</template>

<script setup>
import { ref, onMounted, watch, onUnmounted } from 'vue'
import { getHealthAssessment } from '@/api/api'

const props = defineProps({
    carriageId: { type: String, default: '' },
    isOffline: { type: Boolean, default: false }
})

const assessmentData = ref([])
const tableRef = ref(null)

const getStatusType = (status) => {
    if (status === '健康') return 'success'
    if (status === '亚健康') return 'warning'
    if (status === '非健康') return 'danger'
    return 'info'
}

const getStatusClass = (status) => {
    if (status === '健康') return 'text-success'
    if (status === '亚健康') return 'text-warning'
    if (status === '非健康') return 'text-danger'
    return ''
}

const getLifeClass = (actual, rated) => {
    if (!rated) return ''
    const ratio = actual / rated
    if (ratio >= 1) return 'life-danger'
    if (ratio >= 0.8) return 'life-warning'
    return 'life-normal'
}

const getSuggestion = (status) => {
    if (status === '健康') return '设备运行良好，继续保持'
    if (status === '亚健康') return '寿命接近临界点，请关注使用寿命'
    if (status === '非健康') return '设备状态严重异常，请立即更换部件'
    return '暂无建议'
}

const fetchData = () => {
    if (!props.carriageId) return
    getHealthAssessment(props.carriageId).then(res => {
        assessmentData.value = res.data || res.vw_health_assessment || []
    }).catch(() => {
        // 请求失败（含离线）时保留上次数据，等待下次轮询重试
    })
}

watch(() => props.carriageId, () => {
    assessmentData.value = []
    fetchData()
})

import { MONITOR_CONFIG } from '@/config/monitorConfig.js'

let dataTimer = null

onMounted(() => {
    fetchData()
    dataTimer = setInterval(fetchData, MONITOR_CONFIG.refreshInterval)
})

onUnmounted(() => {
  if(dataTimer) clearInterval(dataTimer)
})
</script>

<style scoped lang="scss">
.healthy-container {
    padding: 20px;
}

.content {
    height: 350px; 
    position: relative;
}

.scrollable-table {
    background: transparent !important;
    
    :deep(.el-table__inner-wrapper::before) { display: none; }
    
    :deep(.el-table__header-wrapper) { 
        background: rgba(255, 255, 255, 0.05); 
        z-index: 10;
        position: relative;
    }

    // 默认隐藏滚动条，鼠标悬停时显示 (针对 Webkit 浏览器)
    :deep(.el-scrollbar__bar.is-vertical) {
        opacity: 0;
        transition: opacity 0.3s;
    }

    &:hover {
        :deep(.el-scrollbar__bar.is-vertical) {
            opacity: 1;
        }
    }
}

.section-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 20px;

    .title-icon {
        width: 4px;
        height: 16px;
        background: #ffffff;
        border-radius: 2px;
    }

    .title-text {
        color: #ffffff;
        font-size: 16px;
        font-weight: bold;
        flex: 1;
    }

    .health-legend {
        display: flex;
        gap: 12px;
        .legend-item {
            display: flex;
            align-items: center;
            gap: 4px;
            font-size: 11px;
            color: rgba(255,255,255,0.7);
            i {
                display: inline-block;
                width: 10px;
                height: 10px;
                border-radius: 50%;
            }
            &.healthy    i { background: #42ad5d; }
            &.sub-healthy i { background: #ffa55c; }
            &.unhealthy  i { background: #e65355; }
        }
    }
}

.life-compare {
    display: flex;
    align-items: center;
    gap: 8px;
    font-family: 'Outfit', sans-serif;
    .divider { color: #394153; }
    .rated { color: #676e82; font-size: 12px; }
    
    .actual {
        font-weight: bold;
        &.life-normal { color: #42ad5d; }
        &.life-warning { color: #ffa55c; text-shadow: 0 0 8px rgba(255,165,92,0.4); }
        &.life-danger { color: #e65355; text-shadow: 0 0 8px rgba(230,83,85,0.4); }
    }

    .placeholder {
        color: #394153;
        font-size: 13px;
    }
}

.suggestion-box {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    padding: 4px 12px;
    border-radius: 4px;
    background: rgba(255,255,255,0.03);
    
    &.text-success { color: #42ad5d; border-left: 3px solid #42ad5d; }
    &.text-warning { color: #ffa55c; border-left: 3px solid #ffa55c; }
    &.text-danger { color: #e65355; border-left: 3px solid #e65355; }
    
    .sug-icon { font-size: 16px; }
}

.no-data-box {
    padding: 40px 0;
    text-align: center;
    color: #676e82;
}

:deep(.el-tag--dark.el-tag--success) { background-color: #42ad5d; border-color: #42ad5d; }
:deep(.el-tag--dark.el-tag--warning) { background-color: #ffa55c; border-color: #ffa55c; }
:deep(.el-tag--dark.el-tag--danger) { background-color: #e65355; border-color: #e65355; }
</style>
