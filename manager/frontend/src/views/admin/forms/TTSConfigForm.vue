<template>
  <el-form ref="formRef" :model="model" :rules="rules" label-width="140px">
    <el-form-item label="提供商" prop="provider">
      <el-select v-model="model.provider" placeholder="请选择提供商" style="width: 100%">
        <el-option
          v-for="provider in TTS_PROVIDER_OPTIONS"
          :key="provider.value"
          :label="provider.label"
          :value="provider.value"
        />
      </el-select>
      <div v-if="currentProviderMeta.docUrl" class="form-link-tip">
        文档地址：
        <el-link :href="currentProviderMeta.docUrl" target="_blank" type="primary">
          {{ currentProviderMeta.docUrl }}
        </el-link>
      </div>
    </el-form-item>
    <el-form-item label="配置名称" prop="name">
      <el-input v-model="model.name" placeholder="请输入配置名称" />
    </el-form-item>
    <el-form-item label="配置ID" prop="config_id">
      <el-input v-model="model.config_id" placeholder="请输入唯一的配置ID" />
    </el-form-item>

    <template v-if="model.provider === 'doubao_ws'">
      <el-form-item label="应用ID" prop="doubao_ws.appid">
        <el-input v-model="model.doubao_ws.appid" placeholder="请输入应用ID" />
      </el-form-item>
      <el-form-item label="访问令牌" prop="doubao_ws.access_token">
        <el-input v-model="model.doubao_ws.access_token" placeholder="请输入访问令牌" type="password" show-password />
        <div v-if="currentProviderMeta.keyUrl" class="form-link-tip">
           获取密钥：
          <el-link :href="currentProviderMeta.keyUrl" target="_blank" type="primary">
            {{ currentProviderMeta.keyUrl }}
          </el-link>
        </div>
      </el-form-item>
      <el-form-item label="集群" prop="doubao_ws.cluster">
        <el-input v-model="model.doubao_ws.cluster" placeholder="请输入集群名称" />
      </el-form-item>
      <el-form-item label="音色" prop="doubao_ws.voice">
        <el-select
          v-model="model.doubao_ws.voice"
          placeholder="请选择音色"
          style="width: 100%"
          filterable
          :loading="voiceLoading"
          :disabled="voiceLoading"
          allow-create
          default-first-option
        >
          <el-option v-for="option in voiceOptionsList" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="WebSocket主机" prop="doubao_ws.ws_host">
        <el-input v-model="model.doubao_ws.ws_host" placeholder="请输入WebSocket主机地址" />
      </el-form-item>
      <el-form-item label="使用流式" prop="doubao_ws.use_stream">
        <el-switch v-model="model.doubao_ws.use_stream" />
      </el-form-item>
    </template>

    <template v-if="model.provider === 'edge'">
      <el-form-item label="音色" prop="edge.voice">
        <el-select
          v-model="model.edge.voice"
          placeholder="请选择音色"
          style="width: 100%"
          filterable
          :loading="voiceLoading"
          :disabled="voiceLoading"
          allow-create
          default-first-option
        >
          <el-option v-for="option in voiceOptionsList" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="语速" prop="edge.rate">
        <el-input v-model="model.edge.rate" placeholder="请输入语速（如：+0%）" />
      </el-form-item>
      <el-form-item label="音量" prop="edge.volume">
        <el-input v-model="model.edge.volume" placeholder="请输入音量（如：+0%）" />
      </el-form-item>
      <el-form-item label="音调" prop="edge.pitch">
        <el-input v-model="model.edge.pitch" placeholder="请输入音调（如：+0Hz）" />
      </el-form-item>
      <el-form-item label="连接超时" prop="edge.connect_timeout">
        <el-input-number v-model="model.edge.connect_timeout" :min="1" :max="60" style="width: 100%" />
      </el-form-item>
      <el-form-item label="接收超时" prop="edge.receive_timeout">
        <el-input-number v-model="model.edge.receive_timeout" :min="1" :max="300" style="width: 100%" />
      </el-form-item>
    </template>

    <template v-if="model.provider === 'edge_offline'">
      <el-form-item label="服务器URL" prop="edge_offline.server_url">
        <el-input v-model="model.edge_offline.server_url" placeholder="请输入服务器URL" />
      </el-form-item>
      <el-form-item label="超时时间" prop="edge_offline.timeout">
        <el-input-number v-model="model.edge_offline.timeout" :min="1" :max="300" style="width: 100%" />
      </el-form-item>
      <el-form-item label="采样率" prop="edge_offline.sample_rate">
        <el-input-number v-model="model.edge_offline.sample_rate" :min="8000" :max="48000" style="width: 100%" />
      </el-form-item>
      <el-form-item label="声道数" prop="edge_offline.channels">
        <el-input-number v-model="model.edge_offline.channels" :min="1" :max="8" style="width: 100%" />
      </el-form-item>
      <el-form-item label="帧时长" prop="edge_offline.frame_duration">
        <el-input-number v-model="model.edge_offline.frame_duration" :min="1" :max="100" style="width: 100%" />
      </el-form-item>
    </template>

    <template v-if="model.provider === 'aliyun_qwen'">
      <el-form-item label="API Key" prop="qwen_tts.api_key">
        <el-input v-model="model.qwen_tts.api_key" placeholder="请输入API Key" type="password" show-password />
        <div v-if="currentProviderMeta.keyUrl" class="form-link-tip">
           获取密钥：
          <el-link :href="currentProviderMeta.keyUrl" target="_blank" type="primary">
            {{ currentProviderMeta.keyUrl }}
          </el-link>
        </div>
      </el-form-item>
      <el-form-item label="地域" prop="qwen_tts.region">
        <el-select v-model="model.qwen_tts.region" placeholder="请选择地域" style="width: 100%">
          <el-option label="北京" value="beijing" />
          <el-option label="新加坡" value="singapore" />
        </el-select>
      </el-form-item>
      <el-form-item label="模型" prop="qwen_tts.model">
        <el-input v-model="model.qwen_tts.model" placeholder="qwen3-tts-flash" />
      </el-form-item>
      <el-form-item label="音色" prop="qwen_tts.voice">
        <el-input v-model="model.qwen_tts.voice" placeholder="Cherry" />
      </el-form-item>
      <el-form-item label="语种" prop="qwen_tts.language_type">
        <el-select v-model="model.qwen_tts.language_type" placeholder="请选择语种" style="width: 100%">
          <el-option label="自动" value="Auto" />
          <el-option label="中文" value="Chinese" />
          <el-option label="英文" value="English" />
        </el-select>
      </el-form-item>
      <el-form-item label="使用流式" prop="qwen_tts.stream">
        <el-switch v-model="model.qwen_tts.stream" />
      </el-form-item>
      <el-form-item label="帧时长" prop="qwen_tts.frame_duration">
        <el-input-number v-model="model.qwen_tts.frame_duration" :min="1" :max="1000" style="width: 100%" />
      </el-form-item>
    </template>

    <template v-if="model.provider === 'zhipu'">
      <el-form-item label="API Key" prop="zhipu.api_key">
        <el-input v-model="model.zhipu.api_key" placeholder="请输入API Key" type="password" show-password />
        <div v-if="currentProviderMeta.keyUrl" class="form-link-tip">
           获取密钥：
          <el-link :href="currentProviderMeta.keyUrl" target="_blank" type="primary">
            {{ currentProviderMeta.keyUrl }}
          </el-link>
        </div>
      </el-form-item>
      <el-form-item label="API URL" prop="zhipu.api_url">
        <el-input v-model="model.zhipu.api_url" placeholder="https://open.bigmodel.cn/api/paas/v4/audio/speech" />
      </el-form-item>
      <el-form-item label="模型" prop="zhipu.model">
        <el-input v-model="model.zhipu.model" placeholder="glm-tts" />
      </el-form-item>
      <el-form-item label="音色" prop="zhipu.voice">
        <el-select
          v-model="model.zhipu.voice"
          placeholder="请选择音色"
          style="width: 100%"
          filterable
          :loading="voiceLoading"
          :disabled="voiceLoading"
        >
          <el-option v-for="option in voiceOptionsList" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="响应格式" prop="zhipu.response_format">
        <el-select v-model="model.zhipu.response_format" placeholder="请选择响应格式" style="width: 100%">
          <el-option label="WAV" value="wav" />
          <el-option label="PCM" value="pcm" />
        </el-select>
      </el-form-item>
      <el-form-item label="音量" prop="zhipu.volume">
        <el-input-number v-model="model.zhipu.volume" :min="0" :max="10" :step="0.1" style="width: 100%" placeholder="0-10，默认1.0" />
      </el-form-item>
      <el-form-item label="语速" prop="zhipu.speed">
        <el-input-number v-model="model.zhipu.speed" :min="0.5" :max="2.0" :step="0.1" style="width: 100%" placeholder="0.5-2.0，默认1.0" />
      </el-form-item>
      <el-form-item label="使用流式" prop="zhipu.stream">
        <el-switch v-model="model.zhipu.stream" />
      </el-form-item>
      <el-form-item v-if="model.zhipu.stream" label="编码格式" prop="zhipu.encode_format">
        <el-select v-model="model.zhipu.encode_format" placeholder="请选择编码格式" style="width: 100%">
          <el-option label="Base64" value="base64" />
          <el-option label="Hex" value="hex" />
        </el-select>
      </el-form-item>
      <el-form-item label="帧时长" prop="zhipu.frame_duration">
        <el-input-number v-model="model.zhipu.frame_duration" :min="1" :max="1000" style="width: 100%" placeholder="毫秒" />
      </el-form-item>
    </template>

    <template v-if="model.provider === 'minimax'">
      <el-form-item label="API Key" prop="minimax.api_key">
        <el-input v-model="model.minimax.api_key" placeholder="请输入API Key" type="password" show-password />
        <div v-if="currentProviderMeta.keyUrl" class="form-link-tip">
           获取密钥：
          <el-link :href="currentProviderMeta.keyUrl" target="_blank" type="primary">
            {{ currentProviderMeta.keyUrl }}
          </el-link>
        </div>
      </el-form-item>
      <el-form-item label="模型" prop="minimax.model">
        <el-input v-model="model.minimax.model" placeholder="speech-2.8-hd" />
      </el-form-item>
      <el-form-item label="音色" prop="minimax.voice">
        <el-select
          v-model="model.minimax.voice"
          placeholder="请选择音色"
          style="width: 100%"
          filterable
          :loading="voiceLoading"
          :disabled="voiceLoading"
          allow-create
          default-first-option
        >
          <el-option v-for="option in voiceOptionsList" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="语速" prop="minimax.speed">
        <el-input-number v-model="model.minimax.speed" :min="0.5" :max="2.0" :step="0.1" style="width: 100%" placeholder="0.5-2.0，默认1.0" />
      </el-form-item>
      <el-form-item label="音量" prop="minimax.vol">
        <el-input-number v-model="model.minimax.vol" :min="0" :max="2" :step="0.1" style="width: 100%" placeholder="0-2，默认1.0" />
      </el-form-item>
      <el-form-item label="音调" prop="minimax.pitch">
        <el-input-number v-model="model.minimax.pitch" :min="-12" :max="12" :step="1" style="width: 100%" placeholder="-12到12，默认0" />
      </el-form-item>
      <el-form-item label="采样率" prop="minimax.sample_rate">
        <el-input-number v-model="model.minimax.sample_rate" :min="8000" :max="48000" :step="1000" style="width: 100%" placeholder="默认32000" />
      </el-form-item>
      <el-form-item label="比特率" prop="minimax.bitrate">
        <el-input-number v-model="model.minimax.bitrate" :min="32000" :max="320000" :step="16000" style="width: 100%" placeholder="默认128000" />
      </el-form-item>
      <el-form-item label="音频格式" prop="minimax.format">
        <el-select v-model="model.minimax.format" placeholder="请选择音频格式" style="width: 100%">
          <el-option label="MP3" value="mp3" />
          <el-option label="WAV" value="wav" />
          <el-option label="PCM" value="pcm" />
        </el-select>
      </el-form-item>
      <el-form-item label="声道数" prop="minimax.channel">
        <el-input-number v-model="model.minimax.channel" :min="1" :max="2" style="width: 100%" placeholder="默认1" />
      </el-form-item>
    </template>

    <template v-if="model.provider === 'openai'">
      <el-form-item label="API Key" prop="openai.api_key">
        <el-input v-model="model.openai.api_key" placeholder="请输入API Key" type="password" show-password />
        <div v-if="currentProviderMeta.keyUrl" class="form-link-tip">
           获取密钥：
          <el-link :href="currentProviderMeta.keyUrl" target="_blank" type="primary">
            {{ currentProviderMeta.keyUrl }}
          </el-link>
        </div>
      </el-form-item>
      <el-form-item label="API URL" prop="openai.api_url">
        <el-input v-model="model.openai.api_url" placeholder="请输入API URL（默认：https://api.openai.com/v1/audio/speech）" />
      </el-form-item>
      <el-form-item label="模型" prop="openai.model">
        <el-input v-model="model.openai.model" placeholder="请输入模型（默认：tts-1）" />
      </el-form-item>
      <el-form-item label="音色" prop="openai.voice">
        <el-select
          v-model="model.openai.voice"
          placeholder="请选择音色"
          style="width: 100%"
          filterable
          :loading="voiceLoading"
          :disabled="voiceLoading"
        >
          <el-option v-for="option in voiceOptionsList" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="响应格式" prop="openai.response_format">
        <el-select v-model="model.openai.response_format" placeholder="请选择响应格式" style="width: 100%">
          <el-option label="MP3" value="mp3" />
          <el-option label="Opus" value="opus" />
          <el-option label="AAC" value="aac" />
          <el-option label="FLAC" value="flac" />
          <el-option label="WAV" value="wav" />
          <el-option label="PCM" value="pcm" />
        </el-select>
      </el-form-item>
      <el-form-item label="语速" prop="openai.speed">
        <el-input-number v-model="model.openai.speed" :min="0.25" :max="4.0" :step="0.1" style="width: 100%" placeholder="0.25-4.0，默认1.0" />
      </el-form-item>
      <el-form-item label="使用流式" prop="openai.stream">
        <el-switch v-model="model.openai.stream" />
      </el-form-item>
      <el-form-item label="帧时长" prop="openai.frame_duration">
        <el-input-number v-model="model.openai.frame_duration" :min="1" :max="1000" style="width: 100%" placeholder="毫秒" />
      </el-form-item>
    </template>

    <template v-if="model.provider === 'cosyvoice'">
      <el-form-item label="API URL" prop="cosyvoice.api_url">
        <el-input v-model="model.cosyvoice.api_url" placeholder="请输入API URL" />
      </el-form-item>
      <el-form-item label="说话人ID" prop="cosyvoice.spk_id">
        <el-input v-model="model.cosyvoice.spk_id" placeholder="请输入说话人ID" />
      </el-form-item>
      <el-form-item label="帧时长" prop="cosyvoice.frame_duration">
        <el-input-number v-model="model.cosyvoice.frame_duration" :min="1" :max="1000" style="width: 100%" />
      </el-form-item>
      <el-form-item label="目标采样率" prop="cosyvoice.target_sr">
        <el-input-number v-model="model.cosyvoice.target_sr" :min="8000" :max="48000" style="width: 100%" />
      </el-form-item>
      <el-form-item label="音频格式" prop="cosyvoice.audio_format">
        <el-select v-model="model.cosyvoice.audio_format" placeholder="请选择音频格式" style="width: 100%">
          <el-option label="MP3" value="mp3" />
          <el-option label="WAV" value="wav" />
          <el-option label="PCM" value="pcm" />
        </el-select>
      </el-form-item>
      <el-form-item label="指示文本" prop="cosyvoice.instruct_text">
        <el-input v-model="model.cosyvoice.instruct_text" placeholder="请输入指示文本（可选）" />
      </el-form-item>
    </template>
  </el-form>
