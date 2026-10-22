package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"go-blog/internal/model"
)

// MediaRepo 是 media 表的数据访问对象。
type MediaRepo struct {
	db *gorm.DB
}

// NewMediaRepo 构造 MediaRepo。
func NewMediaRepo(db *gorm.DB) *MediaRepo { return &MediaRepo{db: db} }

// Create 写入媒体记录。
func (r *MediaRepo) Create(ctx context.Context, m *model.Media) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// ByID 按主键读取。
func (r *MediaRepo) ByID(ctx context.Context, id uint64) (*model.Media, error) {
	var m model.Media
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&m).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// UserRepo.UpdateAvatar 更新头像地址（存 URL，不存磁盘路径）。
func (r *UserRepo) UpdateAvatar(ctx context.Context, id uint64, url string) error {
	res := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Updates(map[string]any{"avatar_url": url})
	return affected(res)
}

// FindBySHA256 按内容摘要与用途查已有媒体（秒传）。
// 必须限定 purpose：同一张图既当过头像又当聊天图时，
// 复用跨用途记录会让聊天图落到公开桶，破坏私有语义。
// 不存在返回 ErrNotFound。
func (r *MediaRepo) FindBySHA256(ctx context.Context, sum, purpose string) (*model.Media, error) {
	var m model.Media
	err := r.db.WithContext(ctx).Where("sha256 = ? AND purpose = ?", sum, purpose).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
