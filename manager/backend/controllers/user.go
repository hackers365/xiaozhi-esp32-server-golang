package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xiaozhi/manager/backend/buildinfo"
	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type UserController struct {
	DB                  *gorm.DB
	WebSocketController interface {
		RequestMcpToolDetailsFromClient(ctx context.Context, agentID string) ([]MCPTool, error)
		RequestDeviceMcpToolDetailsFromClient(ctx context.Context, deviceID string) ([]MCPTool, error)
		CallMcpToolFromClient(ctx context.Context, body map[string]interface{}) (map[string]interface{}, error)
		InjectMessageToDevice(ctx context.Context, deviceID, message string, skipLlm bool) error
		GetFirstConnectedClientUUID() string
		RequestServerInfoFromClient(ctx context.Context, uuid string) (*WebSocketResponse, error)
	}
}

// UserConfigResponse 普通用户可见的配置响应（不包含 json_data 等敏感字段）
type UserConfigResponse struct {
	ID        uint      `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	ConfigID  string    `json:"config_id"`
	Provider  string    `json:"provider"`
	Enabled   bool      `json:"enabled"`
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toUserConfigResponse(cfg *models.Config) *UserConfigResponse {
	if cfg == nil {
		return nil
	}

	return &UserConfigResponse{
		ID:        cfg.ID,
		Type:      cfg.Type,
		Name:      cfg.Name,
		ConfigID:  cfg.ConfigID,
		Provider:  cfg.Provider,
		Enabled:   cfg.Enabled,
		IsDefault: cfg.IsDefault,
		CreatedAt: cfg.CreatedAt,
		UpdatedAt: cfg.UpdatedAt,
	}
}

func toUserConfigResponseList(configs []models.Config) []UserConfigResponse {
	result := make([]UserConfigResponse, 0, len(configs))
	for i := range configs {
		result = append(result, *toUserConfigResponse(&configs[i]))
	}
	return result
}

func normalizeMemoryMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "none":
		return "none"
	case "long":
		return "long"
	default:
		return "short"
	}
}

type SystemRuntimeInfo struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Commit        string `json:"commit,omitempty"`
	BuildTime     string `json:"buildTime,omitempty"`
	StartedAt     string `json:"startedAt,omitempty"`
	Uptime        string `json:"uptime"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
}

func buildManagerRuntimeInfo() SystemRuntimeInfo {
	uptimeSeconds := buildinfo.UptimeSeconds()
	return SystemRuntimeInfo{
		Name:          "xiaozhi-manager-backend",
		Version:       buildinfo.Version,
		Commit:        buildinfo.Commit,
		BuildTime:     buildinfo.BuildTime,
		StartedAt:     buildinfo.StartedAtRFC3339(),
		Uptime:        buildinfo.UptimeText(uptimeSeconds),
		UptimeSeconds: uptimeSeconds,
	}
}

func parseStringValue(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func parseInt64Value(v interface{}) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int8:
		return int64(n)
	case int16:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint:
		return int64(n)
	case uint8:
		return int64(n)
	case uint16:
		return int64(n)
	case uint32:
		return int64(n)
	case uint64:
		if n > uint64(^uint64(0)>>1) {
			return int64(^uint64(0) >> 1)
		}
		return int64(n)
	case float32:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
		if f, err := n.Float64(); err == nil {
			return int64(f)
		}
	case string:
		if i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
			return i
		}
	}
	return 0
}

func parseServerRuntimeInfo(body map[string]interface{}) *SystemRuntimeInfo {
	if body == nil {
		return nil
	}
	info := &SystemRuntimeInfo{
		Name:    "xiaozhi-server",
		Version: "unknown",
	}
	if name := parseStringValue(body["server_name"]); name != "" {
		info.Name = name
	}
	if version := parseStringValue(body["version"]); version != "" {
		info.Version = version
	}
	info.Commit = parseStringValue(body["commit"])
	info.BuildTime = parseStringValue(body["build_time"])
	info.StartedAt = parseStringValue(body["started_at"])
	info.UptimeSeconds = parseInt64Value(body["uptime_seconds"])

	uptimeText := parseStringValue(body["uptime"])
	if uptimeText == "" {
		uptimeText = buildinfo.UptimeText(info.UptimeSeconds)
	}
	if info.UptimeSeconds <= 0 && info.StartedAt == "" {
		if _, err := time.Parse(time.RFC3339, uptimeText); err == nil {
			uptimeText = "未知"
		}
	}
	info.Uptime = uptimeText
	return info
}

