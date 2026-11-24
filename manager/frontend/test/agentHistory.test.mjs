import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

test('AgentHistory.vue explicitly renders user role and filters out non-dialogue tool messages', () => {
  const viewPath = path.resolve(__dirname, '../src/views/user/AgentHistory.vue')
  const content = fs.readFileSync(viewPath, 'utf8')

  assert.match(content, /<template v-else-if="message\.role === 'user'">/, 'user bubble must explicitly guard message.role === user instead of catch-all v-else')
  assert.doesNotMatch(content, /<!-- 右侧：用户消息 -->\s*<template v-else>/, 'tool messages must never fall into user bubble via catch-all v-else')
  assert.match(content, /msg\.role === 'user' \|\| msg\.role === 'assistant'/, 'loadMessages should filter out internal tool/system messages')
})
