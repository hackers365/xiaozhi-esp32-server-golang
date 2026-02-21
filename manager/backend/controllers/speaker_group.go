package controllers

import (
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"xiaozhi/manager/backend/config"
	"xiaozhi/manager/backend/models"
	"xiaozhi/manager/backend/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SpeakerGroupController 声纹组控制器
type SpeakerGroupController struct {
	DB             *gorm.DB
	SpeakerService speakerServiceClient
	AudioStorage   *storage.AudioStorage
	HistoryConfig  *config.HistoryConfig // 历史聊天记录配置
}

// NewSpeakerGroupController 创建声纹组控制器
func NewSpeakerGroupController(db *gorm.DB, cfg *config.Config) *SpeakerGroupController {
	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	speakerService, err := newSpeakerServiceClient(cfg.SpeakerService, httpClient)
	if err != nil {
		log.Printf("初始化 speaker_service 失败 (mode=%s): %v", cfg.SpeakerService.Mode, err)
	}

	audioStorage := storage.NewAudioStorage(
		cfg.Storage.SpeakerAudioPath,
		cfg.Storage.MaxFileSize,
	)

	return &SpeakerGroupController{
		DB:             db,
		SpeakerService: speakerService,
		AudioStorage:   audioStorage,
		HistoryConfig:  &cfg.History,
	}
}

// CreateSpeakerGroup 创建声纹组
func (sgc *SpeakerGroupController) CreateSpeakerGroup(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	var req struct {
		AgentID     uint    `json:"agent_id" binding:"required"`
		Name        string  `json:"name" binding:"required,min=1,max=100"`
		Prompt      string  `json:"prompt"`
		Description string  `json:"description"`
		TTSConfigID *string `json:"tts_config_id"`
		Voice       *string `json:"voice"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	// 验证智能体是否存在且属于当前用户
	var agent models.Agent
	if err := sgc.DB.Where("id = ? AND user_id = ?", req.AgentID, userID).First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusBadRequest, gin.H{"error": "智能体不存在或无权限访问"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询智能体失败"})
		return
	}

	// 检查同一用户下是否已存在相同名称的声纹组
	var existingGroup models.SpeakerGroup
	if err := sgc.DB.Where("user_id = ? AND name = ?", userID, req.Name).First(&existingGroup).Error; err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该声纹组名称已存在，请使用其他名称"})
		return
	} else if err != gorm.ErrRecordNotFound {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
		return
	}

	// 创建声纹组
	speakerGroup := models.SpeakerGroup{
		UserID:      userID.(uint),
		AgentID:     req.AgentID,
		Name:        req.Name,
		Prompt:      req.Prompt,
		Description: req.Description,
		TTSConfigID: req.TTSConfigID,
		Voice:       req.Voice,
		Status:      "active",
		SampleCount: 0,
	}

	if err := sgc.DB.Create(&speakerGroup).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建声纹组失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": gin.H{
			"id":           speakerGroup.ID,
			"agent_id":     speakerGroup.AgentID,
			"name":         speakerGroup.Name,
			"prompt":       speakerGroup.Prompt,
			"description":  speakerGroup.Description,
			"sample_count": speakerGroup.SampleCount,
			"created_at":   speakerGroup.CreatedAt,
		},
	})
}

// GetSpeakerGroups 获取声纹组列表
func (sgc *SpeakerGroupController) GetSpeakerGroups(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	// 获取查询参数
	agentIDStr := c.Query("agent_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}

	offset := (page - 1) * pageSize

	// 构建查询
	query := sgc.DB.Model(&models.SpeakerGroup{}).Where("user_id = ?", userID)

	// 按智能体过滤
	if agentIDStr != "" {
		agentID, err := strconv.ParseUint(agentIDStr, 10, 32)
		if err == nil {
			query = query.Where("agent_id = ?", uint(agentID))
		}
	}

	// 获取总数
	var total int64
	query.Count(&total)

	// 获取数据
	var speakerGroups []models.SpeakerGroup
	if err := query.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&speakerGroups).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
		return
	}

	// 获取智能体信息（用于显示智能体名称）
	agentIDs := make([]uint, 0)
	for _, sg := range speakerGroups {
		agentIDs = append(agentIDs, sg.AgentID)
	}

	var agents []models.Agent
	if len(agentIDs) > 0 {
		sgc.DB.Where("id IN ?", agentIDs).Find(&agents)
	}

	agentMap := make(map[uint]string)
	for _, agent := range agents {
		agentMap[agent.ID] = agent.Name
	}

	// 构建响应
	result := make([]gin.H, 0)
	for _, sg := range speakerGroups {
		result = append(result, gin.H{
			"id":            sg.ID,
			"agent_id":      sg.AgentID,
			"agent_name":    agentMap[sg.AgentID],
			"name":          sg.Name,
			"prompt":        sg.Prompt,
			"description":   sg.Description,
			"tts_config_id": sg.TTSConfigID,
			"voice":         sg.Voice,
			"sample_count":  sg.SampleCount,
			"created_at":    sg.CreatedAt,
			"updated_at":    sg.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  result,
		"total": total,
	})
}

// GetSpeakerGroup 获取声纹组详情（包含样本列表）
func (sgc *SpeakerGroupController) GetSpeakerGroup(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	id := c.Param("id")
	speakerGroupID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的声纹组ID"})
		return
	}

	// 查询声纹组
	var speakerGroup models.SpeakerGroup
	if err := sgc.DB.Where("id = ? AND user_id = ?", speakerGroupID, userID).First(&speakerGroup).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "声纹组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
		return
	}

	// 查询智能体信息
	var agent models.Agent
	sgc.DB.Where("id = ?", speakerGroup.AgentID).First(&agent)

	// 查询样本列表
	var samples []models.SpeakerSample
	sgc.DB.Where("speaker_group_id = ?", speakerGroupID).Order("created_at DESC").Find(&samples)

	// 构建样本响应
	sampleList := make([]gin.H, 0)
	for _, sample := range samples {
		sampleList = append(sampleList, gin.H{
			"id":         sample.ID,
			"uuid":       sample.UUID,
			"file_name":  sample.FileName,
			"file_size":  sample.FileSize,
			"duration":   sample.Duration,
			"file_path":  sample.FilePath,
			"created_at": sample.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"id":            speakerGroup.ID,
			"agent_id":      speakerGroup.AgentID,
			"agent_name":    agent.Name,
			"name":          speakerGroup.Name,
			"prompt":        speakerGroup.Prompt,
			"description":   speakerGroup.Description,
			"tts_config_id": speakerGroup.TTSConfigID,
			"voice":         speakerGroup.Voice,
			"sample_count":  speakerGroup.SampleCount,
			"samples":       sampleList,
			"created_at":    speakerGroup.CreatedAt,
		},
	})
}

// UpdateSpeakerGroup 更新声纹组
func (sgc *SpeakerGroupController) UpdateSpeakerGroup(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	id := c.Param("id")
	speakerGroupID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的声纹组ID"})
		return
	}

	var req struct {
		AgentID     *uint   `json:"agent_id"`
		Name        string  `json:"name"`
		Prompt      string  `json:"prompt"`
		Description string  `json:"description"`
		TTSConfigID *string `json:"tts_config_id"`
		Voice       *string `json:"voice"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	// 查询声纹组
	var speakerGroup models.SpeakerGroup
	if err := sgc.DB.Where("id = ? AND user_id = ?", speakerGroupID, userID).First(&speakerGroup).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "声纹组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
		return
	}

	// 如果更新了智能体ID，需要验证新智能体是否存在
	if req.AgentID != nil && *req.AgentID != speakerGroup.AgentID {
		var agent models.Agent
		if err := sgc.DB.Where("id = ? AND user_id = ?", *req.AgentID, userID).First(&agent).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusBadRequest, gin.H{"error": "智能体不存在或无权限访问"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询智能体失败"})
			return
		}
		speakerGroup.AgentID = *req.AgentID
	}

	// 更新字段
	if req.Name != "" && req.Name != speakerGroup.Name {
		// 检查同一用户下是否已存在相同名称的声纹组（排除当前声纹组）
		var existingGroup models.SpeakerGroup
		if err := sgc.DB.Where("user_id = ? AND name = ? AND id != ?", userID, req.Name, speakerGroupID).First(&existingGroup).Error; err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该声纹组名称已存在，请使用其他名称"})
			return
		} else if err != gorm.ErrRecordNotFound {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
			return
		}
		speakerGroup.Name = req.Name
	}
	if req.Prompt != "" {
		speakerGroup.Prompt = req.Prompt
	}
	speakerGroup.Description = req.Description // 允许清空描述
	speakerGroup.TTSConfigID = req.TTSConfigID
	speakerGroup.Voice = req.Voice

	if err := sgc.DB.Save(&speakerGroup).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新声纹组失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    speakerGroup,
	})
}

