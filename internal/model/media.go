package model

import "time"

// 媒体用途。
const (
	MediaPurposeArticle = "article"
	MediaPurposeAvatar  = "avatar"
	MediaPurposeChat    = "chat"
)

// Media 对应 media 表。
//
// 数据库只存对象 Key 与元数据，**不存磁盘绝对路径**：
// 换存储实现或迁移机器时，Key 依然有效。
type Media struct {
	ID           uint64    `gorm:"column:id;primaryKey"`
	UploaderID   uint64    `gorm:"column:uploader_id"`
	Bucket       string    `gorm:"column:bucket"`
	ObjectKey    string    `gorm:"column:object_key"`
	OriginalName string    `gorm:"column:original_name"`
	MimeType     string    `gorm:"column:mime_type"`
	Size         int64     `gorm:"column:size"`
	Width        int       `gorm:"column:width"`
	Height       int       `gorm:"column:height"`
	SHA256       string    `gorm:"column:sha256"`
	Variants     *string   `gorm:"column:variants"` // JSON: {"64":"key","128":"key"}
	Purpose      string    `gorm:"column:purpose"`
	Status       uint8     `gorm:"column:status"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

// TableName 指定表名。
func (Media) TableName() string { return "media" }