// 注入消息到设备
func (uc *UserController) InjectMessage(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req struct {
		DeviceID string `json:"device_id" binding:"required"`
		Message  string `json:"message" binding:"required"`
		SkipLlm  bool   `json:"skip_llm"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	// 验证设备是否属于当前用户
	var device models.Device

	if err := uc.DB.Where("device_name = ? AND user_id = ?", req.DeviceID, userID).First(&device).Error; err != nil {
		log.Printf("[InjectMessage] 设备查询失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "设备不存在或不属于当前用户"})
		return
	}

	// 通过WebSocket发送消息注入请求到主服务器
	ctx := context.Background()
	err := uc.WebSocketController.InjectMessageToDevice(ctx, device.DeviceName, req.Message, req.SkipLlm)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "消息注入失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "消息注入请求已发送",
		"data": gin.H{
			"device_id": req.DeviceID,
			"message":   req.Message,
			"skip_llm":  req.SkipLlm,
		},
	})
}

// 用户直接创建设备（无需验证码）
func (uc *UserController) CreateDevice(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req struct {
		DeviceName string `json:"device_name" binding:"required,min=2,max=50"`
		AgentID    uint   `json:"agent_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	// 验证智能体是否存在且属于当前用户
	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", req.AgentID, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "智能体不存在或不属于当前用户"})
		return
	}

	// 生成6位随机设备代码，确保不重复
	var deviceCode string
	for i := 0; i < 10; i++ { // 最多尝试10次
		code := generateRandomCode()

		// 检查代码是否已存在
		var count int64
		if err := uc.DB.Model(&models.Device{}).Where("device_code = ?", code).Count(&count).Error; err == nil && count == 0 {
			deviceCode = code
			break
		}
	}

	// 如果10次都重复，使用时间戳生成
	if deviceCode == "" {
		deviceCode = fmt.Sprintf("%06d", time.Now().Unix()%1000000)
	}

	// 创建设备
	device := models.Device{
		UserID:     userID.(uint),
		AgentID:    req.AgentID,
		DeviceCode: deviceCode,
		DeviceName: req.DeviceName,
		Activated:  true, // 新创建的设备默认未激活
	}

	if err := uc.DB.Create(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建设备失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "设备创建成功",
		"data": gin.H{
			"device_code": deviceCode,
			"device":      device,
		},
	})
}

// 生成6位随机数字代码
func generateRandomCode() string {
	// 生成6位随机数字
	code := fmt.Sprintf("%06d", rand.Intn(1000000))
	return code
}

