<template>
  <div class="config-wizard">
    <div class="wizard-header">
      <h2>配置向导</h2>
      <p class="wizard-desc">按步骤完成 OTA、VAD、ASR、LLM、TTS 基础配置，可随时跳过某步。</p>
    </div>

    <el-steps :active="currentStep" finish-status="success" align-center class="wizard-steps">
      <el-step title="OTA" description="服务地址" />
      <el-step title="VAD" description="语音活动检测" />
      <el-step title="ASR" description="语音识别" />
      <el-step title="LLM" description="大语言模型" />
      <el-step title="TTS" description="语音合成" />
    </el-steps>

    <el-card class="step-card" shadow="hover">
      <!-- Step 1: OTA -->
      <template v-if="currentStep === 0">
        <div class="step-title">OTA 配置</div>
        <p class="step-hint">填写本服务对外访问的域名或 IP，将自动生成 OTA 地址和 WebSocket 地址（最后一步会展示）。</p>
        <el-form :model="otaForm" label-width="140px" class="wizard-form">
          <el-form-item label="域名或 IP" prop="host">
            <el-input v-model="otaForm.host" placeholder="如 192.168.1.100 或 manager.example.com" clearable />
          </el-form-item>
          <el-form-item label="端口" prop="port">
            <el-input-number v-model="otaForm.port" :min="1" :max="65535" style="width: 100%" />
          </el-form-item>
          <el-form-item label="协议" prop="protocol">
            <el-radio-group v-model="otaForm.protocol">
              <el-radio value="http">HTTP</el-radio>
              <el-radio value="https">HTTPS</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="签名密钥" prop="signature_key">
            <el-input v-model="otaForm.signature_key" placeholder="与 MQTT Server 认证共用" clearable />
          </el-form-item>
          <el-form-item label="MQTT/UDP 模式" prop="mqttMode">
            <el-radio-group v-model="otaForm.mqttMode">
              <el-radio value="disabled">关闭</el-radio>
              <el-radio value="builtin">内置 MQTT Server</el-radio>
              <el-radio value="external">外部 Broker</el-radio>
            </el-radio-group>
            <span class="form-hint">内置模式自动写入 embed 客户端；外部模式只保留 MQTT 客户端与 UDP。</span>
          </el-form-item>

          <template v-if="otaForm.mqttMode === 'builtin'">
            <el-form-item label="MQTT Server 端口" prop="mqttServerPort" required>
              <el-input-number v-model="otaForm.mqttServerPort" :min="1" :max="65535" style="width: 100%" placeholder="1883 常用，8883 将启用 TLS" />
              <span class="form-hint">IP 复用上方域名；8883 时自动启用 TLS，默认开启认证。</span>
            </el-form-item>
          </template>

          <template v-if="otaForm.mqttMode === 'external'">
            <el-form-item label="MQTT Broker 地址" prop="externalMqttHost" required>
              <el-input v-model="otaForm.externalMqttHost" placeholder="如 emqx.example.com 或 10.0.0.8" clearable />
            </el-form-item>
            <el-form-item label="MQTT Broker 端口" prop="externalMqttPort" required>
              <el-input-number v-model="otaForm.externalMqttPort" :min="1" :max="65535" style="width: 100%" />
            </el-form-item>
            <el-form-item label="MQTT 协议" prop="externalMqttType" required>
              <el-select v-model="otaForm.externalMqttType" style="width: 100%">
                <el-option label="TCP" value="tcp" />
                <el-option label="SSL/TLS" value="ssl" />
                <el-option label="WebSocket (WS)" value="ws" />
                <el-option label="WebSocket Secure (WSS)" value="wss" />
              </el-select>
            </el-form-item>
            <el-form-item label="MQTT 客户端ID" prop="externalMqttClientId" required>
              <el-input v-model="otaForm.externalMqttClientId" placeholder="如 xiaozhi_manager" clearable />
            </el-form-item>
            <el-form-item label="MQTT 用户名" prop="externalMqttUsername">
              <el-input v-model="otaForm.externalMqttUsername" placeholder="外部 Broker 账号（可选）" clearable />
            </el-form-item>
            <el-form-item label="MQTT 密码" prop="externalMqttPassword">
              <el-input v-model="otaForm.externalMqttPassword" type="password" show-password placeholder="外部 Broker 密码（可选）" />
            </el-form-item>
          </template>

          <template v-if="otaForm.mqttMode !== 'disabled'">
            <el-form-item label="UDP 端口" prop="udpPort" required>
              <el-input-number v-model="otaForm.udpPort" :min="1" :max="65535" style="width: 100%" placeholder="如 8990" />
              <span class="form-hint">外网 IP 复用上方域名，外网端口与监听端口均为本端口；监听主机 0.0.0.0。</span>
            </el-form-item>
          </template>
        </el-form>
      </template>

      <!-- Step 2: VAD -->
      <template v-if="currentStep === 1">
        <div class="step-title">VAD 配置</div>
        <VADConfigForm ref="vadFormRef" :model="vadForm" :rules="vadFormRules" class="wizard-form" />
      </template>

      <!-- Step 3: ASR -->
      <template v-if="currentStep === 2">
        <div class="step-title">ASR 配置</div>
        <ASRConfigForm ref="asrFormRef" :model="asrForm" :rules="asrFormRules" class="wizard-form" />
      </template>

      <!-- Step 4: LLM -->
      <template v-if="currentStep === 3">
        <div class="step-title">LLM 配置</div>
        <LLMConfigForm ref="llmFormRef" :model="llmForm" :rules="llmFormRules" class="wizard-form" />
      </template>

      <!-- Step 5: TTS -->
      <template v-if="currentStep === 4">
        <div class="step-title">TTS 配置</div>
        <TTSConfigForm
          ref="ttsFormRef"
          :model="ttsForm"
          :rules="ttsFormRules"
          :voice-options="voiceOptions"
          :voice-loading="voiceLoading"
          class="wizard-form"
        />
      </template>

      <!-- 完成页：展示 OTA 地址与 WebSocket 地址 -->
      <template v-if="currentStep === 5">
        <div class="step-title">配置完成</div>
        <p class="step-hint">以下是根据您在 OTA 步骤填写的域名/IP 生成的地址，请下发至设备或固件使用。</p>
        <div class="result-box">
          <div class="result-item">
            <span class="result-label">OTA 地址（API 根地址）：</span>
            <el-input :model-value="finalOtaUrl" readonly>
              <template #append>
                <el-button @click="copyToClipboard(finalOtaUrl)" :icon="CopyDocument">复制</el-button>
              </template>
            </el-input>
          </div>
          <div class="result-item">
            <span class="result-label">WebSocket 地址：</span>
            <el-input :model-value="finalWsUrl" readonly>
              <template #append>
                <el-button @click="copyToClipboard(finalWsUrl)" :icon="CopyDocument">复制</el-button>
              </template>
            </el-input>
          </div>
          <div v-if="isMqttEnabled && finalMqttEndpoint" class="result-item">
            <span class="result-label">MQTT 端点（供终端连接）：</span>
            <el-input :model-value="finalMqttEndpoint" readonly>
              <template #append>
                <el-button @click="copyToClipboard(finalMqttEndpoint)" :icon="CopyDocument">复制</el-button>
              </template>
            </el-input>
          </div>
          <div v-if="isMqttEnabled && finalUdpEndpoint" class="result-item">
            <span class="result-label">UDP 信息（供终端连接）：</span>
            <el-input :model-value="finalUdpEndpoint" readonly>
              <template #append>
                <el-button @click="copyToClipboard(finalUdpEndpoint)" :icon="CopyDocument">复制</el-button>
              </template>
            </el-input>
          </div>
        </div>
        <div class="ota-test-section">
          <el-button type="warning" :loading="otaTestLoading" @click="runOtaTest">
            OTA 测试
          </el-button>
          <div v-if="otaTestResult !== null" class="ota-test-result">
            <span class="result-label">OTA 接口返回：</span>
            <pre class="ota-test-json">{{ otaTestResult }}</pre>
          </div>
        </div>
      </template>

      <div class="step-actions">
        <el-button v-if="currentStep > 0 && currentStep < 5" @click="prevStep">上一步</el-button>
        <el-button v-if="currentStep < 5" type="info" plain @click="skipStep">跳过</el-button>
        <el-button
          v-if="currentStep >= 1 && currentStep <= 4"
          type="warning"
          plain
          :loading="testingStep"
          @click="testCurrentStepConfig"
        >
          测试当前配置
        </el-button>
        <template v-if="currentStep < 5">
          <el-button type="primary" :loading="saving" @click="saveAndNext">
            {{ currentStep === 4 ? '保存并完成' : '保存并下一步' }}
          </el-button>
        </template>
        <template v-else>
          <el-button type="primary" @click="$router.push('/dashboard')">返回首页</el-button>
          <el-button @click="currentStep = 0">重新配置</el-button>
        </template>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument } from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'