// DeleteSpeakerGroup 删除声纹组
func (sgc *SpeakerGroupController) DeleteSpeakerGroup(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	id := c.Param("id")
	speakerGroupID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的声纹组ID"})
		return
	}

	// 查询声纹组
	var speakerGroup models.SpeakerGroup
	if err := sgc.DB.Where("id = ? AND user_id = ?", speakerGroupID, userID).First(&speakerGroup).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "声纹组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
		return
	}

	// 查询所有样本（用于删除本地文件和数据库记录）
	var samples []models.SpeakerSample
	sgc.DB.Where("speaker_group_id = ?", speakerGroupID).Find(&samples)

	// 调用 asr_server 删除接口（通过 speaker_id，即声纹组的主键 ID，一次性删除所有样本）
	err = sgc.callDeleteAPI(fmt.Sprintf("%d", speakerGroup.ID), speakerGroup.AgentID, userID)
	if err != nil {
		log.Printf("asr_server 删除声纹组失败 (speaker_id: %d): %v", speakerGroup.ID, err)
		// 继续执行本地删除，不中断流程
	}

	// 删除所有样本的本地文件和数据库记录
	for _, sample := range samples {
		// 删除本地文件
		sgc.AudioStorage.DeleteAudioFile(sample.FilePath)

		// 删除数据库记录
		sgc.DB.Delete(&sample)
	}

	// 删除声纹组
	if err := sgc.DB.Delete(&speakerGroup).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除声纹组失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "声纹组删除成功",
	})
}

