export const DOUBAO_AUTH_MODE_API_KEY = 'api_key'
export const DOUBAO_AUTH_MODE_LEGACY = 'legacy'
export const DEFAULT_DOUBAO_WS_URL = 'wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream'
export const DEFAULT_DOUBAO_MODEL = 'seed-tts-2.0-standard'

export const DOUBAO_MODEL_OPTIONS = [
  { label: '豆包语音合成 2.0 Standard（推荐）', value: 'seed-tts-2.0-standard' },
  { label: '豆包语音合成 2.0 Expressive', value: 'seed-tts-2.0-expressive' },
  { label: '豆包语音合成 1.1（1.0 音色）', value: 'seed-tts-1.1' },
  { label: '豆包声音复刻 2.0 Standard', value: 'seed-icl-2.0-standard' },
  { label: '豆包声音复刻 2.0 Expressive', value: 'seed-icl-2.0-expressive' },
  { label: '豆包声音复刻 1.0', value: 'seed-icl-1.0' }
]

export function detectDoubaoAuthMode(data = {}) {
  if (String(data?.api_key || '').trim()) {
    return DOUBAO_AUTH_MODE_API_KEY
  }
  if (String(data?.appid || '').trim() || String(data?.access_token || '').trim()) {
    return DOUBAO_AUTH_MODE_LEGACY
  }
  return DOUBAO_AUTH_MODE_API_KEY
}

export function createDefaultDoubaoWSConfig() {
  return {
    auth_mode: DOUBAO_AUTH_MODE_API_KEY,
    api_key: '',
    appid: '',
    access_token: '',
    model: DEFAULT_DOUBAO_MODEL,
    resource_id: '',
    voice: '',
    ws_url: DEFAULT_DOUBAO_WS_URL
  }
}

export function normalizeDoubaoWSConfig(data = {}) {
  const wsUrl = String(data?.ws_url || '').trim()
    || (String(data?.ws_host || '').trim()
      ? `wss://${String(data.ws_host).trim()}/api/v3/tts/unidirectional/stream`
      : DEFAULT_DOUBAO_WS_URL)
  return {
    auth_mode: detectDoubaoAuthMode(data),
    api_key: String(data?.api_key || ''),
    appid: String(data?.appid || ''),
    access_token: String(data?.access_token || ''),
    model: String(data?.model || '').trim() || DEFAULT_DOUBAO_MODEL,
    resource_id: String(data?.resource_id || ''),
    voice: String(data?.voice || ''),
    ws_url: wsUrl
  }
}

export function serializeDoubaoWSConfig(data = {}) {
  const authMode = data?.auth_mode || detectDoubaoAuthMode(data)
  const config = {
    model: String(data?.model || '').trim() || DEFAULT_DOUBAO_MODEL,
    resource_id: String(data?.resource_id || '').trim(),
    voice: String(data?.voice || '').trim(),
    ws_url: String(data?.ws_url || '').trim() || DEFAULT_DOUBAO_WS_URL
  }
  if (authMode === DOUBAO_AUTH_MODE_LEGACY) {
    config.appid = String(data?.appid || '').trim()
    config.access_token = String(data?.access_token || '').trim()
  } else {
    config.api_key = String(data?.api_key || '').trim()
  }
  return config
}

export function reconcileDoubaoVoice(previousOptions, nextOptions, currentVoice) {
  const current = String(currentVoice || '').trim()
  if (!current) return ''
  const previousValues = new Set((Array.isArray(previousOptions) ? previousOptions : []).map((item) => String(item?.value || '').trim()))
  const next = Array.isArray(nextOptions) ? nextOptions : []
  const nextValues = new Set(next.map((item) => String(item?.value || '').trim()))
  if (nextValues.has(current) || !previousValues.has(current)) return current
  return String(next[0]?.value || '')
}
