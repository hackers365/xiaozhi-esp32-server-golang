<template>
  <div class="scheduler-tasks">
    <div class="header">
      <h2>定时任务管理</h2>
      <el-button type="primary" @click="openCreate">新增任务</el-button>
    </div>

    <el-table :data="tasks" v-loading="loading" border>
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="name" label="名称" min-width="180" />
      <el-table-column prop="schedule_type" label="调度类型" width="110" />
      <el-table-column label="执行时间/规则" min-width="220">
        <template #default="{ row }">
          <span v-if="row.schedule_type === 'once'">{{ formatDateTime(row.run_at) }}</span>
          <span v-else-if="row.schedule_type === 'interval'">每 {{ row.interval_sec || 0 }} 秒</span>
          <span v-else>{{ row.cron_expr || '-' }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="task_mode" label="任务模式" width="120" />
      <el-table-column prop="enabled" label="启用" width="90">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '是' : '否' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="240" fixed="right">
        <template #default="{ row }">
          <el-button size="small" @click="openEdit(row)">编辑</el-button>
          <el-button size="small" type="warning" @click="toggleEnabled(row)">
            {{ row.enabled ? '停用' : '启用' }}
          </el-button>
          <el-button size="small" type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑任务' : '新增任务'" width="720px">
      <el-form :model="form" label-width="120px">
        <el-form-item label="任务名称">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="调度类型">
          <el-select v-model="form.schedule_type" style="width: 100%">
            <el-option label="一次性" value="once" />
            <el-option label="周期(秒)" value="interval" />
            <el-option label="Cron" value="cron" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.schedule_type === 'once'" label="执行时间">
          <el-date-picker
            v-model="form.run_at"
            type="datetime"
            value-format="YYYY-MM-DDTHH:mm:ssZ"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item v-if="form.schedule_type === 'interval'" label="周期秒数">
          <el-input-number v-model="form.interval_sec" :min="1" />
        </el-form-item>
        <el-form-item v-if="form.schedule_type === 'cron'" label="Cron表达式">
          <el-input v-model="form.cron_expr" placeholder="例如: 0 0 8 * * *" />
        </el-form-item>
        <el-form-item label="时区">
          <el-input v-model="form.timezone" placeholder="Asia/Shanghai" />
        </el-form-item>
        <el-form-item label="任务模式">
          <el-select v-model="form.task_mode" style="width: 100%">
            <el-option label="注入LLM" value="inject_llm" />
            <el-option label="调用MCP工具" value="mcp_call" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="form.task_mode === 'inject_llm'" label="任务文本">
          <el-input v-model="form.task_text" type="textarea" :rows="3" />
        </el-form-item>
        <el-form-item v-else label="工具名称">
          <el-input v-model="form.tool_name" />
        </el-form-item>
        <el-form-item v-if="form.task_mode === 'mcp_call'" label="工具参数(JSON)">
          <el-input v-model="argumentsText" type="textarea" :rows="4" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../../utils/api'

const loading = ref(false)
const tasks = ref([])
const dialogVisible = ref(false)
const editingId = ref(null)
const argumentsText = ref('{}')

const createDefaultForm = () => ({
  name: '',
  enabled: true,
  schedule_type: 'once',
  run_at: '',
  cron_expr: '',
  interval_sec: 60,
  timezone: 'Asia/Shanghai',
  task_mode: 'inject_llm',
  task_text: '',
  tool_name: '',
  arguments: {}
})

const form = ref(createDefaultForm())

const loadTasks = async () => {
  loading.value = true
  try {
    const res = await api.get('/admin/scheduler-tasks')
    tasks.value = res.data?.data || []
  } catch (e) {
    ElMessage.error(e.response?.data?.error || e.message || '加载定时任务失败')
  } finally {
    loading.value = false
  }
}

const openCreate = () => {
  editingId.value = null
  form.value = createDefaultForm()
  argumentsText.value = '{}'
  dialogVisible.value = true
}

const openEdit = (row) => {
  editingId.value = row.id
  form.value = {
    ...createDefaultForm(),
    ...row
  }
  argumentsText.value = JSON.stringify(row.arguments || {}, null, 2)
  dialogVisible.value = true
}

const submit = async () => {
  try {
    const parsedArgs = JSON.parse(argumentsText.value || '{}')
    const payload = {
      ...form.value,
      arguments: parsedArgs
    }
    if (editingId.value) {
      await api.put(`/admin/scheduler-tasks/${editingId.value}`, payload)
      ElMessage.success('更新成功')
    } else {
      await api.post('/admin/scheduler-tasks', payload)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    await loadTasks()
  } catch (e) {
    if (e instanceof SyntaxError) {
      ElMessage.error('工具参数 JSON 格式错误')
      return
    }
    ElMessage.error(e.response?.data?.error || e.message || '保存失败')
  }
}

const toggleEnabled = async (row) => {
  try {
    await api.put(`/admin/scheduler-tasks/${row.id}`, {
      ...row,
      enabled: !row.enabled
    })
    ElMessage.success('状态更新成功')
    await loadTasks()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || e.message || '更新状态失败')
  }
}

const remove = async (row) => {
  try {
    await ElMessageBox.confirm(`确定删除任务「${row.name}」吗？`, '提示', {
      type: 'warning'
    })
    await api.delete(`/admin/scheduler-tasks/${row.id}`)
    ElMessage.success('删除成功')
    await loadTasks()
  } catch (e) {
    if (e === 'cancel' || e === 'close') return
    ElMessage.error(e.response?.data?.error || e.message || '删除失败')
  }
}

const formatDateTime = (v) => {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  return d.toLocaleString()
}

onMounted(loadTasks)
</script>

<style scoped>
.scheduler-tasks {
  padding: 20px;
  background: #fff;
  border-radius: 8px;
}
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}
</style>
