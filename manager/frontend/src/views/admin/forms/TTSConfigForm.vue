<template>
  <el-form ref="formRef" :model="model" :rules="rules" label-width="120px">
    <el-form-item label="提供商" prop="provider">
      <el-select v-model="model.provider" placeholder="请选择提供商" style="width: 100%">
        <el-option
          v-for="provider in TTS_PROVIDER_OPTIONS"
          :key="provider.value"
          :label="provider.label"
          :value="provider.value"
        >
          <div style="display:flex; align-items:center; justify-content:space-between; gap:8px;">
            <span>{{ provider.label }}</span>
            <el-tag v-if="provider.supports_voice_clone" size="small" type="success" effect="plain">支持复刻</el-tag>
          </div>
        </el-option>
      </el-select>
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
      </el-form-item>
      <el-form-item label="模型" prop="doubao_ws.model">
        <el-select v-model="model.doubao_ws.model" placeholder="请选择模型" style="width: 100%">
          <el-option v-for="option in DOUBAO_MODEL_OPTIONS" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="资源 ID" prop="doubao_ws.resource_id">
        <el-input v-model="model.doubao_ws.resource_id" placeholder="可选；如 TTS-SeedTTS2.xxxxx，优先使用控制台实例 ID" />
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
      <el-form-item label="WebSocket URL" prop="doubao_ws.ws_url" label-width="unset">
        <el-input v-model="model.doubao_ws.ws_url" placeholder="wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream" />
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
      </el-form-item>
      <el-form-item label="地域" prop="qwen_tts.region">
        <el-select v-model="model.qwen_tts.region" placeholder="请选择地域" style="width: 100%">
          <el-option label="北京" value="beijing" />
          <el-option label="新加坡" value="singapore" />
        </el-select>
      </el-form-item>
      <el-form-item label="模型" prop="qwen_tts.model">
        <el-select
          v-model="model.qwen_tts.model"
          placeholder="qwen3-tts-flash"
          filterable
          allow-create
          default-first-option
          style="width: 100%"
        >
          <el-option v-for="option in ALIYUN_TTS_MODEL_OPTIONS" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="音色" prop="qwen_tts.voice">
        <div style="display: flex; gap: 8px; width: 100%;">
          <el-select
            v-model="model.qwen_tts.voice"
            placeholder="请选择音色"
            style="flex: 1;"
            filterable
            allow-create
            default-first-option
            :loading="voiceLoading"
          >
            <el-option v-for="option in voiceOptionsList" :key="option.value" :label="option.label" :value="option.value" />
          </el-select>
          <VoicePreviewButton
            provider="aliyun_qwen"
            :model="model.qwen_tts.model"
            :voice="model.qwen_tts.voice"
            :instruction="model.qwen_tts.voice_prompt"
            :api-key="aliyunPreviewApiKey"
            :api-url="aliyunPreviewApiURL"
            :region="model.qwen_tts.region"
            :config-id="model.config_id"
            is-admin
          />
        </div>
      </el-form-item>
      <el-form-item v-if="supportsAliyunInstruction(model.qwen_tts.model)" label="声音指令 (voice_prompt)" prop="qwen_tts.voice_prompt">
        <el-input v-model="model.qwen_tts.voice_prompt" type="textarea" placeholder="例如：年轻女性声音，语气欢快，发音清晰" :rows="2" />
      </el-form-item>
      <el-form-item label="语种" prop="qwen_tts.language_type">
        <el-select v-model="model.qwen_tts.language_type" placeholder="请选择语种" style="width: 100%">
          <el-option label="自动" value="Auto" />
          <el-option label="中文" value="Chinese" />
          <el-option label="英文" value="English" />
        </el-select>
      </el-form-item>
      <el-form-item label="音频格式" prop="qwen_tts.format">
        <el-select v-model="model.qwen_tts.format" placeholder="请选择音频格式" style="width: 100%">
          <el-option label="Default" value="" />
          <el-option label="PCM" value="pcm" />
          <el-option label="WAV" value="wav" />
          <el-option label="MP3" value="mp3" />
          <el-option label="ogg_opus" value="ogg_opus" />
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

    <template v-if="model.provider === 'xunfei'">
      <XunfeiCommonConfig :model-value="model" prefix="xunfei" default-ws-url="wss://tts-api.xfyun.cn/v2/tts" />
      <el-form-item label="音色" prop="xunfei.voice">
        <el-input v-model="model.xunfei.voice" placeholder="请输入音色，例如 xiaoyan" />
      </el-form-item>
      <el-form-item label="音频编码" prop="xunfei.audio_encoding">
        <el-select v-model="model.xunfei.audio_encoding" placeholder="请选择音频编码" style="width: 100%">
          <el-option label="RAW" value="raw" />
          <el-option label="Opus" value="opus" />
        </el-select>
      </el-form-item>
      <el-form-item label="采样率" prop="xunfei.sample_rate">
        <el-select v-model="model.xunfei.sample_rate" placeholder="请选择采样率" style="width: 100%">
          <el-option label="8000" :value="8000" />
          <el-option label="16000" :value="16000" />
        </el-select>
      </el-form-item>
      <el-form-item label="语速" prop="xunfei.speed">
        <el-input-number v-model="model.xunfei.speed" :min="0" :max="100" style="width: 100%" />
      </el-form-item>
      <el-form-item label="音量" prop="xunfei.volume">
        <el-input-number v-model="model.xunfei.volume" :min="0" :max="100" style="width: 100%" />
      </el-form-item>
      <el-form-item label="音调" prop="xunfei.pitch">
        <el-input-number v-model="model.xunfei.pitch" :min="0" :max="100" style="width: 100%" />
      </el-form-item>
      <el-form-item label="文本编码" prop="xunfei.tte">
        <el-select v-model="model.xunfei.tte" placeholder="请选择文本编码" style="width: 100%">
          <el-option label="UTF8" value="UTF8" />
          <el-option label="UNICODE" value="UNICODE" />
          <el-option label="GB2312" value="GB2312" />
        </el-select>
      </el-form-item>
      <el-form-item label="数字发音" prop="xunfei.reg">
        <el-input-number v-model="model.xunfei.reg" :min="0" :max="2" style="width: 100%" />
      </el-form-item>
      <el-form-item label="数字读法" prop="xunfei.rdn">
        <el-input-number v-model="model.xunfei.rdn" :min="0" :max="2" style="width: 100%" />
      </el-form-item>
    </template>

    <template v-if="model.provider === 'xunfei_super_tts'">
      <XunfeiCommonConfig :model-value="model" prefix="xunfei_super_tts" default-ws-url="wss://cbm01.cn-huabei-1.xf-yun.com/v1/private/mcd9m97e6" />
      <el-form-item label="音色" prop="xunfei_super_tts.voice">
        <el-select
          v-model="model.xunfei_super_tts.voice"
          placeholder="请选择或输入音色"
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
      <el-form-item label="音频编码" prop="xunfei_super_tts.audio_encoding">
        <el-select v-model="model.xunfei_super_tts.audio_encoding" placeholder="请选择音频编码" style="width: 100%">
          <el-option label="RAW" value="raw" />
          <el-option label="Opus" value="opus" />
        </el-select>
      </el-form-item>
      <el-form-item label="采样率" prop="xunfei_super_tts.sample_rate">
        <el-select v-model="model.xunfei_super_tts.sample_rate" placeholder="请选择采样率" style="width: 100%">
          <el-option label="8000" :value="8000" />
          <el-option label="16000" :value="16000" />
          <el-option label="24000" :value="24000" />
        </el-select>
      </el-form-item>
      <el-form-item label="语速" prop="xunfei_super_tts.speed">
        <el-input-number v-model="model.xunfei_super_tts.speed" :min="0" :max="100" style="width: 100%" />
      </el-form-item>
      <el-form-item label="音量" prop="xunfei_super_tts.volume">
        <el-input-number v-model="model.xunfei_super_tts.volume" :min="0" :max="100" style="width: 100%" />
      </el-form-item>
      <el-form-item label="音调" prop="xunfei_super_tts.pitch">
        <el-input-number v-model="model.xunfei_super_tts.pitch" :min="0" :max="100" style="width: 100%" />
      </el-form-item>
      <el-form-item label="背景音" prop="xunfei_super_tts.bgs">
        <el-input-number v-model="model.xunfei_super_tts.bgs" :min="0" :max="10" style="width: 100%" />
      </el-form-item>
      <el-form-item label="数字发音" prop="xunfei_super_tts.reg">
        <el-input-number v-model="model.xunfei_super_tts.reg" :min="0" :max="2" style="width: 100%" />
      </el-form-item>
      <el-form-item label="数字读法" prop="xunfei_super_tts.rdn">
        <el-input-number v-model="model.xunfei_super_tts.rdn" :min="0" :max="2" style="width: 100%" />
      </el-form-item>
      <el-form-item label="韵律增强" prop="xunfei_super_tts.rhy">
        <el-input-number v-model="model.xunfei_super_tts.rhy" :min="0" :max="1" style="width: 100%" />
      </el-form-item>
      <el-form-item label="口语级别" prop="xunfei_super_tts.oral_level">
        <el-select v-model="model.xunfei_super_tts.oral_level" placeholder="请选择口语级别" style="width: 100%">
          <el-option label="low" value="low" />
          <el-option label="mid" value="mid" />
          <el-option label="high" value="high" />
        </el-select>
      </el-form-item>
      <el-form-item label="Spark Assist" prop="xunfei_super_tts.spark_assist">
        <el-input-number v-model="model.xunfei_super_tts.spark_assist" :min="0" :max="1" style="width: 100%" />
      </el-form-item>
      <el-form-item label="停顿切分" prop="xunfei_super_tts.stop_split">
        <el-input-number v-model="model.xunfei_super_tts.stop_split" :min="0" :max="1" style="width: 100%" />
      </el-form-item>
      <el-form-item label="保留口语化" prop="xunfei_super_tts.remain">
        <el-input-number v-model="model.xunfei_super_tts.remain" :min="0" :max="1" style="width: 100%" />
      </el-form-item>
      <el-form-item label-width="0">
        <div class="indextts-help">
          <div class="indextts-help-subtitle">x4 系列支持口语化参数，x5/x6 建议主要调整音色、语速和采样率。</div>
        </div>
      </el-form-item>
    </template>



    <template v-if="model.provider === 'indextts_vllm'">
      <el-form-item label-width="0" class="indextts-help-item">
        <div class="indextts-help">
          <div class="indextts-help-head">
            <div class="indextts-help-title">IndexTTS vLLM 接口指南</div>
            <div class="indextts-help-subtitle">确保服务端支持 /audio/speech、/audio/voices，复刻需 /audio/clone</div>
          </div>
          <div class="indextts-help-links">
            <el-link :href="indexTTSDocURL" target="_blank" type="primary" :underline="false">项目接入文档（GitHub）</el-link>
            <span class="indextts-help-divider">|</span>
            <el-link :href="indexTTSReferenceURL" target="_blank" type="info" :underline="false">参考 api_server.py</el-link>
          </div>
          <div class="indextts-help-tags">
            <el-tag size="small" effect="plain" type="success">/audio/speech</el-tag>
            <el-tag size="small" effect="plain" type="warning">/audio/voices</el-tag>
            <el-tag size="small" effect="plain" type="info">/audio/clone</el-tag>
          </div>
        </div>
      </el-form-item>
      <el-form-item label="API URL" prop="indextts_vllm.api_url">
        <el-input v-model="model.indextts_vllm.api_url" placeholder="请输入IndexTTS服务地址（例如：http://127.0.0.1:7860）" />
      </el-form-item>
      <el-form-item label="API Key" prop="indextts_vllm.api_key">
        <el-input v-model="model.indextts_vllm.api_key" placeholder="可选，按需填写" type="password" show-password />
      </el-form-item>
      <el-form-item label="模型" prop="indextts_vllm.model">
        <el-input v-model="model.indextts_vllm.model" placeholder="indextts-vllm" />
      </el-form-item>
      <el-form-item label="音色" prop="indextts_vllm.voice">
        <el-select
          v-model="model.indextts_vllm.voice"
          placeholder="请选择音色"
          style="width: 100%"
          filterable
          :loading="voiceLoading"
          :disabled="voiceLoading"
          allow-create
          default-first-option
          @visible-change="handleIndexTTSVoiceVisibleChange"
        >
          <el-option v-for="option in voiceOptionsList" :key="option.value" :label="option.label" :value="option.value" />
        </el-select>
      </el-form-item>
      <el-form-item label="帧时长" prop="indextts_vllm.frame_duration">
        <el-input-number v-model="model.indextts_vllm.frame_duration" :min="1" :max="1000" style="width: 100%" />
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
import XunfeiCommonConfig from './XunfeiCommonConfig.vue'
import VoicePreviewButton from '@/components/common/VoicePreviewButton.vue'
import { ALIYUN_TTS_MODEL_OPTIONS, resolveAliyunAPIURL, serializeAliyunTtsConfig, supportsAliyunInstruction } from './aliyunTtsOptions'

