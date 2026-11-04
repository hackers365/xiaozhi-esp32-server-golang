package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type accessScope struct {
	ActorUserID uint
	IsAdmin     bool
}

func scopeFromContext(c *gin.Context) accessScope {
	uid, _ := c.Get("user_id")
	role, _ := c.Get("role")
	userID, _ := uid.(uint)
	roleName, _ := role.(string)
	return accessScope{
		ActorUserID: userID,
		IsAdmin:     roleName == "admin",
	}
}

func targetUserIDFromScope(scope accessScope, requested uint) (uint, error) {
	if scope.IsAdmin {
		if requested == 0 {
			return 0, fmt.Errorf("请选择所属用户")
		}
		return requested, nil
	}
	if scope.ActorUserID == 0 {
		return 0, fmt.Errorf("用户未认证")
	}
	return scope.ActorUserID, nil
}

type AgentPayload struct {
	UserID           uint                    `json:"user_id"`
	Name             string                  `json:"name"`
	Nickname         *string                 `json:"nickname"`
	CustomPrompt     string                  `json:"custom_prompt"`
	LLMConfigID      *string                 `json:"llm_config_id"`
	TTSConfigID      *string                 `json:"tts_config_id"`
	RealtimeEnabled  bool                    `json:"realtime_enabled"`
	RealtimeConfigID *string                 `json:"realtime_config_id"`
	Voice            *string                 `json:"voice"`
	ASRSpeed         string                  `json:"asr_speed"`
	MemoryMode       *string                 `json:"memory_mode"`
	SpeakerChatMode  *string                 `json:"speaker_chat_mode"`
	MCPServiceNames  string                  `json:"mcp_service_names"`
	OpenClaw         *OpenClawConfigResponse `json:"openclaw"`
	OpenClawConfig   *string                 `json:"openclaw_config"`
	KnowledgeBaseIDs *[]uint                 `json:"knowledge_base_ids"`
}

type AgentResponse struct {
	models.Agent
	LLMConfig        interface{} `json:"llm_config,omitempty"`
	TTSConfig        interface{} `json:"tts_config,omitempty"`
	KnowledgeBaseIDs []uint      `json:"knowledge_base_ids"`
	DeviceCount      int64       `json:"device_count"`
	Username         string      `json:"username,omitempty"`
}

type AgentService struct {
	DB *gorm.DB
}

func NewAgentService(db *gorm.DB) *AgentService {
	return &AgentService{DB: db}
}

func (svc *AgentService) List(scope accessScope) ([]AgentResponse, error) {
	var agents []models.Agent
	query := svc.DB.Order("id DESC")
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.Find(&agents).Error; err != nil {
		return nil, err
	}
	return svc.enrichAgents(scope, agents)
}

func (svc *AgentService) Get(scope accessScope, id uint) (*AgentResponse, error) {
	var agent models.Agent
	query := svc.DB.Where("id = ?", id)
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("智能体不存在")
		}
		return nil, err
	}
	items, err := svc.enrichAgents(scope, []models.Agent{agent})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("智能体不存在")
	}
	return &items[0], nil
}

func (svc *AgentService) Create(scope accessScope, payload AgentPayload) (*AgentResponse, error) {
	targetUserID, err := targetUserIDFromScope(scope, payload.UserID)
	if err != nil {
		return nil, err
	}
	if err := svc.assertUserExists(targetUserID); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	nickname := name
	if payload.Nickname != nil {
		nickname = strings.TrimSpace(*payload.Nickname)
	}
	if name == "" {
		return nil, fmt.Errorf("请输入智能体名称")
	}
	if len([]rune(name)) > 50 {
		return nil, fmt.Errorf("智能体名称不能超过50个字符")
	}
	if nickname == "" {
		nickname = name
	}
	if len([]rune(nickname)) > 50 {
		return nil, fmt.Errorf("智能体昵称不能超过50个字符")
	}

	agent := models.Agent{
		UserID:           targetUserID,
		Name:             name,
		Nickname:         nickname,
		CustomPrompt:     payload.CustomPrompt,
		LLMConfigID:      cleanStringPtr(payload.LLMConfigID),
		TTSConfigID:      cleanStringPtr(payload.TTSConfigID),
		RealtimeEnabled:  payload.RealtimeEnabled,
		RealtimeConfigID: cleanStringPtr(payload.RealtimeConfigID),
		Voice:            cleanStringPtr(payload.Voice),
		ASRSpeed:         normalizeASRSpeed(payload.ASRSpeed),
		MemoryMode:       normalizeMemoryModeFromPtr(payload.MemoryMode),
		SpeakerChatMode:  normalizeSpeakerChatModeFromPtr(payload.SpeakerChatMode),
	}
	normalizedMCP, err := normalizeAgentMCPServices(svc.DB, payload.MCPServiceNames)
	if err != nil {
		return nil, err
	}
	agent.MCPServiceNames = normalizedMCP
	if err := validateAgentRealtimeSelection(svc.DB, agent.RealtimeEnabled, agent.RealtimeConfigID); err != nil {
		return nil, err
	}
	applyOpenClawConfigToAgent(&agent, resolvePayloadOpenClawConfig(defaultOpenClawConfig(), payload))

	knowledgeBaseIDs := []uint{}
	if payload.KnowledgeBaseIDs != nil {
		knowledgeBaseIDs = *payload.KnowledgeBaseIDs
	}
	if err := svc.validateKnowledgeBaseOwnership(targetUserID, knowledgeBaseIDs); err != nil {
		return nil, err
	}

	if err := svc.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&agent).Error; err != nil {
			return err
		}
		return replaceAgentKnowledgeBaseLinks(tx, agent.ID, knowledgeBaseIDs)
	}); err != nil {
		return nil, err
	}
	return svc.Get(scope, agent.ID)
}

