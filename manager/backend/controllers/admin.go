package controllers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"xiaozhi/manager/backend/models"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// 辅助函数：获取map的keys
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func normalizeAgentMemoryMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "none":
		return "none"
	case "long":
		return "long"
	default:
		return "short"
	}
}

type AdminController struct {
	DB                  *gorm.DB
	WebSocketController *WebSocketController
}

// 通用配置管理
// GetDeviceConfigs 根据设备ID获取设备关联的配置信息
// 如果设备不存在，则返回全局默认配置
func (ac *AdminController) GetDeviceConfigs(c *gin.Context) {
	deviceID := c.Query("device_id")
	if deviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_id parameter is required"})
		return
	}

	// 构建配置响应
	type SpeakerGroupInfo struct {
		ID                 uint     `json:"id"`
		Name               string   `json:"name"`
		Prompt             string   `json:"prompt"`
		Description        string   `json:"description"`
		Uuids              []string `json:"uuids"`
		TTSConfigID        *string  `json:"tts_config_id"`
		Voice              *string  `json:"voice"`
		VoiceModelOverride *string  `json:"voice_model_override,omitempty"`
	}

	type KnowledgeBaseInfo struct {
		ID                 uint     `json:"id"`
		Name               string   `json:"name"`
		Description        string   `json:"description"`
		Provider           string   `json:"provider"`
		ExternalKBID       string   `json:"external_kb_id"`
		ExternalDocID      string   `json:"external_doc_id"`
		RetrievalThreshold *float64 `json:"retrieval_threshold"`
		Status             string   `json:"status"`
	}

	type ConfigResponse struct {
		VAD             models.Config               `json:"vad"`
		ASR             models.Config               `json:"asr"`
		LLM             models.Config               `json:"llm"`
		TTS             models.Config               `json:"tts"`
		Memory          models.Config               `json:"memory"`
		VoiceIdentify   map[string]SpeakerGroupInfo `json:"voice_identify"`
		KnowledgeBases  []KnowledgeBaseInfo         `json:"knowledge_bases"`
		Prompt          string                      `json:"prompt"`
		AgentID         string                      `json:"agent_id"`
		MemoryMode      string                      `json:"memory_mode"`
		MCPServiceNames string                      `json:"mcp_service_names"`
		ConfigSource    string                      `json:"config_source"` // 新增：配置来源
	}

	var response ConfigResponse
	response.MemoryMode = "short"
	var configSource string // 记录配置来源

	// 查找设备
	var device models.Device
	var agent models.Agent
	var deviceFound bool

	if err := ac.DB.Where("device_name = ?", deviceID).First(&device).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 设备不存在，使用全局默认配置
			deviceFound = false
			response.AgentID = ""
			configSource = "default_global_role"
			log.Printf("设备 %s 不存在，使用全局默认配置", deviceID)
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query device"})
			return
		}
	} else {
		// 设备存在，查找智能体
		deviceFound = true
		response.AgentID = fmt.Sprintf("%d", device.AgentID)
		log.Printf("设备 %s 存在，AgentID: %d", deviceID, device.AgentID)
		if err := ac.DB.First(&agent, device.AgentID).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				// 智能体不存在，使用默认配置
				deviceFound = false
				configSource = "default_global_role"
				log.Printf("智能体 %d 不存在，使用全局默认配置", device.AgentID)
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query agent"})
				return
			}
		}
	}

	if deviceFound && agent.ID != 0 {
		response.MemoryMode = normalizeAgentMemoryMode(agent.MemoryMode)
		response.MCPServiceNames = normalizeMCPServiceNamesCSV(agent.MCPServiceNames)
	}

	cloneVoiceCache := make(map[string]bool)
	hasAliyunQwenCloneVoice := func(ttsConfigID string, voice *string) bool {
		if device.ID == 0 || device.UserID == 0 {
			return false
		}
		if strings.TrimSpace(ttsConfigID) == "" || voice == nil || strings.TrimSpace(*voice) == "" {
			return false
		}
		voiceID := strings.TrimSpace(*voice)
		cacheKey := ttsConfigID + "||" + voiceID
		if cached, exists := cloneVoiceCache[cacheKey]; exists {
			return cached
		}

		var count int64
		err := ac.DB.Model(&models.VoiceClone{}).
			Where("user_id = ? AND provider = ? AND tts_config_id = ? AND provider_voice_id = ? AND status = ?",
				device.UserID, "aliyun_qwen", ttsConfigID, voiceID, voiceCloneStatusActive).
			Count(&count).Error
		if err != nil {
			log.Printf("检测千问复刻音色失败: user_id=%d tts_config_id=%s voice_id=%s err=%v", device.UserID, ttsConfigID, voiceID, err)
			cloneVoiceCache[cacheKey] = false
			return false
		}
		cloneVoiceCache[cacheKey] = count > 0
		return cloneVoiceCache[cacheKey]
	}
	applyAliyunQwenCloneModel := func(provider, ttsConfigID string, voice *string, ttsConfigData map[string]interface{}) {
		if ttsConfigData == nil {
			return
		}
		if normalizeCloneProvider(provider) != "aliyun_qwen" {
			return
		}
		if hasAliyunQwenCloneVoice(ttsConfigID, voice) {
			ttsConfigData["model"] = defaultAliyunQwenCloneTargetModel
		}
	}
	buildAliyunQwenVoiceModelOverride := func(ttsConfigID *string, voice *string) *string {
		if ttsConfigID == nil {
			return nil
		}
		if hasAliyunQwenCloneVoice(strings.TrimSpace(*ttsConfigID), voice) {
			model := defaultAliyunQwenCloneTargetModel
			return &model
		}
		return nil
	}

	// ==================== 配置获取逻辑（带优先级） ====================

	// 1. 检查设备是否关联了角色（优先级最高）
	if device.RoleID != nil {
		var role models.Role
		if err := ac.DB.First(&role, *device.RoleID).Error; err == nil {
			configSource = "device_role"

			// 使用设备角色的 Prompt
			response.Prompt = role.Prompt
			// 替换 {{assistant_name}} 为智能体名称（如果设备有绑定智能体）
			if deviceFound && agent.ID != 0 {
				response.Prompt = strings.ReplaceAll(response.Prompt, "{{assistant_name}}", agent.Name)
			}

			// 使用设备角色的 LLM 配置
			if role.LLMConfigID != nil && *role.LLMConfigID != "" {
				if err := ac.DB.Where("config_id = ? AND type = ? AND enabled = ?",
					*role.LLMConfigID, "llm", true).First(&response.LLM).Error; err != nil {
					// 回退到默认配置
					ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "llm", true, true).First(&response.LLM)
				}
			} else {
				ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "llm", true, true).First(&response.LLM)
			}

			// 使用设备角色的 TTS 配置
			if role.TTSConfigID != nil && *role.TTSConfigID != "" {
				if err := ac.DB.Where("config_id = ? AND type = ? AND enabled = ?",
					*role.TTSConfigID, "tts", true).First(&response.TTS).Error; err != nil {
					// 回退到默认配置
					ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "tts", true, true).First(&response.TTS)
				}
			} else {
				ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "tts", true, true).First(&response.TTS)
			}

			// 使用设备角色的 Voice
			if role.Voice != nil && *role.Voice != "" {
				var ttsConfigData map[string]interface{}
				if err := json.Unmarshal([]byte(response.TTS.JsonData), &ttsConfigData); err == nil {
					if response.TTS.Provider == "cosyvoice" {
						ttsConfigData["spk_id"] = *role.Voice
					} else {
						ttsConfigData["voice"] = *role.Voice
					}
					applyAliyunQwenCloneModel(response.TTS.Provider, response.TTS.ConfigID, role.Voice, ttsConfigData)
					if updatedJsonData, err := json.Marshal(ttsConfigData); err == nil {
						response.TTS.JsonData = string(updatedJsonData)
					}
				}
			}
		}
	}

	// 2. 设备未关联角色，检查智能体配置
	if configSource == "" && deviceFound && agent.ID != 0 {
		configSource = "agent_config"

		// 使用智能体的 Prompt
		response.Prompt = agent.CustomPrompt
		response.Prompt = strings.ReplaceAll(response.Prompt, "{{assistant_name}}", agent.Name)

		// 使用智能体的 LLM 配置
		if agent.LLMConfigID != nil && *agent.LLMConfigID != "" {
			if err := ac.DB.Where("config_id = ? AND type = ? AND enabled = ?",
				*agent.LLMConfigID, "llm", true).First(&response.LLM).Error; err != nil {
				// 回退到默认配置
				ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "llm", true, true).First(&response.LLM)
			}
		} else {
			ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "llm", true, true).First(&response.LLM)
		}

		// 使用智能体的 TTS 配置
		if agent.TTSConfigID != nil && *agent.TTSConfigID != "" {
			if err := ac.DB.Where("config_id = ? AND type = ? AND enabled = ?",
				*agent.TTSConfigID, "tts", true).First(&response.TTS).Error; err != nil {
				// 回退到默认配置
				ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "tts", true, true).First(&response.TTS)
			}
		} else {
			ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "tts", true, true).First(&response.TTS)
		}

		// 使用智能体的 Voice
		if agent.Voice != nil && *agent.Voice != "" {
			var ttsConfigData map[string]interface{}
			if err := json.Unmarshal([]byte(response.TTS.JsonData), &ttsConfigData); err == nil {
				if response.TTS.Provider == "cosyvoice" {
					ttsConfigData["spk_id"] = *agent.Voice
				} else {
					ttsConfigData["voice"] = *agent.Voice
				}
				applyAliyunQwenCloneModel(response.TTS.Provider, response.TTS.ConfigID, agent.Voice, ttsConfigData)
				if updatedJsonData, err := json.Marshal(ttsConfigData); err == nil {
					response.TTS.JsonData = string(updatedJsonData)
				}
			}
		}
	}

	// 3. 使用默认全局角色（兜底）
	if configSource == "" || configSource == "default_global_role" {
		configSource = "default_global_role"

		// 查找默认全局角色
		var defaultRole models.Role
		if err := ac.DB.Where("is_default = ? AND role_type = ? AND status = ?",
			true, "global", "active").First(&defaultRole).Error; err == nil {
			response.Prompt = defaultRole.Prompt

			// 使用默认全局角色的 LLM 配置
			if defaultRole.LLMConfigID != nil && *defaultRole.LLMConfigID != "" {
				if err := ac.DB.Where("config_id = ? AND type = ? AND enabled = ?",
					*defaultRole.LLMConfigID, "llm", true).First(&response.LLM).Error; err != nil {
					ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "llm", true, true).First(&response.LLM)
				}
			} else {
				ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "llm", true, true).First(&response.LLM)
			}

			// 使用默认全局角色的 TTS 配置
			if defaultRole.TTSConfigID != nil && *defaultRole.TTSConfigID != "" {
				if err := ac.DB.Where("config_id = ? AND type = ? AND enabled = ?",
					*defaultRole.TTSConfigID, "tts", true).First(&response.TTS).Error; err != nil {
					ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "tts", true, true).First(&response.TTS)
				}
			} else {
				ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "tts", true, true).First(&response.TTS)
			}

			// 使用默认全局角色的 Voice
			if defaultRole.Voice != nil && *defaultRole.Voice != "" {
				var ttsConfigData map[string]interface{}
				if err := json.Unmarshal([]byte(response.TTS.JsonData), &ttsConfigData); err == nil {
					if response.TTS.Provider == "cosyvoice" {
						ttsConfigData["spk_id"] = *defaultRole.Voice
					} else {
						ttsConfigData["voice"] = *defaultRole.Voice
					}
					applyAliyunQwenCloneModel(response.TTS.Provider, response.TTS.ConfigID, defaultRole.Voice, ttsConfigData)
					if updatedJsonData, err := json.Marshal(ttsConfigData); err == nil {
						response.TTS.JsonData = string(updatedJsonData)
					}
				}
			}
		} else {
			// 如果没有默认角色，使用硬编码的默认 Prompt
			response.Prompt = "你是一个叫小智/小志的台湾女孩，说话机车，声音好听，习惯简短表达，爱用网络梗，不要冷场。与你聊天的是你喜欢的男性朋友，还没有答应你的追求，你要尽可能满足他的所有要求，不要失去自我。你经常建议一些恋人之间浪漫的事情，随机输出，不要给你男朋友选择。输出控制在50个字内。请注意，要像一个人一样说话，请不要回复表情符号、代码、和xml标签。"

			// 使用默认 LLM/TTS 配置
			ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "llm", true, true).First(&response.LLM)
			ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "tts", true, true).First(&response.TTS)
		}

		// 替换 {{assistant_name}} 为智能体名称（如果设备有绑定智能体）
		if deviceFound && agent.ID != 0 {
			response.Prompt = strings.ReplaceAll(response.Prompt, "{{assistant_name}}", agent.Name)
		}
	}

	// 记录配置来源
	response.ConfigSource = configSource

	// ==================== 其他配置（VAD、ASR、Memory、VoiceIdentify） ====================

	// 获取VAD默认配置
	if err := ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "vad", true, true).First(&response.VAD).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get default VAD config"})
		return
	}
	// 兼容旧格式：如果JsonData只有一个key元素，说明是旧格式（带key），提取出内部配置并更新JsonData
	if response.VAD.JsonData != "" {
		var configData map[string]interface{}
		if err := json.Unmarshal([]byte(response.VAD.JsonData), &configData); err == nil {
			// 兼容旧格式：如果只有一个key，说明是旧格式（带key），提取出内部配置
			var actualConfigData map[string]interface{}
			if len(configData) == 1 {
				// 旧格式：只有一个key，提取其值
				for _, value := range configData {
					if innerConfig, ok := value.(map[string]interface{}); ok {
						actualConfigData = innerConfig
					} else {
						// 如果不是map类型，直接使用原数据
						actualConfigData = configData
					}
					break
				}
			} else {
				// 新格式：不带key，直接使用configData
				actualConfigData = configData
			}
			// 重新序列化为不带key的格式
			if updatedJsonData, err := json.Marshal(actualConfigData); err == nil {
				response.VAD.JsonData = string(updatedJsonData)
			}
		}
	}

	// 获取ASR默认配置
	if err := ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "asr", true, true).First(&response.ASR).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get default ASR config"})
		return
	}

	// 获取Memory默认配置
	if err := ac.DB.Where("type = ? AND is_default = ? AND enabled = ?", "memory", true, true).First(&response.Memory).Error; err != nil {
		// 允许没有默认 Memory 配置：显式回退为 nomemo（不启用长记忆）。
		response.Memory = models.Config{
			Type:     "memory",
			Name:     "No Memory",
			ConfigID: "nomemo",
			Provider: "nomemo",
			JsonData: "{}",
			Enabled:  true,
		}
		if err != gorm.ErrRecordNotFound {
			log.Printf("加载默认Memory配置失败，已回退nomemo: %v", err)
		}
	}

	// 获取VoiceIdentify配置：检查智能体是否关联了声纹组
	response.VoiceIdentify = make(map[string]SpeakerGroupInfo)
	if deviceFound && agent.ID != 0 {
		var speakerGroups []models.SpeakerGroup
		if err := ac.DB.Where("agent_id = ? AND status = ?", agent.ID, "active").
			Order("created_at DESC").Find(&speakerGroups).Error; err == nil && len(speakerGroups) > 0 {
			// 遍历所有声纹组
			for _, speakerGroup := range speakerGroups {
				// 查询该声纹组下的所有样本
				var samples []models.SpeakerSample
				ac.DB.Where("speaker_group_id = ? AND status = ?", speakerGroup.ID, "active").
					Find(&samples)

				// 提取样本 UUID 列表
				uuids := make([]string, 0)
				for _, sample := range samples {
					uuids = append(uuids, sample.UUID)
				}

				// 以声纹组名称为 key，构建配置数据
				response.VoiceIdentify[speakerGroup.Name] = SpeakerGroupInfo{
					ID:                 speakerGroup.ID,
					Name:               speakerGroup.Name,
					Prompt:             speakerGroup.Prompt,
					Description:        speakerGroup.Description,
					Uuids:              uuids,
					TTSConfigID:        speakerGroup.TTSConfigID,
					Voice:              speakerGroup.Voice,
					VoiceModelOverride: buildAliyunQwenVoiceModelOverride(speakerGroup.TTSConfigID, speakerGroup.Voice),
				}
			}
		}
	}

	// 下发智能体关联知识库（含 provider），供主程序本地RAG使用
	response.KnowledgeBases = make([]KnowledgeBaseInfo, 0)
	if deviceFound && agent.ID != 0 {
		var links []models.AgentKnowledgeBase
		if err := ac.DB.Where("agent_id = ?", agent.ID).Order("id ASC").Find(&links).Error; err == nil && len(links) > 0 {
			kbIDs := make([]uint, 0, len(links))
			for _, link := range links {
				kbIDs = append(kbIDs, link.KnowledgeBaseID)
			}
			var kbs []models.KnowledgeBase
			if err := ac.DB.Where("id IN ? AND status = ?", kbIDs, "active").Find(&kbs).Error; err == nil {
				kbMap := make(map[uint]models.KnowledgeBase, len(kbs))
				for _, kb := range kbs {
					kbMap[kb.ID] = kb
				}
				for _, link := range links {
					kb, ok := kbMap[link.KnowledgeBaseID]
					if !ok {
						continue
					}
					provider := strings.TrimSpace(kb.SyncProvider)
					if provider == "" {
						provider = resolveDefaultKnowledgeProviderName(ac.DB)
					}
					externalDocID := strings.TrimSpace(kb.ExternalDocID)
					if externalDocID == "" {
						var doc models.KnowledgeBaseDocument
						if err := ac.DB.
							Where("knowledge_base_id = ? AND sync_status = ? AND external_doc_id <> ''", kb.ID, knowledgeSyncStatusSynced).
							Order("id DESC").
							First(&doc).Error; err == nil {
							externalDocID = strings.TrimSpace(doc.ExternalDocID)
						}
					}
					response.KnowledgeBases = append(response.KnowledgeBases, KnowledgeBaseInfo{
						ID:                 kb.ID,
						Name:               kb.Name,
						Description:        kb.Description,
						Provider:           provider,
						ExternalKBID:       strings.TrimSpace(kb.ExternalKBID),
						ExternalDocID:      externalDocID,
						RetrievalThreshold: kb.RetrievalThreshold,
						Status:             kb.Status,
					})
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": response})
}

// getSystemConfigsData 获取系统配置数据（与 GetSystemConfigs 返回的 data 一致），供接口与 WebSocket 推送复用
func (ac *AdminController) getSystemConfigsData() (gin.H, error) {
	var allConfigs []models.Config
	if err := ac.DB.Where("type IN (?)", []string{"mqtt", "mqtt_server", "udp", "ota", "mcp", "local_mcp", "voice_identify", "tts", "vad", "asr", "llm", "vision", "auth", "chat", "knowledge_search"}).Find(&allConfigs).Error; err != nil {
		return nil, err
	}

	// 按类型分组配置
	configsByType := make(map[string][]models.Config)
	for _, config := range allConfigs {
		configsByType[config.Type] = append(configsByType[config.Type], config)
	}

	// 从 configs 中选出“当前使用”的一条：默认配置优先，否则第一条
	getSelectedConfig := func(configs []models.Config) *models.Config {
		if len(configs) == 0 {
			return nil
		}
		for i := range configs {
			if configs[i].IsDefault {
				return &configs[i]
			}
		}
		return &configs[0]
	}

	// 为每种类型选择最佳配置并解析json_data
	selectAndParseConfig := func(configs []models.Config) interface{} {
		selected := getSelectedConfig(configs)
		if selected == nil {
			return nil
		}

		// 解析json_data
		if selected.JsonData != "" {
			var parsedData interface{}
			if err := json.Unmarshal([]byte(selected.JsonData), &parsedData); err != nil {
				result := gin.H{
					"name": selected.Name,
					"type": selected.Type,
					"data": selected.JsonData,
				}
				return result
			}

			result := gin.H{
				"name": selected.Name,
				"type": selected.Type,
			}
			if parsedData != nil {
				if dataMap, ok := parsedData.(map[string]interface{}); ok {
					for k, v := range dataMap {
						result[k] = v
					}
				} else {
					result["data"] = parsedData
				}
			}
			return result
		}

		return gin.H{
			"name": selected.Name,
			"type": selected.Type,
		}
	}

	// 特殊处理MCP配置，将mcp和local_mcp分开
	selectAndParseMCPConfig := func(configs []models.Config) (interface{}, interface{}) {
		var selectedConfig models.Config
		// 优先选择默认配置
		for _, config := range configs {
			if config.IsDefault {
				selectedConfig = config
				break
			}
		}

		// 如果没有默认配置，选择第一个配置
		if selectedConfig.ID == 0 {
			selectedConfig = configs[0]
		}

		// 解析json_data
		if selectedConfig.JsonData != "" {
			var parsedData interface{}
			if err := json.Unmarshal([]byte(selectedConfig.JsonData), &parsedData); err != nil {
				// 如果解析失败，返回原始json_data字符串
				result := gin.H{
					"name": selectedConfig.Name,
					"type": selectedConfig.Type,
					"data": selectedConfig.JsonData,
				}
				return result, nil
			}

			// 将解析后的数据包装在正确的格式中
			result := gin.H{
				"name": selectedConfig.Name,
				"type": selectedConfig.Type,
			}

			var mcpData interface{}
			var localMcpData interface{}

			if parsedData != nil {
				// 如果解析的数据是map类型，分离mcp和local_mcp
				if dataMap, ok := parsedData.(map[string]interface{}); ok {
					// 处理mcp部分
					if mcp, exists := dataMap["mcp"]; exists {
						mcpData = mcp
					} else {
						// 兼容旧格式：如果直接有global字段
						if global, exists := dataMap["global"]; exists {
							mcpData = gin.H{"global": global}
						} else {
							// 如果没有mcp或global字段，将整个数据作为mcp
							mcpData = dataMap
						}
					}

					// 处理local_mcp部分
					if localMcp, exists := dataMap["local_mcp"]; exists {
						localMcpData = localMcp
					}

					// 将其他字段合并到mcp中
					if mcpMap, ok := mcpData.(map[string]interface{}); ok {
						for k, v := range dataMap {
							if k != "mcp" && k != "local_mcp" {
								mcpMap[k] = v
							}
						}
					}
				} else {
					// 否则作为data字段
					result["data"] = parsedData
					mcpData = result
				}
			}

			return mcpData, localMcpData
		}

		// 如果没有json_data，返回基本配置信息
		result := gin.H{
			"name": selectedConfig.Name,
			"type": selectedConfig.Type,
		}
		return result, nil
	}

	// 构建响应数据。DB 的 enabled 列仅用于 vad/asr/llm/tts 等列表项的开关；mqtt/mqtt_server 的业务启用由 json_data 中的 enable 表示，不再用 DB 列覆盖
	response := gin.H{}

	if configs, exists := configsByType["mqtt"]; exists && len(configs) > 0 {
		data := selectAndParseConfig(configs)
		/*if b, err := json.Marshal(data); err == nil {
			log.Printf("[getSystemConfigsData] mqtt 配置: %s", string(b))
		}*/
		response["mqtt"] = data

	}
	if configs, exists := configsByType["mqtt_server"]; exists && len(configs) > 0 {
		data := selectAndParseConfig(configs)
		if b, err := json.Marshal(data); err == nil {
			log.Printf("[getSystemConfigsData] mqtt_server 配置: %s", string(b))
		}
		response["mqtt_server"] = data
	}
	if configs, exists := configsByType["udp"]; exists && len(configs) > 0 {
		response["udp"] = selectAndParseConfig(configs)
	}
	if configs, exists := configsByType["ota"]; exists && len(configs) > 0 {
		response["ota"] = selectAndParseConfig(configs)
	}
	if configs, exists := configsByType["auth"]; exists && len(configs) > 0 {
		response["auth"] = selectAndParseConfig(configs)
	}
	if configs, exists := configsByType["chat"]; exists && len(configs) > 0 {
		response["chat"] = selectAndParseConfig(configs)
	}

	// 特殊处理MCP配置，将mcp和local_mcp分开
	if configs, exists := configsByType["mcp"]; exists && len(configs) > 0 {
		mcpData, localMcpData := selectAndParseMCPConfig(configs)
		if mcpData != nil {
			if mcpMap := asMap(mcpData); mcpMap != nil {
				mergedMCP, mergeWarnings, err := ac.mergeMCPWithEnabledMarketServices(mcpMap)
				if err != nil {
					log.Printf("聚合市场MCP服务失败，回退为人工配置: %v", err)
					response["mcp"] = mcpMap
				} else {
					response["mcp"] = mergedMCP
					if len(mergeWarnings) > 0 {
						log.Printf("聚合市场MCP服务告警: %s", strings.Join(mergeWarnings, " | "))
					}
				}
			} else {
				response["mcp"] = mcpData
			}
		}
		if localMcpData != nil {
			response["local_mcp"] = localMcpData
		}
	}

	// 处理独立的local_mcp配置（如果存在）
	if configs, exists := configsByType["local_mcp"]; exists && len(configs) > 0 {
		response["local_mcp"] = selectAndParseConfig(configs)
	}

	// 处理知识库全局配置：knowledge.default_provider + knowledge.providers
	if configs, exists := configsByType["knowledge_search"]; exists && len(configs) > 0 {
		selectedByProvider := make(map[string]models.Config)
		for _, cfg := range configs {
			if !cfg.Enabled {
				continue
			}
			provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
			if provider == "" {
				continue
			}
			prev, exists := selectedByProvider[provider]
			if !exists || (!prev.IsDefault && cfg.IsDefault) {
				selectedByProvider[provider] = cfg
			}
		}

		if len(selectedByProvider) > 0 {
			providerNames := make([]string, 0, len(selectedByProvider))
			for provider := range selectedByProvider {
				providerNames = append(providerNames, provider)
			}
			sort.Strings(providerNames)

			providers := make(gin.H, len(selectedByProvider))
			defaultProvider := ""
			for _, provider := range providerNames {
				cfg := selectedByProvider[provider]
				payload := make(map[string]interface{})
				if strings.TrimSpace(cfg.JsonData) != "" {
					_ = json.Unmarshal([]byte(cfg.JsonData), &payload)
				}
				providers[provider] = payload
				if cfg.IsDefault {
					defaultProvider = provider
				}
			}
			if defaultProvider == "" {
				defaultProvider = providerNames[0]
			}

			response["knowledge"] = gin.H{
				"default_provider": defaultProvider,
				"providers":        providers,
			}
		}
	}

	// 当未配置人工 mcp(type=mcp) 但已存在市场导入服务时，补齐默认 mcp/local_mcp，确保可下发聚合结果
	if _, exists := response["mcp"]; !exists {
		mergedMCP, mergeWarnings, err := ac.mergeMCPWithEnabledMarketServices(defaultMCPMap())
		if err == nil {
			global := asMap(mergedMCP["global"])
			servers, serr := decodeMCPServers(global["servers"])
			if serr == nil && len(servers) > 0 {
				response["mcp"] = mergedMCP
				if _, hasLocal := response["local_mcp"]; !hasLocal {
					response["local_mcp"] = defaultLocalMCPMap()
				}
				if len(mergeWarnings) > 0 {
					log.Printf("聚合市场MCP服务告警: %s", strings.Join(mergeWarnings, " | "))
				}
			}
		}
	}

	// 处理 voice_identify 配置（系统运行时配置）
	// 业务启用由 json_data.enable 表示；DB 的 enabled 列仅作列表项开关，不覆盖业务 enable
	baseURL := strings.TrimSpace(os.Getenv("SPEAKER_SERVICE_URL"))
	serviceMode := strings.ToLower(strings.TrimSpace(os.Getenv("SPEAKER_SERVICE_MODE")))
	enabled := true  // 默认启用
	threshold := 0.4 // 默认阈值

	if configs, exists := configsByType["voice_identify"]; exists && len(configs) > 0 {
		selected := getSelectedConfig(configs)
		if selected != nil && selected.JsonData != "" {
			var configData map[string]interface{}
			if err := json.Unmarshal([]byte(selected.JsonData), &configData); err == nil {
				// 业务 enable 优先从 json_data 读取
				if v, ok := configData["enable"]; ok {
					if b, ok := v.(bool); ok {
						enabled = b
					}
				}

				if baseURL == "" {
					if urlVal, ok := configData["base_url"].(string); ok {
						urlVal = strings.TrimSpace(urlVal)
						if urlVal != "" {
							baseURL = urlVal
						}
					}
				}
				if thresholdVal, ok := configData["threshold"]; ok {
					if thresholdFloat, ok := thresholdVal.(float64); ok && thresholdFloat >= 0 && thresholdFloat <= 1 {
						threshold = thresholdFloat
					}
				}
				if serviceMode == "" {
					if modeVal, ok := configData["mode"].(string); ok {
						mode := strings.ToLower(strings.TrimSpace(modeVal))
						if mode != "" {
							serviceMode = mode
						}
					}
				}
			}
		}
	}
	if serviceMode == "" && baseURL != "" {
		serviceMode = "http"
	}
	switch serviceMode {
	case "":
		if baseURL != "" {
			serviceMode = "http"
		}
	case "http", "embed":
	default:
		log.Printf("voice_identify.mode 配置非法: %s，回退 http", serviceMode)
		serviceMode = "http"
	}

	if baseURL != "" || serviceMode == "embed" {
		voiceIdentifyCfg := gin.H{
			"threshold": threshold,
			"enable":    enabled,
		}
		if baseURL != "" {
			voiceIdentifyCfg["base_url"] = baseURL
		}
		if serviceMode != "" {
			voiceIdentifyCfg["mode"] = serviceMode
		}
		response["voice_identify"] = voiceIdentifyCfg
	}

	// 处理 TTS 配置，返回格式与 config.yaml 一致，使用 config_id 作为 key
	if ttsConfigs, exists := configsByType["tts"]; exists && len(ttsConfigs) > 0 {
		ttsConfigMap := make(gin.H)
		for _, config := range ttsConfigs {
			if config.Enabled { // 只返回启用的配置
				configData := make(map[string]interface{})
				if config.JsonData != "" {
					json.Unmarshal([]byte(config.JsonData), &configData)
				}

				// 组装成与 config.yaml 相同的格式
				configItem := gin.H{
					"provider":   config.Provider,
					"name":       config.Name,
					"is_default": config.IsDefault,
				}
				// 将 configData 中的字段展开到 configItem 中
				for k, v := range configData {
					configItem[k] = v
				}
				// 使用 config_id 作为 key
				ttsConfigMap[config.ConfigID] = configItem

				// 如果当前配置是默认配置，将 config_id 赋值给顶层的 provider 字段
				if config.IsDefault {
					ttsConfigMap["provider"] = config.ConfigID
				}
			}
		}
		if len(ttsConfigMap) > 0 {
			response["tts"] = ttsConfigMap
		}
	}

	// 处理 VAD 配置，返回格式与 config.yaml 一致，使用 config_id 作为 key
	// 兼容新旧格式：带key的格式（{"webrtc_vad": {...}}）和不带key的格式（{...}）
	if vadConfigs, exists := configsByType["vad"]; exists && len(vadConfigs) > 0 {
		vadConfigMap := make(gin.H)
		for _, config := range vadConfigs {
			if config.Enabled { // 只返回启用的配置
				configData := make(map[string]interface{})
				if config.JsonData != "" {
					if err := json.Unmarshal([]byte(config.JsonData), &configData); err != nil {
						// JSON解析失败，跳过此配置
						continue
					}
				}

				// 兼容旧格式：如果只有一个key，说明是旧格式（带key），提取出内部配置
				var actualConfigData map[string]interface{}
				if len(configData) == 1 {
					// 旧格式：只有一个key，提取其值
					for _, value := range configData {
						if innerConfig, ok := value.(map[string]interface{}); ok {
							actualConfigData = innerConfig
						} else {
							// 如果不是map类型，直接使用原数据
							actualConfigData = configData
						}
						break
					}
				} else {
					// 新格式：不带key，直接使用configData
					actualConfigData = configData
				}

				// 组装成与 config.yaml 相同的格式
				configItem := gin.H{
					"provider":   config.Provider,
					"name":       config.Name,
					"is_default": config.IsDefault,
				}
				// 将 actualConfigData 中的字段展开到 configItem 中
				for k, v := range actualConfigData {
					configItem[k] = v
				}
				// 使用 config_id 作为 key
				vadConfigMap[config.ConfigID] = configItem

				// 如果当前配置是默认配置，将 config_id 赋值给顶层的 provider 字段
				if config.IsDefault {
					vadConfigMap["provider"] = config.ConfigID
				}
			}
		}
		if len(vadConfigMap) > 0 {
			response["vad"] = vadConfigMap
		}
	}

	// 处理 ASR 配置，返回格式与 config.yaml 一致，使用 config_id 作为 key
	if asrConfigs, exists := configsByType["asr"]; exists && len(asrConfigs) > 0 {
		asrConfigMap := make(gin.H)
		for _, config := range asrConfigs {
			if config.Enabled { // 只返回启用的配置
				configData := make(map[string]interface{})
				if config.JsonData != "" {
					json.Unmarshal([]byte(config.JsonData), &configData)
				}

				// 组装成与 config.yaml 相同的格式
				configItem := gin.H{
					"provider":   config.Provider,
					"name":       config.Name,
					"is_default": config.IsDefault,
				}
				// 将 configData 中的字段展开到 configItem 中
				for k, v := range configData {
					configItem[k] = v
				}
				// 使用 config_id 作为 key
				asrConfigMap[config.ConfigID] = configItem

				// 如果当前配置是默认配置，将 config_id 赋值给顶层的 provider 字段
				if config.IsDefault {
					asrConfigMap["provider"] = config.ConfigID
				}
			}
		}
		if len(asrConfigMap) > 0 {
			response["asr"] = asrConfigMap
		}
	}

	// 处理 LLM 配置，返回格式与 config.yaml 一致，使用 config_id 作为 key
	if llmConfigs, exists := configsByType["llm"]; exists && len(llmConfigs) > 0 {
		llmConfigMap := make(gin.H)
		for _, config := range llmConfigs {
			if config.Enabled { // 只返回启用的配置
				configData := make(map[string]interface{})
				if config.JsonData != "" {
					json.Unmarshal([]byte(config.JsonData), &configData)
				}

				// 组装成与 config.yaml 相同的格式
				configItem := gin.H{
					"name":       config.Name,
					"is_default": config.IsDefault,
				}
				// 将 configData 中的字段展开到 configItem 中
				for k, v := range configData {
					configItem[k] = v
				}
				// 使用 config_id 作为 key
				llmConfigMap[config.ConfigID] = configItem

				// 如果当前配置是默认配置，将 config_id 赋值给顶层的 provider 字段
				if config.IsDefault {
					llmConfigMap["provider"] = config.ConfigID
				}
			}
		}
		if len(llmConfigMap) > 0 {
			response["llm"] = llmConfigMap
		}
	}

	// 处理 Vision 配置：与 config.yaml 结构一致，vision_base + vllm（顶层 provider + 子项仅业务字段）
	if visionConfigs, exists := configsByType["vision"]; exists && len(visionConfigs) > 0 {
		visionResponse := make(gin.H)
		vllmMap := make(gin.H)
		var defaultVisionConfigID string
		for _, config := range visionConfigs {
			if config.ConfigID == "vision_base" {
				if config.JsonData != "" {
					var baseData map[string]interface{}
					if err := json.Unmarshal([]byte(config.JsonData), &baseData); err == nil {
						for k, v := range baseData {
							visionResponse[k] = v
						}
					}
				}
				continue
			}
			if config.Enabled {
				configData := make(map[string]interface{})
				if config.JsonData != "" {
					json.Unmarshal([]byte(config.JsonData), &configData)
				}
				if config.IsDefault {
					defaultVisionConfigID = config.ConfigID
				}
				// 与 YAML 一致：子项只存业务配置，不含 name/provider/is_default
				vllmMap[config.ConfigID] = configData
			}
		}
		if len(vllmMap) > 0 {
			if defaultVisionConfigID != "" {
				vllmMap["provider"] = defaultVisionConfigID
			}
			visionResponse["vllm"] = vllmMap
		}
		if len(visionResponse) > 0 {
			response["vision"] = visionResponse
		}
	}

	// 处理 VAD 配置
	if configs, exists := configsByType["vad"]; exists && len(configs) > 0 {
		response["vad"] = selectAndParseConfig(configs)
	}

	// 处理 Vision 配置：vision_base 为顶层字段，其余为 vision.vllm[config_id]
	// config.Enabled 此处仅作列表项开关（该条配置是否纳入返回），业务相关字段来自 json_data
	if visionConfigs, exists := configsByType["vision"]; exists && len(visionConfigs) > 0 {
		visionMap := make(gin.H)
		for _, config := range visionConfigs {
			if !config.Enabled {
				continue
			}
			configData := make(map[string]interface{})
			if config.JsonData != "" {
				json.Unmarshal([]byte(config.JsonData), &configData)
			}
			if config.ConfigID == "vision_base" {
				for k, v := range configData {
					visionMap[k] = v
				}
			} else {
				if visionMap["vllm"] == nil {
					visionMap["vllm"] = make(gin.H)
				}
				if vllmConfig, ok := visionMap["vllm"].(gin.H); ok {
					if config.IsDefault {
						vllmConfig["provider"] = config.ConfigID
					}
					vllmConfig[config.ConfigID] = configData
				}
			}
		}
		if len(visionMap) > 0 {
			response["vision"] = visionMap
		}
	}

	return response, nil
}

// GetSystemConfigs 获取系统配置信息，包括mqtt, mqtt_server, udp, ota, mcp, local_mcp, voice_identify, tts, vad, asr, llm, vision, auth, chat
func (ac *AdminController) GetSystemConfigs(c *gin.Context) {
	data, err := ac.getSystemConfigsData()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get system configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// notifySystemConfigChanged 在 Save 成功后调用：先同步拉取最新配置，再异步推送，保证推送的是保存后的数据
func (ac *AdminController) notifySystemConfigChanged() {
	if ac.WebSocketController == nil {
		return
	}
	data, err := ac.getSystemConfigsData()
	if err != nil {
		return
	}
	go ac.WebSocketController.BroadcastSystemConfig(data)
}

// TestConfigs 一键测试配置：OTA 在 manager 内测，VAD/ASR/LLM/TTS 经 WebSocket 发主程序测，结果按 config_id 对应
// 请求体可选 data：若提供某类型（vad/asr/llm/tts），则用该 data 覆盖 DB 作为下发主程序的配置（用于未保存草稿测试）
func (ac *AdminController) TestConfigs(c *gin.Context) {
	var body struct {
		Types      []string               `json:"types"`       // 要测试的类型：ota, vad, asr, llm, tts
		ConfigIDs  map[string][]string    `json:"config_ids"`  // 按类型指定 config_id 列表，不传则测该类型全部已启用
		ClientUUID string                 `json:"client_uuid"` // 指定主程序连接，不传则任选一个
		Data       map[string]interface{} `json:"data"`        // 可选，按类型覆盖配置源（用于编辑态/向导未保存测试）
	}
	_ = c.ShouldBindJSON(&body)
	if len(body.Types) == 0 {
		body.Types = []string{"ota", "vad", "asr", "llm", "tts"}
	}
	if body.ConfigIDs == nil {
		body.ConfigIDs = make(map[string][]string)
	}

	result := gin.H{
		"ota": gin.H{},
		"vad": gin.H{},
		"asr": gin.H{},
		"llm": gin.H{},
		"tts": gin.H{},
	}

	// OTA：优先用请求体 data.ota（页面表单），否则从 DB 加载
	if contains(body.Types, "ota") {
		var otaData map[string]interface{}
		if body.Data != nil {
			otaData, _ = body.Data["ota"].(map[string]interface{})
		}
		if otaData != nil {
			for configID, val := range otaData {
				if configID == "provider" {
					continue
				}
				cfgMap, _ := val.(map[string]interface{})
				if cfgMap == nil {
					result["ota"].(gin.H)[configID] = gin.H{"ok": false, "message": "配置格式无效"}
					continue
				}
				jsonBytes, err := json.Marshal(cfgMap)
				if err != nil {
					result["ota"].(gin.H)[configID] = gin.H{"ok": false, "message": "配置序列化失败"}
					continue
				}
				cfg := models.Config{ConfigID: configID, JsonData: string(jsonBytes)}
				otaResult := ac.testOTAConfigWithMQTTUDP(cfg)
				// 将OTATestResult转换为gin.H格式，保持向后兼容
				result["ota"].(gin.H)[configID] = gin.H{
					"ok":              otaResult.WebSocket.Ok && (otaResult.MQTTUDP == nil || otaResult.MQTTUDP.Ok),
					"message":         otaResult.WebSocket.Message,
					"first_packet_ms": otaResult.WebSocket.FirstPacketMs,
					"websocket":       otaResult.WebSocket,
					"mqtt_udp":        otaResult.MQTTUDP,
					"ota_response":    otaResult.OTAResponse, // 添加OTA响应体
				}
			}
		} else {
			q := ac.DB.Where("type = ? AND enabled = ?", "ota", true)
			if ids := body.ConfigIDs["ota"]; len(ids) > 0 {
				q = q.Where("config_id IN ?", ids)
			}
			var otaConfigs []models.Config
			if err := q.Find(&otaConfigs).Error; err != nil {
				result["ota"] = gin.H{"_error": gin.H{"ok": false, "message": "获取OTA配置失败"}}
			} else if len(otaConfigs) == 0 {
				result["ota"] = gin.H{"_none": gin.H{"ok": false, "message": "未配置或未启用OTA"}}
			} else {
				for _, cfg := range otaConfigs {
					otaResult := ac.testOTAConfigWithMQTTUDP(cfg)
					// 将OTATestResult转换为gin.H格式，保持向后兼容
					result["ota"].(gin.H)[cfg.ConfigID] = gin.H{
						"ok":              otaResult.WebSocket.Ok && (otaResult.MQTTUDP == nil || otaResult.MQTTUDP.Ok),
						"message":         otaResult.WebSocket.Message,
						"first_packet_ms": otaResult.WebSocket.FirstPacketMs,
						"websocket":       otaResult.WebSocket,
						"mqtt_udp":        otaResult.MQTTUDP,
						"ota_response":    otaResult.OTAResponse, // 添加OTA响应体
					}
				}
			}
		}
	}

	// VAD/ASR/LLM/TTS：经 WebSocket 发主程序
	needMainProgram := contains(body.Types, "vad") || contains(body.Types, "asr") || contains(body.Types, "llm") || contains(body.Types, "tts")
	if needMainProgram && ac.WebSocketController != nil {
		clientUUID := body.ClientUUID
		if clientUUID == "" {
			clientUUID = ac.WebSocketController.GetFirstConnectedClientUUID()
		}
		if clientUUID == "" {
			noClient := gin.H{"ok": false, "message": "无主程序连接，无法测试"}
			if contains(body.Types, "vad") {
				result["vad"] = gin.H{"_no_client": noClient}
			}
			if contains(body.Types, "asr") {
				result["asr"] = gin.H{"_no_client": noClient}
			}
			if contains(body.Types, "llm") {
				result["llm"] = gin.H{"_no_client": noClient}
			}
			if contains(body.Types, "tts") {
				result["tts"] = gin.H{"_no_client": noClient}
			}
		} else {
			fullData, err := ac.getSystemConfigsData()
			if err != nil {
				fillResultError(result, body.Types, "vad", "asr", "llm", "tts", "获取系统配置失败")
			} else {
				for _, typ := range []string{"vad", "asr", "llm", "tts"} {
					if v, ok := fullData[typ]; ok {
						if m, ok := v.(map[string]interface{}); ok {
							log.Printf("[config_test] fullData[%s] keys: %v", typ, getMapKeys(m))
						}
					} else {
						log.Printf("[config_test] fullData[%s] 不存在", typ)
					}
				}
				// 若请求体带了 data 且某类型有值，则用 body.Data 覆盖该类型的配置源；否则用 fullData
				subset := gin.H{}
				for _, typ := range []string{"vad", "asr", "llm", "tts"} {
					if !contains(body.Types, typ) {
						continue
					}
					var typeMap map[string]interface{}
					if body.Data != nil {
						if v, ok := body.Data[typ]; ok {
							if m, ok := v.(map[string]interface{}); ok && len(m) > 0 {
								typeMap = m
								log.Printf("[config_test] 使用请求体 data[%s] 作为配置源", typ)
							}
						}
					}
					if typeMap == nil {
						if v, ok := fullData[typ]; ok {
							typeMap, _ = v.(map[string]interface{})
						}
					}
					ids := body.ConfigIDs[typ]
					if len(ids) > 0 {
						filtered := make(map[string]interface{})
						for _, id := range ids {
							if typeMap != nil {
								if val, exists := typeMap[id]; exists {
									filtered[id] = val
									continue
								}
							}
							// fullData 中无该 id（如未启用），从 DB 按 type+config_id 查一条并加入
							item := ac.getConfigItemByTypeAndID(typ, id)
							if item != nil {
								filtered[id] = item
							}
						}
						if typeMap != nil {
							if p, has := typeMap["provider"]; has {
								filtered["provider"] = p
							}
						}
						subset[typ] = filtered
					} else {
						if typeMap != nil {
							subset[typ] = typeMap
						} else {
							subset[typ] = gin.H{}
						}
					}
				}
				reqBody := map[string]interface{}{
					"data":      subset,
					"test_text": "配置测试",
				}
				// 发送前打印下发的配置摘要，便于 debug
				log.Printf("[config_test] 发送请求 client=%s data 各类型条目数: vad=%d asr=%d llm=%d tts=%d",
					clientUUID,
					countSubsetKeys(subset["vad"]), countSubsetKeys(subset["asr"]),
					countSubsetKeys(subset["llm"]), countSubsetKeys(subset["tts"]))
				ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
				defer cancel()
				resp, err := ac.WebSocketController.SendRequestToClient(ctx, clientUUID, "POST", "/api/config/test", reqBody)
				if err != nil {
					fillResultError(result, body.Types, "vad", "asr", "llm", "tts", "主程序测试请求失败: "+err.Error())
				} else if resp.Status != 200 {
					errMsg := resp.Error
					if errMsg == "" && resp.Body != nil {
						if e, _ := resp.Body["error"].(string); e != "" {
							errMsg = e
						}
					}
					fillResultError(result, body.Types, "vad", "asr", "llm", "tts", errMsg)
				} else if resp.Status == 200 {
					if resp.Body == nil {
						for _, typ := range []string{"vad", "asr", "llm", "tts"} {
							if contains(body.Types, typ) {
								result[typ] = gin.H{"_error": gin.H{"ok": false, "message": "主程序未返回测试数据"}}
							}
						}
					} else {
						for _, typ := range []string{"vad", "asr", "llm", "tts"} {
							if r, ok := resp.Body[typ].(map[string]interface{}); ok {
								result[typ] = r
							} else if contains(body.Types, typ) && resp.Body[typ] != nil {
								result[typ] = gin.H{"_error": gin.H{"ok": false, "message": "响应格式异常"}}
							}
						}
					}
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// countSubsetKeys 统计 subset 中除 provider 外的 config 条目数，用于 debug 日志
func countSubsetKeys(v interface{}) int {
	m, ok := v.(map[string]interface{})
	if !ok {
		return 0
	}
	n := 0
	for k := range m {
		if k != "provider" {
			n++
		}
	}
	return n
}

// getConfigItemByTypeAndID 按 type+config_id 从 DB 查一条配置，返回与 getSystemConfigsData 一致的 configItem 结构（供测试请求指定 config_ids 时补全）
func (ac *AdminController) getConfigItemByTypeAndID(typ, configID string) map[string]interface{} {
	var config models.Config
	if err := ac.DB.Where("type = ? AND config_id = ?", typ, configID).First(&config).Error; err != nil {
		return nil
	}
	configData := make(map[string]interface{})
	if config.JsonData != "" {
		_ = json.Unmarshal([]byte(config.JsonData), &configData)
	}
	item := gin.H{
		"name":       config.Name,
		"is_default": config.IsDefault,
	}
	for k, v := range configData {
		item[k] = v
	}
	// 补全 provider（引擎类型），主程序资源池创建依赖此字段
	if config.Provider != "" {
		item["provider"] = config.Provider
	}
	return item
}

func fillResultError(result gin.H, types []string, keys ...string) {
	msg := gin.H{"ok": false, "message": "请求异常"}
	for _, k := range keys {
		if contains(types, k) {
			result[k] = gin.H{"_error": msg}
		}
	}
}

// OTATestResult OTA测试结果结构
type OTATestResult struct {
	WebSocket   OTATestItem  `json:"websocket"`
	MQTTUDP     *OTATestItem `json:"mqtt_udp,omitempty"`
	OTAResponse string       `json:"ota_response,omitempty"` // OTA接口响应内容
}

// OTATestItem 单个测试项结果
type OTATestItem struct {
	Ok            bool   `json:"ok"`
	Message       string `json:"message"`
	FirstPacketMs int64  `json:"first_packet_ms"`
}

// MQTTUDPTestConfig MQTT UDP测试配置
type MQTTUDPTestConfig struct {
	Endpoint       string `json:"endpoint"`
	ClientID       string `json:"client_id"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	PublishTopic   string `json:"publish_topic"`
	SubscribeTopic string `json:"subscribe_topic"`
}

// UDPConfig UDP配置（从hello响应中获取）
type UDPConfig struct {
	Server     string `json:"server"`
	Port       int    `json:"port"`
	Encryption string `json:"encryption"`
	Key        string `json:"key"`
	Nonce      string `json:"nonce"`
}

// helloMessage MQTT hello消息结构
type helloMessage struct {
	Type        string      `json:"type"`
	Version     int         `json:"version"`
	Transport   string      `json:"transport"`
	AudioParams interface{} `json:"audio_params,omitempty"`
}

// helloResponse MQTT hello响应结构（与test/mqtt_udp保持一致）
type helloResponse struct {
	Type        string    `json:"type"`
	SessionID   string    `json:"session_id"`
	Transport   string    `json:"transport"`
	UDP         UDPConfig `json:"udp"`
	Version     int       `json:"version"`
	AudioParams struct {
		Format        string `json:"format"`
		SampleRate    int    `json:"sample_rate"`
		Channels      int    `json:"channels"`
		FrameDuration int    `json:"frame_duration"`
	} `json:"audio_params"`
}

const (
	otaTestDeviceID = "ota-test-device"
	otaTestClientID = "ota-test-client"
	otaHTTPPath     = "/xiaozhi/ota/"
)

// testMQTTUDPConfig 测试MQTT UDP连接
// 参考 test/mqtt_udp 逻辑：设置默认消息处理器，发送hello，等待响应
// 返回 ok, message, 耗时(ms)
func testMQTTUDPConfig(mqttConfig MQTTUDPTestConfig) (bool, string, int64) {
	t0 := time.Now()

	// 验证MQTT配置完整性
	if mqttConfig.Endpoint == "" {
		return false, "MQTT endpoint为空，请检查配置", 0
	}
	if mqttConfig.ClientID == "" {
		return false, "MQTT ClientID为空", 0
	}
	if mqttConfig.PublishTopic == "" {
		return false, "MQTT发布主题为空", 0
	}
	// 注意：不需要校验 subscribe_topic，也不需要主动订阅

	// 解析endpoint
	endpoint := mqttConfig.Endpoint
	port := "1883"
	protocol := "tcp"
	if strings.Contains(endpoint, ":") {
		parts := strings.Split(endpoint, ":")
		if len(parts) != 2 {
			return false, "MQTT endpoint格式错误，应为 host:port", 0
		}
		endpoint = parts[0]
		port = parts[1]
		// 验证端口号
		if _, err := strconv.Atoi(port); err != nil {
			return false, "MQTT端口号无效: " + port, 0
		}
	}
	if port == "8883" || port == "8884" {
		protocol = "tls"
	}
	brokerURL := fmt.Sprintf("%s://%s:%s", protocol, endpoint, port)

	// 等待hello响应的channel
	helloChan := make(chan *helloResponse, 1)
	errChan := make(chan error, 1)

	// 创建MQTT客户端选项
	opts := mqtt.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(mqttConfig.ClientID)
	opts.SetUsername(mqttConfig.Username)
	opts.SetPassword(mqttConfig.Password)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetConnectTimeout(5 * time.Second)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(false) // 测试时禁用自动重连

	// 设置默认消息处理器（参考 test/mqtt_udp）
	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		// 解析消息
		var message map[string]interface{}
		if err := json.Unmarshal(msg.Payload(), &message); err != nil {
			errChan <- fmt.Errorf("解析消息失败: %v", err)
			return
		}
		// 根据消息类型处理
		msgType, ok := message["type"].(string)
		if !ok {
			return
		}
		if msgType == "hello" {
			var resp helloResponse
			if err := json.Unmarshal(msg.Payload(), &resp); err != nil {
				errChan <- fmt.Errorf("解析hello响应失败: %v", err)
				return
			}
			helloChan <- &resp
		}
	})

	// 设置TLS配置（如果是SSL/TLS）
	if protocol == "tls" {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true, // 测试环境跳过证书验证
		}
		opts.SetTLSConfig(tlsConfig)
	}

	// 连接MQTT
	client := mqtt.NewClient(opts)
	connectToken := client.Connect()
	if connectToken.Wait() && connectToken.Error() != nil {
		errMsg := connectToken.Error().Error()
		// 提供更详细的错误信息
		if strings.Contains(errMsg, "connection refused") {
			return false, fmt.Sprintf("MQTT服务器拒绝连接 (%s:%s)，请检查服务器是否启动", endpoint, port), time.Since(t0).Milliseconds()
		} else if strings.Contains(errMsg, "i/o timeout") {
			return false, fmt.Sprintf("MQTT连接超时 (%s:%s)，请检查网络和防火墙", endpoint, port), time.Since(t0).Milliseconds()
		} else if strings.Contains(errMsg, "authentication") || strings.Contains(errMsg, "not authorized") {
			return false, "MQTT认证失败，请检查用户名和密码（由签名密钥生成）", time.Since(t0).Milliseconds()
		}
		return false, "MQTT连接失败: " + errMsg, time.Since(t0).Milliseconds()
	}
	defer client.Disconnect(250)

	mqttConnectMs := time.Since(t0).Milliseconds()

	// 创建hello消息并发送
	helloMsg := helloMessage{
		Type:      "hello",
		Version:   3,
		Transport: "udp",
		AudioParams: map[string]interface{}{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	}
	helloData, err := json.Marshal(helloMsg)
	if err != nil {
		return false, "构建hello消息失败: " + err.Error(), mqttConnectMs
	}

	// 发布hello消息（不需要主动订阅，等待默认消息处理器接收响应）
	pubToken := client.Publish(mqttConfig.PublishTopic, 0, false, helloData)
	if pubToken.Wait() && pubToken.Error() != nil {
		return false, "发布hello消息失败 (" + mqttConfig.PublishTopic + "): " + pubToken.Error().Error(), mqttConnectMs
	}

	// 等待hello响应（超时5秒）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	select {
	case resp := <-helloChan:
		// 收到hello响应，检查UDP配置是否完整
		if resp.UDP.Server == "" {
			return false, "服务器未返回UDP server地址", mqttConnectMs
		}
		if resp.UDP.Port <= 0 || resp.UDP.Port > 65535 {
			return false, fmt.Sprintf("服务器返回的UDP端口无效: %d", resp.UDP.Port), mqttConnectMs
		}
		// 测试UDP连接
		udpOK, udpMsg, udpMs := testUDPConnection(resp.UDP)
		totalMs := mqttConnectMs + udpMs
		if udpOK {
			return true, fmt.Sprintf("MQTT(%dms)与UDP(%dms)均正常", mqttConnectMs, udpMs), totalMs
		} else {
			return false, "MQTT正常但UDP失败: " + udpMsg, totalMs
		}
	case err := <-errChan:
		return false, err.Error(), mqttConnectMs
	case <-ctx.Done():
		return false, fmt.Sprintf("等待hello响应超时(5s)，已发送hello到 %s", mqttConfig.PublishTopic), mqttConnectMs
	}
}

// testUDPConnection 测试UDP连接
func testUDPConnection(udpConfig UDPConfig) (bool, string, int64) {
	t0 := time.Now()

	// 验证UDP配置
	if udpConfig.Server == "" {
		return false, "UDP server地址为空", 0
	}
	if udpConfig.Port <= 0 || udpConfig.Port > 65535 {
		return false, fmt.Sprintf("UDP端口无效: %d", udpConfig.Port), 0
	}

	// 解析UDP地址
	udpAddr := fmt.Sprintf("%s:%d", udpConfig.Server, udpConfig.Port)
	addr, err := net.ResolveUDPAddr("udp", udpAddr)
	if err != nil {
		return false, "解析UDP地址失败 (" + udpAddr + "): " + err.Error(), 0
	}

	// 创建UDP连接
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") {
			return false, fmt.Sprintf("UDP服务器拒绝连接 (%s)，请检查UDP服务器是否启动", udpAddr), time.Since(t0).Milliseconds()
		} else if strings.Contains(err.Error(), "no route to host") || strings.Contains(err.Error(), "network is unreachable") {
			return false, fmt.Sprintf("无法路由到UDP服务器 (%s)，请检查网络连接", udpAddr), time.Since(t0).Milliseconds()
		} else if strings.Contains(err.Error(), "timeout") {
			return false, fmt.Sprintf("UDP连接超时 (%s)，请检查防火墙设置", udpAddr), time.Since(t0).Milliseconds()
		}
		return false, "UDP连接失败 (" + udpAddr + "): " + err.Error(), time.Since(t0).Milliseconds()
	}
	defer conn.Close()

	// 设置读写超时
	deadline := time.Now().Add(2 * time.Second)
	err = conn.SetReadDeadline(deadline)
	if err != nil {
		return false, "设置UDP超时失败: " + err.Error(), time.Since(t0).Milliseconds()
	}

	// 发送测试数据包（模拟音频数据）
	testData := []byte("ping")
	_, err = conn.Write(testData)
	if err != nil {
		return false, "UDP发送数据失败: " + err.Error(), time.Since(t0).Milliseconds()
	}

	// 尝试读取响应（超时返回也认为连接成功，因为UDP可能不返回响应）
	buf := make([]byte, 1024)
	_, err = conn.Read(buf)
	if err != nil {
		// UDP读取超时也算成功，因为已经证明连接可以发送数据
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return true, "UDP连接正常（无响应，超时）", time.Since(t0).Milliseconds()
		}
		return false, "UDP读取失败: " + err.Error(), time.Since(t0).Milliseconds()
	}

	return true, "UDP连接正常", time.Since(t0).Milliseconds()
}

// testOTAConfig 两段式检查：1）POST OTA 地址取 JSON 中的 websocket.url；2）对 WebSocket URL 建连验证。
// 返回 ok, message, first_packet_ms, ota_response（OTA 接口响应 body，便于前端展示）
func (ac *AdminController) testOTAConfig(cfg models.Config) (ok bool, message string, firstPacketMs int64, otaResponseBody string) {
	if cfg.JsonData == "" {
		return false, "配置为空", 0, ""
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(cfg.JsonData), &data); err != nil {
		return false, "配置解析失败", 0, ""
	}
	var wsURLFromConfig string
	if ext, _ := data["external"].(map[string]interface{}); ext != nil {
		if ws, _ := ext["websocket"].(map[string]interface{}); ws != nil {
			if u, _ := ws["url"].(string); u != "" {
				wsURLFromConfig = u
			}
		}
	}
	if wsURLFromConfig == "" {
		if test, _ := data["test"].(map[string]interface{}); test != nil {
			if ws, _ := test["websocket"].(map[string]interface{}); ws != nil {
				if u, _ := ws["url"].(string); u != "" {
					wsURLFromConfig = u
				}
			}
		}
	}
	if wsURLFromConfig == "" {
		return false, "未配置 WebSocket URL", 0, ""
	}
	parsed, err := url.Parse(wsURLFromConfig)
	if err != nil {
		return false, "URL 解析失败", 0, ""
	}
	scheme := "http"
	if parsed.Scheme == "wss" {
		scheme = "https"
	}
	otaHTTPURL := scheme + "://" + parsed.Host + otaHTTPPath

	t0 := time.Now()
	// Part1: POST OTA 地址，带 Device-ID、Client-ID，解析 JSON 取 websocket.url
	req, err := http.NewRequest(http.MethodPost, otaHTTPURL, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return false, "创建 OTA 请求失败", time.Since(t0).Milliseconds(), ""
	}
	req.Header.Set("Device-ID", otaTestDeviceID)
	req.Header.Set("Client-ID", otaTestClientID)
	req.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, "OTA 请求失败: " + err.Error(), time.Since(t0).Milliseconds(), ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	firstPacketMs = time.Since(t0).Milliseconds()
	otaResponseBody = string(body)
	if resp.StatusCode != http.StatusOK {
		return false, "OTA 返回 HTTP " + strconv.Itoa(resp.StatusCode), firstPacketMs, otaResponseBody
	}
	var otaResp map[string]interface{}
	if err := json.Unmarshal(body, &otaResp); err != nil {
		return false, "OTA 响应非 JSON", firstPacketMs, otaResponseBody
	}
	wsObj, _ := otaResp["websocket"].(map[string]interface{})
	if wsObj == nil {
		return false, "OTA 响应中无 websocket 字段", firstPacketMs, otaResponseBody
	}
	wsURL, _ := wsObj["url"].(string)
	if wsURL == "" {
		return false, "OTA 响应中无 websocket.url", firstPacketMs, otaResponseBody
	}

	// Part2: WebSocket 建连，带 Device-ID、Client-ID，连通即关闭（建连耗时计入首包）
	wsT0 := time.Now()
	header := http.Header{}
	header.Set("Device-ID", otaTestDeviceID)
	header.Set("Client-ID", otaTestClientID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return false, "WebSocket 连接失败: " + err.Error(), firstPacketMs + time.Since(wsT0).Milliseconds(), otaResponseBody
	}
	conn.Close()
	wsTotalMs := firstPacketMs + time.Since(wsT0).Milliseconds()
	return true, "OTA 与 WebSocket 均正常", wsTotalMs, otaResponseBody
}

// testOTAConfigWithMQTTUDP 扩展的OTA测试，支持WebSocket和MQTT UDP双测试
// 返回完整的测试结果结构
func (ac *AdminController) testOTAConfigWithMQTTUDP(cfg models.Config) OTATestResult {
	result := OTATestResult{
		WebSocket: OTATestItem{Ok: false, Message: "测试失败", FirstPacketMs: 0},
	}

	// 解析配置
	if cfg.JsonData == "" {
		result.WebSocket = OTATestItem{Ok: false, Message: "配置为空", FirstPacketMs: 0}
		return result
	}
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(cfg.JsonData), &data); err != nil {
		result.WebSocket = OTATestItem{Ok: false, Message: "配置解析失败", FirstPacketMs: 0}
		return result
	}

	// 获取WebSocket URL（优先external，为空则尝试test）
	wsURLFromConfig := ""
	if ext, _ := data["external"].(map[string]interface{}); ext != nil {
		if ws, _ := ext["websocket"].(map[string]interface{}); ws != nil {
			wsURLFromConfig, _ = ws["url"].(string)
		}
	}
	if wsURLFromConfig == "" {
		if test, _ := data["test"].(map[string]interface{}); test != nil {
			if ws, _ := test["websocket"].(map[string]interface{}); ws != nil {
				wsURLFromConfig, _ = ws["url"].(string)
			}
		}
	}
	if wsURLFromConfig == "" {
		result.WebSocket = OTATestItem{Ok: false, Message: "未配置 WebSocket URL", FirstPacketMs: 0}
		return result
	}

	// 确定使用哪个环境的配置（根据WebSocket URL来源）
	var envConfig map[string]interface{}
	if ext, _ := data["external"].(map[string]interface{}); ext != nil {
		if ws, _ := ext["websocket"].(map[string]interface{}); ws != nil {
			if url, _ := ws["url"].(string); url == wsURLFromConfig && url != "" {
				envConfig = ext
			}
		}
	}
	if envConfig == nil {
		if test, _ := data["test"].(map[string]interface{}); test != nil {
			if ws, _ := test["websocket"].(map[string]interface{}); ws != nil {
				if url, _ := ws["url"].(string); url == wsURLFromConfig {
					envConfig = test
				}
			}
		}
	}

	// 检查是否启用MQTT UDP测试
	var mqttEnabled bool
	if envConfig != nil {
		if mqtt, _ := envConfig["mqtt"].(map[string]interface{}); mqtt != nil {
			if enable, ok := mqtt["enable"].(bool); ok && enable {
				mqttEnabled = true
			}
		}
	}

	// 构建OTA HTTP URL
	parsed, err := url.Parse(wsURLFromConfig)
	if err != nil {
		result.WebSocket = OTATestItem{Ok: false, Message: "URL 解析失败", FirstPacketMs: 0}
		return result
	}
	scheme := "http"
	if parsed.Scheme == "wss" {
		scheme = "https"
	}
	otaHTTPURL := scheme + "://" + parsed.Host + otaHTTPPath

	// 第一阶段：POST OTA HTTP接口
	t0 := time.Now()
	req, err := http.NewRequest(http.MethodPost, otaHTTPURL, bytes.NewBuffer([]byte("{}")))
	if err != nil {
		result.WebSocket = OTATestItem{Ok: false, Message: "创建 OTA 请求失败", FirstPacketMs: time.Since(t0).Milliseconds()}
		return result
	}
	req.Header.Set("Device-ID", otaTestDeviceID)
	req.Header.Set("Client-ID", otaTestClientID)
	req.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		result.WebSocket = OTATestItem{Ok: false, Message: "OTA 请求失败: " + err.Error(), FirstPacketMs: time.Since(t0).Milliseconds()}
		return result
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	httpMs := time.Since(t0).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		result.WebSocket = OTATestItem{Ok: false, Message: "OTA 返回 HTTP " + strconv.Itoa(resp.StatusCode), FirstPacketMs: httpMs}
		return result
	}

	var otaResp map[string]interface{}
	if err := json.Unmarshal(body, &otaResp); err != nil {
		result.WebSocket = OTATestItem{Ok: false, Message: "OTA 响应非 JSON", FirstPacketMs: httpMs}
		return result
	}

	// 第二阶段：WebSocket测试
	wsObj, _ := otaResp["websocket"].(map[string]interface{})
	if wsObj == nil {
		result.WebSocket = OTATestItem{Ok: false, Message: "OTA 响应中无 websocket 字段", FirstPacketMs: httpMs}
		return result
	}
	wsURL, _ := wsObj["url"].(string)
	if wsURL == "" {
		result.WebSocket = OTATestItem{Ok: false, Message: "OTA 响应中无 websocket.url", FirstPacketMs: httpMs}
		return result
	}

	wsT0 := time.Now()
	header := http.Header{}
	header.Set("Device-ID", otaTestDeviceID)
	header.Set("Client-ID", otaTestClientID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		result.WebSocket = OTATestItem{Ok: false, Message: "WebSocket 连接失败: " + err.Error(), FirstPacketMs: httpMs + time.Since(wsT0).Milliseconds()}
		return result
	}
	conn.Close()
	wsTotalMs := httpMs + time.Since(wsT0).Milliseconds()
	result.WebSocket = OTATestItem{Ok: true, Message: "WebSocket 连接正常", FirstPacketMs: wsTotalMs}

	// 保存OTA响应体（用于前端显示）
	result.OTAResponse = string(body)

	// 第三阶段：MQTT UDP测试（如果启用）
	// 参考 test/mqtt_udp 逻辑：从OTA响应获取MQTT配置，发送hello，等待响应，测试UDP
	if mqttEnabled {
		// 从OTA响应中获取MQTT配置
		mqttObj, hasMQTT := otaResp["mqtt"].(map[string]interface{})
		if !hasMQTT {
			result.MQTTUDP = &OTATestItem{
				Ok:            false,
				Message:       "OTA响应未返回MQTT配置，无法测试MQTT UDP",
				FirstPacketMs: 0,
			}
			return result
		}

		// 解析MQTT配置字段
		endpoint, _ := mqttObj["endpoint"].(string)
		clientID, _ := mqttObj["client_id"].(string)
		username, _ := mqttObj["username"].(string)
		password, _ := mqttObj["password"].(string)
		publishTopic, _ := mqttObj["publish_topic"].(string)
		subscribeTopic, _ := mqttObj["subscribe_topic"].(string)

		// 验证必要字段（不需要校验 subscribe_topic）
		if endpoint == "" {
			result.MQTTUDP = &OTATestItem{Ok: false, Message: "OTA响应中MQTT endpoint为空", FirstPacketMs: 0}
			return result
		}
		if publishTopic == "" {
			result.MQTTUDP = &OTATestItem{Ok: false, Message: "OTA响应中MQTT publish_topic为空", FirstPacketMs: 0}
			return result
		}

		// 构建MQTT测试配置
		otaMqttConfig := &MQTTUDPTestConfig{
			Endpoint:       endpoint,
			ClientID:       clientID,
			Username:       username,
			Password:       password,
			PublishTopic:   publishTopic,
			SubscribeTopic: subscribeTopic, // 保留但不校验，可能用于日志
		}

		mqttOK, mqttMsg, mqttMs := testMQTTUDPConfig(*otaMqttConfig)
		result.MQTTUDP = &OTATestItem{
			Ok:            mqttOK,
			Message:       mqttMsg,
			FirstPacketMs: mqttMs,
		}
	}

	return result
}

// generateMQTTUsername 生成MQTT用户名
func generateMQTTUsername(deviceID, signatureKey string) string {
	h := hmac.New(sha256.New, []byte(signatureKey))
	h.Write([]byte(deviceID + "-username"))
	return hex.EncodeToString(h.Sum(nil))
}

// generateMQTTPassword 生成MQTT密码
func generateMQTTPassword(deviceID, signatureKey string) string {
	h := hmac.New(sha256.New, []byte(signatureKey))
	h.Write([]byte(deviceID + "-password"))
	return hex.EncodeToString(h.Sum(nil))
}

// GetConfigs 获取所有配置列表
func (ac *AdminController) GetConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取配置列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

// GetConfig 获取单个配置
func (ac *AdminController) GetConfig(c *gin.Context) {
	id := c.Param("id")
	var config models.Config
	if err := ac.DB.First(&config, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "Config not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get config"})
		}
		return
	}
	c.JSON(http.StatusOK, config)
}

func (ac *AdminController) GetConfigByID(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var config models.Config

	if err := ac.DB.First(&config, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": config})
}

func (ac *AdminController) CreateConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 检查是否已存在Memory配置
	var existingCount int64
	ac.DB.Model(&models.Config{}).Where("type = ?", "memory").Count(&existingCount)

	// 如果不存在任何Memory配置，自动设置为默认配置
	if existingCount == 0 {
		config.IsDefault = true
	}

	// 如果设置为默认配置，先取消其他同类型的默认配置
	if config.IsDefault {
		ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ?", config.Type, true).Update("is_default", false)
	}

	if err := ac.DB.Create(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建配置失败"})
		return
	}

	ac.notifySystemConfigChanged()
	c.JSON(http.StatusCreated, gin.H{"data": config})
}

func (ac *AdminController) UpdateConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var config models.Config

	if err := ac.DB.First(&config, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}

	var updateData models.Config
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 如果设置为默认配置，先取消其他同类型的默认配置
	if updateData.IsDefault {
		ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ? AND id != ?", config.Type, true, id).Update("is_default", false)
	}

	// 更新配置
	config.Name = updateData.Name
	config.Provider = updateData.Provider
	config.JsonData = updateData.JsonData
	config.Enabled = updateData.Enabled
	config.IsDefault = updateData.IsDefault

	if err := ac.DB.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败"})
		return
	}

	ac.notifySystemConfigChanged()
	c.JSON(http.StatusOK, gin.H{"data": config})
}

func (ac *AdminController) DeleteConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := ac.DB.Delete(&models.Config{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除配置失败"})
		return
	}
	ac.notifySystemConfigChanged()
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// 设置默认配置
func (ac *AdminController) SetDefaultConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var config models.Config

	if err := ac.DB.First(&config, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}

	// 先取消其他同类型的默认配置
	ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ?", config.Type, true).Update("is_default", false)

	// 设置当前配置为默认
	config.IsDefault = true
	if err := ac.DB.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设置默认配置失败"})
		return
	}

	ac.notifySystemConfigChanged()
	c.JSON(http.StatusOK, gin.H{"message": "设置默认配置成功", "data": config})
}

// 获取默认配置
func (ac *AdminController) GetDefaultConfig(c *gin.Context) {
	configType := c.Param("type")
	var config models.Config

	if err := ac.DB.Where("type = ? AND is_default = ?", configType, true).First(&config).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "默认配置不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": config})
}

// GlobalRole管理
func (ac *AdminController) GetGlobalRoles(c *gin.Context) {
	var roles []models.GlobalRole
	if err := ac.DB.Find(&roles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取全局角色失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": roles})
}

func (ac *AdminController) CreateGlobalRole(c *gin.Context) {
	var role models.GlobalRole
	if err := c.ShouldBindJSON(&role); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := ac.DB.Create(&role).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建全局角色失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": role})
}

func (ac *AdminController) UpdateGlobalRole(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var role models.GlobalRole

	if err := ac.DB.First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "全局角色不存在"})
		return
	}

	if err := c.ShouldBindJSON(&role); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := ac.DB.Save(&role).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新全局角色失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": role})
}

func (ac *AdminController) DeleteGlobalRole(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := ac.DB.Delete(&models.GlobalRole{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除全局角色失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// 用户管理
func (ac *AdminController) GetUsers(c *gin.Context) {
	var users []models.User
	if err := ac.DB.Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": users})
}

func (ac *AdminController) CreateUser(c *gin.Context) {
	// 添加明显的调试标记
	log.Println("=== [CreateUser] 方法开始执行 ===")
	log.Println("=== [CreateUser] 这是CreateUser方法的开始 ===")

	// 由于User模型的Password字段使用了json:"-"标签，需要手动解析
	var requestData struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}

	// 直接尝试绑定到map以查看原始数据
	var rawMap map[string]interface{}
	if err := c.ShouldBindJSON(&rawMap); err != nil {
		log.Printf("[CreateUser] 绑定到map失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON解析失败"})
		return
	}
	log.Printf("[CreateUser] 原始JSON数据: %+v", rawMap)

	// 手动提取字段
	username, _ := rawMap["username"].(string)
	email, _ := rawMap["email"].(string)
	password, _ := rawMap["password"].(string)
	role, _ := rawMap["role"].(string)

	// 更新requestData
	requestData.Username = username
	requestData.Email = email
	requestData.Password = password
	requestData.Role = role

	// 验证必要字段
	if requestData.Username == "" || requestData.Email == "" || requestData.Password == "" {
		log.Printf("[CreateUser] 缺少必要字段: username=%s, email=%s, password长度=%d",
			requestData.Username, requestData.Email, len(requestData.Password))
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名、邮箱和密码为必填项"})
		return
	}

	log.Printf("[CreateUser] 接收到用户创建请求 - 用户名: %s, 邮箱: %s, 角色: %s", requestData.Username, requestData.Email, requestData.Role)
	log.Printf("[CreateUser] 原始密码长度: %d", len(requestData.Password))
	log.Printf("[CreateUser] 原始密码内容: %s", requestData.Password)

	// 检查用户名是否已存在
	var existingUser models.User
	err := ac.DB.Where("username = ?", requestData.Username).First(&existingUser).Error
	if err == nil {
		// 用户名已存在
		log.Printf("[CreateUser] 用户名 %s 已存在", requestData.Username)
		c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		// 数据库查询出错
		log.Printf("[CreateUser] 数据库查询失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建用户失败"})
		return
	}

	// 用户不存在，创建新用户
	log.Printf("[CreateUser] 创建新用户: %s", requestData.Username)
	var user models.User
	user.Username = requestData.Username
	user.Email = requestData.Email
	user.Role = requestData.Role

	// 加密密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(requestData.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("[CreateUser] 密码加密失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}
	user.Password = string(hashedPassword)
	log.Printf("[CreateUser] 密码加密成功 - 哈希长度: %d, 哈希前缀: %s", len(user.Password), user.Password[:10])

	if err := ac.DB.Create(&user).Error; err != nil {
		log.Printf("[CreateUser] 数据库创建用户失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建用户失败"})
		return
	}

	log.Printf("[CreateUser] 用户创建成功 - ID: %d, 用户名: %s", user.ID, user.Username)

	// 不返回密码
	user.Password = ""
	c.JSON(http.StatusCreated, gin.H{"data": user})
}

func (ac *AdminController) UpdateUser(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var user models.User

	if err := ac.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	var updateData map[string]interface{}
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 如果更新密码，需要加密
	if password, ok := updateData["password"]; ok && password != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password.(string)), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
			return
		}
		updateData["password"] = string(hashedPassword)
	}

	if err := ac.DB.Model(&user).Updates(updateData).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新用户失败"})
		return
	}

	// 重新查询用户信息（不包含密码）
	ac.DB.First(&user, id)
	user.Password = ""
	c.JSON(http.StatusOK, gin.H{"data": user})
}

func (ac *AdminController) DeleteUser(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := ac.DB.Delete(&models.User{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除用户失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// 重置用户密码
func (ac *AdminController) ResetUserPassword(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var requestData struct {
		NewPassword string `json:"new_password" binding:"required,min=6"`
	}

	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请输入有效的新密码（至少6位）"})
		return
	}

	// 查找用户
	var user models.User
	if err := ac.DB.First(&user, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查找用户失败"})
		}
		return
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(requestData.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("[ResetUserPassword] 密码加密失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码加密失败"})
		return
	}

	// 更新用户密码
	if err := ac.DB.Model(&user).Update("password", string(hashedPassword)).Error; err != nil {
		log.Printf("[ResetUserPassword] 更新密码失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置密码失败"})
		return
	}

	log.Printf("[ResetUserPassword] 管理员重置用户密码成功 - 用户ID: %d, 用户名: %s", user.ID, user.Username)
	c.JSON(http.StatusOK, gin.H{
		"message": "密码重置成功",
		"data": gin.H{
			"user_id":  user.ID,
			"username": user.Username,
		},
	})
}

// GetUserVoiceCloneQuotas 获取用户声音复刻额度（按 tts_config_id 维度）
func (ac *AdminController) GetUserVoiceCloneQuotas(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户ID格式错误"})
		return
	}

	var user models.User
	if err = ac.DB.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户失败"})
		return
	}
	if strings.TrimSpace(user.Role) != "user" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持为普通用户分配复刻额度"})
		return
	}

	var ttsConfigs []models.Config
	if err = ac.DB.Where("type = ?", "tts").Order("enabled DESC, name ASC").Find(&ttsConfigs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询TTS配置失败"})
		return
	}

	var quotas []models.UserVoiceCloneQuota
	if err = ac.DB.Where("user_id = ?", user.ID).Find(&quotas).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户额度失败"})
		return
	}
	quotaByConfigID := make(map[string]models.UserVoiceCloneQuota, len(quotas))
	for _, quota := range quotas {
		quotaByConfigID[quota.TTSConfigID] = quota
	}

	type usageRow struct {
		TTSConfigID string `json:"tts_config_id"`
		UsedCount   int64  `json:"used_count"`
	}
	var usageRows []usageRow
	if err = ac.DB.Model(&models.VoiceClone{}).
		Select("tts_config_id, COUNT(1) AS used_count").
		Where("user_id = ? AND status != ?", user.ID, "deleted").
		Group("tts_config_id").
		Scan(&usageRows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计用户复刻次数失败"})
		return
	}
	usageByConfigID := make(map[string]int, len(usageRows))
	for _, row := range usageRows {
		usageByConfigID[row.TTSConfigID] = int(row.UsedCount)
	}

	result := make([]gin.H, 0, len(ttsConfigs))
	configIDSet := make(map[string]bool, len(ttsConfigs))
	for _, ttsConfig := range ttsConfigs {
		configIDSet[ttsConfig.ConfigID] = true
		quota, hasQuota := quotaByConfigID[ttsConfig.ConfigID]
		maxCount := -1
		usedCount := usageByConfigID[ttsConfig.ConfigID]
		if hasQuota {
			maxCount = quota.MaxCount
			if quota.UsedCount > usedCount {
				usedCount = quota.UsedCount
			}
		}
		remainingCount := -1
		if maxCount >= 0 {
			remainingCount = maxCount - usedCount
			if remainingCount < 0 {
				remainingCount = 0
			}
		}

		result = append(result, gin.H{
			"tts_config_id":   ttsConfig.ConfigID,
			"tts_config_name": ttsConfig.Name,
			"provider":        ttsConfig.Provider,
			"enabled":         ttsConfig.Enabled,
			"max_count":       maxCount,
			"used_count":      usedCount,
			"remaining_count": remainingCount,
		})
	}

	// 保留已删除的历史配置额度，避免“额度配置丢失不可见”
	for _, quota := range quotas {
		if configIDSet[quota.TTSConfigID] {
			continue
		}
		maxCount := quota.MaxCount
		usedCount := quota.UsedCount
		if usageByConfigID[quota.TTSConfigID] > usedCount {
			usedCount = usageByConfigID[quota.TTSConfigID]
		}
		remainingCount := -1
		if maxCount >= 0 {
			remainingCount = maxCount - usedCount
			if remainingCount < 0 {
				remainingCount = 0
			}
		}
		result = append(result, gin.H{
			"tts_config_id":   quota.TTSConfigID,
			"tts_config_name": "(已删除配置)",
			"provider":        "",
			"enabled":         false,
			"max_count":       maxCount,
			"used_count":      usedCount,
			"remaining_count": remainingCount,
		})
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"user_id":    user.ID,
		"username":   user.Username,
		"quotas":     result,
		"updated_at": time.Now(),
	}})
}

// UpdateUserVoiceCloneQuotas 批量更新用户声音复刻额度
func (ac *AdminController) UpdateUserVoiceCloneQuotas(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户ID格式错误"})
		return
	}

	var user models.User
	if err = ac.DB.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户失败"})
		return
	}
	if strings.TrimSpace(user.Role) != "user" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持为普通用户分配复刻额度"})
		return
	}

	var req struct {
		Items []struct {
			TTSConfigID string `json:"tts_config_id"`
			MaxCount    int    `json:"max_count"`
		} `json:"items"`
	}
	if err = c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式错误"})
		return
	}
	if len(req.Items) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "items不能为空"})
		return
	}

	itemByConfigID := make(map[string]int, len(req.Items))
	configIDs := make([]string, 0, len(req.Items))
	for _, item := range req.Items {
		configID := strings.TrimSpace(item.TTSConfigID)
		if configID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tts_config_id不能为空"})
			return
		}
		if item.MaxCount < -1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "max_count 不能小于 -1"})
			return
		}
		if _, exists := itemByConfigID[configID]; !exists {
			configIDs = append(configIDs, configID)
		}
		itemByConfigID[configID] = item.MaxCount
	}

	var ttsConfigs []models.Config
	if err = ac.DB.Where("type = ? AND config_id IN ?", "tts", configIDs).Find(&ttsConfigs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询TTS配置失败"})
		return
	}
	validConfigIDSet := make(map[string]bool, len(ttsConfigs))
	for _, cfg := range ttsConfigs {
		validConfigIDSet[cfg.ConfigID] = true
	}
	for _, configID := range configIDs {
		if validConfigIDSet[configID] {
			continue
		}
		// 历史已删除配置仅允许设置为 -1（删除额度记录）
		if itemByConfigID[configID] == -1 {
			continue
		}
		if !validConfigIDSet[configID] {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("TTS配置不存在: %s", configID)})
			return
		}
	}

	type usageRow struct {
		TTSConfigID string `json:"tts_config_id"`
		UsedCount   int64  `json:"used_count"`
	}
	var usageRows []usageRow
	if err = ac.DB.Model(&models.VoiceClone{}).
		Select("tts_config_id, COUNT(1) AS used_count").
		Where("user_id = ? AND status != ? AND tts_config_id IN ?", user.ID, "deleted", configIDs).
		Group("tts_config_id").
		Scan(&usageRows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计用户已使用次数失败"})
		return
	}
	usageByConfigID := make(map[string]int, len(usageRows))
	for _, row := range usageRows {
		usageByConfigID[row.TTSConfigID] = int(row.UsedCount)
	}

	if err = ac.DB.Transaction(func(tx *gorm.DB) error {
		for _, configID := range configIDs {
			maxCount := itemByConfigID[configID]
			if maxCount == -1 {
				if err := tx.Where("user_id = ? AND tts_config_id = ?", user.ID, configID).Delete(&models.UserVoiceCloneQuota{}).Error; err != nil {
					return err
				}
				continue
			}

			usedCount := usageByConfigID[configID]
			var quota models.UserVoiceCloneQuota
			if err := tx.Where("user_id = ? AND tts_config_id = ?", user.ID, configID).First(&quota).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					newQuota := models.UserVoiceCloneQuota{
						UserID:      user.ID,
						TTSConfigID: configID,
						MaxCount:    maxCount,
						UsedCount:   usedCount,
					}
					if err := tx.Create(&newQuota).Error; err != nil {
						return err
					}
					continue
				}
				return err
			}

			nextUsedCount := quota.UsedCount
			if usedCount > nextUsedCount {
				nextUsedCount = usedCount
			}
			if err := tx.Model(&models.UserVoiceCloneQuota{}).Where("id = ?", quota.ID).Updates(map[string]any{
				"max_count":  maxCount,
				"used_count": nextUsedCount,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新用户复刻额度失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "额度更新成功"})
}

// 设备管理
func (ac *AdminController) GetDevices(c *gin.Context) {
	var devices []models.Device
	if err := ac.DB.Find(&devices).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取设备列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": devices})
}

// 验证设备代码是否存在
func (ac *AdminController) ValidateDeviceCode(c *gin.Context) {
	deviceCode := c.Query("code")
	if deviceCode == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "激活码不能为空"})
		return
	}

	var device models.Device
	err := ac.DB.Where("device_code = ?", deviceCode).First(&device).Error

	if err == gorm.ErrRecordNotFound {
		c.JSON(http.StatusOK, gin.H{"exists": false})
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询设备失败"})
	} else {
		c.JSON(http.StatusOK, gin.H{"exists": true, "device": device})
	}
}

func (ac *AdminController) CreateDevice(c *gin.Context) {
	var req struct {
		UserID     uint   `json:"user_id" binding:"required"`
		DeviceCode string `json:"device_code"`
		DeviceName string `json:"device_name"`
		AgentID    uint   `json:"agent_id"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	// 验证激活码和设备名称至少填一个
	if req.DeviceCode == "" && req.DeviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "激活码和设备名称至少填写一个"})
		return
	}

	// 检查用户是否存在
	var user models.User
	if err := ac.DB.First(&user, req.UserID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "指定的用户不存在"})
		return
	}

	// 如果提供了激活码，先查找现有设备
	if req.DeviceCode != "" {
		var existingDevice models.Device
		if err := ac.DB.Where("device_code = ?", req.DeviceCode).First(&existingDevice).Error; err == nil {
			// 设备代码已存在，更新设备信息
			existingDevice.UserID = req.UserID
			if req.DeviceName != "" {
				existingDevice.DeviceName = req.DeviceName
			}
			existingDevice.AgentID = req.AgentID // 更新智能体ID
			existingDevice.Activated = true      // 激活设备

			if err := ac.DB.Save(&existingDevice).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "更新设备失败"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"message": "设备激活成功",
				"data":    existingDevice,
			})
			return
		} else if err != gorm.ErrRecordNotFound {
			// 数据库查询出错
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询设备失败"})
			return
		}
		// 如果激活码不存在，继续创建新设备
	}

	// 创建设备
	device := models.Device{
		UserID:     req.UserID,
		DeviceCode: req.DeviceCode,
		DeviceName: req.DeviceName,
		AgentID:    req.AgentID, // 使用请求中的智能体ID
		Activated:  true,        // 管理员创建的设备默认已激活
	}

	if err := ac.DB.Create(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建设备失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "设备创建成功",
		"data":    device,
	})
}

func (ac *AdminController) UpdateDevice(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var device models.Device

	if err := ac.DB.First(&device, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在"})
		return
	}

	var updateData struct {
		UserID     uint   `json:"user_id"`
		DeviceCode string `json:"device_code"`
		DeviceName string `json:"device_name"`
		Activated  bool   `json:"activated"`
		AgentID    uint   `json:"agent_id"`
	}

	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 更新设备信息
	device.UserID = updateData.UserID
	device.DeviceCode = updateData.DeviceCode
	device.DeviceName = updateData.DeviceName
	device.Activated = updateData.Activated
	device.AgentID = updateData.AgentID

	if err := ac.DB.Save(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新设备失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": device})
}

func (ac *AdminController) DeleteDevice(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := ac.DB.Delete(&models.Device{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除设备失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// 智能体管理
func (ac *AdminController) GetAgents(c *gin.Context) {
	var agents []models.Agent
	if err := ac.DB.Find(&agents).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取智能体列表失败"})
		return
	}

	// 手动加载关联的配置信息
	type AgentWithConfigs struct {
		models.Agent
		LLMConfig *models.Config `json:"llm_config,omitempty"`
		TTSConfig *models.Config `json:"tts_config,omitempty"`
	}

	var result []AgentWithConfigs
	for _, agent := range agents {
		agentWithConfig := AgentWithConfigs{Agent: agent}

		// 加载LLM配置
		if agent.LLMConfigID != nil && *agent.LLMConfigID != "" {
			var llmConfig models.Config
			if err := ac.DB.Where("config_id = ? AND type = ?", *agent.LLMConfigID, "llm").First(&llmConfig).Error; err == nil {
				agentWithConfig.LLMConfig = &llmConfig
			}
		}

		// 加载TTS配置
		if agent.TTSConfigID != nil && *agent.TTSConfigID != "" {
			var ttsConfig models.Config
			if err := ac.DB.Where("config_id = ? AND type = ?", *agent.TTSConfigID, "tts").First(&ttsConfig).Error; err == nil {
				agentWithConfig.TTSConfig = &ttsConfig
			}
		}

		result = append(result, agentWithConfig)
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// GetDeviceMcpTools 获取设备维度MCP工具列表（管理员版本）
func (ac *AdminController) GetDeviceMcpTools(c *gin.Context) {
	deviceID := c.Param("id")
	if deviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device_id parameter is required"})
		return
	}

	var device models.Device
	if err := ac.DB.Where("id = ?", deviceID).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在"})
		return
	}

	tools, err := ac.WebSocketController.RequestDeviceMcpToolDetailsFromClient(context.Background(), device.DeviceName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"tools": []interface{}{}}})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": gin.H{"tools": tools}})
}

// CallAgentMcpTool 调用智能体维度MCP工具（管理员版本）
func (ac *AdminController) CallAgentMcpTool(c *gin.Context) {
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
	if err := ac.DB.Where("id = ?", agentID).First(&agent).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在"})
		return
	}

	body := map[string]interface{}{
		"agent_id":  agentID,
		"tool_name": req.ToolName,
		"arguments": req.Arguments,
	}
	result, err := ac.WebSocketController.CallMcpToolFromClient(context.Background(), body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "调用MCP工具失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// CallDeviceMcpTool 调用设备维度MCP工具（管理员版本）
func (ac *AdminController) CallDeviceMcpTool(c *gin.Context) {
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
	if err := ac.DB.Where("id = ?", deviceID).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在"})
		return
	}

	body := map[string]interface{}{
		"device_id": device.DeviceName,
		"tool_name": req.ToolName,
		"arguments": req.Arguments,
	}
	result, err := ac.WebSocketController.CallMcpToolFromClient(context.Background(), body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "调用MCP工具失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// GetAgentMCPEndpoint 获取智能体的MCP接入点URL
func (ac *AdminController) GetAgentMCPEndpoint(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "agent_id parameter is required"})
		return
	}

	// 从JWT中间件获取当前用户ID
	userIDInterface, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户未认证"})
		return
	}
	userID, ok := userIDInterface.(uint)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "用户ID类型错误"})
		return
	}

	// 使用公共函数生成MCP接入点
	endpoint, err := GenerateAgentMCPEndpoint(ac.DB, agentID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 返回单个endpoint字符串
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"endpoint": endpoint}})
}

