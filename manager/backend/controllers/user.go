package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type UserController struct {
	DB                  *gorm.DB
	InternalAuthToken   string
	EndpointAuthToken   string
	WebSocketController interface {
		RequestMcpToolDetailsFromClient(ctx context.Context, agentID string) ([]MCPTool, error)
		RequestMcpEndpointStatusFromClient(ctx context.Context, agentID string) (map[string]interface{}, error)
		RequestDeviceMcpToolDetailsFromClient(ctx context.Context, deviceID string) ([]MCPTool, error)
		CallMcpToolFromClient(ctx context.Context, body map[string]interface{}) (map[string]interface{}, error)
		RequestOpenClawStatusFromClient(ctx context.Context, agentID string) (map[string]interface{}, error)
		CallOpenClawChatFromClient(ctx context.Context, body map[string]interface{}) (map[string]interface{}, error)
		CallOpenClawChatStreamFromClient(ctx context.Context, body map[string]interface{}, onResponse func(*WebSocketResponse) error) (map[string]interface{}, error)
		InjectMessageToDevice(ctx context.Context, deviceID, message string, skipLlm bool, autoListen bool) error
		InjectMessageParamsToDevice(ctx context.Context, params map[string]interface{}) error
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

// 语音推送到设备
func (uc *UserController) InjectMessage(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var req struct {
		DeviceID    string      `json:"device_id" binding:"required"`
		Message     string      `json:"message" binding:"required"`
		Mode        string      `json:"mode"` // "notify" (单向异步通知) 或 "speak" (默认双向对话)
		SkipLlm     bool        `json:"skip_llm"`
		AutoListen  *bool       `json:"auto_listen"`
		Voice       string      `json:"voice"`
		TTSConfigID string      `json:"tts_config_id"`
		Speed       float64     `json:"speed"`
		AudioURL    string      `json:"audio_url"`
		Subtitles   interface{} `json:"subtitles"`
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

	autoListen := true
	if req.AutoListen != nil {
		autoListen = *req.AutoListen
	}

	params := map[string]interface{}{
		"device_id":   device.DeviceName,
		"message":     req.Message,
		"mode":        req.Mode,
		"skip_llm":    req.SkipLlm,
		"auto_listen": autoListen,
	}
	if req.Voice != "" {
		params["voice"] = req.Voice
	}
	if req.TTSConfigID != "" {
		params["tts_config_id"] = req.TTSConfigID
	}
	if req.Speed > 0 {
		params["speed"] = req.Speed
	}
	if req.AudioURL != "" {
		params["audio_url"] = req.AudioURL
	}
	if req.Subtitles != nil {
		params["subtitles"] = req.Subtitles
	}

	// 通过WebSocket发送语音推送请求到主服务器
	ctx := context.Background()
	err := uc.WebSocketController.InjectMessageParamsToDevice(ctx, params)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "语音推送失败: " + err.Error()})
		return
	}

	responseData := gin.H{
		"device_id":   req.DeviceID,
		"message":     req.Message,
		"mode":        req.Mode,
		"skip_llm":    req.SkipLlm,
		"auto_listen": autoListen,
	}
	if req.Voice != "" {
		responseData["voice"] = req.Voice
	}
	if req.AudioURL != "" {
		responseData["audio_url"] = req.AudioURL
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "语音推送请求已发送",
		"data":    responseData,
	})
}

