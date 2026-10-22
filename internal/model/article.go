// Package model 定义与数据库表一一对应的模型。
package model

import "time"

// 文章状态，取值与 DATABASE.md 5.1 / PRD 3.2 一致。
const (
	ArticleDraft     uint8 = 0 // 草稿，仅作者可见
	ArticlePending   uint8 = 1 // 待审核，仅作者与管理员可见
	ArticlePublished uint8 = 2 // 已发布，公开可见
	ArticleRejected  uint8 = 3 // 审核未通过，仅作者与管理员可见
	ArticleOffline   uint8 = 4 // 被下架，仅作者与管理员可见
	ArticleArchived  uint8 = 5 // 已归档，仅作者可见
	ArticleDeleted   uint8 = 6 // 软删除，公开 URL 返回 410
)

// 搜索索引同步状态（TECH_DESIGN 7.4）。
const (
	SearchSyncPending uint8 = 0 // 待同步
	SearchSyncDone    uint8 = 1 // 已同步
	SearchSyncFailed  uint8 = 2 // 同步失败，等待对账
)

// Article 对应 articles 表。
//
// content_md 与 content_html 分列存储：列表页永远不要 SELECT 正文，
// 否则一页 20 篇 × 数百 KB 会把内存拖垮。
type Article struct {
	ID               uint64     `gorm:"column:id;primaryKey"`
	AuthorID         uint64     `gorm:"column:author_id"`
	CategoryID       uint64     `gorm:"column:category_id"`
	Title            string     `gorm:"column:title"`
	Slug             string     `gorm:"column:slug"`
	Summary          string     `gorm:"column:summary"`
	ContentMD        string     `gorm:"column:content_md"`
	ContentHTML      *string    `gorm:"column:content_html"`
	CoverURL         string     `gorm:"column:cover_url"`
	Status           uint8      `gorm:"column:status"`
	IsTop            uint8      `gorm:"column:is_top"`
	AllowComment     uint8      `gorm:"column:allow_comment"`
	ViewCount        uint64     `gorm:"column:view_count"`
	LikeCount        uint32     `gorm:"column:like_count"`
	FavoriteCount    uint32     `gorm:"column:favorite_count"`
	CommentCount     uint32     `gorm:"column:comment_count"`
	WordCount        uint32     `gorm:"column:word_count"`
	ReadingTime      uint32     `gorm:"column:reading_time"`
	Version          uint32     `gorm:"column:version"`
	SearchSyncStatus uint8      `gorm:"column:search_sync_status"`
	SearchSyncedAt   *time.Time `gorm:"column:search_synced_at"`
	RejectReason     string     `gorm:"column:reject_reason"`
	OfflineReason    string     `gorm:"column:offline_reason"`
	PublishedAt      *time.Time `gorm:"column:published_at"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	DeletedAt        *time.Time `gorm:"column:deleted_at"`
}

// TableName 指定表名。
func (Article) TableName() string { return "articles" }

// IsDeleted 判断是否已软删除。
func (a *Article) IsDeleted() bool { return a.DeletedAt != nil }

// IsPublic 判断是否对外可见。
func (a *Article) IsPublic() bool { return a.Status == ArticlePublished && !a.IsDeleted() }

// Category 对应 categories 表。article_count 是冗余计数，任何变更都要同步维护。
type Category struct {
	ID           uint64    `gorm:"column:id;primaryKey"`
	Name         string    `gorm:"column:name"`
	Slug         string    `gorm:"column:slug"`
	Description  string    `gorm:"column:description"`
	Sort         int       `gorm:"column:sort"`
	ArticleCount uint32    `gorm:"column:article_count"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

// TableName 指定表名。
func (Category) TableName() string { return "categories" }

// Tag 对应 tags 表。
type Tag struct {
	ID           uint64    `gorm:"column:id;primaryKey"`
	Name         string    `gorm:"column:name"`
	Slug         string    `gorm:"column:slug"`
	ArticleCount uint32    `gorm:"column:article_count"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

// TableName 指定表名。
func (Tag) TableName() string { return "tags" }

// ArticleTag 对应 article_tags 关联表，联合主键、无自增 ID。
type ArticleTag struct {
	ArticleID uint64 `gorm:"column:article_id;primaryKey"`
	TagID     uint64 `gorm:"column:tag_id;primaryKey"`
}

// TableName 指定表名。
func (ArticleTag) TableName() string { return "article_tags" }
