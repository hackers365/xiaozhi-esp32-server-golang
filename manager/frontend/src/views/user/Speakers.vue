<template>
  <div class="speakers-page">
    <!-- 筛选栏 -->
    <div class="filter-bar">
      <el-select
        v-model="filterAgentId"
        placeholder="按智能体筛选"
        clearable
        style="width: 200px; margin-right: 10px;"
        @change="loadSpeakerGroups"
      >
        <el-option label="全部智能体" value="" />
        <el-option
          v-for="agent in agents"
          :key="agent.id"
          :label="agent.name"
          :value="agent.id"
        />
      </el-select>
      <el-input
        v-model="searchKeyword"
        placeholder="搜索声纹组名称"
        clearable
        style="width: 250px;"
        @input="handleSearch"
      >
        <template #prefix>
          <el-icon><Search /></el-icon>
        </template>
      </el-input>
      <el-button class="create-group-button" type="primary" @click="handleAddGroup">
        <el-icon><Plus /></el-icon>
        创建声纹组
      </el-button>
    </div>

    <!-- 声纹组列表 -->
    <div v-loading="loading" class="speakers-content">
      <el-table :data="filteredGroups" stripe style="width: 100%">
        <el-table-column prop="name" label="声纹组名称" min-width="150" />
        <el-table-column prop="agent_name" label="关联智能体" min-width="120" />
        <el-table-column label="Prompt" min-width="200">
          <template #default="{ row }">
            <el-popover
              placement="top"
              :width="300"
              trigger="hover"
              v-if="row.prompt"
            >
              <template #reference>
                <span class="prompt-text">{{ truncateText(row.prompt, 30) }}</span>
              </template>
              <div class="prompt-popover">{{ row.prompt }}</div>
            </el-popover>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="sample_count" label="样本数量" width="100" align="center">
          <template #default="{ row }">
            <el-tag type="info">{{ row.sample_count }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="created_at" label="创建时间" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column label="操作" width="360" fixed="right">
          <template #default="{ row }">
            <div class="action-buttons">
            <el-button
              type="success"
              size="small"
              @click="handleVerifyGroup(row)"
            >
              <el-icon><VideoPlay /></el-icon>
              验证
            </el-button>
            <el-button
              type="primary"
              size="small"
              @click="handleViewSamples(row)"
            >
              <el-icon><View /></el-icon>
              管理声纹
            </el-button>
              <el-button
                type="primary"
                size="small"
                plain
                @click="handleEditGroup(row)"
              >
                  <el-icon><Edit /></el-icon>
                  编辑
                </el-button>
              <el-button
                type="danger"
                size="small"
                @click="handleDeleteGroup(row)"
              >
                <el-icon><Delete /></el-icon>
                删除
                </el-button>
              </div>
          </template>
        </el-table-column>
      </el-table>

      <div v-if="filteredGroups.length === 0 && !loading" class="empty-state">
        <el-empty description="暂无声纹组数据" />
      </div>
    </div>

    <!-- 创建/编辑声纹组对话框 -->
    <el-dialog
      v-model="showGroupDialog"
      :title="groupDialogMode === 'add' ? '创建声纹组' : '编辑声纹组'"
      width="600px"
    >
      <el-form
        ref="groupFormRef"
        :model="groupForm"
        :rules="groupRules"
        label-width="100px"
      >
        <el-form-item label="关联智能体" prop="agent_id">
          <el-select
            v-model="groupForm.agent_id"
            placeholder="请选择智能体"
            style="width: 100%"
          >
            <el-option
              v-for="agent in agents"
              :key="agent.id"
              :label="agent.name"
              :value="agent.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="声纹名称" prop="name">
          <el-input
            v-model="groupForm.name"
            placeholder="请输入声纹名称"
            :maxlength="100"
            show-word-limit
          />
        </el-form-item>
        <el-form-item label="Prompt" prop="prompt">
          <el-input
            v-model="groupForm.prompt"
            type="textarea"
            :rows="4"
            placeholder="请输入角色提示词（可选）"
          />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input
            v-model="groupForm.description"
            type="textarea"
            :rows="3"
            placeholder="请输入描述信息（可选）"
            :maxlength="200"
            show-word-limit
          />
        </el-form-item>
        <el-form-item label="我复刻的音色" v-if="cloneVoicePresets.length > 0">
          <div class="clone-voice-line" v-loading="cloneVoicesLoading">
            <button
              v-for="clone in cloneVoicePresets"
              :key="clone.id"
              type="button"
              class="clone-voice-item"
              :class="{ active: isCloneVoiceSelected(clone) }"
              :title="`${clone.tts_config_name || clone.tts_config_id} · ${clone.provider_voice_id}`"
              @click="applyCloneVoice(clone)"
            >
              <span class="clone-voice-name">{{ clone.name || clone.provider_voice_id }}</span>
            </button>
          </div>
          <div class="form-help">点击后会自动填充 TTS 配置和音色</div>
        </el-form-item>
        <el-form-item label="TTS配置" prop="tts_config_id">
          <el-select
            v-model="groupForm.tts_config_id"
            placeholder="请选择TTS配置（可选）"
            clearable
            style="width: 100%"
            @change="handleTtsConfigChange"
          >
            <el-option
              v-for="ttsConfig in ttsConfigs"
              :key="ttsConfig.config_id"
              :label="ttsConfig.is_default ? `${ttsConfig.name} (默认)` : ttsConfig.name"
              :value="ttsConfig.config_id"
            >
              <div class="config-option">
                {{ ttsConfig.name }}
                <el-tag v-if="ttsConfig.is_default" type="success" size="small" style="margin-left: 8px;">默认</el-tag>
              </div>
              <span class="config-desc">{{ ttsConfig.provider || '暂无描述' }}</span>
            </el-option>
          </el-select>
          <div class="form-help" v-if="groupForm.tts_config_id">
            {{ getCurrentTtsConfigInfo() }}
          </div>
        </el-form-item>
        <el-form-item label="音色" prop="voice" v-if="groupForm.tts_config_id">
          <el-select
            v-model="groupForm.voice"
            placeholder="请选择或输入音色"
            filterable
            allow-create
            clearable
            style="width: 100%"
          >
            <el-option
              v-for="voice in currentVoiceOptions"
              :key="voice.value"
              :label="voice.label"
              :value="voice.value"
            />
          </el-select>
          <div class="form-help">
            当前TTS配置: {{ getCurrentTtsConfigName() }}，可以搜索音色名称或值，也可以手动输入自定义音色值。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showGroupDialog = false">取消</el-button>
        <el-button type="primary" @click="handleSubmitGroup" :loading="submitting">
          {{ groupDialogMode === 'add' ? '创建' : '保存' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 样本管理弹层 -->
    <el-drawer
      v-model="showSampleDrawer"
      title="样本管理"
      :size="800"
      :before-close="handleCloseSampleDrawer"
    >
      <div v-if="currentGroup" class="sample-drawer">
        <!-- 声纹组信息 -->
        <el-card class="group-info-card" shadow="never">
          <div class="group-info">
            <h3>{{ currentGroup.name }}</h3>
            <div v-if="currentGroup.prompt" class="prompt-section">
              <strong>Prompt:</strong>
              <p>{{ currentGroup.prompt }}</p>
            </div>
            <div v-if="currentGroup.description" class="description-section">
              <strong>描述:</strong>
              <p>{{ currentGroup.description }}</p>
            </div>
          </div>
        </el-card>

        <!-- 样本列表 -->
        <div class="samples-section">
          <div class="samples-header">
            <h4>样本列表</h4>
            <div class="samples-header-actions">
              <el-button type="success" @click="handleVerifyFromSamples">
                <el-icon><VideoPlay /></el-icon>
                验证声纹
              </el-button>
              <el-button type="primary" @click="handleAddSample">
                <el-icon><Plus /></el-icon>
                上传新样本
              </el-button>
            </div>
          </div>

          <el-table :data="samples" stripe style="width: 100%">
            <el-table-column prop="uuid" label="UUID" min-width="200">
              <template #default="{ row }">
                <el-tooltip :content="row.uuid" placement="top">
                  <span class="uuid-text">{{ truncateId(row.uuid) }}</span>
                </el-tooltip>
                <el-button
                  type="text"
                  size="small"
                  @click="copyToClipboard(row.uuid)"
                  style="margin-left: 8px;"
                >
                  <el-icon><DocumentCopy /></el-icon>
                </el-button>
              </template>
            </el-table-column>
            <el-table-column prop="file_name" label="文件名" min-width="150" />
            <el-table-column prop="file_size" label="文件大小" width="100">
              <template #default="{ row }">
                {{ formatFileSize(row.file_size) }}
              </template>
            </el-table-column>
            <el-table-column prop="duration" label="时长" width="80">
              <template #default="{ row }">
                {{ row.duration ? row.duration + 's' : '-' }}
              </template>
            </el-table-column>
            <el-table-column prop="created_at" label="创建时间" width="180">
              <template #default="{ row }">
                {{ formatDate(row.created_at) }}
              </template>
            </el-table-column>
            <el-table-column label="操作" width="180" fixed="right">
              <template #default="{ row }">
                <el-button
                  type="primary"
                  size="small"
                  link
                  @click="handlePlaySample(row)"
                >
                  <el-icon><VideoPlay /></el-icon>
                  播放
                </el-button>
                <el-button
                  type="primary"
                  size="small"
                  link
                  @click="handleDownloadSample(row)"
                >
                  <el-icon><Download /></el-icon>
                  下载
                </el-button>
                <el-button
                  type="danger"
                  size="small"
                  link
                  @click="handleDeleteSample(row)"
                >
                  <el-icon><Delete /></el-icon>
                  删除
                </el-button>
              </template>
            </el-table-column>
          </el-table>

          <div v-if="samples.length === 0" class="empty-samples">
            <el-empty description="暂无样本，请上传音频文件" />
          </div>
        </div>
      </div>
    </el-drawer>

    <!-- 上传样本对话框 -->
    <el-dialog
      v-model="showUploadDialog"
      title="添加声纹样本"
      width="600px"
      :before-close="handleCloseUploadDialog"
    >
      <el-tabs v-model="uploadMode" class="upload-tabs">
        <!-- 从历史记录选择 -->
        <el-tab-pane label="从历史记录选择" name="history">
          <div class="history-section">
            <el-form :model="historyForm" label-width="100px">
              <el-form-item label="智能体">
                <el-select
                  v-model="historyForm.agent_id"
                  placeholder="请选择智能体"
                  style="width: 100%"
                  @change="loadHistoryMessages"
                  clearable
                >
                  <el-option
                    v-for="agent in agents"
                    :key="agent.id"
                    :label="agent.name"
                    :value="agent.id"
                  />
                </el-select>
              </el-form-item>
            </el-form>
            
            <div v-loading="loadingHistory" class="history-list">
              <div v-if="historyMessages.length === 0 && !loadingHistory" class="empty-history">
                <el-empty description="暂无历史聊天记录，请先选择智能体" />
              </div>
              <el-table
                v-else
                :data="historyMessages"
                row-key="message_id"
                stripe
                style="width: 100%"
                max-height="400"
                @row-click="handleSelectHistoryMessage"
              >
                <el-table-column label="选择" width="80" align="center">
                  <template #default="{ row }">
                    <el-radio
                      :model-value="historyForm.selected_message_id"
                      :label="row.message_id"
                      @change="historyForm.selected_message_id = row.message_id"
                    />
                  </template>
                </el-table-column>
                <el-table-column prop="content" label="消息内容" min-width="200">
                  <template #default="{ row }">
                    <div class="message-content">{{ truncateText(row.content, 50) }}</div>
                  </template>
                </el-table-column>
                <el-table-column prop="device_id" label="设备ID" width="150">
                  <template #default="{ row }">
                    <el-tooltip :content="row.device_id" placement="top">
                      <span>{{ truncateId(row.device_id) }}</span>
                    </el-tooltip>
                  </template>
                </el-table-column>
                <el-table-column prop="created_at" label="时间" width="180">
                  <template #default="{ row }">
                    {{ formatDate(row.created_at) }}
                  </template>
                </el-table-column>
                <el-table-column label="操作" width="100">
                  <template #default="{ row }">
                    <el-button
                      type="primary"
                      size="small"
                      link
                      @click.stop="handlePreviewHistoryAudio(row)"
                    >
                      <el-icon><VideoPlay /></el-icon>
                      试听
                    </el-button>
                  </template>
                </el-table-column>
              </el-table>
            </div>
          </div>
        </el-tab-pane>
        
        <!-- 上传文件 -->
        <el-tab-pane label="上传文件" name="upload">
          <el-form
            ref="uploadFormRef"
            :model="uploadForm"
            :rules="uploadRules"
            label-width="0"
          >
            <el-form-item prop="audio">
          <el-upload
            ref="uploadRef"
            :auto-upload="false"
            :on-change="handleFileChange"
            :on-remove="handleFileRemove"
            :limit="1"
            accept=".wav,audio/wav"
            drag
                class="audio-upload"
          >
                <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
            <div class="el-upload__text">
                  将 WAV 音频文件拖到此处，或<em>点击选择文件</em>
            </div>
            <template #tip>
              <div class="el-upload__tip">
                只能上传 WAV 格式的音频文件，建议时长 3-10 秒，文件大小不超过 10MB
              </div>
            </template>
          </el-upload>
              <div v-if="uploadForm.audioFile" class="file-info">
            <el-icon><Document /></el-icon>
                <span>{{ uploadForm.audioFile.name }}</span>
                <span class="file-size">({{ formatFileSize(uploadForm.audioFile.size) }})</span>
          </div>
        </el-form-item>
      </el-form>
        </el-tab-pane>

        <!-- 录制音频 -->
        <el-tab-pane label="录制音频" name="record">
          <div class="record-section">
            <div class="record-status">
              <div v-if="!isRecording && !recordedBlob" class="record-ready">
                <el-icon size="48" color="var(--apple-primary)"><Microphone /></el-icon>
                <p>点击下方按钮开始录制</p>
                <p class="record-tip">建议录制 3-10 秒的清晰音频</p>
              </div>
              <div v-else-if="isRecording" class="record-recording">
                <div class="recording-indicator">
                  <span class="recording-dot"></span>
                  <span class="recording-text">正在录制中...</span>
                </div>
                <div class="record-time">{{ formatRecordTime(recordTime) }}</div>
                <p class="record-tip">点击停止按钮结束录制</p>
              </div>
              <div v-else-if="recordedBlob" class="record-complete">
                <el-icon size="48" color="var(--apple-success)"><CircleCheck /></el-icon>
                <p>录制完成</p>
                <p class="record-tip">时长: {{ formatRecordTime(recordTime) }}</p>
                <audio :src="recordedBlobUrl" controls class="record-preview"></audio>
              </div>
            </div>

            <div class="record-controls">
          <el-button 
                v-if="!isRecording && !recordedBlob"
            type="primary" 
            size="large"
                @click="startRecording"
                :disabled="!canRecord"
              >
                <el-icon><VideoPlay /></el-icon>
                开始录制
              </el-button>
              <el-button
                v-if="isRecording"
                type="danger"
                size="large"
                @click="stopRecording"
              >
                <el-icon><VideoPause /></el-icon>
                停止录制
              </el-button>
              <el-button
                v-if="recordedBlob"
                type="primary"
                size="large"
                @click="startRecording"
                :disabled="!canRecord"
              >
                <el-icon><Refresh /></el-icon>
                重新录制
          </el-button>
        </div>
          </div>
        </el-tab-pane>
      </el-tabs>

      <template #footer>
        <el-button @click="handleCloseUploadDialog">取消</el-button>
        <el-button
          type="primary"
          @click="handleSubmitSample"
          :loading="submitting"
          :disabled="!hasAudioFile"
        >
          确定
        </el-button>
      </template>
    </el-dialog>

    <!-- 验证声纹组对话框 -->
    <el-dialog
      v-model="showVerifyDialog"
      :title="`验证声纹组: ${currentVerifyGroup?.name || ''}`"
      width="600px"
      :before-close="handleCloseVerifyDialog"
    >
      <el-tabs v-model="verifyMode" class="verify-tabs">
        <!-- 上传文件 -->
        <el-tab-pane label="上传文件" name="upload">
          <el-form
            ref="verifyFormRef"
            :model="verifyForm"
            :rules="verifyRules"
            label-width="0"
          >
            <el-form-item prop="audio">
              <el-upload
                ref="verifyUploadRef"
                :auto-upload="false"
                :on-change="handleVerifyFileChange"
                :on-remove="handleVerifyFileRemove"
                :limit="1"
                accept=".wav,audio/wav"
                drag
                class="audio-upload"
                :file-list="verifyFileList"
              >
                <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
                <div class="el-upload__text">
                  将 WAV 音频文件拖到此处，或<em>点击选择文件</em>
                </div>
                <template #tip>
                  <div class="el-upload__tip">
                    只能上传 WAV 格式的音频文件，建议时长 3-10 秒，文件大小不超过 10MB
                  </div>
                </template>
              </el-upload>
              <div v-if="verifyForm.audioFile" class="file-info">
                <el-icon><Document /></el-icon>
                <span>{{ verifyForm.audioFile.name }}</span>
                <span class="file-size">({{ formatFileSize(verifyForm.audioFile.size) }})</span>
              </div>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- 录制音频 -->
        <el-tab-pane label="录制音频" name="record">
          <div class="record-section">
            <div class="record-status">
              <div v-if="!isVerifyRecording && !verifyRecordedBlob" class="record-ready">
                <el-icon size="48" color="var(--apple-primary)"><Microphone /></el-icon>
                <p>点击下方按钮开始录制</p>
                <p class="record-tip">建议录制 3-10 秒的清晰音频</p>
              </div>
              <div v-else-if="isVerifyRecording" class="record-recording">
                <div class="recording-indicator">
                  <span class="recording-dot"></span>
                  <span class="recording-text">正在录制中...</span>
                </div>
                <div class="record-time">{{ formatRecordTime(verifyRecordTime) }}</div>
                <p class="record-tip">点击停止按钮结束录制</p>
              </div>
              <div v-else-if="verifyRecordedBlob" class="record-complete">
                <el-icon size="48" color="var(--apple-success)"><CircleCheck /></el-icon>
                <p>录制完成</p>
                <p class="record-tip">时长: {{ formatRecordTime(verifyRecordTime) }}</p>
                <audio :src="verifyRecordedBlobUrl" controls class="record-preview"></audio>
              </div>
            </div>

            <div class="record-controls">
              <el-button
                v-if="!isVerifyRecording && !verifyRecordedBlob"
                type="primary"
                size="large"
                @click="startVerifyRecording"
                :disabled="!canRecord"
              >
                <el-icon><VideoPlay /></el-icon>
                开始录制
              </el-button>
              <el-button
                v-if="isVerifyRecording"
                type="danger"
                size="large"
                @click="stopVerifyRecording"
              >
                <el-icon><VideoPause /></el-icon>
                停止录制
              </el-button>
              <el-button
                v-if="verifyRecordedBlob"
                type="primary"
                size="large"
                @click="startVerifyRecording"
                :disabled="!canRecord"
              >
                <el-icon><Refresh /></el-icon>
                重新录制
              </el-button>
            </div>
          </div>
        </el-tab-pane>
      </el-tabs>

      <!-- 验证结果展示 -->
      <div v-if="verifyResult" class="verify-result">
        <el-divider>验证结果</el-divider>
        <div :class="['result-content', verifyResult.verified ? 'result-success' : 'result-failed']">
          <div class="result-icon">
            <el-icon v-if="verifyResult.verified" size="48" color="var(--apple-success)"><CircleCheck /></el-icon>
            <el-icon v-else size="48" color="var(--apple-danger)"><CircleClose /></el-icon>
          </div>
          <div class="result-info">
            <div class="result-status">
              {{ verifyResult.verified ? '验证通过' : '验证未通过' }}
            </div>
            <div class="result-details">
              <div>置信度: <strong>{{ (verifyResult.confidence * 100).toFixed(1) }}%</strong></div>
              <div>阈值: {{ (verifyResult.threshold * 100).toFixed(1) }}%</div>
            </div>
            <div class="result-message">{{ verifyResult.message }}</div>
          </div>
        </div>
      </div>

      <template #footer>
        <el-button @click="handleCloseVerifyDialog">取消</el-button>
        <el-button
          type="primary"
          @click="handleSubmitVerify"
          :loading="verifying"
          :disabled="!hasVerifyAudioFile"
        >
          验证
        </el-button>
      </template>
    </el-dialog>

    <!-- 音频播放器（隐藏） -->
    <audio ref="audioPlayer" style="display: none;" />
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { 
  Plus, 
  Edit, 
  Delete, 
  View,
  Search,
  UploadFilled,
  Document,
  DocumentCopy,
  VideoPlay,
  Download,
  Microphone,
  CircleCheck,
  CircleClose,
  Refresh,
  VideoPause
} from '@element-plus/icons-vue'
import api from '../../utils/api'

const loading = ref(false)
const submitting = ref(false)
const speakerGroups = ref([])
const agents = ref([])
const samples = ref([])
const filterAgentId = ref('')
const searchKeyword = ref('')

// 对话框状态
const showGroupDialog = ref(false)
const groupDialogMode = ref('add') // 'add' | 'edit'
const currentGroup = ref(null)
const showSampleDrawer = ref(false)
const showUploadDialog = ref(false)
const uploadMode = ref('history') // 'upload' | 'record' | 'history'

// 验证对话框相关
const showVerifyDialog = ref(false)
const verifyMode = ref('upload') // 'upload' | 'record'
const currentVerifyGroup = ref(null)
const verifying = ref(false)
const verifyResult = ref(null)

// 验证表单
const verifyForm = reactive({
  audioFile: null,
  audio: null
})

// 验证文件列表（用于 el-upload 组件）
const verifyFileList = ref([])

const verifyRules = {
  audio: [
    {
      validator: (rule, value, callback) => {
        if (!verifyForm.audioFile && !verifyRecordedBlob.value) {
          callback(new Error('请上传或录制音频文件'))
        } else {
          callback()
        }
      },
      trigger: ['change', 'blur']
    }
  ]
}

// 验证录音相关
const isVerifyRecording = ref(false)
const verifyMediaRecorder = ref(null)
const verifyRecordedBlob = ref(null)
const verifyRecordedBlobUrl = ref('')
const verifyRecordTime = ref(0)
const verifyRecordTimer = ref(null)

// 录音相关
const isRecording = ref(false)
const mediaRecorder = ref(null)
const recordedBlob = ref(null)
const recordedBlobUrl = ref('')
const recordTime = ref(0)
const recordTimer = ref(null)
const canRecord = ref(false)

// 表单引用
const groupFormRef = ref()
const uploadFormRef = ref()
const uploadRef = ref()
const verifyFormRef = ref()
const verifyUploadRef = ref()
const audioPlayer = ref()

// 声纹组表单
const groupForm = reactive({
  agent_id: null,
  name: '',
  prompt: '',
  description: '',
  tts_config_id: null,
  voice: null
})

const groupRules = {
  agent_id: [
    { required: true, message: '请选择关联智能体', trigger: 'change' }
  ],
  name: [
    { required: true, message: '请输入声纹名称', trigger: 'blur' },
    { min: 1, max: 100, message: '长度在 1 到 100 个字符', trigger: 'blur' }
  ]
}

// TTS配置相关
const ttsConfigs = ref([])
const currentVoiceOptions = ref([])
const cloneVoicePresets = ref([])
const cloneVoicesLoading = ref(false)

// 上传表单
const uploadForm = reactive({
  audioFile: null,
  audio: null
})

const uploadRules = {
  audio: [
    { 
      validator: (rule, value, callback) => {
        if (!uploadForm.audioFile && !recordedBlob.value) {
          callback(new Error('请上传或录制音频文件'))
        } else {
          callback()
        }
      }, 
      trigger: ['change', 'blur']
    }
  ]
}

// 历史记录相关
const loadingHistory = ref(false)
const historyMessages = ref([])
const historyForm = reactive({
  agent_id: null,
  selected_message_id: null
})

// 计算是否有音频文件
const hasAudioFile = computed(() => {
  if (uploadMode.value === 'history') {
    return historyForm.selected_message_id !== null
  }
  return uploadForm.audioFile !== null || recordedBlob.value !== null
})

// 过滤后的声纹组列表
const filteredGroups = computed(() => {
  let result = speakerGroups.value

  // 按智能体过滤
  if (filterAgentId.value) {
    result = result.filter(g => g.agent_id === filterAgentId.value)
  }

  // 按关键词搜索
  if (searchKeyword.value) {
    const keyword = searchKeyword.value.toLowerCase()
    result = result.filter(g =>
      g.name.toLowerCase().includes(keyword) ||
      (g.prompt && g.prompt.toLowerCase().includes(keyword)) ||
      (g.description && g.description.toLowerCase().includes(keyword))
    )
  }

  return result
})

// 加载智能体列表
const loadAgents = async () => {
  try {
    const response = await api.get('/user/agents')
    agents.value = response.data.data || []
  } catch (error) {
    console.error('加载智能体列表失败:', error)
    ElMessage.error('加载智能体列表失败')
  }
}

// 加载TTS配置列表
const loadTtsConfigs = async () => {
  try {
    const response = await api.get('/user/tts-configs')
    ttsConfigs.value = response.data.data || []
  } catch (error) {
    console.error('加载TTS配置失败:', error)
    ElMessage.error('加载TTS配置失败')
  }
}

const normalizeCloneStatus = (clone) => {
  const status = String(clone?.status || '').trim().toLowerCase()
  const taskStatus = String(clone?.task_status || '').trim().toLowerCase()
  if (status === 'failed' || taskStatus === 'failed') return 'failed'
  if (status === 'active' || taskStatus === 'succeeded') return 'active'
  if (taskStatus === 'queued' || taskStatus === 'processing') return taskStatus
  if (status === 'queued' || status === 'processing') return status
  return status || taskStatus || 'unknown'
}

const loadCloneVoicePresets = async () => {
  cloneVoicesLoading.value = true
  try {
    const response = await api.get('/user/voice-clones')
    const cloneList = response.data.data || []
    cloneVoicePresets.value = cloneList
      .filter(clone => normalizeCloneStatus(clone) === 'active')
      .filter(clone => clone?.tts_config_id && clone?.provider_voice_id)
      .map(clone => ({
        id: clone.id,
        name: clone.name || clone.provider_voice_id,
        provider_voice_id: clone.provider_voice_id,
        tts_config_id: clone.tts_config_id,
        tts_config_name: clone.tts_config_name || ''
      }))
  } catch (error) {
    console.error('加载复刻音色失败:', error)
    cloneVoicePresets.value = []
  } finally {
    cloneVoicesLoading.value = false
  }
}

const isCloneVoiceSelected = (clone) => {
  return groupForm.tts_config_id === clone?.tts_config_id && groupForm.voice === clone?.provider_voice_id
}

const applyCloneVoice = async (clone) => {
  if (!clone) return
  const ttsConfig = ttsConfigs.value.find(config => config.config_id === clone.tts_config_id)
  if (!ttsConfig) {
    return
  }
  groupForm.tts_config_id = clone.tts_config_id
  await handleTtsConfigChange(clone.tts_config_id)
  groupForm.voice = clone.provider_voice_id
}

// TTS配置变化时，加载对应的音色选项
const handleTtsConfigChange = async (configId) => {
  if (!configId) {
    currentVoiceOptions.value = []
    groupForm.voice = null
    return
  }
  
  const config = ttsConfigs.value.find(c => c.config_id === configId)
  if (!config) {
    currentVoiceOptions.value = []
    return
  }

  try {
    // 从后端API获取该provider的完整音色列表
    const params = { provider: config.provider }
    // 总是带上config_id参数
    if (configId) {
      params.config_id = configId
    }
    const response = await api.get('/user/voice-options', { params })
    currentVoiceOptions.value = response.data.data || []
  } catch (error) {
    console.error('加载音色列表失败:', error)
    currentVoiceOptions.value = []
    ElMessage.warning('加载音色列表失败，请稍后重试')
  }
}

// 根据不同provider提取音色选项
const extractVoiceOptions = (provider, config) => {
  const options = []
  
  if (!config) return options
  
  // 根据不同的TTS提供商提取音色
  switch (provider) {
    case 'edge':
    case 'microsoft':
      // Edge TTS 常用音色
      if (config.voice) {
        options.push({ label: config.voice, value: config.voice })
      }
      // 添加常用的中文音色
      const edgeVoices = [
        { label: 'zh-CN-XiaoxiaoNeural (晓晓)', value: 'zh-CN-XiaoxiaoNeural' },
        { label: 'zh-CN-YunxiNeural (云希)', value: 'zh-CN-YunxiNeural' },
        { label: 'zh-CN-YunyangNeural (云扬)', value: 'zh-CN-YunyangNeural' },
        { label: 'zh-CN-XiaoyiNeural (晓伊)', value: 'zh-CN-XiaoyiNeural' },
        { label: 'zh-CN-YunjianNeural (云健)', value: 'zh-CN-YunjianNeural' },
        { label: 'zh-CN-XiaochenNeural (晓辰)', value: 'zh-CN-XiaochenNeural' },
        { label: 'zh-CN-XiaohanNeural (晓涵)', value: 'zh-CN-XiaohanNeural' }
      ]
      edgeVoices.forEach(v => {
        if (!options.find(o => o.value === v.value)) {
          options.push(v)
        }
      })
      break
      
    case 'doubao':
    case 'doubao_ws':
      // 豆包TTS音色
      if (config.voice) {
        options.push({ label: config.voice, value: config.voice })
      }
      const doubaoVoices = [
        { label: 'Vivi 2.0（通用场景）', value: 'zh_female_vv_uranus_bigtts' },
        { label: '小何 2.0（通用场景）', value: 'zh_female_xiaohe_uranus_bigtts' },
        { label: '云舟 2.0（通用场景）', value: 'zh_male_m191_uranus_bigtts' },
        { label: '小天 2.0（通用场景）', value: 'zh_male_taocheng_uranus_bigtts' },
        { label: '团子 2.0（通用场景）', value: 'zh_female_tuanzi_uranus_bigtts' }
      ]
      doubaoVoices.forEach(v => {
        if (!options.find(o => o.value === v.value)) {
          options.push(v)
        }
      })
      break
      
    case 'cosyvoice':
      // CosyVoice 使用 spk_id
      if (config.spk_id) {
        options.push({ label: config.spk_id, value: config.spk_id })
      }
      const cosyVoices = [
        { label: '中文女', value: '中文女' },
        { label: '中文男', value: '中文男' },
        { label: '粤语女', value: '粤语女' },
        { label: '英文女', value: '英文女' },
        { label: '英文男', value: '英文男' },
        { label: '日语男', value: '日语男' },
        { label: '韩语女', value: '韩语女' }
      ]
      cosyVoices.forEach(v => {
        if (!options.find(o => o.value === v.value)) {
          options.push(v)
        }
      })
      break
      
    case 'minimax':
      // Minimax TTS 使用 voice
      if (config.voice) {
        options.push({ label: config.voice, value: config.voice })
      }
      const minimaxVoices = [
        { label: '青涩（男声）', value: 'male-qn-qingse' },
        { label: '青涩（女声）', value: 'female-qn-qingse' },
        { label: '少年（男声）', value: 'male-shaonian' },
        { label: '少年（女声）', value: 'female-shaonian' },
        { label: '成熟（男声）', value: 'male-chengshu' },
        { label: '成熟（女声）', value: 'female-chengshu' },
        { label: '温暖（男声）', value: 'male-wennuan' },
        { label: '温暖（女声）', value: 'female-wennuan' },
        { label: '清朗（男声）', value: 'male-qinglang' },
        { label: '清朗（女声）', value: 'female-qinglang' },
        { label: '厚重（男声）', value: 'male-houzhong' },
        { label: '厚重（女声）', value: 'female-houzhong' }
      ]
      minimaxVoices.forEach(v => {
        if (!options.find(o => o.value === v.value)) {
          options.push(v)
        }
      })
      break
      
    default:
      // 其他provider，尝试从配置中提取
      if (config.voice) {
        options.push({ label: config.voice, value: config.voice })
      }
      if (config.spk_id) {
        options.push({ label: config.spk_id, value: config.spk_id })
      }
  }
  
  return options
}

// 获取当前TTS配置名称
const getCurrentTtsConfigName = () => {
  if (!groupForm.tts_config_id) return ''
  const config = ttsConfigs.value.find(c => c.config_id === groupForm.tts_config_id)
  return config ? config.name : ''
}

// 获取当前TTS配置信息
const getCurrentTtsConfigInfo = () => {
  if (!groupForm.tts_config_id) return ''
  const config = ttsConfigs.value.find(c => c.config_id === groupForm.tts_config_id)
  if (!config) return ''
  return `TTS提供商: ${config.provider || '未知'}`
}

// 加载声纹组列表
const loadSpeakerGroups = async () => {
  try {
    loading.value = true
    const params = {}
    if (filterAgentId.value) {
      params.agent_id = filterAgentId.value
    }
    const response = await api.get('/user/speaker-groups', { params })
    speakerGroups.value = response.data.data || []
  } catch (error) {
    console.error('加载声纹组列表失败:', error)
    ElMessage.error('加载声纹组列表失败: ' + (error.response?.data?.error || error.message))
  } finally {
    loading.value = false
  }
}

// 搜索处理
const handleSearch = () => {
  // 搜索是客户端过滤，不需要重新请求
}

// 创建声纹组
const handleAddGroup = async () => {
  groupDialogMode.value = 'add'
  resetGroupForm()
  await loadCloneVoicePresets()
  showGroupDialog.value = true
}

// 编辑声纹组
const handleEditGroup = async (group) => {
  groupDialogMode.value = 'edit'
  currentGroup.value = group
  groupForm.agent_id = group.agent_id
  groupForm.name = group.name
  groupForm.prompt = group.prompt || ''
  groupForm.description = group.description || ''
  groupForm.tts_config_id = group.tts_config_id || null
  groupForm.voice = group.voice || null
  await loadCloneVoicePresets()
  
  // 如果有TTS配置，加载对应的音色选项
  if (groupForm.tts_config_id) {
    await handleTtsConfigChange(groupForm.tts_config_id)
  }
  
  showGroupDialog.value = true
}

// 提交声纹组
const handleSubmitGroup = async () => {
  if (!groupFormRef.value) return

  try {
    await groupFormRef.value.validate()
    submitting.value = true

    if (groupDialogMode.value === 'add') {
      const response = await api.post('/user/speaker-groups', groupForm)
      ElMessage.success('创建成功')
      showGroupDialog.value = false
      await loadSpeakerGroups()
    } else {
      const response = await api.put(`/user/speaker-groups/${currentGroup.value.id}`, groupForm)
      ElMessage.success('更新成功')
      showGroupDialog.value = false
      await loadSpeakerGroups()
    }
  } catch (error) {
    if (error.fields) {
      // 表单验证错误
      return
    }
    console.error('提交失败:', error)
    ElMessage.error('操作失败: ' + (error.response?.data?.error || error.message))
  } finally {
    submitting.value = false
  }
}

// 验证声纹组
const handleVerifyGroup = async (group) => {
  // 先清理之前的数据
  resetVerifyForm()
  
  // 等待 DOM 更新完成
  await nextTick()
  
  currentVerifyGroup.value = group
  verifyResult.value = null
  verifyMode.value = 'upload'
  showVerifyDialog.value = true
  
  // 再次确保清空上传组件
  await nextTick()
  verifyUploadRef.value?.clearFiles()
  verifyFileList.value = []
  
  // 检查浏览器是否支持录音
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    stream.getTracks().forEach(track => track.stop())
    canRecord.value = true
  } catch (error) {
    console.warn('浏览器不支持录音:', error)
    canRecord.value = false
    if (verifyMode.value === 'record') {
      ElMessage.warning('您的浏览器不支持录音功能，请使用上传文件方式')
      verifyMode.value = 'upload'
    }
  }
}

// 关闭验证对话框
const handleCloseVerifyDialog = () => {
  if (isVerifyRecording.value) {
    stopVerifyRecording()
  }
  resetVerifyForm()
  showVerifyDialog.value = false
}

// 验证文件变化处理
const handleVerifyFileChange = async (file, fileList) => {
  // 先清空文件列表，确保旧文件被移除
  verifyFileList.value = []
  await nextTick()
  
  // 如果已有文件，先清理之前的文件
  if (verifyForm.audioFile) {
    verifyForm.audioFile = null
    verifyForm.audio = null
  }
  
  // 清理录音相关
  if (verifyRecordedBlob.value) {
    if (verifyRecordedBlobUrl.value) {
      URL.revokeObjectURL(verifyRecordedBlobUrl.value)
      verifyRecordedBlobUrl.value = ''
    }
    verifyRecordedBlob.value = null
    verifyRecordTime.value = 0
  }
  
  // 清理验证结果
  verifyResult.value = null
  
  const fileObj = file.raw || file
  if (!fileObj) {
    ElMessage.warning('文件对象无效')
    verifyUploadRef.value?.clearFiles()
    verifyForm.audioFile = null
    verifyFileList.value = []
    return
  }

  // 验证文件类型
  const fileName = fileObj.name || file.name || ''
  const fileType = fileObj.type || file.type || ''
  if (!fileType.includes('wav') && !fileName.toLowerCase().endsWith('.wav')) {
    ElMessage.warning('只能上传 WAV 格式的音频文件')
    verifyUploadRef.value?.clearFiles()
    verifyForm.audioFile = null
    verifyFileList.value = []
    return
  }

  // 验证文件大小（10MB）
  const fileSize = fileObj.size || file.size || 0
  if (fileSize > 10 * 1024 * 1024) {
    ElMessage.warning('文件大小不能超过 10MB')
    verifyUploadRef.value?.clearFiles()
    verifyForm.audioFile = null
    verifyFileList.value = []
    return
  }

  // 设置新文件
  verifyForm.audioFile = file
  verifyForm.audio = file
  
  // 更新文件列表显示（只显示最新文件）
  verifyFileList.value = [file]
  
  await nextTick()

  if (verifyFormRef.value) {
    verifyFormRef.value.clearValidate('audio')
  }
}

// 验证文件移除处理
const handleVerifyFileRemove = () => {
  verifyForm.audioFile = null
  verifyForm.audio = null
  verifyFileList.value = []
  verifyResult.value = null // 清理验证结果
  if (verifyFormRef.value) {
    verifyFormRef.value.validateField('audio')
  }
}

// 开始验证录音
const startVerifyRecording = async () => {
  try {
    // 停止之前的录音（如果有）
    if (verifyMediaRecorder.value && verifyMediaRecorder.value.state !== 'inactive') {
      verifyMediaRecorder.value.stop()
    }

    // 清理之前的录音
    if (verifyRecordedBlobUrl.value) {
      URL.revokeObjectURL(verifyRecordedBlobUrl.value)
      verifyRecordedBlobUrl.value = ''
    }
    verifyRecordedBlob.value = null
    verifyRecordTime.value = 0

    // 获取麦克风权限
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        channelCount: 1,
        sampleRate: 16000,
        echoCancellation: true,
        noiseSuppression: true
      }
    })

    // 创建 MediaRecorder
    const chunks = []
    const options = {
      mimeType: 'audio/webm;codecs=opus'
    }

    if (!MediaRecorder.isTypeSupported(options.mimeType)) {
      verifyMediaRecorder.value = new MediaRecorder(stream)
    } else {
      verifyMediaRecorder.value = new MediaRecorder(stream, options)
    }

    verifyMediaRecorder.value.ondataavailable = (e) => {
      if (e.data.size > 0) {
        chunks.push(e.data)
      }
    }

    verifyMediaRecorder.value.onstop = async () => {
      stream.getTracks().forEach(track => track.stop())
      
      try {
        // 将录制的音频转换为 WAV 格式
        const blob = new Blob(chunks, { type: chunks[0]?.type || 'audio/webm' })
        const wavBlob = await convertToWav(blob)
        
        verifyRecordedBlob.value = wavBlob
        verifyRecordedBlobUrl.value = URL.createObjectURL(wavBlob)
        
        // 创建 File 对象用于上传
        const fileName = `verify_recording_${Date.now()}.wav`
        const file = new File([wavBlob], fileName, { type: 'audio/wav' })
        verifyForm.audioFile = { raw: file, name: fileName, size: wavBlob.size }
        verifyForm.audio = file

        if (verifyFormRef.value) {
          verifyFormRef.value.clearValidate('audio')
        }
      } catch (error) {
        console.error('处理录音数据失败:', error)
        ElMessage.error('处理录音数据失败，请重试')
        verifyRecordedBlob.value = null
        verifyRecordedBlobUrl.value = ''
        verifyForm.audioFile = null
        verifyForm.audio = null
      }

      chunks.length = 0
    }

    // 开始录制
    verifyMediaRecorder.value.start(100)
    isVerifyRecording.value = true

    // 开始计时
    verifyRecordTimer.value = setInterval(() => {
      verifyRecordTime.value += 0.1
    }, 100)

    ElMessage.success('开始录制')
  } catch (error) {
    console.error('录音失败:', error)
    ElMessage.error('录音失败: ' + error.message)
    canRecord.value = false
  }
}

