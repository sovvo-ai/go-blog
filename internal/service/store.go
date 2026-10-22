package service

import (
	"context"
	"time"

	"go-blog/internal/model"
)

// UserStore 是认证与用户服务所需的最小数据能力。
//
// 定义为接口而不是直接依赖 repository.UserRepo：
//   - 单测与集成测试可以用内存实现，不必起 MySQL
//   - 服务层不感知 SQL 细节，换存储实现不影响业务代码
//
// 生产实现：internal/repository.UserRepo（由 wire.Bind 绑定）。
type UserStore interface {
	Create(ctx context.Context, u *model.User) error
	ByID(ctx context.Context, id uint64) (*model.User, error)
	ByAccount(ctx context.Context, account string) (*model.User, error)
	ByEmail(ctx context.Context, email string) (*model.User, error)
	ExistsUsername(ctx context.Context, username string) (bool, error)
	ExistsEmail(ctx context.Context, email string) (bool, error)
	MarkEmailVerified(ctx context.Context, id uint64, at time.Time) error
	TouchLogin(ctx context.Context, id uint64, at time.Time, ip string) error
	UpdatePassword(ctx context.Context, id uint64, hash string) error
	UpdateProfile(ctx context.Context, id uint64, nickname, bio string) error
	PublishedArticleCount(ctx context.Context, id uint64) (int64, error)
}