// 获取用户所有设备概览（只读）
func (uc *UserController) GetMyDevices(c *gin.Context) {
	userID, _ := c.Get("user_id")

	type DeviceOverview struct {
		ID           uint       `json:"id"`
		DeviceName   string     `json:"device_name"`
		DeviceCode   string     `json:"device_code"`
		AgentID      uint       `json:"agent_id"`
		AgentName    string     `json:"agent_name,omitempty"`
		Activated    bool       `json:"activated"`
		LastActiveAt *time.Time `json:"last_active_at"`
		CreatedAt    time.Time  `json:"created_at"`
	}

	var devices []models.Device
	if err := uc.DB.Where("user_id = ?", userID).Find(&devices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取设备列表失败"})
		return
	}

	// 构建设备概览信息
	var result []DeviceOverview
	for _, device := range devices {
		overview := DeviceOverview{
			ID:           device.ID,
			DeviceName:   device.DeviceName,
			DeviceCode:   device.DeviceCode,
			AgentID:      device.AgentID,
			Activated:    device.Activated,
			LastActiveAt: device.LastActiveAt,
			CreatedAt:    device.CreatedAt,
		}

		// 如果设备绑定了智能体，获取智能体名称
		if device.AgentID > 0 {
			var agent models.Agent
			if err := uc.DB.Where("id = ? AND user_id = ?", device.AgentID, userID).First(&agent).Error; err == nil {
				overview.AgentName = agent.Name
			}
		}

		result = append(result, overview)
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// 智能体管理
func (uc *UserController) GetAgents(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var agents []models.Agent
	if err := uc.DB.Where("user_id = ?", userID).Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取智能体列表失败"})
		return
	}

	// 手动加载关联的配置信息
	type AgentWithConfigs struct {
		models.Agent
		LLMConfig        *UserConfigResponse `json:"llm_config,omitempty"`
		TTSConfig        *UserConfigResponse `json:"tts_config,omitempty"`
		KnowledgeBaseIDs []uint              `json:"knowledge_base_ids,omitempty"`
	}

	var result []AgentWithConfigs
	for _, agent := range agents {
		agentWithConfig := AgentWithConfigs{Agent: agent}

		// 加载LLM配置
		if agent.LLMConfigID != nil && *agent.LLMConfigID != "" {
			var llmConfig models.Config
			if err := uc.DB.Where("config_id = ? AND type = ?", *agent.LLMConfigID, "llm").First(&llmConfig).Error; err == nil {
				agentWithConfig.LLMConfig = toUserConfigResponse(&llmConfig)
			}
		}

		// 加载TTS配置
		if agent.TTSConfigID != nil && *agent.TTSConfigID != "" {
			var ttsConfig models.Config
			if err := uc.DB.Where("config_id = ? AND type = ?", *agent.TTSConfigID, "tts").First(&ttsConfig).Error; err == nil {
				agentWithConfig.TTSConfig = toUserConfigResponse(&ttsConfig)
			}
		}
		if ids, err := uc.listAgentKnowledgeBaseIDs(agent.ID); err == nil {
			agentWithConfig.KnowledgeBaseIDs = ids
		}

		result = append(result, agentWithConfig)
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (uc *UserController) CreateAgent(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req struct {
		Name             string  `json:"name" binding:"required,min=2,max=50"`
		CustomPrompt     string  `json:"custom_prompt"`
		LLMConfigID      *string `json:"llm_config_id"`
		TTSConfigID      *string `json:"tts_config_id"`
		Voice            *string `json:"voice"`
		ASRSpeed         string  `json:"asr_speed"`
		MemoryMode       string  `json:"memory_mode"`
		MCPServiceNames  string  `json:"mcp_service_names"`
		KnowledgeBaseIDs []uint  `json:"knowledge_base_ids"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	// 设置默认值
	if req.ASRSpeed == "" {
		req.ASRSpeed = "normal"
	}
	req.MemoryMode = normalizeMemoryMode(req.MemoryMode)
	normalizedMCPServiceNames, err := uc.normalizeAndValidateAgentMCPServices(req.MCPServiceNames)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := uc.validateKnowledgeBaseOwnership(userID.(uint), req.KnowledgeBaseIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	agent := models.Agent{
		UserID:          userID.(uint),
		Name:            req.Name,
		CustomPrompt:    req.CustomPrompt,
		LLMConfigID:     req.LLMConfigID,
		TTSConfigID:     req.TTSConfigID,
		Voice:           req.Voice,
		ASRSpeed:        req.ASRSpeed,
		MemoryMode:      req.MemoryMode,
		MCPServiceNames: normalizedMCPServiceNames,
		Status:          "active",
	}

	if err := uc.DB.Create(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建智能体失败"})
		return
	}
	if err := uc.updateAgentKnowledgeBaseLinks(agent.ID, req.KnowledgeBaseIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新智能体知识库关联失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"success": true, "data": gin.H{"agent": agent, "knowledge_base_ids": uniqueUintSlice(req.KnowledgeBaseIDs)}})
}

func (uc *UserController) GetAgent(c *gin.Context) {
	userID, _ := c.Get("user_id")
	id, _ := strconv.Atoi(c.Param("id"))

	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", id, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在"})
		return
	}

	// 手动加载关联的配置信息
	type AgentWithConfigs struct {
		models.Agent
		LLMConfig        *UserConfigResponse `json:"llm_config,omitempty"`
		TTSConfig        *UserConfigResponse `json:"tts_config,omitempty"`
		KnowledgeBaseIDs []uint              `json:"knowledge_base_ids,omitempty"`
	}

	result := AgentWithConfigs{Agent: agent}

	// 加载LLM配置
	if agent.LLMConfigID != nil && *agent.LLMConfigID != "" {
		var llmConfig models.Config
		if err := uc.DB.Where("config_id = ? AND type = ?", *agent.LLMConfigID, "llm").First(&llmConfig).Error; err == nil {
			result.LLMConfig = toUserConfigResponse(&llmConfig)
		}
	}

	// 加载TTS配置
	if agent.TTSConfigID != nil && *agent.TTSConfigID != "" {
		var ttsConfig models.Config
		if err := uc.DB.Where("config_id = ? AND type = ?", *agent.TTSConfigID, "tts").First(&ttsConfig).Error; err == nil {
			result.TTSConfig = toUserConfigResponse(&ttsConfig)
		}
	}
	if ids, err := uc.listAgentKnowledgeBaseIDs(agent.ID); err == nil {
		result.KnowledgeBaseIDs = ids
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (uc *UserController) UpdateAgent(c *gin.Context) {
	userID, _ := c.Get("user_id")
	id := c.Param("id")

	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", id, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在"})
		return
	}

	var req struct {
		Name             string  `json:"name" binding:"required,min=2,max=50"`
		CustomPrompt     string  `json:"custom_prompt"`
		LLMConfigID      *string `json:"llm_config_id"`
		TTSConfigID      *string `json:"tts_config_id"`
		Voice            *string `json:"voice"`
		ASRSpeed         string  `json:"asr_speed"`
		MemoryMode       *string `json:"memory_mode"`
		MCPServiceNames  string  `json:"mcp_service_names"`
		KnowledgeBaseIDs []uint  `json:"knowledge_base_ids"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	// 更新字段
	agent.Name = req.Name
	agent.CustomPrompt = req.CustomPrompt
	agent.LLMConfigID = req.LLMConfigID
	agent.TTSConfigID = req.TTSConfigID
	agent.Voice = req.Voice

	if req.ASRSpeed != "" {
		agent.ASRSpeed = req.ASRSpeed
	} else {
		agent.ASRSpeed = "normal"
	}
	if req.MemoryMode != nil {
		agent.MemoryMode = normalizeMemoryMode(*req.MemoryMode)
	} else if strings.TrimSpace(agent.MemoryMode) == "" {
		agent.MemoryMode = "short"
	}
	normalizedMCPServiceNames, err := uc.normalizeAndValidateAgentMCPServices(req.MCPServiceNames)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	agent.MCPServiceNames = normalizedMCPServiceNames

	if err := uc.DB.Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新智能体失败"})
		return
	}
	if err := uc.validateKnowledgeBaseOwnership(userID.(uint), req.KnowledgeBaseIDs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := uc.updateAgentKnowledgeBaseLinks(agent.ID, req.KnowledgeBaseIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新智能体知识库关联失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"agent": agent, "knowledge_base_ids": uniqueUintSlice(req.KnowledgeBaseIDs)}})
}

func (uc *UserController) DeleteAgent(c *gin.Context) {
	userID, _ := c.Get("user_id")
	id := c.Param("id")

	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", id, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在"})
		return
	}

	if err := uc.DB.Delete(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除智能体失败"})
		return
	}
	_ = uc.DB.Where("agent_id = ?", agent.ID).Delete(&models.AgentKnowledgeBase{}).Error

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// 获取智能体关联的设备
func (uc *UserController) GetAgentDevices(c *gin.Context) {
	userID, _ := c.Get("user_id")
	agentID := c.Param("id")

	// 首先验证智能体是否存在且属于当前用户
	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", agentID, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在"})
		return
	}

	// 获取属于该智能体的设备
	var devices []models.Device
	if err := uc.DB.Where("user_id = ? AND agent_id = ?", userID, agentID).Find(&devices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取设备列表失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": devices})
}

