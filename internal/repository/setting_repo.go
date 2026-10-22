package repository

import (
	"context"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go-blog/internal/model"
)

// SettingRepo 是 settings 表的数据访问对象。
type SettingRepo struct {
	db *gorm.DB
}

// NewSettingRepo 构造 SettingRepo。
func NewSettingRepo(db *gorm.DB) *SettingRepo { return &SettingRepo{db: db} }

// Get 读取单个配置；不存在时 ok=false。
func (r *SettingRepo) Get(ctx context.Context, key string) (value string, ok bool, err error) {
	var s model.Setting
	err = r.db.WithContext(ctx).Where("config_key = ?", key).Take(&s).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	return s.ConfigValue, true, nil
}

// GetBool 读取布尔配置，不存在或解析失败时返回默认值。
func (r *SettingRepo) GetBool(ctx context.Context, key string, def bool) (bool, error) {
	value, ok, err := r.Get(ctx, key)
	if err != nil || !ok {
		return def, err
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return def, nil
	}
	return b, nil
}

// All 读取全部配置为 map。
func (r *SettingRepo) All(ctx context.Context) (map[string]string, error) {
	var rows []model.Setting
	if err := r.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.ConfigKey] = row.ConfigValue
	}
	return out, nil
}

// Set 写入单个配置（upsert）。
func (r *SettingRepo) Set(ctx context.Context, key, value, group, desc string) error {
	s := model.Setting{
		ConfigKey:   key,
		ConfigValue: value,
		ValueType:   "string",
		ConfigGroup: group,
		Description: desc,
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "config_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"config_value", "value_type", "config_group", "description", "updated_at"}),
		}).
		Create(&s).Error
}