func (svc *AgentService) Update(scope accessScope, id uint, payload AgentPayload) (*AgentResponse, error) {
	var agent models.Agent
	query := svc.DB.Where("id = ?", id)
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("智能体不存在")
		}
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, fmt.Errorf("请输入智能体名称")
	}
	if len([]rune(name)) > 50 {
		return nil, fmt.Errorf("智能体名称不能超过50个字符")
	}
	agent.Name = name
	if payload.Nickname != nil {
		agent.Nickname = strings.TrimSpace(*payload.Nickname)
	}
	ensureAgentNickname(&agent)
	if len([]rune(agent.Nickname)) > 50 {
		return nil, fmt.Errorf("智能体昵称不能超过50个字符")
	}

	agent.CustomPrompt = payload.CustomPrompt
	agent.LLMConfigID = cleanStringPtr(payload.LLMConfigID)
	agent.TTSConfigID = cleanStringPtr(payload.TTSConfigID)
	agent.RealtimeEnabled = payload.RealtimeEnabled
	agent.RealtimeConfigID = cleanStringPtr(payload.RealtimeConfigID)
	agent.Voice = cleanStringPtr(payload.Voice)
	agent.ASRSpeed = normalizeASRSpeed(payload.ASRSpeed)
	if payload.MemoryMode != nil {
		agent.MemoryMode = normalizeAgentMemoryMode(*payload.MemoryMode)
	} else if strings.TrimSpace(agent.MemoryMode) == "" {
		agent.MemoryMode = "short"
	}
	if payload.SpeakerChatMode != nil {
		agent.SpeakerChatMode = normalizeAgentSpeakerChatMode(*payload.SpeakerChatMode)
	} else if strings.TrimSpace(agent.SpeakerChatMode) == "" {
		agent.SpeakerChatMode = "off"
	}
	normalizedMCP, err := normalizeAgentMCPServices(svc.DB, payload.MCPServiceNames)
	if err != nil {
		return nil, err
	}
	agent.MCPServiceNames = normalizedMCP
	if err := validateAgentRealtimeSelection(svc.DB, agent.RealtimeEnabled, agent.RealtimeConfigID); err != nil {
		return nil, err
	}
	applyOpenClawConfigToAgent(&agent, resolvePayloadOpenClawConfig(buildOpenClawConfigFromAgent(agent), payload))

	if payload.KnowledgeBaseIDs != nil {
		if err := svc.validateKnowledgeBaseOwnership(agent.UserID, *payload.KnowledgeBaseIDs); err != nil {
			return nil, err
		}
	}

	if err := svc.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&agent).Error; err != nil {
			return err
		}
		if payload.KnowledgeBaseIDs != nil {
			return replaceAgentKnowledgeBaseLinks(tx, agent.ID, *payload.KnowledgeBaseIDs)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return svc.Get(scope, agent.ID)
}

