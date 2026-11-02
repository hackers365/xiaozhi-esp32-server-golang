const providers = {
  vad: new Set(['ten_vad', 'webrtc_vad', 'silero_vad']),
  asr: new Set(['funasr', 'aliyun_funasr', 'doubao', 'aliyun_qwen3', 'mimo', 'xunfei']),
  tts: new Set([
    'doubao',
    'doubao_ws',
    'cosyvoice',
    'edge',
    'edge_offline',
    'xiaozhi',
    'xunfei',
    'xunfei_super_tts',
    'openai',
    'zhipu',
    'minimax',
    'aliyun_qwen',
    'indextts_vllm',
    'mimo'
  ]),
  memory: new Set(['nomemo', 'memobase', 'mem0', 'memos']),
  vision: new Set(['aliyun_vision', 'doubao_vision'])
}

function clean(value) {
  return String(value || '').trim().toLowerCase()
}

function known(type, value) {
  const normalized = clean(value)
  return providers[type]?.has(normalized) ? normalized : ''
}

function has(data, ...keys) {
  return keys.some(key => Object.prototype.hasOwnProperty.call(data || {}, key))
}

function includes(value, ...needles) {
  const text = clean(value)
  return needles.some(needle => text.includes(needle))
}

function stringValue(data, ...keys) {
  for (const key of keys) {
    const value = data?.[key]
    if (typeof value === 'string') return value
  }
  return ''
}

function resolve(type, provider, configId, data, infer, fallback) {
  return known(type, provider) || known(type, data?.provider) || known(type, configId) || infer(data || {}) || fallback
}

export function resolveVADProvider(provider, configId, data = {}) {
  return resolve('vad', provider, configId, data, (value) => {
    if (has(value, 'hop_size')) return 'ten_vad'
    if (has(value, 'model_path', 'min_silence_duration_ms')) return 'silero_vad'
    if (has(value, 'vad_mode', 'vad_sample_rate', 'pool_min_size', 'pool_max_idle')) return 'webrtc_vad'
    return ''
  }, 'ten_vad')
}

export function resolveASRProvider(provider, configId, data = {}) {
  return resolve('asr', provider, configId, data, (value) => {
    const model = stringValue(value, 'model')
    const wsUrl = stringValue(value, 'ws_url')
    if (includes(model, 'mimo-v2.5-asr') || includes(stringValue(value, 'api_url'), 'xiaomimimo.com')) return 'mimo'
    if (has(value, 'appid', 'api_secret')) return 'xunfei'
    if (includes(model, 'qwen3-asr') || includes(wsUrl, '/realtime')) return 'aliyun_qwen3'
    if (includes(model, 'fun-asr') || includes(wsUrl, '/inference')) return 'aliyun_funasr'
    if (has(value, 'access_token', 'resource_id', 'end_window_size', 'chunk_duration')) return 'doubao'
    if (has(value, 'host', 'port', 'chunk_size', 'chunk_interval', 'max_connections')) return 'funasr'
    return ''
  }, 'funasr')
}

export function resolveTTSProvider(provider, configId, data = {}) {
  return resolve('tts', provider, configId, data, (value) => {
    const url = stringValue(value, 'api_url', 'server_url', 'ws_url')
    const model = stringValue(value, 'model')
    if (includes(model, 'mimo-v2.5-tts') || includes(url, 'xiaomimimo.com')) return 'mimo'
    if (has(value, 'spk_id', 'instruct_text')) return 'cosyvoice'
    if (has(value, 'server_url')) return 'edge_offline'
    if (has(value, 'rate', 'pitch') && has(value, 'voice')) return 'edge'
    if (includes(url, 'xfyun.cn')) return has(value, 'double_stream', 'bgs', 'oral_level', 'spark_assist', 'stop_split', 'remain') ? 'xunfei_super_tts' : 'xunfei'
    if (includes(url, 'dashscope.aliyuncs.com') || includes(model, 'qwen') || has(value, 'language_type', 'region')) return 'aliyun_qwen'
    if (includes(url, 'bigmodel.cn') || includes(model, 'glm-tts')) return 'zhipu'
    if (includes(url, 'minimax')) return 'minimax'
    if (includes(model, 'indextts')) return 'indextts_vllm'
    if (includes(url, 'openspeech', 'volces.com', 'volcengine')) return has(value, 'ws_url', 'ws_host', 'use_stream', 'resource_id') ? 'doubao_ws' : 'doubao'
    if (has(value, 'api_key', 'model', 'voice', 'response_format')) return 'openai'
    return ''
  }, 'doubao_ws')
}

export function resolveMemoryProvider(provider, configId, data = {}) {
  return resolve('memory', provider, configId, data, (value) => {
    const baseUrl = stringValue(value, 'base_url')
    if (includes(baseUrl, 'memobase')) return 'memobase'
    if (includes(baseUrl, 'mem0.ai')) return 'mem0'
    if (includes(baseUrl, 'memos', 'memtensor')) return 'memos'
    return ''
  }, 'memobase')
}

export function resolveVisionProvider(provider, configId, data = {}) {
  return resolve('vision', provider, configId, data, (value) => {
    const baseUrl = stringValue(value, 'base_url')
    const model = stringValue(value, 'model_name', 'model')
    if (includes(baseUrl, 'dashscope.aliyuncs.com') || includes(model, 'qwen-vl')) return 'aliyun_vision'
    if (includes(baseUrl, 'volces.com', 'volcengine') || includes(model, 'doubao')) return 'doubao_vision'
    return ''
  }, 'aliyun_vision')
}
