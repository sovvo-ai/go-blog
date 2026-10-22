package repository

import (
	"context"

	"gorm.io/gorm"

	"go-blog/internal/model"
)

// ListNotifications 分页返回用户通知，按 ID 倒序（新通知在前）。
func (r *InteractionRepo) ListNotifications(ctx context.Context, userID uint64, unreadOnly bool, cursorID uint64, limit int) ([]model.Notification, error) {
	var rows []model.Notification
	q := r.db.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ?", userID)
	if unreadOnly {
		q = q.Where("is_read = 0")
	}
	if cursorID > 0 {
		q = q.Where("id < ?", cursorID)
	}
	err := q.Order("id DESC").Limit(limit + 1).Find(&rows).Error
	return rows, err
}

// UnreadCount 统计未读通知数。
func (r *InteractionRepo) UnreadCount(ctx context.Context, userID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND is_read = 0", userID).
		Count(&n).Error
	return n, err
}

// MarkNotificationRead 标记通知已读；仅限本人。返回 (是否命中, error)。
func (r *InteractionRepo) MarkNotificationRead(ctx context.Context, id, userID uint64) (bool, error) {
	res := r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("id = ? AND user_id = ?", id, userID).
		Update("is_read", 1)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// ListReports 分页返回举报队列（管理端）。
func (r *InteractionRepo) ListReports(ctx context.Context, status *uint8, targetType string, cursorID uint64, limit int) ([]model.Report, error) {
	var rows []model.Report
	q := r.db.WithContext(ctx).Model(&model.Report{})
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	if targetType != "" {
		q = q.Where("target_type = ?", targetType)
	}
	if cursorID > 0 {
		q = q.Where("id < ?", cursorID)
	}
	err := q.Order("id DESC").Limit(limit + 1).Find(&rows).Error
	return rows, err
}

// UpdateReportStatus 更新举报处理状态（管理端）。
func (r *InteractionRepo) UpdateReportStatus(ctx context.Context, id, handlerID uint64, status uint8, note string) error {
	updates := map[string]any{
		"status":      status,
		"handler_id":  handlerID,
		"handle_note": note,
		"handled_at":  gorm.Expr("NOW()"),
	}
	return r.db.WithContext(ctx).Model(&model.Report{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// MarkAllNotificationsRead 把用户全部未读通知置为已读，返回更新的条数。
func (r *InteractionRepo) MarkAllNotificationsRead(ctx context.Context, userID uint64) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND is_read = 0", userID).
		Update("is_read", 1)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