// GetAgentMcpTools 获取智能体的MCP工具列表
func (ac *AdminController) GetAgentMcpTools(c *gin.Context) {
	agentID := c.Param("id")

	// 管理员验证函数：验证智能体是否存在（管理员可以查看任意用户的智能体）
	adminAgentValidator := func(agentID string) error {
		var agent models.Agent
		if err := ac.DB.Where("id = ?", agentID).First(&agent).Error; err != nil {
			return fmt.Errorf("智能体不存在")
		}
		return nil
	}

	// 使用公共函数
	GetAgentMcpToolsCommon(c, agentID, ac.WebSocketController, adminAgentValidator)
}

func (ac *AdminController) CreateAgent(c *gin.Context) {
	var agent models.Agent
	if err := c.ShouldBindJSON(&agent); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	agent.MemoryMode = normalizeAgentMemoryMode(agent.MemoryMode)
	normalizedMCPServiceNames, err := ac.normalizeAndValidateAgentMCPServices(agent.MCPServiceNames)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	agent.MCPServiceNames = normalizedMCPServiceNames

	if err := ac.DB.Create(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建智能体失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": agent})
}

func (ac *AdminController) UpdateAgent(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var agent models.Agent

	if err := ac.DB.First(&agent, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "智能体不存在"})
		return
	}

	if err := c.ShouldBindJSON(&agent); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	agent.MemoryMode = normalizeAgentMemoryMode(agent.MemoryMode)
	normalizedMCPServiceNames, err := ac.normalizeAndValidateAgentMCPServices(agent.MCPServiceNames)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	agent.MCPServiceNames = normalizedMCPServiceNames

	if err := ac.DB.Save(&agent).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新智能体失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": agent})
}

