<template>
  <div class="mobile-more-page">
    <van-cell-group inset title="常用功能">
      <van-cell
        v-for="item in commonItems"
        :key="item.path"
        :title="item.title"
        :label="item.desc"
        is-link
        @click="go(item.path)"
      />
    </van-cell-group>

    <template v-if="authStore.isAdmin">
      <van-cell-group inset title="服务配置">
        <van-cell
          v-for="item in serviceItems"
          :key="item.path"
          :title="item.title"
          is-link
          @click="go(item.path)"
        />
      </van-cell-group>

      <van-cell-group inset title="AI 配置">
        <van-cell
          v-for="item in aiItems"
          :key="item.path"
          :title="item.title"
          is-link
          @click="go(item.path)"
        />
      </van-cell-group>

      <van-cell-group inset title="系统管理">
        <van-cell
          v-for="item in systemItems"
          :key="item.path"
          :title="item.title"
          is-link
          @click="go(item.path)"
        />
      </van-cell-group>
    </template>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../../stores/auth'

const router = useRouter()
const authStore = useAuthStore()

const commonItems = computed(() => {
  if (authStore.isAdmin) {
    return [
      { title: '配置向导', desc: '首次部署推荐从这里开始', path: '/admin/config-wizard' },
      { title: '资源池统计', desc: '查看系统资源池使用情况', path: '/admin/pool-stats' }
    ]
  }

  return [
    { title: '我的角色', desc: '管理个人角色模板', path: '/user/roles' },
    { title: '声音复刻', desc: '管理声音复刻任务', path: '/voice-clones' },
    { title: '我的知识库', desc: '管理知识库文档', path: '/user/knowledge-bases' }
  ]
})

const serviceItems = [
  { title: 'OTA 配置', path: '/admin/ota-config' },
  { title: 'MQTT 配置', path: '/admin/mqtt-config' },
  { title: 'MQTT Server 配置', path: '/admin/mqtt-server-config' },
  { title: 'UDP 配置', path: '/admin/udp-config' },
  { title: 'MCP 配置', path: '/admin/mcp-config' },
  { title: 'MCP 市场', path: '/admin/mcp-market' },
  { title: '声纹识别配置', path: '/admin/speaker-config' },
  { title: '聊天设置', path: '/admin/chat-settings' }
]

const aiItems = [
  { title: 'VAD 配置', path: '/admin/vad-config' },
  { title: 'ASR 配置', path: '/admin/asr-config' },
  { title: 'LLM 配置', path: '/admin/llm-config' },
  { title: 'TTS 配置', path: '/admin/tts-config' },
  { title: 'Vision 配置', path: '/admin/vision-config' },
  { title: 'Memory 配置', path: '/admin/memory-config' },
  { title: '知识库检索配置', path: '/admin/knowledge-search-config' }
]

const systemItems = [
  { title: '全局角色', path: '/admin/global-roles' },
  { title: '用户管理', path: '/admin/users' },
  { title: '设备管理', path: '/admin/devices' },
  { title: '智能体管理', path: '/admin/agents' },
  { title: '定时任务', path: '/admin/scheduler-tasks' }
]

const go = (path) => {
  router.push(path)
}
</script>

<style scoped>
.mobile-more-page {
  padding: 12px 0 24px;
}

:deep(.van-cell-group) {
  margin-bottom: 12px;
  border-radius: 10px;
  overflow: hidden;
}

:deep(.van-cell-group__title) {
  font-weight: 600;
  color: #323233;
}
</style>