func (svc *AgentService) Delete(scope accessScope, id uint) error {
	var agent models.Agent
	query := svc.DB.Where("id = ?", id)
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("智能体不存在")
		}
		return err
	}
	deviceCount, err := countDevicesByAgentID(svc.DB, agent.ID)
	if err != nil {
		return fmt.Errorf("查询智能体绑定设备失败")
	}
	if deviceCount > 0 {
		return fmt.Errorf("智能体已绑定设备，请先移除所有设备后再删除")
	}
	return svc.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&agent).Error; err != nil {
			return err
		}
		return tx.Where("agent_id = ?", agent.ID).Delete(&models.AgentKnowledgeBase{}).Error
	})
}

func (svc *AgentService) enrichAgents(scope accessScope, agents []models.Agent) ([]AgentResponse, error) {
	if len(agents) == 0 {
		return []AgentResponse{}, nil
	}

	userIDs := make([]uint, 0)
	agentIDs := make([]uint, 0, len(agents))
	configIDs := make(map[string]string)
	for i := range agents {
		ensureAgentNickname(&agents[i])
		agentIDs = append(agentIDs, agents[i].ID)
		userIDs = appendUniqueUint(userIDs, agents[i].UserID)
		if agents[i].LLMConfigID != nil && strings.TrimSpace(*agents[i].LLMConfigID) != "" {
			configIDs["llm:"+strings.TrimSpace(*agents[i].LLMConfigID)] = strings.TrimSpace(*agents[i].LLMConfigID)
		}
		if agents[i].TTSConfigID != nil && strings.TrimSpace(*agents[i].TTSConfigID) != "" {
			configIDs["tts:"+strings.TrimSpace(*agents[i].TTSConfigID)] = strings.TrimSpace(*agents[i].TTSConfigID)
		}
	}

	configByTypeID := make(map[string]models.Config)
	if len(configIDs) > 0 {
		var configs []models.Config
		if err := svc.DB.Where("type IN ? AND config_id IN ?", []string{"llm", "tts"}, mapValues(configIDs)).Find(&configs).Error; err != nil {
			return nil, err
		}
		for _, cfg := range configs {
			configByTypeID[cfg.Type+":"+cfg.ConfigID] = cfg
		}
	}

	kbIDsByAgent := make(map[uint][]uint)
	var links []models.AgentKnowledgeBase
	if err := svc.DB.Where("agent_id IN ?", agentIDs).Order("id ASC").Find(&links).Error; err != nil {
		return nil, err
	}
	for _, link := range links {
		kbIDsByAgent[link.AgentID] = append(kbIDsByAgent[link.AgentID], link.KnowledgeBaseID)
	}

	deviceCountByAgent := make(map[uint]int64)
	var countRows []struct {
		AgentID uint
		Count   int64
	}
	if err := svc.DB.Model(&models.Device{}).
		Select("agent_id, COUNT(*) as count").
		Where("agent_id IN ?", agentIDs).
		Group("agent_id").
		Scan(&countRows).Error; err != nil {
		return nil, err
	}
	for _, row := range countRows {
		deviceCountByAgent[row.AgentID] = row.Count
	}

	usernameByID := make(map[uint]string)
	if scope.IsAdmin && len(userIDs) > 0 {
		var users []models.User
		if err := svc.DB.Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, user := range users {
			usernameByID[user.ID] = user.Username
		}
	}

	result := make([]AgentResponse, 0, len(agents))
	for _, agent := range agents {
		item := AgentResponse{
			Agent:            agent,
			KnowledgeBaseIDs: kbIDsByAgent[agent.ID],
			DeviceCount:      deviceCountByAgent[agent.ID],
			Username:         usernameByID[agent.UserID],
		}
		if cfg, ok := configByTypeID["llm:"+safeStringPtr(agent.LLMConfigID)]; ok {
			if scope.IsAdmin {
				item.LLMConfig = &cfg
			} else {
				item.LLMConfig = toUserConfigResponse(&cfg)
			}
		}
		if cfg, ok := configByTypeID["tts:"+safeStringPtr(agent.TTSConfigID)]; ok {
			if scope.IsAdmin {
				item.TTSConfig = &cfg
			} else {
				item.TTSConfig = toUserConfigResponse(&cfg)
			}
		}
		if item.KnowledgeBaseIDs == nil {
			item.KnowledgeBaseIDs = []uint{}
		}
		result = append(result, item)
	}
	return result, nil
}

func (svc *AgentService) assertUserExists(userID uint) error {
	var count int64
	if err := svc.DB.Model(&models.User{}).Where("id = ?", userID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("指定的用户不存在")
	}
	return nil
}