const DOUBAO_MODEL_OPTIONS = [
  { label: '豆包语音合成 1.1', value: 'seed-tts-1.1' },
  { label: '豆包语音合成 2.0 Standard', value: 'seed-tts-2.0-standard' },
  { label: '豆包语音合成 2.0 Expressive', value: 'seed-tts-2.0-expressive' },
  { label: '豆包声音复刻 1.0', value: 'seed-icl-1.0' },
  { label: '豆包声音复刻 2.0 Standard', value: 'seed-icl-2.0-standard' },
  { label: '豆包声音复刻 2.0 Expressive', value: 'seed-icl-2.0-expressive' }
]

const props = defineProps({
  model: { type: Object, required: true },
  rules: { type: Object, default: () => ({}) },
  voiceOptions: { type: Array, default: () => [] },
  voiceLoading: { type: Boolean, default: false }
})
const emit = defineEmits(['request-voice-options'])

const formRef = ref()
// 保证音色选项始终为数组且响应式，供下拉使用
const voiceOptionsList = computed(() => Array.isArray(props.voiceOptions) ? props.voiceOptions : [])
const aliyunPreviewApiKey = computed(() => {
  return String(props.model.qwen_tts?.api_key || '').trim()
})
const aliyunPreviewApiURL = computed(() => resolveAliyunAPIURL(props.model.qwen_tts?.api_url, props.model.qwen_tts?.region))
const indexTTSDocURL = 'https://github.com/hackers365/xiaozhi-esp32-server-golang/blob/main/doc/indextts_vllm_api.md'
const indexTTSReferenceURL = 'https://github.com/hackers365/index-tts-vllm/blob/master/api_server.py'