// AddSample 添加声纹样本
func (sgc *SpeakerGroupController) AddSample(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	groupIDStr := c.Param("id") // 改为使用 :id 参数
	groupID, err := strconv.ParseUint(groupIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的声纹组ID"})
		return
	}

	// 验证声纹组是否存在且属于当前用户
	var speakerGroup models.SpeakerGroup
	if err := sgc.DB.Where("id = ? AND user_id = ?", groupID, userID).First(&speakerGroup).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "声纹组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
		return
	}

	var file multipart.File
	var header *multipart.FileHeader
	var fileName string

	// 检查是否从历史聊天记录中获取音频
	messageID := c.PostForm("message_id")
	if messageID != "" {
		// 从历史聊天记录中获取音频
		var chatMessage models.ChatMessage
		if err := sgc.DB.Where("message_id = ? AND user_id = ? AND role = ? AND is_deleted = ?",
			messageID, userID, "user", false).First(&chatMessage).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "历史聊天记录不存在或不是用户消息"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询历史聊天记录失败"})
			return
		}

		if chatMessage.AudioPath == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该消息没有音频数据"})
			return
		}

		// 读取音频文件
		audioBasePath := sgc.HistoryConfig.AudioBasePath
		if audioBasePath == "" {
			audioBasePath = "./storage/chat_history/audio"
		}
		fullPath := filepath.Join(audioBasePath, chatMessage.AudioPath)

		audioData, err := os.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				c.JSON(http.StatusNotFound, gin.H{"error": "音频文件不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取音频文件失败: " + err.Error()})
			return
		}

		// 创建临时文件用于 multipart
		tempFile, err := os.CreateTemp("", "audio_*.wav")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建临时文件失败: " + err.Error()})
			return
		}
		defer os.Remove(tempFile.Name()) // 清理临时文件
		defer tempFile.Close()

		if _, err := tempFile.Write(audioData); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "写入临时文件失败: " + err.Error()})
			return
		}
		tempFile.Seek(0, 0)

		// 创建 multipart.File 和 FileHeader
		file = tempFile
		fileInfo, _ := tempFile.Stat()
		header = &multipart.FileHeader{
			Filename: fmt.Sprintf("history_%s.wav", messageID),
			Size:     fileInfo.Size(),
		}
		fileName = header.Filename
	} else {
		// 从上传的文件中获取音频
		file, header, err = c.Request.FormFile("audio")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "音频文件缺失: " + err.Error()})
			return
		}
		defer file.Close()
		fileName = header.Filename
	}

	// 生成 UUID
	sampleUUID := uuid.New().String()

	// 保存音频文件到本地
	filePath, savedFileSize, err := sgc.AudioStorage.SaveAudioFile(
		userID.(uint),
		uint(groupID),
		sampleUUID,
		fileName,
		file,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存音频文件失败: " + err.Error()})
		return
	}

	// 调用 asr_server 注册接口
	file.Seek(0, 0) // 重置文件指针
	err = sgc.callRegisterAPI(
		fmt.Sprintf("%d", speakerGroup.ID), // speaker_id 使用声纹组的主键 ID
		speakerGroup.Name,                  // speaker_name 使用组名称
		sampleUUID,
		speakerGroup.AgentID, // agent_id
		file,
		header,
		userID,
	)
	if err != nil {
		// 如果注册失败，删除已保存的文件
		sgc.AudioStorage.DeleteAudioFile(filePath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "注册声纹失败: " + err.Error()})
		return
	}

	// 创建样本记录
	sample := models.SpeakerSample{
		SpeakerGroupID: uint(groupID),
		UserID:         userID.(uint),
		UUID:           sampleUUID,
		FilePath:       filePath,
		FileName:       fileName,
		FileSize:       savedFileSize,
		Status:         "active",
	}

	if err := sgc.DB.Create(&sample).Error; err != nil {
		// 如果数据库保存失败，删除文件和 asr_server 中的记录
		sgc.AudioStorage.DeleteAudioFile(filePath)
		sgc.callDeleteAPI(sampleUUID, speakerGroup.AgentID, userID, sampleUUID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存样本记录失败"})
		return
	}

	// 更新声纹组的样本数量
	sgc.DB.Model(&speakerGroup).Update("sample_count", gorm.Expr("sample_count + 1"))

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": gin.H{
			"id":         sample.ID,
			"uuid":       sample.UUID,
			"file_name":  sample.FileName,
			"file_size":  sample.FileSize,
			"file_path":  sample.FilePath,
			"created_at": sample.CreatedAt,
		},
	})
}