func (svc *AgentService) validateKnowledgeBaseOwnership(userID uint, knowledgeBaseIDs []uint) error {
	uniqueIDs := uniqueUintSlice(knowledgeBaseIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}
	var count int64
	if err := svc.DB.Model(&models.KnowledgeBase{}).Where("user_id = ? AND id IN ?", userID, uniqueIDs).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(uniqueIDs)) {
		return fmt.Errorf("包含无效或越权的知识库ID")
	}
	return nil
}

type DevicePayload struct {
	UserID     uint   `json:"user_id"`
	NickName   string `json:"nick_name"`
	DeviceCode string `json:"device_code"`
	DeviceName string `json:"device_name"`
	AgentID    uint   `json:"agent_id"`
	Activated  *bool  `json:"activated"`
	Code       string `json:"code"`
	DeviceMAC  string `json:"device_mac"`
}

type DeviceResponse struct {
	models.Device
	AgentName string `json:"agent_name,omitempty"`
	RoleName  string `json:"role_name,omitempty"`
	RoleType  string `json:"role_type,omitempty"`
	Username  string `json:"username,omitempty"`
}

type DeviceService struct {
	DB *gorm.DB
}

func NewDeviceService(db *gorm.DB) *DeviceService {
	return &DeviceService{DB: db}
}

func (svc *DeviceService) List(scope accessScope) ([]DeviceResponse, error) {
	var devices []models.Device
	query := svc.DB.Order("id DESC")
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.Find(&devices).Error; err != nil {
		return nil, err
	}
	return svc.enrichDevices(scope, devices)
}

func (svc *DeviceService) ListByAgent(scope accessScope, agentID uint) ([]DeviceResponse, error) {
	var agent models.Agent
	query := svc.DB.Where("id = ?", agentID)
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("智能体不存在")
		}
		return nil, err
	}

	var devices []models.Device
	if err := svc.DB.Where("user_id = ? AND agent_id = ?", agent.UserID, agent.ID).Find(&devices).Error; err != nil {
		return nil, err
	}
	return svc.enrichDevices(scope, devices)
}

func (svc *DeviceService) Create(scope accessScope, payload DevicePayload) (*DeviceResponse, error) {
	targetUserID, err := targetUserIDFromScope(scope, payload.UserID)
	if err != nil {
		return nil, err
	}
	if err := svc.assertUserExists(targetUserID); err != nil {
		return nil, err
	}
	nickName, err := normalizeDeviceNickName(payload.NickName)
	if err != nil {
		return nil, err
	}
	deviceName := strings.TrimSpace(payload.DeviceName)
	deviceCode := strings.TrimSpace(payload.DeviceCode)
	if scope.IsAdmin {
		if deviceCode == "" && deviceName == "" {
			return nil, fmt.Errorf("激活码和设备标识至少填写一个")
		}
	} else if deviceName == "" {
		return nil, fmt.Errorf("请输入设备标识")
	}
	if nickName == "" {
		nickName = deviceName
	}
	if payload.AgentID > 0 {
		if err := svc.assertAgentOwnedByUser(payload.AgentID, targetUserID); err != nil {
			return nil, err
		}
	}
	if deviceCode == "" && !scope.IsAdmin {
		deviceCode = generateUniqueDeviceCode(svc.DB)
	}
	activated := true
	if payload.Activated != nil {
		activated = *payload.Activated
	}

	var device models.Device
	if scope.IsAdmin && deviceCode != "" {
		if err := svc.DB.Where("device_code = ?", deviceCode).First(&device).Error; err == nil {
			device.UserID = targetUserID
			device.NickName = nickName
			device.AgentID = payload.AgentID
			device.Activated = activated
			if deviceName != "" {
				device.DeviceName = deviceName
			}
			updates := map[string]interface{}{
				"user_id":   device.UserID,
				"nick_name": device.NickName,
				"agent_id":  device.AgentID,
				"activated": device.Activated,
			}
			if deviceName != "" {
				updates["device_name"] = device.DeviceName
			}
			if err := updateDeviceColumns(svc.DB, device.ID, updates); err != nil {
				return nil, err
			}
			return svc.Get(scope, device.ID)
		} else if err != gorm.ErrRecordNotFound {
			return nil, err
		}
	}

	device = models.Device{
		UserID:     targetUserID,
		NickName:   nickName,
		DeviceCode: deviceCode,
		DeviceName: deviceName,
		AgentID:    payload.AgentID,
		Activated:  activated,
	}
	if err := svc.DB.Create(&device).Error; err != nil {
		return nil, err
	}
	return svc.Get(scope, device.ID)
}

