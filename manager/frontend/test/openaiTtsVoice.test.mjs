import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

import { TTS_PROVIDERS_WITH_VOICES } from '../src/views/admin/forms/ttsProviderOptions.js'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

test('openai provider is in TTS_PROVIDERS_WITH_VOICES and supports allow-create in TTSConfigForm', () => {
  assert.ok(TTS_PROVIDERS_WITH_VOICES.includes('openai'), 'openai should be in TTS_PROVIDERS_WITH_VOICES')

  const formPath = path.resolve(__dirname, '../src/views/admin/forms/TTSConfigForm.vue')
  const content = fs.readFileSync(formPath, 'utf8')
  const openaiSectionMatch = content.match(/<template v-if="model\.provider === 'openai'">([\s\S]*?)<\/template>/)
  assert.ok(openaiSectionMatch, 'openai section should exist in TTSConfigForm.vue')

  const voiceSelectMatch = openaiSectionMatch[1].match(/<el-form-item label="音色" prop="openai\.voice">([\s\S]*?)<\/el-form-item>/)
  assert.ok(voiceSelectMatch, 'openai.voice form item should exist')
  assert.match(voiceSelectMatch[1], /\ballow-create\b/, 'openai.voice el-select should support allow-create for custom voices')
  assert.match(voiceSelectMatch[1], /\bdefault-first-option\b/, 'openai.voice el-select should have default-first-option')
})