// GetSamples 获取声纹组下的所有样本
func (sgc *SpeakerGroupController) GetSamples(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	groupIDStr := c.Param("id") // 改为使用 :id 参数
	groupID, err := strconv.ParseUint(groupIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的声纹组ID"})
		return
	}

	// 验证声纹组是否存在且属于当前用户
	var speakerGroup models.SpeakerGroup
	if err := sgc.DB.Where("id = ? AND user_id = ?", groupID, userID).First(&speakerGroup).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "声纹组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
		return
	}

	// 查询样本列表
	var samples []models.SpeakerSample
	if err := sgc.DB.Where("speaker_group_id = ?", groupID).Order("created_at DESC").Find(&samples).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询样本失败"})
		return
	}

	// 构建响应
	result := make([]gin.H, 0)
	for _, sample := range samples {
		result = append(result, gin.H{
			"id":         sample.ID,
			"uuid":       sample.UUID,
			"file_name":  sample.FileName,
			"file_size":  sample.FileSize,
			"duration":   sample.Duration,
			"file_path":  sample.FilePath,
			"created_at": sample.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  result,
		"total": len(result),
	})
}

// DeleteSample 删除声纹样本
func (sgc *SpeakerGroupController) DeleteSample(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	groupIDStr := c.Param("id") // 改为使用 :id 参数
	sampleIDStr := c.Param("sample_id")

	groupID, err := strconv.ParseUint(groupIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的声纹组ID"})
		return
	}

	sampleID, err := strconv.ParseUint(sampleIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的样本ID"})
		return
	}

	// 验证样本是否存在且属于当前用户
	var sample models.SpeakerSample
	if err := sgc.DB.Where("id = ? AND speaker_group_id = ? AND user_id = ?", sampleID, groupID, userID).First(&sample).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "样本不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询样本失败"})
		return
	}

	// 查询声纹组以获取 AgentID
	var speakerGroup models.SpeakerGroup
	if err := sgc.DB.Where("id = ?", groupID).First(&speakerGroup).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
		return
	}

	// 调用 asr_server 删除接口（通过 UUID）
	sgc.callDeleteAPI(sample.UUID, speakerGroup.AgentID, userID, sample.UUID)

	// 删除本地文件
	sgc.AudioStorage.DeleteAudioFile(sample.FilePath)

	// 删除数据库记录
	if err := sgc.DB.Delete(&sample).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除样本失败"})
		return
	}

	// 更新声纹组的样本数量
	sgc.DB.Model(&models.SpeakerGroup{}).Where("id = ?", groupID).Update("sample_count", gorm.Expr("sample_count - 1"))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "样本删除成功",
	})
}