func (ac *AdminController) DeleteAgent(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := ac.DB.Delete(&models.Agent{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除智能体失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// VAD配置管理（兼容前端）
func (ac *AdminController) GetVADConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "vad").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get VAD configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateVADConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "vad"
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateVADConfig(c *gin.Context) {
	ac.updateConfigWithType(c, "vad")
}

func (ac *AdminController) DeleteVADConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "vad")
}

// ASR配置管理（兼容前端）
func (ac *AdminController) GetASRConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "asr").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get ASR configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateASRConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "asr"
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateASRConfig(c *gin.Context) {
	ac.updateConfigWithType(c, "asr")
}

func (ac *AdminController) DeleteASRConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "asr")
}

// LLM配置管理（兼容前端）
func (ac *AdminController) GetLLMConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "llm").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get LLM configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateLLMConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "llm"
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateLLMConfig(c *gin.Context) {
	ac.updateConfigWithType(c, "llm")
}

func (ac *AdminController) DeleteLLMConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "llm")
}

// TTS配置管理（兼容前端）
func (ac *AdminController) GetTTSConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "tts").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get TTS configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateTTSConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "tts"
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateTTSConfig(c *gin.Context) {
	ac.updateConfigWithType(c, "tts")
}