// 将设备添加到智能体
func (uc *UserController) AddDeviceToAgent(c *gin.Context) {
	userID, _ := c.Get("user_id")
	agentID := c.Param("id")

	var req struct {
		Code string `json:"code" binding:"required,len=6"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "验证码格式错误"})
		return
	}

	// 首先验证智能体是否存在且属于当前用户
	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", agentID, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在"})
		return
	}

	// 验证设备验证码（user_id为0表示设备未绑定用户）
	var device models.Device
	if err := uc.DB.Where("device_code = ? AND user_id = 0", req.Code).First(&device).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "验证码无效或设备已被绑定"})
		return
	}

	// 绑定设备到用户和智能体
	device.UserID = userID.(uint)

	// 转换agentID字符串为uint
	agentIDInt, err := strconv.Atoi(agentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的智能体ID"})
		return
	}
	device.AgentID = uint(agentIDInt)

	// 自动激活设备
	device.Activated = true

	if err := uc.DB.Save(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设备绑定失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"success": true, "data": device})
}

// 从智能体移除设备
func (uc *UserController) RemoveDeviceFromAgent(c *gin.Context) {
	userID, _ := c.Get("user_id")
	agentID := c.Param("id")
	deviceID := c.Param("device_id")

	// 首先验证智能体是否存在且属于当前用户
	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", agentID, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在"})
		return
	}

	// 查找设备并验证所有权
	var device models.Device
	if err := uc.DB.Where("id = ? AND user_id = ? AND agent_id = ?", deviceID, userID, agentID).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在或不属于此智能体"})
		return
	}

	// 将设备从智能体中移除（设置agent_id为0，但保持用户绑定）
	device.AgentID = 0
	if err := uc.DB.Save(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "移除设备失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "设备移除成功"})
}

// 获取角色模板
func (uc *UserController) GetRoleTemplates(c *gin.Context) {
	var roles []models.GlobalRole
	if err := uc.DB.Find(&roles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取角色模板失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": roles})
}

// 获取音色选项
func (uc *UserController) GetVoiceOptions(c *gin.Context) {
	provider := c.Query("provider")
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provider参数必填"})
		return
	}
	configID := c.Query("config_id")

	var systemVoices []VoiceOption
	// 特殊处理：阿里云千问，根据配置中的模型过滤音色
	if provider == "aliyun_qwen" {
		// 如果没有提供 config_id，则返回不区分模型的基础音色列表（用于管理员配置页等场景）
		if configID == "" {
			systemVoices = GetVoiceOptionsByProvider("aliyun_qwen")
		} else {
			// 查找对应的 TTS 配置（type=tts）
			var cfg models.Config
			if err := uc.DB.Where("type = ? AND config_id = ?", "tts", configID).First(&cfg).Error; err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "未找到对应的TTS配置"})
				return
			}

			// 解析 json_data 获取 model
			type qwenConfig struct {
				Model string `json:"model"`
			}
			var qc qwenConfig
			if cfg.JsonData != "" {
				_ = json.Unmarshal([]byte(cfg.JsonData), &qc)
			}
			if qc.Model == "" {
				qc.Model = "qwen3-tts-flash"
			}

			systemVoices = GetAliyunQwenVoicesByModel(qc.Model)
		}
	} else {
		// 其他 provider：根据provider获取固定音色列表
		systemVoices = GetVoiceOptionsByProvider(provider)
	}

	result := make([]VoiceOption, 0, len(systemVoices)+8)
	if userID, ok := c.Get("user_id"); ok && configID != "" {
		var clones []models.VoiceClone
		if err := uc.DB.Where("user_id = ? AND provider = ? AND tts_config_id = ? AND status = ?", userID, provider, configID, "active").Order("created_at DESC").Find(&clones).Error; err == nil {
			for _, clone := range clones {
				result = append(result, BuildVoiceOptionForClone(clone))
			}
		}
	}

	seen := make(map[string]bool)
	for _, v := range result {
		seen[v.Value] = true
	}
	for _, v := range systemVoices {
		if seen[v.Value] {
			continue
		}
		result = append(result, v)
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// 获取LLM配置列表
func (uc *UserController) GetLLMConfigs(c *gin.Context) {
	var configs []models.Config
	// 从全局配置中获取所有启用的LLM配置，默认配置排在前面
	if err := uc.DB.Where("type = ? AND enabled = ?", "llm", true).Order("is_default DESC, name ASC").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取LLM配置失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": toUserConfigResponseList(configs)})
}

// 获取TTS配置列表
func (uc *UserController) GetTTSConfigs(c *gin.Context) {
	var configs []models.Config
	// 从全局配置中获取所有启用的TTS配置，默认配置排在前面
	if err := uc.DB.Where("type = ? AND enabled = ?", "tts", true).Order("is_default DESC, name ASC").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取TTS配置失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": toUserConfigResponseList(configs)})
}

// GetDeviceMcpTools 获取设备维度MCP工具列表（用户版本）
func (uc *UserController) GetDeviceMcpTools(c *gin.Context) {
	userID, _ := c.Get("user_id")
	deviceID := c.Param("id")
	if deviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_id parameter is required"})
		return
	}

	var device models.Device
	if err := uc.DB.Where("id = ? AND user_id = ?", deviceID, userID).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在或不属于当前用户"})
		return
	}

	tools, err := uc.WebSocketController.RequestDeviceMcpToolDetailsFromClient(context.Background(), device.DeviceName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"tools": []interface{}{}}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"tools": tools}})
}

// CallAgentMcpTool 调用智能体维度MCP工具（用户版本）
func (uc *UserController) CallAgentMcpTool(c *gin.Context) {
	userID, _ := c.Get("user_id")
	agentID := c.Param("id")

	var req struct {
		ToolName  string                 `json:"tool_name" binding:"required"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", agentID, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在或不属于当前用户"})
		return
	}

	body := map[string]interface{}{
		"agent_id":  agentID,
		"tool_name": req.ToolName,
		"arguments": req.Arguments,
	}
	result, err := uc.WebSocketController.CallMcpToolFromClient(context.Background(), body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "调用MCP工具失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (uc *UserController) GetAgentMCPServiceOptions(c *gin.Context) {
	userID, _ := c.Get("user_id")
	id := c.Param("id")

	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", id, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在"})
		return
	}

	options, err := listEnabledGlobalMCPServiceNames(uc.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取MCP服务选项失败: %v", err)})
		return
	}

	normalized := normalizeMCPServiceNamesCSV(agent.MCPServiceNames)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"options":           options,
		"selected":          splitMCPServiceNames(normalized),
		"mcp_service_names": normalized,
	}})
}

