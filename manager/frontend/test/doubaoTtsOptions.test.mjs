import test from 'node:test'
import assert from 'node:assert/strict'

import {
  DOUBAO_AUTH_MODE_API_KEY,
  DOUBAO_AUTH_MODE_LEGACY,
  DEFAULT_DOUBAO_MODEL,
  DEFAULT_DOUBAO_WS_URL,
  createDefaultDoubaoWSConfig,
  detectDoubaoAuthMode,
  normalizeDoubaoWSConfig,
  reconcileDoubaoVoice,
  serializeDoubaoWSConfig
} from '../src/views/admin/forms/doubaoTtsOptions.js'

test('detectDoubaoAuthMode prefers api_key when present and falls back to legacy for existing configs', () => {
  assert.equal(detectDoubaoAuthMode({}), DOUBAO_AUTH_MODE_API_KEY)
  assert.equal(detectDoubaoAuthMode({ api_key: 'new-key' }), DOUBAO_AUTH_MODE_API_KEY)
  assert.equal(detectDoubaoAuthMode({ api_key: 'new-key', appid: 'old-app' }), DOUBAO_AUTH_MODE_API_KEY)
  assert.equal(detectDoubaoAuthMode({ appid: 'old-app', access_token: 'old-token' }), DOUBAO_AUTH_MODE_LEGACY)
  assert.equal(detectDoubaoAuthMode({ access_token: 'old-token' }), DOUBAO_AUTH_MODE_LEGACY)
})

test('createDefaultDoubaoWSConfig defaults to new api_key auth mode and 2.0 model without placeholder credentials', () => {
  const cfg = createDefaultDoubaoWSConfig()
  assert.equal(cfg.auth_mode, DOUBAO_AUTH_MODE_API_KEY)
  assert.equal(cfg.api_key, '')
  assert.equal(cfg.appid, '')
  assert.equal(cfg.access_token, '')
  assert.equal(cfg.model, DEFAULT_DOUBAO_MODEL)
  assert.equal(cfg.ws_url, DEFAULT_DOUBAO_WS_URL)
})

test('normalizeDoubaoWSConfig preserves legacy config and ws_host fallback', () => {
  const normalized = normalizeDoubaoWSConfig({
    appid: '123456',
    access_token: 'legacy-token',
    model: 'seed-tts-1.1',
    voice: 'zh_female_cancan_mars_bigtts',
    ws_host: 'openspeech.bytedance.com'
  })
  assert.equal(normalized.auth_mode, DOUBAO_AUTH_MODE_LEGACY)
  assert.equal(normalized.appid, '123456')
  assert.equal(normalized.access_token, 'legacy-token')
  assert.equal(normalized.model, 'seed-tts-1.1')
  assert.equal(normalized.ws_url, DEFAULT_DOUBAO_WS_URL)
})

test('serializeDoubaoWSConfig emits only active auth mode credentials', () => {
  const apiKeyCfg = serializeDoubaoWSConfig({
    auth_mode: DOUBAO_AUTH_MODE_API_KEY,
    api_key: '  key-123  ',
    appid: 'stale-app',
    access_token: 'stale-token',
    model: 'seed-tts-2.0-standard',
    voice: 'zh_female_vv_uranus_bigtts'
  })
  assert.equal(apiKeyCfg.api_key, 'key-123')
  assert.equal('appid' in apiKeyCfg, false)
  assert.equal('access_token' in apiKeyCfg, false)
  assert.equal(apiKeyCfg.ws_url, DEFAULT_DOUBAO_WS_URL)

  const legacyCfg = serializeDoubaoWSConfig({
    auth_mode: DOUBAO_AUTH_MODE_LEGACY,
    api_key: 'stale-key',
    appid: ' 1001 ',
    access_token: ' tok-1 ',
    model: 'seed-tts-1.1',
    voice: 'zh_female_cancan_mars_bigtts'
  })
  assert.equal(legacyCfg.appid, '1001')
  assert.equal(legacyCfg.access_token, 'tok-1')
  assert.equal('api_key' in legacyCfg, false)
})

test('reconcileDoubaoVoice keeps custom voice and switches mismatched system voice on model change', () => {
  const v2 = [{ value: 'zh_female_vv_uranus_bigtts' }, { value: 'ICL_uranus_zh_female_xingganyujie_tob' }]
  const v1 = [{ value: 'zh_female_cancan_mars_bigtts' }]

  assert.equal(reconcileDoubaoVoice(v2, v1, 'zh_female_vv_uranus_bigtts'), 'zh_female_cancan_mars_bigtts')
  assert.equal(reconcileDoubaoVoice(v2, v1, 'custom_speaker_id'), 'custom_speaker_id')
  assert.equal(reconcileDoubaoVoice(v2, v2, 'ICL_uranus_zh_female_xingganyujie_tob'), 'ICL_uranus_zh_female_xingganyujie_tob')
})