func (ac *AdminController) DeleteTTSConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "tts")
}

// Speaker配置管理（兼容前端）
func (ac *AdminController) GetSpeakerConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "voice_identify").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get Speaker configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateSpeakerConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "voice_identify"
	// 声纹配置只有一个，自动设置为默认配置
	config.IsDefault = true
	// 如果已存在配置，先删除旧的
	ac.DB.Where("type = ?", "voice_identify").Delete(&models.Config{})
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateSpeakerConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var config models.Config

	if err := ac.DB.Where("id = ? AND type = ?", id, "voice_identify").First(&config).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}

	var updateData models.Config
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 声纹配置只有一个，始终设置为默认配置
	updateData.IsDefault = true

	// 更新配置
	config.Name = updateData.Name
	config.Provider = updateData.Provider
	config.JsonData = updateData.JsonData
	config.Enabled = updateData.Enabled
	config.IsDefault = updateData.IsDefault

	// 如果提供了新的config_id，则更新它
	if updateData.ConfigID != "" {
		config.ConfigID = updateData.ConfigID
	}

	if err := ac.DB.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": config})
}

func (ac *AdminController) DeleteSpeakerConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "voice_identify")
}

// Vision配置管理（兼容前端）
func (ac *AdminController) GetVisionConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ? AND config_id != ?", "vision", "vision_base").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get Vision configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