// 停止验证录音
const stopVerifyRecording = () => {
  if (verifyMediaRecorder.value && verifyMediaRecorder.value.state !== 'inactive') {
    verifyMediaRecorder.value.stop()
  }
  isVerifyRecording.value = false
  
  if (verifyRecordTimer.value) {
    clearInterval(verifyRecordTimer.value)
    verifyRecordTimer.value = null
  }

  ElMessage.success('录制完成')
}

// 提交验证
const handleSubmitVerify = async () => {
  if (!verifyFormRef.value) return

  try {
    await verifyFormRef.value.validate()

    if (!verifyForm.audioFile && !verifyRecordedBlob.value) {
      ElMessage.warning('请上传或录制音频文件')
      return
    }

    verifying.value = true
    verifyResult.value = null

    let file
    if (verifyForm.audioFile) {
      // 使用上传的文件
      file = verifyForm.audioFile.raw || verifyForm.audioFile
    } else if (verifyRecordedBlob.value) {
      // 使用录制的音频
      const fileName = `verify_recording_${Date.now()}.wav`
      file = new File([verifyRecordedBlob.value], fileName, { type: 'audio/wav' })
    } else {
      ElMessage.warning('请上传或录制音频文件')
      return
    }

    const formData = new FormData()
    formData.append('audio', file)

    const response = await api.post(`/user/speaker-groups/${currentVerifyGroup.value.id}/verify`, formData)
    
    if (response.data.success && response.data.data) {
      verifyResult.value = {
        verified: response.data.data.verified,
        confidence: response.data.data.confidence,
        threshold: response.data.data.threshold,
        message: response.data.data.message
      }
      
      if (verifyResult.value.verified) {
        ElMessage.success('验证通过！')
      } else {
        ElMessage.warning('验证未通过')
      }
    } else {
      ElMessage.error('验证失败')
    }
  } catch (error) {
    if (error.fields) {
      return
    }
    console.error('验证失败:', error)
    ElMessage.error('验证失败: ' + (error.response?.data?.error || error.message))
  } finally {
    verifying.value = false
  }
}

