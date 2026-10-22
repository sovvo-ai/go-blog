package bootstrap

import (
	"context"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"go-blog/internal/config"
	"go-blog/internal/handler"
	"go-blog/internal/search"
)

// NewDependencyChecks 组装就绪探针要检查的依赖清单。
//
// 必需依赖：MySQL、Redis。任一不可用则 /readyz 返回 503。
// Meilisearch 在阶段 4 接入后追加，未接入时不注册检查项——
// 注册一个永远 skipped 的项会让运维误以为"已检查通过"。
func NewDependencyChecks(cfg *config.Config, db *gorm.DB, rdb *redis.Client, searcher search.Searcher) ([]handler.DependencyCheck, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	checks := []handler.DependencyCheck{
		{Name: "mysql", Required: true, Ping: sqlPinger{db: sqlDB}.Ping},
		{Name: "redis", Required: true, Ping: redisPinger{client: rdb}.Ping},
	}
	// 检索服务按"可降级"注册：挂了只影响搜索，不该让整站不可用。
	// 未配置时完全不注册——注册一个永远 skipped 的项会让人误以为已检查。
	if cfg.Search.Enabled() {
		checks = append(checks, handler.DependencyCheck{
			Name:     "meilisearch",
			Required: false,
			Ping:     func(ctx context.Context) error { return searcher.Healthy(ctx) },
		})
	}
	return checks, nil
}