// VerifySpeakerGroup 验证声纹组
func (sgc *SpeakerGroupController) VerifySpeakerGroup(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	id := c.Param("id")
	speakerGroupID, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的声纹组ID"})
		return
	}

	// 验证声纹组是否存在且属于当前用户
	var speakerGroup models.SpeakerGroup
	if err := sgc.DB.Where("id = ? AND user_id = ?", speakerGroupID, userID).First(&speakerGroup).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "声纹组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询声纹组失败"})
		return
	}

	// 获取上传的音频文件
	file, header, err := c.Request.FormFile("audio")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "音频文件缺失: " + err.Error()})
		return
	}
	defer file.Close()

	// 调用 asr_server 验证接口
	result, err := sgc.callVerifyAPI(fmt.Sprintf("%d", speakerGroup.ID), speakerGroup.AgentID, file, header, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "验证失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"verified":     result.Verified,
			"confidence":   result.Confidence,
			"threshold":    result.Threshold,
			"speaker_id":   fmt.Sprintf("%d", speakerGroup.ID),
			"speaker_name": speakerGroup.Name,
			"message":      sgc.getVerifyMessage(result.Verified, result.Confidence),
		},
	})
}

// getVerifyMessage 生成验证结果提示信息
func (sgc *SpeakerGroupController) getVerifyMessage(verified bool, confidence float32) string {
	if verified {
		return fmt.Sprintf("验证通过，相似度: %.1f%%", confidence*100)
	}
	return fmt.Sprintf("验证未通过，相似度: %.1f%%", confidence*100)
}

// VerifyResult 验证结果
type VerifyResult struct {
	SpeakerID   string  `json:"speaker_id"`
	SpeakerName string  `json:"speaker_name"`
	Verified    bool    `json:"verified"`
	Confidence  float32 `json:"confidence"`
	Threshold   float32 `json:"threshold"`
}

