<template>
  <el-form ref="formRef" :model="model" :rules="rules" label-width="120px">
    <el-form-item label="提供商" prop="provider">
      <el-select v-model="model.provider" placeholder="请选择提供商" style="width: 100%" @change="onProviderChange">
        <el-option label="OpenAI" value="openai" />
        <el-option label="Azure OpenAI" value="azure" />
        <el-option label="Anthropic" value="anthropic" />
        <el-option label="智谱AI" value="zhipu" />
        <el-option label="阿里云" value="aliyun" />
        <el-option label="豆包" value="doubao" />
        <el-option label="SiliconFlow" value="siliconflow" />
        <el-option label="DeepSeek" value="deepseek" />
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
    <el-form-item label="模型类型" prop="type">
      <el-select v-model="model.type" placeholder="请选择模型类型" style="width: 100%">
        <el-option label="OpenAI" value="openai" />
        <el-option label="Ollama" value="ollama" />
      </el-select>
    </el-form-item>
    <el-form-item label="模型名称" prop="model_name">
      <el-input v-model="model.model_name" placeholder="请输入模型名称" />
    </el-form-item>
    <el-form-item label="API密钥" prop="api_key">
      <el-input v-model="model.api_key" type="password" placeholder="请输入API密钥" show-password />
      <div v-if="currentProviderMeta.keyUrl" class="form-link-tip">
        获取密钥：
        <el-link :href="currentProviderMeta.keyUrl" target="_blank" type="primary">
          {{ currentProviderMeta.keyUrl }}
        </el-link>
      </div>
    </el-form-item>
    <el-form-item label="基础URL" prop="base_url">
      <el-input v-model="model.base_url" placeholder="请输入基础URL" style="width: 100%" />
    </el-form-item>
    <el-form-item label="max_tokens" prop="max_tokens">
      <el-input-number v-model="model.max_tokens" :min="1" :max="100000" placeholder="max_tokens" style="width: 100%" />
    </el-form-item>
    <el-form-item label="温度" prop="temperature">
      <el-input-number v-model="model.temperature" :min="0" :max="2" :step="0.1" placeholder="温度" style="width: 100%" />
    </el-form-item>
    <el-form-item label="Top P" prop="top_p">
      <el-input-number v-model="model.top_p" :min="0" :max="1" :step="0.1" placeholder="Top P" style="width: 100%" />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { computed, ref, watch } from 'vue'

const providerMeta = {
  openai: {
    baseUrl: 'https://api.openai.com/v1',
    docUrl: 'https://developers.openai.com/api/docs',
    keyUrl: 'https://platform.openai.com/api-keys'
  },
  azure: {
    baseUrl: 'https://your-resource-name.openai.azure.com',
    docUrl: 'https://learn.microsoft.com/azure/ai-services/openai',
    keyUrl: 'https://portal.azure.com'
  },
  anthropic: {
    baseUrl: 'https://api.anthropic.com',
    docUrl: 'https://docs.anthropic.com/',
    keyUrl: 'https://console.anthropic.com/settings/keys'
  },
  zhipu: {
    baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
    docUrl: 'https://open.bigmodel.cn/dev/api',
    keyUrl: 'https://open.bigmodel.cn/usercenter/proj-mgmt/apikeys'
  },
  aliyun: {
    baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    docUrl: 'https://help.aliyun.com/zh/model-studio/',
    keyUrl: 'https://help.aliyun.com/zh/model-studio/get-api-key'
  },
  doubao: {
    baseUrl: 'https://ark.cn-beijing.volces.com/api/v3',
    docUrl: 'https://www.volcengine.com/docs/82379',
    keyUrl: 'https://console.volcengine.com/ark/region:ark+cn-beijing/apiKey'
  },
  siliconflow: {
    baseUrl: 'https://api.siliconflow.cn/v1',
    docUrl: 'https://docs.siliconflow.cn/',
    keyUrl: 'https://cloud.siliconflow.cn/account/ak'
  },
  deepseek: {
    baseUrl: 'https://api.deepseek.com/v1',
    docUrl: 'https://api-docs.deepseek.com/',
    keyUrl: 'https://platform.deepseek.com/api_keys'
  }
}

const quickUrls = {
  openai: providerMeta.openai.baseUrl,
  azure: providerMeta.azure.baseUrl,
  anthropic: providerMeta.anthropic.baseUrl,
  zhipu: providerMeta.zhipu.baseUrl,
  aliyun: providerMeta.aliyun.baseUrl,
  doubao: providerMeta.doubao.baseUrl,
  siliconflow: providerMeta.siliconflow.baseUrl,
  deepseek: providerMeta.deepseek.baseUrl
}

const props = defineProps({
  model: { type: Object, required: true },
  rules: { type: Object, default: () => ({}) }
})

const formRef = ref()
const currentProviderMeta = computed(() => providerMeta[props.model?.provider] || {})

watch(() => props.model?.provider, (value) => {
  if (value && quickUrls[value] && props.model) {
    props.model.base_url = quickUrls[value]
  }
}, { immediate: true })

function onProviderChange(value) {
  if (value && quickUrls[value] && props.model) {
    props.model.base_url = quickUrls[value]
  }
}

function getJsonData() {
  const m = props.model
  const config = {
    type: m.type,
    model_name: m.model_name,
    api_key: m.api_key,
    base_url: m.base_url,
    max_tokens: m.max_tokens
  }
  if (m.temperature !== undefined && m.temperature !== null) config.temperature = m.temperature
  if (m.top_p !== undefined && m.top_p !== null) config.top_p = m.top_p
  return JSON.stringify(config, null, 2)
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
.form-link-tip {
  margin-top: 8px;
  font-size: 12px;
  color: #606266;
  line-height: 1.4;
  word-break: break-all;
}
</style>