function handleIndexTTSVoiceVisibleChange(visible) {
  if (visible) {
    emit('request-voice-options', 'indextts_vllm')
  }
}

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
      config.model = form.doubao_ws?.model || 'seed-tts-2.0-standard'
      config.resource_id = form.doubao_ws?.resource_id
      config.voice = form.doubao_ws?.voice
      config.ws_url = form.doubao_ws?.ws_url || 'wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream'
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
      return JSON.stringify(serializeAliyunTtsConfig(form.qwen_tts))
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
    case 'xunfei':
      config.provider = 'xunfei'
      config.app_id = form.xunfei?.app_id
      config.api_key = form.xunfei?.api_key
      config.api_secret = form.xunfei?.api_secret
      config.ws_url = form.xunfei?.ws_url
      config.voice = form.xunfei?.voice
      config.audio_encoding = form.xunfei?.audio_encoding || 'raw'
      config.sample_rate = form.xunfei?.sample_rate || 16000
      config.speed = form.xunfei?.speed ?? 50
      config.volume = form.xunfei?.volume ?? 50
      config.pitch = form.xunfei?.pitch ?? 50
      config.tte = form.xunfei?.tte || 'UTF8'
      config.reg = form.xunfei?.reg ?? 0
      config.rdn = form.xunfei?.rdn ?? 0
      config.frame_duration = form.xunfei?.frame_duration || 60
      config.connect_timeout = form.xunfei?.connect_timeout || 10
      config.read_timeout = form.xunfei?.read_timeout || 30
      break
    case 'xunfei_super_tts':
      config.provider = 'xunfei_super_tts'
      config.double_stream = true
      config.app_id = form.xunfei_super_tts?.app_id
      config.api_key = form.xunfei_super_tts?.api_key
      config.api_secret = form.xunfei_super_tts?.api_secret
      config.ws_url = form.xunfei_super_tts?.ws_url
      config.voice = form.xunfei_super_tts?.voice
      config.audio_encoding = form.xunfei_super_tts?.audio_encoding || 'raw'
      config.sample_rate = form.xunfei_super_tts?.sample_rate || 24000
      config.speed = form.xunfei_super_tts?.speed ?? 50
      config.volume = form.xunfei_super_tts?.volume ?? 50
      config.pitch = form.xunfei_super_tts?.pitch ?? 50
      config.bgs = form.xunfei_super_tts?.bgs ?? 0
      config.reg = form.xunfei_super_tts?.reg ?? 0
      config.rdn = form.xunfei_super_tts?.rdn ?? 0
      config.rhy = form.xunfei_super_tts?.rhy ?? 0
      config.oral_level = form.xunfei_super_tts?.oral_level || 'mid'
      config.spark_assist = form.xunfei_super_tts?.spark_assist ?? 1
      config.stop_split = form.xunfei_super_tts?.stop_split ?? 0
      config.remain = form.xunfei_super_tts?.remain ?? 0
      config.frame_duration = form.xunfei_super_tts?.frame_duration || 60
      config.connect_timeout = form.xunfei_super_tts?.connect_timeout || 10
      config.read_timeout = form.xunfei_super_tts?.read_timeout || 30
      break
    case 'indextts_vllm':
      config.provider = 'indextts_vllm'
      config.api_url = form.indextts_vllm?.api_url
      config.api_key = form.indextts_vllm?.api_key
      config.model = form.indextts_vllm?.model || 'indextts-vllm'
      config.voice = form.indextts_vllm?.voice
      config.response_format = 'wav'
      config.stream = false
      config.frame_duration = form.indextts_vllm?.frame_duration || 60
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
.indextts-help-item :deep(.el-form-item__content) {
  margin-left: 0 !important;
}

.indextts-help {
  width: 100%;
  border: 1px solid rgba(255, 255, 255, 0.92);
  border-radius: 18px;
  padding: 12px 14px;
  background: rgba(248, 250, 252, 0.9);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.7);
}

.indextts-help-head {
  margin-bottom: 8px;
}

.indextts-help-title {
  font-size: 14px;
  font-weight: 700;
  color: var(--apple-text);
}

.indextts-help-subtitle {
  margin-top: 4px;
  font-size: 12px;
  color: var(--apple-text-secondary);
}

.indextts-help-links {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.indextts-help-divider {
  color: var(--apple-text-tertiary);
  font-size: 12px;
}

.indextts-help-tags {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.form-item-tip {
  margin-left: 8px;
  font-size: 12px;
  color: var(--apple-text-secondary);
}
</style>
