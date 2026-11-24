import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

import {
  checkAudioRecordingSupport,
  formatAudioRecordError,
  isInsecureHttpContext,
  requestAudioStream
} from '../src/utils/audioRecorder.js'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

class FakeMediaRecorder {}

test('checkAudioRecordingSupport identifies insecure LAN HTTP context with actionable guidance', () => {
  const env = {
    window: {
      isSecureContext: false,
      location: { protocol: 'http:', hostname: '192.168.1.20' }
    },
    navigator: {},
    MediaRecorder: FakeMediaRecorder
  }

  assert.equal(isInsecureHttpContext(env), true)
  const result = checkAudioRecordingSupport(env)
  assert.equal(result.supported, false)
  assert.equal(result.reason, 'insecure_context')
  assert.match(result.message, /HTTPS/)
  assert.match(result.message, /localhost/)
})

test('checkAudioRecordingSupport reports supported on secure origin without invoking getUserMedia', () => {
  let getUserMediaCalled = false
  const env = {
    window: {
      isSecureContext: true,
      location: { protocol: 'http:', hostname: 'localhost' }
    },
    navigator: {
      mediaDevices: {
        getUserMedia: async () => {
          getUserMediaCalled = true
          return {}
        }
      }
    },
    MediaRecorder: FakeMediaRecorder
  }

  const result = checkAudioRecordingSupport(env)
  assert.equal(result.supported, true)
  assert.equal(result.reason, '')
  assert.equal(getUserMediaCalled, false)
})

test('checkAudioRecordingSupport reports unsupported browser when MediaRecorder or mediaDevices is missing', () => {
  const envMissingRecorder = {
    window: { isSecureContext: true, location: { protocol: 'https:', hostname: 'example.com' } },
    navigator: { mediaDevices: { getUserMedia: async () => ({}) } },
    MediaRecorder: undefined
  }
  const res1 = checkAudioRecordingSupport(envMissingRecorder)
  assert.equal(res1.supported, false)
  assert.equal(res1.reason, 'unsupported_browser')

  const envMissingMediaDevices = {
    window: { isSecureContext: true, location: { protocol: 'https:', hostname: 'example.com' } },
    navigator: {},
    MediaRecorder: FakeMediaRecorder
  }
  const res2 = checkAudioRecordingSupport(envMissingMediaDevices)
  assert.equal(res2.supported, false)
  assert.equal(res2.reason, 'unsupported_browser')
})

test('requestAudioStream falls back to { audio: true } on OverconstrainedError', async () => {
  const calls = []
  const fakeStream = { id: 'fallback-stream' }
  const env = {
    window: { isSecureContext: true, location: { protocol: 'https:', hostname: 'example.com' } },
    navigator: {
      mediaDevices: {
        getUserMedia: async (constraints) => {
          calls.push(constraints)
          if (calls.length === 1) {
            const err = new Error('sampleRate not supported')
            err.name = 'OverconstrainedError'
            throw err
          }
          return fakeStream
        }
      }
    },
    MediaRecorder: FakeMediaRecorder
  }

  const stream = await requestAudioStream({ channelCount: 1, sampleRate: 16000 }, env)
  assert.equal(stream, fakeStream)
  assert.equal(calls.length, 2)
  assert.deepEqual(calls[0], { audio: { channelCount: 1, sampleRate: 16000 } })
  assert.deepEqual(calls[1], { audio: true })
})

test('requestAudioStream throws SecurityError cleanly in insecure HTTP context', async () => {
  const env = {
    window: { isSecureContext: false, location: { protocol: 'http:', hostname: '10.0.0.5' } },
    navigator: {},
    MediaRecorder: FakeMediaRecorder
  }

  await assert.rejects(
    () => requestAudioStream({ channelCount: 1 }, env),
    (err) => err.name === 'SecurityError' && /HTTPS/.test(err.message)
  )
})

test('formatAudioRecordError returns friendly messages for common browser recording errors', () => {
  const secureEnv = {
    window: { isSecureContext: true, location: { protocol: 'https:', hostname: 'example.com' } },
    navigator: { mediaDevices: { getUserMedia: async () => ({}) } }
  }

  assert.match(formatAudioRecordError({ name: 'NotAllowedError' }, secureEnv), /麦克风权限/)
  assert.match(formatAudioRecordError({ name: 'NotFoundError' }, secureEnv), /未检测到可用的麦克风/)
  assert.match(formatAudioRecordError({ name: 'NotReadableError' }, secureEnv), /占用/)
  assert.match(formatAudioRecordError({ name: 'SecurityError' }, secureEnv), /HTTPS/)
})

test('Speakers.vue and VoiceClones.vue use audioRecorder helpers and show clear alert when recording is unavailable', () => {
  const speakersPath = path.resolve(__dirname, '../src/views/user/Speakers.vue')
  const speakersSrc = fs.readFileSync(speakersPath, 'utf8')
  assert.match(speakersSrc, /from '\.\.\/\.\.\/utils\/audioRecorder'/)
  assert.match(speakersSrc, /v-if="!canRecord && recordUnavailableReason"/)
  assert.doesNotMatch(speakersSrc, /await navigator\.mediaDevices\.getUserMedia/)

  const voiceClonesPath = path.resolve(__dirname, '../src/views/user/VoiceClones.vue')
  const voiceClonesSrc = fs.readFileSync(voiceClonesPath, 'utf8')
  assert.match(voiceClonesSrc, /from '\.\.\/\.\.\/utils\/audioRecorder'/)
  assert.match(voiceClonesSrc, /v-if="!recordCapability\.supported"/)
  assert.doesNotMatch(voiceClonesSrc, /await navigator\.mediaDevices\.getUserMedia/)
})
