<template>
  <el-form
    ref="formRef"
    :model="form"
    :rules="rules"
    :label-position="labelPosition"
    :label-width="labelWidth"
    class="shared-agent-form"
  >
    <el-form-item v-if="isAdmin" label="所属用户" prop="user_id">
      <el-select
        v-model="form.user_id"
        placeholder="请选择所属用户"
        filterable
        style="width: 100%"
        :loading="loading.users"
      >
        <el-option
          v-for="user in users"
          :key="user.id"
          :label="userLabel(user)"
          :value="user.id"
        />
      </el-select>
    </el-form-item>

    <div class="agent-form-grid">
      <el-form-item label="智能体名称" prop="name">
        <el-input v-model="form.name" placeholder="请输入管理侧显示的智能体名称" maxlength="50" show-word-limit />
      </el-form-item>
      <el-form-item label="智能体昵称" prop="nickname">
        <el-input v-model="form.nickname" placeholder="给大模型使用，例如：小辉" maxlength="50" show-word-limit />
      </el-form-item>
    </div>

    <el-form-item label="角色介绍" prop="custom_prompt">
      <el-input
        v-model="form.custom_prompt"
        type="textarea"
        :rows="4"
        placeholder="请输入角色介绍 / 系统提示词"
        maxlength="10000"
        show-word-limit
      />
    </el-form-item>

    <el-form-item label="关联知识库" prop="knowledge_base_ids">
      <el-select
        v-model="form.knowledge_base_ids"
        multiple
        filterable
        collapse-tags
        collapse-tags-tooltip
        clearable
        placeholder="请选择要关联的知识库"
        style="width: 100%"
        :loading="loading.knowledgeBases"
        :disabled="isAdmin && !form.user_id"
      >
        <el-option
          v-for="kb in knowledgeBases"
          :key="kb.id"
          :label="kb.name || `知识库 #${kb.id}`"
          :value="kb.id"
        />
      </el-select>
    </el-form-item>

    <div class="agent-form-grid">
      <el-form-item label="语言模型" prop="llm_config_id">
        <el-select
          v-model="form.llm_config_id"
          placeholder="请选择语言模型"
          style="width: 100%"
          clearable
          filterable
          :loading="loading.configs"
        >
          <el-option
            v-for="config in llmConfigs"
            :key="config.config_id"
            :label="config.is_default ? `${config.name} (默认)` : config.name"
            :value="config.config_id"
          >
            <div class="config-option">
              <span>{{ config.name }}</span>
              <span>{{ config.provider || config.config_id }}</span>
            </div>
          </el-option>
        </el-select>
      </el-form-item>

      <el-form-item label="TTS配置" prop="tts_config_id">
        <el-select
          v-model="form.tts_config_id"
          placeholder="请选择TTS配置"
          style="width: 100%"
          clearable
          filterable
          :loading="loading.configs"
        >
          <el-option
            v-for="config in ttsConfigs"
            :key="config.config_id"
            :label="config.is_default ? `${config.name} (默认)` : config.name"
            :value="config.config_id"
          >
            <div class="config-option">
              <span>{{ config.name }}</span>
              <span>{{ config.provider || config.config_id }}</span>
            </div>
          </el-option>
        </el-select>
      </el-form-item>
    </div>

    <el-form-item v-if="form.tts_config_id" label="TTS音色" prop="voice">
      <div style="display: flex; gap: 8px; width: 100%;">
        <el-select
          v-model="form.voice"
          placeholder="请选择或输入精确音色值"
          style="flex: 1;"
          filterable
          allow-create
          default-first-option
          reserve-keyword
          clearable
          :loading="loading.voices"
          :filter-method="filterVoice"
        >
          <el-option
            v-for="voice in visibleVoiceOptions"
            :key="voice.value"
            :label="voice.label || voice.value"
            :value="voice.value"
          >
            <div class="voice-option">
              <span>{{ voice.label || voice.value }}</span>
              <span>{{ voice.value }}</span>
            </div>
          </el-option>
        </el-select>
        <VoicePreviewButton
          v-if="supportsAliyunPreview(selectedTtsConfig?.provider)"
          :provider="selectedTtsConfig?.provider || 'aliyun_qwen'"
          :config-id="form.tts_config_id"
          :voice="form.voice"
          :instruction="form.voice_prompt"
          :is-admin="isAdmin"
        />
      </div>
    </el-form-item>

    <el-form-item
      v-if="form.tts_config_id && (selectedTtsConfig?.provider === 'aliyun_qwen' || selectedTtsConfig?.provider === 'cosyvoice' || selectedOpenAIOMNIEngine)"
      label="音色 Prompt (合成提示词)"
      prop="voice_prompt"
    >
      <el-input
        v-model="form.voice_prompt"
        type="textarea"
        :rows="3"
        placeholder="可输入文本描述想要合成出的语音风格或情感，例如：'一个温柔的前台女声，说话稍微快一点，带着活泼开朗的语气。'"
      />
    </el-form-item>

    <div v-if="cloneVoices.length" class="clone-voice-row" v-loading="loading.cloneVoices">
      <button
        v-for="clone in cloneVoices"
        :key="clone.id"
        type="button"
        class="clone-voice-button"
        :class="{ active: form.tts_config_id === clone.tts_config_id && form.voice === clone.provider_voice_id }"
        :title="`${clone.tts_config_name || clone.tts_config_id} · ${clone.provider_voice_id}`"
        @click="applyCloneVoice(clone)"
      >
        {{ clone.name || clone.provider_voice_id }}
      </button>
    </div>

    <div class="agent-form-grid agent-form-grid-three">
      <el-form-item label="语音识别速度" prop="asr_speed">
        <el-select v-model="form.asr_speed" style="width: 100%">
          <el-option label="正常" value="normal" />
          <el-option label="耐心" value="patient" />
          <el-option label="快速" value="fast" />
        </el-select>
      </el-form-item>
      <el-form-item label="记忆模式" prop="memory_mode">
        <el-select v-model="form.memory_mode" style="width: 100%">
          <el-option label="无记忆" value="none" />
          <el-option label="短记忆" value="short" />
          <el-option label="长记忆" value="long" />
        </el-select>
      </el-form-item>
      <el-form-item label="声纹聊天限制" prop="speaker_chat_mode">
        <el-select v-model="form.speaker_chat_mode" style="width: 100%">
          <el-option label="关闭" value="off" />
          <el-option label="仅命中声纹时允许聊天" value="identified_only" />
        </el-select>
      </el-form-item>
    </div>

    <el-form-item label="MCP服务">
      <el-select
        v-model="selectedMcpServices"
        multiple
        filterable
        collapse-tags
        collapse-tags-tooltip
        clearable
        style="width: 100%"
        placeholder="留空则使用全部已启用服务"
        :loading="loading.mcpServices"
      >
        <el-option
          v-for="serviceName in mcpServiceOptions"
          :key="serviceName"
          :label="serviceName"
          :value="serviceName"
        />
      </el-select>
    </el-form-item>

    <div class="openclaw-panel">
      <div class="openclaw-switch-row">
        <span>允许进入 OpenClaw 模式</span>
        <el-switch v-model="form.openclaw_allowed" />
      </div>
      <div class="agent-form-grid">
        <el-form-item label="OpenClaw进入关键词">
          <el-select
            v-model="form.openclaw_enter_keywords"
            multiple
            filterable
            allow-create
            default-first-option
            clearable
            style="width: 100%"
            placeholder="输入后回车，可添加多个关键词"
          />
        </el-form-item>
        <el-form-item label="OpenClaw退出关键词">
          <el-select
            v-model="form.openclaw_exit_keywords"
            multiple
            filterable
            allow-create
            default-first-option
            clearable
            style="width: 100%"
            placeholder="输入后回车，可添加多个关键词"
          />
        </el-form-item>
      </div>
    </div>

  </el-form>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import VoicePreviewButton from './VoicePreviewButton.vue'