</template>

<script setup>
import { ref, computed } from 'vue'
import { TTS_PROVIDER_OPTIONS } from './ttsProviderOptions'

const providerMeta = {
  doubao_ws: {
    docUrl: 'https://www.volcengine.com/docs/6561/1257543',
    keyUrl: 'https://www.volcengine.com/docs/6561/196768#q1：哪里可以获取到以下参数appid，cluster，token，authorization-type，secret-key-？'
  },
  edge: {
    docUrl: 'https://github.com/rany2/edge-tts',
    keyUrl: ''
  },
  edge_offline: {
    docUrl: 'https://github.com/rany2/edge-tts',
    keyUrl: ''
  },
  openai: {
    docUrl: 'https://platform.openai.com/docs/guides/text-to-speech',
    keyUrl: 'https://platform.openai.com/api-keys'
  },
  aliyun_qwen: {
    docUrl: 'https://help.aliyun.com/zh/model-studio/',
    keyUrl: 'https://help.aliyun.com/zh/model-studio/get-api-key'
  },
  zhipu: {
    docUrl: 'https://open.bigmodel.cn/dev/api',
    keyUrl: 'https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys'
  },
  minimax: {
    docUrl: 'https://platform.minimaxi.com/docs/guides/speech-t2a-websocket',
    keyUrl: 'https://platform.minimaxi.com/user-center/basic-information/interface-key'
  }
}