// 重置验证表单
const resetVerifyForm = () => {
  if (verifyFormRef.value) {
    verifyFormRef.value.resetFields()
  }
  if (verifyUploadRef.value) {
    verifyUploadRef.value.clearFiles()
  }
  verifyForm.audioFile = null
  verifyForm.audio = null
  
  // 清理验证录音相关
  if (isVerifyRecording.value) {
    stopVerifyRecording()
  }
  if (verifyRecordedBlobUrl.value) {
    URL.revokeObjectURL(verifyRecordedBlobUrl.value)
    verifyRecordedBlobUrl.value = ''
  }
  verifyRecordedBlob.value = null
  verifyRecordTime.value = 0
  verifyMode.value = 'upload'
  verifyResult.value = null
}

// 计算是否有验证音频文件
const hasVerifyAudioFile = computed(() => {
  return verifyForm.audioFile !== null || verifyRecordedBlob.value !== null
})

// 删除声纹组
const handleDeleteGroup = async (group) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除声纹组"${group.name}"吗？此操作将删除该组下的所有样本，且不可恢复。`,
      '确认删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    loading.value = true
    await api.delete(`/user/speaker-groups/${group.id}`)
    ElMessage.success('删除成功')
    await loadSpeakerGroups()
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除失败:', error)
      ElMessage.error('删除失败: ' + (error.response?.data?.error || error.message))
    }
  } finally {
    loading.value = false
  }
}

// 查看样本
const handleViewSamples = async (group) => {
  currentGroup.value = group
  showSampleDrawer.value = true
  await loadSamples(group.id)
}

// 从样本管理弹层中验证声纹组
const handleVerifyFromSamples = () => {
  if (currentGroup.value) {
    showSampleDrawer.value = false
    handleVerifyGroup(currentGroup.value)
  }
}

// 加载样本列表
const loadSamples = async (groupId) => {
  try {
    const response = await api.get(`/user/speaker-groups/${groupId}/samples`)
    samples.value = response.data.data || []
  } catch (error) {
    console.error('加载样本列表失败:', error)
    ElMessage.error('加载样本列表失败')
  }
}

// 关闭样本弹层
const handleCloseSampleDrawer = () => {
  showSampleDrawer.value = false
  currentGroup.value = null
  samples.value = []
}

// 添加样本
const handleAddSample = async () => {
  resetUploadForm()
  uploadMode.value = 'history'
  showUploadDialog.value = true
  
  // 初始化历史记录表单
  historyForm.agent_id = currentGroup.value?.agent_id || null
  historyForm.selected_message_id = null
  historyMessages.value = []
  
  // 如果声纹组有关联的智能体，自动加载历史记录
  if (currentGroup.value?.agent_id) {
    historyForm.agent_id = currentGroup.value.agent_id
    await loadHistoryMessages()
  }
  
  // 检查浏览器是否支持录音
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    stream.getTracks().forEach(track => track.stop())
    canRecord.value = true
  } catch (error) {
    console.warn('浏览器不支持录音:', error)
    canRecord.value = false
    if (uploadMode.value === 'record') {
      ElMessage.warning('您的浏览器不支持录音功能，请使用上传文件方式')
      uploadMode.value = 'upload'
    }
  }
}

// 关闭上传对话框
const handleCloseUploadDialog = () => {
  if (isRecording.value) {
    stopRecording()
  }
  resetUploadForm()
  showUploadDialog.value = false
}

// 文件变化处理
const handleFileChange = (file) => {
  const fileObj = file.raw || file
  if (!fileObj) {
    ElMessage.warning('文件对象无效')
    uploadRef.value?.clearFiles()
    uploadForm.audioFile = null
      return
    }

  // 验证文件类型
  const fileName = fileObj.name || file.name || ''
  const fileType = fileObj.type || file.type || ''
  if (!fileType.includes('wav') && !fileName.toLowerCase().endsWith('.wav')) {
    ElMessage.warning('只能上传 WAV 格式的音频文件')
    uploadRef.value?.clearFiles()
    uploadForm.audioFile = null
    return
  }

  // 验证文件大小（10MB）
  const fileSize = fileObj.size || file.size || 0
  if (fileSize > 10 * 1024 * 1024) {
    ElMessage.warning('文件大小不能超过 10MB')
    uploadRef.value?.clearFiles()
    uploadForm.audioFile = null
    return
  }

  uploadForm.audioFile = file
  uploadForm.audio = file

  if (uploadFormRef.value) {
    uploadFormRef.value.clearValidate('audio')
  }
}

// 文件移除处理
const handleFileRemove = () => {
  uploadForm.audioFile = null
  uploadForm.audio = null
  if (uploadFormRef.value) {
    uploadFormRef.value.validateField('audio')
  }
}

// 开始录音
const startRecording = async () => {
  try {
    // 停止之前的录音（如果有）
    if (mediaRecorder.value && mediaRecorder.value.state !== 'inactive') {
      mediaRecorder.value.stop()
    }

    // 清理之前的录音
    if (recordedBlobUrl.value) {
      URL.revokeObjectURL(recordedBlobUrl.value)
      recordedBlobUrl.value = ''
    }
    recordedBlob.value = null
    recordTime.value = 0

    // 获取麦克风权限
    const stream = await navigator.mediaDevices.getUserMedia({
      audio: {
        channelCount: 1,
        sampleRate: 16000,
        echoCancellation: true,
        noiseSuppression: true
      }
    })

    // 创建 MediaRecorder（使用 WAV 格式）
    const chunks = []
    const options = {
      mimeType: 'audio/webm;codecs=opus' // 先录制为 webm，然后转换为 WAV
    }

    // 检查浏览器支持
    if (!MediaRecorder.isTypeSupported(options.mimeType)) {
      // 如果不支持，使用默认格式
      mediaRecorder.value = new MediaRecorder(stream)
      } else {
      mediaRecorder.value = new MediaRecorder(stream, options)
    }

    mediaRecorder.value.ondataavailable = (e) => {
      if (e.data.size > 0) {
        chunks.push(e.data)
      }
    }

    mediaRecorder.value.onstop = async () => {
      stream.getTracks().forEach(track => track.stop())
      
      try {
        // 将录制的音频转换为 WAV 格式
        const blob = new Blob(chunks, { type: chunks[0]?.type || 'audio/webm' })
        const wavBlob = await convertToWav(blob)
        
        recordedBlob.value = wavBlob
        recordedBlobUrl.value = URL.createObjectURL(wavBlob)
        
        // 创建 File 对象用于上传
        const fileName = `recording_${Date.now()}.wav`
        const file = new File([wavBlob], fileName, { type: 'audio/wav' })
        uploadForm.audioFile = { raw: file, name: fileName, size: wavBlob.size }
        uploadForm.audio = file

        if (uploadFormRef.value) {
          uploadFormRef.value.clearValidate('audio')
        }
      } catch (error) {
        console.error('处理录音数据失败:', error)
        ElMessage.error('处理录音数据失败，请重试')
        recordedBlob.value = null
        recordedBlobUrl.value = ''
        uploadForm.audioFile = null
        uploadForm.audio = null
      }

      chunks.length = 0
    }

    // 开始录制
    mediaRecorder.value.start(100) // 每100ms收集一次数据
    isRecording.value = true

    // 开始计时
    recordTimer.value = setInterval(() => {
      recordTime.value += 0.1
    }, 100)

    ElMessage.success('开始录制')
  } catch (error) {
    console.error('录音失败:', error)
    ElMessage.error('录音失败: ' + error.message)
    canRecord.value = false
  }
}

// 停止录音
const stopRecording = () => {
  if (mediaRecorder.value && mediaRecorder.value.state !== 'inactive') {
    mediaRecorder.value.stop()
  }
  isRecording.value = false
  
  if (recordTimer.value) {
    clearInterval(recordTimer.value)
    recordTimer.value = null
  }

  ElMessage.success('录制完成')
}

// 将音频转换为 WAV 格式
const convertToWav = async (blob) => {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = async (e) => {
      try {
        const audioContext = new (window.AudioContext || window.webkitAudioContext)()
        const arrayBuffer = e.target.result
        const audioBuffer = await audioContext.decodeAudioData(arrayBuffer)
        
        // 转换为 WAV
        const wav = audioBufferToWav(audioBuffer)
        const wavBlob = new Blob([wav], { type: 'audio/wav' })
        resolve(wavBlob)
      } catch (error) {
        console.error('转换 WAV 失败:', error)
        // 如果转换失败，直接使用原始 blob（可能需要后端支持 webm 格式）
        reject(error)
      }
    }
    reader.onerror = reject
    reader.readAsArrayBuffer(blob)
  })
}

// 将 AudioBuffer 转换为 WAV 格式
const audioBufferToWav = (buffer) => {
  const length = buffer.length
  const numberOfChannels = buffer.numberOfChannels
  const sampleRate = buffer.sampleRate
  const bytesPerSample = 2
  const blockAlign = numberOfChannels * bytesPerSample
  const byteRate = sampleRate * blockAlign
  const dataSize = length * blockAlign
  const bufferSize = 44 + dataSize

  const arrayBuffer = new ArrayBuffer(bufferSize)
  const view = new DataView(arrayBuffer)

  // WAV 文件头
  const writeString = (offset, string) => {
    for (let i = 0; i < string.length; i++) {
      view.setUint8(offset + i, string.charCodeAt(i))
    }
  }

  writeString(0, 'RIFF')
  view.setUint32(4, bufferSize - 8, true)
  writeString(8, 'WAVE')
  writeString(12, 'fmt ')
  view.setUint32(16, 16, true) // fmt chunk size
  view.setUint16(20, 1, true) // audio format (PCM)
  view.setUint16(22, numberOfChannels, true)
  view.setUint32(24, sampleRate, true)
  view.setUint32(28, byteRate, true)
  view.setUint16(32, blockAlign, true)
  view.setUint16(34, 16, true) // bits per sample
  writeString(36, 'data')
  view.setUint32(40, dataSize, true)

  // 写入音频数据
  let offset = 44
  for (let i = 0; i < length; i++) {
    for (let channel = 0; channel < numberOfChannels; channel++) {
      const sample = Math.max(-1, Math.min(1, buffer.getChannelData(channel)[i]))
      view.setInt16(offset, sample < 0 ? sample * 0x8000 : sample * 0x7FFF, true)
      offset += 2
    }
  }

  return arrayBuffer
}

// 格式化录音时长
const formatRecordTime = (seconds) => {
  const mins = Math.floor(seconds / 60)
  const secs = Math.floor(seconds % 60)
  const ms = Math.floor((seconds % 1) * 10)
  return `${mins.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}.${ms}`
}

// 加载历史聊天记录
const loadHistoryMessages = async () => {
  if (!historyForm.agent_id) {
    historyMessages.value = []
    return
  }

  try {
    loadingHistory.value = true
    const response = await api.get('/user/history/messages', {
      params: {
        agent_id: historyForm.agent_id,
        role: 'user',
        page: 1,
        page_size: 50
      }
    })
    
    // 只显示有音频的消息
    historyMessages.value = (response.data.data || []).filter(msg => msg.audio_path)
  } catch (error) {
    console.error('加载历史聊天记录失败:', error)
    ElMessage.error('加载历史聊天记录失败: ' + (error.response?.data?.error || error.message))
    historyMessages.value = []
  } finally {
    loadingHistory.value = false
  }
}

// 选择历史消息
const handleSelectHistoryMessage = (row) => {
  historyForm.selected_message_id = row.message_id
}

// 试听历史音频
const handlePreviewHistoryAudio = async (message) => {
  try {
    const response = await api.get(`/user/history/messages/${message.id}/audio`, {
      responseType: 'blob'
    })
    
    const blob = new Blob([response.data], { type: 'audio/wav' })
    const blobUrl = URL.createObjectURL(blob)
    
    audioPlayer.value.src = blobUrl
    audioPlayer.value.play().catch(err => {
      console.error('播放失败:', err)
      ElMessage.warning('播放失败，请检查音频文件')
    })
    
    audioPlayer.value.onended = () => {
      URL.revokeObjectURL(blobUrl)
    }
  } catch (error) {
    console.error('试听失败:', error)
    ElMessage.error('试听失败: ' + (error.response?.data?.error || error.message))
  }
}

// 提交样本
const handleSubmitSample = async () => {
  if (uploadMode.value === 'history') {
    // 从历史记录中选择
    if (!historyForm.selected_message_id) {
      ElMessage.warning('请选择一条历史聊天记录')
      return
    }

    try {
      submitting.value = true
      const formData = new FormData()
      formData.append('message_id', historyForm.selected_message_id)

      await api.post(`/user/speaker-groups/${currentGroup.value.id}/samples`, formData)
      ElMessage.success('添加成功')
      handleCloseUploadDialog()
      await loadSamples(currentGroup.value.id)
      await loadSpeakerGroups() // 刷新列表以更新样本数量
    } catch (error) {
      console.error('添加失败:', error)
      ElMessage.error('添加失败: ' + (error.response?.data?.error || error.message))
    } finally {
      submitting.value = false
    }
    return
  }

  // 原有的上传/录制逻辑
  if (!uploadFormRef.value) return

  try {
    await uploadFormRef.value.validate()

    if (!uploadForm.audioFile && !recordedBlob.value) {
      ElMessage.warning('请上传或录制音频文件')
      return
    }

    submitting.value = true

    let file
    if (uploadForm.audioFile) {
      // 使用上传的文件
      file = uploadForm.audioFile.raw || uploadForm.audioFile
    } else if (recordedBlob.value) {
      // 使用录制的音频
      const fileName = `recording_${Date.now()}.wav`
      file = new File([recordedBlob.value], fileName, { type: 'audio/wav' })
    } else {
      ElMessage.warning('请上传或录制音频文件')
      return
    }

    const formData = new FormData()
    formData.append('audio', file)

    await api.post(`/user/speaker-groups/${currentGroup.value.id}/samples`, formData)
    ElMessage.success('上传成功')
    handleCloseUploadDialog()
    await loadSamples(currentGroup.value.id)
    await loadSpeakerGroups() // 刷新列表以更新样本数量
  } catch (error) {
    if (error.fields) {
      return
    }
    console.error('上传失败:', error)
    ElMessage.error('上传失败: ' + (error.response?.data?.error || error.message))
  } finally {
    submitting.value = false
  }
}

// 播放样本
const handlePlaySample = async (sample) => {
  try {
    // 构建音频文件URL（需要后端提供文件访问接口）
    // 使用 api.get 获取文件，然后创建 blob URL
    const response = await api.get(
      `/user/speaker-groups/${currentGroup.value.id}/samples/${sample.id}/file`,
      {
        responseType: 'blob'
      }
    )
    
    // 创建 blob URL
    const blob = new Blob([response.data], { type: 'audio/wav' })
    const blobUrl = URL.createObjectURL(blob)
    
    audioPlayer.value.src = blobUrl
    audioPlayer.value.play().catch(err => {
      console.error('播放失败:', err)
      ElMessage.warning('播放失败，请检查音频文件')
    })
    
    // 播放结束后清理 blob URL
    audioPlayer.value.onended = () => {
      URL.revokeObjectURL(blobUrl)
    }
  } catch (error) {
    console.error('播放失败:', error)
    ElMessage.error('播放失败: ' + (error.response?.data?.error || error.message))
  }
}

// 下载样本
const handleDownloadSample = async (sample) => {
  try {
    // 使用 api.get 获取文件，然后创建下载链接
    const response = await api.get(
      `/user/speaker-groups/${currentGroup.value.id}/samples/${sample.id}/file`,
      {
        responseType: 'blob'
      }
    )
    
    // 创建 blob URL 并下载
    const blob = new Blob([response.data], { type: 'audio/wav' })
    const blobUrl = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = blobUrl
    link.download = sample.file_name || 'audio.wav'
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    
    // 清理 blob URL
    setTimeout(() => {
      URL.revokeObjectURL(blobUrl)
    }, 100)
  } catch (error) {
    console.error('下载失败:', error)
    ElMessage.error('下载失败: ' + (error.response?.data?.error || error.message))
  }
}

// 删除样本
const handleDeleteSample = async (sample) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除样本"${sample.file_name}"吗？此操作不可恢复。`,
      '确认删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    await api.delete(`/user/speaker-groups/${currentGroup.value.id}/samples/${sample.id}`)
    ElMessage.success('删除成功')
    await loadSamples(currentGroup.value.id)
    await loadSpeakerGroups() // 刷新列表以更新样本数量
  } catch (error) {
    if (error !== 'cancel') {
      console.error('删除失败:', error)
      ElMessage.error('删除失败: ' + (error.response?.data?.error || error.message))
    }
  }
}