// CallDeviceMcpTool 调用设备维度MCP工具（用户版本）
func (uc *UserController) CallDeviceMcpTool(c *gin.Context) {
	userID, _ := c.Get("user_id")
	deviceID := c.Param("id")

	var req struct {
		ToolName  string                 `json:"tool_name" binding:"required"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	var device models.Device
	if err := uc.DB.Where("id = ? AND user_id = ?", deviceID, userID).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在或不属于当前用户"})
		return
	}

	body := map[string]interface{}{
		"device_id": device.DeviceName,
		"tool_name": req.ToolName,
		"arguments": req.Arguments,
	}
	result, err := uc.WebSocketController.CallMcpToolFromClient(context.Background(), body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "调用MCP工具失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// GetAgentMCPEndpoint 获取智能体的MCP接入点URL（用户版本）
func (uc *UserController) GetAgentMCPEndpoint(c *gin.Context) {
	userID, _ := c.Get("user_id")
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	// 验证智能体是否存在且属于当前用户
	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", agentID, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在或不属于当前用户"})
		return
	}

	// 使用公共函数生成MCP接入点
	endpoint, err := GenerateAgentMCPEndpoint(uc.DB, agentID, userID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 返回单个endpoint字符串
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"endpoint": endpoint}})
}

// GetAgentMcpTools 获取智能体的MCP工具列表（用户版本）
func (uc *UserController) GetAgentMcpTools(c *gin.Context) {
	userID, _ := c.Get("user_id")
	agentID := c.Param("id")

	// 用户验证函数：验证智能体是否存在且属于当前用户
	userAgentValidator := func(agentID string) error {
		var agent models.Agent
		if err := uc.DB.Where("id = ? AND user_id = ?", agentID, userID).First(&agent).Error; err != nil {
			return fmt.Errorf("智能体不存在或不属于当前用户")
		}
		return nil
	}

	// 使用公共函数
	GetAgentMcpToolsCommon(c, agentID, uc.WebSocketController, userAgentValidator)
}

// 获取仪表板统计数据
func (uc *UserController) GetDashboardStats(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userRole, _ := c.Get("role")

	type DashboardStats struct {
		TotalUsers    int64              `json:"totalUsers"`
		TotalDevices  int64              `json:"totalDevices"`
		TotalAgents   int64              `json:"totalAgents"`
		OnlineDevices int64              `json:"onlineDevices"`
		Manager       SystemRuntimeInfo  `json:"manager"`
		Server        *SystemRuntimeInfo `json:"server,omitempty"`
	}

	stats := DashboardStats{}

	if userRole == "admin" {
		// 管理员查看全部数据
		uc.DB.Model(&models.User{}).Count(&stats.TotalUsers)
		uc.DB.Model(&models.Device{}).Count(&stats.TotalDevices)
		uc.DB.Model(&models.Agent{}).Count(&stats.TotalAgents)
		// 在线设备：最近5分钟内活跃的设备
		fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
		uc.DB.Model(&models.Device{}).Where("last_active_at > ?", fiveMinutesAgo).Count(&stats.OnlineDevices)
	} else {
		// 普通用户只查看自己的数据
		stats.TotalUsers = 0 // 普通用户不显示用户数
		uc.DB.Model(&models.Device{}).Where("user_id = ?", userID).Count(&stats.TotalDevices)
		uc.DB.Model(&models.Agent{}).Where("user_id = ?", userID).Count(&stats.TotalAgents)
		// 在线设备：用户自己的最近5分钟内活跃的设备
		fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
		uc.DB.Model(&models.Device{}).Where("user_id = ? AND last_active_at > ?", userID, fiveMinutesAgo).Count(&stats.OnlineDevices)
	}

	stats.Manager = buildManagerRuntimeInfo()
	if uc.WebSocketController != nil {
		clientUUID := uc.WebSocketController.GetFirstConnectedClientUUID()
		if clientUUID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			resp, err := uc.WebSocketController.RequestServerInfoFromClient(ctx, clientUUID)
			if err != nil {
				log.Printf("获取主服务运行信息失败: %v", err)
			} else if resp != nil && resp.Status == http.StatusOK {
				stats.Server = parseServerRuntimeInfo(resp.Body)
			}
		}
	}

	c.JSON(http.StatusOK, stats)
}

func (uc *UserController) updateAgentKnowledgeBaseLinks(agentID uint, knowledgeBaseIDs []uint) error {
	return uc.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&models.AgentKnowledgeBase{}).Error; err != nil {
			return err
		}
		for _, kbID := range uniqueUintSlice(knowledgeBaseIDs) {
			if err := tx.Create(&models.AgentKnowledgeBase{AgentID: agentID, KnowledgeBaseID: kbID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