import api from '@/utils/api'
import { testWithData, parseJsonData } from '@/utils/configTest'
import VADConfigForm from './forms/VADConfigForm.vue'
import ASRConfigForm from './forms/ASRConfigForm.vue'
import LLMConfigForm from './forms/LLMConfigForm.vue'
import TTSConfigForm from './forms/TTSConfigForm.vue'

const currentStep = ref(0)
const saving = ref(false)
const testingStep = ref(false)
const otaTestLoading = ref(false)
const otaTestResult = ref(null)
const otaConfigId = ref(null)
const vadConfigId = ref(null)
const asrConfigId = ref(null)
const llmConfigId = ref(null)
const ttsConfigId = ref(null)

const otaForm = reactive({
  host: '',
  port: 8989,
  protocol: 'http',
  signature_key: 'xiaozhi_ota_signature_key',
  mqttMode: 'disabled', // disabled | builtin | external
  mqttServerPort: 1883,
  udpPort: 8990,
  externalMqttHost: '',
  externalMqttPort: 1883,
  externalMqttType: 'tcp',
  externalMqttClientId: 'xiaozhi_manager',
  externalMqttUsername: '',
  externalMqttPassword: ''
})

const vadForm = reactive({
  name: '默认VAD',
  config_id: 'ten_vad_default',
  provider: 'ten_vad',
  webrtc_vad: {
    pool_min_size: 5,
    pool_max_size: 1000,
    pool_max_idle: 100,
    vad_sample_rate: 16000,
    vad_mode: 2
  },
  silero_vad: {
    model_path: 'config/models/vad/silero_vad.onnx',
    threshold: 0.5,
    min_silence_duration_ms: 100,
    sample_rate: 16000,
    channels: 1,
    pool_size: 10,
    acquire_timeout_ms: 3000
  },
  ten_vad: {
    hop_size: 320,
    threshold: 0.3,
    pool_size: 10,
    acquire_timeout_ms: 3000
  }
})
const vadFormRef = ref()
const vadFormRules = {
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }],
  config_id: [{ required: true, message: '请输入配置ID', trigger: 'blur' }],
  provider: [{ required: true, message: '请选择提供商', trigger: 'change' }],
  'ten_vad.hop_size': [{ required: true, message: '请输入帧移大小', trigger: 'blur' }],
  'ten_vad.threshold': [{ required: true, message: '请输入VAD检测阈值', trigger: 'blur' }],
  'ten_vad.pool_size': [{ required: true, message: '请输入连接池大小', trigger: 'blur' }],
  'ten_vad.acquire_timeout_ms': [{ required: true, message: '请输入获取超时时间', trigger: 'blur' }]
}

