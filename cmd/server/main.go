// 初稳性核算服务入口：加载装载状态存储、预置示范算例、启动 HTTP 服务。
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"stability-service/internal/api"
	"stability-service/internal/loadcase"
)

func main() {
	addr := envOr("STABILITY_ADDR", ":8080")
	dataDir := envOr("STABILITY_DATA_DIR", "./data")

	store, err := loadcase.Open(filepath.Join(dataDir, "loadcases.json"))
	if err != nil {
		log.Fatalf("打开装载状态存储失败: %v", err)
	}
	if err := loadcase.SeedDemoBarge(store); err != nil {
		log.Printf("预置示范驳船算例失败（不影响既有数据）: %v", err)
	}

	gin.SetMode(gin.ReleaseMode)
	r := api.NewRouter(store)
	log.Printf("初稳性核算服务已启动：监听 %s，数据目录 %s", addr, dataDir)
	if err := r.Run(addr); err != nil {
		log.Fatalf("服务退出: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
