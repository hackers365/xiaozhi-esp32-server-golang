<template>
  <div class="config-page">
    <div class="page-actions">
      <el-button
        type="warning"
        plain
        :loading="testingAll"
        @click="testAllConfigs"
        :disabled="!getEnabledConfigs().length"
      >
        测试全部
      </el-button>
      <el-button type="primary" @click="showDialog = true">
        <el-icon><Plus /></el-icon>
        添加配置
      </el-button>
    </div>

    <el-table :data="configs" style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="name" label="配置名称" />
      <el-table-column prop="config_id" label="配置ID" width="150" />
      <el-table-column prop="provider" label="提供商">
        <template #default="scope">
          {{ scope.row.provider }}
        </template>
      </el-table-column>
      <el-table-column prop="enabled" label="启用状态" width="80" align="center">
        <template #default="scope">
          <el-switch 
            v-model="scope.row.enabled" 
            @change="toggleEnable(scope.row)"
          />
        </template>
      </el-table-column>
      <el-table-column prop="is_default" label="默认配置" width="80" align="center">
        <template #default="scope">
          <el-switch 
            v-model="scope.row.is_default" 
            @change="toggleDefault(scope.row)"
            :disabled="scope.row.is_default && getEnabledConfigs().length === 1"
          />
        </template>
      </el-table-column>
      <el-table-column label="测试结果" width="120" align="center">
        <template #default="scope">
          <template v-if="testResults[scope.row.config_id]">
            <el-tooltip v-if="testResults[scope.row.config_id].ok" :content="formatTestResultTip(testResults[scope.row.config_id])" placement="top">
              <span class="test-result test-ok">{{ formatTestResultLabel(testResults[scope.row.config_id]) }}</span>
            </el-tooltip>
            <el-tooltip v-else :content="testResults[scope.row.config_id].message" placement="top" :show-after="200">
              <span class="test-result test-err">错误</span>
            </el-tooltip>
          </template>
          <span v-else class="test-result test-none">-</span>
        </template>
      </el-table-column>
      <el-table-column prop="created_at" label="创建时间" width="180">
        <template #default="scope">
          {{ formatDate(scope.row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="260">
        <template #default="scope">
          <el-button size="small" @click="editConfig(scope.row)">编辑</el-button>
          <el-button
            size="small"
            type="warning"
            :loading="testingId === scope.row.config_id"
            @click="testConfig(scope.row, 'tts')"
          >
            测试
          </el-button>
          <el-button
            size="small"
            type="danger"
            @click="deleteConfig(scope.row.id)"
          >
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 添加/编辑配置弹窗 -->
    <el-dialog
      v-model="showDialog"
      :title="editingConfig ? '编辑TTS配置' : '添加TTS配置'"
      width="600px"
      @close="handleDialogClose"
    >
      <TTSConfigForm
        ref="formRef"
        :model="form"
        :rules="rules"
        :voice-options="voiceOptions"
        :voice-loading="voiceLoading"
        @request-voice-options="handleVoiceOptionsRequest"
      />
      
      <template #footer>
        <el-button @click="handleDialogClose">取消</el-button>
        <el-button type="warning" plain @click="testCurrentConfig" :loading="testingCurrent">
          测试
        </el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import api from '../../utils/api'
import { testSingleConfig, testWithData, parseJsonData } from '../../utils/configTest'
import TTSConfigForm from './forms/TTSConfigForm.vue'
import { TTS_PROVIDERS_WITH_VOICES } from './forms/ttsProviderOptions'
import { resolveTTSProvider } from './forms/configProviderUtils'
import { normalizeAliyunTtsConfig, reconcileAliyunVoice } from './forms/aliyunTtsOptions'

const configs = ref([])
const testingId = ref(null)
const testingAll = ref(false)
const testingCurrent = ref(false)
const testResults = ref({})
const loading = ref(false)
const saving = ref(false)
const showDialog = ref(false)
const editingConfig = ref(null)
const formRef = ref()

// 音色列表相关
const voiceOptions = ref([])
const voiceLoading = ref(false)

const form = reactive({
  name: '',
  config_id: '',
  provider: 'doubao_ws',
  is_default: false,
  enabled: true,
  double_stream: false,
  cosyvoice: {
    api_url: 'https://tts.linkerai.cn/tts',
    spk_id: 'spk_id',
    frame_duration: 60,
    target_sr: 24000,
    audio_format: 'mp3',
    instruct_text: '你好'
  },
  qwen_tts: {
    api_key: '',
    api_url: 'https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation',
    region: 'beijing',
    model: 'qwen3-tts-flash',
    voice: 'Cherry',
    voice_prompt: '',
    language_type: 'Chinese',
    format: 'ogg_opus',
    stream: true,
    frame_duration: 60
  },
  doubao: {
    appid: '6886011847',
    access_token: 'access_token',
    model: 'seed-tts-2.0-standard',
    voice: 'BV001_streaming',
    api_url: 'https://openspeech.bytedance.com/api/v3/tts/unidirectional'
  },
  doubao_ws: {
    appid: '6886011847',
    access_token: 'access_token',
    model: 'seed-tts-2.0-standard',
    resource_id: '',
    voice: '',
    ws_url: 'wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream'
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
  xunfei: {
    app_id: '',
    api_key: '',
    api_secret: '',
    ws_url: 'wss://tts-api.xfyun.cn/v2/tts',
    voice: 'xiaoyan',
    audio_encoding: 'raw',
    sample_rate: 16000,
    speed: 50,
    volume: 50,
    pitch: 50,
    tte: 'UTF8',
    reg: 0,
    rdn: 0,
    frame_duration: 60,
    connect_timeout: 10,
    read_timeout: 30
  },
  xunfei_super_tts: {
    app_id: '',
    api_key: '',
    api_secret: '',
    ws_url: 'wss://cbm01.cn-huabei-1.xf-yun.com/v1/private/mcd9m97e6',
    voice: 'x6_lingxiaoxue_pro',
    audio_encoding: 'raw',
    sample_rate: 24000,
    speed: 50,
    volume: 50,
    pitch: 50,
    bgs: 0,
    reg: 0,
    rdn: 0,
    rhy: 0,
    oral_level: 'mid',
    spark_assist: 1,
    stop_split: 0,
    remain: 0,
    frame_duration: 60,
    connect_timeout: 10,
    read_timeout: 30
  },
  indextts_vllm: {
    api_url: 'http://127.0.0.1:7860',
    api_key: '',
    model: 'indextts-vllm',
    voice: '',
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

const rules = {
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }],
  config_id: [{ required: true, message: '请输入配置ID', trigger: 'blur' }],
  provider: [{ required: true, message: '请选择提供商', trigger: 'change' }],
  // CosyVoice 验证规则
  'cosyvoice.api_url': [{ required: true, message: '请输入API URL', trigger: 'blur' }],
  'cosyvoice.spk_id': [{ required: true, message: '请输入说话人ID', trigger: 'blur' }],
  // 豆包 TTS 验证规则
  'doubao.appid': [{ required: true, message: '请输入应用ID', trigger: 'blur' }],
  'doubao.access_token': [{ required: true, message: '请输入访问令牌', trigger: 'blur' }],
  'doubao.model': [{ required: true, message: '请选择模型', trigger: 'change' }],
  'doubao.voice': [{ required: true, message: '请输入音色', trigger: 'blur' }],
  'doubao.api_url': [{ required: true, message: '请输入API URL', trigger: 'blur' }],
  // 豆包 WebSocket 验证规则
  'doubao_ws.appid': [{ required: true, message: '请输入应用ID', trigger: 'blur' }],
  'doubao_ws.access_token': [{ required: true, message: '请输入访问令牌', trigger: 'blur' }],
  'doubao_ws.model': [{ required: true, message: '请选择模型', trigger: 'change' }],
  'doubao_ws.voice': [{ required: true, message: '请输入音色', trigger: 'blur' }],
  'doubao_ws.ws_url': [{ required: true, message: '请输入WebSocket URL', trigger: 'blur' }],
  // Edge TTS 验证规则
  'edge.voice': [{ required: true, message: '请输入音色', trigger: 'blur' }],
  'edge.rate': [{ required: true, message: '请输入语速', trigger: 'blur' }],
  'edge.volume': [{ required: true, message: '请输入音量', trigger: 'blur' }],
  // Edge 离线验证规则
  'edge_offline.server_url': [{ required: true, message: '请输入服务器URL', trigger: 'blur' }],
  // OpenAI TTS 验证规则
  'openai.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  // 讯飞 TTS 验证规则
  'xunfei.app_id': [{ required: true, message: '请输入应用ID', trigger: 'blur' }],
  'xunfei.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  'xunfei.api_secret': [{ required: true, message: '请输入API Secret', trigger: 'blur' }],
  'xunfei.ws_url': [{ required: true, message: '请输入WebSocket URL', trigger: 'blur' }],
  'xunfei.voice': [{ required: true, message: '请输入音色', trigger: 'blur' }],
  'xunfei_super_tts.app_id': [{ required: true, message: '请输入应用ID', trigger: 'blur' }],
  'xunfei_super_tts.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  'xunfei_super_tts.api_secret': [{ required: true, message: '请输入API Secret', trigger: 'blur' }],
  'xunfei_super_tts.ws_url': [{ required: true, message: '请输入WebSocket URL', trigger: 'blur' }],
  'xunfei_super_tts.voice': [{ required: true, message: '请输入音色', trigger: 'blur' }],
  // 智谱 TTS 验证规则
  'zhipu.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  // Minimax TTS 验证规则
  'minimax.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  // 千问 TTS 验证规则
  'qwen_tts.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  'indextts_vllm.api_url': [{ required: true, message: '请输入API URL', trigger: 'blur' }]
}

const loadConfigs = async () => {
  loading.value = true
  try {
    const response = await api.get('/admin/tts-configs')
    configs.value = (response.data.data || []).map(normalizeTTSConfigRow)
  } catch (error) {
    ElMessage.error('加载配置失败')
  } finally {
    loading.value = false
  }
}

function normalizeTTSConfigRow(row) {
  const data = parseJsonData(row?.json_data)
  return {
    ...row,
    provider: resolveTTSProvider(row?.provider, row?.config_id, data)
  }
}

const editConfig = (config) => {
  config = normalizeTTSConfigRow(config)
  editingConfig.value = config
  form.name = config.name
  form.config_id = config.config_id
  form.provider = config.provider
  form.is_default = config.is_default
  form.enabled = config.enabled
  form.double_stream = false

  // IndexTTS 改为点击音色下拉时再请求
  loadVoiceOptions(config.provider)

  // 解析配置JSON并填充到对应的表单字段
  try {
    const configData = JSON.parse(config.json_data || '{}')
    form.double_stream = configData.double_stream === true
    
    switch (config.provider) {
      case 'cosyvoice':
        form.cosyvoice.api_url = configData.api_url || ''
        form.cosyvoice.spk_id = configData.spk_id || ''
        form.cosyvoice.frame_duration = configData.frame_duration || 60
        form.cosyvoice.target_sr = configData.target_sr || 24000
        form.cosyvoice.audio_format = configData.audio_format || 'mp3'
        form.cosyvoice.instruct_text = configData.instruct_text || ''
        break
      case 'doubao':
        form.doubao.appid = configData.appid || ''
        form.doubao.access_token = configData.access_token || ''
        form.doubao.model = configData.model || 'seed-tts-2.0-standard'
        form.doubao.voice = configData.voice || ''
        form.doubao.api_url = configData.api_url || 'https://openspeech.bytedance.com/api/v3/tts/unidirectional'
        break
      case 'doubao_ws':
        form.doubao_ws.appid = configData.appid || ''
        form.doubao_ws.access_token = configData.access_token || ''
        form.doubao_ws.model = configData.model || 'seed-tts-2.0-standard'
        form.doubao_ws.resource_id = configData.resource_id || ''
        form.doubao_ws.voice = configData.voice || ''
        form.doubao_ws.ws_url = configData.ws_url || (configData.ws_host ? `wss://${configData.ws_host}/api/v3/tts/unidirectional/stream` : 'wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream')
        break
      case 'edge':
        form.edge.voice = configData.voice || ''
        form.edge.rate = configData.rate || '+0%'
        form.edge.volume = configData.volume || '+0%'
        form.edge.pitch = configData.pitch || '+0Hz'
        form.edge.connect_timeout = configData.connect_timeout || 10
        form.edge.receive_timeout = configData.receive_timeout || 60
        break
      case 'edge_offline':
        form.edge_offline.server_url = configData.server_url || ''
        form.edge_offline.timeout = configData.timeout || 30
        form.edge_offline.sample_rate = configData.sample_rate || 16000
        form.edge_offline.channels = configData.channels || 1
        form.edge_offline.frame_duration = configData.frame_duration || 20
        break
      case 'aliyun_qwen':
        Object.assign(form.qwen_tts, normalizeAliyunTtsConfig(configData))
        break
      case 'openai':
        form.openai.api_key = configData.api_key || ''
        form.openai.api_url = configData.api_url || 'https://api.openai.com/v1/audio/speech'
        form.openai.model = configData.model || 'tts-1'
        form.openai.voice = configData.voice || 'alloy'
        form.openai.response_format = configData.response_format || 'mp3'
        form.openai.speed = configData.speed || 1.0
        form.openai.stream = configData.stream !== undefined ? configData.stream : true
        form.openai.frame_duration = configData.frame_duration || 60
        break
      case 'xunfei':
        form.xunfei.app_id = configData.app_id || ''
        form.xunfei.api_key = configData.api_key || ''
        form.xunfei.api_secret = configData.api_secret || ''
        form.xunfei.ws_url = configData.ws_url || 'wss://tts-api.xfyun.cn/v2/tts'
        form.xunfei.voice = configData.voice || 'xiaoyan'
        form.xunfei.audio_encoding = configData.audio_encoding || 'raw'
        form.xunfei.sample_rate = configData.sample_rate || 16000
        form.xunfei.speed = configData.speed ?? 50
        form.xunfei.volume = configData.volume ?? 50
        form.xunfei.pitch = configData.pitch ?? 50
        form.xunfei.tte = configData.tte || 'UTF8'
        form.xunfei.reg = configData.reg ?? 0
        form.xunfei.rdn = configData.rdn ?? 0
        form.xunfei.frame_duration = configData.frame_duration || 60
        form.xunfei.connect_timeout = configData.connect_timeout || 10
        form.xunfei.read_timeout = configData.read_timeout || 30
        break
      case 'xunfei_super_tts':
        form.xunfei_super_tts.app_id = configData.app_id || ''
        form.xunfei_super_tts.api_key = configData.api_key || ''
        form.xunfei_super_tts.api_secret = configData.api_secret || ''
        form.xunfei_super_tts.ws_url = configData.ws_url || 'wss://cbm01.cn-huabei-1.xf-yun.com/v1/private/mcd9m97e6'
        form.xunfei_super_tts.voice = configData.voice || 'x6_lingxiaoxue_pro'
        form.xunfei_super_tts.audio_encoding = configData.audio_encoding || 'raw'
        form.xunfei_super_tts.sample_rate = configData.sample_rate || 24000
        form.xunfei_super_tts.speed = configData.speed ?? 50
        form.xunfei_super_tts.volume = configData.volume ?? 50
        form.xunfei_super_tts.pitch = configData.pitch ?? 50
        form.xunfei_super_tts.bgs = configData.bgs ?? 0
        form.xunfei_super_tts.reg = configData.reg ?? 0
        form.xunfei_super_tts.rdn = configData.rdn ?? 0
        form.xunfei_super_tts.rhy = configData.rhy ?? 0
        form.xunfei_super_tts.oral_level = configData.oral_level || 'mid'
        form.xunfei_super_tts.spark_assist = configData.spark_assist ?? 1
        form.xunfei_super_tts.stop_split = configData.stop_split ?? 0
        form.xunfei_super_tts.remain = configData.remain ?? 0
        form.xunfei_super_tts.frame_duration = configData.frame_duration || 60
        form.xunfei_super_tts.connect_timeout = configData.connect_timeout || 10
        form.xunfei_super_tts.read_timeout = configData.read_timeout || 30
        break
      case 'indextts_vllm':
        form.indextts_vllm.api_url = configData.api_url || 'http://127.0.0.1:7860'
        form.indextts_vllm.api_key = configData.api_key || ''
        form.indextts_vllm.model = configData.model || 'indextts-vllm'
        form.indextts_vllm.voice = configData.voice || ''
        form.indextts_vllm.frame_duration = configData.frame_duration || 60
        break
      case 'zhipu':
        // 智谱配置从 json_data 中读取
        form.zhipu.api_key = configData.api_key || ''
        form.zhipu.api_url = configData.api_url || 'https://open.bigmodel.cn/api/paas/v4/audio/speech'
        form.zhipu.model = configData.model || 'glm-tts'
        form.zhipu.voice = configData.voice || 'tongtong'
        form.zhipu.response_format = configData.response_format || 'pcm'
        form.zhipu.speed = configData.speed || 1.0
        form.zhipu.volume = configData.volume || 1.0
        form.zhipu.stream = configData.stream !== undefined ? configData.stream : true
        form.zhipu.encode_format = configData.encode_format || 'base64'
        form.zhipu.frame_duration = configData.frame_duration || 60
        break
      case 'minimax':
        form.minimax.api_key = configData.api_key || ''
        form.minimax.model = configData.model || 'speech-2.8-hd'
        form.minimax.voice = configData.voice || 'male-qn-qingse'
        form.minimax.speed = configData.speed || 1.0
        form.minimax.vol = configData.vol || configData.volume || 1.0
        form.minimax.pitch = configData.pitch || 0
        form.minimax.sample_rate = configData.sample_rate || 32000
        form.minimax.bitrate = configData.bitrate || 128000
        form.minimax.format = configData.format || 'mp3'
        form.minimax.channel = configData.channel || 1
        break
    }
  } catch (error) {
    console.error('解析配置JSON失败:', error)
  }
  
  showDialog.value = true
}

const handleSave = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (valid) {
      saving.value = true
      try {
        // 如果是新增配置且当前没有任何配置，则自动设为默认配置
        const isFirstConfig = !editingConfig.value && configs.value.length === 0
        
        const configData = {
          name: form.name,
          config_id: form.config_id,
          provider: form.provider,
          is_default: isFirstConfig || form.is_default, // 首次添加时自动设为默认
          enabled: form.enabled !== undefined ? form.enabled : true,
          json_data: formRef.value.getJsonData()
        }
        
        if (editingConfig.value) {
          await api.put(`/admin/tts-configs/${editingConfig.value.id}`, configData)
          ElMessage.success('配置更新成功')
        } else {
          await api.post('/admin/tts-configs', configData)
          ElMessage.success('配置创建成功')
        }
        
        showDialog.value = false
        loadConfigs()
      } catch (error) {
        ElMessage.error('保存失败: ' + (error.response?.data?.message || error.message))
      } finally {
        saving.value = false
      }
    }
  })
}

const toggleEnable = async (config) => {
  try {
    await api.post(`/admin/configs/${config.id}/toggle`)
    ElMessage.success(`${config.enabled ? '启用' : '禁用'}成功`)
  } catch (error) {
    // 恢复开关状态
    config.enabled = !config.enabled
    ElMessage.error('操作失败')
  }
}

const toggleDefault = async (config) => {
  try {
    if (!config.enabled) {
      ElMessage.warning('请先启用该配置才能设为默认')
      config.is_default = false
      return
    }
    
    const configData = {
      name: config.name,
      config_id: config.config_id,
      provider: config.provider,
      is_default: config.is_default,
      enabled: config.enabled,
      json_data: config.json_data
    }
    
    await api.put(`/admin/tts-configs/${config.id}`, configData)
    ElMessage.success(config.is_default ? '设为默认成功' : '取消默认成功')
    
    // 刷新列表以更新其他配置的默认状态
    loadConfigs()
  } catch (error) {
    // 恢复开关状态
    config.is_default = !config.is_default
    ElMessage.error('操作失败')
  }
}

const getEnabledConfigs = () => {
  return configs.value.filter(config => config.enabled)
}

function formatTestResultLabel(r) {
  if (!r?.ok) return '错误'
  return r.first_packet_ms != null ? `正确 ${r.first_packet_ms}ms` : '正确'
}
function formatTestResultTip(r) {
  if (!r?.ok) return ''
  return r.first_packet_ms != null ? `通过，耗时 ${r.first_packet_ms}ms` : '通过'
}
function formatTestMessage(result) {
  const base = result.message || ''
  return result.first_packet_ms != null ? `${base} ${result.first_packet_ms}ms` : base
}

const testConfig = async (row, type) => {
  testingId.value = row.config_id
  try {
    const result = await testSingleConfig(type, row.config_id)
    testResults.value = { ...testResults.value, [row.config_id]: result }
    if (result.ok) {
      ElMessage.success(`${row.name || row.config_id}：${formatTestMessage(result)}`)
    } else {
      ElMessage.warning(`${row.name || row.config_id}：${result.message}`)
    }
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '测试请求失败')
  } finally {
    testingId.value = null
  }
}

const testAllConfigs = async () => {
  const list = getEnabledConfigs()
  if (!list.length) {
    ElMessage.warning('没有已启用的配置')
    return
  }
  testingAll.value = true
  testResults.value = {}
  let okCount = 0
  try {
    for (const row of list) {
      try {
        const result = await testSingleConfig('tts', row.config_id)
        testResults.value = { ...testResults.value, [row.config_id]: result }
        if (result.ok) okCount++
      } catch (_) {
        testResults.value = { ...testResults.value, [row.config_id]: { ok: false, message: '请求失败' } }
      }
    }
    ElMessage.success(`全部测试完成：${okCount}/${list.length} 通过`)
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '测试请求失败')
  } finally {
    testingAll.value = false
  }
}

