<template>
  <div class="config-page">
    <div class="page-header">
      <div class="header-left">
        <h2>ASR配置管理</h2>
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
      :title="editingConfig ? '编辑ASR配置' : '添加ASR配置'"
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
          <el-select v-model="form.provider" placeholder="请选择提供商" style="width: 100%" @change="onProviderChange">
            <el-option label="FunASR" value="funasr" />
            <el-option label="豆包" value="doubao" />
            <el-option label="Microsoft ASR" value="microsoft" />
          </el-select>
        </el-form-item>
        
        <el-form-item label="配置名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入配置名称" />
        </el-form-item>
        
        <el-form-item label="配置ID" prop="config_id">
          <el-input v-model="form.config_id" placeholder="请输入唯一的配置ID" />
        </el-form-item>
        
        <!-- 移除是否默认开关，现在在列表页操作 -->
        
        <!-- FunASR配置字段 -->
        <div v-if="form.provider === 'funasr'">
          <el-form-item label="主机地址" prop="funasr.host">
            <el-input v-model="form.funasr.host" placeholder="请输入主机地址" />
          </el-form-item>
          
          <el-form-item label="端口" prop="funasr.port">
            <el-input-number v-model="form.funasr.port" :min="1" :max="65535" style="width: 100%" />
          </el-form-item>
          
          <el-form-item label="模式" prop="funasr.mode">
            <el-select v-model="form.funasr.mode" placeholder="请选择模式" style="width: 100%">
              <el-option label="2pass" value="2pass" />
              <el-option label="offline" value="offline" />
              <el-option label="online" value="online" />
            </el-select>
          </el-form-item>
          
          <el-form-item label="采样率" prop="funasr.sample_rate">
            <el-select v-model="form.funasr.sample_rate" placeholder="请选择采样率" style="width: 100%">
              <el-option label="8000" :value="8000" />
              <el-option label="16000" :value="16000" />
              <el-option label="44100" :value="44100" />
              <el-option label="48000" :value="48000" />
            </el-select>
          </el-form-item>
          
          <el-form-item label="块大小" prop="funasr.chunk_size">
            <div style="display: flex; gap: 8px; width: 100%">
              <el-input-number 
                v-model="form.funasr.chunk_size[0]" 
                :min="1" 
                placeholder="前向块大小"
                style="flex: 1"
              />
              <el-input-number 
                v-model="form.funasr.chunk_size[1]" 
                :min="1" 
                placeholder="中间块大小"
                style="flex: 1"
              />
              <el-input-number 
                v-model="form.funasr.chunk_size[2]" 
                :min="1" 
                placeholder="后向块大小"
                style="flex: 1"
              />
            </div>
            <div class="form-tip">
              <el-icon><InfoFilled /></el-icon>
              格式：[前向, 中间, 后向]，例如：[5, 10, 5]
            </div>
          </el-form-item>
          
          <el-form-item label="块间隔" prop="funasr.chunk_interval">
            <el-input-number v-model="form.funasr.chunk_interval" :min="1" style="width: 100%" />
          </el-form-item>
          
          <el-form-item label="最大连接数" prop="funasr.max_connections">
            <el-input-number v-model="form.funasr.max_connections" :min="1" style="width: 100%" />
          </el-form-item>
          
          <el-form-item label="超时时间(秒)" prop="funasr.timeout">
            <el-input-number v-model="form.funasr.timeout" :min="1" style="width: 100%" />
          </el-form-item>
          
          <el-form-item label="自动结束" prop="funasr.auto_end">
            <el-switch v-model="form.funasr.auto_end" />
            <div class="form-tip">
              <el-icon><InfoFilled /></el-icon>
              确保FunASR已进行相应配置
            </div>
          </el-form-item>
        </div>

        <!-- 豆包ASR配置字段 -->
        <div v-if="form.provider === 'doubao'">
          <el-form-item label="应用ID" prop="doubao.appid">
            <el-input v-model="form.doubao.appid" placeholder="请输入应用ID" />
          </el-form-item>
          
          <el-form-item label="访问令牌" prop="doubao.access_token">
            <el-input v-model="form.doubao.access_token" type="password" placeholder="请输入访问令牌" show-password />
          </el-form-item>
          
          <el-form-item label="WebSocket URL" prop="doubao.ws_url">
            <el-input v-model="form.doubao.ws_url" placeholder="请输入WebSocket URL" />
          </el-form-item>
          
          <el-form-item label="模型名称" prop="doubao.model_name">
            <el-input v-model="form.doubao.model_name" placeholder="请输入模型名称" />
          </el-form-item>
          
          <el-form-item label="结束窗口大小" prop="doubao.end_window_size">
            <el-input-number v-model="form.doubao.end_window_size" :min="1" style="width: 100%" />
          </el-form-item>
          
          <el-form-item label="启用标点符号" prop="doubao.enable_punc">
            <el-switch v-model="form.doubao.enable_punc" />
          </el-form-item>
          
          <el-form-item label="启用反向文本标准化" prop="doubao.enable_itn">
            <el-switch v-model="form.doubao.enable_itn" />
          </el-form-item>
          
          <el-form-item label="启用数字检测修正" prop="doubao.enable_ddc">
            <el-switch v-model="form.doubao.enable_ddc" />
          </el-form-item>
          
          <el-form-item label="分块时长(毫秒)" prop="doubao.chunk_duration">
            <el-input-number v-model="form.doubao.chunk_duration" :min="1" style="width: 100%" />
          </el-form-item>
          
          <el-form-item label="超时时间(秒)" prop="doubao.timeout">
            <el-input-number v-model="form.doubao.timeout" :min="1" style="width: 100%" />
          </el-form-item>
        </div>

        <!-- Microsoft ASR配置字段 -->
        <div v-if="form.provider === 'microsoft'">
          <el-form-item label="订阅密钥" prop="microsoft.subscription_key">
            <el-input v-model="form.microsoft.subscription_key" placeholder="请输入Azure订阅密钥" type="password" show-password />
          </el-form-item>
          
          <el-form-item label="服务区域" prop="microsoft.region">
            <el-input v-model="form.microsoft.region" placeholder="请输入服务区域（如：eastasia, koreacentral）" />
          </el-form-item>
          
          <el-form-item label="识别语言" prop="microsoft.language">
            <el-input v-model="form.microsoft.language" placeholder="请输入语言代码（如：zh-CN）" />
          </el-form-item>
          
          <el-form-item label="超时时间(秒)" prop="microsoft.timeout">
            <el-input-number v-model="form.microsoft.timeout" :min="1" :max="300" style="width: 100%" />
          </el-form-item>
          
          <div class="form-tip">
            <el-icon><InfoFilled /></el-icon>
            采样率固定为 16kHz，仅支持流式识别模式
          </div>
        </div>
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
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, InfoFilled } from '@element-plus/icons-vue'
import api from '../../utils/api'