import { supportsAliyunPreview } from './voicePreview'
import {
  buildAgentPayload,
  useAgentFormOptions
} from '../../composables/useAgentFormOptions'

const props = defineProps({
  modelValue: {
    type: Object,
    required: true
  },
  isAdmin: {
    type: Boolean,
    default: false
  },
  mode: {
    type: String,
    default: 'create'
  },
  labelPosition: {
    type: String,
    default: 'top'
  },
  labelWidth: {
    type: String,
    default: '120px'
  }
})

const emit = defineEmits(['update:modelValue'])

const form = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value)
})

const targetUserId = computed(() => props.isAdmin ? Number(form.value.user_id || 0) : 0)

const {
  users,
  llmConfigs,
  ttsConfigs,
  knowledgeBases,
  mcpServiceOptions,
  voiceOptions,
  cloneVoices,
  loading,
  loadUsers,
  loadConfigs,
  loadKnowledgeBases,
  loadMcpServiceOptions,
  loadVoiceOptions,
  loadCloneVoices
} = useAgentFormOptions({
  isAdmin: computed(() => props.isAdmin),
  targetUserId
})

const rules = computed(() => ({
  user_id: props.isAdmin ? [{ required: true, message: '请选择所属用户', trigger: 'change' }] : [],
  name: [
    { required: true, message: '请输入智能体名称', trigger: 'blur' },
    { min: 1, max: 50, message: '长度不能超过 50 个字符', trigger: 'blur' }
  ],
  nickname: [
    { required: true, message: '请输入智能体昵称', trigger: 'blur' },
    { min: 1, max: 50, message: '长度不能超过 50 个字符', trigger: 'blur' }
  ],
  asr_speed: [{ required: true, message: '请选择语音识别速度', trigger: 'change' }],
  memory_mode: [{ required: true, message: '请选择记忆模式', trigger: 'change' }],
  speaker_chat_mode: [{ required: true, message: '请选择声纹聊天限制', trigger: 'change' }]
}))