// 复制到剪贴板
const copyToClipboard = async (text) => {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制到剪贴板')
  } catch (error) {
    console.error('复制失败:', error)
    ElMessage.error('复制失败')
  }
}

// 重置表单
const resetGroupForm = () => {
  if (groupFormRef.value) {
    groupFormRef.value.resetFields()
  }
  Object.assign(groupForm, {
    agent_id: null,
    name: '',
    prompt: '',
    description: '',
    tts_config_id: null,
    voice: null
  })
  currentGroup.value = null
  currentVoiceOptions.value = []
}

const resetUploadForm = () => {
  if (uploadFormRef.value) {
    uploadFormRef.value.resetFields()
  }
  if (uploadRef.value) {
    uploadRef.value.clearFiles()
  }
  uploadForm.audioFile = null
  uploadForm.audio = null
  
  // 清理录音相关
  if (isRecording.value) {
    stopRecording()
  }
  if (recordedBlobUrl.value) {
    URL.revokeObjectURL(recordedBlobUrl.value)
    recordedBlobUrl.value = ''
  }
  recordedBlob.value = null
  recordTime.value = 0
  uploadMode.value = 'history'
  
  // 清理历史记录相关
  historyForm.agent_id = null
  historyForm.selected_message_id = null
  historyMessages.value = []
}

