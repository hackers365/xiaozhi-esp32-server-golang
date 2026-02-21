<template>
  <div class="config-page">
    <div class="page-header">
      <div class="header-left">
        <h2>声纹识别配置</h2>
      </div>
    </div>

    <el-card v-loading="loading" class="config-card">
      <el-alert
        title="提示"
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 20px;"
      >
        <template #default>
          如果是docker-compose环境部署会读取环境变量中的api地址，无需进行配置
        </template>
      </el-alert>
      
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-width="120px"
      >
        <el-form-item label="服务模式" prop="mode">
          <el-select v-model="form.mode" style="width: 100%">
            <el-option label="HTTP（外部服务）" value="http" />
            <el-option label="Embed（内置服务）" value="embed" />
          </el-select>
          <div class="form-tip">
            <el-icon><InfoFilled /></el-icon>
            {{ modeTipText }}
          </div>
        </el-form-item>

        <el-form-item v-if="isHttpMode" label="服务地址" prop="base_url">
          <el-input 
            v-model="form.base_url" 
            placeholder="请输入HTTP服务地址，如：http://192.168.208.214:8080"
            style="width: 100%"
          />
          <div class="form-tip">
            <el-icon><InfoFilled /></el-icon>
            填写声纹服务地址即可，例如：http://127.0.0.1:9000
          </div>
        </el-form-item>
        
        <el-form-item label="识别阈值" prop="threshold">
          <el-input-number 
            v-model="form.threshold" 
            :min="0" 
            :max="1" 
            :step="0.1" 
            :precision="2"
            placeholder="0.4"
            style="width: 100%"
          />
          <div class="form-tip">
            <el-icon><InfoFilled /></el-icon>
            声纹识别阈值，范围 0.0-1.0，默认 0.4。值越大识别越严格
          </div>
        </el-form-item>
        
        <el-form-item label="启用状态">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      
      <div class="form-actions">
        <el-button type="primary" @click="handleSave" :loading="saving">
          保存配置
        </el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { InfoFilled } from '@element-plus/icons-vue'
import api from '../../utils/api'

const loading = ref(false)
const saving = ref(false)
const formRef = ref()
const currentConfig = ref(null)

const form = reactive({
  mode: 'http',
  base_url: 'http://192.168.208.214:8080',
  threshold: 0.4,
  enabled: true
})

const normalizeMode = (value) => {
  const mode = String(value || '').trim().toLowerCase()
  return mode === 'embed' ? 'embed' : 'http'
}

const isHttpMode = computed(() => form.mode === 'http')
const modeTipText = computed(() => (
  isHttpMode.value
    ? 'HTTP 模式：通过服务地址连接声纹服务。'
    : 'Embed 模式：使用程序内置声纹能力。'
))

const rules = {
  mode: [
    { required: true, message: '请选择服务模式', trigger: 'change' }
  ],
  base_url: [
    {
      validator: (_rule, value, callback) => {
        if (!isHttpMode.value) {
          callback()
          return
        }
        const normalized = String(value || '').trim()
        if (!normalized) {
          callback(new Error('请输入服务地址'))
          return
        }
        if (!/^https?:\/\/.+/.test(normalized)) {
          callback(new Error('请输入有效的HTTP地址，如：http://192.168.208.214:8080'))
          return
        }
        callback()
      },
      trigger: ['blur', 'change']
    }
  ],
  threshold: [
    { required: true, message: '请输入识别阈值', trigger: 'blur' },
    { 
      type: 'number', 
      min: 0, 
      max: 1, 
      message: '阈值必须在 0.0 到 1.0 之间', 
      trigger: 'blur' 
    }
  ]
}

const loadConfig = async () => {
  loading.value = true
  try {
    const response = await api.get('/admin/speaker-configs')
    const configs = response.data.data || []
    
    if (configs.length > 0) {
      // 如果有配置，使用第一个（应该只有一个）
      currentConfig.value = configs[0]
      const configObj = JSON.parse(configs[0].json_data || '{}')

      // 解析 service 配置
      let mode = 'http'
      if (configObj.service && typeof configObj.service === 'object') {
        mode = normalizeMode(configObj.service.mode)
      }
      form.mode = mode

      // 解析服务地址
      if (configObj.service && typeof configObj.service === 'object' && configObj.service.base_url) {
        form.base_url = configObj.service.base_url
      }

      // 读取阈值配置
      if (configObj.service && configObj.service.threshold !== undefined) {
        form.threshold = configObj.service.threshold
      } else if (configObj.threshold !== undefined) {
        // 兼容旧格式
        form.threshold = configObj.threshold
      } else {
        // 默认值
        form.threshold = 0.4
      }

      // 开关对应 json_data.enable（业务启用），不使用接口返回的 enabled 列
      form.enabled = typeof configObj.enable === 'boolean' ? configObj.enable : true
    }
  } catch (error) {
    ElMessage.error('加载配置失败')
  } finally {
    loading.value = false
  }
}

const handleSave = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (valid) {
      saving.value = true
      try {
        // 构建配置数据：开关写入 json_data.enable，对外输出以该字段为准
        const serviceConfig = {
          mode: form.mode,
          base_url: String(form.base_url || ''),
          threshold: Number(form.threshold)
        }

        const configData = {
          service: serviceConfig,
          enable: form.enabled
        }
        
        const saveData = {
          name: '声纹识别配置',
          config_id: 'asr_server',
          provider: 'asr_server',
          is_default: true,
          enabled: form.enabled,
          json_data: JSON.stringify(configData)
        }
        
        if (currentConfig.value) {
          // 更新现有配置
          await api.put(`/admin/speaker-configs/${currentConfig.value.id}`, saveData)
          ElMessage.success('配置更新成功')
        } else {
          // 创建新配置
          await api.post('/admin/speaker-configs', saveData)
          ElMessage.success('配置创建成功')
        }
        
        // 重新加载配置
        await loadConfig()
      } catch (error) {
        ElMessage.error('保存失败: ' + (error.response?.data?.message || error.message))
      } finally {
        saving.value = false
      }
    }
  })
}

watch(() => form.mode, () => {
  if (!isHttpMode.value && formRef.value) {
    formRef.value.clearValidate(['base_url'])
  }
})

onMounted(() => {
  loadConfig()
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

.config-card {
  max-width: 800px;
}

.form-tip {
  margin-top: 8px;
  font-size: 12px;
  color: #909399;
  display: flex;
  align-items: center;
  gap: 4px;
}

.form-tip .el-icon {
  font-size: 14px;
  color: #409eff;
}

.form-actions {
  margin-top: 20px;
  padding-top: 20px;
  border-top: 1px solid #ebeef5;
}
</style>
