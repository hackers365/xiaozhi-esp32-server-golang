<template>
  <div class="mqtt-config">
    <!-- 页面头部 -->
    <div class="page-header">
      <div class="header-content">
        <div class="title-section">
          <el-icon class="title-icon">
            <Connection />
          </el-icon>
          <h1 class="page-title">MQTT配置管理</h1>
        </div>
      </div>
    </div>

    <!-- 配置说明 -->
    <div class="config-description">
      <el-alert
        title="配置说明"
        description="配置MQTT连接参数和认证信息。此配置页面是主程序以mqtt client角色连接mqtt server的配置信息，可以是程序自带的mqtt server，也可以配置连接外部emqx"
        type="info"
        :closable="false"
        show-icon
      />
    </div>

    <!-- 表单容器 -->
    <div class="form-container">
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        class="config-form"
        v-loading="loading"
      >
        <!-- 基础配置卡片 -->
        <el-card class="config-card basic-config" shadow="never">
          <template #header>
            <div class="card-header">
              <el-icon class="card-icon">
                <Setting />
              </el-icon>
              <span class="card-title">基础配置</span>
            </div>
          </template>
          
          <div class="form-grid">
            <el-form-item label="启用MQTT" prop="enable" class="form-item">
              <el-switch v-model="form.enable" />
            </el-form-item>
          </div>
        </el-card>

        <!-- 连接配置卡片 -->
        <el-card class="config-card connection-config" shadow="never">
          <template #header>
            <div class="card-header">
              <el-icon class="card-icon connection-icon">
                <Link />
              </el-icon>
              <span class="card-title">连接配置</span>
            </div>
          </template>
          
          <div class="form-grid">
            <el-form-item label="配置名称" prop="name" class="form-item">
              <el-input v-model="form.name" placeholder="请输入配置名称" />
            </el-form-item>
            
            <el-form-item label="连接类型" prop="type" class="form-item">
              <el-select v-model="form.type" placeholder="请选择连接类型" style="width: 100%">
                <el-option label="Embed（内嵌）" value="embed" />
                <el-option label="TCP" value="tcp" />
                <el-option label="WebSocket (WS)" value="ws" />
                <el-option label="WebSocket Secure (WSS)" value="wss" />
                <el-option label="SSL/TLS" value="ssl" />
              </el-select>
            </el-form-item>

            <el-form-item v-if="!isEmbedType" label="Broker地址" prop="broker" class="form-item">
              <el-input
                v-model="form.broker"
                placeholder="请输入MQTT Broker地址"
              />
            </el-form-item>
            
            <el-form-item v-if="!isEmbedType" label="端口" prop="port" class="form-item">
              <el-input-number
                v-model="form.port"
                :min="1"
                :max="65535"
                placeholder="请输入端口号"
                style="width: 100%"
              />
            </el-form-item>
            
            <el-form-item v-if="!isEmbedType" label="客户端ID" prop="client_id" class="form-item">
              <el-input
                v-model="form.client_id"
                placeholder="请输入客户端ID"
              />
            </el-form-item>
          </div>
        </el-card>

        <!-- 认证配置卡片 -->
        <el-card class="config-card auth-config" shadow="never">
          <template #header>
            <div class="card-header">
              <el-icon class="card-icon auth-icon">
                <User />
              </el-icon>
              <span class="card-title">认证配置</span>
              <el-tooltip content="连接mqtt server的用户名密码，需要具有任意订阅权限" placement="top">
                <el-icon class="help-icon"><QuestionFilled /></el-icon>
              </el-tooltip>
            </div>
          </template>
          
          <div class="form-grid">
            <el-form-item label="用户名" prop="username" class="form-item">
              <el-input v-model="form.username" placeholder="请输入用户名" />
            </el-form-item>
            
            <el-form-item label="密码" prop="password" class="form-item">
              <el-input v-model="form.password" type="password" placeholder="请输入密码" show-password />
            </el-form-item>
          </div>
        </el-card>

        <!-- 操作按钮 -->
        <div class="action-section">
          <el-button type="primary" @click="handleSave" :loading="saving" class="save-button">
            保存配置
          </el-button>
        </div>
      </el-form>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Connection, Setting, Link, User, QuestionFilled } from '@element-plus/icons-vue'
import api from '@/utils/api'