func (svc *DeviceService) Update(scope accessScope, id uint, payload DevicePayload) (*DeviceResponse, error) {
	var device models.Device
	query := svc.DB.Where("id = ?", id)
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.First(&device).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("设备不存在或不属于当前用户")
		}
		return nil, err
	}

	nextUserID := device.UserID
	if scope.IsAdmin && payload.UserID > 0 {
		nextUserID = payload.UserID
	}
	if err := svc.assertUserExists(nextUserID); err != nil {
		return nil, err
	}
	nickName, err := normalizeDeviceNickName(payload.NickName)
	if err != nil {
		return nil, err
	}
	if nickName == "" {
		nickName = device.NickName
	}
	if !scope.IsAdmin && nickName == "" {
		return nil, fmt.Errorf("设备昵称不能为空")
	}
	nextAgentID := device.AgentID
	if payload.AgentID > 0 || scope.IsAdmin {
		nextAgentID = payload.AgentID
	}
	if nextAgentID > 0 {
		if err := svc.assertAgentOwnedByUser(nextAgentID, nextUserID); err != nil {
			return nil, err
		}
	}

	updates := map[string]interface{}{
		"user_id":   nextUserID,
		"nick_name": nickName,
		"agent_id":  nextAgentID,
	}
	device.UserID = nextUserID
	device.NickName = nickName
	device.AgentID = nextAgentID
	if scope.IsAdmin {
		device.DeviceCode = strings.TrimSpace(payload.DeviceCode)
		device.DeviceName = strings.TrimSpace(payload.DeviceName)
		if payload.Activated != nil {
			device.Activated = *payload.Activated
		}
		updates["device_code"] = device.DeviceCode
		updates["device_name"] = device.DeviceName
		updates["activated"] = device.Activated
	}
	if err := updateDeviceColumns(svc.DB, device.ID, updates); err != nil {
		return nil, err
	}
	return svc.Get(scope, device.ID)
}

func (svc *DeviceService) Get(scope accessScope, id uint) (*DeviceResponse, error) {
	var device models.Device
	query := svc.DB.Where("id = ?", id)
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.First(&device).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("设备不存在")
		}
		return nil, err
	}
	items, err := svc.enrichDevices(scope, []models.Device{device})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("设备不存在")
	}
	return &items[0], nil
}

func (svc *DeviceService) Delete(scope accessScope, id uint) error {
	query := svc.DB.Where("id = ?", id)
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	var device models.Device
	if err := query.First(&device).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("设备不存在或不属于当前用户")
		}
		return err
	}
	return svc.DB.Delete(&device).Error
}

func (svc *DeviceService) BindToAgent(scope accessScope, agentID uint, payload DevicePayload) (*DeviceResponse, error) {
	var agent models.Agent
	query := svc.DB.Where("id = ?", agentID)
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("智能体不存在")
		}
		return nil, err
	}

	code := strings.TrimSpace(payload.Code)
	deviceName := strings.TrimSpace(payload.DeviceMAC)
	if deviceName == "" {
		deviceName = strings.TrimSpace(payload.DeviceName)
	}
	if code == "" && deviceName == "" {
		return nil, fmt.Errorf("请填写设备验证码或设备MAC")
	}
	if code != "" && !isSixDigitCode(code) {
		return nil, fmt.Errorf("验证码格式错误")
	}
	nickName, err := normalizeDeviceNickName(payload.NickName)
	if err != nil {
		return nil, err
	}

	var device models.Device
	deviceExists := true
	if code != "" {
		if err := svc.DB.Where("device_code = ?", code).First(&device).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				deviceExists = false
			} else {
				return nil, err
			}
		}
	} else {
		normalizedDeviceName := normalizeDeviceNameCandidate(deviceName)
		if err := svc.DB.Where("LOWER(REPLACE(device_name, '-', ':')) = ?", normalizedDeviceName).First(&device).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				deviceExists = false
			} else {
				return nil, err
			}
		}
	}

	if deviceExists {
		if device.UserID != 0 && device.UserID != agent.UserID {
			if code != "" {
				return nil, fmt.Errorf("验证码无效或设备已被绑定")
			}
			return nil, fmt.Errorf("设备MAC无效或设备已被绑定")
		}
		device.UserID = agent.UserID
		device.AgentID = agent.ID
		device.Activated = true
		if nickName != "" {
			device.NickName = nickName
		} else if strings.TrimSpace(device.NickName) == "" {
			device.NickName = strings.TrimSpace(device.DeviceName)
		}
		if err := updateDeviceColumns(svc.DB, device.ID, map[string]interface{}{
			"user_id":   device.UserID,
			"agent_id":  device.AgentID,
			"activated": device.Activated,
			"nick_name": device.NickName,
		}); err != nil {
			return nil, err
		}
		return svc.Get(accessScope{ActorUserID: agent.UserID, IsAdmin: scope.IsAdmin}, device.ID)
	}

	device = models.Device{
		UserID:     agent.UserID,
		AgentID:    agent.ID,
		NickName:   nickName,
		DeviceCode: code,
		DeviceName: deviceName,
		Activated:  true,
	}
	if device.NickName == "" {
		device.NickName = strings.TrimSpace(device.DeviceName)
	}
	if err := svc.DB.Create(&device).Error; err != nil {
		return nil, err
	}
	return svc.Get(accessScope{ActorUserID: agent.UserID, IsAdmin: scope.IsAdmin}, device.ID)
}

