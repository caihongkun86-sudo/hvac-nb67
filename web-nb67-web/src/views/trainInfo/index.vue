<template>
    <div class="warp2">
        <!-- 顶部导航与筛选 -->
        <div class="header-monitor">
            <div class="header-left">
                <el-button type="primary" class="nav-btn" icon="Calendar" @click="gotoPath('historyData')">历史数据</el-button>
                <el-button type="primary" class="nav-btn" icon="Bell" @click="gotoPath('historyAlarm')">历史报警</el-button>
                <el-button type="primary" class="nav-btn" icon="Warning" @click="gotoPath('historyWarning')">历史预警</el-button>
            </div>

            <div class="header-right">
                <el-form :inline="true" :model="filterForm" class="filter-form">
                    <el-form-item label="车号">
                        <el-select v-model="filterForm.trainNo" placeholder="请选择车号" @change="handleTrainChange">
                            <el-option v-for="item in trainList" :key="item.train_id" :label="item.label" :value="item.train_id" />
                        </el-select>
                    </el-form-item>
                    <el-form-item label="车厢号">
                        <el-select v-model="filterForm.carriageNo" placeholder="请选择车厢号" class="carriage-select">
                            <el-option v-for="item in carriageList" :key="item.carriage_no" :label="item.label" :value="item.carriage_no" />
                        </el-select>
                    </el-form-item>
                    <el-form-item>
                        <el-button type="primary" class="nav-btn" icon="Search" @click="handleQuery">查询</el-button>
                    </el-form-item>
                </el-form>
            </div>
        </div>

        <div class="monitor-container">
            <!-- 2. 全车概览 (固定在顶部) -->
            <div class="section-box sticky-section">
                <CarCrew :trainId="currentTrainNo" :activeCarriage="currentCarriageNo" :isOffline="isOffline" @select="handleCarSelect" />
            </div>

            <!-- 3. 运行参数 (全车6节) -->
            <div class="section-box">
                <RunState :trainId="currentTrainNo" :isOffline="isOffline" />
            </div>

            <!-- 4. 机组原理图 -->
            <div class="section-box">
                <TrainUnitMap :carriageId="fullCarriageId" :isOffline="isOffline" />
            </div>

            <!-- 5. 健康评估信息 -->
            <div class="section-box">
                <Healthy :carriageId="fullCarriageId" :isOffline="isOffline" />
            </div>

            <!-- 6. 预警中心 (左右分栏) -->
            <el-row :gutter="15">
                <el-col :span="12">
                    <div class="section-box">
                        <ActualWarning :ActualWarningData="filteredActualAlarms"/>
                    </div>
                </el-col>
                <el-col :span="12">
                    <div class="section-box">
                        <StateWarning :StateWarningData="filteredStateWarnings"/>
                    </div>
                </el-col>
            </el-row>

            <div class="monitor-container-bottom">
                <div class="section-box chart-box">
                    <Echart :carriageId="fullCarriageId" :isOffline="isOffline" />
                </div>
            </div>


        </div>
    </div>
</template>

<script setup>

import CarCrew from "./CarCrew.vue"
import RunState from "./RunState.vue"
import TrainUnitMap from "@/views/airConditioner/TrainUnitMap.vue"
import Healthy from "@/views/airConditioner/Healthy.vue"
import ActualWarning from "./ActualWarning.vue"
import StateWarning from "./StateWarning.vue"
import Echart from "@/views/airConditioner/Echart.vue"
import { formatTime } from '@/utils/time'
import { ref, reactive, onMounted, onUnmounted, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
    getRealtimeAlarmDetail,
    getStatusAlert,
    getAlarmInformation,
    getRunningState,
    getTrainTemperature,
    getRealtimeAlarm,
    getActiveCarApi,
    getAirSystemApi,
    getTrainSelection,
    getLatestDataTime
} from '@/api/api'
let route = useRoute()
let router = useRouter()

// 筛选状态与 URL 参数初始化 (保持 String 类型以匹配 el-option)
const filterForm = reactive({
    trainNo: String(parseInt(String(route.query.trainNo || '7001')) || 7001),
    carriageNo: String(route.query.trainCoach || '1')
})

const currentTrainNo = ref(filterForm.trainNo)
const currentCarriageNo = ref(filterForm.carriageNo)

// 计算全路径车厢 ID ( e.g. 700101 )
const fullCarriageId = computed(() => {
    if (!currentTrainNo.value || !currentCarriageNo.value) return ''
    return `${currentTrainNo.value}0${currentCarriageNo.value}`
})


// 生成车号列表 (6001 - 6037)
const trainList = ref(Array.from({ length: 37 }, (_, i) => ({
    train_id: (6001 + i).toString(),
    label: `0${6001 + i}`
})))

// 生成车厢号列表 (TC1/MP1/M1/M2/MP2/TC2)
const CARRIAGE_NAMES = { '1':'TC1','2':'MP1','3':'M1','4':'M2','5':'MP2','6':'TC2' }
const carriageList = ref(Array.from({ length: 6 }, (_, i) => ({
    carriage_no: (i + 1).toString(),
    label: CARRIAGE_NAMES[String(i + 1)]
})))

let AlarmInfoData = ref([])  //报警信息
let ActualWarningData = ref([]) //实时报警
let StateWarningData = ref([]) //状态预警
let RunStateData = ref([]) //运行状态信息
let CarTemperatureData = ref([]) //车厢温度信息

// 离线检测：5分钟无新数据视为离线
const isOffline = ref(false)
const OFFLINE_THRESHOLD_MS = 5 * 60 * 1000
let offlineCheckTimer = null

