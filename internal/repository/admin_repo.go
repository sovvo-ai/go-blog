package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go-blog/internal/authz"
	"go-blog/internal/model"
)

// AdminRepo 负责管理端的数据访问。
type AdminRepo struct {
	db *gorm.DB
}

// NewAdminRepo 构造 AdminRepo。
func NewAdminRepo(db *gorm.DB) *AdminRepo { return &AdminRepo{db: db} }

// Overview 统计。
type Overview struct {
	ArticleCount        int64
	UserCount           int64
	PendingArticleCount int64
	PendingCommentCount int64
	TodayNewArticles    int64
	TodayNewUsers       int64
}

// GetOverview 统计概览。
func (r *AdminRepo) GetOverview(ctx context.Context) (*Overview, error) {
	o := &Overview{}
	if err := r.db.WithContext(ctx).Model(&model.Article{}).
		Where("deleted_at IS NULL").Count(&o.ArticleCount).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&model.User{}).Count(&o.UserCount).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&model.Article{}).
		Where("status = ? AND deleted_at IS NULL", model.ArticlePending).
		Count(&o.PendingArticleCount).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&model.Comment{}).
		Where("status = ? AND deleted_at IS NULL", model.CommentPending).
		Count(&o.PendingCommentCount).Error; err != nil {
		return nil, err
	}
	today := time.Now().Truncate(24 * time.Hour)
	if err := r.db.WithContext(ctx).Model(&model.Article{}).
		Where("created_at >= ? AND deleted_at IS NULL", today).
		Count(&o.TodayNewArticles).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("created_at >= ?", today).
		Count(&o.TodayNewUsers).Error; err != nil {
		return nil, err
	}
	return o, nil
}

// UserListFilter 是管理端用户列表过滤条件。
type UserListFilter struct {
	Query    string
	Role     *uint8
	Status   *uint8
	CursorID uint64
	Limit    int
}

// ListUsers 分页返回用户列表。
func (r *AdminRepo) ListUsers(ctx context.Context, f UserListFilter) ([]model.User, error) {
	var rows []model.User
	q := r.db.WithContext(ctx).Model(&model.User{})
	if f.Query != "" {
		like := "%" + f.Query + "%"
		q = q.Where("username LIKE ? OR email LIKE ? OR nickname LIKE ?", like, like, like)
	}
	if f.Role != nil {
		q = q.Where("role = ?", *f.Role)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.CursorID > 0 {
		q = q.Where("id < ?", f.CursorID)
	}
	err := q.Order("id DESC").Limit(f.Limit + 1).Find(&rows).Error
	return rows, err
}

// UpdateUserStatus 修改用户状态。
func (r *AdminRepo) UpdateUserStatus(ctx context.Context, id uint64, status uint8) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("status", status).Error
}

// UpdateUserRole 修改用户角色。
func (r *AdminRepo) UpdateUserRole(ctx context.Context, id uint64, role authz.Role) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("role", uint8(role)).Error
}

// CountAdmins 统计管理员数量（用于"不能撤销最后一个 admin"校验）。
func (r *AdminRepo) CountAdmins(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("role = ?", authz.RoleAdmin).
		Count(&n).Error
	return n, err
}

// ListArticlesAdmin 分页返回全站文章（含草稿、待审、已删）。
func (r *AdminRepo) ListArticlesAdmin(ctx context.Context, status *uint8, authorID uint64, query string, cursorID uint64, limit int) ([]model.Article, error) {
	var rows []model.Article
	q := r.db.WithContext(ctx).Model(&model.Article{})
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	if authorID > 0 {
		q = q.Where("author_id = ?", authorID)
	}
	if query != "" {
		q = q.Where("title LIKE ?", "%"+query+"%")
	}
	if cursorID > 0 {
		q = q.Where("id < ?", cursorID)
	}
	err := q.Order("id DESC").Limit(limit + 1).Find(&rows).Error
	return rows, err
}

// CreateOperationLog 写入操作审计日志。
func (r *AdminRepo) CreateOperationLog(ctx context.Context, log *model.OperationLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// ArticleByID 管理端按 ID 取文章：任意状态，含已删除。
func (r *AdminRepo) ArticleByID(ctx context.Context, id uint64) (*model.Article, error) {
	var a model.Article
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&a).Error
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// MediaListFilter 是管理端媒体列表筛选条件。
type MediaListFilter struct {
	Purpose    string
	UploaderID uint64
	CursorID   uint64
	Limit      int
}

// ListMedia 分页返回媒体记录（按 ID 倒序）。
func (r *AdminRepo) ListMedia(ctx context.Context, f MediaListFilter) ([]model.Media, error) {
	var rows []model.Media
	q := r.db.WithContext(ctx).Model(&model.Media{})
	if f.Purpose != "" {
		q = q.Where("purpose = ?", f.Purpose)
	}
	if f.UploaderID > 0 {
		q = q.Where("uploader_id = ?", f.UploaderID)
	}
	if f.CursorID > 0 {
		q = q.Where("id < ?", f.CursorID)
	}
	err := q.Order("id DESC").Limit(f.Limit + 1).Find(&rows).Error
	return rows, err
}
