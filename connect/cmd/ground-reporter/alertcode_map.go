package main

import (
	"fmt"
	"strconv"
	"strings"
)

// oldSeqToNewCode maps the old internal seq (1-26) to the new platform HVAC code
// as defined in alertcode_v2.xlsx (HVAC101-HVAC115). Multiple old seqs collapse to
// the same platform code because the new scheme uses 15 codes covering both machines.
var oldSeqToNewCode = map[int]string{
	1: "HVAC101", 2: "HVAC102", 3: "HVAC101", 4: "HVAC102",
	5: "HVAC103", 6: "HVAC103",
	7: "HVAC104", 8: "HVAC105", 9: "HVAC106",
	10: "HVAC107", 11: "HVAC107",
	12: "HVAC108", 13: "HVAC109", 14: "HVAC108", 15: "HVAC109",
	16: "HVAC110", 17: "HVAC111", 18: "HVAC110", 19: "HVAC111",
	20: "HVAC112",
	21: "HVAC113", 22: "HVAC114", 23: "HVAC113", 24: "HVAC114",
	25: "HVAC115", 26: "HVAC115", 76: "HVAC116",
}

// platformHvacCode converts an internal HVAC code (HVAC{carriage*100+seq}) to
// the platform-facing code (HVAC101-HVAC115) per alertcode_v2.xlsx.
// Non-HVAC codes are returned unchanged.
func platformHvacCode(code string) string {
	upper := strings.ToUpper(code)
	if !strings.HasPrefix(upper, "HVAC") {
		return code
	}
	n, err := strconv.Atoi(code[4:])
	if err != nil {
		return code
	}
	seq := n % 100
	if seq == 0 {
		return code
	}
	if newCode, ok := oldSeqToNewCode[seq]; ok {
		return newCode
	}
	// fallback: strip carriage multiplier
	return fmt.Sprintf("HVAC%d", 100+seq)
}

// predictSeqToLocation maps internal predict seq (1-26) to location string.
// Source: alertcode_v2.xlsx (PHM v2). Used for per-carriage prediction codes
// HVAC{carriage*100+seq}, looked up before seq remapping to platform code.
var predictSeqToLocation = map[int]string{
	1: "空调机组1", 2: "空调机组1",
	3: "空调机组2", 4: "空调机组2",
	5: "空调机组1", 6: "空调机组2",
	7: "空调机组1&2", 8: "空调机组1&2",
	9:  "空调机组1&2", // 车厢超温 — PHM v2 HVAC106 location
	10: "空调机组1", 11: "空调机组2",
	12: "空调机组1", 13: "空调机组1",
	14: "空调机组2", 15: "空调机组2",
	16: "空调机组1", 17: "空调机组1",
	18: "空调机组2", 19: "空调机组2",
	20: "废排单元",
	21: "空调机组1", 22: "空调机组1",
	23: "空调机组2", 24: "空调机组2",
	25: "空调机组1", 26: "空调机组2",
	76: "空调机组1&2",
}

// alarmSeqToLocation maps alarm seq (27-75) to location string.
// Source: alertcode_v2.xlsx (PHM v2), HVAC127-HVAC175.
var alarmSeqToLocation = map[int]string{
	27: "通风机1-1过流", 28: "通风机1-2过流",
	29: "冷凝风机1-1过流", 30: "冷凝风机1-2过流",
	31: "变频器1-1", 32: "压缩机1-1低压", 33: "压缩机1-1高压连锁",
	34: "变频器1-2", 35: "压缩机1-2低压", 36: "压缩机1-2高压连锁",
	37: "新风阀U1", 38: "回风阀U1", 39: "空气净化U1",
	40: "扩展模块U1", 41: "新风温度传感器U1",
	42: "送风温度传感器1-1", 43: "送风温度传感器1-2",
	44: "回风温度传感器U1",
	45: "融霜温度传感器1-1", 46: "融霜温度传感器1-2",
	47: "通风机2-1过流", 48: "通风机2-2过流",
	49: "冷凝风机2-1过流", 50: "冷凝风机2-2过流",
	51: "变频器2-1", 52: "压缩机2-1低压", 53: "压缩机2-1高压连锁",
	54: "变频器2-2", 55: "压缩机2-2低压", 56: "压缩机2-2高压连锁",
	57: "新风阀U2", 58: "回风阀U2", 59: "空气净化U2",
	60: "扩展模块U2", 61: "新风温度传感器U2",
	62: "送风温度传感器2-1", 63: "送风温度传感器2-2",
	64: "回风温度传感器U2",
	65: "融霜温度传感器2-1", 66: "融霜温度传感器2-2",
	67: "车厢温度传感器1", 68: "车厢温度传感器2",
	69: "紧急逆变器",
	70: "变频器1-1通讯", 71: "变频器1-2通讯",
	72: "变频器2-1通讯", 73: "变频器2-2通讯",
	74: "机组1供电", 75: "机组2供电",
}

