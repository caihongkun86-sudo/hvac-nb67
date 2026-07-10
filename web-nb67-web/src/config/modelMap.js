// 宁波地铁工作模式编码 → 模型名映射
// key: model_id（来自 payload_json.model_id）
// value: { modelId: '模型名' }
// modelId 对应协议字段 ModelId（uint8），取值范围 0-9

export const MODEL_MAP = {
  0: '停机',
  1: '通风',
  2: '强冷',
  3: '弱冷',
  4: '强暖',
  5: '弱暖',
  6: '紧急通风',
  7: '预冷',
  8: '预暖',
  9: '融霜',
}

/**
 * 根据模型编码返回模型名；无映射时原样返回编码
 * @param {number|string} modelId 模型编码
 */
export function getModelName(modelId) {
  if (modelId === '' || modelId === null || modelId === undefined) return '-'
  const modelMap = MODEL_MAP[Number(modelId)]
  if (!modelMap) return String(modelId)
  return modelMap || String(modelId)
}
