//go:build asr_server

package main

import (
	"context"
	"net/http"
	"time"

	"voice_server/server"
	"xiaozhi-esp32-server-golang/internal/domain/asr"
	log "xiaozhi-esp32-server-golang/logger"
)

var (
	asrHTTPServer *http.Server // 本进程内嵌的 asr_server HTTP 服务句柄，用于优雅关闭
)

// InitAsrServerEmbed 预初始化本进程内嵌 ASR 共享依赖，避免首次 embed 识别时再懒加载。
func InitAsrServerEmbed(configPath string) {
	if err := asr.InitAsrServerEmbed(configPath); err != nil {
		log.Warnf("内嵌 ASR 预初始化失败: %v", err)
		return
	}
	log.Info("内嵌 ASR 预初始化完成")
}

// StartAsrServerHTTP 在本进程内启动 asr_server 的 HTTP 服务（独立端口）。是否调用由 main 根据 -asr-enable 决定。
// configPath：asr_server 配置文件路径，空则使用默认路径 asr_server.json
func StartAsrServerHTTP(configPath string) {
	if configPath == "" {
		configPath = asr.DefaultEmbedAsrConfigPath
	}
	log.Infof("正在启动内嵌 asr_server HTTP 服务，配置文件: %s", configPath)

	handler, addr, readTimeout, err := server.Setup(configPath)
	if err != nil {
		log.Warnf("asr_server 初始化失败，跳过启动: %v", err)
		return
	}

	asrHTTPServer = &http.Server{
		Addr:        addr,
		Handler:     handler,
		ReadTimeout: readTimeout,
	}

	go func() {
		log.Infof("asr_server HTTP 服务启动在 %s", addr)
		if err := asrHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("asr_server HTTP 服务异常退出: %v", err)
		}
	}()
}

// StopAsrServerHTTP 优雅关闭本进程内嵌的 asr_server HTTP 服务
func StopAsrServerHTTP() {
	if asrHTTPServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := asrHTTPServer.Shutdown(ctx); err != nil {
			log.Warnf("asr_server HTTP 关闭超时或异常: %v", err)
		}
		asrHTTPServer = nil
		log.Info("asr_server HTTP 服务已关闭")
	}
}

// ShutdownAsrServer 关闭本进程内 asr_server（含 embed/http 共享引擎资源）。
func ShutdownAsrServer() {
	StopAsrServerHTTP()
	server.SharedEngineProvider().Close()
	log.Info("asr_server 共享引擎资源已释放")
}