// alarmSeqToFaultName maps alarm seq (27-75) to fault_name string.
// Source: alertcode_v2.xlsx (PHM v2), HVAC127-HVAC175.
var alarmSeqToFaultName = map[int]string{
	27: "通风机1-1过流故障", 28: "通风机1-2过流故障",
	29: "冷凝风机1-1过流故障", 30: "冷凝风机1-2过流故障",
	31: "变频器1-1故障", 32: "压缩机1-1低压故障", 33: "压缩机1-1高压连锁故障",
	34: "变频器1-2故障", 35: "压缩机1-2低压故障", 36: "压缩机1-2高压连锁故障",
	37: "新风阀U1故障", 38: "回风阀U1故障", 39: "空气净化U1故障",
	40: "扩展模块U1故障", 41: "新风温度传感器U1故障",
	42: "送风温度传感器1-1故障", 43: "送风温度传感器1-2故障",
	44: "回风温度传感器U1故障",
	45: "融霜温度传感器1-1故障", 46: "融霜温度传感器1-2故障",
	47: "通风机2-1过流故障", 48: "通风机2-2过流故障",
	49: "冷凝风机2-1过流故障", 50: "冷凝风机2-2过流故障",
	51: "变频器2-1故障", 52: "压缩机2-1低压故障", 53: "压缩机2-1高压连锁故障",
	54: "变频器2-2故障", 55: "压缩机2-2低压故障", 56: "压缩机2-2高压连锁故障",
	57: "新风阀U2故障", 58: "回风阀U2故障", 59: "空气净化U2故障",
	60: "扩展模块U2故障", 61: "新风温度传感器U2故障",
	62: "送风温度传感器2-1故障", 63: "送风温度传感器2-2故障",
	64: "回风温度传感器U2故障",
	65: "融霜温度传感器2-1故障", 66: "融霜温度传感器2-2故障",
	67: "车厢温度传感器1故障", 68: "车厢温度传感器2故障",
	69: "紧急逆变器故障",
	70: "变频器1-1通讯故障", 71: "变频器1-2通讯故障",
	72: "变频器2-1通讯故障", 73: "变频器2-2通讯故障",
	74: "机组1供电故障", 75: "机组2供电故障",
}