// callVerifyAPI 调用 asr_server 验证接口
func (sgc *SpeakerGroupController) callVerifyAPI(speakerID string, agentID uint, file multipart.File, header *multipart.FileHeader, userID interface{}) (*VerifyResult, error) {
	if sgc.SpeakerService == nil {
		return nil, fmt.Errorf("speaker_service 未初始化")
	}

	audioData, err := readAndResetMultipartFile(file)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return sgc.SpeakerService.Verify(ctx, speakerVerifyRequest{
		SpeakerID: speakerID,
		AgentID:   agentID,
		UserID:    fmt.Sprintf("%v", userID),
		AudioData: audioData,
		FileName:  header.Filename,
	})
}

// GetSampleFile 获取样本音频文件
func (sgc *SpeakerGroupController) GetSampleFile(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "认证信息缺失"})
		return
	}

	groupIDStr := c.Param("id")
	sampleIDStr := c.Param("sample_id")

	groupID, err := strconv.ParseUint(groupIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的声纹组ID"})
		return
	}

	sampleID, err := strconv.ParseUint(sampleIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的样本ID"})
		return
	}

	// 验证样本是否存在且属于当前用户
	var sample models.SpeakerSample
	if err := sgc.DB.Where("id = ? AND speaker_group_id = ? AND user_id = ?", sampleID, groupID, userID).First(&sample).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "样本不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询样本失败"})
		return
	}

	// 检查文件是否存在
	if !sgc.AudioStorage.FileExists(sample.FilePath) {
		c.JSON(http.StatusNotFound, gin.H{"error": "音频文件不存在"})
		return
	}

	// 打开文件
	file, err := sgc.AudioStorage.GetAudioFile(sample.FilePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取文件失败"})
		return
	}
	defer file.Close()

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取文件信息失败"})
		return
	}

	// 设置响应头
	c.Header("Content-Type", "audio/wav")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", sample.FileName))
	c.Header("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	// 返回文件内容
	c.File(sample.FilePath)
}

// callRegisterAPI 调用 asr_server 注册接口
func (sgc *SpeakerGroupController) callRegisterAPI(speakerID, speakerName, uuid string, agentID uint, file multipart.File, header *multipart.FileHeader, userID interface{}) error {
	if sgc.SpeakerService == nil {
		return fmt.Errorf("speaker_service 未初始化")
	}

	audioData, err := readAndResetMultipartFile(file)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return sgc.SpeakerService.Register(ctx, speakerRegisterRequest{
		SpeakerID:   speakerID,
		SpeakerName: speakerName,
		UUID:        uuid,
		AgentID:     agentID,
		UserID:      fmt.Sprintf("%v", userID),
		AudioData:   audioData,
		FileName:    header.Filename,
	})
}

// callDeleteAPI 调用 asr_server 删除接口
// speakerID: 作为路径参数（speaker_id 或 uuid）
// agentID: Agent ID
// uuid: 可选，如果提供则作为查询参数（用于删除单个样本）
func (sgc *SpeakerGroupController) callDeleteAPI(speakerID string, agentID uint, userID interface{}, uuid ...string) error {
	if sgc.SpeakerService == nil {
		return fmt.Errorf("speaker_service 未初始化")
	}

	deleteReq := speakerDeleteRequest{
		SpeakerID: speakerID,
		AgentID:   agentID,
		UserID:    fmt.Sprintf("%v", userID),
	}
	if len(uuid) > 0 && uuid[0] != "" {
		deleteReq.UUID = uuid[0]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := sgc.SpeakerService.Delete(ctx, deleteReq)
	if err != nil {
		if deleteReq.UUID != "" {
			log.Printf("asr_server 删除失败 (speaker_id: %s, uuid: %s): %v", speakerID, deleteReq.UUID, err)
			return nil
		}
		log.Printf("asr_server 删除失败 (speaker_id: %s): %v", speakerID, err)
		return err
	}

	return nil
}

func readAndResetMultipartFile(file multipart.File) ([]byte, error) {
	if file == nil {
		return nil, fmt.Errorf("音频文件为空")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("重置文件指针失败: %w", err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("读取音频文件失败: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("重置文件指针失败: %w", err)
	}
	return data, nil
}
