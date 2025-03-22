package initapp

import (
	"log"
)

func Init(configPath string) error {

	log.Printf("[Init] 开始初始化应用程序...")

	// 迁移配置文件
	if err := MigrateConfig(configPath); err != nil {
		log.Printf("[Init] 配置迁移失败: %v", err)
		return err
	}

	log.Printf("[Init] 应用程序初始化完成")
	return nil
}