// alertcodeLocationMap retains full entries for all carriages for predict codes (seq 1-26).
// For alarm codes (seq 27-75), use alarmSeqToLocation via locationByCode.
// Source: alertcode_v2.xlsx (PHM v2).
var alertcodeLocationMap = map[string]string{
	"HVAC101": "空调机组1",
	"HVAC102": "空调机组1",
	"HVAC103": "空调机组2",
	"HVAC104": "空调机组2",
	"HVAC105": "空调机组1",
	"HVAC106": "空调机组2",
	"HVAC107": "空调机组1&2",
	"HVAC108": "空调机组1&2",
	"HVAC109": "空调机组1&2",
	"HVAC110": "空调机组1",
	"HVAC111": "空调机组2",
	"HVAC112": "空调机组1",
	"HVAC113": "空调机组1",
	"HVAC114": "空调机组2",
	"HVAC115": "空调机组2",
	"HVAC116": "空调机组1",
	"HVAC117": "空调机组1",
	"HVAC118": "空调机组2",
	"HVAC119": "空调机组2",
	"HVAC120": "废排单元",
	"HVAC121": "空调机组1",
	"HVAC122": "空调机组1",
	"HVAC123": "空调机组2",
	"HVAC124": "空调机组2",
	"HVAC125": "空调机组1",
	"HVAC126": "空调机组2",
	"HVAC176": "空调机组1&2",
	"HVAC201": "空调机组1",
	"HVAC202": "空调机组1",
	"HVAC203": "空调机组2",
	"HVAC204": "空调机组2",
	"HVAC205": "空调机组1",
	"HVAC206": "空调机组2",
	"HVAC207": "空调机组1&2",
	"HVAC208": "空调机组1&2",
	"HVAC209": "空调机组1&2",
	"HVAC210": "空调机组1",
	"HVAC211": "空调机组2",
	"HVAC212": "空调机组1",
	"HVAC213": "空调机组1",
	"HVAC214": "空调机组2",
	"HVAC215": "空调机组2",
	"HVAC216": "空调机组1",
	"HVAC217": "空调机组1",
	"HVAC218": "空调机组2",
	"HVAC219": "空调机组2",
	"HVAC220": "废排单元",
	"HVAC221": "空调机组1",
	"HVAC222": "空调机组1",
	"HVAC223": "空调机组2",
	"HVAC224": "空调机组2",
	"HVAC225": "空调机组1",
	"HVAC226": "空调机组2",
	"HVAC276": "空调机组1&2",
	"HVAC301": "空调机组1",
	"HVAC302": "空调机组1",
	"HVAC303": "空调机组2",
	"HVAC304": "空调机组2",
	"HVAC305": "空调机组1",
	"HVAC306": "空调机组2",
	"HVAC307": "空调机组1&2",
	"HVAC308": "空调机组1&2",
	"HVAC309": "空调机组1&2",
	"HVAC310": "空调机组1",
	"HVAC311": "空调机组2",
	"HVAC312": "空调机组1",
	"HVAC313": "空调机组1",
	"HVAC314": "空调机组2",
	"HVAC315": "空调机组2",
	"HVAC316": "空调机组1",
	"HVAC317": "空调机组1",
	"HVAC318": "空调机组2",
	"HVAC319": "空调机组2",
	"HVAC320": "废排单元",
	"HVAC321": "空调机组1",
	"HVAC322": "空调机组1",
	"HVAC323": "空调机组2",
	"HVAC324": "空调机组2",
	"HVAC325": "空调机组1",
	"HVAC326": "空调机组2",
	"HVAC376": "空调机组1&2",
	"HVAC401": "空调机组1",
	"HVAC402": "空调机组1",
	"HVAC403": "空调机组2",
	"HVAC404": "空调机组2",
	"HVAC405": "空调机组1",
	"HVAC406": "空调机组2",
	"HVAC407": "空调机组1&2",
	"HVAC408": "空调机组1&2",
	"HVAC409": "空调机组1&2",
	"HVAC410": "空调机组1",
	"HVAC411": "空调机组2",
	"HVAC412": "空调机组1",
	"HVAC413": "空调机组1",
	"HVAC414": "空调机组2",
	"HVAC415": "空调机组2",
	"HVAC416": "空调机组1",
	"HVAC417": "空调机组1",
	"HVAC418": "空调机组2",
	"HVAC419": "空调机组2",
	"HVAC420": "废排单元",
	"HVAC421": "空调机组1",
	"HVAC422": "空调机组1",
	"HVAC423": "空调机组2",
	"HVAC424": "空调机组2",
	"HVAC425": "空调机组1",
	"HVAC426": "空调机组2",
	"HVAC476": "空调机组1&2",
	"HVAC501": "空调机组1",
	"HVAC502": "空调机组1",
	"HVAC503": "空调机组2",
	"HVAC504": "空调机组2",
	"HVAC505": "空调机组1",
	"HVAC506": "空调机组2",
	"HVAC507": "空调机组1&2",
	"HVAC508": "空调机组1&2",
	"HVAC509": "空调机组1&2",
	"HVAC510": "空调机组1",
	"HVAC511": "空调机组2",
	"HVAC512": "空调机组1",
	"HVAC513": "空调机组1",
	"HVAC514": "空调机组2",
	"HVAC515": "空调机组2",
	"HVAC516": "空调机组1",
	"HVAC517": "空调机组1",
	"HVAC518": "空调机组2",
	"HVAC519": "空调机组2",
	"HVAC520": "废排单元",
	"HVAC521": "空调机组1",
	"HVAC522": "空调机组1",
	"HVAC523": "空调机组2",
	"HVAC524": "空调机组2",
	"HVAC525": "空调机组1",
	"HVAC526": "空调机组2",
	"HVAC576": "空调机组1&2",
	"HVAC601": "空调机组1",
	"HVAC602": "空调机组1",
	"HVAC603": "空调机组2",
	"HVAC604": "空调机组2",
	"HVAC605": "空调机组1",
	"HVAC606": "空调机组2",
	"HVAC607": "空调机组1&2",
	"HVAC608": "空调机组1&2",
	"HVAC609": "空调机组1&2",
	"HVAC610": "空调机组1",
	"HVAC611": "空调机组2",
	"HVAC612": "空调机组1",
	"HVAC613": "空调机组1",
	"HVAC614": "空调机组2",
	"HVAC615": "空调机组2",
	"HVAC616": "空调机组1",
	"HVAC617": "空调机组1",
	"HVAC618": "空调机组2",
	"HVAC619": "空调机组2",
	"HVAC620": "废排单元",
	"HVAC621": "空调机组1",
	"HVAC622": "空调机组1",
	"HVAC623": "空调机组2",
	"HVAC624": "空调机组2",
	"HVAC625": "空调机组1",
	"HVAC626": "空调机组2",
	"HVAC676": "空调机组1&2",
}