const asrForm = reactive({
  name: '默认ASR',
  config_id: 'funasr_default',
  provider: 'funasr',
  embed: {
    mode: 'offline'
  },
  funasr: {
    host: '127.0.0.1',
    port: 10095,
    mode: 'offline',
    sample_rate: 16000,
    chunk_size: [5, 10, 5],
    chunk_interval: 10,
    max_connections: 100,
    timeout: 30,
    auto_end: false
  },
  aliyun_funasr: {
    api_key: '',
    ws_url: 'wss://dashscope.aliyuncs.com/api-ws/v1/inference/',
    model: 'fun-asr-realtime',
    format: 'pcm',
    sample_rate: 16000,
    vocabulary_id: '',
    disfluency_removal_enabled: false,
    timeout: 30
  },
  doubao: {
    appid: '',
    access_token: '',
    ws_url: 'wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_nostream',
    model_name: 'bigmodel',
    end_window_size: 800,
    enable_punc: true,
    enable_itn: true,
    enable_ddc: false,
    chunk_duration: 200,
    timeout: 30
  },
  aliyun_qwen3: {
    api_key: '',
    ws_url: 'wss://dashscope.aliyuncs.com/api-ws/v1/realtime',
    model: 'qwen3-asr-flash-realtime',
    format: 'pcm',
    sample_rate: 16000,
    language: 'zh',
    auto_end: true,
    vad_threshold: 0.0,
    vad_silence_ms: 400,
    timeout: 30
  }
})
const asrFormRef = ref()
const validateAliyunPcm = (rule, value, callback) => {
  if (value !== 'pcm') {
    callback(new Error('格式必须为pcm'))
    return
  }
  callback()
}
const validateAliyun16000 = (rule, value, callback) => {
  if (Number(value) !== 16000) {
    callback(new Error('采样率必须为16000'))
    return
  }
  callback()
}
const asrFormRules = {
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }],
  config_id: [{ required: true, message: '请输入配置ID', trigger: 'blur' }],
  provider: [{ required: true, message: '请选择提供商', trigger: 'change' }],
  'funasr.host': [{ required: true, message: '请输入主机地址', trigger: 'blur' }],
  'funasr.port': [{ required: true, message: '请输入端口', trigger: 'blur' }],
  'aliyun_funasr.ws_url': [{ required: true, message: '请输入WS URL', trigger: 'blur' }],
  'aliyun_funasr.model': [{ required: true, message: '请输入模型名称', trigger: 'blur' }],
  'aliyun_funasr.format': [
    { required: true, message: '请选择音频格式', trigger: 'change' },
    { validator: validateAliyunPcm, trigger: 'change' }
  ],
  'aliyun_funasr.sample_rate': [
    { required: true, message: '请选择采样率', trigger: 'change' },
    { validator: validateAliyun16000, trigger: 'change' }
  ],
  'aliyun_funasr.timeout': [{ required: true, message: '请输入超时时间', trigger: 'blur' }],
  'doubao.appid': [{ required: true, message: '请输入应用ID', trigger: 'blur' }],
  'doubao.access_token': [{ required: true, message: '请输入访问令牌', trigger: 'blur' }],
  'doubao.ws_url': [{ required: true, message: '请输入WebSocket URL', trigger: 'blur' }],
  'doubao.model_name': [{ required: true, message: '请输入模型名称', trigger: 'blur' }],
  'aliyun_qwen3.ws_url': [{ required: true, message: '请输入WS URL', trigger: 'blur' }],
  'aliyun_qwen3.model': [{ required: true, message: '请输入模型名称', trigger: 'blur' }],
  'aliyun_qwen3.format': [{ required: true, message: '请选择音频格式', trigger: 'change' }],
  'aliyun_qwen3.sample_rate': [{ required: true, message: '请选择采样率', trigger: 'change' }],
  'aliyun_qwen3.language': [{ required: true, message: '请输入语言', trigger: 'blur' }],
  'aliyun_qwen3.timeout': [{ required: true, message: '请输入超时时间', trigger: 'blur' }]
}

const llmForm = reactive({
  name: '默认LLM',
  config_id: 'openai_default',
  provider: 'openai',
  type: 'openai',
  model_name: 'gpt-3.5-turbo',
  api_key: '',
  base_url: 'https://api.openai.com/v1',
  max_tokens: 4000,
  temperature: 0.7,
  top_p: 0.9
})
const llmFormRef = ref()
const llmFormRules = {
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }],
  config_id: [{ required: true, message: '请输入配置ID', trigger: 'blur' }],
  provider: [{ required: false }],
  type: [{ required: true, message: '请选择模型类型', trigger: 'change' }],
  model_name: [{ required: true, message: '请输入模型名称', trigger: 'blur' }],
  api_key: [{ required: true, message: '请输入API密钥', trigger: 'blur' }],
  base_url: [{ required: true, message: '请输入基础URL', trigger: 'blur' }],
  max_tokens: [{ required: true, message: '请输入max_tokens', trigger: 'blur' }]
}

const ttsForm = reactive({
  name: '默认TTS',
  config_id: 'minimax_default',
  provider: 'minimax',
  qwen_tts: {
    api_key: '',
    api_url: 'https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation',
    region: 'beijing',
    model: 'qwen3-tts-flash',
    voice: 'Cherry',
    language_type: 'Chinese',
    stream: true,
    frame_duration: 60
  },
  doubao_ws: {
    appid: '',
    access_token: '',
    cluster: 'volcano_tts',
    voice: 'zh_female_wanwanxiaohe_moon_bigtts',
    ws_host: '',
    use_stream: true
  },
  edge: {
    voice: 'zh-CN-XiaoxiaoNeural',
    rate: '+0%',
    volume: '+0%',
    pitch: '+0Hz',
    connect_timeout: 10,
    receive_timeout: 60
  },
  edge_offline: {
    server_url: 'ws://localhost:8080/tts',
    timeout: 30,
    sample_rate: 16000,
    channels: 1,
    frame_duration: 20
  },
  openai: {
    api_key: '',
    api_url: 'https://api.openai.com/v1/audio/speech',
    model: 'tts-1',
    voice: 'alloy',
    response_format: 'mp3',
    speed: 1.0,
    stream: true,
    frame_duration: 60
  },
  zhipu: {
    api_key: '',
    api_url: 'https://open.bigmodel.cn/api/paas/v4/audio/speech',
    model: 'glm-tts',
    voice: 'tongtong',
    response_format: 'pcm',
    speed: 1.0,
    volume: 1.0,
    stream: true,
    encode_format: 'base64',
    frame_duration: 60
  },
  minimax: {
    api_key: '',
    model: 'speech-2.8-hd',
    voice: 'male-qn-qingse',
    speed: 1.0,
    vol: 1.0,
    pitch: 0,
    sample_rate: 32000,
    bitrate: 128000,
    format: 'mp3',
    channel: 1
  }
})
const ttsFormRef = ref()
const voiceOptions = ref([])
const voiceLoading = ref(false)
const ttsFormRules = {
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }],
  config_id: [{ required: true, message: '请输入配置ID', trigger: 'blur' }],
  provider: [{ required: true, message: '请选择提供商', trigger: 'change' }],
  'doubao_ws.appid': [{ required: true, message: '请输入应用ID', trigger: 'blur' }],
  'doubao_ws.access_token': [{ required: true, message: '请输入访问令牌', trigger: 'blur' }],
  'minimax.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  'qwen_tts.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }]
}

