export function supportsAliyunPreview(provider) {
  return String(provider || '').trim() === 'aliyun_qwen'
}

export function previewPlaybackIdentity(value = {}) {
  return [value.provider, value.configId, value.model, value.voice, value.instruction, value.apiKey, value.apiUrl, value.region, value.text].map((item) => String(item || '')).join('\u0000')
}

export async function extractPreviewError(error) {
  const data = error?.response?.data
  if (data && typeof data.text === 'function') {
    try {
      const parsed = JSON.parse(await data.text())
      if (parsed?.error) return String(parsed.error)
      if (parsed?.message) return String(parsed.message)
    } catch (_) {
      // Fall through to the regular request error.
    }
  } else if (data?.error) {
    return String(data.error)
  }
  return String(error?.message || '合成试听音频失败')
}
