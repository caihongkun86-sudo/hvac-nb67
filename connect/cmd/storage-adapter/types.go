package main

import "encoding/json"

type StorageRecord struct {
	// ... existing fields ...
	SchemaVersion string `json:"schema_version"`

	EventTimeText  string `json:"event_time_text"`
	EventTimeValid bool   `json:"event_time_valid"`
	IngestTime     string `json:"ingest_time"`
	ProcessTime    string `json:"process_time"`

	LineID     int32  `json:"line_id"`
	TrainID    int32  `json:"train_id"`
	CarriageID int32  `json:"carriage_id"`
	DeviceID   string `json:"device_id"`
	FrameNo    *int64 `json:"frame_no"`

	ParserVersion string `json:"parser_version"`
	QualityCode   int16  `json:"quality_code"`

	WmodeU1 *int16 `json:"wmode_u1"`
	WmodeU2 *int16 `json:"wmode_u2"`

	FCpU11 *float64 `json:"f_cp_u11"`
	FCpU12 *float64 `json:"f_cp_u12"`
	FCpU21 *float64 `json:"f_cp_u21"`
	FCpU22 *float64 `json:"f_cp_u22"`

	ICpU11 *float64 `json:"i_cp_u11"`
	ICpU12 *float64 `json:"i_cp_u12"`
	ICpU21 *float64 `json:"i_cp_u21"`
	ICpU22 *float64 `json:"i_cp_u22"`

	SuckpU11 *float64 `json:"suckp_u11"`
	SuckpU12 *float64 `json:"suckp_u12"`
	SuckpU21 *float64 `json:"suckp_u21"`
	SuckpU22 *float64 `json:"suckp_u22"`

	HighpressU11 *float64 `json:"highpress_u11"`
	HighpressU12 *float64 `json:"highpress_u12"`
	HighpressU21 *float64 `json:"highpress_u21"`
	HighpressU22 *float64 `json:"highpress_u22"`

	FasU1 *float64 `json:"fas_u1"`
	FasU2 *float64 `json:"fas_u2"`
	RasU1 *float64 `json:"ras_u1"`
	RasU2 *float64 `json:"ras_u2"`

	PresdiffU1 *float64 `json:"presdiff_u1"`
	PresdiffU2 *float64 `json:"presdiff_u2"`

	AqCo2U1  *float64 `json:"aq_co2_u1"`
	AqCo2U2  *float64 `json:"aq_co2_u2"`
	AqTvocU1 *float64 `json:"aq_tvoc_u1"`
	AqTvocU2 *float64 `json:"aq_tvoc_u2"`
	AqPm25U1 *float64 `json:"aq_pm2_5_u1"`
	AqPm25U2 *float64 `json:"aq_pm2_5_u2"`
	AqPm10U1 *float64 `json:"aq_pm10_u1"`
	AqPm10U2 *float64 `json:"aq_pm10_u2"`

	BocfltEfU11 *bool `json:"bocflt_ef_u11"`
	BocfltEfU12 *bool `json:"bocflt_ef_u12"`
	BocfltEfU21 *bool `json:"bocflt_ef_u21"`
	BocfltEfU22 *bool `json:"bocflt_ef_u22"`
	BocfltCfU11 *bool `json:"bocflt_cf_u11"`
	BocfltCfU12 *bool `json:"bocflt_cf_u12"`
	BocfltCfU21 *bool `json:"bocflt_cf_u21"`
	BocfltCfU22 *bool `json:"bocflt_cf_u22"`

	BlpfltCompU11 *bool `json:"blpflt_comp_u11"`
	BlpfltCompU12 *bool `json:"blpflt_comp_u12"`
	BlpfltCompU21 *bool `json:"blpflt_comp_u21"`
	BlpfltCompU22 *bool `json:"blpflt_comp_u22"`

	BscfltCompU11 *bool `json:"bscflt_comp_u11"`
	BscfltCompU12 *bool `json:"bscflt_comp_u12"`
	BscfltCompU21 *bool `json:"bscflt_comp_u21"`
	BscfltCompU22 *bool `json:"bscflt_comp_u22"`

	BfltTempover   *bool `json:"bflt_tempover"`
	BfltDiffpresU1 *bool `json:"bflt_diffpres_u1"`
	BfltDiffpresU2 *bool `json:"bflt_diffpres_u2"`

	PayloadJSON json.RawMessage `json:"payload_json"`
}

type EventRecord struct {
	// EventMeta 出现在 nb67 SubEvent 顶层（包含 device_id, event_time_text 等）。
	// 关键：空帧（hits=[]）也带 EventMeta —— 是 lifecycle close 状态机的唯一信号源。
	EventMeta EventMeta `json:"event_meta"`
	Hits      []Hit     `json:"hits"`
}

type Hit struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Severity int16  `json:"severity"`
	Level    *int16 `json:"level,omitempty"`
	// 新增：接住 topic 里的触发条件快照
	TriggerConditionSnapshot string    `json:"trigger_condition_snapshot,omitempty"`
	EventMeta                EventMeta `json:"event_meta"`
}

// EventMeta 兼容 nb67 输出的两种数字风格：
//   nb67_event_processor 把 line_id/train_id 作为 string 输出（来自 parsedInput.json.Number）
//   早期/部分链路可能输出整型
// 用 json.Number 同时接受字符串和数字。
type EventMeta struct {
	EventTimeText string      `json:"event_time_text"`
	LineID        json.Number `json:"line_id"`
	TrainID       json.Number `json:"train_id"`
	CarriageID    json.Number `json:"carriage_id"`
	DeviceID      string      `json:"device_id"`
}

// LineIDInt32 / TrainIDInt32 / CarriageIDInt32：把 json.Number 安全转 int32（0 兜底）。
func (m EventMeta) LineIDInt32() int32     { return numberToInt32(m.LineID) }
func (m EventMeta) TrainIDInt32() int32    { return numberToInt32(m.TrainID) }
func (m EventMeta) CarriageIDInt32() int32 { return numberToInt32(m.CarriageID) }

func numberToInt32(n json.Number) int32 {
	if n == "" {
		return 0
	}
	if i, err := n.Int64(); err == nil {
		return int32(i)
	}
	if f, err := n.Float64(); err == nil {
		return int32(f)
	}
	return 0
}

type pendingMessage struct {
	isEvent bool
	record  any // Can be StorageRecord or []EventFlatRecord (we'll flatten them)
	msg     saramaMessage
}

type EventFlatRecord struct {
	EventTime  string
	IngestTime string
	LineID     int32
	TrainID    int32
	CarriageID int32
	DeviceID   string
	EventType  string
	FaultCode  string
	FaultName  string
	Severity   int16
	Payload    json.RawMessage
}
