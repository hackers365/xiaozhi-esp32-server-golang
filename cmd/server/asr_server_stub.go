//go:build !asr_server

package main

import log "xiaozhi-esp32-server-golang/logger"

// InitAsrServerEmbed 未启用 asr_server 编译时的空实现。
func InitAsrServerEmbed(configPath string) {
	_ = configPath
	log.Warn("asr_server 内嵌未编译进本二进制，请使用 -tags asr_server 重新编译以启用")
}

// StartAsrServerHTTP 未启用 asr_server 编译时的空实现。需使用 -tags asr_server 编译以启用内嵌 asr_server。
func StartAsrServerHTTP(configPath string) {
	log.Warn("asr_server 内嵌未编译进本二进制，请使用 -tags asr_server 重新编译以启用")
}

// StopAsrServerHTTP 未启用 asr_server 编译时的空实现。
func StopAsrServerHTTP() {}

// ShutdownAsrServer 未启用 asr_server 编译时的空实现。
func ShutdownAsrServer() {}
