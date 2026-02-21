package main

import (
	"encoding/json"
	"flag"
	"fmt"
	stdlog "log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"xiaozhi-esp32-server-golang/internal/app/server"
	user_config "xiaozhi-esp32-server-golang/internal/domain/config"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func main() {
	bootstrapEarlyLogging()

	// 解析命令行参数
	configFile := flag.String("c", defaultConfigFilePath, "配置文件路径")
	managerEnable := flag.Bool("manager-enable", defaultManagerEnable, "是否启用内嵌 manager")
	managerConfig := flag.String("manager-config", "", "manager 配置文件路径，启用时可选，默认 manager/backend/config/config.json")
	asrEnable := flag.Bool("asr-enable", defaultAsrEnable, "是否启用内嵌 asr_server")
	asrConfig := flag.String("asr-config", "", "asr_server 配置文件路径，启用时可选，默认 asr_server.json")
	flag.Parse()

	if *configFile == "" {
		fmt.Println("配置文件路径不能为空")
		return
	}

	// 先启动 manager，再 Init，否则 Init 里 updateConfigFromAPI 会一直连不上 manager 导致卡死
	if *managerEnable {
		StartManagerHTTP(*managerConfig)
	}

	err := Init(*configFile)
	if err != nil {
		return
	}

	if shouldInitAsrServerEmbed() {
		// 使用 Init 后的最终配置判断并预初始化内嵌 ASR，避免后续懒加载。
		// 触发条件：
		// 1) ASR provider 为 embed
		// 2) 声纹服务模式为 embed（即使 ASR provider 不是 embed，也需要共享引擎）
		InitAsrServerEmbed(*asrConfig)
	}
	if *asrEnable {
		StartAsrServerHTTP(*asrConfig)
	}

	// 根据配置启动 pprof 服务
	if viper.GetBool("server.pprof.enable") {
		pprofPort := viper.GetInt("server.pprof.port")
		go func() {
			log.Infof("启动 pprof 服务，端口: %d", pprofPort)
			if err := http.ListenAndServe(fmt.Sprintf(":%d", pprofPort), nil); err != nil {
				log.Errorf("pprof 服务启动失败: %v", err)
			}
		}()
		log.Infof("pprof 地址: http://localhost:%d/debug/pprof/", pprofPort)
	} else {
		log.Info("pprof 服务已禁用")
	}

	// 创建服务器
	appInstance := server.NewApp()

	var lock sync.RWMutex
	// 注册 system_config 热更：用 viper 当前配置与推送配置对比，仅当内容变更时合并并触发热更
	user_config.RegisterManagerSystemConfigHandler(func(data map[string]interface{}) {
		lock.Lock()
		defer lock.Unlock()
		current := viper.AllSettings()
		oldMqttServer := current["mqtt_server"]
		oldMqtt := current["mqtt"]
		oldUdp := current["udp"]
		oldMcp := current["mcp"]
		oldLocalMcp := current["local_mcp"]

		var doMqttServer, doMqttReload, doUdpReload, doMcpReload bool
		if data["mqtt_server"] != nil {
			if !SystemConfigEqual(data["mqtt_server"], oldMqttServer) {
				doMqttServer = true
			}
		}
		if data["mqtt"] != nil {
			if !SystemConfigEqual(data["mqtt"], oldMqtt) {
				doMqttReload = true
			}
		}
		if data["udp"] != nil {
			if udpListenChanged(data["udp"], oldUdp) {
				doUdpReload = true
			}
		}
		if data["mcp"] != nil {
			if !SystemConfigEqual(data["mcp"], oldMcp) {
				doMcpReload = true
			}
		}
		if data["local_mcp"] != nil {
			if !SystemConfigEqual(data["local_mcp"], oldLocalMcp) {
				doMcpReload = true
			}
		}

		ApplySystemConfigToViper(data)

		var wg sync.WaitGroup
		if doMqttServer {
			go func() {
				wg.Add(1)
				defer wg.Done()
				appInstance.ReloadMqttServer()
			}()
		}
		if doMqttReload || doUdpReload {
			go func() {
				wg.Add(1)
				defer wg.Done()
				appInstance.ReloadMqttUdpWithFlags(doMqttReload, doUdpReload)
			}()
		}
		if doMcpReload {
			go func() {
				wg.Add(1)
				defer wg.Done()
				if err := appInstance.ReloadMCP(); err != nil {
					log.Errorf("ReloadMCP failed: %v", err)
				}
			}()
		}
		wg.Wait()
	})
	appInstance.Run()

	// 阻塞监听退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	log.Info("服务器已启动，按 Ctrl+C 退出")
	<-quit

	log.Info("正在关闭服务器...")

	// 停止周期性配置更新服务
	StopPeriodicConfigUpdate()
	if *managerEnable {
		StopManagerHTTP()
	}
	ShutdownAsrServer()

	log.Info("服务器已关闭")
}

// bootstrapEarlyLogging 配置启动早期日志输出，避免日志系统初始化前写入 stderr 导致终端显示为红字。
func bootstrapEarlyLogging() {
	log.UseStdout()
	logrus.SetOutput(os.Stdout)
	stdlog.SetOutput(os.Stdout)
}

func udpListenChanged(newUdpCfg interface{}, oldUdpCfg interface{}) bool {
	newListenHost, newListenPort := udpListenHostPort(newUdpCfg)
	oldListenHost, oldListenPort := udpListenHostPort(oldUdpCfg)
	if newListenHost == "" && newListenPort == 0 {
		return false
	}
	return newListenHost != oldListenHost || newListenPort != oldListenPort
}

func udpListenHostPort(cfg interface{}) (string, int) {
	if cfg == nil {
		return "", 0
	}
	type udpListen struct {
		ListenHost string `json:"listen_host"`
		ListenPort int    `json:"listen_port"`
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return "", 0
	}
	var parsed udpListen
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", 0
	}
	return parsed.ListenHost, parsed.ListenPort
}

// shouldInitAsrServerEmbed 判断是否需要预初始化内嵌 asr_server 共享引擎。
func shouldInitAsrServerEmbed() bool {
	asrSelected := strings.TrimSpace(viper.GetString("asr.provider"))
	if strings.EqualFold(asrSelected, "embed") {
		return true
	}

	// manager 下发场景中 asr.provider 可能是默认 config_id（例如 "asr_embed_1"），
	// 需要反查 asr.<config_id>.provider 是否为 embed。
	if asrSelected != "" {
		asrMap := viper.GetStringMap("asr")
		if selectedCfg, ok := asrMap[asrSelected].(map[string]interface{}); ok {
			if p, ok := selectedCfg["provider"].(string); ok && strings.EqualFold(strings.TrimSpace(p), "embed") {
				return true
			}
		}
	}

	serviceMode := strings.ToLower(strings.TrimSpace(viper.GetString("voice_identify.mode")))

	return serviceMode == "embed"
}
