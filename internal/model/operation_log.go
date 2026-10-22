package model

import "time"

// OperationLog 对应 operation_logs 表（操作审计）。
type OperationLog struct {
	ID         uint64    `gorm:"column:id;primaryKey"`
	OperatorID uint64    `gorm:"column:operator_id"`
	Action     string    `gorm:"column:action"`
	TargetType string    `gorm:"column:target_type"`
	TargetID   uint64    `gorm:"column:target_id"`
	Detail     *string   `gorm:"column:detail"` // JSON
	IP         string    `gorm:"column:ip"`
	UserAgent  string    `gorm:"column:user_agent"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

// TableName 指定表名。
func (OperationLog) TableName() string { return "operation_logs" }
