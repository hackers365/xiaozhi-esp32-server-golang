export const ALIYUN_TTS_MODEL_OPTIONS = [
  { label: 'qwen3-tts-flash (Qwen3 TTS 闪电版)', value: 'qwen3-tts-flash' },
  { label: 'qwen3-tts-instruct-flash (Qwen3 TTS 指令控音版)', value: 'qwen3-tts-instruct-flash' },
  { label: 'cosyvoice-v3-flash (CosyVoice v3 闪电版)', value: 'cosyvoice-v3-flash' },
  { label: 'cosyvoice-v2 (CosyVoice v2 高质量版)', value: 'cosyvoice-v2' },
  { label: 'cosyvoice-v1 (CosyVoice v1 标准版)', value: 'cosyvoice-v1' },
  { label: 'qwen-audio-3.0-tts-flash (Qwen Audio 3.0 闪电版)', value: 'qwen-audio-3.0-tts-flash' },
  { label: 'qwen-audio-3.0-tts-plus (Qwen Audio 3.0 专业版)', value: 'qwen-audio-3.0-tts-plus' }
]

export const ALIYUN_TTS_API_URL_BEIJING = 'https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation'
export const ALIYUN_TTS_API_URL_SINGAPORE = 'https://dashscope-intl.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation'

export function resolveAliyunAPIURL(apiURL, region) {
  const value = String(apiURL || '').trim()
  let hostname = ''
  try {
    hostname = value ? new URL(value).hostname.toLowerCase() : ''
  } catch (_) {
    return value
  }
  if (!value || hostname === 'dashscope.aliyuncs.com' || hostname === 'dashscope-intl.aliyuncs.com') {
    return String(region || '').toLowerCase() === 'singapore' ? ALIYUN_TTS_API_URL_SINGAPORE : ALIYUN_TTS_API_URL_BEIJING
  }
  return value
}

export function supportsAliyunInstruction(model) {
  const normalized = String(model || '').trim().toLowerCase()
  return normalized.startsWith('qwen-audio-') || (normalized.startsWith('qwen3-tts-') && normalized.includes('instruct'))
}

export function normalizeAliyunTtsConfig(data = {}) {
  const { instructions, ...rest } = data || {}
  return {
    ...rest,
    voice_prompt: String(rest.voice_prompt || instructions || '')
  }
}

export function serializeAliyunTtsConfig(data = {}) {
  const { instructions, language_type, ...rest } = data || {}
  const model = rest.model || 'qwen3-tts-flash'
  const result = {
    ...rest,
    provider: 'aliyun_qwen',
    api_url: resolveAliyunAPIURL(rest.api_url, rest.region),
    model,
    voice: rest.voice || 'Cherry',
    frame_duration: rest.frame_duration || 60,
    format: rest.format || 'ogg_opus',
    voice_prompt: String(rest.voice_prompt || '')
  }
  if (/^(qwen3-tts|qwen-tts)/i.test(model)) result.language_type = language_type || 'Chinese'
  return result
}

export function reconcileAliyunVoice(previousOptions, nextOptions, currentVoice) {
  const current = String(currentVoice || '').trim()
  const previousValues = new Set((Array.isArray(previousOptions) ? previousOptions : []).map((item) => String(item?.value || '').trim()))
  const next = Array.isArray(nextOptions) ? nextOptions : []
  const nextValues = new Set(next.map((item) => String(item?.value || '').trim()))

  if (!current || nextValues.has(current) || !previousValues.has(current)) return current
  return String(next[0]?.value || '')
}