// 格式化日期
const formatDate = (dateString) => {
  if (!dateString) return '-'
  return new Date(dateString).toLocaleString('zh-CN')
}

// 截断ID显示
const truncateId = (id) => {
  if (!id) return '-'
  if (id.length > 20) {
    return id.substring(0, 10) + '...' + id.substring(id.length - 10)
  }
  return id
}

// 截断文本
const truncateText = (text, maxLength) => {
  if (!text) return '-'
  if (text.length <= maxLength) return text
  return text.substring(0, maxLength) + '...'
}

// 格式化文件大小
const formatFileSize = (bytes) => {
  if (!bytes) return '0 B'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(2) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(2) + ' MB'
}

onMounted(() => {
  loadAgents()
  loadSpeakerGroups()
  loadTtsConfigs()
  loadCloneVoicePresets()
})

// 组件卸载前清理资源
onBeforeUnmount(() => {
  if (isRecording.value) {
    stopRecording()
  }
  if (recordedBlobUrl.value) {
    URL.revokeObjectURL(recordedBlobUrl.value)
  }
  if (recordTimer.value) {
    clearInterval(recordTimer.value)
  }
  if (mediaRecorder.value && mediaRecorder.value.state !== 'inactive') {
    mediaRecorder.value.stop()
  }
})
</script>

