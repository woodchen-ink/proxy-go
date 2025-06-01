package initapp

import (
	"log"
)

func Init(configPath string) error {

	log.Printf("[Init] 开始初始化应用程序...")

	// 迁移配置文件已移除，不再需要
	log.Printf("[Init] 应用程序初始化完成")
	return nil
}
