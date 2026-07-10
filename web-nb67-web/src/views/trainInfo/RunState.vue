<template>
    <div class="run-state-card">
        <div class="section-header">
            <span class="title-icon"></span>
            <span class="title-text">运行状态信息</span>
            <div class="legend">
                <span class="legend-item run"><i></i>运行</span>
                <span class="legend-item stop"><i></i>停机</span>
            </div>
        </div>
        <div class="table-container">
            <div class="custom-table">
                <!-- 表头 -->
                <div class="table-header flex-row">
                    <div class="cell label-cell"></div>
                    <div v-for="n in 6" :key="n" class="cell car-cell">
                        <div class="unit-titles">
                            <span v-if="n <= 3">机组一</span>
                            <span v-else>机组二</span>
                            <span v-if="n <= 3">机组二</span>
                            <span v-else>机组一</span>
                        </div>
                    </div>
                </div>

                <!-- 数据行 -->
                <div v-for="row in rows" :key="row.key" class="table-row flex-row">
                    <div class="cell label-cell">{{ row.label }}</div>
                    <div v-for="n in 6" :key="n" class="cell car-group">
                        <div class="unit-data-pair">
                            <div :class="['unit-val', getValClass(n, 1, row)]">
                                {{ getVal(n, 1, row) }}
                            </div>
                            <div :class="['unit-val', getValClass(n, 2, row)]">
                                {{ getVal(n, 2, row) }}
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    </div>
</template>

<script setup>
import { ref, onMounted, watch, onUnmounted } from 'vue'
import { getRunningState } from '@/api/api'

const props = defineProps({
    trainId:   { type: String, default: '' },
    isOffline: { type: Boolean, default: false }
})

const rawData = ref([])
let refreshTimer = null

const rows = [
    { label: '压缩机1', key: 'comp_u1', isStatus: true },
    { label: '压缩机2', key: 'comp_u2', isStatus: true },
    { label: '通风机',  key: 'ef',      isStatus: true },
    { label: '冷凝风机', key: 'cf',     isStatus: true },
    { label: '新风阀',  key: 'fadpos', suffix1: '%', suffix2: 's', isGlobal: true, multiplier: 0.35 },
    { label: '回风阀',  key: 'radpos', suffix1: '%', suffix2: 's', isGlobal: true, multiplier: 0.35 },
    { label: '废排风阀', key: 'dmp_exu_pos', suffix1: '%', suffix2: 's', isGlobal: true, multiplier: 0.35 },
    // 空气质量6行（来自示意图右上角，需求2迁移至此）
    { label: 'CO₂',    key: 'aq_co2',   suffix: 'ppm', isAQ: true },
    { label: 'TVOC',   key: 'aq_tvoc',  suffix: 'ppb', isAQ: true },
    { label: 'PM2.5',  key: 'aq_pm25',  suffix: 'µg/m³', isAQ: true },
    { label: 'PM10',   key: 'aq_pm10',  suffix: 'µg/m³', isAQ: true },
    { label: '空气温度', key: 'aq_t',   suffix: '℃', isAQ: true },
    { label: '空气湿度', key: 'aq_h',   suffix: 'RH%', isAQ: true },
]

// 对称逻辑：1-3车(1,2), 4-6车(2,1)
const getUnitIdx = (carIdx, pos) => {
    if (carIdx <= 3) return pos // 1, 2
    return pos === 1 ? 2 : 1    // 2, 1
}