// 用户直接创建设备（无需验证码）
func (uc *UserController) CreateDevice(c *gin.Context) {
	var req DevicePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}
	device, err := NewDeviceService(uc.DB).Create(scopeFromContext(c), req)
	if err != nil {
		writeServiceError(c, err, "创建设备失败")
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"message": "设备创建成功",
		"data": gin.H{
			"device_code": device.DeviceCode,
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

func isSixDigitCode(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func normalizeDeviceNameCandidate(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", ":"))
}

func normalizeDeviceNickName(value string) (string, error) {
	nickName := strings.TrimSpace(value)
	if len([]rune(nickName)) > 50 {
		return "", fmt.Errorf("设备昵称最多 50 个字符")
	}
	return nickName, nil
}

func generateUniqueDeviceCode(db *gorm.DB) string {
	for i := 0; i < 10; i++ { // 最多尝试10次
		code := generateRandomCode()

		var count int64
		if err := db.Model(&models.Device{}).Where("device_code = ?", code).Count(&count).Error; err == nil && count == 0 {
			return code
		}
	}

	return fmt.Sprintf("%06d", time.Now().Unix()%1000000)
}

// 获取用户所有设备概览（只读）
func (uc *UserController) GetMyDevices(c *gin.Context) {
	result, err := NewDeviceService(uc.DB).List(scopeFromContext(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取设备列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

// UpdateDevice 更新当前用户自己的设备昵称。device_name 是设备端标识，不在这里修改。
func (uc *UserController) UpdateDevice(c *gin.Context) {
	deviceID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req DevicePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}
	device, err := NewDeviceService(uc.DB).Update(scopeFromContext(c), deviceID, req)
	if err != nil {
		writeServiceError(c, err, "更新设备失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": device})
}

// DeleteDevice 从系统中删除当前用户自己的设备。删除后设备需要重新走激活流程才能再次进入系统。
func (uc *UserController) DeleteDevice(c *gin.Context) {
	deviceID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	if err := NewDeviceService(uc.DB).Delete(scopeFromContext(c), deviceID); err != nil {
		writeServiceError(c, err, "删除设备失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "设备已从系统删除，需要重新激活后才能再次使用"})
}

// 智能体管理
func (uc *UserController) GetAgents(c *gin.Context) {
	result, err := NewAgentService(uc.DB).List(scopeFromContext(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取智能体列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (uc *UserController) CreateAgent(c *gin.Context) {
	var req AgentPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}
	agent, err := NewAgentService(uc.DB).Create(scopeFromContext(c), req)
	if err != nil {
		writeServiceError(c, err, "创建智能体失败")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": gin.H{"agent": agent, "knowledge_base_ids": agent.KnowledgeBaseIDs}})
}

func (uc *UserController) GetAgent(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	result, err := NewAgentService(uc.DB).Get(scopeFromContext(c), id)
	if err != nil {
		writeServiceError(c, err, "获取智能体失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}

func (uc *UserController) UpdateAgent(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req AgentPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}
	agent, err := NewAgentService(uc.DB).Update(scopeFromContext(c), id, req)
	if err != nil {
		writeServiceError(c, err, "更新智能体失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"agent": agent, "knowledge_base_ids": agent.KnowledgeBaseIDs}})
}

func (uc *UserController) DeleteAgent(c *gin.Context) {
	id, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	if err := NewAgentService(uc.DB).Delete(scopeFromContext(c), id); err != nil {
		writeServiceError(c, err, "删除智能体失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// 获取智能体关联的设备
func (uc *UserController) GetAgentDevices(c *gin.Context) {
	agentID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	devices, err := NewDeviceService(uc.DB).ListByAgent(scopeFromContext(c), agentID)
	if err != nil {
		writeServiceError(c, err, "获取设备列表失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": devices})
}

// 将设备添加到智能体
func (uc *UserController) AddDeviceToAgent(c *gin.Context) {
	agentID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	var req DevicePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}
	device, err := NewDeviceService(uc.DB).BindToAgent(scopeFromContext(c), agentID, req)
	if err != nil {
		writeServiceError(c, err, "设备绑定失败")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": device})
}

// 从智能体移除设备
func (uc *UserController) RemoveDeviceFromAgent(c *gin.Context) {
	agentID, ok := parseUintParam(c, "id")
	if !ok {
		return
	}
	deviceID, ok := parseUintParam(c, "device_id")
	if !ok {
		return
	}
	if err := NewDeviceService(uc.DB).UnbindFromAgent(scopeFromContext(c), agentID, deviceID); err != nil {
		writeServiceError(c, err, "移除设备失败")
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

func trimSuffixFoldForURL(s string, suffix string) string {
	if len(s) < len(suffix) {
		return s
	}
	start := len(s) - len(suffix)
	if strings.EqualFold(s[start:], suffix) {
		return s[:start]
	}
	return s
}

func normalizeIndexTTSVoiceOptionsBaseURL(raw string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	baseURL = trimSuffixFoldForURL(baseURL, "/audio/speech")
	baseURL = trimSuffixFoldForURL(baseURL, "/audio/voices")
	return strings.TrimRight(baseURL, "/")
}

func (uc *UserController) fetchIndexTTSVoices(c *gin.Context, configID, overrideURL, overrideAPIKey string) ([]VoiceOption, error) {
	baseURL := "http://127.0.0.1:7860"
	apiKey := ""
	if strings.TrimSpace(configID) != "" {
		var cfg models.Config
		if err := uc.DB.Where("type = ? AND config_id = ?", "tts", configID).First(&cfg).Error; err == nil {
			var cfgMap map[string]any
			if strings.TrimSpace(cfg.JsonData) != "" && json.Unmarshal([]byte(cfg.JsonData), &cfgMap) == nil {
				if v, ok := cfgMap["api_url"].(string); ok && strings.TrimSpace(v) != "" {
					baseURL = strings.TrimSpace(v)
				}
				if v, ok := cfgMap["api_key"].(string); ok {
					apiKey = strings.TrimSpace(v)
				}
			}
		}
	}
	if strings.TrimSpace(overrideURL) != "" {
		baseURL = strings.TrimSpace(overrideURL)
	}
	if strings.TrimSpace(overrideAPIKey) != "" {
		apiKey = strings.TrimSpace(overrideAPIKey)
	}
	baseURL = normalizeIndexTTSVoiceOptionsBaseURL(baseURL)
	if baseURL == "" {
		baseURL = "http://127.0.0.1:7860"
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, baseURL+indexTTSVoicesEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("IndexTTS 获取音色失败: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	voiceMap := map[string]any{}
	if err = json.Unmarshal(body, &voiceMap); err != nil {
		return nil, err
	}
	result := make([]VoiceOption, 0, len(voiceMap))
	normalizedConfigPrefix := strings.ToLower(strings.TrimSpace(configID))
	if normalizedConfigPrefix != "" {
		normalizedConfigPrefix += "_"
	}
	for voice := range voiceMap {
		v := strings.TrimSpace(voice)
		if v == "" {
			continue
		}
		// 过滤掉当前 IndexTTS 配置实例生成的内部前缀音色，避免和复刻音色重复展示。
		if normalizedConfigPrefix != "" && strings.HasPrefix(strings.ToLower(v), normalizedConfigPrefix) {
			continue
		}
		result = append(result, VoiceOption{Value: v, Label: v})
	}
	return result, nil
}

// 获取音色选项
func (uc *UserController) GetVoiceOptions(c *gin.Context) {
	scope := scopeFromContext(c)
	voices, err := getVoiceOptionsForUser(
		uc.DB,
		c,
		scope.ActorUserID,
		c.Query("provider"),
		c.Query("config_id"),
		c.Query("api_url"),
		c.Query("api_key"),
	)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "IndexTTS") {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": voices})
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

func (uc *UserController) GetMCPServiceOptions(c *gin.Context) {
	options, err := listEnabledGlobalMCPServiceNames(uc.DB)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取MCP服务选项失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"options": options,
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
	endpoint, err := GenerateAgentMCPEndpoint(uc.DB, agentID, userID.(uint), uc.EndpointAuthToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	data := newMcpEndpointData(endpoint)
	if uc.WebSocketController == nil {
		data["status_message"] = "websocket controller unavailable"
		c.JSON(http.StatusOK, gin.H{"data": data})
		return
	}

	statusResult, statusErr := uc.WebSocketController.RequestMcpEndpointStatusFromClient(context.Background(), agentID)
	if statusErr != nil {
		data["status_message"] = statusErr.Error()
		c.JSON(http.StatusOK, gin.H{"data": data})
		return
	}

	applyMcpEndpointStatus(data, statusResult)
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// GetAgentOpenClawEndpoint 获取智能体的OpenClaw接入点URL（用户版本）
func (uc *UserController) GetAgentOpenClawEndpoint(c *gin.Context) {
	userID, _ := c.Get("user_id")
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", agentID, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在或不属于当前用户"})
		return
	}

	data := gin.H{
		"endpoint":  "",
		"status":    "unknown",
		"connected": false,
	}

	endpoint, err := GenerateAgentOpenClawEndpoint(uc.DB, agentID, userID.(uint), uc.EndpointAuthToken)
	if err != nil {
		data["status_message"] = err.Error()
		c.JSON(http.StatusOK, gin.H{"data": data})
		return
	}
	data["endpoint"] = endpoint

	if uc.WebSocketController == nil {
		data["status_message"] = "websocket controller unavailable"
		c.JSON(http.StatusOK, gin.H{"data": data})
		return
	}

	statusResult, statusErr := uc.WebSocketController.RequestOpenClawStatusFromClient(context.Background(), agentID)
	if statusErr != nil {
		data["status_message"] = statusErr.Error()
		c.JSON(http.StatusOK, gin.H{"data": data})
		return
	}

	connected, _ := statusResult["connected"].(bool)
	status, _ := statusResult["status"].(string)
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		if connected {
			status = "online"
		} else {
			status = "offline"
		}
	}

	data["connected"] = connected
	data["status"] = status
	if msg, ok := statusResult["status_message"].(string); ok && strings.TrimSpace(msg) != "" {
		data["status_message"] = msg
	}

	c.JSON(http.StatusOK, gin.H{"data": data})
}

// CallAgentOpenClawChatTest 调用智能体 OpenClaw 对话测试（用户版本）
func (uc *UserController) CallAgentOpenClawChatTest(c *gin.Context) {
	userID, _ := c.Get("user_id")
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}
	if uc.WebSocketController == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "websocket controller unavailable"})
		return
	}

	var req struct {
		Message   string `json:"message" binding:"required"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "message 不能为空"})
		return
	}

	var agent models.Agent
	if err := uc.DB.Where("id = ? AND user_id = ?", agentID, userID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在或不属于当前用户"})
		return
	}

	body := map[string]interface{}{
		"agent_id": agentID,
		"message":  req.Message,
	}
	if req.TimeoutMs > 0 {
		body["timeout_ms"] = req.TimeoutMs
	}

	if wantsOpenClawSSE(c) {
		if !prepareOpenClawSSE(c) {
			return
		}
		_ = writeOpenClawSSE(c, "start", map[string]interface{}{
			"agent_id": agentID,
		})

		terminalErrorSent := false
		result, err := uc.WebSocketController.CallOpenClawChatStreamFromClient(
			c.Request.Context(),
			body,
			func(resp *WebSocketResponse) error {
				if resp == nil {
					return nil
				}
				payload := map[string]interface{}{
					"status": resp.Status,
				}
				if resp.Body != nil {
					payload["data"] = resp.Body
				}
				if msg := strings.TrimSpace(resp.Error); msg != "" {
					payload["error"] = msg
				}

				switch resp.Status {
				case http.StatusPartialContent:
					return writeOpenClawSSE(c, "chunk", payload)
				case http.StatusOK:
					return writeOpenClawSSE(c, "result", payload)
				default:
					terminalErrorSent = true
					return writeOpenClawSSE(c, "error", payload)
				}
			},
		)
		if err != nil {
			if !terminalErrorSent {
				_ = writeOpenClawSSE(c, "error", map[string]interface{}{
					"error": err.Error(),
				})
			}
			_ = writeOpenClawSSE(c, "done", map[string]interface{}{
				"ok": false,
			})
			return
		}

		_ = writeOpenClawSSE(c, "done", map[string]interface{}{
			"ok":   true,
			"data": result,
		})
		return
	}

	result, err := uc.WebSocketController.CallOpenClawChatFromClient(context.Background(), body)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(strings.ToLower(msg), "not connected"), strings.Contains(msg, "未连接"):
			c.JSON(http.StatusConflict, gin.H{"error": msg})
		case strings.Contains(strings.ToLower(msg), "timeout"), strings.Contains(msg, "超时"):
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": msg})
		case strings.Contains(strings.ToLower(msg), "missing"), strings.Contains(msg, "参数"):
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		case strings.Contains(msg, "没有连接的客户端"):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": msg})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "调用OpenClaw对话测试失败: " + msg})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
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
		TotalUsers       int64  `json:"totalUsers"`
		TotalDevices     int64  `json:"totalDevices"`
		TotalAgents      int64  `json:"totalAgents"`
		OnlineDevices    int64  `json:"onlineDevices"`
		ProgramStartedAt string `json:"programStartedAt"`
	}

	stats := DashboardStats{
		ProgramStartedAt: programStartedAt.Format(time.RFC3339),
	}

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

	c.JSON(http.StatusOK, stats)
}