const finalOtaUrl = computed(() => {
  if (!otaForm.host?.trim()) return '请先在 OTA 步骤填写域名或 IP'
  const proto = otaForm.protocol === 'https' ? 'https' : 'http'
  return `${proto}://${otaForm.host.trim()}:${otaForm.port}`
})

const finalWsUrl = computed(() => {
  if (!otaForm.host?.trim()) return '请先在 OTA 步骤填写域名或 IP'
  const proto = otaForm.protocol === 'https' ? 'wss' : 'ws'
  return `${proto}://${otaForm.host.trim()}:${otaForm.port}/xiaozhi/v1/`
})

const isMqttEnabled = computed(() => otaForm.mqttMode !== 'disabled')
const isBuiltinMqttMode = computed(() => otaForm.mqttMode === 'builtin')
const isExternalMqttMode = computed(() => otaForm.mqttMode === 'external')

const finalMqttEndpoint = computed(() => {
  if (!isMqttEnabled.value) return ''
  if (isBuiltinMqttMode.value) {
    if (!otaForm.host?.trim()) return ''
    return `${otaForm.host.trim()}:${otaForm.mqttServerPort}`
  }
  if (!otaForm.externalMqttHost?.trim()) return ''
  return `${otaForm.externalMqttHost.trim()}:${otaForm.externalMqttPort}`
})

const finalUdpEndpoint = computed(() => {
  if (!isMqttEnabled.value || !otaForm.host?.trim()) return ''
  return `${otaForm.host.trim()}:${otaForm.udpPort}`
})

function buildWsUrl() {
  if (!otaForm.host?.trim()) return ''
  const proto = otaForm.protocol === 'https' ? 'wss' : 'ws'
  return `${proto}://${otaForm.host.trim()}:${otaForm.port}/xiaozhi/v1/`
}

const MQTT_SERVER_DEFAULT_USER = 'admin'
const MQTT_SERVER_DEFAULT_PASS = 'admin123'

function normalizeMqttConnType(value) {
  const t = String(value || '').trim().toLowerCase()
  if (!t) return 'tcp'
  if (t === 'websocket') return 'ws'
  return t
}

async function saveMqttServerConfig(enable) {
  const port = Number(otaForm.mqttServerPort) || 1883
  const useTls = !!enable && port === 8883
  const configData = {
    enable: !!enable,
    listen_host: '0.0.0.0',
    listen_port: port,
    username: MQTT_SERVER_DEFAULT_USER,
    password: MQTT_SERVER_DEFAULT_PASS,
    signature_key: otaForm.signature_key?.trim() || 'xiaozhi_ota_signature_key',
    enable_auth: !!enable,
    tls: {
      enable: useTls,
      port: 8883,
      pem: '',
      key: ''
    }
  }
  const payload = {
    name: 'MQTT Server配置',
    config_id: 'mqtt_server_mqtt_server_config',
    provider: 'mqtt_server',
    json_data: JSON.stringify(configData),
    enabled: true,
    is_default: true
  }
  const res = await api.get('/admin/mqtt-server-configs')
  const list = res.data?.data || []
  const existing = list.find(c => c.is_default) || list[0]
  if (existing?.id) {
    await api.put(`/admin/mqtt-server-configs/${existing.id}`, payload)
  } else {
    await api.post('/admin/mqtt-server-configs', payload)
  }
}

async function saveMqttConfig(mode) {
  let configData
  if (mode === 'builtin') {
    const port = Number(otaForm.mqttServerPort) || 1883
    configData = {
      enable: true,
      broker: '127.0.0.1',
      type: 'embed',
      port,
      client_id: 'xiaozhi_manager',
      username: MQTT_SERVER_DEFAULT_USER,
      password: MQTT_SERVER_DEFAULT_PASS
    }
  } else if (mode === 'external') {
    const brokerPort = Number(otaForm.externalMqttPort) || 1883
    configData = {
      enable: true,
      broker: otaForm.externalMqttHost.trim(),
      type: normalizeMqttConnType(otaForm.externalMqttType),
      port: brokerPort,
      client_id: otaForm.externalMqttClientId.trim(),
      username: otaForm.externalMqttUsername,
      password: otaForm.externalMqttPassword
    }
  } else {
    configData = {
      enable: false,
      broker: '',
      type: 'embed',
      port: 1883,
      client_id: '',
      username: '',
      password: ''
    }
  }
  const payload = {
    name: 'MQTT配置',
    config_id: 'mqtt_wizard_default',
    is_default: true,
    json_data: JSON.stringify(configData)
  }
  const res = await api.get('/admin/mqtt-configs')
  const list = res.data?.data || []
  const existing = list.find(c => c.is_default) || list[0]
  if (existing?.id) {
    await api.put(`/admin/mqtt-configs/${existing.id}`, payload)
  } else {
    await api.post('/admin/mqtt-configs', payload)
  }
}

async function saveUdpConfig() {
  const host = otaForm.host?.trim() || '0.0.0.0'
  const port = Number(otaForm.udpPort) || 8990
  const configData = {
    listen_host: '0.0.0.0',
    listen_port: port,
    external_host: host,
    external_port: port
  }
  const payload = {
    name: 'UDP配置',
    config_id: 'udp_wizard_default',
    is_default: true,
    json_data: JSON.stringify(configData)
  }
  const res = await api.get('/admin/udp-configs')
  const list = res.data?.data || []
  const existing = list.find(c => c.is_default) || list[0]
  if (existing?.id) {
    await api.put(`/admin/udp-configs/${existing.id}`, payload)
  } else {
    await api.post('/admin/udp-configs', payload)
  }
}