const formRef = ref(null)
const MAX_VISIBLE_VOICE_OPTIONS = 300
const voiceSearchKeyword = ref('')
const filteredVoiceOptions = ref([])
const previousTtsConfigId = ref(null)
const suppressTtsConfigWatch = ref(false)

const selectedTtsConfig = computed(() => {
  return ttsConfigs.value.find((config) => config.config_id === form.value.tts_config_id) || null
})

const selectedOpenAIOMNIEngine = computed(() => {
  if (selectedTtsConfig.value?.provider !== 'openai') return false
  const engine = selectedTtsConfig.value?.engine || ''
  return engine.includes('omni')
})

const visibleVoiceOptions = computed(() => {
  const selected = String(form.value.voice || '').trim()
  const options = Array.isArray(filteredVoiceOptions.value) ? [...filteredVoiceOptions.value] : []
  if (selected && !options.some((item) => item.value === selected)) {
    options.unshift({ label: selected, value: selected })
  }
  return options.slice(0, MAX_VISIBLE_VOICE_OPTIONS)
})

const selectedMcpServices = computed({
  get: () => String(form.value.mcp_service_names || '')
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean),
  set: (items) => {
    form.value.mcp_service_names = Array.isArray(items) ? items.join(',') : ''
  }
})

const userLabel = (user) => {
  const name = user?.username || user?.name || `用户 #${user?.id}`
  return `${name} (ID: ${user?.id})`
}

const loadTargetUserOptions = async () => {
  await Promise.all([
    loadKnowledgeBases().catch(() => []),
    loadCloneVoices(form.value.tts_config_id || '').catch(() => [])
  ])
  const validKnowledgeBaseIds = new Set(knowledgeBases.value.map((item) => Number(item.id)))
  if (validKnowledgeBaseIds.size) {
    form.value.knowledge_base_ids = (form.value.knowledge_base_ids || []).filter((id) => validKnowledgeBaseIds.has(Number(id)))
  } else if (props.isAdmin && targetUserId.value) {
    form.value.knowledge_base_ids = []
  }
}

const applyDefaultConfigs = () => {
  if (props.mode !== 'create') return
  if (!form.value.llm_config_id) {
    const defaultLlm = llmConfigs.value.find((config) => config.is_default)
    if (defaultLlm) form.value.llm_config_id = defaultLlm.config_id
  }
  if (!form.value.tts_config_id) {
    const defaultTts = ttsConfigs.value.find((config) => config.is_default)
    if (defaultTts) form.value.tts_config_id = defaultTts.config_id
  }
}

const limitVoiceOptions = (voices) => {
  const list = Array.isArray(voices) ? voices : []
  const visible = list.slice(0, MAX_VISIBLE_VOICE_OPTIONS)
  const selected = String(form.value.voice || '').trim()
  if (selected && !visible.some((voice) => voice.value === selected)) {
    const selectedOption = list.find((voice) => voice.value === selected)
    if (selectedOption) visible.unshift(selectedOption)
  }
  return visible
}

const filterVoice = (value = '') => {
  voiceSearchKeyword.value = value
  const keyword = String(value || '').trim().toLowerCase()
  if (!keyword) {
    filteredVoiceOptions.value = limitVoiceOptions(voiceOptions.value)
    return
  }

  const matchedVoices = voiceOptions.value.filter((voice) => {
    const label = String(voice.label || '').toLowerCase()
    const voiceValue = String(voice.value || '').toLowerCase()
    return label.includes(keyword) || voiceValue.includes(keyword)
  })
  filteredVoiceOptions.value = limitVoiceOptions(matchedVoices)
}

const syncFilteredVoiceOptions = () => {
  filterVoice(voiceSearchKeyword.value)
}