const props = defineProps({
  model: { type: Object, required: true },
  rules: { type: Object, default: () => ({}) },
  voiceOptions: { type: Array, default: () => [] },
  voiceLoading: { type: Boolean, default: false }
})

const formRef = ref()
const currentProviderMeta = computed(() => providerMeta[props.model?.provider] || {})
// 保证音色选项始终为数组且响应式，供下拉使用
const voiceOptionsList = computed(() => Array.isArray(props.voiceOptions) ? props.voiceOptions : [])

function getJsonData() {
  const form = props.model
  const config = {}
  switch (form.provider) {
    case 'cosyvoice':
      config.api_url = form.cosyvoice?.api_url
      config.spk_id = form.cosyvoice?.spk_id
      config.frame_duration = form.cosyvoice?.frame_duration
      config.target_sr = form.cosyvoice?.target_sr
      config.audio_format = form.cosyvoice?.audio_format
      config.instruct_text = form.cosyvoice?.instruct_text
      break
    case 'doubao_ws':
      config.appid = form.doubao_ws?.appid
      config.access_token = form.doubao_ws?.access_token
      config.cluster = form.doubao_ws?.cluster
      config.voice = form.doubao_ws?.voice
      config.ws_host = form.doubao_ws?.ws_host
      config.use_stream = form.doubao_ws?.use_stream
      break
    case 'edge':
      config.voice = form.edge?.voice
      config.rate = form.edge?.rate
      config.volume = form.edge?.volume
      config.pitch = form.edge?.pitch
      config.connect_timeout = form.edge?.connect_timeout
      config.receive_timeout = form.edge?.receive_timeout
      break
    case 'edge_offline':
      config.server_url = form.edge_offline?.server_url
      config.timeout = form.edge_offline?.timeout
      config.sample_rate = form.edge_offline?.sample_rate
      config.channels = form.edge_offline?.channels
      config.frame_duration = form.edge_offline?.frame_duration
      break
    case 'aliyun_qwen':
      config.provider = 'aliyun_qwen'
      config.api_key = form.qwen_tts?.api_key
      config.api_url = form.qwen_tts?.api_url
      config.region = form.qwen_tts?.region
      config.model = form.qwen_tts?.model || 'qwen3-tts-flash'
      config.voice = form.qwen_tts?.voice || 'Cherry'
      config.language_type = form.qwen_tts?.language_type || 'Chinese'
      config.stream = form.qwen_tts?.stream
      config.frame_duration = form.qwen_tts?.frame_duration || 60
      break
    case 'openai':
      config.api_key = form.openai?.api_key
      config.api_url = form.openai?.api_url
      config.model = form.openai?.model
      config.voice = form.openai?.voice
      config.response_format = form.openai?.response_format
      config.speed = form.openai?.speed
      config.stream = form.openai?.stream
      config.frame_duration = form.openai?.frame_duration
      break
    case 'zhipu':
      config.provider = 'zhipu'
      config.api_key = form.zhipu?.api_key
      config.api_url = form.zhipu?.api_url || 'https://open.bigmodel.cn/api/paas/v4/audio/speech'
      config.model = form.zhipu?.model || 'glm-tts'
      config.voice = form.zhipu?.voice
      config.response_format = form.zhipu?.response_format
      config.speed = form.zhipu?.speed
      config.volume = form.zhipu?.volume || 1.0
      config.stream = form.zhipu?.stream
      config.encode_format = form.zhipu?.encode_format || 'base64'
      config.frame_duration = form.zhipu?.frame_duration
      break
    case 'minimax':
      config.provider = 'minimax'
      config.api_key = form.minimax?.api_key
      config.model = form.minimax?.model || 'speech-2.8-hd'
      config.voice = form.minimax?.voice || 'male-qn-qingse'
      config.speed = form.minimax?.speed || 1.0
      config.vol = form.minimax?.vol || 1.0
      config.pitch = form.minimax?.pitch || 0
      config.sample_rate = form.minimax?.sample_rate || 32000
      config.bitrate = form.minimax?.bitrate || 128000
      config.format = form.minimax?.format || 'mp3'
      config.channel = form.minimax?.channel || 1
      break
  }
  return JSON.stringify(config)
}

function validate(callback) {
  return formRef.value?.validate(callback)
}

function resetFields() {
  formRef.value?.resetFields()
}

defineExpose({ validate, getJsonData, resetFields })
</script>

<style scoped>
:deep(.el-form-item__label) {
  white-space: nowrap;
}

.form-link-tip {
  margin-top: 8px;
  font-size: 12px;
  color: #606266;
  line-height: 1.4;
  word-break: break-all;
}
</style>