async function saveOta() {
  const wsUrl = buildWsUrl()
  if (!wsUrl) {
    ElMessage.warning('请填写域名或 IP')
    return false
  }
  if (isMqttEnabled.value) {
    const host = otaForm.host?.trim()
    if (!host) {
      ElMessage.warning('请填写域名或 IP')
      return false
    }
    const udpPort = Number(otaForm.udpPort)
    if (!udpPort || udpPort < 1 || udpPort > 65535) {
      ElMessage.warning('请输入有效的 UDP 端口（1-65535）')
      return false
    }
  }

  if (isBuiltinMqttMode.value) {
    const mqttPort = Number(otaForm.mqttServerPort)
    if (!mqttPort || mqttPort < 1 || mqttPort > 65535) {
      ElMessage.warning('请输入有效的 MQTT Server 端口（1-65535）')
      return false
    }
  }

  if (isExternalMqttMode.value) {
    if (!otaForm.externalMqttHost?.trim()) {
      ElMessage.warning('请输入外部 MQTT Broker 地址')
      return false
    }
    const externalPort = Number(otaForm.externalMqttPort)
    if (!externalPort || externalPort < 1 || externalPort > 65535) {
      ElMessage.warning('请输入有效的外部 MQTT 端口（1-65535）')
      return false
    }
    if (!otaForm.externalMqttClientId?.trim()) {
      ElMessage.warning('请输入外部 MQTT 客户端ID')
      return false
    }
  }

  try {
    if (isBuiltinMqttMode.value) {
      await saveMqttServerConfig(true)
      await saveMqttConfig('builtin')
      await saveUdpConfig()
    } else if (isExternalMqttMode.value) {
      await saveMqttServerConfig(false)
      await saveMqttConfig('external')
      await saveUdpConfig()
    } else {
      await saveMqttServerConfig(false)
      await saveMqttConfig('disabled')
    }
  } catch (e) {
    ElMessage.error('MQTT/UDP 配置保存失败: ' + (e.response?.data?.message || e.message))
    return false
  }
  const mqttEndpoint = isMqttEnabled.value ? finalMqttEndpoint.value : ''
  const payload = {
    name: 'OTA配置',
    config_id: 'ota_ota_config',
    provider: 'default',
    json_data: JSON.stringify({
      signature_key: otaForm.signature_key?.trim() || 'xiaozhi_ota_signature_key',
      test: {
        websocket: { url: wsUrl },
        mqtt: { enable: isMqttEnabled.value, endpoint: mqttEndpoint }
      },
      external: {
        websocket: { url: wsUrl },
        mqtt: { enable: isMqttEnabled.value, endpoint: mqttEndpoint }
      }
    }, null, 2),
    enabled: true,
    is_default: true
  }
  try {
    if (otaConfigId.value) {
      await api.put(`/admin/ota-configs/${otaConfigId.value}`, payload)
    } else {
      const res = await api.post('/admin/ota-configs', payload)
      otaConfigId.value = res.data?.data?.id ?? null
    }
    ElMessage.success(isMqttEnabled.value ? 'OTA 及 MQTT/UDP 配置已保存' : 'OTA 配置已保存，MQTT/UDP 已关闭')
    return true
  } catch (e) {
    ElMessage.error('OTA 保存失败: ' + (e.response?.data?.message || e.message))
    return false
  }
}

async function saveVad() {
  if (!vadFormRef.value) return false
  try {
    await vadFormRef.value.validate()
  } catch (_) {
    return false
  }
  const payload = {
    name: vadForm.name,
    config_id: vadForm.config_id,
    provider: vadForm.provider,
    json_data: vadFormRef.value.getJsonData(),
    enabled: true,
    is_default: true
  }
  try {
    if (vadConfigId.value) {
      await api.put(`/admin/vad-configs/${vadConfigId.value}`, payload)
    } else {
      const res = await api.post('/admin/vad-configs', payload)
      vadConfigId.value = res.data?.data?.id ?? null
    }
    ElMessage.success('VAD 配置已保存')
    return true
  } catch (e) {
    ElMessage.error('VAD 保存失败: ' + (e.response?.data?.message || e.message))
    return false
  }
}

async function saveAsr() {
  if (!asrFormRef.value) return false
  try {
    await asrFormRef.value.validate()
  } catch (_) {
    return false
  }
  const payload = {
    name: asrForm.name,
    config_id: asrForm.config_id,
    provider: asrForm.provider,
    json_data: asrFormRef.value.getJsonData(),
    enabled: true,
    is_default: true
  }
  try {
    if (asrConfigId.value) {
      await api.put(`/admin/asr-configs/${asrConfigId.value}`, payload)
    } else {
      const res = await api.post('/admin/asr-configs', payload)
      asrConfigId.value = res.data?.data?.id ?? null
    }
    ElMessage.success('ASR 配置已保存')
    return true
  } catch (e) {
    ElMessage.error('ASR 保存失败: ' + (e.response?.data?.message || e.message))
    return false
  }
}

async function saveLlm() {
  if (!llmFormRef.value) return false
  try {
    await llmFormRef.value.validate()
  } catch (_) {
    return false
  }
  const payload = {
    name: llmForm.name,
    config_id: llmForm.config_id,
    provider: llmForm.provider,
    json_data: llmFormRef.value.getJsonData(),
    enabled: true,
    is_default: true
  }
  try {
    if (llmConfigId.value) {
      await api.put(`/admin/llm-configs/${llmConfigId.value}`, payload)
    } else {
      const res = await api.post('/admin/llm-configs', payload)
      llmConfigId.value = res.data?.data?.id ?? null
    }
    ElMessage.success('LLM 配置已保存')
    return true
  } catch (e) {
    ElMessage.error('LLM 保存失败: ' + (e.response?.data?.message || e.message))
    return false
  }
}

async function saveTts() {
  if (!ttsFormRef.value) return false
  try {
    await ttsFormRef.value.validate()
  } catch (_) {
    return false
  }
  const payload = {
    name: ttsForm.name,
    config_id: ttsForm.config_id,
    provider: ttsForm.provider,
    json_data: ttsFormRef.value.getJsonData(),
    enabled: true,
    is_default: true
  }
  try {
    if (ttsConfigId.value) {
      await api.put(`/admin/tts-configs/${ttsConfigId.value}`, payload)
    } else {
      const res = await api.post('/admin/tts-configs', payload)
      ttsConfigId.value = res.data?.data?.id ?? null
    }
    ElMessage.success('TTS 配置已保存')
    return true
  } catch (e) {
    ElMessage.error('TTS 保存失败: ' + (e.response?.data?.message || e.message))
    return false
  }
}