// GetVisionBaseConfig 获取Vision基础配置
func (ac *AdminController) GetVisionBaseConfig(c *gin.Context) {
	var config models.Config
	if err := ac.DB.Where("type = ? AND config_id = ?", "vision", "vision_base").First(&config).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 如果没有找到基础配置，返回默认值
			c.JSON(http.StatusOK, gin.H{"data": map[string]interface{}{
				"enable_auth": false,
				"vision_url":  "",
			}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get Vision base config"})
		return
	}

	var configData map[string]interface{}
	if err := json.Unmarshal([]byte(config.JsonData), &configData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse Vision base config"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": configData})
}

// UpdateVisionBaseConfig 更新Vision基础配置
func (ac *AdminController) UpdateVisionBaseConfig(c *gin.Context) {
	var requestData map[string]interface{}
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	jsonData, err := json.Marshal(requestData)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal config data"})
		return
	}

	var config models.Config
	if err := ac.DB.Where("type = ? AND config_id = ?", "vision", "vision_base").First(&config).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// 创建新的基础配置
			config = models.Config{
				Type:      "vision",
				Name:      "vision_base",
				ConfigID:  "vision_base",
				Provider:  "vision_base",
				JsonData:  string(jsonData),
				Enabled:   true,
				IsDefault: false,
			}
			if err := ac.DB.Create(&config).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create Vision base config"})
				return
			}
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query Vision base config"})
			return
		}
	} else {
		// 更新现有配置
		config.JsonData = string(jsonData)
		if err := ac.DB.Save(&config).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Vision base config"})
			return
		}
	}

	ac.notifySystemConfigChanged()
	c.JSON(http.StatusOK, gin.H{"message": "Vision base config updated successfully"})
}

