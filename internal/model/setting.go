package model

import "time"

// Setting 对应 settings 表（K-V 结构）。
type Setting struct {
	ConfigKey   string    `gorm:"column:config_key;primaryKey"`
	ConfigValue string    `gorm:"column:config_value"`
	ValueType   string    `gorm:"column:value_type"`
	ConfigGroup string    `gorm:"column:config_group"`
	Description string    `gorm:"column:description"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

// TableName 指定表名。
func (Setting) TableName() string { return "settings" }