async function loadVadIfExists() {
  try {
    const res = await api.get('/admin/vad-configs')
    const list = res.data?.data || []
    const config = list.find(c => c.is_default) || list[0]
    if (!config) return
    vadConfigId.value = config.id
    vadForm.name = config.name
    vadForm.config_id = config.config_id
    vadForm.provider = config.provider || 'ten_vad'
    const data = JSON.parse(config.json_data || '{}')
    if (config.provider === 'webrtc_vad') {
      Object.assign(vadForm.webrtc_vad, data.webrtc_vad || data)
    } else if (config.provider === 'silero_vad') {
      Object.assign(vadForm.silero_vad, data.silero_vad || data)
    } else {
      Object.assign(vadForm.ten_vad, data.ten_vad || data)
    }
  } catch (_) {}
}

async function loadAsrIfExists() {
  try {
    const res = await api.get('/admin/asr-configs')
    const list = res.data?.data || []
    const config = list.find(c => c.is_default) || list[0]
    if (!config) return
    asrConfigId.value = config.id
    asrForm.name = config.name
    asrForm.config_id = config.config_id
    asrForm.provider = config.provider || 'funasr'
    const data = JSON.parse(config.json_data || '{}')
    if (config.provider === 'embed') {
      Object.assign(asrForm.embed, data.embed || data)
      if (!asrForm.embed.mode) asrForm.embed.mode = 'offline'
    } else if (config.provider === 'doubao') {
      Object.assign(asrForm.doubao, data.doubao || data)
    } else if (config.provider === 'aliyun_funasr') {
      Object.assign(asrForm.aliyun_funasr, data.aliyun_funasr || data)
    } else if (config.provider === 'aliyun_qwen3') {
      Object.assign(asrForm.aliyun_qwen3, data.aliyun_qwen3 || data)
    } else {
      const obj = data.funasr || data
      const funasr = { ...asrForm.funasr }
      if (typeof obj.chunk_size === 'number') funasr.chunk_size = [5, 10, 5]
      else if (Array.isArray(obj.chunk_size) && obj.chunk_size.length === 3) funasr.chunk_size = [...obj.chunk_size]
      Object.assign(funasr, obj)
      asrForm.funasr = funasr
    }
  } catch (_) {}
}

async function loadLlmIfExists() {
  try {
    const res = await api.get('/admin/llm-configs')
    const list = res.data?.data || []
    const config = list.find(c => c.is_default) || list[0]
    if (!config) return
    llmConfigId.value = config.id
    llmForm.name = config.name
    llmForm.config_id = config.config_id
    llmForm.provider = config.provider || 'openai'
    const data = JSON.parse(config.json_data || '{}')
    if (data.type !== undefined) llmForm.type = data.type
    if (data.model_name !== undefined) llmForm.model_name = data.model_name
    if (data.api_key !== undefined) llmForm.api_key = data.api_key
    if (data.base_url !== undefined) llmForm.base_url = data.base_url
    if (data.max_tokens !== undefined) llmForm.max_tokens = data.max_tokens
    if (data.temperature !== undefined) llmForm.temperature = data.temperature
    if (data.top_p !== undefined) llmForm.top_p = data.top_p
  } catch (_) {}
}

async function loadTtsIfExists() {
  try {
    const res = await api.get('/admin/tts-configs')
    const list = res.data?.data || []
    const config = list.find(c => c.is_default) || list[0]
    if (!config) return
    ttsConfigId.value = config.id
    ttsForm.name = config.name
    ttsForm.config_id = config.config_id
    ttsForm.provider = config.provider || 'minimax'
    const data = JSON.parse(config.json_data || '{}')
    const p = config.provider
    if (p === 'doubao_ws') Object.assign(ttsForm.doubao_ws, data)
    else if (p === 'edge') Object.assign(ttsForm.edge, data)
    else if (p === 'edge_offline') Object.assign(ttsForm.edge_offline, data)
    else if (p === 'aliyun_qwen') Object.assign(ttsForm.qwen_tts, data)
    else if (p === 'openai') Object.assign(ttsForm.openai, data)
    else if (p === 'zhipu') Object.assign(ttsForm.zhipu, data)
    else if (p === 'minimax') Object.assign(ttsForm.minimax, data)
  } catch (_) {}
}

async function saveAndNext() {
  saving.value = true
  let ok = false
  try {
    if (currentStep.value === 0) ok = await saveOta()
    else if (currentStep.value === 1) ok = await saveVad()
    else if (currentStep.value === 2) ok = await saveAsr()
    else if (currentStep.value === 3) ok = await saveLlm()
    else if (currentStep.value === 4) ok = await saveTts()
    if (ok) {
      if (currentStep.value === 4) {
        currentStep.value = 5
      } else {
        currentStep.value++
      }
    }
  } finally {
    saving.value = false
  }
}

function skipStep() {
  if (currentStep.value === 4) {
    currentStep.value = 5
  } else {
    currentStep.value++
  }
}

function prevStep() {
  if (currentStep.value > 0) currentStep.value--
}

function formatTestMessage(result) {
  const base = result.message || ''
  return result.first_packet_ms != null ? `${base} ${result.first_packet_ms}ms` : base
}