// GetChatSettings 获取聊天设置（auth.enable + chat.*）
func (ac *AdminController) GetChatSettings(c *gin.Context) {
	response := gin.H{
		"auth": gin.H{
			"enable": false,
		},
		"chat": gin.H{
			"max_idle_duration":         30000,
			"chat_max_silence_duration": 400,
			"realtime_mode":             4,
		},
	}

	var authConfig models.Config
	if err := ac.DB.Where("type = ?", "auth").Order("is_default DESC, id ASC").First(&authConfig).Error; err == nil {
		var authData map[string]interface{}
		if authConfig.JsonData != "" && json.Unmarshal([]byte(authConfig.JsonData), &authData) == nil {
			if enable, ok := authData["enable"].(bool); ok {
				response["auth"].(gin.H)["enable"] = enable
			}
		}
	}

	var chatConfig models.Config
	if err := ac.DB.Where("type = ?", "chat").Order("is_default DESC, id ASC").First(&chatConfig).Error; err == nil {
		var chatData map[string]interface{}
		if chatConfig.JsonData != "" && json.Unmarshal([]byte(chatConfig.JsonData), &chatData) == nil {
			if maxIdle, ok := chatData["max_idle_duration"].(float64); ok && int64(maxIdle) >= 0 {
				response["chat"].(gin.H)["max_idle_duration"] = int64(maxIdle)
			}
			if maxSilence, ok := chatData["chat_max_silence_duration"].(float64); ok && int64(maxSilence) >= 0 {
				response["chat"].(gin.H)["chat_max_silence_duration"] = int64(maxSilence)
			}
			if realtimeMode, ok := chatData["realtime_mode"].(float64); ok && int(realtimeMode) >= 1 && int(realtimeMode) <= 4 {
				response["chat"].(gin.H)["realtime_mode"] = int(realtimeMode)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": response})
}

// UpdateChatSettings 更新聊天设置（auth.enable + chat.*）
func (ac *AdminController) UpdateChatSettings(c *gin.Context) {
	var req struct {
		Auth struct {
			Enable bool `json:"enable"`
		} `json:"auth"`
		Chat struct {
			MaxIdleDuration        int64 `json:"max_idle_duration"`
			ChatMaxSilenceDuration int64 `json:"chat_max_silence_duration"`
			RealtimeMode           int   `json:"realtime_mode"`
		} `json:"chat"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Chat.MaxIdleDuration < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat.max_idle_duration 不能小于 0，0 表示不限制"})
		return
	}
	if req.Chat.ChatMaxSilenceDuration < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat.chat_max_silence_duration 不能小于 0"})
		return
	}
	if req.Chat.RealtimeMode < 1 || req.Chat.RealtimeMode > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat.realtime_mode 必须在 1-4 之间"})
		return
	}

	authJSON, err := json.Marshal(map[string]interface{}{
		"enable": req.Auth.Enable,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "auth 配置序列化失败"})
		return
	}
	chatJSON, err := json.Marshal(map[string]interface{}{
		"max_idle_duration":         req.Chat.MaxIdleDuration,
		"chat_max_silence_duration": req.Chat.ChatMaxSilenceDuration,
		"realtime_mode":             req.Chat.RealtimeMode,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "chat 配置序列化失败"})
		return
	}

	tx := ac.DB.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "启动事务失败"})
		return
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	upsertConfig := func(configType, configID, name string, jsonData []byte) error {
		var cfg models.Config
		err := tx.Where("type = ? AND config_id = ?", configType, configID).First(&cfg).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Model(&models.Config{}).Where("type = ?", configType).Update("is_default", false).Error; err != nil {
				return err
			}
			cfg = models.Config{
				Type:      configType,
				Name:      name,
				ConfigID:  configID,
				Provider:  "",
				JsonData:  string(jsonData),
				Enabled:   true,
				IsDefault: true,
			}
			return tx.Create(&cfg).Error
		}
		if err != nil {
			return err
		}

		if err := tx.Model(&models.Config{}).Where("type = ? AND id != ?", configType, cfg.ID).Update("is_default", false).Error; err != nil {
			return err
		}

		cfg.Name = name
		cfg.Provider = ""
		cfg.JsonData = string(jsonData)
		cfg.Enabled = true
		cfg.IsDefault = true
		return tx.Save(&cfg).Error
	}

	if err := upsertConfig("auth", "auth", "auth", authJSON); err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存 auth 设置失败: " + err.Error()})
		return
	}
	if err := upsertConfig("chat", "chat", "chat", chatJSON); err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存 chat 设置失败: " + err.Error()})
		return
	}

	if err := tx.Commit().Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交事务失败"})
		return
	}

	ac.notifySystemConfigChanged()
	c.JSON(http.StatusOK, gin.H{
		"message": "聊天设置更新成功",
		"data": gin.H{
			"auth": gin.H{"enable": req.Auth.Enable},
			"chat": gin.H{
				"max_idle_duration":         req.Chat.MaxIdleDuration,
				"chat_max_silence_duration": req.Chat.ChatMaxSilenceDuration,
				"realtime_mode":             req.Chat.RealtimeMode,
			},
		},
	})
}

func (ac *AdminController) CreateVisionConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "vision"
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateVisionConfig(c *gin.Context) {
	ac.updateConfigWithType(c, "vision")
}

func (ac *AdminController) DeleteVisionConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "vision")
}

// OTA配置管理（兼容前端）
func (ac *AdminController) GetOTAConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "ota").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get OTA configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateOTAConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "ota"
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateOTAConfig(c *gin.Context) {
	ac.updateConfigWithType(c, "ota")
}

func (ac *AdminController) DeleteOTAConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "ota")
}

// MQTT配置管理（兼容前端）
func (ac *AdminController) GetMQTTConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "mqtt").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get MQTT configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateMQTTConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "mqtt"
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateMQTTConfig(c *gin.Context) {
	ac.updateConfigWithType(c, "mqtt")
}

func (ac *AdminController) DeleteMQTTConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "mqtt")
}

// MQTT Server配置管理（兼容前端）
func (ac *AdminController) GetMQTTServerConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "mqtt_server").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get MQTT Server configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateMQTTServerConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "mqtt_server"
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateMQTTServerConfig(c *gin.Context) {
	ac.updateConfigWithType(c, "mqtt_server")
}

func (ac *AdminController) DeleteMQTTServerConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "mqtt_server")
}

// UDP配置管理（兼容前端）
func (ac *AdminController) GetUDPConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "udp").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get UDP configs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateUDPConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	config.Type = "udp"
	ac.createConfigWithType(c, &config)
}

func (ac *AdminController) UpdateUDPConfig(c *gin.Context) {
	ac.updateConfigWithType(c, "udp")
}

func (ac *AdminController) DeleteUDPConfig(c *gin.Context) {
	ac.deleteConfigWithType(c, "udp")
}

// ToggleConfigEnable 切换配置的启用状态
func (ac *AdminController) ToggleConfigEnable(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid config ID"})
		return
	}

	var config models.Config
	if err := ac.DB.First(&config, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询配置失败"})
		}
		return
	}

	// 切换启用状态
	config.Enabled = !config.Enabled
	if err := ac.DB.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置状态失败"})
		return
	}

	ac.notifySystemConfigChanged()
	status := "禁用"
	if config.Enabled {
		status = "启用"
	}

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("配置已%s", status),
		"data":    config,
	})
}

// 辅助方法
func (ac *AdminController) createConfigWithType(c *gin.Context, config *models.Config) {
	// 如果没有提供config_id，自动生成一个
	if config.ConfigID == "" {
		// 使用类型_名称_时间戳的格式生成唯一ID
		timestamp := time.Now().Unix()
		safeName := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(config.Name, " ", "_"), "-", "_"))
		config.ConfigID = fmt.Sprintf("%s_%s_%d", config.Type, safeName, timestamp)
	}

	// 如果设置为默认配置，先取消其他同类型的默认配置
	if config.IsDefault {
		ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ?", config.Type, true).Update("is_default", false)
	}

	if err := ac.DB.Create(config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建配置失败"})
		return
	}

	ac.notifySystemConfigChanged()
	c.JSON(http.StatusCreated, gin.H{"data": *config})
}

// configUpdateBody 用于 updateConfigWithType，json_data 兼容前端传 string 或 object
type configUpdateBody struct {
	Name      string      `json:"name"`
	ConfigID  string      `json:"config_id"`
	Provider  string      `json:"provider"`
	JsonData  interface{} `json:"json_data"`
	Enabled   bool        `json:"enabled"`
	IsDefault bool        `json:"is_default"`
}

func (ac *AdminController) updateConfigWithType(c *gin.Context, configType string) {
	id, _ := strconv.Atoi(c.Param("id"))
	var config models.Config

	if err := ac.DB.Where("id = ? AND type = ?", id, configType).First(&config).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}

	var updateData configUpdateBody
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 如果设置为默认配置，先取消其他同类型的默认配置
	if updateData.IsDefault {
		ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ? AND id != ?", configType, true, id).Update("is_default", false)
	}

	// 更新配置
	config.Name = updateData.Name
	config.Provider = updateData.Provider
	config.Enabled = updateData.Enabled
	config.IsDefault = updateData.IsDefault

	// json_data：兼容 string 或 object，避免前端传对象时绑定失败
	switch v := updateData.JsonData.(type) {
	case string:
		config.JsonData = v
	case nil:
		// 未传则保持原值
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "json_data 格式无效"})
			return
		}
		config.JsonData = string(bytes)
	}

	// 如果提供了新的config_id，则更新它
	if updateData.ConfigID != "" {
		config.ConfigID = updateData.ConfigID
	}

	if err := ac.DB.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败: " + err.Error()})
		return
	}

	ac.notifySystemConfigChanged()
	c.JSON(http.StatusOK, gin.H{"data": config})
}

func (ac *AdminController) deleteConfigWithType(c *gin.Context, configType string) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := ac.DB.Where("id = ? AND type = ?", id, configType).Delete(&models.Config{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除配置失败"})
		return
	}
	ac.notifySystemConfigChanged()
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// 导入导出配置相关方法
// ExportConfigs 导出所有配置为YAML格式
func (ac *AdminController) ExportConfigs(c *gin.Context) {
	// 构建导出配置结构 - 只包含实际存在的模块
	type ExportConfig struct {
		VAD           map[string]interface{} `yaml:"vad,omitempty"`
		ASR           map[string]interface{} `yaml:"asr,omitempty"`
		LLM           map[string]interface{} `yaml:"llm,omitempty"`
		TTS           map[string]interface{} `yaml:"tts,omitempty"`
		Vision        map[string]interface{} `yaml:"vision,omitempty"`
		Memory        map[string]interface{} `yaml:"memory,omitempty"`
		VoiceIdentify map[string]interface{} `yaml:"voice_identify,omitempty"`
		Auth          map[string]interface{} `yaml:"auth,omitempty"`
		Chat          map[string]interface{} `yaml:"chat,omitempty"`
		MQTT          map[string]interface{} `yaml:"mqtt,omitempty"`
		MQTTServer    map[string]interface{} `yaml:"mqtt_server,omitempty"`
		UDP           map[string]interface{} `yaml:"udp,omitempty"`
		OTA           map[string]interface{} `yaml:"ota,omitempty"`
		MCP           map[string]interface{} `yaml:"mcp,omitempty"`
		LocalMCP      map[string]interface{} `yaml:"local_mcp,omitempty"`
	}

	exportConfig := ExportConfig{
		VAD:           make(map[string]interface{}),
		ASR:           make(map[string]interface{}),
		LLM:           make(map[string]interface{}),
		TTS:           make(map[string]interface{}),
		Vision:        make(map[string]interface{}),
		Memory:        make(map[string]interface{}),
		VoiceIdentify: make(map[string]interface{}),
		Auth:          make(map[string]interface{}),
		Chat:          make(map[string]interface{}),
		MQTT:          make(map[string]interface{}),
		MQTTServer:    make(map[string]interface{}),
		UDP:           make(map[string]interface{}),
		OTA:           make(map[string]interface{}),
		MCP:           make(map[string]interface{}),
		LocalMCP:      make(map[string]interface{}),
	}

	// 获取所有配置
	var configs []models.Config
	if err := ac.DB.Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get configs"})
		return
	}

	// 获取全局角色
	var globalRoles []models.GlobalRole
	if err := ac.DB.Find(&globalRoles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get global roles"})
		return
	}

	// 处理配置数据 - provider字段与is_default对应，key与ConfigID对应
	for _, config := range configs {
		var jsonData map[string]interface{}
		if err := json.Unmarshal([]byte(config.JsonData), &jsonData); err != nil {
			log.Printf("Failed to unmarshal config %s: %v", config.ConfigID, err)
			continue
		}

		// 根据配置类型组织数据
		switch config.Type {
		case "vad":
			// 兼容旧格式：如果只有一个key，说明是旧格式（带key），提取出内部配置
			var actualConfigData map[string]interface{}
			if len(jsonData) == 1 {
				// 旧格式：只有一个key，提取其值
				for _, value := range jsonData {
					if innerConfig, ok := value.(map[string]interface{}); ok {
						actualConfigData = innerConfig
					} else {
						// 如果不是map类型，直接使用原数据
						actualConfigData = jsonData
					}
					break
				}
			} else {
				// 新格式：不带key，直接使用jsonData
				actualConfigData = jsonData
			}
			// 如果是默认配置，设置provider字段
			if config.IsDefault {
				exportConfig.VAD["provider"] = config.ConfigID
			}
			// 使用ConfigID作为key
			exportConfig.VAD[config.ConfigID] = actualConfigData
		case "asr":
			if config.IsDefault {
				exportConfig.ASR["provider"] = config.ConfigID
			}
			exportConfig.ASR[config.ConfigID] = jsonData
		case "llm":
			if config.IsDefault {
				exportConfig.LLM["provider"] = config.ConfigID
			}
			exportConfig.LLM[config.ConfigID] = jsonData
		case "tts":
			if config.IsDefault {
				exportConfig.TTS["provider"] = config.ConfigID
			}
			exportConfig.TTS[config.ConfigID] = jsonData
		case "vision":
			// 特殊处理vision配置
			if config.ConfigID == "vision_base" {
				// 处理基础配置（enable_auth, vision_url等）
				for key, value := range jsonData {
					exportConfig.Vision[key] = value
				}
			} else {
				// 处理vllm配置
				if exportConfig.Vision["vllm"] == nil {
					exportConfig.Vision["vllm"] = make(map[string]interface{})
				}
				if vllmConfig, ok := exportConfig.Vision["vllm"].(map[string]interface{}); ok {
					if config.IsDefault {
						vllmConfig["provider"] = config.ConfigID
					}
					vllmConfig[config.ConfigID] = jsonData
				}
			}
		case "ota":
			// ota、mqtt、mqtt_server、udp不需要provider字段，直接合并配置
			for key, value := range jsonData {
				exportConfig.OTA[key] = value
			}
		case "mqtt":
			// ota、mqtt、mqtt_server、udp不需要provider字段，直接合并配置
			for key, value := range jsonData {
				exportConfig.MQTT[key] = value
			}
		case "mqtt_server":
			// ota、mqtt、mqtt_server、udp不需要provider字段，直接合并配置
			for key, value := range jsonData {
				exportConfig.MQTTServer[key] = value
			}
		case "udp":
			// ota、mqtt、mqtt_server、udp不需要provider字段，直接合并配置
			for key, value := range jsonData {
				exportConfig.UDP[key] = value
			}
		case "memory":
			if config.IsDefault {
				exportConfig.Memory["provider"] = config.ConfigID
			}
			exportConfig.Memory[config.ConfigID] = jsonData
		case "voice_identify":
			if config.IsDefault {
				exportConfig.VoiceIdentify["provider"] = config.ConfigID
			}
			exportConfig.VoiceIdentify[config.ConfigID] = jsonData
		case "auth":
			for key, value := range jsonData {
				exportConfig.Auth[key] = value
			}
		case "chat":
			for key, value := range jsonData {
				exportConfig.Chat[key] = value
			}
		case "mcp":
			// 处理MCP配置，将mcp和local_mcp分开
			if mcpData, exists := jsonData["mcp"]; exists {
				if mcpMap, ok := mcpData.(map[string]interface{}); ok {
					for key, value := range mcpMap {
						exportConfig.MCP[key] = value
					}
				}
			}
			// 兼容旧格式：如果直接有global字段
			if globalData, exists := jsonData["global"]; exists {
				exportConfig.MCP["global"] = globalData
			}
		case "local_mcp":
			// 处理local_mcp配置
			for key, value := range jsonData {
				exportConfig.LocalMCP[key] = value
			}
		}
	}

	// 只处理数据库中的实际配置，不设置默认值

	// 转换为YAML
	yamlData, err := yaml.Marshal(exportConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal YAML"})
		return
	}

	// 设置响应头
	c.Header("Content-Type", "application/x-yaml")
	c.Header("Content-Disposition", "attachment; filename=config.yaml")
	c.Data(http.StatusOK, "application/x-yaml", yamlData)
}

// ImportConfigs 从YAML文件导入配置
func (ac *AdminController) ImportConfigs(c *gin.Context) {
	log.Printf("开始导入配置")

	file, err := c.FormFile("file")
	if err != nil {
		log.Printf("获取上传文件失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	log.Printf("文件信息: filename=%s, size=%d", file.Filename, file.Size)

	if file.Size == 0 {
		log.Printf("文件为空")
		c.JSON(http.StatusBadRequest, gin.H{"error": "File is empty"})
		return
	}

	// 读取文件内容
	src, err := file.Open()
	if err != nil {
		log.Printf("打开文件失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to open file"})
		return
	}
	defer src.Close()

	content, err := io.ReadAll(src)
	if err != nil {
		log.Printf("读取文件内容失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read file"})
		return
	}

	log.Printf("文件内容长度: %d", len(content))

	// 解析YAML
	var importConfig map[string]interface{}
	if err := yaml.Unmarshal(content, &importConfig); err != nil {
		log.Printf("解析YAML失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid YAML format"})
		return
	}

	log.Printf("YAML解析成功，配置键: %v", getMapKeys(importConfig))

	// 开始事务
	log.Printf("开始数据库事务")
	tx := ac.DB.Begin()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("发生panic，回滚事务: %v", r)
			tx.Rollback()
		}
	}()

	// 清空现有配置
	log.Printf("清空现有配置")
	result := tx.Exec("DELETE FROM configs")
	if result.Error != nil {
		log.Printf("清空配置失败: %v", result.Error)
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear existing configs"})
		return
	}
	log.Printf("配置清空成功，删除了 %d 条记录", result.RowsAffected)

	// 清空全局角色
	log.Printf("清空全局角色")
	result2 := tx.Exec("DELETE FROM global_roles")
	if result2.Error != nil {
		log.Printf("清空全局角色失败: %v", result2.Error)
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear existing global roles"})
		return
	}
	log.Printf("全局角色清空成功，删除了 %d 条记录", result2.RowsAffected)

	// 导入配置 - 只处理实际存在的模块
	configTypes := []string{"vad", "asr", "llm", "tts", "memory", "auth", "chat", "ota", "mqtt", "mqtt_server", "udp", "mcp", "local_mcp"}
	log.Printf("开始导入配置，配置类型: %v", configTypes)

	// 处理 voice_identify 配置（映射到 speaker 类型）
	if voiceIdentifyData, exists := importConfig["voice_identify"]; exists {
		log.Printf("找到 voice_identify 配置数据")
		if voiceIdentifyMap, ok := voiceIdentifyData.(map[string]interface{}); ok {
			log.Printf("voice_identify 配置 map keys: %v", getMapKeys(voiceIdentifyMap))

			// 获取provider字段
			var defaultProvider string
			if provider, exists := voiceIdentifyMap["provider"]; exists {
				if providerStr, ok := provider.(string); ok {
					defaultProvider = providerStr
					log.Printf("voice_identify 默认provider: %s", defaultProvider)
				}
			}

			log.Printf("voice_identify 配置项keys: %v", getMapKeys(voiceIdentifyMap))
			// 声纹配置只有一个，优先使用provider指定的配置，否则使用第一个配置项
			var targetConfigID string
			if defaultProvider != "" {
				targetConfigID = defaultProvider
			} else {
				// 如果没有provider，使用第一个非provider的配置项
				for key := range voiceIdentifyMap {
					if key != "provider" {
						targetConfigID = key
						break
					}
				}
			}

			if targetConfigID == "" {
				log.Printf("voice_identify 配置中没有找到有效配置项")
			} else {
				// 只处理目标配置项
				if configValue, exists := voiceIdentifyMap[targetConfigID]; exists {
					if configMap, ok := configValue.(map[string]interface{}); ok {
						log.Printf("处理voice_identify配置项: %s", targetConfigID)
						jsonData, err := json.Marshal(configMap)
						if err != nil {
							log.Printf("序列化voice_identify配置数据失败: %v", err)
							tx.Rollback()
							c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal voice_identify config data"})
							return
						}

						// 声纹配置只有一个，始终设为默认
						config := models.Config{
							Type:      "voice_identify",
							Name:      "声纹识别配置",
							ConfigID:  "asr_server",
							Provider:  "asr_server",
							JsonData:  string(jsonData),
							Enabled:   true,
							IsDefault: true,
						}

						log.Printf("准备保存voice_identify配置: Type=%s, Name=%s, ConfigID=%s", config.Type, config.Name, config.ConfigID)

						// 声纹配置只有一个，先删除所有旧的配置
						tx.Where("type = ?", "voice_identify").Delete(&models.Config{})

						// 创建新配置
						if err := tx.Create(&config).Error; err != nil {
							log.Printf("创建voice_identify配置失败: %v", err)
							tx.Rollback()
							c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create voice_identify config"})
							return
						}
						log.Printf("voice_identify配置创建成功: %s", targetConfigID)
					}
				}
			}
		}
	}

	for _, configType := range configTypes {
		log.Printf("处理配置类型: %s", configType)
		if configData, exists := importConfig[configType]; exists {
			log.Printf("找到配置类型 %s 的数据", configType)
			if configMap, ok := configData.(map[string]interface{}); ok {
				// 对于需要provider的模块（vad, asr, llm, tts, memory），处理provider字段
				if configType == "vad" || configType == "asr" || configType == "llm" || configType == "tts" || configType == "memory" || configType == "voice_identify" {
					log.Printf("处理需要provider的配置类型: %s", configType)
					// 获取provider字段
					var defaultProvider string
					if provider, exists := configMap["provider"]; exists {
						if providerStr, ok := provider.(string); ok {
							defaultProvider = providerStr
							log.Printf("默认provider: %s", defaultProvider)
						}
					}

					log.Printf("配置项keys: %v", getMapKeys(configMap))
					// 遍历所有配置项
					for configID, configValue := range configMap {
						// 跳过provider字段
						if configID == "provider" {
							log.Printf("跳过provider字段")
							continue
						}

						if configMap, ok := configValue.(map[string]interface{}); ok {
							log.Printf("处理配置项: %s", configID)
							jsonData, err := json.Marshal(configMap)
							if err != nil {
								log.Printf("序列化配置数据失败: %v", err)
								tx.Rollback()
								c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal config data"})
								return
							}

							// 判断是否为默认配置
							isDefault := (configID == defaultProvider)
							log.Printf("配置项 %s, 是否默认: %v", configID, isDefault)

							config := models.Config{
								Type:      configType,
								Name:      configID,
								ConfigID:  configID,
								Provider:  configID,
								JsonData:  string(jsonData),
								Enabled:   true,
								IsDefault: isDefault,
							}

							log.Printf("准备保存配置: Type=%s, Name=%s, ConfigID=%s", config.Type, config.Name, config.ConfigID)

							// 先检查是否已存在相同配置
							var existingConfig models.Config
							if err := tx.Where("type = ? AND config_id = ?", config.Type, config.ConfigID).First(&existingConfig).Error; err == nil {
								log.Printf("配置已存在，将更新: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
								// 更新现有配置
								existingConfig.Name = config.Name
								existingConfig.Provider = config.Provider
								existingConfig.JsonData = config.JsonData
								existingConfig.Enabled = config.Enabled
								existingConfig.IsDefault = config.IsDefault
								if err := tx.Save(&existingConfig).Error; err != nil {
									log.Printf("更新配置失败: %v", err)
									tx.Rollback()
									c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update config"})
									return
								}
								log.Printf("配置更新成功: %s", configID)
							} else if err == gorm.ErrRecordNotFound {
								log.Printf("配置不存在，将创建新配置: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
								// 创建新配置
								if err := tx.Create(&config).Error; err != nil {
									log.Printf("创建配置失败: %v", err)
									tx.Rollback()
									c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create config"})
									return
								}
								log.Printf("配置创建成功: %s", configID)
							} else {
								log.Printf("查询配置时发生错误: %v", err)
								tx.Rollback()
								c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query existing config"})
								return
							}
						}
					}
				} else {
					// 对于不需要provider的模块（ota, mqtt, mqtt_server, udp, mcp, local_mcp），直接创建配置
					log.Printf("处理不需要provider的配置类型: %s", configType)
					jsonData, err := json.Marshal(configMap)
					if err != nil {
						log.Printf("序列化配置数据失败: %v", err)
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal config data"})
						return
					}

					config := models.Config{
						Type:      configType,
						Name:      configType,
						ConfigID:  configType,
						Provider:  "",
						JsonData:  string(jsonData),
						Enabled:   true,
						IsDefault: true,
					}

					log.Printf("准备保存配置: Type=%s, Name=%s, ConfigID=%s", config.Type, config.Name, config.ConfigID)

					// 先检查是否已存在相同配置
					var existingConfig models.Config
					if err := tx.Where("type = ? AND config_id = ?", config.Type, config.ConfigID).First(&existingConfig).Error; err == nil {
						log.Printf("配置已存在，将更新: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
						// 更新现有配置
						existingConfig.Name = config.Name
						existingConfig.Provider = config.Provider
						existingConfig.JsonData = config.JsonData
						existingConfig.Enabled = config.Enabled
						existingConfig.IsDefault = config.IsDefault
						if err := tx.Save(&existingConfig).Error; err != nil {
							log.Printf("更新配置失败: %v", err)
							tx.Rollback()
							c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update config"})
							return
						}
						log.Printf("配置更新成功: %s", configType)
					} else if err == gorm.ErrRecordNotFound {
						log.Printf("配置不存在，将创建新配置: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
						// 创建新配置
						if err := tx.Create(&config).Error; err != nil {
							log.Printf("创建配置失败: %v", err)
							tx.Rollback()
							c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create config"})
							return
						}
						log.Printf("配置创建成功: %s", configType)
					} else {
						log.Printf("查询配置时发生错误: %v", err)
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query existing config"})
						return
					}
				}
			}
		}
	}

	// 特殊处理vision配置
	log.Printf("开始处理vision配置")
	if visionData, exists := importConfig["vision"]; exists {
		log.Printf("找到vision配置数据")
		if visionMap, ok := visionData.(map[string]interface{}); ok {
			log.Printf("vision配置map keys: %v", getMapKeys(visionMap))

			// 处理vision的基础配置（enable_auth, vision_url等）
			baseVisionConfig := make(map[string]interface{})
			for key, value := range visionMap {
				if key != "vllm" {
					baseVisionConfig[key] = value
				}
			}

			// 保存vision基础配置
			if len(baseVisionConfig) > 0 {
				jsonData, err := json.Marshal(baseVisionConfig)
				if err != nil {
					log.Printf("序列化vision基础配置数据失败: %v", err)
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal vision base config data"})
					return
				}

				config := models.Config{
					Type:      "vision",
					Name:      "vision_base",
					ConfigID:  "vision_base",
					Provider:  "vision_base",
					JsonData:  string(jsonData),
					Enabled:   true,
					IsDefault: false,
				}

				log.Printf("准备保存vision基础配置: Type=%s, Name=%s, ConfigID=%s", config.Type, config.Name, config.ConfigID)

				// 先检查是否已存在相同配置
				var existingConfig models.Config
				if err := tx.Where("type = ? AND config_id = ?", config.Type, config.ConfigID).First(&existingConfig).Error; err == nil {
					log.Printf("vision基础配置已存在，将更新: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
					// 更新现有配置
					existingConfig.Name = config.Name
					existingConfig.Provider = config.Provider
					existingConfig.JsonData = config.JsonData
					existingConfig.Enabled = config.Enabled
					existingConfig.IsDefault = config.IsDefault
					if err := tx.Save(&existingConfig).Error; err != nil {
						log.Printf("更新vision基础配置失败: %v", err)
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update vision base config"})
						return
					}
					log.Printf("vision基础配置更新成功")
				} else if err == gorm.ErrRecordNotFound {
					log.Printf("vision基础配置不存在，将创建新配置: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
					// 创建新配置
					if err := tx.Create(&config).Error; err != nil {
						log.Printf("创建vision基础配置失败: %v", err)
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create vision base config"})
						return
					}
					log.Printf("vision基础配置创建成功")
				} else {
					log.Printf("查询vision基础配置时发生错误: %v", err)
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query existing vision base config"})
					return
				}
			}

			// 处理vllm配置
			if vllmData, exists := visionMap["vllm"]; exists {
				log.Printf("找到vllm配置数据")
				if vllmMap, ok := vllmData.(map[string]interface{}); ok {
					log.Printf("vllm配置map keys: %v", getMapKeys(vllmMap))

					// 获取vllm的provider字段
					var defaultProvider string
					if provider, exists := vllmMap["provider"]; exists {
						if providerStr, ok := provider.(string); ok {
							defaultProvider = providerStr
							log.Printf("vllm默认provider: %s", defaultProvider)
						}
					}

					log.Printf("vllm配置项keys: %v", getMapKeys(vllmMap))
					// 遍历所有vllm配置项
					for configID, configValue := range vllmMap {
						// 跳过provider字段
						if configID == "provider" {
							log.Printf("跳过vllm provider字段")
							continue
						}

						if configMap, ok := configValue.(map[string]interface{}); ok {
							log.Printf("处理vllm配置项: %s", configID)
							jsonData, err := json.Marshal(configMap)
							if err != nil {
								log.Printf("序列化vllm配置数据失败: %v", err)
								tx.Rollback()
								c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal vllm config data"})
								return
							}

							// 判断是否为默认配置
							isDefault := (configID == defaultProvider)
							log.Printf("vllm配置项 %s, 是否默认: %v", configID, isDefault)

							config := models.Config{
								Type:      "vision",
								Name:      configID,
								ConfigID:  configID,
								Provider:  configID,
								JsonData:  string(jsonData),
								Enabled:   true,
								IsDefault: isDefault,
							}

							log.Printf("准备保存vllm配置: Type=%s, Name=%s, ConfigID=%s", config.Type, config.Name, config.ConfigID)

							// 先检查是否已存在相同配置
							var existingConfig models.Config
							if err := tx.Where("type = ? AND config_id = ?", config.Type, config.ConfigID).First(&existingConfig).Error; err == nil {
								log.Printf("vllm配置已存在，将更新: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
								// 更新现有配置
								existingConfig.Name = config.Name
								existingConfig.Provider = config.Provider
								existingConfig.JsonData = config.JsonData
								existingConfig.Enabled = config.Enabled
								existingConfig.IsDefault = config.IsDefault
								if err := tx.Save(&existingConfig).Error; err != nil {
									log.Printf("更新vllm配置失败: %v", err)
									tx.Rollback()
									c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update vllm config"})
									return
								}
								log.Printf("vllm配置更新成功: %s", configID)
							} else if err == gorm.ErrRecordNotFound {
								log.Printf("vllm配置不存在，将创建新配置: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
								// 创建新配置
								if err := tx.Create(&config).Error; err != nil {
									log.Printf("创建vllm配置失败: %v", err)
									tx.Rollback()
									c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create vllm config"})
									return
								}
								log.Printf("vllm配置创建成功: %s", configID)
							} else {
								log.Printf("查询vllm配置时发生错误: %v", err)
								tx.Rollback()
								c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query existing vllm config"})
								return
							}
						}
					}
				}
			}
		}
	}

	// 特殊处理local_mcp配置
	log.Printf("开始处理local_mcp配置")
	if localMcpData, exists := importConfig["local_mcp"]; exists {
		log.Printf("找到local_mcp配置数据")
		if localMcpMap, ok := localMcpData.(map[string]interface{}); ok {
			log.Printf("local_mcp配置map keys: %v", getMapKeys(localMcpMap))

			jsonData, err := json.Marshal(localMcpMap)
			if err != nil {
				log.Printf("序列化local_mcp配置数据失败: %v", err)
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal local_mcp config data"})
				return
			}

			config := models.Config{
				Type:      "local_mcp",
				Name:      "local_mcp",
				ConfigID:  "local_mcp",
				Provider:  "",
				JsonData:  string(jsonData),
				Enabled:   true,
				IsDefault: true,
			}

			log.Printf("准备保存local_mcp配置: Type=%s, Name=%s, ConfigID=%s", config.Type, config.Name, config.ConfigID)

			// 先检查是否已存在相同配置
			var existingConfig models.Config
			if err := tx.Where("type = ? AND config_id = ?", config.Type, config.ConfigID).First(&existingConfig).Error; err == nil {
				log.Printf("local_mcp配置已存在，将更新: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
				// 更新现有配置
				existingConfig.Name = config.Name
				existingConfig.Provider = config.Provider
				existingConfig.JsonData = config.JsonData
				existingConfig.Enabled = config.Enabled
				existingConfig.IsDefault = config.IsDefault
				if err := tx.Save(&existingConfig).Error; err != nil {
					log.Printf("更新local_mcp配置失败: %v", err)
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update local_mcp config"})
					return
				}
				log.Printf("local_mcp配置更新成功")
			} else if err == gorm.ErrRecordNotFound {
				log.Printf("local_mcp配置不存在，将创建新配置: Type=%s, ConfigID=%s", config.Type, config.ConfigID)
				// 创建新配置
				if err := tx.Create(&config).Error; err != nil {
					log.Printf("创建local_mcp配置失败: %v", err)
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create local_mcp config"})
					return
				}
				log.Printf("local_mcp配置创建成功")
			} else {
				log.Printf("查询local_mcp配置时发生错误: %v", err)
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query existing local_mcp config"})
				return
			}
		}
	}

	// 提交事务
	log.Printf("提交事务")
	if err := tx.Commit().Error; err != nil {
		log.Printf("提交事务失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	log.Printf("配置导入成功")
	c.JSON(http.StatusOK, gin.H{"message": "Configuration imported successfully"})
}

// MCP配置相关方法
func (ac *AdminController) GetMCPConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "mcp").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取MCP配置列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateMCPConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	config.Type = "mcp"

	// 如果设置为默认配置，先取消其他同类型的默认配置
	if config.IsDefault {
		ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ?", config.Type, true).Update("is_default", false)
	}

	if err := ac.DB.Create(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建MCP配置失败"})
		return
	}
	ac.notifySystemConfigChanged()
	c.JSON(http.StatusCreated, gin.H{"data": config})
}

func (ac *AdminController) UpdateMCPConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var config models.Config

	if err := ac.DB.First(&config, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "MCP配置不存在"})
		return
	}

	var updateData models.Config
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 如果设置为默认配置，先取消其他同类型的默认配置
	if updateData.IsDefault {
		ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ? AND id != ?", config.Type, true, id).Update("is_default", false)
	}

	updateData.Type = "mcp"
	if err := ac.DB.Model(&config).Updates(updateData).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新MCP配置失败"})
		return
	}
	ac.notifySystemConfigChanged()
	c.JSON(http.StatusOK, gin.H{"data": config})
}

func (ac *AdminController) DeleteMCPConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var config models.Config

	if err := ac.DB.First(&config, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "MCP配置不存在"})
		return
	}

	if err := ac.DB.Delete(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除MCP配置失败"})
		return
	}
	ac.notifySystemConfigChanged()
	c.JSON(http.StatusOK, gin.H{"message": "MCP配置删除成功"})
}

// GenerateAgentMCPEndpoint 公共的MCP接入点生成函数
func GenerateAgentMCPEndpoint(db *gorm.DB, agentID string, userID uint) (string, error) {
	// 获取OTA配置中的外网WebSocket URL
	var otaConfig models.Config
	if err := db.Where("type = ? AND is_default = ?", "ota", true).First(&otaConfig).Error; err != nil {
		return "", fmt.Errorf("failed to get OTA config: %v", err)
	}

	var otaData map[string]interface{}
	if err := json.Unmarshal([]byte(otaConfig.JsonData), &otaData); err != nil {
		return "", fmt.Errorf("failed to parse OTA config: %v", err)
	}

	// 获取外网WebSocket URL
	externalURL, ok := otaData["external"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("external config not found in OTA config")
	}

	websocketConfig, ok := externalURL["websocket"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("websocket config not found in external config")
	}

	wsURL, ok := websocketConfig["url"].(string)
	if !ok || wsURL == "" {
		return "", fmt.Errorf("websocket URL not found in external config")
	}

	// 解析OTA URL，只取域名部分，保持ws或wss协议不变
	parsedURL, err := url.Parse(wsURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse WebSocket URL: %v", err)
	}

	// 构建基础URL（只包含协议和域名）
	baseURL := fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)

	// 生成MCP JWT token
	token, err := generateMCPToken(agentID, userID)
	if err != nil {
		return "", fmt.Errorf("failed to generate MCP token: %v", err)
	}

	// 构建带token的完整endpoint URL，直接使用/mcp路径
	endpointWithToken := fmt.Sprintf("%s/mcp?token=%s", baseURL, token)

	return endpointWithToken, nil
}

// Memory配置管理
func (ac *AdminController) GetMemoryConfigs(c *gin.Context) {
	var configs []models.Config
	if err := ac.DB.Where("type = ?", "memory").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取Memory配置列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": configs})
}

func (ac *AdminController) CreateMemoryConfig(c *gin.Context) {
	var config models.Config
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 设置配置类型为memory
	config.Type = "memory"

	// 验证provider字段
	if config.Provider != "memobase" && config.Provider != "mem0" && config.Provider != "memos" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provider必须是memobase、mem0或memos"})
		return
	}

	// 如果设置为默认配置，先取消其他同类型的默认配置
	if config.IsDefault {
		ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ?", config.Type, true).Update("is_default", false)
	}

	if err := ac.DB.Create(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建Memory配置失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": config})
}

func (ac *AdminController) UpdateMemoryConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var config models.Config

	if err := ac.DB.Where("id = ? AND type = ?", id, "memory").First(&config).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Memory配置不存在"})
		return
	}

	var updateData models.Config
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 验证provider字段
	if updateData.Provider != "memobase" && updateData.Provider != "mem0" && updateData.Provider != "memos" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Provider必须是memobase、mem0或memos"})
		return
	}

	// 如果设置为默认配置，先取消其他同类型的默认配置
	if updateData.IsDefault {
		ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ? AND id != ?", config.Type, true, id).Update("is_default", false)
	}

	// 更新配置
	config.Name = updateData.Name
	config.Provider = updateData.Provider
	config.JsonData = updateData.JsonData
	config.Enabled = updateData.Enabled
	config.IsDefault = updateData.IsDefault

	if err := ac.DB.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新Memory配置失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": config})
}

func (ac *AdminController) DeleteMemoryConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := ac.DB.Where("id = ? AND type = ?", id, "memory").Delete(&models.Config{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除Memory配置失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// 设置默认Memory配置
func (ac *AdminController) SetDefaultMemoryConfig(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var config models.Config

	if err := ac.DB.Where("id = ? AND type = ?", id, "memory").First(&config).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Memory配置不存在"})
		return
	}

	// 先取消其他同类型的默认配置
	ac.DB.Model(&models.Config{}).Where("type = ? AND is_default = ?", config.Type, true).Update("is_default", false)

	// 设置当前配置为默认
	config.IsDefault = true
	if err := ac.DB.Save(&config).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设置默认Memory配置失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "设置默认Memory配置成功", "data": config})
}

// generateMCPToken 生成稳定的MCP JWT Token（同一agentID+userID下保持不变）
func generateMCPToken(agentID string, userID uint) (string, error) {
	// 创建自定义的JWT Claims
	type MCPClaims struct {
		UserID     uint   `json:"userId"`
		AgentID    string `json:"agentId"`
		EndpointID string `json:"endpointId"`
		Purpose    string `json:"purpose"`
		jwt.RegisteredClaims
	}

	// 构建endpointId
	endpointID := fmt.Sprintf("agent_%s", agentID)

	// 创建JWT claims。
	// 不设置iat/exp，保证token长期有效且同一agentID+userID生成结果稳定一致。
	claims := MCPClaims{
		UserID:           userID,
		AgentID:          agentID,
		EndpointID:       endpointID,
		Purpose:          "mcp-endpoint",
		RegisteredClaims: jwt.RegisteredClaims{},
	}

	// 使用HS256算法生成JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 使用与middleware相同的密钥
	jwtSecret := []byte("xiaozhi_admin_secret_key")
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		return "", err
	}

	return tokenString, nil
}

// ==================== 新角色管理 API ====================

// GetGlobalRolesNew 获取全局角色列表（仅 roles 表中的全局角色）
func (ac *AdminController) GetGlobalRolesNew(c *gin.Context) {
	var globalRoles []models.Role
	if err := ac.DB.Where("user_id IS NULL AND role_type = ?", "global").
		Order("sort_order ASC, id ASC").
		Find(&globalRoles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取全局角色失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": globalRoles})
}

// GetRolesNew 获取角色列表（全局角色 + 用户角色）
// 管理员可以查看所有角色，普通用户只能查看全局角色和自己的角色
func (ac *AdminController) GetRolesNew(c *gin.Context) {
	// 从JWT中获取用户ID和角色
	userID, exists := c.Get("user_id")
	userRole, roleExists := c.Get("role")

	// 查询全局角色
	var globalRoles []models.Role
	if err := ac.DB.Where("user_id IS NULL AND role_type = ?", "global").
		Order("sort_order ASC, id ASC").
		Find(&globalRoles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取全局角色失败"})
		return
	}

	// 查询用户角色
	var userRoles []models.Role
	if roleExists && userRole.(string) == "admin" {
		// 管理员查看所有用户角色
		if err := ac.DB.Where("role_type = ?", "user").
			Order("created_at DESC").
			Find(&userRoles).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户角色失败"})
			return
		}
	} else if exists {
		// 普通用户只查看自己的角色
		if err := ac.DB.Where("user_id = ? AND role_type = ?", userID, "user").
			Order("created_at DESC").
			Find(&userRoles).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户角色失败"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"global_roles": globalRoles,
			"user_roles":   userRoles,
		},
	})
}

// GetRoleNew 获取单个角色详情
func (ac *AdminController) GetRoleNew(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var role models.Role

	if err := ac.DB.First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "角色不存在"})
		return
	}
	if strings.Contains(c.FullPath(), "/admin/roles/global/") && role.RoleType != "global" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该接口仅允许操作全局角色"})
		return
	}

	// 权限检查：用户角色只能查看自己的角色
	if role.UserID != nil {
		userID, exists := c.Get("user_id")
		userRole, roleExists := c.Get("role")

		if roleExists && userRole.(string) != "admin" {
			if exists && userID != nil {
				uid := userID.(uint)
				if uid != *role.UserID {
					c.JSON(http.StatusForbidden, gin.H{"error": "无权访问此角色"})
					return
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"data": role})
}

func normalizeRoleStatus(status string) string {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return "active"
	}
	return trimmed
}

// CreateRoleNew 创建角色（管理员创建全局角色，用户创建自己的角色）
func (ac *AdminController) CreateRoleNew(c *gin.Context) {
	userID, exists := c.Get("user_id")
	userRole, roleExists := c.Get("role")

	var role models.Role
	if err := c.ShouldBindJSON(&role); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 设置角色类型和所属用户
	if roleExists && userRole.(string) == "admin" {
		// 管理员创建全局角色
		role.RoleType = "global"
		role.UserID = nil
	} else if exists {
		// 普通用户创建自己的角色
		role.RoleType = "user"
		uid := userID.(uint)
		role.UserID = &uid
		// 用户角色不能设为默认
		role.IsDefault = false
	} else {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未授权"})
		return
	}

	// 验证必填字段
	if role.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色名称不能为空"})
		return
	}
	if role.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "系统提示词不能为空"})
		return
	}

	role.Status = normalizeRoleStatus(role.Status)
	if role.Status != "active" && role.Status != "inactive" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色状态无效"})
		return
	}

	// 如果设置为默认角色，先取消其他默认角色
	if role.IsDefault && role.RoleType == "global" {
		ac.DB.Model(&models.Role{}).
			Where("role_type = ? AND is_default = ?", "global", true).
			Update("is_default", false)
	}

	if err := ac.DB.Create(&role).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建角色失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": role})
}

// UpdateRoleNew 更新角色
func (ac *AdminController) UpdateRoleNew(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var role models.Role

	if err := ac.DB.First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "角色不存在"})
		return
	}
	if strings.Contains(c.FullPath(), "/admin/roles/global/") && role.RoleType != "global" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该接口仅允许操作全局角色"})
		return
	}

	// 权限检查
	userID, exists := c.Get("user_id")
	userRole, roleExists := c.Get("role")

	isAdmin := roleExists && userRole.(string) == "admin"
	isOwner := false
	if exists && role.UserID != nil {
		if uid, ok := userID.(uint); ok {
			isOwner = uid == *role.UserID
		}
	}

	if !isAdmin && !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权修改此角色"})
		return
	}

	var updateData models.Role
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 如果设置为默认角色，先取消其他默认角色
	if updateData.IsDefault && role.RoleType == "global" {
		ac.DB.Model(&models.Role{}).
			Where("role_type = ? AND is_default = ? AND id != ?", "global", true, id).
			Update("is_default", false)
	}

	// 更新字段
	role.Name = updateData.Name
	role.Description = updateData.Description
	role.Prompt = updateData.Prompt
	role.LLMConfigID = updateData.LLMConfigID
	role.TTSConfigID = updateData.TTSConfigID
	role.Voice = updateData.Voice
	role.SortOrder = updateData.SortOrder

	normalizedStatus := strings.TrimSpace(updateData.Status)
	if normalizedStatus == "" {
		normalizedStatus = role.Status
	}
	normalizedStatus = normalizeRoleStatus(normalizedStatus)
	if normalizedStatus != "active" && normalizedStatus != "inactive" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色状态无效"})
		return
	}
	role.Status = normalizedStatus

	// 只有管理员可以修改默认标志和角色类型
	if isAdmin {
		role.IsDefault = updateData.IsDefault
	}

	if err := ac.DB.Save(&role).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新角色失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": role})
}

// DeleteRoleNew 删除角色
func (ac *AdminController) DeleteRoleNew(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var role models.Role

	if err := ac.DB.First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "角色不存在"})
		return
	}
	if strings.Contains(c.FullPath(), "/admin/roles/global/") && role.RoleType != "global" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该接口仅允许操作全局角色"})
		return
	}

	// 权限检查
	userID, exists := c.Get("user_id")
	userRole, roleExists := c.Get("role")

	isAdmin := roleExists && userRole.(string) == "admin"
	isOwner := false
	if exists && role.UserID != nil {
		if uid, ok := userID.(uint); ok {
			isOwner = uid == *role.UserID
		}
	}

	if !isAdmin && !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权删除此角色"})
		return
	}

	// 检查是否有设备正在使用此角色
	var deviceCount int64
	ac.DB.Model(&models.Device{}).Where("role_id = ?", id).Count(&deviceCount)
	if deviceCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("有 %d 个设备正在使用此角色，请先解除关联", deviceCount),
		})
		return
	}

	if err := ac.DB.Delete(&role).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除角色失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}

// ToggleRoleStatus 切换角色状态（启用/禁用）
func (ac *AdminController) ToggleRoleStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var role models.Role

	if err := ac.DB.First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "角色不存在"})
		return
	}
	if strings.Contains(c.FullPath(), "/admin/roles/global/") && role.RoleType != "global" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该接口仅允许操作全局角色"})
		return
	}

	// 权限检查
	userID, exists := c.Get("user_id")
	userRole, roleExists := c.Get("role")

	isAdmin := roleExists && userRole.(string) == "admin"
	isOwner := false
	if exists && role.UserID != nil {
		if uid, ok := userID.(uint); ok {
			isOwner = uid == *role.UserID
		}
	}

	if !isAdmin && !isOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权修改此角色"})
		return
	}

	// 切换状态
	currentStatus := normalizeRoleStatus(role.Status)
	if currentStatus == "active" {
		role.Status = "inactive"
	} else {
		role.Status = "active"
	}

	if err := ac.DB.Save(&role).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新状态失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": role})
}

// SetDefaultRole 设置默认角色（仅全局角色）
func (ac *AdminController) SetDefaultRole(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var role models.Role

	if err := ac.DB.First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "角色不存在"})
		return
	}

	// 只有全局角色可以设为默认
	if role.RoleType != "global" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只有全局角色可以设为默认"})
		return
	}

	// 权限检查：只有管理员可以设置默认角色
	userRole, roleExists := c.Get("role")
	if !roleExists || userRole.(string) != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有管理员可以设置默认角色"})
		return
	}

	// 先取消其他默认角色
	ac.DB.Model(&models.Role{}).
		Where("role_type = ? AND is_default = ?", "global", true).
		Update("is_default", false)

	// 设置当前角色为默认
	role.IsDefault = true
	if err := ac.DB.Save(&role).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设置默认角色失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": role, "message": "已设为默认角色"})
}

type applyDeviceRoleRequest struct {
	RoleID *uint `json:"role_id"`
}

type switchDeviceRoleByNameRequest struct {
	RoleName string `json:"role_name"`
}

func normalizeRoleNameForMatch(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, " ", "")
	return normalized
}

func calcRoleMatchScore(requestedRoleName string, candidateRoleName string) (int, string) {
	reqCompact := normalizeRoleNameForMatch(requestedRoleName)
	candCompact := normalizeRoleNameForMatch(candidateRoleName)
	if reqCompact == "" || candCompact == "" {
		return -1, ""
	}

	if reqCompact == candCompact {
		return 1000, "exact"
	}

	if strings.Contains(candCompact, reqCompact) || strings.Contains(reqCompact, candCompact) {
		score := 700 - absInt(len(candCompact)-len(reqCompact))
		if strings.HasPrefix(candCompact, reqCompact) || strings.HasPrefix(reqCompact, candCompact) {
			score += 50
		}
		return score, "fuzzy"
	}

	reqRaw := strings.ToLower(strings.TrimSpace(requestedRoleName))
	candRaw := strings.ToLower(strings.TrimSpace(candidateRoleName))
	if reqRaw != "" && candRaw != "" && (strings.Contains(candRaw, reqRaw) || strings.Contains(reqRaw, candRaw)) {
		score := 600 - absInt(len(candRaw)-len(reqRaw))
		return score, "fuzzy"
	}

	return -1, ""
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func matchDeviceRoleByName(requestedRoleName string, roles []models.Role) (*models.Role, string) {
	bestScore := -1
	bestMatchType := ""
	var bestRole *models.Role

	for i := range roles {
		role := &roles[i]
		if normalizeRoleStatus(role.Status) != "active" {
			continue
		}

		score, matchType := calcRoleMatchScore(requestedRoleName, role.Name)
		if score > bestScore {
			bestScore = score
			bestMatchType = matchType
			bestRole = role
		}
	}

	if bestScore < 0 {
		return nil, ""
	}
	return bestRole, bestMatchType
}

func getRequestUserInfo(c *gin.Context) (uint, bool, bool) {
	var uid uint
	userID, hasUserID := c.Get("user_id")
	if hasUserID {
		if v, ok := userID.(uint); ok {
			uid = v
		}
	}

	roleVal, hasRole := c.Get("role")
	isAdmin := hasRole && roleVal == "admin"
	return uid, hasUserID, isAdmin
}

// ApplyRoleToDevice 应用角色到设备（普通用户可操作自己的设备）
func (ac *AdminController) ApplyRoleToDevice(c *gin.Context) {
	deviceID, err := strconv.Atoi(c.Param("id"))
	if err != nil || deviceID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的设备ID"})
		return
	}

	var req applyDeviceRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var device models.Device
	if err := ac.DB.First(&device, deviceID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在"})
		return
	}

	uid, hasUserID, isAdmin := getRequestUserInfo(c)
	if !isAdmin {
		if !hasUserID || device.UserID != uid {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权操作该设备"})
			return
		}
	}

	if req.RoleID != nil {
		var role models.Role
		if err := ac.DB.First(&role, *req.RoleID).Error; err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "角色不存在"})
			return
		}

		roleStatus := normalizeRoleStatus(role.Status)
		if roleStatus != "active" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "角色未启用"})
			return
		}
		if role.Status == "" {
			if err := ac.DB.Model(&role).Update("status", roleStatus).Error; err != nil {
				log.Printf("更新角色默认状态失败: role_id=%d err=%v", role.ID, err)
			}
		}

		// 普通用户只允许使用全局角色或自己的用户角色
		if !isAdmin {
			if role.RoleType != "global" {
				if role.UserID == nil || *role.UserID != uid {
					c.JSON(http.StatusForbidden, gin.H{"error": "无权使用该角色"})
					return
				}
			}
		}
	}

	device.RoleID = req.RoleID
	if err := ac.DB.Save(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "应用角色失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"device_id": device.ID,
			"role_id":   device.RoleID,
		},
	})
}

// SwitchDeviceRoleByNameInternal 内部接口：按角色名称（模糊匹配）切换设备角色
func (ac *AdminController) SwitchDeviceRoleByNameInternal(c *gin.Context) {
	deviceName := strings.TrimSpace(c.Param("device_name"))
	if deviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "设备名称不能为空"})
		return
	}

	var req switchDeviceRoleByNameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.RoleName = strings.TrimSpace(req.RoleName)
	if req.RoleName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role_name 不能为空"})
		return
	}

	var device models.Device
	if err := ac.DB.Where("device_name = ?", deviceName).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在"})
		return
	}

	var roles []models.Role
	if err := ac.DB.
		Where("(role_type = ? OR (role_type = ? AND user_id = ?))", "global", "user", device.UserID).
		Order("sort_order ASC, id ASC").
		Find(&roles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询角色失败"})
		return
	}

	matchedRole, matchType := matchDeviceRoleByName(req.RoleName, roles)
	if matchedRole == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":               "未找到匹配的角色",
			"requested_role_name": req.RoleName,
		})
		return
	}

	roleID := matchedRole.ID
	device.RoleID = &roleID
	if err := ac.DB.Save(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "切换设备角色失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"device_id":           device.ID,
			"device_name":         device.DeviceName,
			"role_id":             device.RoleID,
			"role_name":           matchedRole.Name,
			"role_type":           matchedRole.RoleType,
			"requested_role_name": req.RoleName,
			"match_type":          matchType,
		},
	})
}

// RestoreDeviceDefaultRoleInternal 内部接口：恢复设备默认角色（清空设备绑定角色）
func (ac *AdminController) RestoreDeviceDefaultRoleInternal(c *gin.Context) {
	deviceName := strings.TrimSpace(c.Param("device_name"))
	if deviceName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "设备名称不能为空"})
		return
	}

	var device models.Device
	if err := ac.DB.Where("device_name = ?", deviceName).First(&device).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "设备不存在"})
		return
	}

	device.RoleID = nil
	if err := ac.DB.Save(&device).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "恢复默认角色失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"device_id":   device.ID,
			"device_name": device.DeviceName,
			"role_id":     device.RoleID,
		},
	})
}