const getVal = (carIdx, pos, row) => {
    // 匹配车厢：严格按 ID 匹配，不再使用下标兜底以防数据错位
    const carData = rawData.value.find(item => {
        const cNo = String(item.carriage_id || item.carriage_no || '')
        return cNo == String(carIdx) || 
               cNo == ("0" + carIdx) || 
               cNo.endsWith("0" + carIdx)
    })
    
    if (!carData) return '-'
    const unitIdx = getUnitIdx(carIdx, pos)
    
    // 智能查找 Key：支持多种命名规范 (PascalCase, snake_case, cfbk前缀等)
    const findKeyInObj = (base) => {
        const toPascal = (s) => s.split('_').map(w => w.charAt(0).toUpperCase() + w.slice(1).toLowerCase()).join('');
        const pBase = toPascal(base);
        
        // 生成所有可能的字段名格式
        const possible = [
            base,                          // ef_u1
            base.toLowerCase(),            // ef_u1
            pBase,                         // EfU1
            `Cfbk${pBase}`,                // CfbkEfU1
            `Cfbk${pBase}1`,               // CfbkEfU11 (状态字段通常带11/12后缀)
            `Cfbk${pBase}2`,               // CfbkEfU12
            `cfbk_${base}`.toLowerCase(),  // cfbk_ef_u1
            `cfbk_${base}1`.toLowerCase(), // cfbk_ef_u11
            `cfbk_${base}2`.toLowerCase(), // cfbk_ef_u12
            `cfbk_${base}`.toUpperCase()   // CFBK_EF_U1
        ];
        
        // 第一阶段：精确匹配
        for (const k of possible) {
            if (carData[k] !== undefined) {
                return k;
            }
        }
        
        // 第二阶段：模糊匹配，优先选择 Cfbk 前缀的运行状态字段
        const flatBase = base.toLowerCase().replace(/_/g, '');
        const matchedFields = Object.keys(carData).filter(k => 
            k.toLowerCase().replace(/_/g, '').includes(flatBase)
        );
        
        if (matchedFields.length > 0) {
            // 优先选择 Cfbk 前缀的字段（运行状态）
            const cfbkField = matchedFields.find(k => k.startsWith('Cfbk'));
            if (cfbkField) {
                return cfbkField;
            }
            // 其次选择小写 cfbk_ 前缀的字段
            const cfbkLowerField = matchedFields.find(k => k.startsWith('cfbk_'));
            if (cfbkLowerField) {
                return cfbkLowerField;
            }
            // 最后返回第一个匹配的字段
            return matchedFields[0];
        }
        
        return undefined;
    };

    if (row.isGlobal) {
        // 全局型：直接用 key 查找，不附加 _u${unitIdx}（如废排风阀，一车一个传感器）
        const key = findKeyInObj(row.key)
        const originalNum = carData[key]
        if (originalNum !== undefined && originalNum !== null) {
            if (row.multiplier && row.suffix1 && row.suffix2) {
                // 同时显示原始值和换算后的值（如：70%(200s)）
                const convertedNum = Math.ceil(Number(originalNum) * row.multiplier)
                return `${originalNum}${row.suffix1} (${convertedNum}${row.suffix2})`
            }
            // 如果有 multiplier，进行乘法运算
            if (row.multiplier) {
                return `${Math.ceil(Number(originalNum) * row.multiplier)}${row.suffix}`
            }
            return `${originalNum}${row.suffix}`
        }
        return '-'
    } else if (row.isAQ) {
        // 空气质量类：每列只取机组1的值（每车厢一个传感器，U1/U2取平均或取U1）
        // key 格式：aq_co2 → AqCo2U1 / AqCo2U2
        const suffix = `U${unitIdx}`
        const keyMap = {
            'aq_co2': `AqCo2${suffix}`, 'aq_tvoc': `AqTvoc${suffix}`,
            'aq_pm25': `AqPm25${suffix}`, 'aq_pm10': `AqPm10${suffix}`,
            'aq_t': `AqT${suffix}`, 'aq_h': `AqH${suffix}`
        }
        const rawKey = keyMap[row.key]
        const val = rawKey ? carData[rawKey] : undefined
        if (val === undefined || val === null || val === '') return '-'
        // aq_t 和 aq_h 原始值需 ÷10 后才是实际值（同协议约定）
        const needsScale = row.key === 'aq_t' || row.key === 'aq_h'
        const displayVal = needsScale ? (Number(val) / 10).toFixed(1) : val
        return `${displayVal}${row.suffix}`
    } else if (row.isStatus) {
        // 状态类：构造基础 Key
        let baseKey = row.key.startsWith('comp_u')
            ? `comp_u${unitIdx}${row.key.slice(-1)}`
            : `${row.key}_u${unitIdx}`;

        const key = findKeyInObj(baseKey);
        const val = carData[key];

        if (val === undefined || val === null) return '-'
        // 允许数字 1, 布尔值 true, 字符串 "true" (忽略大小写)
        const isRun = val == 1 || val === true || String(val).toLowerCase() === 'true';
        return isRun ? '运行' : '停机'
    } else {
        // 数值类：如新风阀、回风阀
        const key = findKeyInObj(`${row.key}_u${unitIdx}`);
        const num = carData[key];
        return num !== undefined && num !== null ? `${num}${row.suffix}` : '-'
    }
}

