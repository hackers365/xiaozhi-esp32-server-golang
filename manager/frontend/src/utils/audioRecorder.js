/**
 * Browser audio recording capability detection and error formatting helpers.
 *
 * Local deployments accessed over non-HTTPS LAN URLs (e.g. http://192.168.x.x:8080)
 * run in a non-secure context where browsers disable navigator.mediaDevices.
 * Checking environment capability up front avoids premature permission prompts
 * and provides actionable guidance to users.
 */

const getDefaultEnv = () => {
  const win = typeof window !== 'undefined' ? window : undefined
  const nav = typeof navigator !== 'undefined' ? navigator : undefined
  const recorder = typeof MediaRecorder !== 'undefined' ? MediaRecorder : undefined
  return { window: win, navigator: nav, MediaRecorder: recorder }
}

export const isLocalhostHostname = (hostname = '') => {
  const normalized = String(hostname || '').trim().toLowerCase()
  return normalized === 'localhost' || normalized === '127.0.0.1' || normalized === '[::1]' || normalized === '::1'
}

export const isInsecureHttpContext = (env = getDefaultEnv()) => {
  const win = env?.window
  if (!win) return false
  if (typeof win.isSecureContext === 'boolean') {
    return !win.isSecureContext
  }
  const protocol = String(win.location?.protocol || '').toLowerCase()
  const hostname = String(win.location?.hostname || '')
  return protocol === 'http:' && !isLocalhostHostname(hostname)
}

export const checkAudioRecordingSupport = (env = getDefaultEnv()) => {
  const nav = env?.navigator
  const mediaRecorderCtor = env?.MediaRecorder

  if (isInsecureHttpContext(env)) {
    return {
      supported: false,
      reason: 'insecure_context',
      message: '当前页面使用非 HTTPS 的局域网地址访问，浏览器安全策略已禁止调用麦克风。请改用 HTTPS 或本机 localhost 访问，或切换为“上传文件”方式。'
    }
  }

  const hasGetUserMedia = Boolean(nav?.mediaDevices && typeof nav.mediaDevices.getUserMedia === 'function')
  const hasMediaRecorder = typeof mediaRecorderCtor === 'function'

  if (!hasGetUserMedia || !hasMediaRecorder) {
    return {
      supported: false,
      reason: 'unsupported_browser',
      message: '当前浏览器不支持录音功能（缺少麦克风接口或 MediaRecorder），请更换新版 Chrome/Edge/Firefox 浏览器或使用“上传文件”方式。'
    }
  }

  return {
    supported: true,
    reason: '',
    message: ''
  }
}

export const formatAudioRecordError = (error, env = getDefaultEnv()) => {
  if (isInsecureHttpContext(env)) {
    return '当前页面使用非 HTTPS 地址访问，浏览器禁止调用麦克风，请改用 HTTPS/localhost 或使用上传文件方式'
  }

  const name = String(error?.name || '')
  const rawMessage = String(error?.message || '')

  switch (name) {
    case 'NotAllowedError':
    case 'PermissionDeniedError':
      return '未获得麦克风权限，请在浏览器地址栏允许麦克风权限后重试'
    case 'NotFoundError':
    case 'DevicesNotFoundError':
      return '未检测到可用的麦克风设备，请检查麦克风连接后重试'
    case 'NotReadableError':
    case 'TrackStartError':
      return '麦克风正被其他应用占用或无法访问，请关闭占用程序后重试'
    case 'OverconstrainedError':
    case 'ConstraintNotSatisfiedError':
      return '当前麦克风不支持指定的采样率或声道参数，请重试或使用上传文件方式'
    case 'SecurityError':
      return '浏览器安全策略阻止了麦克风访问，请改用 HTTPS 或本机 localhost 访问'
    default:
      if (!env?.navigator?.mediaDevices || typeof env.navigator.mediaDevices.getUserMedia !== 'function') {
        return '当前浏览器环境不支持麦克风录音，请改用 HTTPS/localhost 或使用上传文件方式'
      }
      return rawMessage ? `录音失败: ${rawMessage}` : '录音失败，请检查麦克风权限后重试'
  }
}

export const requestAudioStream = async (preferredAudioConstraints, env = getDefaultEnv()) => {
  const support = checkAudioRecordingSupport(env)
  if (!support.supported) {
    const err = new Error(support.message)
    err.name = support.reason === 'insecure_context' ? 'SecurityError' : 'NotSupportedError'
    throw err
  }

  const mediaDevices = env.navigator.mediaDevices
  if (!preferredAudioConstraints || preferredAudioConstraints === true) {
    return mediaDevices.getUserMedia({ audio: true })
  }

  try {
    return await mediaDevices.getUserMedia({ audio: preferredAudioConstraints })
  } catch (error) {
    const name = String(error?.name || '')
    if (name === 'OverconstrainedError' || name === 'ConstraintNotSatisfiedError') {
      return mediaDevices.getUserMedia({ audio: true })
    }
    throw error
  }
}