const configs = ref([])
const loading = ref(false)
const saving = ref(false)
const showDialog = ref(false)
const editingConfig = ref(null)
const formRef = ref()

const form = reactive({
  name: '',
  config_id: '',
  provider: '',
  is_default: false,
  enabled: true,
  funasr: {
    host: 'localhost',
    port: 10095,
    mode: 'offline',
    sample_rate: 16000,
    chunk_size: [5, 10, 5],
    chunk_interval: 10,
    max_connections: 100,
    timeout: 30,
    auto_end: false
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
  microsoft: {
    subscription_key: '',
    region: 'eastasia',
    language: 'zh-CN',
    timeout: 60
  }
})

// 根据provider生成配置JSON
const generateConfig = () => {
  if (form.provider === 'funasr') {
    return JSON.stringify(form.funasr)
  } else if (form.provider === 'doubao') {
    return JSON.stringify(form.doubao)
  } else if (form.provider === 'microsoft') {
    return JSON.stringify(form.microsoft)
  }
  return '{}'
}

const rules = {
  name: [{ required: true, message: '请输入配置名称', trigger: 'blur' }],
  config_id: [{ required: true, message: '请输入配置ID', trigger: 'blur' }],
  provider: [{ required: true, message: '请选择提供商', trigger: 'change' }],
  'funasr.host': [{ required: true, message: '请输入主机地址', trigger: 'blur' }],
  'funasr.port': [{ required: true, message: '请输入端口', trigger: 'blur' }],
  'funasr.mode': [{ required: true, message: '请选择模式', trigger: 'change' }],
  'funasr.sample_rate': [{ required: true, message: '请选择采样率', trigger: 'change' }],
  'funasr.chunk_size': [{ required: true, message: '请输入块大小', trigger: 'blur' }],
  'funasr.chunk_interval': [{ required: true, message: '请输入块间隔', trigger: 'blur' }],
  'funasr.max_connections': [{ required: true, message: '请输入最大连接数', trigger: 'blur' }],
  'funasr.timeout': [{ required: true, message: '请输入超时时间', trigger: 'blur' }],
  'doubao.appid': [{ required: true, message: '请输入应用ID', trigger: 'blur' }],
  'doubao.access_token': [{ required: true, message: '请输入访问令牌', trigger: 'blur' }],
  'doubao.ws_url': [{ required: true, message: '请输入WebSocket URL', trigger: 'blur' }],
  'doubao.model_name': [{ required: true, message: '请输入模型名称', trigger: 'blur' }],
  'doubao.end_window_size': [{ required: true, message: '请输入结束窗口大小', trigger: 'blur' }],
  'doubao.timeout': [{ required: true, message: '请输入超时时间', trigger: 'blur' }],
  // Microsoft ASR 验证规则
  'microsoft.subscription_key': [{ required: true, message: '请输入订阅密钥', trigger: 'blur' }],
  'microsoft.region': [{ required: true, message: '请输入服务区域', trigger: 'blur' }],
  'microsoft.language': [{ required: true, message: '请输入识别语言', trigger: 'blur' }]
}

const loadConfigs = async () => {
  loading.value = true
  try {
    const response = await api.get('/admin/asr-configs')
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
  
  // 解析配置JSON并填充到对应字段
  try {
    const configObj = JSON.parse(config.json_data || '{}')
    
    // 兼容新旧格式：检查是否是包装格式（包含provider层）还是直接格式
    if (configObj.funasr) {
      // 旧格式：包含provider层
      const funasrConfig = { ...form.funasr, ...configObj.funasr }
      // 兼容chunk_size：如果是单个数字或无效格式，转换为默认值 [5, 10, 5]
      if (typeof funasrConfig.chunk_size === 'number') {
        funasrConfig.chunk_size = [5, 10, 5]
      } else if (!Array.isArray(funasrConfig.chunk_size) || funasrConfig.chunk_size.length !== 3) {
        funasrConfig.chunk_size = [5, 10, 5]
      }
      form.funasr = funasrConfig
    } else if (configObj.doubao) {
      // 旧格式：包含provider层
      form.doubao = { ...form.doubao, ...configObj.doubao }
    } else if (config.provider === 'funasr' && configObj.host) {
      // 新格式：直接包含配置内容
      const funasrConfig = { ...form.funasr, ...configObj }
      // 兼容chunk_size：如果是单个数字或无效格式，转换为默认值 [5, 10, 5]
      if (typeof funasrConfig.chunk_size === 'number') {
        funasrConfig.chunk_size = [5, 10, 5]
      } else if (!Array.isArray(funasrConfig.chunk_size) || funasrConfig.chunk_size.length !== 3) {
        funasrConfig.chunk_size = [5, 10, 5]
      }
      form.funasr = funasrConfig
    } else if (config.provider === 'doubao' && (configObj.appid || configObj.access_token)) {
      // 新格式：直接包含配置内容
      form.doubao = { ...form.doubao, ...configObj }
    } else if (config.provider === 'microsoft' && (configObj.subscription_key || configObj.region)) {
      // Microsoft ASR 配置
      form.microsoft = { ...form.microsoft, ...configObj }
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
          enabled: form.enabled !== undefined ? form.enabled : true, // 确保enabled字段存在
          json_data: generateConfig()
        }
        
        if (editingConfig.value) {
          await api.put(`/admin/asr-configs/${editingConfig.value.id}`, configData)
          ElMessage.success('配置更新成功')
        } else {
          await api.post('/admin/asr-configs', configData)
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
    
    await api.put(`/admin/asr-configs/${config.id}`, configData)
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
    
    await api.delete(`/admin/asr-configs/${id}`)
    ElMessage.success('删除成功')
    loadConfigs()
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败')
    }
  }
}

const onProviderChange = () => {
  // 当选择funasr时，设置默认模式为offline
  if (form.provider === 'funasr') {
    form.funasr.mode = 'offline'
  }
}

const resetForm = () => {
  editingConfig.value = null
  form.name = ''
  form.config_id = ''
  form.provider = ''
  form.is_default = false
  form.enabled = true
  form.funasr = {
    host: 'localhost',
    port: 10095,
    mode: 'offline',
    sample_rate: 16000,
    chunk_size: [5, 10, 5],
    chunk_interval: 10,
    max_connections: 100,
    timeout: 30,
    auto_end: false
  }
  form.doubao = {
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
  }
  form.microsoft = {
    subscription_key: '',
    region: 'eastasia',
    language: 'zh-CN',
    timeout: 60
  }
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
</style>