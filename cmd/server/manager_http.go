//go:build manager

package main

import (
	"context"
	"net/http"
	"time"

	log "xiaozhi-esp32-server-golang/logger"
	mbconfig "xiaozhi/manager/backend/config"
	"xiaozhi/manager/backend/database"
	"xiaozhi/manager/backend/middleware"
	"xiaozhi/manager/backend/router"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	defaultManagerHTTPPort   = "9000"
	defaultManagerConfigPath = "manager.json"
)

var (
	managerHTTPServer *http.Server // 本进程内嵌的 manager HTTP 服务句柄，用于优雅关闭
	managerDB         *gorm.DB     // manager 使用的 DB，退出时关闭
)

// StartManagerHTTP 在本进程内启动 manager 的 HTTP 服务（双端口）。是否调用由 main 根据 -manager-enable 决定。
// configPath：manager 配置文件路径，空则使用默认路径
func StartManagerHTTP(configPath string) {
	if configPath == "" {
		configPath = defaultManagerConfigPath
	}
	log.Infof("正在启动内嵌 manager HTTP 服务，配置文件: %s", configPath)

	cfg := mbconfig.LoadWithPath(configPath)
	if err := middleware.ConfigureJWT(cfg.JWT.Secret, cfg.JWT.ExpireHour); err != nil {
		log.Fatalf("manager JWT 配置无效: %v", err)
	}
	port := cfg.Server.Port
	if port == "" {
		port = defaultManagerHTTPPort
	}
	cfg.Server.Port = port

	db := database.Init(cfg.Database)
	if db == nil {
		log.Warn("manager 数据库初始化失败，跳过启动 manager HTTP")
		return
	}
	managerDB = db

	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := router.Setup(db, cfg)

	managerHTTPServer = &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		log.Infof("manager HTTP 服务启动在端口: %s", port)
		if err := managerHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("manager HTTP 服务异常退出: %v", err)
		}
	}()
}

// StopManagerHTTP 优雅关闭本进程内嵌的 manager HTTP 服务并关闭数据库连接
func StopManagerHTTP() {
	if managerHTTPServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := managerHTTPServer.Shutdown(ctx); err != nil {
			log.Warnf("manager HTTP 关闭超时或异常: %v", err)
		}
		managerHTTPServer = nil
		log.Info("manager HTTP 服务已关闭")
	}
	if managerDB != nil {
		database.Close(managerDB)
		managerDB = nil
	}
}