const testCurrentConfig = async () => {
  if (!formRef.value) return
  try {
    await formRef.value.validate()
  } catch (_) {
    return
  }
  const configId = form.config_id?.trim()
  if (!configId) {
    ElMessage.warning('请填写配置ID')
    return
  }
  const payload = {
    name: form.name,
    config_id: configId,
    provider: form.provider,
    is_default: form.is_default,
    ...parseJsonData(formRef.value.getJsonData())
  }
  testingCurrent.value = true
  try {
    const result = await testWithData('tts', { [configId]: payload })
    if (result.ok) {
      ElMessage.success(formatTestMessage(result) || '测试通过')
    } else {
      ElMessage.warning(result.message || '测试未通过')
    }
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '测试请求失败')
  } finally {
    testingCurrent.value = false
  }
}

const deleteConfig = async (id) => {
  try {
    await ElMessageBox.confirm('确定要删除这个配置吗？', '提示', {
      confirmButtonText: '确定',
      cancelButtonText: '取消',
      type: 'warning'
    })
    
    await api.delete(`/admin/tts-configs/${id}`)
    ElMessage.success('删除成功')
    loadConfigs()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}

const resetForm = () => {
  editingConfig.value = null
  Object.assign(form, {
    name: '',
    config_id: '',
    provider: 'doubao_ws',
    is_default: false,
    enabled: true,
    cosyvoice: {
      api_url: 'https://tts.linkerai.top/tts',
      spk_id: 'spk_id',
      frame_duration: 60,
      target_sr: 24000,
      audio_format: 'mp3',
      instruct_text: '你好'
    },
    qwen_tts: {
      api_key: '',
      api_url: 'https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation',
      region: 'beijing',
      model: 'qwen3-tts-flash',
      voice: 'Cherry',
      voice_prompt: '',
      language_type: 'Chinese',
      format: 'ogg_opus',
      stream: true,
      frame_duration: 60
    },
    doubao: {
      appid: '6886011847',
      access_token: 'access_token',
      model: 'seed-tts-2.0-standard',
      voice: 'BV001_streaming',
      api_url: 'https://openspeech.bytedance.com/api/v3/tts/unidirectional'
    },
    doubao_ws: {
      appid: '6886011847',
      access_token: 'access_token',
      model: 'seed-tts-2.0-standard',
      resource_id: '',
      voice: '',
      ws_url: 'wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream'
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
    xunfei: {
      app_id: '',
      api_key: '',
      api_secret: '',
      ws_url: 'wss://tts-api.xfyun.cn/v2/tts',
      voice: 'xiaoyan',
      audio_encoding: 'raw',
      sample_rate: 16000,
      speed: 50,
      volume: 50,
      pitch: 50,
      tte: 'UTF8',
      reg: 0,
      rdn: 0,
      frame_duration: 60,
      connect_timeout: 10,
      read_timeout: 30
    },
    xunfei_super_tts: {
      app_id: '',
      api_key: '',
      api_secret: '',
      ws_url: 'wss://cbm01.cn-huabei-1.xf-yun.com/v1/private/mcd9m97e6',
      voice: 'x6_lingxiaoxue_pro',
      audio_encoding: 'raw',
      sample_rate: 24000,
      speed: 50,
      volume: 50,
      pitch: 50,
      bgs: 0,
      reg: 0,
      rdn: 0,
      rhy: 0,
      oral_level: 'mid',
      spark_assist: 1,
      stop_split: 0,
      remain: 0,
      frame_duration: 60,
      connect_timeout: 10,
      read_timeout: 30
    },
    indextts_vllm: {
      api_url: 'http://127.0.0.1:7860',
      api_key: '',
      model: 'indextts-vllm',
      voice: '',
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
}

const handleDialogClose = () => {
  showDialog.value = false
  resetForm()
  if (formRef.value) {
    formRef.value.resetFields()
  }
}

const formatDate = (dateString) => {
  return new Date(dateString).toLocaleString('zh-CN')
}

// 加载音色列表
const loadVoiceOptions = async (provider, options = {}) => {
  const trigger = options?.trigger || 'auto'
  if (!provider) {
    voiceOptions.value = []
    return
  }

  // IndexTTS 仅在下拉展开时请求
  if (provider === 'indextts_vllm' && trigger !== 'dropdown') {
    voiceOptions.value = []
    return
  }
  
  // 只有这些 provider 需要从后端获取音色列表
  if (!TTS_PROVIDERS_WITH_VOICES.includes(provider)) {
    voiceOptions.value = []
    return
  }
  
  voiceLoading.value = true
  try {
    const params = { provider, config_id: form.config_id || undefined }
    if (provider === 'aliyun_qwen') {
      const model = String(form.qwen_tts?.model || '').trim()
      if (model) {
        params.model = model
      }
    }
    if (provider === 'indextts_vllm') {
      const apiURL = String(form.indextts_vllm?.api_url || '').trim()
      const apiKey = String(form.indextts_vllm?.api_key || '').trim()
      if (apiURL) {
        params.api_url = apiURL
      }
      if (apiKey) {
        params.api_key = apiKey
      }
    }
    const previousOptions = voiceOptions.value
    const response = await api.get(`/user/voice-options`, {
      params
    })
    const nextOptions = response.data.data || []
    voiceOptions.value = nextOptions
    if (provider === 'aliyun_qwen') {
      form.qwen_tts.voice = reconcileAliyunVoice(previousOptions, nextOptions, form.qwen_tts.voice)
    }
  } catch (error) {
    console.error('加载音色列表失败:', error)
    voiceOptions.value = []
  } finally {
    voiceLoading.value = false
  }
}

const handleVoiceOptionsRequest = (provider) => {
  if (!showDialog.value) return
  loadVoiceOptions(provider || form.provider, { trigger: 'dropdown' })
}

// 监听 provider 变化，自动加载对应的音色列表
watch(() => form.provider, (newProvider) => {
  if (showDialog.value) {
    loadVoiceOptions(newProvider)
  }
}, { immediate: false })

watch(() => form.qwen_tts.model, (newModel) => {
  if (showDialog.value && form.provider === 'aliyun_qwen') {
    loadVoiceOptions('aliyun_qwen')
  }
})

// 监听对话框打开，加载当前 provider 的音色列表（nextTick 确保弹窗已渲染后再请求）
watch(showDialog, (isOpen) => {
  if (isOpen && form.provider) {
    nextTick(() => loadVoiceOptions(form.provider))
  }
})

onMounted(() => {
  loadConfigs()
})
</script>

<style scoped>
.config-page {
  padding: 20px;
  background: rgba(255, 255, 255, 0.88);
  border-radius: 8px;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
}

.page-actions {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 20px;
}

.test-result { font-size: 12px; }
.test-result.test-ok { color: var(--el-color-success); }
.test-result.test-err { color: var(--el-color-danger); cursor: help; }
.test-result.test-none { color: var(--el-text-color-placeholder); }
</style>
