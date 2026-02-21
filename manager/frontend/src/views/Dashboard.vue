<template>
  <div class="dashboard">
    <el-row :gutter="20">
      <el-col :span="6" v-if="authStore.isAdmin">
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon">
              <el-icon size="40" color="#409EFF"><User /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-number">{{ stats.totalUsers }}</div>
              <div class="stat-label">总用户数</div>
            </div>
          </div>
        </el-card>
      </el-col>
      
      <el-col :span="authStore.isAdmin ? 6 : 8">
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon">
              <el-icon size="40" color="#67C23A"><Monitor /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-number">{{ stats.totalDevices }}</div>
              <div class="stat-label">{{ authStore.isAdmin ? '设备总数' : '我的设备' }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
      
      <el-col :span="authStore.isAdmin ? 6 : 8">
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon">
              <el-icon size="40" color="#E6A23C"><Cpu /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-number">{{ stats.totalAgents }}</div>
              <div class="stat-label">{{ authStore.isAdmin ? '智能体数量' : '我的智能体' }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
      
      <el-col :span="authStore.isAdmin ? 6 : 8">
        <el-card class="stat-card">
          <div class="stat-content">
            <div class="stat-icon">
              <el-icon size="40" color="#F56C6C"><Connection /></el-icon>
            </div>
            <div class="stat-info">
              <div class="stat-number">{{ stats.onlineDevices }}</div>
              <div class="stat-label">在线设备</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
    
    <!-- 服务地址（紧凑） + OTA 测试 -->
    <el-card class="address-card address-card-compact" v-if="authStore.isAdmin" style="margin: 20px 0;">
      <template #header>
        <div class="config-header address-card-header">
          <span>
            <el-icon size="16" color="#409EFF"><Link /></el-icon>
            服务地址
          </span>
          <el-button type="warning" size="small" :loading="otaTestLoading" @click="runOtaTest">
            OTA 测试
          </el-button>
        </div>
      </template>
      <div v-loading="addressLoading" class="address-compact">
        <template v-if="!addressLoading && (serviceAddress.otaUrl || serviceAddress.wsUrl)">
          <div class="address-line">
            <span class="address-tag">OTA</span>
            <span class="address-text" :title="serviceAddress.otaUrl">{{ serviceAddress.otaUrl || '—' }}</span>
            <el-button v-if="serviceAddress.otaUrl" link type="primary" size="small" :icon="CopyDocument" @click="copyAddress(serviceAddress.otaUrl)" />
          </div>
          <div class="address-line">
            <span class="address-tag">WS</span>
            <span class="address-text" :title="serviceAddress.wsUrl">{{ serviceAddress.wsUrl || '—' }}</span>
            <el-button v-if="serviceAddress.wsUrl" link type="primary" size="small" :icon="CopyDocument" @click="copyAddress(serviceAddress.wsUrl)" />
          </div>
          <template v-if="serviceAddress.mqttEndpoint">
            <div class="address-line">
              <span class="address-tag">MQTT</span>
              <span class="address-text" :title="serviceAddress.mqttEndpoint">{{ serviceAddress.mqttEndpoint }}</span>
              <el-button link type="primary" size="small" :icon="CopyDocument" @click="copyAddress(serviceAddress.mqttEndpoint)" />
            </div>
          </template>
          <template v-if="serviceAddress.udpAddress">
            <div class="address-line">
              <span class="address-tag">UDP</span>
              <span class="address-text" :title="serviceAddress.udpAddress">{{ serviceAddress.udpAddress }}</span>
              <el-button link type="primary" size="small" :icon="CopyDocument" @click="copyAddress(serviceAddress.udpAddress)" />
            </div>
          </template>
          <div v-if="otaTestResult !== null" class="ota-test-block">
            <span class="address-tag">OTA 接口返回</span>
            <pre class="ota-test-pre">{{ otaTestResult }}</pre>
          </div>
        </template>
        <div v-else-if="!addressLoading" class="address-empty">暂无 OTA 配置</div>
      </div>
    </el-card>

    <!-- 配置管理卡片 - 放在统计数据和系统信息之间 -->
    <el-card class="config-card" v-if="authStore.isAdmin" style="margin: 20px 0;">
      <template #header>
        <div class="config-header">
          <el-icon size="18" color="#409EFF"><Setting /></el-icon>
          <span>配置管理</span>
        </div>
      </template>
      <div class="config-actions">
        <el-button
          type="primary"
          @click="$router.push('/admin/config-wizard')"
          class="config-btn"
        >
          <el-icon><Guide /></el-icon>
          配置向导
        </el-button>
        <el-button 
          type="primary" 
          @click="exportConfig"
          class="config-btn"
        >
          <el-icon><Download /></el-icon>
          导出配置
        </el-button>
        <el-button 
          type="success" 
          @click="importConfig"
          class="config-btn"
        >
           <el-icon><Upload /></el-icon>
           导入配置
           <div class="btn-tip">支持YAML/JSON</div>
         </el-button>
      </div>
      <input
        ref="fileInput"
        type="file"
        accept=".yaml,.yml,.json"
        style="display: none"
        @change="handleFileChange"
      />
    </el-card>
    
    <el-row :gutter="20" style="margin-top: 20px;">
      <el-col :span="12">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>系统信息</span>
            </div>
          </template>
          <div class="system-info">
            <div class="info-item">
              <span class="info-label">管理后台版本：</span>
              <span class="info-value">{{ systemInfo.manager.version }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">管理后台运行：</span>
              <span class="info-value">{{ systemInfo.manager.uptime }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">主服务版本：</span>
              <span class="info-value">{{ systemInfo.server.version }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">主服务运行：</span>
              <span class="info-value">{{ systemInfo.server.uptime }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">当前用户：</span>
              <span class="info-value">{{ authStore.user?.username }}</span>
            </div>
            <div class="info-item">
              <span class="info-label">用户角色：</span>
              <el-tag :type="authStore.isAdmin ? 'danger' : 'primary'">
                {{ authStore.isAdmin ? '管理员' : '普通用户' }}
              </el-tag>
            </div>
          </div>
        </el-card>
      </el-col>
      
      <el-col :span="12">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>快速操作</span>
            </div>
          </template>
          <div class="quick-actions">
            <template v-if="authStore.isAdmin">
              <el-button type="primary" @click="$router.push('/admin/users')">
                <el-icon><User /></el-icon>
                用户管理
              </el-button>
              <el-button type="success" @click="$router.push('/admin/llm-config')">
                <el-icon><Setting /></el-icon>
                LLM配置
              </el-button>
              <el-button type="warning" @click="$router.push('/admin/vad-config')">
                <el-icon><Setting /></el-icon>
                VAD配置
              </el-button>
            </template>
            <template v-else>
              <el-button type="primary" @click="$router.push('/agents')">
                <el-icon><Monitor /></el-icon>
                智能体管理
              </el-button>
              <el-text type="info">
                普通用户主要功能在智能体管理页面
              </el-text>
            </template>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import api from '@/utils/api'
import { ElMessage } from 'element-plus'
import {
  User,
  Monitor,
  Connection,
  Setting,
  Plus,
  Download,
  Upload,
  Cpu,
  Guide,
  Link,
  CopyDocument
} from '@element-plus/icons-vue'

const authStore = useAuthStore()

// 服务地址（OTA、WS、MQTT、UDP）
const addressLoading = ref(false)
const serviceAddress = ref({
  otaUrl: '',
  wsUrl: '',
  mqttEndpoint: '',
  udpAddress: ''
})

async function loadServiceAddress() {
  addressLoading.value = true
  serviceAddress.value = { otaUrl: '', wsUrl: '', mqttEndpoint: '', udpAddress: '' }
  try {
    const [otaRes, udpRes] = await Promise.all([
      api.get('/admin/ota-configs'),
      api.get('/admin/udp-configs')
    ])
    const otaList = otaRes.data?.data || []
    const config = otaList.find(c => c.is_default) || otaList[0]
    if (config?.json_data) {
      const data = JSON.parse(config.json_data || '{}')
      console.log('[Dashboard] OTA配置数据:', data)

      // 选择环境配置：优先 external，如果为空则使用 test
      let envData = data.external || {}
      const hasExternalWs = envData.websocket?.url
      const hasExternalOta = envData.ota_url
      if (!hasExternalWs && !hasExternalOta) {
        envData = data.test || {}
      }

      // OTA URL：优先使用配置中的 ota_url，没有则从 websocket.url 解析
      let otaUrl = envData.ota_url || ''
      if (!otaUrl) {
        const wsUrl = envData.websocket?.url || ''
        if (wsUrl) {
          const m = wsUrl.match(/^(wss?):\/\/([^:/]+)(?::(\d+))?/)
          if (m) {
            const proto = m[1] === 'wss' ? 'https' : 'http'
            const port = m[3] || (m[1] === 'wss' ? '443' : '80')
            otaUrl = `${proto}://${m[2]}:${port}/xiaozhi/ota/`
          }
        }
      }
      serviceAddress.value.otaUrl = otaUrl

      // WebSocket URL
      serviceAddress.value.wsUrl = envData.websocket?.url || ''

      // MQTT endpoint
      const mqttEnabled = envData.mqtt?.enable
      const endpoint = envData.mqtt?.endpoint || ''
      if (mqttEnabled && endpoint) {
        serviceAddress.value.mqttEndpoint = endpoint
      }
    }
    // UDP address
    const udpList = udpRes.data?.data || []
    const udpConfig = udpList.find(c => c.is_default) || udpList[0]
    if (udpConfig?.json_data) {
      const udpData = JSON.parse(udpConfig.json_data || '{}')
      const host = udpData.external_host || ''
      const port = udpData.external_port
      if (host && port != null) {
        serviceAddress.value.udpAddress = `${host}:${port}`
      }
    }
  } catch (err) {
    console.error('加载服务地址失败:', err)
  } finally {
    addressLoading.value = false
  }
}

function copyAddress(text) {
  if (!text) return
  navigator.clipboard.writeText(text).then(() => {
    ElMessage.success('已复制到剪贴板')
  }).catch(() => {
    ElMessage.error('复制失败')
  })
}

// 首页 OTA 测试（展示 OTA 接口返回内容）
const otaTestLoading = ref(false)
const otaTestResult = ref(null)

function formatOtaResponseDisplay(str) {
  if (str == null || str === '') return ''
  const s = String(str).trim()
  if (!s) return ''
  try {
    return JSON.stringify(JSON.parse(s), null, 2)
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

const stats = ref({
  totalUsers: 0,
  totalDevices: 0,
  totalAgents: 0,
  onlineDevices: 0
})

const systemInfo = ref({
  manager: {
    version: 'unknown',
    uptime: '0天 0小时 0分钟'
  },
  server: {
    version: '未连接',
    uptime: '未连接'
  }
})
const fileInput = ref(null)

onMounted(async () => {
  await loadStats()
  if (authStore.isAdmin) {
    loadServiceAddress()
  }
})

// 加载统计数据
const loadStats = async () => {
  try {
    const response = await api.get('/dashboard/stats')
    const manager = response.data?.manager || {}
    const server = response.data?.server || null
    stats.value = {
      totalUsers: response.data.totalUsers || 0,
      totalDevices: response.data.totalDevices || 0,
      totalAgents: response.data.totalAgents || 0,
      onlineDevices: response.data.onlineDevices || 0
    }
    systemInfo.value = {
      manager: {
        version: manager.version || 'unknown',
        uptime: manager.uptime || '0天 0小时 0分钟'
      },
      server: {
        version: server?.version || '未连接',
        uptime: server?.uptime || '未连接'
      }
    }
  } catch (error) {
    console.error('加载统计数据失败:', error)
    // 使用默认值
    stats.value = {
      totalUsers: 0,
      totalDevices: 0,
      totalAgents: 0,
      onlineDevices: 0
    }
    systemInfo.value = {
      manager: {
        version: 'unknown',
        uptime: '0天 0小时 0分钟'
      },
      server: {
        version: '未连接',
        uptime: '未连接'
      }
    }
  }
}

// 导出配置
const exportConfig = async () => {
  try {
    const response = await fetch('/api/admin/configs/export', {
      method: 'GET',
      headers: {
        'Authorization': `Bearer ${authStore.token}`
      }
    })
    
    if (response.ok) {
      const blob = await response.blob()
      const url = window.URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = 'config.yaml'
      document.body.appendChild(a)
      a.click()
      window.URL.revokeObjectURL(url)
      document.body.removeChild(a)
      
      ElMessage.success('配置导出成功')
    } else {
      ElMessage.error('配置导出失败')
    }
  } catch (error) {
    console.error('导出配置失败:', error)
    ElMessage.error('配置导出失败')
  }
}

// 导入配置
const importConfig = () => {
  fileInput.value.click()
}

// 处理文件选择
const handleFileChange = async (event) => {
  const file = event.target.files[0]
  if (!file) return
  
  // 检查文件格式
  const validExtensions = ['.yaml', '.yml', '.json']
  const fileExtension = file.name.toLowerCase().substring(file.name.lastIndexOf('.'))
  
  if (!validExtensions.includes(fileExtension)) {
    ElMessage.error('请选择YAML或JSON格式的文件')
    return
  }
  
  const formData = new FormData()
  formData.append('file', file)
  
  try {
    const response = await fetch('/api/admin/configs/import', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${authStore.token}`
      },
      body: formData
    })
    
    if (response.ok) {
      ElMessage.success('配置导入成功')
    } else {
      const error = await response.json()
      ElMessage.error(error.error || '配置导入失败')
    }
  } catch (error) {
    console.error('导入配置失败:', error)
    ElMessage.error('配置导入失败')
  }
  
  // 清空文件输入
  event.target.value = ''
}
</script>

<style scoped>
.dashboard {
  padding: 0;
}

.config-card {
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  box-shadow: 0 2px 12px 0 rgba(0, 0, 0, 0.1);
}

.config-header {
  display: flex;
  align-items: center;
  font-size: 16px;
  font-weight: 600;
  color: #303133;
}

.config-header .el-icon {
  margin-right: 8px;
}

.address-card {
  border: 1px solid #e4e7ed;
  border-radius: 8px;
  box-shadow: 0 2px 12px 0 rgba(0, 0, 0, 0.1);
}

.address-card-compact .el-card__body {
  padding: 8px 16px 12px;
}

.address-card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.address-card-header .el-icon {
  margin-right: 6px;
  vertical-align: -0.2em;
}

.address-compact {
  min-height: 32px;
}

.address-line {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 4px;
  font-size: 13px;
}

.address-line:last-of-type {
  margin-bottom: 0;
}

.address-tag {
  flex-shrink: 0;
  width: 48px;
  color: #909399;
}

.address-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #303133;
}

.ota-test-block {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid #ebeef5;
}

.ota-test-block .address-tag {
  display: block;
  margin-bottom: 4px;
}

.ota-test-pre {
  margin: 0;
  padding: 8px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.4;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 160px;
  overflow: auto;
}

.address-empty {
  color: #909399;
  font-size: 13px;
  padding: 4px 0;
}

.config-actions {
  display: flex;
  gap: 15px;
  padding: 10px 0;
}

.config-btn {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 12px 20px;
  border-radius: 6px;
  transition: all 0.3s ease;
  font-weight: 500;
}

.config-btn .el-icon {
  margin-right: 8px;
  font-size: 16px;
}

.config-btn:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.config-btn {
  position: relative;
}

.btn-tip {
  position: absolute;
  bottom: -20px;
  left: 50%;
  transform: translateX(-50%);
  font-size: 10px;
  color: #909399;
  white-space: nowrap;
  opacity: 0;
  transition: opacity 0.3s ease;
}

.config-btn:hover .btn-tip {
  opacity: 1;
}

.stat-card {
  height: 120px;
}

.stat-content {
  display: flex;
  align-items: center;
  height: 100%;
}

.stat-icon {
  margin-right: 20px;
}

.stat-info {
  flex: 1;
}

.stat-number {
  font-size: 32px;
  font-weight: bold;
  color: #333;
  line-height: 1;
}

.stat-label {
  font-size: 14px;
  color: #666;
  margin-top: 8px;
}

.card-header {
  font-weight: bold;
  font-size: 16px;
}

.system-info {
  padding: 10px 0;
}

.info-item {
  display: flex;
  align-items: center;
  margin-bottom: 15px;
}

.info-item:last-child {
  margin-bottom: 0;
}

.info-label {
  width: 120px;
  color: #666;
}

.info-value {
  color: #333;
  font-weight: 500;
}

.quick-actions {
  display: flex;
  flex-direction: column;
  gap: 15px;
  padding: 10px 0;
}

.quick-actions .el-button {
  justify-content: flex-start;
}
</style>
