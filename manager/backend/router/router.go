package router

import (
	"io/fs"
	"net/http"
	"xiaozhi/manager/backend/config"
	"xiaozhi/manager/backend/controllers"
	"xiaozhi/manager/backend/middleware"
	"xiaozhi/manager/backend/static"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Setup(db *gorm.DB, cfg *config.Config) *gin.Engine {
	r := gin.Default()
	internalAuthToken := config.ResolveInternalAuthToken(cfg)
	endpointAuthToken := config.ResolveEndpointAuthToken(cfg)

	// CORS配置
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization", "X-API-Token"}
	corsConfig.AllowCredentials = true
	r.Use(cors.New(corsConfig))

	// 初始化控制器
	authController := &controllers.AuthController{DB: db}
	webSocketController := controllers.NewWebSocketController(db, endpointAuthToken)
	adminController := &controllers.AdminController{
		DB:                  db,
		WebSocketController: webSocketController,
		InternalAuthToken:   internalAuthToken,
		EndpointAuthToken:   endpointAuthToken,
	}
	userController := &controllers.UserController{
		DB:                  db,
		WebSocketController: webSocketController,
		InternalAuthToken:   internalAuthToken,
		EndpointAuthToken:   endpointAuthToken,
	}
	deviceActivationController := &controllers.DeviceActivationController{DB: db}
	setupController := &controllers.SetupController{DB: db}
	speakerGroupController := controllers.NewSpeakerGroupController(db, cfg)
	voiceCloneController := controllers.NewVoiceCloneController(db, cfg)
	poolStatsController := controllers.NewPoolStatsController()

	// 初始化聊天历史控制器（使用传入的 cfg，不重新 Load 避免内嵌时读错路径）
	audioBasePath := "./storage/chat_history/audio"
	maxFileSize := int64(10 * 1024 * 1024) // 默认10MB
	if cfg.History.AudioBasePath != "" {
		audioBasePath = cfg.History.AudioBasePath
	}
	if cfg.History.MaxFileSize > 0 {
		maxFileSize = cfg.History.MaxFileSize
	}
	chatHistoryController := &controllers.ChatHistoryController{
		DB:            db,
		AudioBasePath: audioBasePath,
		MaxFileSize:   maxFileSize,
	}

	// API路由组
	api := r.Group("/api")
	{
		// 公开路由（无需认证）
		api.POST("/login", authController.Login)
		api.POST("/register", authController.Register)

		// 数据库初始化相关路由（无需认证）
		api.GET("/setup/status", setupController.CheckSetupStatus)
		api.POST("/setup/initialize", setupController.InitializeDatabase)

		// 内部服务接口（服务间 Token 认证）
		internal := api.Group("")
		internal.Use(middleware.InternalServiceAuth(internalAuthToken))
		{
			internal.GET("/internal/device/check-activation", deviceActivationController.CheckDeviceActivation)
			internal.GET("/internal/device/activation-info", deviceActivationController.GetActivationInfo)
			internal.POST("/internal/device/activate", deviceActivationController.ActivateDevice)

			internal.GET("/configs", adminController.GetDeviceConfigs)
			internal.GET("/system/configs", adminController.GetSystemConfigs)
			internal.POST("/internal/history/messages", chatHistoryController.SaveMessage)                         // 保存消息（内部服务接口）
			internal.PUT("/internal/history/messages/:message_id/audio", chatHistoryController.UpdateMessageAudio) // 更新消息音频（内部服务接口）
			internal.GET("/internal/history/messages", chatHistoryController.GetMessagesForInit)                   // 获取消息（用于初始化加载，内部服务接口）
			internal.POST("/internal/pool/stats", poolStatsController.ReportPoolStats)                             // 上报资源池统计数据（内部服务接口）
			internal.POST("/internal/devices/:device_name/switch-role", adminController.SwitchDeviceRoleByNameInternal)
			internal.POST("/internal/devices/:device_name/restore-default-role", adminController.RestoreDeviceDefaultRoleInternal)
		}

		// 需要认证的路由
		auth := api.Group("")
		auth.Use(middleware.JWTAuth())
		{
			auth.GET("/profile", authController.GetProfile)
			// 通用接口，获取系统中的设备信息
			auth.GET("/dashboard/stats", userController.GetDashboardStats)
			// 设备角色接口（管理员和普通用户均可访问，控制器内做权限校验）
			auth.POST("/devices/:id/apply-role", adminController.ApplyRoleToDevice)

			// 角色管理（文档主路径）
			auth.GET("/roles", adminController.GetRolesNew)
			auth.GET("/roles/:id", adminController.GetRoleNew)
			auth.POST("/roles", adminController.CreateRoleNew)
			auth.PUT("/roles/:id", adminController.UpdateRoleNew)
			auth.DELETE("/roles/:id", adminController.DeleteRoleNew)
			auth.PATCH("/roles/:id/toggle", adminController.ToggleRoleStatus)

			// 用户路由
			user := auth.Group("/user")
			{
				// 角色管理
				user.GET("/roles", adminController.GetRolesNew)
				user.GET("/roles/:id", adminController.GetRoleNew)
				user.POST("/roles", adminController.CreateRoleNew)
				user.PUT("/roles/:id", adminController.UpdateRoleNew)
				user.DELETE("/roles/:id", adminController.DeleteRoleNew)
				user.PATCH("/roles/:id/toggle", adminController.ToggleRoleStatus)

				// API Token（供OpenAPI调用）
				user.GET("/api-tokens", userController.ListAPITokens)
				user.POST("/api-tokens", userController.CreateAPIToken)
				user.DELETE("/api-tokens/:id", userController.RevokeAPIToken)

				// 设备管理
				user.GET("/devices", userController.GetMyDevices)
				user.POST("/devices", userController.CreateDevice)

				// 智能体管理
				user.GET("/agents", userController.GetAgents)
				user.POST("/agents", userController.CreateAgent)
				user.GET("/agents/:id", userController.GetAgent)
				user.PUT("/agents/:id", userController.UpdateAgent)
				user.DELETE("/agents/:id", userController.DeleteAgent)
				user.GET("/agents/:id/devices", userController.GetAgentDevices)
				user.POST("/agents/:id/devices", userController.AddDeviceToAgent)
				user.DELETE("/agents/:id/devices/:device_id", userController.RemoveDeviceFromAgent)
				user.GET("/agents/:id/knowledge-bases", userController.GetAgentKnowledgeBases)
				user.PUT("/agents/:id/knowledge-bases", userController.UpdateAgentKnowledgeBases)

				// 用户知识库管理（纯文本）
				user.GET("/knowledge-bases", userController.GetKnowledgeBases)
				user.POST("/knowledge-bases", userController.CreateKnowledgeBase)
				user.GET("/knowledge-bases/:id", userController.GetKnowledgeBase)
				user.PUT("/knowledge-bases/:id", userController.UpdateKnowledgeBase)
				user.DELETE("/knowledge-bases/:id", userController.DeleteKnowledgeBase)
				user.POST("/knowledge-bases/:id/sync", userController.SyncKnowledgeBase)
				user.POST("/knowledge-bases/:id/test-search", userController.TestKnowledgeBaseSearch)
				user.GET("/knowledge-bases/:id/documents", userController.GetKnowledgeBaseDocuments)
				user.POST("/knowledge-bases/:id/documents", userController.CreateKnowledgeBaseDocument)
				user.POST("/knowledge-bases/:id/documents/upload", userController.CreateKnowledgeBaseDocumentByUpload)
				user.PUT("/knowledge-bases/:id/documents/:doc_id", userController.UpdateKnowledgeBaseDocument)
				user.DELETE("/knowledge-bases/:id/documents/:doc_id", userController.DeleteKnowledgeBaseDocument)
				user.POST("/knowledge-bases/:id/documents/:doc_id/sync", userController.SyncKnowledgeBaseDocument)

				// 角色模板和音色选项
				user.GET("/role-templates", userController.GetRoleTemplates)
				user.GET("/voice-options", userController.GetVoiceOptions)
				user.GET("/voice-clone/capabilities", voiceCloneController.GetCloneProviderCapabilities)
				user.POST("/voice-clones", voiceCloneController.CreateVoiceClone)
				user.GET("/voice-clones", voiceCloneController.GetVoiceClones)
				user.PUT("/voice-clones/:id", voiceCloneController.UpdateVoiceClone)
				user.DELETE("/voice-clones/:id", voiceCloneController.DeleteVoiceClone)
				user.POST("/voice-clones/:id/retry", voiceCloneController.RetryVoiceClone)
				user.POST("/voice-clones/:id/append-audio", voiceCloneController.AppendVoiceCloneAudio)
				user.GET("/voice-clones/:id/preview", voiceCloneController.PreviewClonedVoice)
				user.GET("/voice-clones/:id/audios", voiceCloneController.GetVoiceCloneAudios)
				user.GET("/voice-clones/audios/:audio_id/file", voiceCloneController.GetVoiceCloneAudioFile)

				// 角色管理（暂时注释，待实现）
				// user.GET("/roles", adminController.GetRoles)
				// user.GET("/roles/:id", adminController.GetRole)
				// user.POST("/roles", adminController.CreateRole)
				// user.PUT("/roles/:id", adminController.UpdateRole)
				// user.DELETE("/roles/:id", adminController.DeleteRole)

				// 配置列表
				user.GET("/llm-configs", userController.GetLLMConfigs)
				user.GET("/tts-configs", userController.GetTTSConfigs)

				// MCP接入点
				user.GET("/agents/:id/mcp-services/options", userController.GetAgentMCPServiceOptions)
				user.GET("/agents/:id/mcp-endpoint", userController.GetAgentMCPEndpoint)
				user.GET("/agents/:id/openclaw-endpoint", userController.GetAgentOpenClawEndpoint)
				user.POST("/agents/:id/openclaw-chat-test", userController.CallAgentOpenClawChatTest)
				user.GET("/agents/:id/mcp-tools", userController.GetAgentMcpTools)
				user.POST("/agents/:id/mcp-call", userController.CallAgentMcpTool)
				user.GET("/devices/:id/mcp-tools", userController.GetDeviceMcpTools)
				user.POST("/devices/:id/mcp-call", userController.CallDeviceMcpTool)

				// 消息注入
				user.POST("/devices/inject-message", userController.InjectMessage)

				// 声纹组管理
				user.POST("/speaker-groups", speakerGroupController.CreateSpeakerGroup)
				user.GET("/speaker-groups", speakerGroupController.GetSpeakerGroups)
				user.GET("/speaker-groups/:id", speakerGroupController.GetSpeakerGroup)
				user.PUT("/speaker-groups/:id", speakerGroupController.UpdateSpeakerGroup)
				user.DELETE("/speaker-groups/:id", speakerGroupController.DeleteSpeakerGroup)
				user.POST("/speaker-groups/:id/verify", speakerGroupController.VerifySpeakerGroup)

				// 声纹样本管理（注意：使用 :id 而不是 :group_id，避免路由冲突）
				user.POST("/speaker-groups/:id/samples", speakerGroupController.AddSample)
				user.GET("/speaker-groups/:id/samples", speakerGroupController.GetSamples)
				user.GET("/speaker-groups/:id/samples/:sample_id/file", speakerGroupController.GetSampleFile)
				user.DELETE("/speaker-groups/:id/samples/:sample_id", speakerGroupController.DeleteSample)

				// 聊天历史
				user.GET("/history/messages", chatHistoryController.GetMessages)
				user.DELETE("/history/messages/:id", chatHistoryController.DeleteMessage)
				user.GET("/history/export", chatHistoryController.ExportMessages)
				user.GET("/history/agents/:agent_id/messages", chatHistoryController.GetMessagesByAgent)
				user.GET("/history/messages/:id/audio", chatHistoryController.GetAudioFile)
			}

			// 外部OpenAPI路由（支持JWT或API Token）
			openV1 := api.Group("/open/v1")
			openV1.Use(middleware.OpenAPIAuth(db))
			{
				openV1.GET("/profile", authController.GetProfile)
				openV1.GET("/devices", userController.GetMyDevices)
				openV1.POST("/devices", userController.CreateDevice)
				openV1.GET("/agents", userController.GetAgents)
				openV1.POST("/agents", userController.CreateAgent)
				openV1.GET("/agents/:id", userController.GetAgent)
				openV1.PUT("/agents/:id", userController.UpdateAgent)
				openV1.DELETE("/agents/:id", userController.DeleteAgent)
				openV1.GET("/history/messages", chatHistoryController.GetMessages)
				openV1.GET("/history/export", chatHistoryController.ExportMessages)
				openV1.POST("/devices/inject-message", userController.InjectMessage)
				openV1.GET("/agents/:id/mcp-tools", userController.GetAgentMcpTools)
				openV1.POST("/agents/:id/mcp-call", userController.CallAgentMcpTool)
				openV1.GET("/devices/:id/mcp-tools", userController.GetDeviceMcpTools)
				openV1.POST("/devices/:id/mcp-call", userController.CallDeviceMcpTool)
			}

			// 管理员路由
			admin := auth.Group("/admin")
			admin.Use(middleware.AdminAuth())
			{
				// 通用配置管理
				admin.GET("/configs", adminController.GetConfigs)
				admin.POST("/configs", adminController.CreateConfig)
				admin.GET("/configs/:id", adminController.GetConfig)
				admin.PUT("/configs/:id", adminController.UpdateConfig)
				admin.DELETE("/configs/:id", adminController.DeleteConfig)
				admin.POST("/configs/:id/toggle", adminController.ToggleConfigEnable)

				// 具体配置类型路由（兼容前端）
				admin.GET("/vad-configs", adminController.GetVADConfigs)
				admin.POST("/vad-configs", adminController.CreateVADConfig)
				admin.PUT("/vad-configs/:id", adminController.UpdateVADConfig)
				admin.DELETE("/vad-configs/:id", adminController.DeleteVADConfig)

				admin.GET("/asr-configs", adminController.GetASRConfigs)
				admin.POST("/asr-configs", adminController.CreateASRConfig)
				admin.PUT("/asr-configs/:id", adminController.UpdateASRConfig)
				admin.DELETE("/asr-configs/:id", adminController.DeleteASRConfig)

				admin.GET("/llm-configs", adminController.GetLLMConfigs)
				admin.POST("/llm-configs", adminController.CreateLLMConfig)
				admin.PUT("/llm-configs/:id", adminController.UpdateLLMConfig)
				admin.DELETE("/llm-configs/:id", adminController.DeleteLLMConfig)

				admin.GET("/tts-configs", adminController.GetTTSConfigs)
				admin.POST("/tts-configs", adminController.CreateTTSConfig)
				admin.PUT("/tts-configs/:id", adminController.UpdateTTSConfig)
				admin.DELETE("/tts-configs/:id", adminController.DeleteTTSConfig)

				admin.GET("/speaker-configs", adminController.GetSpeakerConfigs)
				admin.POST("/speaker-configs", adminController.CreateSpeakerConfig)
				admin.PUT("/speaker-configs/:id", adminController.UpdateSpeakerConfig)
				admin.DELETE("/speaker-configs/:id", adminController.DeleteSpeakerConfig)

				admin.GET("/vision-configs", adminController.GetVisionConfigs)
				admin.POST("/vision-configs", adminController.CreateVisionConfig)
				admin.PUT("/vision-configs/:id", adminController.UpdateVisionConfig)
				admin.DELETE("/vision-configs/:id", adminController.DeleteVisionConfig)

				// Vision基础配置
				admin.GET("/vision-base-config", adminController.GetVisionBaseConfig)
				admin.PUT("/vision-base-config", adminController.UpdateVisionBaseConfig)

				// 聊天设置（auth/chat）
				admin.GET("/chat-settings", adminController.GetChatSettings)
				admin.PUT("/chat-settings", adminController.UpdateChatSettings)

				admin.GET("/ota-configs", adminController.GetOTAConfigs)
				admin.POST("/ota-configs", adminController.CreateOTAConfig)
				admin.PUT("/ota-configs/:id", adminController.UpdateOTAConfig)
				admin.DELETE("/ota-configs/:id", adminController.DeleteOTAConfig)

				admin.GET("/mqtt-configs", adminController.GetMQTTConfigs)
				admin.POST("/mqtt-configs", adminController.CreateMQTTConfig)
				admin.PUT("/mqtt-configs/:id", adminController.UpdateMQTTConfig)
				admin.DELETE("/mqtt-configs/:id", adminController.DeleteMQTTConfig)

				admin.GET("/mqtt-server-configs", adminController.GetMQTTServerConfigs)
				admin.POST("/mqtt-server-configs", adminController.CreateMQTTServerConfig)
				admin.PUT("/mqtt-server-configs/:id", adminController.UpdateMQTTServerConfig)
				admin.DELETE("/mqtt-server-configs/:id", adminController.DeleteMQTTServerConfig)

				admin.GET("/udp-configs", adminController.GetUDPConfigs)
				admin.POST("/udp-configs", adminController.CreateUDPConfig)
				admin.PUT("/udp-configs/:id", adminController.UpdateUDPConfig)
				admin.DELETE("/udp-configs/:id", adminController.DeleteUDPConfig)

				admin.GET("/mcp-configs", adminController.GetMCPConfigs)
				admin.POST("/mcp-configs", adminController.CreateMCPConfig)
				admin.POST("/mcp-configs/discover-tools", adminController.DiscoverMCPConfigTools)
				admin.PUT("/mcp-configs/:id", adminController.UpdateMCPConfig)
				admin.DELETE("/mcp-configs/:id", adminController.DeleteMCPConfig)
				admin.GET("/mcp-markets", adminController.GetMCPMarkets)
				admin.POST("/mcp-markets", adminController.CreateMCPMarket)
				admin.PUT("/mcp-markets/:id", adminController.UpdateMCPMarket)
				admin.DELETE("/mcp-markets/:id", adminController.DeleteMCPMarket)
				admin.POST("/mcp-markets/:id/test", adminController.TestMCPMarket)
				admin.GET("/mcp-market/providers", adminController.GetMCPMarketProviders)
				admin.GET("/mcp-market/services", adminController.GetMCPMarketServices)
				admin.GET("/mcp-market/services/:market_id/*service_id", adminController.GetMCPMarketServiceDetail)
				admin.POST("/mcp-market/import", adminController.ImportMCPMarketService)
				admin.GET("/mcp-market/imported-services", adminController.GetMCPMarketImportedServices)
				admin.POST("/mcp-market/imported-services", adminController.CreateMCPMarketImportedService)
				admin.GET("/mcp-market/imported-services/:id/tools", adminController.GetMCPMarketImportedServiceTools)
				admin.PUT("/mcp-market/imported-services/:id", adminController.UpdateMCPMarketImportedService)
				admin.DELETE("/mcp-market/imported-services/:id", adminController.DeleteMCPMarketImportedService)

				// Memory配置管理
				admin.GET("/memory-configs", adminController.GetMemoryConfigs)
				admin.POST("/memory-configs", adminController.CreateMemoryConfig)
				admin.PUT("/memory-configs/:id", adminController.UpdateMemoryConfig)
				admin.DELETE("/memory-configs/:id", adminController.DeleteMemoryConfig)
				admin.POST("/memory-configs/:id/set-default", adminController.SetDefaultMemoryConfig)

				// 知识库检索配置管理（provider API 调用）
				admin.GET("/knowledge-search-configs", adminController.GetKnowledgeSearchConfigs)
				admin.POST("/knowledge-search-configs", adminController.CreateKnowledgeSearchConfig)
				admin.PUT("/knowledge-search-configs/:id", adminController.UpdateKnowledgeSearchConfig)
				admin.DELETE("/knowledge-search-configs/:id", adminController.DeleteKnowledgeSearchConfig)
				admin.POST("/knowledge-search-configs/weknora/models", adminController.ListWeknoraModels)

				// 全局角色管理（保留兼容旧API）
				admin.GET("/global-roles", adminController.GetGlobalRoles)
				admin.POST("/global-roles", adminController.CreateGlobalRole)
				admin.PUT("/global-roles/:id", adminController.UpdateGlobalRole)
				admin.DELETE("/global-roles/:id", adminController.DeleteGlobalRole)

				// 全局角色管理（新API）
				admin.GET("/roles", adminController.GetRolesNew)
				admin.GET("/roles/global", adminController.GetGlobalRolesNew)
				admin.POST("/roles/global", adminController.CreateRoleNew)
				admin.PUT("/roles/global/:id", adminController.UpdateRoleNew)
				admin.DELETE("/roles/global/:id", adminController.DeleteRoleNew)
				admin.PATCH("/roles/global/:id/toggle", adminController.ToggleRoleStatus)
				admin.PATCH("/roles/global/:id/default", adminController.SetDefaultRole)

				// 设备管理
				admin.GET("/devices", adminController.GetDevices)
				admin.GET("/devices/validate-code", adminController.ValidateDeviceCode)
				admin.POST("/devices", adminController.CreateDevice)
				admin.PUT("/devices/:id", adminController.UpdateDevice)
				admin.DELETE("/devices/:id", adminController.DeleteDevice)

				// 智能体管理
				admin.GET("/agents", adminController.GetAgents)
				admin.POST("/agents", adminController.CreateAgent)
				admin.PUT("/agents/:id", adminController.UpdateAgent)
				admin.DELETE("/agents/:id", adminController.DeleteAgent)
				admin.GET("/agents/:id/mcp-endpoint", adminController.GetAgentMCPEndpoint)
				admin.GET("/agents/:id/openclaw-endpoint", adminController.GetAgentOpenClawEndpoint)
				admin.POST("/agents/:id/openclaw-chat-test", adminController.CallAgentOpenClawChatTest)
				admin.GET("/agents/:id/mcp-tools", adminController.GetAgentMcpTools)
				admin.POST("/agents/:id/mcp-call", adminController.CallAgentMcpTool)
				admin.GET("/devices/:id/mcp-tools", adminController.GetDeviceMcpTools)
				admin.POST("/devices/:id/mcp-call", adminController.CallDeviceMcpTool)
				admin.GET("/scheduler-tasks", adminController.GetSchedulerTasks)
				admin.POST("/scheduler-tasks", adminController.CreateSchedulerTask)
				admin.PUT("/scheduler-tasks/:id", adminController.UpdateSchedulerTask)
				admin.DELETE("/scheduler-tasks/:id", adminController.DeleteSchedulerTask)

				// 用户管理
				admin.GET("/users", adminController.GetUsers)
				admin.POST("/users", adminController.CreateUser)
				admin.PUT("/users/:id", adminController.UpdateUser)
				admin.DELETE("/users/:id", adminController.DeleteUser)
				admin.POST("/users/:id/reset-password", adminController.ResetUserPassword)

				admin.GET("/users/:id/knowledge-bases", adminController.GetUserKnowledgeBasesAdmin)
				admin.POST("/users/:id/knowledge-bases", adminController.CreateUserKnowledgeBaseAdmin)
				admin.PUT("/users/:id/knowledge-bases/:kb_id", adminController.UpdateUserKnowledgeBaseAdmin)
				admin.DELETE("/users/:id/knowledge-bases/:kb_id", adminController.DeleteUserKnowledgeBaseAdmin)

				admin.GET("/users/:id/voice-clone-quotas", adminController.GetUserVoiceCloneQuotas)
				admin.PUT("/users/:id/voice-clone-quotas", adminController.UpdateUserVoiceCloneQuotas)

				// 配置导入导出
				admin.GET("/configs/export", adminController.ExportConfigs)
				admin.POST("/configs/import", adminController.ImportConfigs)
				// 一键测试配置（OTA 在 manager 内，VAD/ASR/LLM/TTS 经 WebSocket 发主程序）
				admin.POST("/configs/test", adminController.TestConfigs)

				// 资源池统计
				admin.GET("/pool/stats", poolStatsController.GetPoolStats)
				admin.GET("/pool/stats/summary", poolStatsController.GetPoolStatsSummary)
			}
		}
	}

	// WebSocket路由
	r.GET("/ws", webSocketController.HandleWebSocket)

	// 发版时嵌入的前端静态资源（-tags embed_ui）：NoRoute 时先尝试静态文件，再 SPA 回退
	if sub, err := fs.Sub(static.FS, "dist"); err == nil {
		r.NoRoute(serveEmbedStatic(sub))
	}

	return r
}

// serveEmbedStatic 未匹配路由时：先尝试从 fsys 返回对应静态文件，否则 GET 返回 index.html（SPA 回退）
func serveEmbedStatic(fsys fs.FS) gin.HandlerFunc {
	indexHTML, _ := fs.ReadFile(fsys, "index.html")
	fileServer := http.FileServer(http.FS(fsys))
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}
		path := c.Request.URL.Path
		if path == "" || path[0] != '/' {
			path = "/" + path
		}
		if path == "/" {
			path = "/index.html"
		}
		name := path[1:]
		if _, err := fs.Stat(fsys, name); err == nil {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		if len(indexHTML) > 0 {
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
			return
		}
		c.Status(http.StatusNotFound)
	}
}