const loading = ref(false)
const saving = ref(false)
const configId = ref(null)
const formRef = ref()
const EMBED_DEFAULT_BROKER = '127.0.0.1'
const EMBED_DEFAULT_PORT = 1883

const form = reactive({
  name: 'MQTT配置',
  is_default: true,
  enable: true,
  broker: EMBED_DEFAULT_BROKER,
  type: 'embed',
  port: EMBED_DEFAULT_PORT,
  client_id: '',
  username: '',
  password: ''
})

const normalizeMqttType = (type) => {
  const normalized = String(type || '').trim().toLowerCase()
  if (!normalized) return 'embed'
  if (normalized === 'websocket') return 'ws'
  return normalized
}

const isEmbedType = computed(() => normalizeMqttType(form.type) === 'embed')

const generateConfig = () => {
  const mqttType = normalizeMqttType(form.type)
  return JSON.stringify({
    enable: form.enable,
    broker: form.broker || EMBED_DEFAULT_BROKER,
    type: mqttType,
    port: form.port,
    client_id: form.client_id,
    username: form.username,
    password: form.password
  })
}

const validateBroker = (rule, value, callback) => {
  if (isEmbedType.value) {
    callback()
    return
  }
  if (!String(value || '').trim()) {
    callback(new Error('请输入MQTT Broker地址'))
    return
  }
  callback()
}

const validatePort = (rule, value, callback) => {
  if (isEmbedType.value) {
    callback()
    return
  }
  if (typeof value !== 'number' || Number.isNaN(value)) {
    callback(new Error('请输入端口号'))
    return
  }
  if (value < 1 || value > 65535) {
    callback(new Error('端口号必须在1-65535之间'))
    return
  }
  callback()
}

const validateClientID = (rule, value, callback) => {
  if (isEmbedType.value) {
    callback()
    return
  }
  if (!String(value || '').trim()) {
    callback(new Error('请输入客户端ID'))
    return
  }
  callback()
}

const rules = {
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }],
  broker: [{ validator: validateBroker, trigger: 'blur' }],
  type: [{ required: true, message: '请选择连接类型', trigger: 'change' }],
  port: [{ validator: validatePort, trigger: 'blur' }],
  client_id: [{ validator: validateClientID, trigger: 'blur' }]
}

const loadConfig = async () => {
  loading.value = true
  try {
    console.log('开始加载MQTT配置...')
    const response = await api.get('/admin/mqtt-configs')
    console.log('MQTT配置API响应:', response)
    const configs = response.data.data || []
    console.log('解析的配置列表:', configs)
    
    // 如果有配置，加载第一个配置
    if (configs.length > 0) {
      const config = configs[0]
      console.log('加载配置:', config)
      configId.value = config.id
      form.name = config.name
      form.is_default = config.is_default
      
      try {
        const configData = JSON.parse(config.json_data || '{}')
        console.log('解析的配置数据:', configData)
        const parsedType = normalizeMqttType(configData.type)
        form.enable = configData.enable !== undefined ? configData.enable : true
        form.broker = configData.broker || EMBED_DEFAULT_BROKER
        form.type = parsedType
        form.port = Number(configData.port) || EMBED_DEFAULT_PORT
        form.client_id = configData.client_id || ''
        form.username = configData.username || ''
        form.password = configData.password || ''
      } catch (error) {
        console.error('解析配置失败:', error)
        ElMessage.warning('配置格式错误，已重置为默认值')
      }
    } else {
      console.log('没有找到配置，使用默认值')
    }
  } catch (error) {
    console.error('加载配置失败:', error)
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
        // 生成config_id，格式为"类型_名称"
        const generatedConfigId = `mqtt_${form.name.replace(/[^a-zA-Z0-9]/g, '_').toLowerCase()}`
        
        const configData = {
          name: form.name,
          config_id: generatedConfigId,
          is_default: true,
          json_data: generateConfig()
        }
        
        if (configId.value) {
          // 更新现有配置
          await api.put(`/admin/mqtt-configs/${configId.value}`, configData)
          ElMessage.success('更新成功')
        } else {
          // 创建新配置
          const response = await api.post('/admin/mqtt-configs', configData)
          configId.value = response.data.data.id
          ElMessage.success('保存成功')
        }
      } catch (error) {
        ElMessage.error(error.response?.data?.message || '保存失败')
      } finally {
        saving.value = false
      }
    }
  })
}