func (svc *DeviceService) UnbindFromAgent(scope accessScope, agentID uint, deviceID uint) error {
	var agent models.Agent
	query := svc.DB.Where("id = ?", agentID)
	if !scope.IsAdmin {
		query = query.Where("user_id = ?", scope.ActorUserID)
	}
	if err := query.First(&agent).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("智能体不存在")
		}
		return err
	}

	var device models.Device
	if err := svc.DB.Where("id = ? AND user_id = ? AND agent_id = ?", deviceID, agent.UserID, agent.ID).First(&device).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("设备不存在或不属于此智能体")
		}
		return err
	}
	return updateDeviceColumns(svc.DB, device.ID, map[string]interface{}{"agent_id": 0})
}

func (svc *DeviceService) enrichDevices(scope accessScope, devices []models.Device) ([]DeviceResponse, error) {
	if len(devices) == 0 {
		return []DeviceResponse{}, nil
	}
	userIDs := make([]uint, 0)
	agentIDs := make([]uint, 0)
	roleIDs := make([]uint, 0)
	for _, device := range devices {
		userIDs = appendUniqueUint(userIDs, device.UserID)
		if device.AgentID > 0 {
			agentIDs = appendUniqueUint(agentIDs, device.AgentID)
		}
		if device.RoleID != nil {
			roleIDs = appendUniqueUint(roleIDs, *device.RoleID)
		}
	}

	usernameByID := make(map[uint]string)
	if scope.IsAdmin && len(userIDs) > 0 {
		var users []models.User
		if err := svc.DB.Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, user := range users {
			usernameByID[user.ID] = user.Username
		}
	}

	agentNameByID := make(map[uint]string)
	if len(agentIDs) > 0 {
		var agents []models.Agent
		if err := svc.DB.Where("id IN ?", agentIDs).Find(&agents).Error; err != nil {
			return nil, err
		}
		for _, agent := range agents {
			agentNameByID[agent.ID] = agent.Name
		}
	}

	roleByID := make(map[uint]models.Role)
	if len(roleIDs) > 0 {
		var roles []models.Role
		if err := svc.DB.Where("id IN ?", roleIDs).Find(&roles).Error; err != nil {
			return nil, err
		}
		for _, role := range roles {
			roleByID[role.ID] = role
		}
	}

	result := make([]DeviceResponse, 0, len(devices))
	for _, device := range devices {
		item := DeviceResponse{
			Device:    device,
			AgentName: agentNameByID[device.AgentID],
			Username:  usernameByID[device.UserID],
		}
		if device.RoleID != nil {
			if role, ok := roleByID[*device.RoleID]; ok {
				item.RoleName = role.Name
				item.RoleType = role.RoleType
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func (svc *DeviceService) assertUserExists(userID uint) error {
	var count int64
	if err := svc.DB.Model(&models.User{}).Where("id = ?", userID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("指定的用户不存在")
	}
	return nil
}

func (svc *DeviceService) assertAgentOwnedByUser(agentID uint, userID uint) error {
	var count int64
	if err := svc.DB.Model(&models.Agent{}).Where("id = ? AND user_id = ?", agentID, userID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("智能体不存在或不属于指定用户")
	}
	return nil
}

func writeServiceError(c *gin.Context, err error, fallback string) {
	if err == nil {
		return
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		msg = fallback
	}
	status := http.StatusBadRequest
	if strings.Contains(msg, "不存在") {
		status = http.StatusNotFound
	}
	if strings.Contains(msg, "无权") || strings.Contains(msg, "越权") {
		status = http.StatusForbidden
	}
	c.JSON(status, gin.H{"error": msg})
}

func normalizeASRSpeed(speed string) string {
	switch strings.ToLower(strings.TrimSpace(speed)) {
	case "patient":
		return "patient"
	case "fast":
		return "fast"
	default:
		return "normal"
	}
}

func normalizeMemoryModeFromPtr(mode *string) string {
	if mode == nil {
		return "short"
	}
	return normalizeAgentMemoryMode(*mode)
}

func normalizeSpeakerChatModeFromPtr(mode *string) string {
	if mode == nil {
		return "off"
	}
	return normalizeAgentSpeakerChatMode(*mode)
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func safeStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func resolvePayloadOpenClawConfig(base OpenClawConfigResponse, payload AgentPayload) OpenClawConfigResponse {
	switch {
	case payload.OpenClaw != nil:
		return normalizeOpenClawConfig(*payload.OpenClaw)
	case payload.OpenClawConfig != nil:
		return parseOpenClawConfig(*payload.OpenClawConfig)
	default:
		return mergeOpenClawConfig(base, nil)
	}
}

func normalizeAgentMCPServices(db *gorm.DB, raw string) (string, error) {
	options, err := listEnabledGlobalMCPServiceNames(db)
	if err != nil {
		return "", err
	}
	return validateMCPServiceNamesCSV(raw, buildMCPServiceNameSet(options))
}

func replaceAgentKnowledgeBaseLinks(db *gorm.DB, agentID uint, knowledgeBaseIDs []uint) error {
	if err := db.Where("agent_id = ?", agentID).Delete(&models.AgentKnowledgeBase{}).Error; err != nil {
		return err
	}
	for _, kbID := range uniqueUintSlice(knowledgeBaseIDs) {
		if err := db.Create(&models.AgentKnowledgeBase{AgentID: agentID, KnowledgeBaseID: kbID}).Error; err != nil {
			return err
		}
	}
	return nil
}

func appendUniqueUint(values []uint, value uint) []uint {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func mapValues(values map[string]string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func parseUintParam(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的ID"})
		return 0, false
	}
	return uint(id), true
}

func getVoiceOptionsForUser(db *gorm.DB, c *gin.Context, targetUserID uint, provider, configID, overrideURL, overrideAPIKey, overrideModel string) ([]VoiceOption, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, fmt.Errorf("provider参数必填")
	}

	var systemVoices []VoiceOption
	if provider == "indextts_vllm" {
		uc := &UserController{DB: db}
		voices, err := uc.fetchIndexTTSVoices(c, configID, overrideURL, overrideAPIKey)
		if err != nil {
			return nil, fmt.Errorf("获取IndexTTS音色失败: %w", err)
		}
		systemVoices = voices
	} else if provider == "aliyun_qwen" {
		if model := strings.TrimSpace(overrideModel); model != "" {
			systemVoices = GetAliyunQwenVoicesByModel(model)
		} else if configID == "" {
			systemVoices = GetVoiceOptionsByProvider("aliyun_qwen")
		} else {
			var cfg models.Config
			if err := db.Where("type = ? AND config_id = ?", "tts", configID).First(&cfg).Error; err != nil {
				return nil, fmt.Errorf("未找到对应的TTS配置")
			}
			var qc struct {
				Model string `json:"model"`
			}
			if cfg.JsonData != "" {
				_ = json.Unmarshal([]byte(cfg.JsonData), &qc)
			}
			if qc.Model == "" {
				qc.Model = "qwen3-tts-flash"
			}
			systemVoices = GetAliyunQwenVoicesByModel(qc.Model)
		}
	} else {
		systemVoices = append([]VoiceOption(nil), GetVoiceOptionsByProvider(provider)...)
		if provider == "openai" && configID != "" && db != nil {
			var cfg models.Config
			if err := db.Where("type = ? AND config_id = ?", "tts", configID).First(&cfg).Error; err == nil && cfg.JsonData != "" {
				var oc struct {
					Voice string `json:"voice"`
				}
				if err := json.Unmarshal([]byte(cfg.JsonData), &oc); err == nil {
					if customVoice := strings.TrimSpace(oc.Voice); customVoice != "" {
						systemVoices = append(systemVoices, VoiceOption{Value: customVoice, Label: customVoice})
					}
				}
			}
		}
	}

	result := make([]VoiceOption, 0, len(systemVoices)+8)
	seen := make(map[string]bool, len(systemVoices)+8)
	for _, voice := range systemVoices {
		key := strings.TrimSpace(voice.Value)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, voice)
	}

	if targetUserID > 0 && configID != "" {
		var clones []models.VoiceClone
		if err := db.Where("user_id = ? AND provider = ? AND tts_config_id = ? AND status = ?", targetUserID, provider, configID, "active").Order("created_at DESC").Find(&clones).Error; err == nil {
			for _, clone := range clones {
				opt := BuildVoiceOptionForClone(clone)
				key := strings.TrimSpace(opt.Value)
				if key == "" {
					continue
				}
				if seen[key] {
					for i := range result {
						if strings.TrimSpace(result[i].Value) == key {
							result = append(result[:i], result[i+1:]...)
							break
						}
					}
				}
				seen[key] = true
				result = append(result, opt)
			}
		}

		var sharedClones []models.VoiceClone
		if err := db.Table("voice_clones").
			Select("voice_clones.*").
			Joins("JOIN users ON users.id = voice_clones.user_id").
			Where("voice_clones.user_id <> ? AND voice_clones.provider = ? AND voice_clones.tts_config_id = ? AND voice_clones.status = ? AND voice_clones.shared_to_all = ? AND users.role = ?",
				targetUserID, provider, configID, "active", true, "admin").
			Order("voice_clones.created_at DESC").
			Scan(&sharedClones).Error; err == nil {
			for _, clone := range sharedClones {
				opt := VoiceOption{
					Value: clone.ProviderVoiceID,
					Label: fmt.Sprintf("[管理员共享] %s (%s)", clone.Name, clone.ProviderVoiceID),
				}
				key := strings.TrimSpace(opt.Value)
				if key == "" || seen[key] {
					continue
				}
				seen[key] = true
				result = append(result, opt)
			}
		}
	}

	return result, nil
}

func getVoiceClonesForUser(db *gorm.DB, targetUserID uint, ttsConfigID string) ([]gin.H, error) {
	query := db.Model(&models.VoiceClone{}).Where("user_id = ? AND status != ?", targetUserID, "deleted")
	if strings.TrimSpace(ttsConfigID) != "" {
		query = query.Where("tts_config_id = ?", strings.TrimSpace(ttsConfigID))
	}

	var clones []models.VoiceClone
	if err := query.Order("created_at DESC").Find(&clones).Error; err != nil {
		return nil, err
	}
	if len(clones) == 0 {
		return []gin.H{}, nil
	}

	ttsConfigIDSet := make(map[string]bool, len(clones))
	for _, clone := range clones {
		if strings.TrimSpace(clone.TTSConfigID) != "" {
			ttsConfigIDSet[clone.TTSConfigID] = true
		}
	}
	ttsConfigNames := make(map[string]string, len(ttsConfigIDSet))
	ttsConfigProviders := make(map[string]string, len(ttsConfigIDSet))
	if len(ttsConfigIDSet) > 0 {
		ids := make([]string, 0, len(ttsConfigIDSet))
		for id := range ttsConfigIDSet {
			ids = append(ids, id)
		}
		var ttsConfigs []models.Config
		if err := db.Where("type = ? AND config_id IN ?", "tts", ids).Find(&ttsConfigs).Error; err == nil {
			for _, ttsConfig := range ttsConfigs {
				ttsConfigNames[ttsConfig.ConfigID] = strings.TrimSpace(ttsConfig.Name)
				ttsConfigProviders[ttsConfig.ConfigID] = strings.TrimSpace(ttsConfig.Provider)
			}
		}
	}

	result := make([]gin.H, 0, len(clones))
	for _, clone := range clones {
		item := gin.H{
			"id":                clone.ID,
			"user_id":           clone.UserID,
			"name":              clone.Name,
			"provider":          clone.Provider,
			"provider_voice_id": clone.ProviderVoiceID,
			"tts_config_id":     clone.TTSConfigID,
			"tts_config_name":   clone.TTSConfigID,
			"shared_to_all":     clone.SharedToAll,
			"status":            clone.Status,
			"created_at":        clone.CreatedAt,
			"updated_at":        clone.UpdatedAt,
		}
		if name, ok := ttsConfigNames[clone.TTSConfigID]; ok && name != "" {
			item["tts_config_name"] = name
		}
		if provider, ok := ttsConfigProviders[clone.TTSConfigID]; ok && provider != "" {
			item["tts_provider"] = provider
		}
		result = append(result, item)
	}
	return result, nil
}