const getValClass = (carIdx, pos, row) => {
    if (!row.isStatus) return ''
    const val = getVal(carIdx, pos, row)
    if (val === '运行') return 'status-run'
    if (val === '停机') return 'status-stop'
    return ''
}

const fetchData = () => {
    if (props.isOffline || !props.trainId) return
    getRunningState(props.trainId).then(res => {
        rawData.value = res.vw_running_state_info || []
        console.log('[RunState] Data Updated:', rawData.value.length)
    })
}

watch(() => props.trainId, () => {
    rawData.value = [] // ID 变化时可以置空
    if (!props.isOffline) fetchData()
})

watch(() => props.isOffline, (offline) => {
    if (offline) {
        rawData.value = [] // 离线时清空运行状态数据
    } else {
        fetchData() // 恢复在线时重新拉取
    }
})

import { MONITOR_CONFIG } from '@/config/monitorConfig.js'

onMounted(() => {
    fetchData()
    refreshTimer = setInterval(fetchData, MONITOR_CONFIG.refreshInterval)
})

onUnmounted(() => {
    if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<style scoped lang="scss">
.run-state-card {
    padding: 0;
}

.section-header {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
    padding-left: 10px;

    .title-icon {
        width: 4px;
        height: 16px;
        background: #ffffff;
        border-radius: 2px;
    }

    .title-text {
        color: #ffffff;
        font-size: 15px;
        font-weight: bold;
        flex: 1;
    }

    .legend {
        display: flex;
        gap: 12px;
        margin-right: 8px;
        .legend-item {
            display: flex;
            align-items: center;
            gap: 4px;
            font-size: 11px;
            color: rgba(255,255,255,0.7);
            i {
                display: inline-block;
                width: 12px;
                height: 12px;
                border-radius: 2px;
            }
            &.run i  { background: #42ad5d; }
            &.stop i { background: #e65355; }
        }
    }
}

.custom-table {
    display: flex;
    flex-direction: column;
    border: 1px solid #262e45;
    background: #1a2234;

    .flex-row {
        display: grid;
        grid-template-columns: 100px repeat(6, 1fr);
        border-bottom: 1px solid #262e45;
        &:last-child { border-bottom: none; }
    }

    .cell {
        padding: 2px 6px;
        display: flex;
        flex-direction: column;
        justify-content: center;
        align-items: center;
        border-right: 1px solid #262e45;
        &:last-child { border-right: none; }
        min-height: 26px;
    }

    .label-cell {
        background: #141a2e !important; // 与表头背景保持一致
        color: #2186cf;
        font-size: 13px;
        font-weight: bold;
        align-items: center;
    }

    .table-header {
        background: #141a2e;
        .unit-titles {
            width: 100%;
            display: flex;
            justify-content: space-between;
            padding: 0 4px;
            font-size: 11px;
            color: #ffffff; // 机组名称改为白色
            
            span {
                flex: 1;
                text-align: center;
                white-space: nowrap;
                font-weight: 500;
            }
        }
    }

    .unit-data-pair {
        width: 100%;
        display: flex;
        gap: 3px;
        .unit-val {
            flex: 1;
            height: 20px;
            line-height: 20px;
            text-align: center;
            font-size: 11px;
            border-radius: 2px;
            color: rgba(255,255,255,0.8);
            background: rgba(255,255,255,0.05) !important; // 统一所有格子的默认背景

            &.status-run { background: #42ad5d !important; color: #fff; }
            &.status-stop { background: #e65355 !important; color: #fff; }
        }
    }
}
</style>