watch(() => form.type, (newType) => {
  const normalized = normalizeMqttType(newType)
  if (newType !== normalized) {
    form.type = normalized
    return
  }

  if (normalized === 'embed') {
    if (!form.broker) {
      form.broker = EMBED_DEFAULT_BROKER
    }
    if (!form.port) {
      form.port = EMBED_DEFAULT_PORT
    }
  }

  formRef.value?.clearValidate(['broker', 'port', 'client_id'])
})

onMounted(() => {
  loadConfig()
})
</script>

<style scoped>
.mqtt-config {
  min-height: 100vh;
  background: #f8f9fa;
  padding: 24px;
}

/* 页面头部 */
.page-header {
  margin-bottom: 24px;
}

.header-content {
  max-width: 1200px;
  margin: 0 auto;
}

.title-section {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 8px;
}

.title-icon {
  font-size: 32px;
  color: #409eff;
}

.page-title {
  font-size: 28px;
  font-weight: 600;
  color: #1f2937;
  margin: 0;
  background: linear-gradient(135deg, #409eff 0%, #67c23a 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

/* 配置说明 */
.config-description {
  max-width: 1200px;
  margin: 0 auto 24px;
}

/* 表单容器 */
.form-container {
  max-width: 1200px;
  margin: 0 auto;
}

.config-form {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

/* 配置卡片 */
.config-card {
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
  transition: all 0.3s ease;
  overflow: hidden;
}

.config-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 10px 25px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05);
}

.basic-config {
  border-left: 4px solid #409eff;
}

.connection-config {
  border-left: 4px solid #67c23a;
}

.auth-config {
  border-left: 4px solid #e6a23c;
}

/* 卡片头部 */
.card-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0;
}

.card-icon {
  font-size: 20px;
  color: #409eff;
}

.connection-icon {
  color: #67c23a;
}

.auth-icon {
  color: #e6a23c;
}

.card-title {
  font-size: 18px;
  font-weight: 600;
  color: #1f2937;
}

.help-icon {
  color: #9ca3af;
  cursor: help;
  font-size: 0.875rem;
}

.help-icon:hover {
  color: #6366f1;
}

/* 表单网格 */
.form-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 24px;
  padding: 24px;
}

.form-item {
  margin-bottom: 0;
}

/* Element Plus 组件深度样式 */
:deep(.el-form-item__label) {
  font-weight: 500;
  color: #374151;
  font-size: 14px;
}

:deep(.el-input__wrapper) {
  border-radius: 8px;
  box-shadow: 0 1px 3px 0 rgba(0, 0, 0, 0.1), 0 1px 2px 0 rgba(0, 0, 0, 0.06);
  transition: all 0.2s ease;
}

:deep(.el-input__wrapper:hover) {
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
}

:deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 3px rgba(64, 158, 255, 0.1);
}

:deep(.el-select .el-input__wrapper) {
  border-radius: 8px;
}

:deep(.el-input-number .el-input__wrapper) {
  border-radius: 8px;
}

:deep(.el-switch) {
  --el-switch-on-color: #409eff;
}

:deep(.el-card__header) {
  background: linear-gradient(135deg, #f8fafc 0%, #f1f5f9 100%);
  border-bottom: 1px solid #e2e8f0;
  padding: 20px 24px;
}

:deep(.el-card__body) {
  padding: 0;
}

/* 操作按钮区域 */
.action-section {
  display: flex;
  justify-content: center;
  padding: 32px 0;
}

.save-button {
  padding: 12px 32px;
  font-size: 16px;
  font-weight: 500;
  border-radius: 8px;
  background: linear-gradient(135deg, #409eff 0%, #67c23a 100%);
  border: none;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
  transition: all 0.3s ease;
}

.save-button:hover {
  transform: translateY(-1px);
  box-shadow: 0 10px 25px -3px rgba(0, 0, 0, 0.1), 0 4px 6px -2px rgba(0, 0, 0, 0.05);
}

/* 响应式设计 */
@media (max-width: 768px) {
  .mqtt-config {
    padding: 16px;
  }
  
  .page-title {
    font-size: 24px;
  }
  
  .title-icon {
    font-size: 28px;
  }
  
  .form-grid {
    grid-template-columns: 1fr;
    gap: 16px;
    padding: 16px;
  }
}

@media (max-width: 480px) {
  .title-section {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
  
  .page-title {
    font-size: 20px;
  }
}
</style>
