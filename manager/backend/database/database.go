package database

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"xiaozhi/manager/backend/config"
	"xiaozhi/manager/backend/models"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func Init(cfg config.DatabaseConfig) *gorm.DB {
	var db *gorm.DB
	var err error

	storageType := cfg.GetStorageType()

	if storageType == "sqlite" {
		if cfg.SQLite == nil {
			log.Println("SQLite配置为空，将使用fallback模式运行（硬编码用户验证）")
			return nil
		}
		// 确保数据库文件所在目录存在，避免 SQLite 报 unable to open database file
		dir := filepath.Dir(cfg.SQLite.FilePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Printf("创建数据库目录失败 %s: %v", dir, err)
			return nil
		}
		log.Println("使用SQLite数据库:", cfg.SQLite.FilePath)
		db, err = gorm.Open(sqlite.Open(cfg.SQLite.FilePath), &gorm.Config{})
	} else {
		if cfg.MySQL == nil {
			log.Println("MySQL配置为空，将使用fallback模式运行（硬编码用户验证）")
			return nil
		}
		// MySQL 数据库连接
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			cfg.MySQL.Username, cfg.MySQL.Password, cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.Database)
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	}

	if err != nil {
		log.Println("数据库连接失败:", err)
		log.Println("将使用fallback模式运行（硬编码用户验证）")
		return nil
	}

	log.Println("数据库连接成功")

	// 自动迁移数据库表结构
	log.Println("开始自动迁移数据库表结构...")
	err = db.AutoMigrate(
		&models.User{},
		&models.APIToken{},
		&models.Device{},
		&models.Agent{},
		&models.KnowledgeBase{},
		&models.KnowledgeBaseDocument{},
		&models.AgentKnowledgeBase{},
		&models.Config{},
		&models.MCPMarketService{},
		&models.GlobalRole{},
		&models.Role{}, // 新增：统一角色表
		&models.ChatMessage{},
		&models.SpeakerGroup{},
		&models.SpeakerSample{},
		&models.VoiceClone{},
		&models.VoiceCloneAudio{},
		&models.VoiceCloneTask{},
		&models.UserVoiceCloneQuota{},
		&models.SchedulerTask{},
	)
	if err != nil {
		log.Printf("数据库表结构迁移失败: %v", err)
		log.Println("将使用fallback模式运行（硬编码用户验证）")
		return nil
	}
	log.Println("数据库表结构迁移成功")

	// 迁移现有全局角色数据到新的 roles 表
	log.Println("检查是否需要迁移全局角色数据...")
	if err := migrateGlobalRolesToRoles(db); err != nil {
		log.Printf("迁移全局角色数据失败: %v", err)
		// 迁移失败不影响启动，只是数据没有迁移
	}

	return db
}

func Close(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		log.Println("获取数据库连接失败:", err)
		return
	}
	sqlDB.Close()
}

// migrateGlobalRolesToRoles 将现有全局角色数据迁移到新的 roles 表
func migrateGlobalRolesToRoles(db *gorm.DB) error {
	// 检查 roles 表是否已有数据
	var count int64
	if err := db.Table("roles").Count(&count).Error; err != nil {
		return fmt.Errorf("检查 roles 表失败: %w", err)
	}

	// 如果 roles 表已有数据，跳过迁移
	if count > 0 {
		log.Println("roles 表已有数据，跳过迁移")
		return nil
	}

	// 检查 global_roles 表是否有数据
	var globalRoleCount int64
	if err := db.Table("global_roles").Count(&globalRoleCount).Error; err != nil {
		// global_roles 表可能不存在，不是错误
		log.Println("global_roles 表不存在，跳过迁移")
		return nil
	}

	if globalRoleCount == 0 {
		log.Println("global_roles 表无数据，跳过迁移")
		return nil
	}

	log.Printf("开始迁移 %d 条全局角色数据到 roles 表...", globalRoleCount)

	// 查询所有全局角色
	var globalRoles []models.GlobalRole
	if err := db.Table("global_roles").Find(&globalRoles).Error; err != nil {
		return fmt.Errorf("查询 global_roles 失败: %w", err)
	}

	// 转换并插入到 roles 表
	for _, gr := range globalRoles {
		role := models.Role{
			UserID:      nil, // 全局角色 user_id 为 NULL
			Name:        gr.Name,
			Description: gr.Description,
			Prompt:      gr.Prompt,
			RoleType:    "global",
			Status:      "active",
			SortOrder:   0,
			IsDefault:   gr.IsDefault,
			CreatedAt:   gr.CreatedAt,
			UpdatedAt:   gr.UpdatedAt,
		}
		if err := db.Create(&role).Error; err != nil {
			log.Printf("插入角色 %s 失败: %v", gr.Name, err)
			continue
		}
		log.Printf("已迁移全局角色: %s", gr.Name)
	}

	log.Println("全局角色数据迁移完成")
	return nil
}