<style scoped>
.speakers-page {
  padding: 0;
}

.filter-bar {
  padding: 15px 20px;
  background: rgba(255, 255, 255, 0.88);
  border-radius: 8px;
  margin-bottom: 20px;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.create-group-button {
  margin-left: auto;
}

.speakers-content {
  background: rgba(255, 255, 255, 0.88);
  border-radius: 8px;
  padding: 20px;
}

.prompt-text {
  color: #606266;
  cursor: pointer;
}

.prompt-popover {
  max-height: 200px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-word;
}

.text-muted {
  color: #909399;
}

.uuid-text {
  font-family: monospace;
  font-size: 12px;
}

.empty-state {
  padding: 40px 0;
}

.sample-drawer {
  padding: 20px;
}

.group-info-card {
  margin-bottom: 20px;
}

.group-info h3 {
  margin: 0 0 15px 0;
  color: #303133;
}

.prompt-section,
.description-section {
  margin-top: 15px;
  padding-top: 15px;
  border-top: 1px solid #f0f0f0;
}

.prompt-section strong,
.description-section strong {
  display: block;
  margin-bottom: 8px;
  color: #606266;
}

.prompt-section p,
.description-section p {
  margin: 0;
  color: #303133;
  white-space: pre-wrap;
  word-break: break-word;
}

.samples-section {
  margin-top: 20px;
}

.samples-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 15px;
}