// alertcodeFaultNameMap is retained for reference but no longer sent to the platform.
// fault_name was removed from Record61 per alertcode_v2.xlsx platform spec.
var alertcodeFaultNameMap = map[string]string{
	"HVAC101": "机组1系统1冷媒泄露预警",
	"HVAC102": "机组1系统2冷媒泄露预警",
	"HVAC103": "机组2系统1冷媒泄露预警",
	"HVAC104": "机组2系统2冷媒泄露预警",
	"HVAC105": "机组1制冷系统预警",
	"HVAC106": "机组2制冷系统预警",
	"HVAC107": "新风温度传感器预警",
	"HVAC108": "回风温度传感器预警",
	"HVAC109": "车厢温度超温预警",
	"HVAC110": "机组1滤网脏堵预警",
	"HVAC111": "机组2滤网脏堵预警",
	"HVAC112": "机组1通风机1电流预警",
	"HVAC113": "机组1通风机2电流预警",
	"HVAC114": "机组2通风机1电流预警",
	"HVAC115": "机组2通风机2电流预警",
	"HVAC116": "机组1冷凝风机1电流预警",
	"HVAC117": "机组1冷凝风机2电流预警",
	"HVAC118": "机组2冷凝风机1电流预警",
	"HVAC119": "机组2冷凝风机2电流预警",
	"HVAC120": "废排风机电流预警",
	"HVAC121": "机组1压缩机1电流预警",
	"HVAC122": "机组1压缩机2电流预警",
	"HVAC123": "机组2压缩机1电流预警",
	"HVAC124": "机组2压缩机2电流预警",
	"HVAC125": "机组1空气质量预警",
	"HVAC126": "机组2空气质量预警",
	"HVAC176": "车厢温度传感器预警",
	"HVAC201": "机组1系统1冷媒泄露预警",
	"HVAC202": "机组1系统2冷媒泄露预警",
	"HVAC203": "机组2系统1冷媒泄露预警",
	"HVAC204": "机组2系统2冷媒泄露预警",
	"HVAC205": "机组1制冷系统预警",
	"HVAC206": "机组2制冷系统预警",
	"HVAC207": "新风温度传感器预警",
	"HVAC208": "回风温度传感器预警",
	"HVAC209": "车厢温度超温预警",
	"HVAC210": "机组1滤网脏堵预警",
	"HVAC211": "机组2滤网脏堵预警",
	"HVAC212": "机组1通风机1电流预警",
	"HVAC213": "机组1通风机2电流预警",
	"HVAC214": "机组2通风机1电流预警",
	"HVAC215": "机组2通风机2电流预警",
	"HVAC216": "机组1冷凝风机1电流预警",
	"HVAC217": "机组1冷凝风机2电流预警",
	"HVAC218": "机组2冷凝风机1电流预警",
	"HVAC219": "机组2冷凝风机2电流预警",
	"HVAC220": "废排风机电流预警",
	"HVAC221": "机组1压缩机1电流预警",
	"HVAC222": "机组1压缩机2电流预警",
	"HVAC223": "机组2压缩机1电流预警",
	"HVAC224": "机组2压缩机2电流预警",
	"HVAC225": "机组1空气质量预警",
	"HVAC226": "机组2空气质量预警",
	"HVAC276": "车厢温度传感器预警",
	"HVAC301": "机组1系统1冷媒泄露预警",
	"HVAC302": "机组1系统2冷媒泄露预警",
	"HVAC303": "机组2系统1冷媒泄露预警",
	"HVAC304": "机组2系统2冷媒泄露预警",
	"HVAC305": "机组1制冷系统预警",
	"HVAC306": "机组2制冷系统预警",
	"HVAC307": "新风温度传感器预警",
	"HVAC308": "回风温度传感器预警",
	"HVAC309": "车厢温度超温预警",
	"HVAC310": "机组1滤网脏堵预警",
	"HVAC311": "机组2滤网脏堵预警",
	"HVAC312": "机组1通风机1电流预警",
	"HVAC313": "机组1通风机2电流预警",
	"HVAC314": "机组2通风机1电流预警",
	"HVAC315": "机组2通风机2电流预警",
	"HVAC316": "机组1冷凝风机1电流预警",
	"HVAC317": "机组1冷凝风机2电流预警",
	"HVAC318": "机组2冷凝风机1电流预警",
	"HVAC319": "机组2冷凝风机2电流预警",
	"HVAC320": "废排风机电流预警",
	"HVAC321": "机组1压缩机1电流预警",
	"HVAC322": "机组1压缩机2电流预警",
	"HVAC323": "机组2压缩机1电流预警",
	"HVAC324": "机组2压缩机2电流预警",
	"HVAC325": "机组1空气质量预警",
	"HVAC326": "机组2空气质量预警",
	"HVAC376": "车厢温度传感器预警",
	"HVAC401": "机组1系统1冷媒泄露预警",
	"HVAC402": "机组1系统2冷媒泄露预警",
	"HVAC403": "机组2系统1冷媒泄露预警",
	"HVAC404": "机组2系统2冷媒泄露预警",
	"HVAC405": "机组1制冷系统预警",
	"HVAC406": "机组2制冷系统预警",
	"HVAC407": "新风温度传感器预警",
	"HVAC408": "回风温度传感器预警",
	"HVAC409": "车厢温度超温预警",
	"HVAC410": "机组1滤网脏堵预警",
	"HVAC411": "机组2滤网脏堵预警",
	"HVAC412": "机组1通风机1电流预警",
	"HVAC413": "机组1通风机2电流预警",
	"HVAC414": "机组2通风机1电流预警",
	"HVAC415": "机组2通风机2电流预警",
	"HVAC416": "机组1冷凝风机1电流预警",
	"HVAC417": "机组1冷凝风机2电流预警",
	"HVAC418": "机组2冷凝风机1电流预警",
	"HVAC419": "机组2冷凝风机2电流预警",
	"HVAC420": "废排风机电流预警",
	"HVAC421": "机组1压缩机1电流预警",
	"HVAC422": "机组1压缩机2电流预警",
	"HVAC423": "机组2压缩机1电流预警",
	"HVAC424": "机组2压缩机2电流预警",
	"HVAC425": "机组1空气质量预警",
	"HVAC426": "机组2空气质量预警",
	"HVAC476": "车厢温度传感器预警",
	"HVAC501": "机组1系统1冷媒泄露预警",
	"HVAC502": "机组1系统2冷媒泄露预警",
	"HVAC503": "机组2系统1冷媒泄露预警",
	"HVAC504": "机组2系统2冷媒泄露预警",
	"HVAC505": "机组1制冷系统预警",
	"HVAC506": "机组2制冷系统预警",
	"HVAC507": "新风温度传感器预警",
	"HVAC508": "回风温度传感器预警",
	"HVAC509": "车厢温度超温预警",
	"HVAC510": "机组1滤网脏堵预警",
	"HVAC511": "机组2滤网脏堵预警",
	"HVAC512": "机组1通风机1电流预警",
	"HVAC513": "机组1通风机2电流预警",
	"HVAC514": "机组2通风机1电流预警",
	"HVAC515": "机组2通风机2电流预警",
	"HVAC516": "机组1冷凝风机1电流预警",
	"HVAC517": "机组1冷凝风机2电流预警",
	"HVAC518": "机组2冷凝风机1电流预警",
	"HVAC519": "机组2冷凝风机2电流预警",
	"HVAC520": "废排风机电流预警",
	"HVAC521": "机组1压缩机1电流预警",
	"HVAC522": "机组1压缩机2电流预警",
	"HVAC523": "机组2压缩机1电流预警",
	"HVAC524": "机组2压缩机2电流预警",
	"HVAC525": "机组1空气质量预警",
	"HVAC526": "机组2空气质量预警",
	"HVAC576": "车厢温度传感器预警",
	"HVAC601": "机组1系统1冷媒泄露预警",
	"HVAC602": "机组1系统2冷媒泄露预警",
	"HVAC603": "机组2系统1冷媒泄露预警",
	"HVAC604": "机组2系统2冷媒泄露预警",
	"HVAC605": "机组1制冷系统预警",
	"HVAC606": "机组2制冷系统预警",
	"HVAC607": "新风温度传感器预警",
	"HVAC608": "回风温度传感器预警",
	"HVAC609": "车厢温度超温预警",
	"HVAC610": "机组1滤网脏堵预警",
	"HVAC611": "机组2滤网脏堵预警",
	"HVAC612": "机组1通风机1电流预警",
	"HVAC613": "机组1通风机2电流预警",
	"HVAC614": "机组2通风机1电流预警",
	"HVAC615": "机组2通风机2电流预警",
	"HVAC616": "机组1冷凝风机1电流预警",
	"HVAC617": "机组1冷凝风机2电流预警",
	"HVAC618": "机组2冷凝风机1电流预警",
	"HVAC619": "机组2冷凝风机2电流预警",
	"HVAC620": "废排风机电流预警",
	"HVAC621": "机组1压缩机1电流预警",
	"HVAC622": "机组1压缩机2电流预警",
	"HVAC623": "机组2压缩机1电流预警",
	"HVAC624": "机组2压缩机2电流预警",
	"HVAC625": "机组1空气质量预警",
	"HVAC626": "机组2空气质量预警",
	"HVAC676": "车厢温度传感器预警",
}

