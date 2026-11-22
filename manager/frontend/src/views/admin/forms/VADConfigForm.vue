<template>
  <el-form ref="formRef" :model="model" :rules="rules" label-width="120px">
    <el-form-item label="提供商" prop="provider">
      <el-select v-model="model.provider" placeholder="请选择提供商" style="width: 100%">
        <el-option label="TEN VAD" value="ten_vad" />
        <el-option label="Silero VAD" value="silero_vad" />
      </el-select>
    </el-form-item>
    <el-form-item label="配置名称" prop="name">
      <el-input v-model="model.name" placeholder="请输入配置名称" />
    </el-form-item>
    <el-form-item label="配置ID" prop="config_id">
      <el-input v-model="model.config_id" placeholder="请输入唯一的配置ID" />
    </el-form-item>
    <template v-if="model.provider === 'webrtc_vad'">
      <el-divider content-position="left">WebRTC VAD 配置</el-divider>
      <el-form-item label="最小连接池大小" prop="webrtc_vad.pool_min_size">
        <el-input-number v-model="model.webrtc_vad.pool_min_size" :min="1" :max="1000" style="width: 100%" />
      </el-form-item>
      <el-form-item label="最大连接池大小" prop="webrtc_vad.pool_max_size">
        <el-input-number v-model="model.webrtc_vad.pool_max_size" :min="1" :max="10000" style="width: 100%" />
      </el-form-item>
      <el-form-item label="最大空闲连接数" prop="webrtc_vad.pool_max_idle">
        <el-input-number v-model="model.webrtc_vad.pool_max_idle" :min="1" :max="1000" style="width: 100%" />
      </el-form-item>
      <el-form-item label="VAD采样率" prop="webrtc_vad.vad_sample_rate">
        <el-select v-model="model.webrtc_vad.vad_sample_rate" style="width: 100%">
          <el-option label="8000 Hz" :value="8000" />
          <el-option label="16000 Hz" :value="16000" />
          <el-option label="32000 Hz" :value="32000" />
          <el-option label="48000 Hz" :value="48000" />
        </el-select>
      </el-form-item>
      <el-form-item label="VAD模式" prop="webrtc_vad.vad_mode">
        <el-select v-model="model.webrtc_vad.vad_mode" style="width: 100%">
          <el-option label="模式0 (质量优先)" :value="0" />
          <el-option label="模式1 (低延迟)" :value="1" />
          <el-option label="模式2 (平衡)" :value="2" />
          <el-option label="模式3 (高精度)" :value="3" />
        </el-select>
      </el-form-item>
    </template>
    <template v-if="model.provider === 'silero_vad'">
      <el-divider content-position="left">Silero VAD 配置</el-divider>
      <el-form-item label="模型路径" prop="silero_vad.model_path">
        <el-input v-model="model.silero_vad.model_path" placeholder="请输入模型文件路径" />
      </el-form-item>
      <el-form-item label="进入阈值" prop="silero_vad.threshold">
        <el-input-number v-model="model.silero_vad.threshold" :min="0" :max="1" :step="0.1" :precision="2" style="width: 100%" />
      </el-form-item>
      <el-form-item label="退出阈值" prop="silero_vad.threshold_low">
        <el-input-number v-model="model.silero_vad.threshold_low" :min="0" :max="1" :step="0.1" :precision="2" style="width: 100%" />
      </el-form-item>
      <el-form-item label="最小静音持续时间(ms)" prop="silero_vad.min_silence_duration_ms">
        <el-input-number v-model="model.silero_vad.min_silence_duration_ms" :min="10" :max="5000" style="width: 100%" />
      </el-form-item>
      <el-form-item label="采样率" prop="silero_vad.sample_rate">
        <el-select v-model="model.silero_vad.sample_rate" style="width: 100%">
          <el-option label="8000 Hz" :value="8000" />
          <el-option label="16000 Hz" :value="16000" />
        </el-select>
      </el-form-item>
      <el-form-item label="声道数" prop="silero_vad.channels">
        <el-select v-model="model.silero_vad.channels" style="width: 100%">
          <el-option label="单声道" :value="1" />
          <el-option label="双声道" :value="2" />
        </el-select>
      </el-form-item>
      <el-form-item label="Session数" prop="silero_vad.pool_size">
        <el-input-number v-model="model.silero_vad.pool_size" :min="1" :max="100" style="width: 100%" />
        <div style="font-size: 12px; color: #909399; margin-top: 4px;">默认：CPU 核心数</div>
      </el-form-item>
      <el-form-item label="获取超时时间(ms)" prop="silero_vad.acquire_timeout_ms">
        <el-input-number v-model="model.silero_vad.acquire_timeout_ms" :min="100" :max="30000" style="width: 100%" />
      </el-form-item>
    </template>
    <template v-if="model.provider === 'ten_vad'">
      <el-divider content-position="left">TEN VAD 配置</el-divider>
      <el-form-item label="帧移大小" prop="ten_vad.hop_size">
        <el-input-number v-model="model.ten_vad.hop_size" :min="128" :max="1024" style="width: 100%" />
        <div style="font-size: 12px; color: #909399; margin-top: 4px;">默认：320</div>
      </el-form-item>
      <el-form-item label="进入阈值" prop="ten_vad.threshold">
        <el-input-number v-model="model.ten_vad.threshold" :min="0" :max="1" :step="0.1" :precision="2" style="width: 100%" />
        <div style="font-size: 12px; color: #909399; margin-top: 4px;">推荐值：0.3</div>
      </el-form-item>
      <el-form-item label="退出阈值" prop="ten_vad.threshold_low">
        <el-input-number v-model="model.ten_vad.threshold_low" :min="0" :max="1" :step="0.1" :precision="2" style="width: 100%" />
        <div style="font-size: 12px; color: #909399; margin-top: 4px;">推荐值：0.2</div>
      </el-form-item>
      <el-form-item label="连接池大小" prop="ten_vad.pool_size">
        <el-input-number v-model="model.ten_vad.pool_size" :min="1" :max="100" style="width: 100%" />
        <div style="font-size: 12px; color: #909399; margin-top: 4px;">推荐值：10</div>
      </el-form-item>
      <el-form-item label="获取超时时间(ms)" prop="ten_vad.acquire_timeout_ms">
        <el-input-number v-model="model.ten_vad.acquire_timeout_ms" :min="100" :max="30000" style="width: 100%" />
        <div style="font-size: 12px; color: #909399; margin-top: 4px;">推荐值：3000</div>
      </el-form-item>
    </template>
  </el-form>
</template>

<script setup>
import { ref, computed } from 'vue'

const props = defineProps({
  model: { type: Object, required: true },
  rules: { type: Object, default: () => ({}) }
})

const formRef = ref()

function getJsonData() {
  const m = props.model
  if (m.provider === 'webrtc_vad') return JSON.stringify(m.webrtc_vad || {})
  if (m.provider === 'silero_vad') return JSON.stringify(m.silero_vad || {})
  if (m.provider === 'ten_vad') return JSON.stringify(m.ten_vad || {})
  return '{}'
}

function validate(callback) {
  return formRef.value?.validate(callback)
}

function resetFields() {
  formRef.value?.resetFields()
}

defineExpose({ validate, getJsonData, resetFields })
</script>