.samples-header h4 {
  margin: 0;
  color: #303133;
}

.empty-samples {
  padding: 40px 0;
}

.file-info {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
  padding: 8px 12px;
  background: rgba(248, 250, 252, 0.92);
  border: 1px solid rgba(229, 229, 234, 0.72);
  border-radius: 12px;
  font-size: 14px;
  color: var(--apple-text-secondary);
}

.file-size {
  color: #909399;
  font-size: 12px;
}

.clone-voice-line {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  width: 100%;
}

.clone-voice-item {
  display: inline-flex;
  align-items: center;
  max-width: 220px;
  min-width: 0;
  padding: 4px 10px;
  border: 1px solid #d1d5db;
  border-radius: 999px;
  background: #f8fafc;
  color: #374151;
  cursor: pointer;
  transition: all 0.2s ease;
  line-height: 1.2;
  outline: none;
}

.clone-voice-item:hover {
  border-color: #93c5fd;
  background: #f1f7ff;
}

.clone-voice-item.active {
  border-color: #3b82f6;
  background: #e9f2ff;
  color: #1d4ed8;
  box-shadow: 0 0 0 1px rgba(59, 130, 246, 0.1);
}

.clone-voice-name {
  font-size: 12px;
  font-weight: 500;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

:deep(.el-upload-dragger) {
  width: 100%;
}

.action-buttons {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.action-buttons .el-button {
  margin: 0;
  white-space: nowrap;
}

/* 上传对话框样式 */
.upload-tabs {
  margin-top: 10px;
}

.audio-upload {
  width: 100%;
}

.audio-upload :deep(.el-upload-dragger) {
  width: 100%;
  padding: 40px 20px;
}

.audio-upload :deep(.el-icon--upload) {
  font-size: 48px;
  color: var(--apple-primary);
  margin-bottom: 16px;
}

.audio-upload :deep(.el-upload__text) {
  font-size: 14px;
  color: #606266;
}

.audio-upload :deep(.el-upload__text em) {
  color: var(--apple-primary);
  font-style: normal;
}

.audio-upload :deep(.el-upload__tip) {
  margin-top: 12px;
  font-size: 12px;
  color: #909399;
}

/* 录音区域样式 */
.record-section {
  padding: 20px 0;
}

.record-status {
  min-height: 200px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 30px;
  background: #f5f7fa;
  border-radius: 8px;
  margin-bottom: 20px;
}

.record-ready,
.record-complete {
  text-align: center;
}

.record-ready p,
.record-complete p {
  margin: 12px 0 0 0;
  color: #303133;
  font-size: 16px;
}

.record-tip {
  margin-top: 8px !important;
  font-size: 14px !important;
  color: #909399 !important;
}

.record-recording {
  text-align: center;
}

.recording-indicator {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  margin-bottom: 16px;
}

.recording-dot {
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--apple-danger);
  animation: pulse 1.5s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.5;
    transform: scale(1.2);
  }
}

.recording-text {
  font-size: 16px;
  color: var(--apple-danger);
  font-weight: 500;
}

.record-time {
  font-size: 32px;
  font-weight: 600;
  color: #303133;
  font-family: 'Courier New', monospace;
  margin: 20px 0;
}

.record-preview {
  width: 100%;
  max-width: 400px;
  margin-top: 20px;
}

.record-controls {
  display: flex;
  justify-content: center;
  gap: 12px;
}

.record-controls .el-button {
  min-width: 120px;
}

/* 历史记录区域样式 */
.history-section {
  padding: 20px 0;
}

.history-list {
  margin-top: 20px;
}

.empty-history {
  padding: 40px 0;
}

.message-content {
  max-width: 300px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.history-list :deep(.el-table__row) {
  cursor: pointer;
}

.history-list :deep(.el-table__row:hover) {
  background-color: #f5f7fa;
}
</style>
