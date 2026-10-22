package model

import "time"

// 评论状态。
const (
	CommentPending  uint8 = 0
	CommentApproved uint8 = 1
	CommentRejected uint8 = 2
	CommentDeleted  uint8 = 3
)

// 举报状态。
const (
	ReportPending  uint8 = 0
	ReportAccepted uint8 = 1
	ReportRejected uint8 = 2
)

// 举报对象类型。
const (
	ReportTargetArticle    = "article"
	ReportTargetComment    = "comment"
	ReportTargetMessage    = "message"
	ReportTargetConversation = "conversation"
)

// ArticleLike 对应 article_likes 表，联合主键天然保证幂等。
type ArticleLike struct {
	UserID    uint64    `gorm:"column:user_id;primaryKey"`
	ArticleID uint64    `gorm:"column:article_id;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 指定表名。
func (ArticleLike) TableName() string { return "article_likes" }

// ArticleFavorite 对应 article_favorites 表。
type ArticleFavorite struct {
	UserID    uint64    `gorm:"column:user_id;primaryKey"`
	ArticleID uint64    `gorm:"column:article_id;primaryKey"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 指定表名。
func (ArticleFavorite) TableName() string { return "article_favorites" }

// Comment 对应 comments 表（两级）。
type Comment struct {
	ID            uint64     `gorm:"column:id;primaryKey"`
	ArticleID     uint64     `gorm:"column:article_id"`
	UserID        uint64     `gorm:"column:user_id"`
	ParentID      uint64     `gorm:"column:parent_id"`
	RootID        uint64     `gorm:"column:root_id"`
	ReplyToUserID uint64     `gorm:"column:reply_to_user_id"`
	Content       string     `gorm:"column:content"`
	Status        uint8      `gorm:"column:status"`
	IP            string     `gorm:"column:ip"`
	UserAgent     string     `gorm:"column:user_agent"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	DeletedAt     *time.Time `gorm:"column:deleted_at"`
}

// TableName 指定表名。
func (Comment) TableName() string { return "comments" }

// Report 对应 reports 表。
type Report struct {
	ID         uint64     `gorm:"column:id;primaryKey"`
	ReporterID uint64     `gorm:"column:reporter_id"`
	TargetType string     `gorm:"column:target_type"`
	TargetID   uint64     `gorm:"column:target_id"`
	Reason     string     `gorm:"column:reason"`
	Detail     string     `gorm:"column:detail"`
	Status     uint8      `gorm:"column:status"`
	HandlerID  uint64     `gorm:"column:handler_id"`
	HandledAt  *time.Time `gorm:"column:handled_at"`
	HandleNote string     `gorm:"column:handle_note"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
}

// TableName 指定表名。
func (Report) TableName() string { return "reports" }

// Notification 对应 notifications 表。
//
// ref_id 必须可空：唯一索引 (user_id, type, ref_id) 中，
// MySQL 允许多个 NULL 但不允许多个空字符串，系统通知（无 ref）必须用 NULL。
type Notification struct {
	ID        uint64    `gorm:"column:id;primaryKey"`
	UserID    uint64    `gorm:"column:user_id"`
	Type      string    `gorm:"column:type"`
	Title     string    `gorm:"column:title"`
	Content   string    `gorm:"column:content"`
	Link      string    `gorm:"column:link"`
	RefID     *uint64   `gorm:"column:ref_id"`
	IsRead    uint8     `gorm:"column:is_read"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 指定表名。
func (Notification) TableName() string { return "notifications" }