// locationByCode returns the location string for a given HVAC code.
// Supports both predict codes (seq 1-26, flat map) and alarm codes (seq 27-75, seq map).
// Falls back to the code itself if not found.
func locationByCode(code string) string {
	if loc, ok := alertcodeLocationMap[code]; ok {
		return loc
	}
	// Alarm codes: HVAC{carriage*100+seq}, seq 27-75
	upper := strings.ToUpper(code)
	if strings.HasPrefix(upper, "HVAC") {
		n, err := strconv.Atoi(code[4:])
		if err == nil {
			seq := n % 100
			if seq >= 27 && seq <= 75 {
				if loc, ok := alarmSeqToLocation[seq]; ok {
					return loc
				}
			}
		}
	}
	return code
}

// faultNameByCode returns the fault_name string for a given HVAC code.
// Supports both predict codes (seq 1-26) and alarm codes (seq 27-75).
// Falls back to empty string if not found.
func faultNameByCode(code string) string {
	if name, ok := alertcodeFaultNameMap[code]; ok {
		return name
	}
	upper := strings.ToUpper(code)
	if strings.HasPrefix(upper, "HVAC") {
		n, err := strconv.Atoi(code[4:])
		if err == nil {
			seq := n % 100
			if seq >= 27 && seq <= 75 {
				return alarmSeqToFaultName[seq]
			}
		}
	}
	return ""
}

// padTrainNo formats a train number string as a 5-digit zero-padded string.
// Per platform spec, train_no and trainNo must be 5 characters wide.
func padTrainNo(s string) string {
	n, err := strconv.Atoi(s)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%05d", n)
}