const checkOnlineStatus = async () => {
    if (!currentTrainNo.value) return
    try {
        const res = await getRunningState(currentTrainNo.value)
        const records = res?.vw_running_state_info || []
        if (records.length === 0) {
            isOffline.value = true
            return
        }
        let latestMs = 0
        for (const r of records) {
            const t = r.ingest_time
            if (t) {
                const ms = new Date(t).getTime()
                if (ms > latestMs) latestMs = ms
            }
        }
        if (latestMs === 0) {
            isOffline.value = true
            return
        }
        isOffline.value = Date.now() - latestMs > OFFLINE_THRESHOLD_MS
    } catch {
        // 网络异常时保持当前状态
    }
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

// 计算过滤后的报警（仅限当前车厢，排除寿命预测预警）
watch(isOffline, (offline) => {
    if (offline) {
        ActualWarningData.value = []
        StateWarningData.value = []
    }
})

const filteredActualAlarms = computed(() => {
    return ActualWarningData.value.filter(item => String(item.carriage_no) == String(currentCarriageNo.value))
})

const filteredStateWarnings = computed(() => {
    // 过滤逻辑：匹配车厢号 且 排除寿命/健康相关的预警（这部分已在健康评估中展示）
    return StateWarningData.value.filter(item => {
        const isSelectedCar = String(item.carriage_no) == String(currentCarriageNo.value);
        const isNotLifeWarning = item.name && !item.name.includes('寿命') && !item.name.includes('健康');
        return isSelectedCar && isNotLifeWarning;
    })
})
const proposeWarn = [
    {
     value: 'ref_leak_u11',
     name: '机组1系统1冷媒泄漏预警',
     classify: '预警',
     title: `1、检查风阀开启状态
            \n2、检查风机运行电流及工作状态
            \n3、确认制冷系统运行压力
            \n4、检查电子膨胀阀系统`
    },{
     value: 'ref_leak_u12',
     name: '机组1系统2冷媒泄漏预警',
     classify: '预警',
     title: `1、检查风阀开启状态
            \n2、检查风机运行电流及工作状态
            \n3、确认制冷系统运行压力
            \n4、检查电子膨胀阀系统`
    },{
     value: 'ref_leak_u21',
     name: '机组2系统1冷媒泄漏预警',
     classify: '预警',
     title: `1、检查风阀开启状态
            \n2、检查风机运行电流及工作状态
            \n3、确认制冷系统运行压力
            \n4、检查电子膨胀阀系统`
    },{
     value: 'ref_leak_u22',
     name: '机组2系统2冷媒泄漏预警',
     classify: '预警',
     title: `1、检查风阀开启状态
            \n2、检查风机运行电流及工作状态
            \n3、确认制冷系统运行压力
            \n4、检查电子膨胀阀系统`
    },{
     value: 'ref_pump_u1',
     name: '机组1制冷（热泵）系统预警',
     classify: '预警',
     title: `1、检查风机及风阀
            \n2、确认压缩机运行状态。
            \n3、检查确认制冷系统冷媒是否泄漏
            \n4、检查电子膨胀阀系统`
    },{
     value: 'ref_pump_u2',
     name: '机组2制冷（热泵）系统预警',
     classify: '预警',
     title: `1、检查风机及风阀
            \n2、确认压缩机运行状态。
            \n3、检查确认制冷系统冷媒是否泄漏
            \n4、检查电子膨胀阀系统`
    },
    {
     value: 'f_cp_u1',
     name: '机组1制冷系统预警',
     classify: '预警',
     title: `1、检查风机及风阀
            \n2、确认压缩机运行状态。
            \n3、检查确认制冷系统冷媒是否泄漏
            \n4、检查电子膨胀阀系统`
    },{
     value: 'f_cp_u2',
     name: '机组2制冷系统预警',
     classify: '预警',
     title: `1、检查风机及风阀
            \n2、确认压缩机运行状态。
            \n3、检查确认制冷系统冷媒是否泄漏
            \n4、检查电子膨胀阀系统`
    },{
     value: 'fat_sensor',
     name: '新风温度传感器预警',
     classify: '预警',
     title: `1、检查温度传感器接线是否良好。
            \n2、重新上电复位，查看故障是否消除。
            \n3、更换新传感器。`
    },{
     value: 'rat_sensor',
     name: '回风温度传感器预警',
     classify: '预警',
     title: `1、检查温度传感器接线是否良好。
            \n2、重新上电复位，查看故障是否消除。
            \n3、更换新传感器。`
    },
    {
     value: 'f_fas',
     name: '新风温度传感器预警',
     classify: '预警',
     title: `1、检查温度传感器接线是否良好。
            \n2、重新上电复位，查看故障是否消除。
            \n3、更换新传感器。`
    },{
     value: 'f_ras',
     name: '回风温度传感器预警',
     classify: '预警',
     title: `1、检查温度传感器接线是否良好。
            \n2、重新上电复位，查看故障是否消除。
            \n3、更换新传感器。`
    },{
     value: 'bflt_tempover',
     name: '车厢温度超温预警',
     classify: '预警',
     title: `1.检查2个机组的制冷系统是否异常`
    },
    {
     value: 'cabin_overtemp',
     name: '车厢温度超温预警',
     classify: '预警',
     title: `1.检查2个机组的制冷系统是否异常`
    },{
     value: 'f_presdiff_u1',
     name: '机组1滤网脏堵预警',
     classify: '预警',
     title: `1.清洗混合风滤网
            \n2.如果滤网不脏，需要检查微压差传感器是否正常`
    },{
     value: 'f_presdiff_u2',
     name: '机组2滤网脏堵预警',
     classify: '预警',
     title: `1.清洗混合风滤网
            \n2.如果滤网不脏，需要检查微压差传感器是否正常`
    },{
     value: 'f_ef_u11',
     name: '机组1通风机1电流预警',
     classify: '预警',
     title: `1、检查风阀状态
            \n2、检查滤网是否安装
            \n3、检查风机轴承`
    }
    ,{
     value: 'f_ef_u12',
     name: '机组1通风机2电流预警',
     classify: '预警',
     title: `1、检查风阀状态
            \n2、检查滤网是否安装
            \n3、检查风机轴承`
    },{
     value: 'f_ef_u21',
     name: '机组2通风机1电流预警',
     classify: '预警',
     title: `1、检查风阀状态
            \n2、检查滤网是否安装
            \n3、检查风机轴承`
    },{
     value: 'f_ef_u22',
     name: '机组2通风机2电流预警',
     classify: '预警',
     title: `1、检查风阀状态
            \n2、检查滤网是否安装
            \n3、检查风机轴承`
    },{
     value: 'f_cf_u11',
     name: '机组1冷凝风机1电流预警',
     classify: '预警',
     title: `1、检查风阀状态
            \n2、检查滤网是否安装
            \n3、检查风机轴承`
    },{
     value: 'f_cf_u12',
     name: '机组1冷凝风机2电流预警',
     classify: '预警',
     title: `1、检查风阀状态
            \n2、检查滤网是否安装
            \n3、检查风机轴承`
    },{
     value: 'f_cf_u21',
     name: '机组2冷凝风机1电流预警',
     classify: '预警',
     title: `1、检查风阀状态
            \n2、检查滤网是否安装
            \n3、检查风机轴承`
    },{
     value: 'f_cf_u22',
     name: '机组2冷凝风机2电流预警',
     classify: '预警',
     title: `1、检查风阀状态
            \n2、检查滤网是否安装
            \n3、检查风机轴承`
    },{
     value: 'f_exufan',
     name: '废排风机电流预警',
     classify: '预警',
     title: `1、检查风阀状态
            \n2、检查滤网是否安装
            \n3、检查风机轴承`
    },{
     value: 'f_fas_u11',
     name: '机组1压缩机1电流预警',
     classify: '预警',
     title: `1、检查冷凝风机
            \n2、检查冷凝器
            \n3、检查压缩机`
    },{
     value: 'f_fas_u12',
     name: '机组1压缩机2电流预警',
     classify: '预警',
     title: `1、检查冷凝风机
            \n2、检查冷凝器
            \n3、检查压缩机`
    },{
     value: 'f_fas_u21',
     name: '机组2压缩机1电流预警',
     classify: '预警',
     title: `1、检查冷凝风机
            \n2、检查冷凝器
            \n3、检查压缩机`
    },{
     value: 'f_fas_u22',
     name: '机组2压缩机2电流预警',
     classify: '预警',
     title: `1、检查冷凝风机
            \n2、检查冷凝器
            \n3、检查压缩机`
    },{
     value: 'f_aq_u1',
     name: '机组1空气质量预警',
     classify: '预警',
     title: `1、检查新风阀开度是否正常
            \n2、检查车厢内空气环境`
    },{
     value: 'f_aq_u2',
     name: '机组2空气质量预警',
     classify: '预警',
     title: `1、检查新风阀开度是否正常
            \n2、检查车厢内空气环境`
    },
    
    {
     value: 'bflt_airclean_u1',
     name: '空气净化U1故障',
     classify: '预警',
     title: `1、检查空气净化器运行是否正常
            \n2、检查故障反馈线路是否正常`
    },
    {
     value: 'bflt_dfstemp_u11',
     name: '融霜温度传感器1-1故障',
     classify: '预警',
     title: `1、检查温度传感器线路是否存在短路、虚接情况
            \n2、测量温度传感器电阻是否正常`
    },
    {
     value: 'bflt_dfstemp_u12',
     name: '融霜温度传感器1-2故障',
     classify: '预警',
     title: `1、检查温度传感器线路是否存在短路、虚接情况
            \n2、测量温度传感器电阻是否正常`
    },
    {
     value: 'bflt_airclean_u2',
     name: '空气净化U2故障',
     classify: '预警',
     title: `1、检查空气净化器运行是否正常
            \n2、检查故障反馈线路是否正常`
    },
    {
     value: 'bflt_dfstemp_u21',
     name: '融霜温度传感器2-1故障',
     classify: '预警',
     title: `1、检查温度传感器线路是否存在短路、虚接情况
            \n2、测量温度传感器电阻是否正常`
    },
    {
     value: 'bflt_dfstemp_u22',
     name: '融霜温度传感器2-2故障',
     classify: '预警',
     title: `1、检查温度传感器线路是否存在短路、虚接情况
            \n2、测量温度传感器电阻是否正常`
    },
    {
     value: 'bflt_seattemp',
     name: '车厢温度传感器2故障',
     classify: '预警',
     title: `1、检查温度传感器线路是否存在短路、虚接情况
            \n2、测量温度传感器电阻是否正常`
    },
    {
     value: 'bcomuflt_vfd_u11',
     name: '变频器1-1通讯故障',
     classify: '预警',
     title: `1、检查变频器是否上电
            \n2、检查通讯线路是否存在虚接情况`
    },
    {
     value: 'bcomuflt_vfd_u12',
     name: '变频器1-2通讯故障',
     classify: '预警',
     title: `1、检查变频器是否上电
            \n2、检查通讯线路是否存在虚接情况`
    },
    {
     value: 'bcomuflt_vfd_u21',
     name: '变频器2-1通讯故障',
     classify: '预警',
     title: `1、检查变频器是否上电
            \n2、检查通讯线路是否存在虚接情况`
    },
    {
     value: 'bcomuflt_vfd_u22',
     name: '变频器2-2通讯故障',
     classify: '预警',
     title: `1、检查变频器是否上电
            \n2、检查通讯线路是否存在虚接情况`
    },{
     value: 'bmcbflt_pwr_u1',
     name: '机组1供电故障',
     classify: '预警',
     title: `1、检查主回路电路是否短路、漏电流是否过大
            \n2、 检查断路器是否损坏`
    },{
     value: 'bmcbflt_pwr_u2',
     name: '机组2供电故障',
     classify: '预警',
     title: `1、检查主回路电路是否短路、漏电流是否过大
            \n2、 检查断路器是否损坏`
    }]
    const propose = [
    {
     value: 'bflt_tempover',
     name: '车厢温度超温预警',
     classify: '轻微',
     title: `1.检查2个机组的制冷系统是否异常`,
    },
    {
     value: 'bocflt_ef_u11',
     name: '通风机1-1过流故障',
     classify: '严重',
     title: `1.检查风机是否堵转
             \n2.检查电机主电路绝缘是否正常
             \n3.检查电机保护器是否损坏`
    },{
     value: 'bocflt_ef_u12',
     name: '通风机1-2过流故障',
     classify: '严重',
     title: `1.检查风机是否堵转
             \n2.检查电机主电路绝缘是否正常
             \n3.检查电机保护器是否损坏`
    },{
     value: 'bocflt_cf_u11',
     name: '冷凝风机1-1过流故障',
     classify: '一般',
     title: `1.检查风机是否堵转
             \n2.检查电机主电路绝缘是否正常
             \n3.检查电机保护器是否损坏`
    },{
     value: 'bocflt_cf_u12',
     name: '冷凝风机1-2过流故障',
     classify: '一般',
     title: `1.检查风机是否堵转
             \n2.检查电机主电路绝缘是否正常
             \n3.检查电机保护器是否损坏`
    },{
     value: 'bflt_vfd_u11',
     name: '变频器1-1故障',
     classify: '一般',
     title: `1.检查变频器运行是否正常
             \n2.检查反馈线路是否正常`
    },{
     value: 'bflt_vfd_com_u11',
     name: '变频器通讯故障U1-1',
     classify: '一般',
     title: `1.检查变频器上通讯接口针孔件连接是否牢靠，并重新紧固
             \n2.检查扩展模块上通讯接口插头连接是否牢靠，针孔件是否安装到位，并重新紧固
             \n3.检查变频器和扩展模块之间各接点位置连线是否有虚接情况，重新紧固
             \n4.更换变频器,重新连接测试\n5.更换扩展模块，重新连接测试`
    },{
     value: 'bflt_vfd_u12',
     name: '变频器1-2故障',
     classify: '一般',
     title: `1.检查变频器运行是否正常
            \n2.检查反馈线路是否正常`
    },{
     value: 'bflt_vfd_com_u12',
     name: '变频器通讯故障U1-2',
     classify: '一般',
     title: `1.检查变频器上通讯接口针孔件连接是否牢靠，并重新紧固
            \n2.检查扩展模块上通讯接口插头连接是否牢靠，针孔件是否安装到位，并重新紧固
            \n3.检查变频器和扩展模块之间各接点位置连线是否有虚接情况，重新紧固
            \n4.更换变频器,重新连接测试
            \n5.更换扩展模块，重新连接测试`
    },{
     value: 'blpflt_comp_u11',
     name: '压缩机1-1低压故障',
     classify: '一般',
     title: `1.检查制冷剂是否泄漏
            \n2.检查通风机运转是否正常
            \n3.检查低压开关是否损坏`
    },{
     value: 'bscflt_comp_u11',
     name: '压缩机1-1高压连锁故障',
     classify: '一般',
     title: `1.检查冷凝风机是否正常运转，转向是否正确
            \n2.检查高压开关、排气温度保护是否正常
            \n3.检查反馈线路是否正常`
    },{
     value: 'bscflt_vent_u11',
     name: '排气故障U1-1',
     classify: '一般',
     title: `1.检查冷凝风机是否正常运转，转向是否正确
            \n2.检查冷凝器是否有大面积异物遮挡或脏堵是否很严重
            \n3.检查空调四周散热是否顺畅`
    },{
     value: 'blpflt_comp_u12',
     name: '压缩机1-2低压故障',
     classify: '一般',
     title: `1.检查制冷剂是否泄漏
            \n2.检查通风机运转是否正常
            \n3.检查低压开关是否损坏`
    },{
     value: 'bscflt_comp_u12',
     name: '压缩机1-2高压连锁故障',
     classify: '一般',
     title: `1.检查冷凝风机运转是否正常
            \n2.检查高压开关、排气温度保护是否正常
            \n3.检查反馈线路是否正常`
    },{
     value: 'bscflt_vent_u12',
     name: '排气故障U1-2',
     classify: '一般',
     title: `1.检查冷凝风机是否正常运转，转向是否正确
            \n2.检查冷凝器是否有大面积异物遮挡或脏堵是否很严重
            \n3.检查空调四周散热是否顺畅`
    },{
     value: 'bflt_fad_u1',
     name: '新风阀U1故障',
     classify: '轻微',
     title: `1.检查风阀执行器动作、反馈指示是否正常
            \n2.检查全开反馈线路是否正常`
    },
    


    {
     value: 'bflt_fad_u11',
     name: '新风阀故障U1-1',
     classify: '轻微',
     title: `1.使用PTU控制新风阀全开，接着全关，观察新风阀是否正常动作
            \n2.新风阀动作正常的情况下，手动打开新风阀到全开位置，在本车厢触摸屏或PTU上查看新风阀的反馈信号是否导通
            \n3.如果反馈信号异常，检查反馈回路各接线位置是否虚接`
    },{
     value: 'bflt_fad_u12',
     name: '新风阀故障U1-2',
     classify: '轻微',
     title: `1.使用PTU控制新风阀全开，接着全关，观察新风阀是否正常动作
            \n2.新风阀动作正常的情况下，手动打开新风阀到全开位置，在本车厢触摸屏或PTU上查看新风阀的反馈信号是否导通
            \n3.如果反馈信号异常，检查反馈回路各接线位置是否虚接`
    },{
     value: 'bflt_rad_u1',
     name: '回风阀U1故障',
     classify: '轻微',
     title: `1.检查风阀执行器动作、反馈指示是否正常
            \n2.检查全开反馈线路是否正常`
    },
    
    
    {
     value: 'bflt_rad_u11',
     name: '回风阀故障U1-1',
     classify: '轻微',
     title: `1.使用PTU控制新风阀全开，接着全关，观察新风阀是否正常动作
            \n2.新风阀动作正常的情况下，手动打开新风阀到全开位置，在本车厢触摸屏或PTU上查看新风阀的反馈信号是否导通
            \n3.如果反馈信号异常，检查反馈回路各接线位置是否虚接`
    },{
     value: 'bflt_rad_u12',
     name: '回风阀故障U1-2',
     classify: '轻微',
     title: `1.使用PTU控制新风阀全开，接着全关，观察新风阀是否正常动作
            \n2.新风阀动作正常的情况下，手动打开新风阀到全开位置，在本车厢触摸屏或PTU上查看新风阀的反馈信号是否导通
            \n3.如果反馈信号异常，检查反馈回路各接线位置是否虚接`
    },{
     value: 'bflt_ap_u11',
     name: '空气净化器故障U1',
     classify: '轻微',
     title: `1.使用PTU控制启动空气净化器，10s后查看空气净化器的反馈信号是否导通
            \n2.如果反馈信号断开状态，检查反馈回路各接点是否有虚接情况
            \n3.更换新空气净化器测试`
    },{
     value: 'bflt_expboard_u1',
     name: '扩展模块U1故障',
     classify: '一般',
     title: `1.检查扩展模块供电是否正常
            \n2.检查扩展模块通讯线路是否存在虚接情况`
    },{
     value: 'bflt_frstemp_u1',
     name: '新风温度传感器U1故障',
     classify: '轻微',
     title: `1.检查温度传感器线路是否存在短路、虚接情况
            \n2.测量温度传感器电阻是否正常`
    },{
     value: 'bflt_rnttemp_u1',
     name: '回风温度传感器U1故障',
     classify: '轻微',
     title:`1.检查温度传感器线路是否存在短路、虚接情况
            \n2.测量温度传感器电阻是否正常`
    },{
     value: 'bflt_splytemp_u11',
     name: '送风温度传感器1-1故障',
     classify: '轻微',
     title: `1.检查温度传感器线路是否存在短路、虚接情况
            \n2.测量温度传感器电阻是否正常`
    },
    {
     value: 'bflt_splytemp_u12',
     name: '送风温度传感器1-2故障',
     classify: '轻微',
     title: `1.检查温度传感器线路是否存在短路、虚接情况
            \n2.测量温度传感器电阻是否正常`
    },{
     value: 'bflt_coiltemp_u11',
     name: '融霜温度传感器故障U1-1',
     classify: '轻微',
     title: `1.拔掉传感器插头，直接测量传感器电阻阻值，对照温度-阻值表，计算温度值和环境温度比对
            \n2.阻值无异常的情况下，检查传感器到扩展模块之间的线路是否有短路或虚接的情况`
    },{
     value: 'bflt_coiltemp_u12',
     name: '融霜温度传感器故障U1-2',
     classify: '轻微',
     title: `1.拔掉传感器插头，直接测量传感器电阻阻值，对照温度-阻值表，计算温度值和环境温度比对
            \n2.阻值无异常的情况下，检查传感器到扩展模块之间的线路是否有短路或虚接的情况`
    },{
     value: 'bflt_insptemp_u11',
     name: '吸气温度传感器故障U1-1',
     classify: '一般',
     title: `1.拔掉传感器插头，直接测量传感器电阻阻值，对照温度-阻值表，计算温度值和环境温度比对
            \n2.阻值无异常的情况下，检查传感器到扩展模块之间的线路是否有短路或虚接的情况`
    },{
     value: 'bflt_insptemp_u12',
     name: '吸气温度传感器故障U1-2',
     classify: '一般',
     title: `1.拔掉传感器插头，直接测量传感器电阻阻值，对照温度-阻值表，计算温度值和环境温度比对
            \n2.阻值无异常的情况下，检查传感器到扩展模块之间的线路是否有短路或虚接的情况`
    },{
     value: 'bflt_lowpres_u11',
     name: '低压压力传感器故障U1-1',
     classify: '一般',
     title: `1.检查吸气压力传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n2.更换新的压力传感器，然后通过PTU顺序启动制冷，查看压力变化值是否正常`
    },{
     value: 'bflt_lowpres_u12',
     name: '低压压力传感器故障U1-2',
     classify: '一般',
     title: `1.检查吸气压力传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n2.更换新的压力传感器，然后通过PTU顺序启动制冷，查看压力变化值是否正常`
    },{
     value: 'bflt_highpres_u11',
     name: '高压压力传感器故障U1-1',
     classify: '一般',
     title: `1.检查高压传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n2.更换新的压力传感器，然后通过PTU顺序启动制冷，查看压力变化值是否正常`
    },{
     value: 'bflt_highpres_u12',
     name: '高压压力传感器故障U1-2',
     classify: '一般',
     title: `1.检查高压传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n2.更换新的压力传感器，然后通过PTU顺序启动制冷，查看压力变化值是否正常`
    },{
     value: 'bflt_diffpres_u1',
     name: '压差传感器故障U1',
     classify: '轻微',
     title: `1.检查混合风滤网是否脏堵特别严重，清洗后再测试差压传感器
            \n2.检查压差传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n3.更换新的压差传感器，开启通风机，重新测试`
    },{
     value: 'bocflt_ef_u21',
     name: '通风机2-1过流故障',
     classify: '严重',
     title: `1.检查风机是否堵转；检查电机主电路绝缘是否正常
            \n2.检查电机保护器是否损坏`
    },{
     value: 'bocflt_ef_u22',
     name: '通风机2-2过流故障',
     classify: '严重',
     title: `1.检查风机是否堵转；检查电机主电路绝缘是否正常
            \n2.检查电机保护器是否损坏`
    },{
     value: 'bocflt_cf_u21',
     name: '冷凝风机2-1过流故障',
     classify: '一般',
     title: `1.检查风机是否堵转；检查电机主电路绝缘是否正常
            \n2.检查电机保护器是否损坏`
    },{
     value: 'bocflt_cf_u22',
     name: '冷凝风机过流故障U2-2',
     classify: '一般',
     title: `1.检查风机是否堵转；检查电机主电路绝缘是否正常
            \n2.检查电机保护器是否损坏`
    },{
     value: 'bflt_vfd_u21',
     name: '变频器2-1故障',
     classify: '一般',
     title: `1.检查变频器运行是否正常
            \n2.检查反馈线路是否正常`
    },{
     value: 'bflt_vfd_com_u21',
     name: '变频器通讯故障U2-1',
     classify: '一般',
     title: `1.检查变频器上通讯接口针孔件连接是否牢靠，并重新紧固
            \n2.检查扩展模块上通讯接口插头连接是否牢靠，针孔件是否安装到位，并重新紧固
            \n3.检查变频器和扩展模块之间各接点位置连线是否有虚接情况，重新紧固
            \n4.更换变频器,重新连接测试
            \n5.更换扩展模块，重新连接测试`
    },{
     value: 'bflt_vfd_u22',
     name: '变频器2-2故障',
     classify: '一般',
     title: `1.检查变频器运行是否正常
            \n2.检查反馈线路是否正常`
    },{
     value: 'bflt_vfd_com_u22',
     name: '变频器通讯故障U2-2',
     classify: '一般',
     title: `1.检查变频器上通讯接口针孔件连接是否牢靠，并重新紧固
            \n2.检查扩展模块上通讯接口插头连接是否牢靠，针孔件是否安装到位，并重新紧固
            \n3.检查变频器和扩展模块之间各接点位置连线是否有虚接情况，重新紧固
            \n4.更换变频器,重新连接测试
            \n5.更换扩展模块，重新连接测试`
    },{
     value: 'blpflt_comp_u21',
     name: '压缩机2-1低压故障',
     classify: '一般',
     title: `1.检查制冷剂是否泄漏
            \n2.检查通风机运转是否正常
            \n3.检查低压开关是否损坏`
    },{
     value: 'bscflt_comp_u21',
     name: '压缩机2-1高压连锁故障',
     classify: '一般',
     title: `1.检查冷凝风机运转是否正常
            \n2.检查高压开关、排气温度保护是否正常
            \n3.检查反馈线路是否正常`
    },{
     value: 'bscflt_vent_u21',
     name: '排气故障U2-1',
     classify: '一般',
     title: `1.检查冷凝风机是否正常运转，转向是否正确
            \n2.检查冷凝器是否有大面积异物遮挡或脏堵是否很严重
            \n3.检查空调四周散热是否顺畅`
    },{
     value: 'blpflt_comp_u22',
     name: '压缩机2-2低压故障',
     classify: '一般',
     title: `1.检查制冷剂是否泄漏
            \n2.检查通风机运转是否正常
            \n3.检查低压开关是否损坏`
    },{
     value: 'bscflt_comp_u22',
     name: '压缩机2-2高压连锁故障',
     classify: '一般',
     title: `1.检查冷凝风机运转是否正常
            \n2.检查高压开关、排气温度保护是否正常
            \n3.检查反馈线路是否正常`
    },{
     value: 'bscflt_vent_u22',
     name: '排气故障U2-2',
     classify: '一般',
     title: `1.检查冷凝风机是否正常运转，转向是否正确
            \n2.检查冷凝器是否有大面积异物遮挡或脏堵是否很严重
            \n3.检查空调四周散热是否顺畅`
    },
    {
     value: 'bflt_fad_u2',
     name: '新风阀U2故障',
     classify: '一般',
     title: `1.检查风阀执行器动作、反馈指示是否正常
            \n2.检查全开反馈线路是否正常`
    },
    
    
    {
     value: 'bflt_fad_u21',
     name: '新风阀故障U2-1',
     classify: '一般',
     title: `1.使用PTU控制新风阀全开，接着全关，观察新风阀是否正常动作
            \n2.新风阀动作正常的情况下，手动打开新风阀到全开位置，在本车厢触摸屏或PTU上查看新风阀的反馈信号是否导通
            \n3.如果反馈信号异常，检查反馈回路各接线位置是否虚接`
    },{
     value: 'bflt_fad_u22',
     name: '新风阀故障U2-2',
     classify: '一般',
     title: `1.使用PTU控制新风阀全开，接着全关，观察新风阀是否正常动作
            \n2.新风阀动作正常的情况下，手动打开新风阀到全开位置，在本车厢触摸屏或PTU上查看新风阀的反馈信号是否导通
            \n3.如果反馈信号异常，检查反馈回路各接线位置是否虚接`
    },{
     value: 'bflt_rad_u2',
     name: '回风阀故障U2-1',
     classify: '轻微',
     title: `1.检查风阀执行器动作、反馈指示是否正常
            \n2.检查全开反馈线路是否正常`
    },
    
    {
     value: 'bflt_rad_u21',
     name: '回风阀故障U2-1',
     classify: '轻微',
     title: `1.使用PTU控制新风阀全开，接着全关，观察新风阀是否正常动作
            \n2.新风阀动作正常的情况下，手动打开新风阀到全开位置，在本车厢触摸屏或PTU上查看新风阀的反馈信号是否导通
            \n3.如果反馈信号异常，检查反馈回路各接线位置是否虚接`
    },{
     value: 'bflt_rad_u22',
     name: '回风阀故障U2-2',
     classify: '轻微',
     title: `1.使用PTU控制新风阀全开，接着全关，观察新风阀是否正常动作
            \n2.新风阀动作正常的情况下，手动打开新风阀到全开位置，在本车厢触摸屏或PTU上查看新风阀的反馈信号是否导通
            \n3.如果反馈信号异常，检查反馈回路各接线位置是否虚接`
    },{
     value: 'bflt_ap_u21',
     name: '空气净化器故障U2',
     classify: '轻微',
     title: `1.使用PTU控制启动空气净化器，10s后查看空气净化器的反馈信号是否导通
            \n2.如果反馈信号断开状态，检查反馈回路各接点是否有虚接情况
            \n3.更换新空气净化器测试`
    },{
     value: 'bflt_expboard_u2',
     name: '扩展模块U2故障',
     classify: '一般',
     title: `1.检查扩展模块供电是否正常
            \n2.检查扩展模块通讯线路是否存在虚接情况`
    },{
     value: 'bflt_frstemp_u2',
     name: '新风温度传感器U2故障',
     classify: '轻微',
     title: `1.检查温度传感器线路是否存在短路、虚接情况
            \n2.测量温度传感器电阻是否正常`
    },{
     value: 'bflt_rnttemp_u2',
     name: '回风温度传感器U2故障',
     classify: '轻微',
     title: `1.检查温度传感器线路是否存在短路、虚接情况
            \n2.测量温度传感器电阻是否正常`
    },{
     value: 'bflt_splytemp_u21',
     name: '送风温度传感器2-1故障',
     classify: '轻微',
     title: `1.检查温度传感器线路是否存在短路、虚接情况
            \n2.测量温度传感器电阻是否正常`
    },{
     value: 'bflt_splytemp_u22',
     name: '送风温度传感器2-2故障',
     classify: '轻微',
     title: `1.检查温度传感器线路是否存在短路、虚接情况
            \n2.测量温度传感器电阻是否正常`
    },{
     value: 'bflt_coiltemp_u21',
     name: '融霜温度传感器故障U2-1',
     classify: '轻微',
     title: `1.拔掉传感器插头，直接测量传感器电阻阻值，对照温度-阻值表，计算温度值和环境温度比对
            \n2.阻值无异常的情况下，检查传感器到扩展模块之间的线路是否有短路或虚接的情况`
    },{
     value: 'bflt_coiltemp_u22',
     name: '融霜温度传感器故障U2-2',
     classify: '轻微',
     title: `1.拔掉传感器插头，直接测量传感器电阻阻值，对照温度-阻值表，计算温度值和环境温度比对
            \n2.阻值无异常的情况下，检查传感器到扩展模块之间的线路是否有短路或虚接的情况`
    },{
     value: 'bflt_insptemp_u21',
     name: '吸气温度传感器故障U2-1',
     classify: '一般',
     title: `1.拔掉传感器插头，直接测量传感器电阻阻值，对照温度-阻值表，计算温度值和环境温度比对
            \n2.阻值无异常的情况下，检查传感器到扩展模块之间的线路是否有短路或虚接的情况`
    },{
     value: 'bflt_insptemp_u22',
     name: '吸气温度传感器故障U2-2',
     classify: '一般',
     title: `1.拔掉传感器插头，直接测量传感器电阻阻值，对照温度-阻值表，计算温度值和环境温度比对
            \n2.阻值无异常的情况下，检查传感器到扩展模块之间的线路是否有短路或虚接的情况`
    },{
     value: 'bflt_lowpres_u21',
     name: '低压压力传感器故障U2-1',
     classify: '一般',
     title: `1.检查吸气压力传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n2.更换新的压力传感器，然后通过PTU顺序启动制冷，查看压力变化值是否正常`
    },{
     value: 'bflt_lowpres_u22',
     name: '低压压力传感器故障U2-2',
     classify: '一般',
     title: `1.检查吸气压力传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n2.更换新的压力传感器，然后通过PTU顺序启动制冷，查看压力变化值是否正常`
    },{
     value: 'bflt_highpres_u21',
     name: '高压压力传感器故障U2-1',
     classify: '一般',
     title: `1.检查高压传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n2.更换新的压力传感器，然后通过PTU顺序启动制冷，查看压力变化值是否正常`
    },{
     value: 'bflt_highpres_u22',
     name: '高压压力传感器故障U2-2',
     classify: '一般',
     title: `1.检查高压传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n2.更换新的压力传感器，然后通过PTU顺序启动制冷，查看压力变化值是否正常`
    },{
     value: 'bflt_diffpres_u2',
     name: '压差传感器故障U2',
     classify: '轻微',
     title: `1.检查混合风滤网是否脏堵特别严重，清洗后再测试差压传感器
            \n2.检查压差传感器和扩展模块之间的接线点位是否牢固，重新紧固
            \n3.更换新的压差传感器，开启通风机，重新测试`
    },{
     value: 'bflt_emergivt',
     name: '紧急逆变器故障',
     classify: '严重',
     title: `1.检查紧急逆变器运行是否正常
            \n2.检查故障反馈线路是否正常`
    },{
     value: 'bflt_powersupply_u1',
     name: '机组1供电故障',
     classify: '严重',
     title: `1.合上断路器，通过PTU顺序启动制冷，将压缩机频率调到最大65HZ,测量三相电流，各相电流应该都小于50A
            \n2.空调停机，检查总电源回路是否有短路
            \n3.使用绝缘测试仪，检测三相回路绝缘电阻大于5MΩ
            \n4.检测辅助触点反馈信号回路是否有虚接情况`
    },{
     value: 'bflt_powersupply_u2',
     name: '机组2供电故障',
     classify: '严重',
     title: `1.合上断路器，通过PTU顺序启动制冷，将压缩机频率调到最大65HZ,测量三相电流，各相电流应该都小于50A
            \n2.空调停机，检查总电源回路是否有短路
            \n3.使用绝缘测试仪，检测三相回路绝缘电阻大于5MΩ
            \n4.检测辅助触点反馈信号回路是否有虚接情况`
    },{
     value: 'bflt_exhaustval',
     name: '废排风阀故障',
     classify: '轻微',
     title: `1.使用PTU控制废排风阀全开，接着全关，观察废排风阀是否正常动作
            \n2.动作正常的情况下，手动打开风阀到全开位置，在本车厢触摸屏或PTU上查看风阀的反馈信号是否导通
            \n3.如果反馈信号异常，检查反馈回路各接线位置是否虚接`
    },{
     value: 'bflt_tcms',
     name: 'MVB 故障',
     classify: '严重',
     title: `1.检查通讯电缆接头是否松动，重新紧固
            \n2.检查通讯电缆各接点位置是否虚接
            \n3.更换新的主控制器，重新连接测试`
    },{
     value: 'bflt_exhaustfan',  //不
     name: '废排风机过流故障',
     classify: '一般',
     title: `1.首先检查电机保护器是否跳闸，如果没有跳闸那就是反馈回路断开触发的故障
            \n2.手动开合电机保护器，通过PTU或本车厢触摸屏查看输入信号状态，检查反馈回路是否正常
            \n3.跳闸引起的原因需要检查三相电路是否缺相或有虚接情况
            \n4.检查电机自身三相电阻是否平衡，用绝缘测试仪测试绝缘电阻是否大于5MΩ
            \n5.检查电机保护器主触点之间是否正常导通，电阻小于20Ω。如有异常，更换新物料`
    },{
     value: 'bflt_airmon_u1',
     name: '机组1空气监测终端通讯故障',
     classify: '轻微',
     title: `1.检查空气监测终端供电是否正常，DC24V
            \n2.检查空气监测终端通讯接口插头是否牢固，针孔件是否安装到位，并重新紧固
            \n3.检查扩展模块上通讯接口插头连接是否牢靠，针孔件是否安装到位，并重新紧固
            \n4.检查扩展模块和空气监测终端之间各接点位置连线是否有虚接情况，重新紧固
            \n5.更换空气监测终端，重新连接测试`
    },{
     value: 'bflt_airmon_u2',
     name: '机组2空气监测终端通讯故障',
     classify: '轻微',
     title: `1.检查空气监测终端供电是否正常，DC24V
            \n2.检查空气监测终端通讯接口插头是否牢固，针孔件是否安装到位，并重新紧固
            \n3.检查扩展模块上通讯接口插头连接是否牢靠，针孔件是否安装到位，并重新紧固
            \n4.检查扩展模块和空气监测终端之间各接点位置连线是否有虚接情况，重新紧固
            \n5.更换空气监测终端，重新连接测试`
    },{
     value: 'bflt_currentmon',
     name: '电流监测单元通讯故障',
     classify: '轻微',
     title: `1.检查电流监测单元供电是否正常，DC24V
            \n2.检查电流监测单元通讯接口插头是否牢固，针孔件是否安装到位，并重新紧固
            \n3.检查主控制器上通讯接口插头连接是否牢靠，针孔件是否安装到位，并重新紧固
            \n4.检查主控制器和电流监测单元之间各接点位置连线是否有虚接情况，重新紧固
            \n5.更换电流监测单元，重新连接测试`
    },
    {
     value: 'bFlt_VehTemp',
     name: '车厢温度传感器1故障',
     classify: '轻微',
     title: `1.检查温度传感器线路是否存在短路、虚接情况
             \n2.测量温度传感器电阻是否正常`
    },
    // 旧
    ,{
     value: 'bflt_vehtemp_u2',
     name: '车厢温度传感器2故障',
     classify: '轻微',
     title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
    },{
     value: 'bflt_vehtemp_u1',
     name: '车厢温度传感器1故障',
     classify: '轻微',
     title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
    },
]
// const propose = [
//     {
//      value: 'dvc_bocflt_ef_u11',
//      name: '通风机1-1过流故障',
//      title: '检查风机是否堵转；检查电机主电路绝缘是否正常；检查电机保护器是否损坏'
//     },{
//      value: 'dvc_bocflt_ef_u12',
//      name: '通风机1-2过流故障',
//      title: '检查风机是否堵转；检查电机主电路绝缘是否正常；检查电机保护器是否损坏'
//     },{
//      value: 'dvc_bocflt_cf_u11',
//      name: '冷凝风机1-1过流故障',
//      title: '检查风机是否堵转；检查电机主电路绝缘是否正常；检查电机保护器是否损坏'
//     },{
//      value: 'dvc_bocflt_cf_u12',
//      name: '冷凝风机1-2过流故障',
//      title: '检查风机是否堵转；检查电机主电路绝缘是否正常；检查电机保护器是否损坏'
//     },{
//      value: 'dvc_bflt_vfd_u11',
//      name: '变频器1-1故障',
//      title: '检查变频器运行是否正常；检查反馈线路是否正常'
//     },{
//      value: 'dvc_blpflt_comp_u11',
//      name: '压缩机1-1低压故障',
//      title: '检查制冷剂是否泄漏；检查通风机运转是否正常；检查低压开关是否损坏'
//     },{
//      value: 'dvc_bscflt_comp_u11',
//      name: '压缩机1-1高压连锁故障',
//      title: '检查冷凝风机运转是否正常；检查高压开关、排气温度保护是否正常；检查反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_vfd_u12',
//      name: '变频器1-2故障',
//      title: '检查变频器运行是否正常；检查反馈线路是否正常'
//     },{
//      value: 'dvc_blpflt_comp_u12',
//      name: '压缩机1-2低压故障',
//      title: '检查制冷剂是否泄漏；检查通风机运转是否正常；检查低压开关是否损坏'
//     },{
//      value: 'dvc_bscflt_comp_u12',
//      name: '压缩机1-2高压连锁故障',
//      title: '检查冷凝风机运转是否正常；检查高压开关、排气温度保护是否正常；检查反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_eev_u11',
//      name: '电子膨胀阀1-1故障',
//      title: '检查电子膨胀阀控制器显示参数是否正常；检查故障反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_eev_u12',
//      name: '电子膨胀阀1-2故障',
//      title: '检查电子膨胀阀控制器显示参数是否正常；检查故障反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_fad_u1',
//      name: '新风阀U1故障',
//      title: '检查风阀执行器动作、反馈指示是否正常；检查全开反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_rad_u1',
//      name: '回风阀U1故障',
//      title: '检查风阀执行器动作、反馈指示是否正常；检查全开反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_airclean_u1',
//      name: '空气净化U1故障',
//      title: '检查空气净化器运行是否正常；检查故障反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_expboard_u1',
//      name: '扩展模块U1故障',
//      title: '检查扩展模块供电是否正常；检查扩展模块通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bflt_frstemp_u1',
//      name: '新风温度传感器U1故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_splytemp_u11',
//      name: '送风温度传感器1-1故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_splytemp_u12',
//      name: '送风温度传感器1-2故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_rnttemp_u1',
//      name: '回风温度传感器U1故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_dfstemp_u11',
//      name: '融霜温度传感器1-1故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_dfstemp_u12',
//      name: '融霜温度传感器1-2故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bocflt_ef_u21',
//      name: '通风机2-1过流故障',
//      title: '检查风机是否堵转；检查电机主电路绝缘是否正常；检查电机保护器是否损坏'
//     },{
//      value: 'dvc_bocflt_ef_u22',
//      name: '通风机2-2过流故障',
//      title: '检查风机是否堵转；检查电机主电路绝缘是否正常；检查电机保护器是否损坏'
//     },{
//      value: 'dvc_bocflt_cf_u21',
//      name: '冷凝风机2-1过流故障',
//      title: '检查风机是否堵转；检查电机主电路绝缘是否正常；检查电机保护器是否损坏'
//     },{
//      value: 'dvc_bocflt_cf_u22',
//      name: '冷凝风机2-2过流故障',
//      title: '检查风机是否堵转；检查电机主电路绝缘是否正常；检查电机保护器是否损坏'
//     },{
//      value: 'dvc_bflt_vfd_u21',
//      name: '变频器2-1故障',
//      title: '检查变频器运行是否正常；检查反馈线路是否正常'
//     },{
//      value: 'dvc_blpflt_comp_u21',
//      name: '压缩机2-1低压故障',
//      title: '检查制冷剂是否泄漏；检查通风机运转是否正常；检查低压开关是否损坏'
//     },{
//      value: 'dvc_bscflt_comp_u21',
//      name: '压缩机2-1高压连锁故障',
//      title: '检查冷凝风机运转是否正常；检查高压开关、排气温度保护是否正常；检查反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_vfd_u22',
//      name: '变频器2-2故障',
//      title: '检查变频器运行是否正常；检查反馈线路是否正常'
//     },{
//      value: 'dvc_blpflt_comp_u22',
//      name: '压缩机2-2低压故障',
//      title: '检查制冷剂是否泄漏；检查通风机运转是否正常；检查低压开关是否损坏'
//     },{
//      value: 'dvc_bscflt_comp_u22',
//      name: '压缩机2-2高压连锁故障',
//      title: '检查冷凝风机运转是否正常；检查高压开关、排气温度保护是否正常；检查反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_eev_u21',
//      name: '电子膨胀阀2-1故障',
//      title: '检查电子膨胀阀控制器显示参数是否正常；检查故障反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_eev_u22',
//      name: '电子膨胀阀2-2故障',
//      title: '检查电子膨胀阀控制器显示参数是否正常；检查故障反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_fad_u2',
//      name: '新风阀U2故障',
//      title: '检查风阀执行器动作、反馈指示是否正常；检查全开反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_rad_u2',
//      name: '回风阀U2故障',
//      title: '检查风阀执行器动作、反馈指示是否正常；检查全开反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_airclean_u2',
//      name: '空气净化U2故障',
//      title: '检查空气净化器运行是否正常；检查故障反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_expboard_u2',
//      name: '扩展模块U2故障',
//      title: '检查扩展模块供电是否正常；检查扩展模块通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bflt_frstemp_u2',
//      name: '新风温度传感器U2故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_splytemp_u21',
//      name: '送风温度传感器2-1故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_splytemp_u22',
//      name: '送风温度传感器2-2故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_rnttemp_u2',
//      name: '回风温度传感器U2故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_dfstemp_u21',
//      name: '融霜温度传感器2-1故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_dfstemp_u22',
//      name: '融霜温度传感器2-2故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_vehtemp',
//      name: '车厢温度传感器1故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_seattemp',
//      name: '车厢温度传感器2故障',
//      title: '检查温度传感器线路是否存在短路、虚接情况；测量温度传感器电阻是否正常'
//     },{
//      value: 'dvc_bflt_emergivt',
//      name: '紧急逆变器故障',
//      title: '检查紧急逆变器运行是否正常；检查故障反馈线路是否正常'
//     },{
//      value: 'dvc_bflt_mvbbus',
//      name: 'MVB 通讯故障',
//      title: '检查控制器供电是否正常；检查MVB连接器是否松动'
//     },{
//      value: 'dvc_bcomuflt_vfd_u11',
//      name: '变频器1-1通讯故障',
//      title: '检查变频器是否上电；检查通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bcomuflt_vfd_u12',
//      name: '变频器1-2通讯故障',
//      title: '检查变频器是否上电；检查通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bcomuflt_vfd_u21',
//      name: '变频器2-1通讯故障',
//      title: '检查变频器是否上电；检查通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bcomuflt_vfd_u22',
//      name: '变频器2-2通讯故障',
//      title: '检查变频器是否上电；检查通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bcomuflt_eev_u11',
//      name: '电子膨胀阀1-1通讯故障',
//      title: '检查电子膨胀阀控制器供电是否正常；检查通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bcomuflt_eev_u12',
//      name: '电子膨胀阀1-2通讯故障',
//      title: '检查电子膨胀阀控制器供电是否正常；检查通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bcomuflt_eev_u21',
//      name: '电子膨胀阀2-1通讯故障',
//      title: '检查电子膨胀阀控制器供电是否正常；检查通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bcomuflt_eev_u22',
//      name: '电子膨胀阀2-2通讯故障',
//      title: '检查电子膨胀阀控制器供电是否正常；检查通讯线路是否存在虚接情况'
//     },{
//      value: 'dvc_bmcbflt_pwr_u1',
//      name: '机组1供电故障',
//      title: '检查主回路电路是否短路、漏电流是否过大；检查断路器是否损坏'
//     },{
//      value: 'dvc_bmcbflt_pwr_u2',
//      name: '机组2供电故障',
//      title: '检查主回路电路是否短路、漏电流是否过大；检查断路器是否损坏'
//     }
// ]
function getLocalTime(i,data) { 
    if (typeof i !== 'number') return;
 
   var d = new Date(data); 
//   2023-02-20T13:50:20.954Z
//   2023-02-08T00:00:00+00:00
    //得到1970年一月一日到现在的秒数 
    var len = d.getTime();
    
    //本地时间与GMT时间的时间偏移差(注意：GMT这是UTC的民间名称。GMT=UTC）
    var offset = d.getTimezoneOffset() * 60000;
 
    //得到现在的格林尼治时间
    var utcTime = len + offset;
 
    return new Date(utcTime + 3600000 * i);
}
// 移除过时的 newDate 格式化函数，统一使用 /src/utils/time.ts

  function runningState(items){
     return  items ==1?'运行':'停机'
  }
  function runningKeys(name){
    const data = ['ef_u1','ef_u2','cf_u1','cf_u2','fad_u1','fad_u2','comp_u21','comp_u22']
    if(data.indexOf(name)!==-1){

    }
  }
  
  function setName (obj,i){
    

    switch(i){
        case 'ef_u1':
        obj.equipmentName='通风机';
        break;

        case 'cf_u1':
        obj.equipmentName='冷凝风机';
        break;

        case 'fad_u1':
        obj.equipmentName='新风阀';
        break;

         case 'comp_u21':
        obj.equipmentName='回风阀';
        break;
        default:
    }
  }
  function containsNumber(str) {
	    var reg=/\d/;
	    return reg.test(str);
	}
    function hasLetter(str) {
    for (var i in str) {
        var asc = str.charCodeAt(i);
        if ((asc >= 65 && asc <= 90 || asc >= 97 && asc <= 122)) {
            return true;
        }
    }
    return false;
}
function extractNumber(str) {
    if (str === null || str === undefined) return null;
    const s = String(str);
    // 正则表达式用于匹配数字
    const regex = /(\d+)/;
    const match = s.match(regex);

    // 如果匹配成功，返回匹配到的第一个数字
    if (match) {
        return String(parseInt(match[1], 10));
    }

    // 如果没有匹配到数字，返回 null 或者可以根据需求抛出错误
    return null;
}
function getCurrentDate() {
    const now = new Date();

    // 获取完整的年份 (4位, 1970-?)
    const year = now.getFullYear();

    // 获取当前月份 (0-11, 0代表1月)
    const month = now.getMonth() + 1;

    // 获取当前日 (1-31)
    const day = now.getDate();

    // 格式化月份和日期，确保始终为两位数
    const formattedMonth = month > 9 ? month : '0' + month;
    const formattedDay = day > 9 ? day : '0' + day;

    return `${year}-${formattedMonth}-${formattedDay}`;
}
const startTime = ref(getCurrentDate()+' 00:00:00')
const endTime = ref(getCurrentDate()+' 23:59:59')
function parentMethod(data){
console.log(data,888);
startTime.value = data[0] + ' 00:00:00'
endTime.value = data[1]+' 23:59:59'
listData()
}
async function listData (){
    // AlarmInfoData.value = []
    AlarmInfoData.value = []
    for (let i=1;i<7;i++){
        const params = {
            state: filterForm.trainNo + String(i).padStart(2, '0'),
            startTime: startTime.value,
            endTime: endTime.value
        }
    const res = await getAlarmInformation(params)
    const proposeAdvice = propose.concat(proposeWarn)
    if(res && res.code === 200 && res.data && res.data.alarm_timeline){
        res.data.alarm_timeline.forEach((item)=>{
            item.state = item.state.slice(0, -1)
            item.start_time = formatTime(item.start_time)
            item.end_time = formatTime(item.end_time)
            // console.log(item.propose,extractNumber(item.propose),extractNumber(item.propose).length);
            if(!extractNumber(item.propose)){
                item.unit_no = '-'
            } else if(extractNumber(item.propose) && extractNumber(item.propose).length == 2){
                if(extractNumber(item.propose).slice(-2,-1) == '1'){
                    item.unit_no = '1机组'
                } else {
                    item.unit_no = '2机组'
                }
            } else if(extractNumber(item.propose) && extractNumber(item.propose).length == 1){
                if(extractNumber(item.propose) == '1'){
                    item.unit_no = '1机组'
                } else {
                    item.unit_no = '2机组'
                }
            }
            proposeAdvice.forEach((itemValue)=>{
                if(itemValue.value == item.propose){
                    item.precautions = itemValue.title
                    item.name = itemValue.name
                    item.classify = itemValue.classify
                }
            })
            AlarmInfoData.value.push(item)
        })
    }
}
}

// 获取指定列车下的车厢 (现为静态，此函数可保留用于扩展或删除)
const fetchCarriages = async (trainId) => {
    // 静态模式下不需要从后端拉取列表并覆盖
}

const handleTrainChange = (val) => {
    filterForm.carriageNo = '1' // 切换车号时默认到1车厢
}

const handleQuery = () => {
    // 只有点击查询或点击车厢图形时才执行此处
    currentTrainNo.value = String(filterForm.trainNo)
    currentCarriageNo.value = String(filterForm.carriageNo)
    
    // 手动点击查询时更新 URL（trainNo 补零到 5 位）
    router.push({
        query: {
            ...route.query,
            trainNo: String(currentTrainNo.value).padStart(5, '0'),
            trainCoach: currentCarriageNo.value
        }
    })
    getTrainApi()
}

const handleCarSelect = (carriageId) => {
    filterForm.carriageNo = carriageId
    handleQuery()
}

const gotoPath = (name) => {
    router.push({
        name,
        query: { trainNo: String(currentTrainNo.value).padStart(5, '0'), trainCoach: currentCarriageNo.value }
    })
}

async function getTrainApi() {
    if (!currentTrainNo.value) return;
    if (isOffline.value) {
        ActualWarningData.value = []
        StateWarningData.value = []
        return
    }

    const tNo = [currentTrainNo.value]
    const proposeAdvice = propose.concat(proposeWarn)

    console.log('[TrainDetail] Fetching data for:', currentTrainNo.value)

    try {
        // 1. 同时请求报警和预警，提升效率且互不干扰
        const [alarmRes, alertRes] = await Promise.all([
            getRealtimeAlarm({trainNo: tNo}).catch(e => ({vw_train_alarm_info: []})),
            getStatusAlert(currentTrainNo.value).catch(e => ({}))
        ]);

        // --- 处理实时报警 (ActualWarning) ---
        const tempActual = []
        const seenActual = new Set()
        if (alarmRes.vw_train_alarm_info) {
            alarmRes.vw_train_alarm_info.forEach((item) => {
                Object.keys(item).forEach((key) => {
                    if(item[key] > 0 && (item[key +'_name'] || item.name)){
                        const carNo = extractNumber(item.carriage_no) ? extractNumber(item.carriage_no).slice(-1) : item.carriage_no
                        const uniqueKey = `${key}_${carNo}`
                        
                        if (!seenActual.has(uniqueKey)) {
                            const newItem = {
                                name: item[key + '_name'] || item.name || '未知故障',
                                code: key,
                                alarm_time: formatTime(item.alarm_time),
                                carriage_no: carNo,
                                train_no: item.train_no,
                                precautions: ''
                            }
                            const cleanCode = newItem.code.replace('dvc_', '')
                            proposeAdvice.forEach((p) => {
                                if(p.value == newItem.code || p.value == cleanCode){
                                    newItem.precautions = p.title
                                    if(!newItem.name || newItem.name === '未知故障') newItem.name = p.name
                                }
                            })
                            tempActual.push(newItem)
                            seenActual.add(uniqueKey)
                        }
                    }
                })
            })
        }
        ActualWarningData.value = tempActual

        // --- 处理实时预警 (StateWarning) ---
        const tempState = []
        const seenState = new Set()
        Object.entries(alertRes).forEach(([key, itemArr]) => {
            if(key.startsWith('vw_') || key.includes('count')) return;
            
            if(Array.isArray(itemArr)){
                itemArr.forEach((items)=>{
                    const carNo = extractNumber(items.carriage_no) ? extractNumber(items.carriage_no).slice(-1) : items.carriage_no
                    const uniqueKey = `${key}_${carNo}`

                    if (!seenState.has(uniqueKey)) {
                        const newItem = {
                            ...items,
                            code: key,
                            warning_time: formatTime(items.warning_time),
                            name: items.name || items.alert_name || items.warning_info || '未知预警',
                            carriage_no: carNo,
                            precautions: items.strategy || ''
                        }
                        tempState.push(newItem)
                        seenState.add(uniqueKey)
                    }
                })
            }
        });
        StateWarningData.value = tempState
        console.log('[TrainDetail] Refresh Success:', new Date().toLocaleTimeString())
    } catch (err) {
        console.error('[TrainDetail] Refresh Error:', err)
    }
}

watch(() => route.query, (newQuery) => {
    // 仅当 URL 参数变化时（如点击查询按钮后或地址栏回车），同步回选择框
    if (newQuery.trainNo) {
        const normalizedNo = String(parseInt(String(newQuery.trainNo)) || 7001)
        filterForm.trainNo = normalizedNo
        currentTrainNo.value = normalizedNo
    }
    if (newQuery.trainCoach) {
        filterForm.carriageNo = String(newQuery.trainCoach)
        currentCarriageNo.value = String(newQuery.trainCoach)
    }
    getTrainApi()
}, { deep: true, immediate: true })

import { MONITOR_CONFIG } from '@/config/monitorConfig.js'

onMounted(() => {
    if (!route.query.trainNo) {
        filterForm.trainNo = '7001'
        currentTrainNo.value = '7001'
    }
    if (!route.query.trainCoach) {
        filterForm.carriageNo = '1'
        currentCarriageNo.value = '1'
    }
    getTrainApi()
    checkOnlineStatus()
    offlineCheckTimer = setInterval(checkOnlineStatus, 30_000)
})

onUnmounted(() => {
    if (offlineCheckTimer) clearInterval(offlineCheckTimer)
})

let timer = setInterval(() => {
    getTrainApi()
}, MONITOR_CONFIG.refreshInterval)

onUnmounted(() => {
    clearInterval(timer)
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

    .header-left {
        display: flex;
        gap: 15px;
    }



    .header-right {
        .filter-form {
            display: flex;
            align-items: center;
            :deep(.el-form-item) {
                margin-bottom: 0 !important;
                margin-right: 12px !important;
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
    padding: 0 15px 10px 15px; // 移除顶部 10px 填充，消除粘性闪跳
    display: flex;
    flex-direction: column;
    gap: 12px;
}

.section-box {
    background: #141a2e;
    border-radius: 8px;
    border: 1px solid #262e45;
    padding: 10px;
    box-shadow: 0 6px 15px rgba(0,0,0,0.3);
    overflow: hidden;
    
    &.sticky-section {
        position: sticky;
        top: 50px; // 紧贴 height: 50px 的 header
        z-index: 999;
        margin-bottom: 0px; // 消除底部间距，增强固定感
        background: #0a0f1d; /* 与背景色对齐，防止半透明遮挡 */
        border-top: none; // 移除顶部边框，与 header 融合
        border-radius: 0 0 8px 8px; // 仅保留底部圆角
        box-shadow: 0 4px 10px rgba(0,0,0,0.4);
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
    display: flex;
    align-items: center;
    justify-content: center;
    
    &:hover, &:focus {
        background: rgba(33, 134, 207, 0.2) !important;
        border-color: #409eff !important;
        color: #ffffff !important;
        box-shadow: 0 0 10px rgba(33, 134, 207, 0.3) !important;
    }

    &:active {
        background: rgba(33, 134, 207, 0.4) !important;
    }

    /* 图标样式微调 */
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
}
</style>

<!-- 全局样式，用于强制覆盖 Element Plus 样式 -->
<style lang="scss">
/* 极高优先级全局覆盖，确保暗色主题生效 */
.filter-form {
    /* 1. 强制控制内部变量 */
    --el-fill-color-blank: #0a0f1d !important;
    --el-border-color: #2186cf !important;
    --el-text-color-regular: #ffffff !important;
    --el-border-color-hover: #409eff !important;

    .el-select {
        width: 100px !important;
        margin: 0 !important;
        
        .el-input__wrapper {
            background-color: #0a0f1d !important;
            /* 使用双重 box-shadow 确保边框和光晕共存 */
            box-shadow: 0 0 0 1px #2186cf inset !important;
            border-radius: 4px !important;
            padding: 0 12px !important;
            height: 32px !important;
            
            &:hover {
                box-shadow: 0 0 0 1px #409eff inset, 0 0 10px rgba(33, 134, 207, 0.4) !important;
            }
            
            &.is-focus {
                box-shadow: 0 0 0 1px #409eff inset, 0 0 15px rgba(33, 134, 207, 0.6) !important;
            }
            
            .el-input__inner {
                color: #ffffff !important;
                font-size: 14px !important;
                background: none !important;
                border: none !important;
            }
            
            .el-select__caret {
                color: #2186cf !important;
                font-weight: bold;
            }
        }
    }
}

/* 下拉菜单弹出层 (Popper) */
.el-select__popper.el-popper {
    background-color: #141a2e !important;
    border: 1px solid #2186cf !important;
    box-shadow: 0 4px 12px rgba(0,0,0,0.5) !important;
    
    .el-select-dropdown__item {
        color: #d1d9e7 !important;
        background-color: transparent !important;
        &:hover, &.hover {
            background-color: rgba(33, 134, 207, 0.2) !important;
            color: #ffffff !important;
        }
        &.selected {
            color: #2186cf !important;
            font-weight: bold;
            background-color: rgba(33, 134, 207, 0.1) !important;
        }
    }
    
    .el-popper__arrow::before {
        background-color: #141a2e !important;
        border-top: 1px solid #2186cf !important;
        border-left: 1px solid #2186cf !important;
    }
}

/* 指导建议弹窗自定义样式 */
.el-popover.advice-popover {
    background: #0c1124 !important;
    border: 1px solid #2186cf !important;
    color: #ffffff !important;
    font-size: 13px !important;
    padding: 15px !important;
    box-shadow: 0 4px 15px rgba(0, 0, 0, 0.6) !important;
    border-radius: 6px !important;

    .el-popover__title {
        color: #2186cf !important;
        font-size: 14px !important;
        font-weight: bold !important;
        margin-bottom: 10px !important;
        border-bottom: 1px solid rgba(33, 134, 207, 0.3) !important;
        padding-bottom: 8px !important;
    }

    // 保持内容换行
    white-space: pre-line !important;
    line-height: 1.6 !important;

    .el-popper__arrow::before {
        background: #0c1124 !important;
        border: 1px solid #2186cf !important;
    }
}
</style>