async function testCurrentStepConfig() {
  const step = currentStep.value
  if (step === 1) {
    if (!vadFormRef.value) return
    try {
      await vadFormRef.value.validate()
    } catch (_) {
      return
    }
    const configId = vadForm.config_id?.trim() || 'vad_wizard'
    const payload = {
      name: vadForm.name,
      config_id: configId,
      provider: vadForm.provider,
      is_default: vadForm.is_default,
      ...parseJsonData(vadFormRef.value.getJsonData())
    }
    testingStep.value = true
    try {
      const result = await testWithData('vad', { [configId]: payload })
      if (result.ok) ElMessage.success(formatTestMessage(result) || '测试通过')
      else ElMessage.warning(result.message || '测试未通过')
    } catch (err) {
      ElMessage.warning(err.response?.data?.error || '测试请求失败')
    } finally {
      testingStep.value = false
    }
    return
  }
  if (step === 2) {
    if (!asrFormRef.value) return
    try {
      await asrFormRef.value.validate()
    } catch (_) {
      return
    }
    const configId = asrForm.config_id?.trim() || 'asr_wizard'
    const payload = {
      name: asrForm.name,
      config_id: configId,
      provider: asrForm.provider,
      is_default: asrForm.is_default,
      ...parseJsonData(asrFormRef.value.getJsonData())
    }
    testingStep.value = true
    try {
      const result = await testWithData('asr', { [configId]: payload })
      if (result.ok) ElMessage.success(formatTestMessage(result) || '测试通过')
      else ElMessage.warning(result.message || '测试未通过')
    } catch (err) {
      ElMessage.warning(err.response?.data?.error || '测试请求失败')
    } finally {
      testingStep.value = false
    }
    return
  }
  if (step === 3) {
    if (!llmFormRef.value) return
    try {
      await llmFormRef.value.validate()
    } catch (_) {
      return
    }
    const configId = llmForm.config_id?.trim() || 'llm_wizard'
    const payload = {
      name: llmForm.name,
      config_id: configId,
      provider: llmForm.provider,
      is_default: llmForm.is_default,
      ...parseJsonData(llmFormRef.value.getJsonData())
    }
    testingStep.value = true
    try {
      const result = await testWithData('llm', { [configId]: payload })
      if (result.ok) ElMessage.success(formatTestMessage(result) || '测试通过')
      else ElMessage.warning(result.message || '测试未通过')
    } catch (err) {
      ElMessage.warning(err.response?.data?.error || '测试请求失败')
    } finally {
      testingStep.value = false
    }
    return
  }
  if (step === 4) {
    if (!ttsFormRef.value) return
    try {
      await ttsFormRef.value.validate()
    } catch (_) {
      return
    }
    const configId = ttsForm.config_id?.trim() || 'tts_wizard'
    const payload = {
      name: ttsForm.name,
      config_id: configId,
      provider: ttsForm.provider,
      is_default: ttsForm.is_default,
      ...parseJsonData(ttsFormRef.value.getJsonData())
    }
    testingStep.value = true
    try {
      const result = await testWithData('tts', { [configId]: payload })
      if (result.ok) ElMessage.success(formatTestMessage(result) || '测试通过')
      else ElMessage.warning(result.message || '测试未通过')
    } catch (err) {
      ElMessage.warning(err.response?.data?.error || '测试请求失败')
    } finally {
      testingStep.value = false
    }
  }
}

async function copyToClipboard(text) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.error('复制失败')
  }
}

function formatOtaResponseDisplay(str) {
  if (str == null || str === '') return ''
  const s = String(str).trim()
  if (!s) return ''
  try {
    const parsed = JSON.parse(s)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return s
  }
}

async function runOtaTest() {
  otaTestLoading.value = true
  otaTestResult.value = null
  try {
    const res = await api.post('/admin/configs/test', { types: ['ota'] }, { timeout: 30000 })
    const data = res.data?.data ?? res.data
    const ota = data?.ota
    if (ota && typeof ota === 'object') {
      const entry = Object.entries(ota).find(([k]) => !k.startsWith('_'))
      if (entry) {
        const [, v] = entry

        // 格式化显示结果
        let displayText = ''

        // WebSocket 结果
        if (v.websocket) {
          const ws = v.websocket
          displayText += `WebSocket: ${ws.ok ? '✓' : '✗'} ${ws.message}`
          if (ws.first_packet_ms != null) {
            displayText += ` (${ws.first_packet_ms}ms)\n`
          } else {
            displayText += '\n'
          }
        }

        // MQTT UDP 结果
        if (v.mqtt_udp) {
          const mqtt = v.mqtt_udp
          displayText += `MQTT UDP: ${mqtt.ok ? '✓' : '✗'} ${mqtt.message}`
          if (mqtt.first_packet_ms != null) {
            displayText += ` (${mqtt.first_packet_ms}ms)\n`
          } else {
            displayText += '\n'
          }
        }

        // OTA 响应内容（如果有）
        if (v.ota_response !== undefined && v.ota_response !== '') {
          displayText += `\n--- OTA 响应 ---\n${formatOtaResponseDisplay(v.ota_response)}`
        }

        otaTestResult.value = displayText.trim() || '未获取到详细信息'

        // 根据整体结果显示消息
        const overallOk = v.ok
        if (overallOk) {
          ElMessage.success(v.message || 'OTA 测试通过')
        } else {
          ElMessage.warning(v.message || 'OTA 测试未通过')
        }
      } else {
        otaTestResult.value = '未获取到 OTA 测试结果'
      }
    } else {
      otaTestResult.value = typeof data === 'string' ? data : JSON.stringify(data || {}, null, 2)
    }
  } catch (e) {
    const errorMsg = (e.response?.data && typeof e.response.data === 'object')
      ? JSON.stringify(e.response.data, null, 2)
      : (e.response?.data?.message || e.message || '请求失败')
    otaTestResult.value = errorMsg
    ElMessage.error('OTA 测试请求失败')
  } finally {
    otaTestLoading.value = false
  }
}