const refreshVoiceOptions = async ({ clearInvalid = true, previousConfigId = previousTtsConfigId.value } = {}) => {
  const provider = selectedTtsConfig.value?.provider
  if (!form.value.tts_config_id || !provider) {
    voiceOptions.value = []
    filteredVoiceOptions.value = []
    form.value.voice = null
    previousTtsConfigId.value = null
    return
  }

  const previousConfig = ttsConfigs.value.find((config) => config.config_id === previousConfigId)
  if (clearInvalid && previousConfig?.provider && previousConfig.provider !== provider) {
    form.value.voice = null
  }

  const voices = await loadVoiceOptions({ provider, configId: form.value.tts_config_id }).catch(() => [])
  await loadCloneVoices(form.value.tts_config_id).catch(() => [])
  if (clearInvalid && form.value.voice && voices.length) {
    const exists = voices.some((voice) => voice.value === form.value.voice)
    if (!exists) form.value.voice = null
  }
  syncFilteredVoiceOptions()
  previousTtsConfigId.value = form.value.tts_config_id
}

const applyCloneVoice = async (clone) => {
  if (!clone?.tts_config_id || !clone?.provider_voice_id) return
  await setTtsConfig(clone.tts_config_id, { clearInvalid: false })
  form.value.voice = clone.provider_voice_id
}

const setTtsConfig = async (configId, options = {}) => {
  const previousConfigId = form.value.tts_config_id
  suppressTtsConfigWatch.value = true
  form.value.tts_config_id = configId || null
  try {
    await refreshVoiceOptions({
      clearInvalid: true,
      previousConfigId,
      ...options
    })
  } finally {
    Promise.resolve().then(() => {
      suppressTtsConfigWatch.value = false
    })
  }
}

const reloadOptions = async () => {
  await Promise.all([
    props.isAdmin ? loadUsers().catch(() => []) : Promise.resolve([]),
    loadConfigs(),
    loadMcpServiceOptions().catch(() => [])
  ])
  applyDefaultConfigs()
  await loadTargetUserOptions()
  await refreshVoiceOptions({ clearInvalid: false })
}

watch(
  () => form.value.user_id,
  async (next, prev) => {
    if (!props.isAdmin || next === prev) return
    form.value.knowledge_base_ids = []
    form.value.voice = null
    previousTtsConfigId.value = null
    await loadTargetUserOptions()
    await refreshVoiceOptions({ clearInvalid: true })
  }
)

watch(
  () => form.value.tts_config_id,
  async (next, prev) => {
    if (next === prev) return
    if (suppressTtsConfigWatch.value) return
    await refreshVoiceOptions({ clearInvalid: true, previousConfigId: prev })
  }
)

onMounted(() => {
  reloadOptions()
})

const validate = () => formRef.value?.validate?.()
const resetFields = () => formRef.value?.resetFields?.()
const clearValidate = () => formRef.value?.clearValidate?.()
const buildPayload = () => buildAgentPayload(form.value, { isAdmin: props.isAdmin })
const hasLlmConfig = (configId) => !configId || llmConfigs.value.some((config) => config.config_id === configId)
const hasTtsConfig = (configId) => !configId || ttsConfigs.value.some((config) => config.config_id === configId)

defineExpose({
  validate,
  resetFields,
  clearValidate,
  reloadOptions,
  refreshVoiceOptions,
  setTtsConfig,
  buildPayload,
  hasLlmConfig,
  hasTtsConfig
})
</script>

<style scoped>
.shared-agent-form {
  display: grid;
  gap: 2px;
}

.agent-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.agent-form-grid-three {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.config-option,
.voice-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.config-option span:last-child,
.voice-option span:last-child {
  color: #909399;
  font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
}

.clone-voice-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: -4px 0 18px;
}

.clone-voice-button {
  border: 1px solid #dcdfe6;
  border-radius: 8px;
  background: #fff;
  color: #606266;
  cursor: pointer;
  font-size: 12px;
  line-height: 1;
  padding: 8px 10px;
}

.clone-voice-button.active {
  border-color: #409eff;
  background: #ecf5ff;
  color: #1677d2;
}

.openclaw-panel {
  margin-bottom: 18px;
  padding: 14px;
  border: 1px solid #ebeef5;
  border-radius: 8px;
  background: #fafafa;
}

.openclaw-switch-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
  color: #303133;
  font-size: 14px;
  font-weight: 600;
}

@media (max-width: 900px) {
  .agent-form-grid,
  .agent-form-grid-three {
    grid-template-columns: 1fr;
  }
}
</style>
