<template>
  <div class="config-page">
    <div class="page-header">
      <div class="header-left">
        <h2>TTS配置管理</h2>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="showDialog = true">
          <el-icon><Plus /></el-icon>
          添加配置
        </el-button>
      </div>
    </div>

    <el-table :data="configs" style="width: 100%" v-loading="loading">
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="name" label="配置名称" />
      <el-table-column prop="config_id" label="配置ID" width="150" />
      <el-table-column prop="provider" label="提供商" />
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
      <el-table-column prop="created_at" label="创建时间" width="180">
        <template #default="scope">
          {{ formatDate(scope.row.created_at) }}
        </template>
      </el-table-column>
      <el-table-column label="操作" width="180">
        <template #default="scope">
          <el-button size="small" @click="editConfig(scope.row)">编辑</el-button>
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
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="120px"
      >
        <el-form-item label="提供商" prop="provider">
          <el-select v-model="form.provider" placeholder="请选择提供商" style="width: 100%">
            <el-option label="CosyVoice" value="cosyvoice" />
            <el-option label="豆包 TTS" value="doubao" />
            <el-option label="豆包 WebSocket" value="doubao_ws" />
            <el-option label="Edge TTS" value="edge" />
            <el-option label="Edge 离线" value="edge_offline" />
            <el-option label="小智 TTS" value="xiaozhi" />
            <el-option label="Microsoft TTS" value="microsoft" />
            <el-option label="OpenAI" value="openai" />
            <el-option label="千问" value="aliyun_qwen" />
            <el-option label="智谱" value="zhipu" />
            <el-option label="Minimax" value="minimax" />
          </el-select>
        </el-form-item>
        
        <el-form-item label="配置名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入配置名称" />
        </el-form-item>
        
        <el-form-item label="配置ID" prop="config_id">
          <el-input v-model="form.config_id" placeholder="请输入唯一的配置ID" />
        </el-form-item>
        
        <!-- 移除是否默认开关，现在在列表页操作 -->
        
        <!-- CosyVoice 配置 -->
        <template v-if="form.provider === 'cosyvoice'">
          <el-form-item label="API URL" prop="cosyvoice.api_url">
            <el-input v-model="form.cosyvoice.api_url" placeholder="请输入API URL" />
          </el-form-item>
          <el-form-item label="说话人ID" prop="cosyvoice.spk_id">
            <el-input v-model="form.cosyvoice.spk_id" placeholder="请输入说话人ID" />
          </el-form-item>
          <el-form-item label="帧时长" prop="cosyvoice.frame_duration">
            <el-input-number v-model="form.cosyvoice.frame_duration" :min="1" :max="1000" style="width: 100%" />
          </el-form-item>
          <el-form-item label="目标采样率" prop="cosyvoice.target_sr">
            <el-input-number v-model="form.cosyvoice.target_sr" :min="8000" :max="48000" style="width: 100%" />
          </el-form-item>
          <el-form-item label="音频格式" prop="cosyvoice.audio_format">
            <el-select v-model="form.cosyvoice.audio_format" placeholder="请选择音频格式" style="width: 100%">
              <el-option label="MP3" value="mp3" />
              <el-option label="WAV" value="wav" />
              <el-option label="PCM" value="pcm" />
            </el-select>
          </el-form-item>
          <el-form-item label="指令文本" prop="cosyvoice.instruct_text">
            <el-input v-model="form.cosyvoice.instruct_text" placeholder="请输入指令文本" />
          </el-form-item>
        </template>

        <!-- 豆包 TTS 配置 -->
        <template v-if="form.provider === 'doubao'">
          <el-form-item label="应用ID" prop="doubao.appid">
            <el-input v-model="form.doubao.appid" placeholder="请输入应用ID" />
          </el-form-item>
          <el-form-item label="访问令牌" prop="doubao.access_token">
            <el-input v-model="form.doubao.access_token" placeholder="请输入访问令牌" type="password" show-password />
          </el-form-item>
          <el-form-item label="集群" prop="doubao.cluster">
            <el-input v-model="form.doubao.cluster" placeholder="请输入集群名称" />
          </el-form-item>
          <el-form-item label="音色" prop="doubao.voice">
            <el-select 
              v-model="form.doubao.voice" 
              placeholder="请选择音色" 
              style="width: 100%" 
              filterable
              :loading="voiceLoading"
              :disabled="voiceLoading"
              allow-create
              default-first-option
            >
              <el-option 
                v-for="option in voiceOptions" 
                :key="option.value" 
                :label="option.label" 
                :value="option.value" 
              />
            </el-select>
          </el-form-item>
          <el-form-item label="API URL" prop="doubao.api_url">
            <el-input v-model="form.doubao.api_url" placeholder="请输入API URL" />
          </el-form-item>
          <el-form-item label="授权信息" prop="doubao.authorization">
            <el-input v-model="form.doubao.authorization" placeholder="请输入授权信息" type="password" show-password />
          </el-form-item>
        </template>

        <!-- 豆包 WebSocket 配置 -->
        <template v-if="form.provider === 'doubao_ws'">
          <el-form-item label="应用ID" prop="doubao_ws.appid">
            <el-input v-model="form.doubao_ws.appid" placeholder="请输入应用ID" />
          </el-form-item>
          <el-form-item label="访问令牌" prop="doubao_ws.access_token">
            <el-input v-model="form.doubao_ws.access_token" placeholder="请输入访问令牌" type="password" show-password />
          </el-form-item>
          <el-form-item label="集群" prop="doubao_ws.cluster">
            <el-input v-model="form.doubao_ws.cluster" placeholder="请输入集群名称" />
          </el-form-item>
          <el-form-item label="音色" prop="doubao_ws.voice">
            <el-select 
              v-model="form.doubao_ws.voice" 
              placeholder="请选择音色" 
              style="width: 100%" 
              filterable
              :loading="voiceLoading"
              :disabled="voiceLoading"
              allow-create
              default-first-option
            >
              <el-option 
                v-for="option in voiceOptions" 
                :key="option.value" 
                :label="option.label" 
                :value="option.value" 
              />
            </el-select>
          </el-form-item>
          <el-form-item label="WebSocket主机" prop="doubao_ws.ws_host">
            <el-input v-model="form.doubao_ws.ws_host" placeholder="请输入WebSocket主机地址" />
          </el-form-item>
          <el-form-item label="使用流式" prop="doubao_ws.use_stream">
            <el-switch v-model="form.doubao_ws.use_stream" />
          </el-form-item>
        </template>

        <!-- Edge TTS 配置 -->
        <template v-if="form.provider === 'edge'">
          <el-form-item label="音色" prop="edge.voice">
            <el-select 
              v-model="form.edge.voice" 
              placeholder="请选择音色" 
              style="width: 100%" 
              filterable
              :loading="voiceLoading"
              :disabled="voiceLoading"
              allow-create
              default-first-option
            >
              <el-option 
                v-for="option in voiceOptions" 
                :key="option.value" 
                :label="option.label" 
                :value="option.value" 
              />
            </el-select>
          </el-form-item>
          <el-form-item label="语速" prop="edge.rate">
            <el-input v-model="form.edge.rate" placeholder="请输入语速（如：+0%）" />
          </el-form-item>
          <el-form-item label="音量" prop="edge.volume">
            <el-input v-model="form.edge.volume" placeholder="请输入音量（如：+0%）" />
          </el-form-item>
          <el-form-item label="音调" prop="edge.pitch">
            <el-input v-model="form.edge.pitch" placeholder="请输入音调（如：+0Hz）" />
          </el-form-item>
          <el-form-item label="连接超时" prop="edge.connect_timeout">
            <el-input-number v-model="form.edge.connect_timeout" :min="1" :max="60" style="width: 100%" />
          </el-form-item>
          <el-form-item label="接收超时" prop="edge.receive_timeout">
            <el-input-number v-model="form.edge.receive_timeout" :min="1" :max="300" style="width: 100%" />
          </el-form-item>
        </template>

        <!-- Edge 离线配置 -->
        <template v-if="form.provider === 'edge_offline'">
          <el-form-item label="服务器URL" prop="edge_offline.server_url">
            <el-input v-model="form.edge_offline.server_url" placeholder="请输入服务器URL" />
          </el-form-item>
          <el-form-item label="超时时间" prop="edge_offline.timeout">
            <el-input-number v-model="form.edge_offline.timeout" :min="1" :max="300" style="width: 100%" />
          </el-form-item>
          <el-form-item label="采样率" prop="edge_offline.sample_rate">
            <el-input-number v-model="form.edge_offline.sample_rate" :min="8000" :max="48000" style="width: 100%" />
          </el-form-item>
          <el-form-item label="声道数" prop="edge_offline.channels">
            <el-input-number v-model="form.edge_offline.channels" :min="1" :max="8" style="width: 100%" />
          </el-form-item>
          <el-form-item label="帧时长" prop="edge_offline.frame_duration">
            <el-input-number v-model="form.edge_offline.frame_duration" :min="1" :max="100" style="width: 100%" />
          </el-form-item>
        </template>

        <!-- 千问 TTS 配置 -->
        <template v-if="form.provider === 'aliyun_qwen'">
          <el-form-item label="API Key" prop="qwen_tts.api_key">
            <el-input v-model="form.qwen_tts.api_key" placeholder="请输入API Key" type="password" show-password />
          </el-form-item>
          <el-form-item label="地域" prop="qwen_tts.region">
            <el-select v-model="form.qwen_tts.region" placeholder="请选择地域" style="width: 100%">
              <el-option label="北京" value="beijing" />
              <el-option label="新加坡" value="singapore" />
            </el-select>
          </el-form-item>
          <el-form-item label="模型" prop="qwen_tts.model">
            <el-input v-model="form.qwen_tts.model" placeholder="qwen3-tts-flash" />
          </el-form-item>
          <el-form-item label="音色" prop="qwen_tts.voice">
            <el-input v-model="form.qwen_tts.voice" placeholder="Cherry" />
          </el-form-item>
          <el-form-item label="语种" prop="qwen_tts.language_type">
            <el-select v-model="form.qwen_tts.language_type" placeholder="请选择语种" style="width: 100%">
              <el-option label="自动" value="Auto" />
              <el-option label="中文" value="Chinese" />
              <el-option label="英文" value="English" />
            </el-select>
          </el-form-item>
          <el-form-item label="使用流式" prop="qwen_tts.stream">
            <el-switch v-model="form.qwen_tts.stream" />
          </el-form-item>
          <el-form-item label="帧时长" prop="qwen_tts.frame_duration">
            <el-input-number v-model="form.qwen_tts.frame_duration" :min="1" :max="1000" style="width: 100%" />
          </el-form-item>
        </template>

        <!-- 智谱 TTS 配置（使用 OpenAI 格式） -->
        <template v-if="form.provider === 'zhipu'">
          <el-form-item label="API Key" prop="zhipu.api_key">
            <el-input v-model="form.zhipu.api_key" placeholder="请输入API Key" type="password" show-password />
          </el-form-item>
          <el-form-item label="API URL" prop="zhipu.api_url">
            <el-input v-model="form.zhipu.api_url" placeholder="https://open.bigmodel.cn/api/paas/v4/audio/speech" />
          </el-form-item>
          <el-form-item label="模型" prop="zhipu.model">
            <el-input v-model="form.zhipu.model" placeholder="glm-tts" />
          </el-form-item>
          <el-form-item label="音色" prop="zhipu.voice">
            <el-select 
              v-model="form.zhipu.voice" 
              placeholder="请选择音色" 
              style="width: 100%" 
              filterable
              :loading="voiceLoading"
              :disabled="voiceLoading"
            >
              <el-option 
                v-for="option in voiceOptions" 
                :key="option.value" 
                :label="option.label" 
                :value="option.value" 
              />
            </el-select>
          </el-form-item>
          <el-form-item label="响应格式" prop="zhipu.response_format">
            <el-select v-model="form.zhipu.response_format" placeholder="请选择响应格式" style="width: 100%">
              <el-option label="WAV" value="wav" />
              <el-option label="PCM" value="pcm" />
            </el-select>
          </el-form-item>
          <el-form-item label="音量" prop="zhipu.volume">
            <el-input-number v-model="form.zhipu.volume" :min="0" :max="10" :step="0.1" style="width: 100%" placeholder="0-10，默认1.0" />
          </el-form-item>
          <el-form-item label="语速" prop="zhipu.speed">
            <el-input-number v-model="form.zhipu.speed" :min="0.5" :max="2.0" :step="0.1" style="width: 100%" placeholder="0.5-2.0，默认1.0" />
          </el-form-item>
          <el-form-item label="使用流式" prop="zhipu.stream">
            <el-switch v-model="form.zhipu.stream" />
          </el-form-item>
          <el-form-item v-if="form.zhipu.stream" label="编码格式" prop="zhipu.encode_format">
            <el-select v-model="form.zhipu.encode_format" placeholder="请选择编码格式" style="width: 100%">
              <el-option label="Base64" value="base64" />
              <el-option label="Hex" value="hex" />
            </el-select>
          </el-form-item>
          <el-form-item label="帧时长" prop="zhipu.frame_duration">
            <el-input-number v-model="form.zhipu.frame_duration" :min="1" :max="1000" style="width: 100%" placeholder="毫秒" />
          </el-form-item>
        </template>

        <!-- Minimax TTS 配置 -->
        <template v-if="form.provider === 'minimax'">
          <el-form-item label="API Key" prop="minimax.api_key">
            <el-input v-model="form.minimax.api_key" placeholder="请输入API Key" type="password" show-password />
          </el-form-item>
          <el-form-item label="模型" prop="minimax.model">
            <el-input v-model="form.minimax.model" placeholder="speech-2.8-hd" />
          </el-form-item>
          <el-form-item label="音色" prop="minimax.voice">
            <el-select 
              v-model="form.minimax.voice" 
              placeholder="请选择音色" 
              style="width: 100%" 
              filterable
              :loading="voiceLoading"
              :disabled="voiceLoading"
              allow-create
              default-first-option
            >
              <el-option 
                v-for="option in voiceOptions" 
                :key="option.value" 
                :label="option.label" 
                :value="option.value" 
              />
            </el-select>
          </el-form-item>
          <el-form-item label="语速" prop="minimax.speed">
            <el-input-number v-model="form.minimax.speed" :min="0.5" :max="2.0" :step="0.1" style="width: 100%" placeholder="0.5-2.0，默认1.0" />
          </el-form-item>
          <el-form-item label="音量" prop="minimax.vol">
            <el-input-number v-model="form.minimax.vol" :min="0" :max="2" :step="0.1" style="width: 100%" placeholder="0-2，默认1.0" />
          </el-form-item>
          <el-form-item label="音调" prop="minimax.pitch">
            <el-input-number v-model="form.minimax.pitch" :min="-12" :max="12" :step="1" style="width: 100%" placeholder="-12到12，默认0" />
          </el-form-item>
          <el-form-item label="采样率" prop="minimax.sample_rate">
            <el-input-number v-model="form.minimax.sample_rate" :min="8000" :max="48000" :step="1000" style="width: 100%" placeholder="默认32000" />
          </el-form-item>
          <el-form-item label="比特率" prop="minimax.bitrate">
            <el-input-number v-model="form.minimax.bitrate" :min="32000" :max="320000" :step="16000" style="width: 100%" placeholder="默认128000" />
          </el-form-item>
          <el-form-item label="音频格式" prop="minimax.format">
            <el-select v-model="form.minimax.format" placeholder="请选择音频格式" style="width: 100%">
              <el-option label="MP3" value="mp3" />
              <el-option label="WAV" value="wav" />
              <el-option label="PCM" value="pcm" />
            </el-select>
          </el-form-item>
          <el-form-item label="声道数" prop="minimax.channel">
            <el-input-number v-model="form.minimax.channel" :min="1" :max="2" style="width: 100%" placeholder="默认1" />
          </el-form-item>
        </template>

        <!-- OpenAI TTS 配置 -->
        <template v-if="form.provider === 'openai'">
          <el-form-item label="API Key" prop="openai.api_key">
            <el-input v-model="form.openai.api_key" placeholder="请输入API Key" type="password" show-password />
          </el-form-item>
          <el-form-item label="API URL" prop="openai.api_url">
            <el-input v-model="form.openai.api_url" placeholder="请输入API URL（默认：https://api.openai.com/v1/audio/speech）" />
          </el-form-item>
          <el-form-item label="模型" prop="openai.model">
            <el-input v-model="form.openai.model" placeholder="请输入模型（默认：tts-1）" />
          </el-form-item>
          <el-form-item label="音色" prop="openai.voice">
            <el-select 
              v-model="form.openai.voice" 
              placeholder="请选择音色" 
              style="width: 100%" 
              filterable
              :loading="voiceLoading"
              :disabled="voiceLoading"
            >
              <el-option 
                v-for="option in voiceOptions" 
                :key="option.value" 
                :label="option.label" 
                :value="option.value" 
              />
            </el-select>
          </el-form-item>
          <el-form-item label="响应格式" prop="openai.response_format">
            <el-select v-model="form.openai.response_format" placeholder="请选择响应格式" style="width: 100%">
              <el-option label="MP3" value="mp3" />
              <el-option label="Opus" value="opus" />
              <el-option label="AAC" value="aac" />
              <el-option label="FLAC" value="flac" />
              <el-option label="WAV" value="wav" />
              <el-option label="PCM" value="pcm" />
            </el-select>
          </el-form-item>
          <el-form-item label="语速" prop="openai.speed">
            <el-input-number v-model="form.openai.speed" :min="0.25" :max="4.0" :step="0.1" style="width: 100%" placeholder="0.25-4.0，默认1.0" />
          </el-form-item>
          <el-form-item label="使用流式" prop="openai.stream">
            <el-switch v-model="form.openai.stream" />
          </el-form-item>
          <el-form-item label="帧时长" prop="openai.frame_duration">
            <el-input-number v-model="form.openai.frame_duration" :min="1" :max="1000" style="width: 100%" placeholder="毫秒" />
          </el-form-item>
        </template>

        <!-- 小智 TTS 配置 -->
        <template v-if="form.provider === 'xiaozhi'">
          <el-form-item label="服务器地址" prop="xiaozhi.server_addr">
            <el-input v-model="form.xiaozhi.server_addr" placeholder="请输入服务器地址（如：wss://api.tenclass.net/xiaozhi/v1/）" />
          </el-form-item>
          <el-form-item label="设备ID" prop="xiaozhi.device_id">
            <el-input v-model="form.xiaozhi.device_id" placeholder="请输入设备ID" />
          </el-form-item>
          <el-form-item label="客户端ID" prop="xiaozhi.client_id">
            <el-input v-model="form.xiaozhi.client_id" placeholder="请输入客户端ID" />
          </el-form-item>
          <el-form-item label="令牌" prop="xiaozhi.token">
            <el-input v-model="form.xiaozhi.token" placeholder="请输入令牌" type="password" show-password />
          </el-form-item>
        </template>

        <!-- Microsoft TTS 配置 -->
        <template v-if="form.provider === 'microsoft'">
          <el-form-item label="订阅密钥" prop="microsoft.subscription_key">
            <el-input v-model="form.microsoft.subscription_key" placeholder="请输入Azure订阅密钥" type="password" show-password />
          </el-form-item>
          <el-form-item label="服务区域" prop="microsoft.region">
            <el-input v-model="form.microsoft.region" placeholder="请输入服务区域（如：eastasia, koreacentral）" />
          </el-form-item>
          <el-form-item label="语音名称" prop="microsoft.voice">
            <el-input v-model="form.microsoft.voice" placeholder="请输入语音名称（如：zh-CN-XiaoxiaoNeural）" />
          </el-form-item>
          <el-form-item label="语言代码" prop="microsoft.language">
            <el-input v-model="form.microsoft.language" placeholder="请输入语言代码（如：zh-CN）" />
          </el-form-item>
          <el-form-item label="超时时间（秒）" prop="microsoft.timeout">
            <el-input-number v-model="form.microsoft.timeout" :min="1" :max="300" style="width: 100%" />
          </el-form-item>
        </template>
      </el-form>
      
      <template #footer>
        <el-button @click="handleDialogClose">取消</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">
          保存
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import api from '../../utils/api'