async function loadOtaIfExists() {
  try {
    const res = await api.get('/admin/ota-configs')
    const list = res.data?.data || []
    const config = list.find(c => c.is_default) || list[0]
    if (!config) return
    otaConfigId.value = config.id
    const data = JSON.parse(config.json_data || '{}')
    if (data.signature_key) otaForm.signature_key = data.signature_key
    const ext = data.external?.websocket?.url || ''
    if (ext) {
      const m = ext.match(/^(wss?):\/\/([^:/]+):?(\d+)?/)
      if (m) {
        otaForm.protocol = m[1] === 'wss' ? 'https' : 'http'
        otaForm.host = m[2]
        otaForm.port = m[3] ? parseInt(m[3], 10) : 8989
      }
    }
    const endpoint = data.test?.mqtt?.endpoint || data.external?.mqtt?.endpoint || ''
    const parts = endpoint.split(':')
    const endpointHost = parts.length >= 2 ? parts.slice(0, -1).join(':') : endpoint
    const endpointPort = parts.length >= 2 ? parseInt(parts[parts.length - 1], 10) : NaN

    let mqttConfigData = null
    let mqttServerData = null
    let udpData = null

    try {
      const mqttRes = await api.get('/admin/mqtt-configs')
      const mqttList = mqttRes.data?.data || []
      const mqttConfig = mqttList.find(c => c.is_default) || mqttList[0]
      if (mqttConfig?.json_data) mqttConfigData = JSON.parse(mqttConfig.json_data)
    } catch (_) {}

    try {
      const mqttServerRes = await api.get('/admin/mqtt-server-configs')
      const mqttServerList = mqttServerRes.data?.data || []
      const mqttServerConfig = mqttServerList.find(c => c.is_default) || mqttServerList[0]
      if (mqttServerConfig?.json_data) mqttServerData = JSON.parse(mqttServerConfig.json_data)
    } catch (_) {}

    try {
      const udpRes = await api.get('/admin/udp-configs')
      const udpList = udpRes.data?.data || []
      const udpConfig = udpList.find(c => c.is_default) || udpList[0]
      if (udpConfig?.json_data) udpData = JSON.parse(udpConfig.json_data)
    } catch (_) {}

    if (udpData?.listen_port) {
      otaForm.udpPort = parseInt(udpData.listen_port, 10) || otaForm.udpPort
    }

    const otaMqttEnabled = !!(data.test?.mqtt?.enable || data.external?.mqtt?.enable)
    const mqttConfigEnabled = mqttConfigData?.enable
    const mqttEnabled = typeof mqttConfigEnabled === 'boolean' ? mqttConfigEnabled : otaMqttEnabled

    if (!mqttEnabled) {
      otaForm.mqttMode = 'disabled'
      return
    }

    if (!mqttConfigData || typeof mqttConfigData !== 'object') {
      otaForm.mqttMode = 'builtin'
      if (!Number.isNaN(endpointPort)) {
        otaForm.mqttServerPort = endpointPort || otaForm.mqttServerPort
      }
      return
    }

    const mqttType = normalizeMqttConnType(mqttConfigData?.type)
    const mqttServerEnabled = mqttServerData?.enable === true

    if (mqttType === 'embed' || mqttServerEnabled) {
      otaForm.mqttMode = 'builtin'
      if (mqttServerData?.listen_port) {
        otaForm.mqttServerPort = parseInt(mqttServerData.listen_port, 10) || otaForm.mqttServerPort
      } else if (mqttConfigData?.port) {
        otaForm.mqttServerPort = parseInt(mqttConfigData.port, 10) || otaForm.mqttServerPort
      } else if (!Number.isNaN(endpointPort)) {
        otaForm.mqttServerPort = endpointPort || otaForm.mqttServerPort
      }
    } else {
      otaForm.mqttMode = 'external'
      otaForm.externalMqttType = mqttType || 'tcp'
      otaForm.externalMqttHost = mqttConfigData?.broker || endpointHost || otaForm.externalMqttHost
      if (mqttConfigData?.port) {
        otaForm.externalMqttPort = parseInt(mqttConfigData.port, 10) || otaForm.externalMqttPort
      } else if (!Number.isNaN(endpointPort)) {
        otaForm.externalMqttPort = endpointPort || otaForm.externalMqttPort
      }
      otaForm.externalMqttClientId = mqttConfigData?.client_id || otaForm.externalMqttClientId
      otaForm.externalMqttUsername = mqttConfigData?.username || ''
      otaForm.externalMqttPassword = mqttConfigData?.password || ''
    }
  } catch (_) {}
}

// 加载 TTS 音色列表（与 TTS 配置页一致）
async function loadTtsVoiceOptions(provider) {
  if (!provider) {
    voiceOptions.value = []
    return
  }
  const providersWithVoices = ['minimax', 'edge', 'doubao', 'doubao_ws', 'zhipu', 'openai']
  if (!providersWithVoices.includes(provider)) {
    voiceOptions.value = []
    return
  }
  voiceLoading.value = true
  try {
    const response = await api.get('/user/voice-options', { params: { provider } })
    voiceOptions.value = response.data.data || []
  } catch (error) {
    console.error('加载音色列表失败:', error)
    voiceOptions.value = []
  } finally {
    voiceLoading.value = false
  }
}

// 进入 TTS 步骤时加载当前 provider 的音色列表
watch(currentStep, (step) => {
  if (step === 4 && ttsForm.provider) {
    nextTick(() => loadTtsVoiceOptions(ttsForm.provider))
  }
}, { immediate: true })

// TTS 步骤内切换 provider 时重新加载音色列表
watch(() => ttsForm.provider, (provider) => {
  if (currentStep.value === 4 && provider) {
    loadTtsVoiceOptions(provider)
  }
}, { immediate: false })

const authStore = useAuthStore()

onMounted(async () => {
  if (authStore.isAdmin) {
    localStorage.setItem('admin_first_login_done', '1')
  }
  await loadOtaIfExists()
  await loadVadIfExists()
  await loadAsrIfExists()
  await loadLlmIfExists()
  await loadTtsIfExists()
})
</script>

<style scoped>
.config-wizard {
  padding: 20px;
  max-width: 720px;
  margin: 0 auto;
}
.wizard-header {
  margin-bottom: 24px;
}
.wizard-header h2 {
  margin: 0 0 8px 0;
  font-size: 22px;
  color: #303133;
}
.wizard-desc {
  margin: 0;
  color: #909399;
  font-size: 14px;
}
.wizard-steps {
  margin-bottom: 24px;
}
.step-card {
  padding: 24px;
}
.step-title {
  font-size: 16px;
  font-weight: 600;
  margin-bottom: 8px;
  color: #303133;
}
.step-hint {
  color: #909399;
  font-size: 13px;
  margin-bottom: 20px;
}
.form-hint {
  display: block;
  color: #909399;
  font-size: 12px;
  margin-top: 4px;
  line-height: 1.4;
}
.wizard-form {
  margin-bottom: 24px;
}
.step-actions {
  display: flex;
  gap: 12px;
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid #ebeef5;
}
.result-box {
  margin: 16px 0;
}
.result-item {
  margin-bottom: 16px;
}
.result-label {
  display: block;
  font-size: 13px;
  color: #606266;
  margin-bottom: 6px;
}
.ota-test-section {
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid #ebeef5;
}
.ota-test-result {
  margin-top: 12px;
}
.ota-test-result .result-label {
  margin-bottom: 6px;
}
.ota-test-json {
  margin: 0;
  padding: 12px;
  background: #f5f7fa;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 280px;
  overflow: auto;
}
</style>
