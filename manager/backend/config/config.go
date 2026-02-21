package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

type Config struct {
	Server         ServerConfig         `json:"server"`
	Database       DatabaseConfig       `json:"database"`
	JWT            JWTConfig            `json:"jwt"`
	SpeakerService SpeakerServiceConfig `json:"speaker_service"`
	Storage        StorageConfig        `json:"storage"`
	History        HistoryConfig        `json:"history"`
}

type ServerConfig struct {
	Port string `json:"port"`
	Mode string `json:"mode"`
}

type DatabaseConfig struct {
	Type   string        `json:"type"` // "mysql" 或 "sqlite"，决定使用哪种数据库
	MySQL  *MySQLConfig  `json:"mysql,omitempty"`
	SQLite *SQLiteConfig `json:"sqlite,omitempty"`
}

// GetStorageType 获取当前配置的存储类型
func (c *DatabaseConfig) GetStorageType() string {
	if c.Type == "sqlite" || c.Type == "mysql" {
		return c.Type
	}
	// 未设置 type 时，根据已有配置推断
	if c.SQLite != nil {
		return "sqlite"
	}
	if c.MySQL != nil {
		return "mysql"
	}
	return "mysql"
}

// MySQLConfig MySQL 数据库配置
type MySQLConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
}

// SQLiteConfig SQLite 数据库配置
type SQLiteConfig struct {
	FilePath string `json:"file_path"` // 数据库文件路径，如 ./data/xiaozhi.db
}

type JWTConfig struct {
	Secret     string `json:"secret"`
	ExpireHour int    `json:"expire_hour"`
}

type SpeakerServiceConfig struct {
	URL  string `json:"url"`  // HTTP 模式下 asr_server 的服务地址
	Mode string `json:"mode"` // 服务模式: http / embed
}

type StorageConfig struct {
	SpeakerAudioPath string `json:"speaker_audio_path"` // 音频文件存储路径
	MaxFileSize      int64  `json:"max_file_size"`      // 最大文件大小（字节），默认10MB
}

type HistoryConfig struct {
	Enabled       bool   `json:"enabled"`
	AudioBasePath string `json:"audio_base_path"` // 音频存储基础路径
	MaxFileSize   int64  `json:"max_file_size"`   // 最大文件大小(字节)，默认10MB
}

func Load() *Config {
	return LoadWithPath("config/config.json")
}

func LoadWithPath(configPath string) *Config {
	config := LoadFromFile(configPath)

	// 仅当使用 MySQL 时，确保有 MySQL 配置并应用环境变量覆盖
	if config.Database.GetStorageType() == "mysql" {
		if config.Database.MySQL == nil {
			config.Database.MySQL = &MySQLConfig{}
		}
		if host := os.Getenv("DB_HOST"); host != "" {
			config.Database.MySQL.Host = host
		}
		if port := os.Getenv("DB_PORT"); port != "" {
			var p int
			fmt.Sscanf(port, "%d", &p)
			config.Database.MySQL.Port = p
		}
		if username := os.Getenv("DB_USER"); username != "" {
			config.Database.MySQL.Username = username
		}
		if password := os.Getenv("DB_PASSWORD"); password != "" {
			config.Database.MySQL.Password = password
		}
		if database := os.Getenv("DB_NAME"); database != "" {
			config.Database.MySQL.Database = database
		}
	}

	// 优先使用环境变量覆盖声纹服务配置
	if serviceURL := os.Getenv("SPEAKER_SERVICE_URL"); serviceURL != "" {
		config.SpeakerService.URL = serviceURL
	}
	if serviceMode := os.Getenv("SPEAKER_SERVICE_MODE"); serviceMode != "" {
		config.SpeakerService.Mode = serviceMode
	}
	// 优先使用环境变量覆盖音频存储路径
	if audioBasePath := os.Getenv("AUDIO_BASE_PATH"); audioBasePath != "" {
		config.History.AudioBasePath = audioBasePath
	}

	fmt.Println("config", config)

	return config
}

func LoadFromFile(configPath string) *Config {
	file, err := os.Open(configPath)
	if err != nil {
		log.Fatalf("无法打开配置文件 %s: %v", configPath, err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		log.Fatalf("解析配置文件失败 %s: %v", configPath, err)
	}

	return &config
}