const configs = ref([])
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
  provider: 'cosyvoice',
  is_default: false,
  enabled: true,
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
    language_type: 'Chinese',
    stream: true,
    frame_duration: 60
  },
  doubao: {
    appid: '6886011847',
    access_token: 'access_token',
    cluster: 'volcano_tts',
    voice: 'BV001_streaming',
    api_url: 'https://openspeech.bytedance.com/api/v1/tts',
    authorization: 'Bearer;'
  },
  doubao_ws: {
    appid: '6886011847',
    access_token: 'access_token',
    cluster: 'volcano_tts',
    voice: 'zh_female_wanwanxiaohe_moon_bigtts',
    ws_host: 'openspeech.bytedance.com',
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
  xiaozhi: {
    server_addr: 'wss://api.tenclass.net/xiaozhi/v1/',
    device_id: 'ba:8f:17:de:94:94',
    client_id: 'e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6d',
    token: 'test-token'
  },
  microsoft: {
    subscription_key: '',
    region: 'eastasia',
    voice: 'zh-CN-XiaoxiaoNeural',
    language: 'zh-CN',
    timeout: 60
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

const generateConfig = () => {
  const config = {}
  
  switch (form.provider) {
    case 'cosyvoice':
      config.api_url = form.cosyvoice.api_url
      config.spk_id = form.cosyvoice.spk_id
      config.frame_duration = form.cosyvoice.frame_duration
      config.target_sr = form.cosyvoice.target_sr
      config.audio_format = form.cosyvoice.audio_format
      config.instruct_text = form.cosyvoice.instruct_text
      break
    case 'doubao':
      config.appid = form.doubao.appid
      config.access_token = form.doubao.access_token
      config.cluster = form.doubao.cluster
      config.voice = form.doubao.voice
      config.api_url = form.doubao.api_url
      config.authorization = form.doubao.authorization
      break
    case 'doubao_ws':
      config.appid = form.doubao_ws.appid
      config.access_token = form.doubao_ws.access_token
      config.cluster = form.doubao_ws.cluster
      config.voice = form.doubao_ws.voice
      config.ws_host = form.doubao_ws.ws_host
      config.use_stream = form.doubao_ws.use_stream
      break
    case 'edge':
      config.voice = form.edge.voice
      config.rate = form.edge.rate
      config.volume = form.edge.volume
      config.pitch = form.edge.pitch
      config.connect_timeout = form.edge.connect_timeout
      config.receive_timeout = form.edge.receive_timeout
      break
    case 'edge_offline':
      config.server_url = form.edge_offline.server_url
      config.timeout = form.edge_offline.timeout
      config.sample_rate = form.edge_offline.sample_rate
      config.channels = form.edge_offline.channels
      config.frame_duration = form.edge_offline.frame_duration
      break
    case 'aliyun_qwen':
      config.provider = 'aliyun_qwen'
      config.api_key = form.qwen_tts.api_key
      config.api_url = form.qwen_tts.api_url
      config.region = form.qwen_tts.region
      config.model = form.qwen_tts.model || 'qwen3-tts-flash'
      config.voice = form.qwen_tts.voice || 'Cherry'
      config.language_type = form.qwen_tts.language_type || 'Chinese'
      config.stream = form.qwen_tts.stream
      config.frame_duration = form.qwen_tts.frame_duration || 60
      break
    case 'openai':
      config.api_key = form.openai.api_key
      config.api_url = form.openai.api_url
      config.model = form.openai.model
      config.voice = form.openai.voice
      config.response_format = form.openai.response_format
      config.speed = form.openai.speed
      config.stream = form.openai.stream
      config.frame_duration = form.openai.frame_duration
      break
    case 'zhipu':
      // 智谱 TTS 配置
      config.provider = 'zhipu'
      config.api_key = form.zhipu.api_key
      config.api_url = form.zhipu.api_url || 'https://open.bigmodel.cn/api/paas/v4/audio/speech'
      config.model = form.zhipu.model || 'glm-tts'
      config.voice = form.zhipu.voice
      config.response_format = form.zhipu.response_format
      config.speed = form.zhipu.speed
      config.volume = form.zhipu.volume || 1.0
      config.stream = form.zhipu.stream
      config.encode_format = form.zhipu.encode_format || 'base64'
      config.frame_duration = form.zhipu.frame_duration
      break
    case 'minimax':
      // Minimax TTS 配置
      config.provider = 'minimax'
      config.api_key = form.minimax.api_key
      config.model = form.minimax.model || 'speech-2.8-hd'
      config.voice = form.minimax.voice || 'male-qn-qingse'
      config.speed = form.minimax.speed || 1.0
      config.vol = form.minimax.vol || 1.0
      config.pitch = form.minimax.pitch || 0
      config.sample_rate = form.minimax.sample_rate || 32000
      config.bitrate = form.minimax.bitrate || 128000
      config.format = form.minimax.format || 'mp3'
      config.channel = form.minimax.channel || 1
      break
    case 'xiaozhi':
      config.server_addr = form.xiaozhi.server_addr
      config.device_id = form.xiaozhi.device_id
      config.client_id = form.xiaozhi.client_id
      config.token = form.xiaozhi.token
      break
    case 'microsoft':
      config.subscription_key = form.microsoft.subscription_key
      config.region = form.microsoft.region
      config.voice = form.microsoft.voice
      config.language = form.microsoft.language
      config.timeout = form.microsoft.timeout
      break
  }
  
  return JSON.stringify(config)
}

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
  'doubao.cluster': [{ required: true, message: '请输入集群', trigger: 'blur' }],
  'doubao.voice': [{ required: true, message: '请输入音色', trigger: 'blur' }],
  'doubao.api_url': [{ required: true, message: '请输入API URL', trigger: 'blur' }],
  // 豆包 WebSocket 验证规则
  'doubao_ws.appid': [{ required: true, message: '请输入应用ID', trigger: 'blur' }],
  'doubao_ws.access_token': [{ required: true, message: '请输入访问令牌', trigger: 'blur' }],
  'doubao_ws.cluster': [{ required: true, message: '请输入集群', trigger: 'blur' }],
  'doubao_ws.voice': [{ required: true, message: '请输入音色', trigger: 'blur' }],
  'doubao_ws.ws_host': [{ required: true, message: '请输入WebSocket主机', trigger: 'blur' }],
  // Edge TTS 验证规则
  'edge.voice': [{ required: true, message: '请输入音色', trigger: 'blur' }],
  'edge.rate': [{ required: true, message: '请输入语速', trigger: 'blur' }],
  'edge.volume': [{ required: true, message: '请输入音量', trigger: 'blur' }],
  // Edge 离线验证规则
  'edge_offline.server_url': [{ required: true, message: '请输入服务器URL', trigger: 'blur' }],
  // 小智 TTS 验证规则
  'xiaozhi.server_addr': [{ required: true, message: '请输入服务器地址', trigger: 'blur' }],
  'xiaozhi.device_id': [{ required: true, message: '请输入设备ID', trigger: 'blur' }],
  'xiaozhi.client_id': [{ required: true, message: '请输入客户端ID', trigger: 'blur' }],
  'xiaozhi.token': [{ required: true, message: '请输入令牌', trigger: 'blur' }],
  // Microsoft TTS 验证规则
  'microsoft.subscription_key': [{ required: true, message: '请输入订阅密钥', trigger: 'blur' }],
  'microsoft.region': [{ required: true, message: '请输入服务区域', trigger: 'blur' }],
  'microsoft.voice': [{ required: true, message: '请输入语音名称', trigger: 'blur' }],
  'microsoft.language': [{ required: true, message: '请输入语言代码', trigger: 'blur' }],
  // OpenAI TTS 验证规则
  'openai.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  // 智谱 TTS 验证规则
  'zhipu.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  // Minimax TTS 验证规则
  'minimax.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }],
  // 千问 TTS 验证规则
  'qwen_tts.api_key': [{ required: true, message: '请输入API Key', trigger: 'blur' }]
}

const loadConfigs = async () => {
  loading.value = true
  try {
    const response = await api.get('/admin/tts-configs')
    configs.value = response.data.data || []
  } catch (error) {
    ElMessage.error('加载配置失败')
  } finally {
    loading.value = false
  }
}

const editConfig = (config) => {
  editingConfig.value = config
  form.name = config.name
  form.config_id = config.config_id
  form.provider = config.provider
  form.is_default = config.is_default
  form.enabled = config.enabled
  
  // 加载对应 provider 的音色列表
  loadVoiceOptions(config.provider)
  
  // 解析配置JSON并填充到对应的表单字段
  try {
    const configData = JSON.parse(config.json_data || '{}')
    
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
        form.doubao.cluster = configData.cluster || ''
        form.doubao.voice = configData.voice || ''
        form.doubao.api_url = configData.api_url || ''
        form.doubao.authorization = configData.authorization || ''
        break
      case 'doubao_ws':
        form.doubao_ws.appid = configData.appid || ''
        form.doubao_ws.access_token = configData.access_token || ''
        form.doubao_ws.cluster = configData.cluster || ''
        form.doubao_ws.voice = configData.voice || ''
        form.doubao_ws.ws_host = configData.ws_host || ''
        form.doubao_ws.use_stream = configData.use_stream !== undefined ? configData.use_stream : true
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
        form.qwen_tts.api_key = configData.api_key || ''
        form.qwen_tts.api_url = configData.api_url || 'https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation'
        form.qwen_tts.region = configData.region || 'beijing'
        form.qwen_tts.model = configData.model || 'qwen3-tts-flash'
        form.qwen_tts.voice = configData.voice || 'Cherry'
        form.qwen_tts.language_type = configData.language_type || 'Chinese'
        form.qwen_tts.stream = configData.stream !== undefined ? configData.stream : true
        form.qwen_tts.frame_duration = configData.frame_duration || 60
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
      case 'xiaozhi':
        form.xiaozhi.server_addr = configData.server_addr || ''
        form.xiaozhi.device_id = configData.device_id || ''
        form.xiaozhi.client_id = configData.client_id || ''
        form.xiaozhi.token = configData.token || ''
        break
      case 'microsoft':
        form.microsoft.subscription_key = configData.subscription_key || ''
        form.microsoft.region = configData.region || 'eastasia'
        form.microsoft.voice = configData.voice || 'zh-CN-XiaoxiaoNeural'
        form.microsoft.language = configData.language || 'zh-CN'
        form.microsoft.timeout = configData.timeout || 60
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
          json_data: generateConfig()
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
    provider: 'cosyvoice',
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
      language_type: 'Chinese',
      stream: true,
      frame_duration: 60
    },
    doubao: {
      appid: '6886011847',
      access_token: 'access_token',
      cluster: 'volcano_tts',
      voice: 'BV001_streaming',
      api_url: 'https://openspeech.bytedance.com/api/v1/tts',
      authorization: 'Bearer;'
    },
    doubao_ws: {
      appid: '6886011847',
      access_token: 'access_token',
      cluster: 'volcano_tts',
      voice: 'zh_female_wanwanxiaohe_moon_bigtts',
      ws_host: 'openspeech.bytedance.com',
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
    xiaozhi: {
      server_addr: 'wss://api.tenclass.net/xiaozhi/v1/',
      device_id: 'ba:8f:17:de:94:94',
      client_id: 'e4b0c442-98fc-4e1b-8c3d-6a5b6a5b6a6d',
      token: 'test-token'
    },
    microsoft: {
      subscription_key: '',
      region: 'eastasia',
      voice: 'zh-CN-XiaoxiaoNeural',
      language: 'zh-CN',
      timeout: 60
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
const loadVoiceOptions = async (provider) => {
  if (!provider) {
    voiceOptions.value = []
    return
  }
  
  // 只有这些 provider 需要从后端获取音色列表
  const providersWithVoices = ['minimax', 'edge', 'doubao', 'doubao_ws', 'zhipu', 'openai']
  if (!providersWithVoices.includes(provider)) {
    voiceOptions.value = []
    return
  }
  
  voiceLoading.value = true
  try {
    const response = await api.get(`/user/voice-options`, {
      params: { provider }
    })
    voiceOptions.value = response.data.data || []
  } catch (error) {
    console.error('加载音色列表失败:', error)
    voiceOptions.value = []
  } finally {
    voiceLoading.value = false
  }
}

// 监听 provider 变化，自动加载对应的音色列表
watch(() => form.provider, (newProvider) => {
  if (showDialog.value) {
    loadVoiceOptions(newProvider)
  }
}, { immediate: false })

// 监听对话框打开，加载当前 provider 的音色列表
watch(showDialog, (isOpen) => {
  if (isOpen && form.provider) {
    loadVoiceOptions(form.provider)
  }
})

onMounted(() => {
  loadConfigs()
})
</script>

<style scoped>
.config-page {
  padding: 20px;
  background: white;
  border-radius: 8px;
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header-left h2 {
  margin: 0;
  color: #333;
}
</style>