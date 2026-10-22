package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"go-blog/internal/model"
)

// ConversationKey 生成单聊会话唯一键：min(uid):max(uid)。
// (A,B) 与 (B,A) 得到同一个 key，由唯一索引保证会话唯一。
func ConversationKey(a, b uint64) string {
	if a > b {
		a, b = b, a
	}
	return strings.Join([]string{"c", itoa64(a), itoa64(b)}, ":")
}

func itoa64(v uint64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// ChatRepo 负责会话、成员、消息的数据访问。
type ChatRepo struct {
	db *gorm.DB
}

// NewChatRepo 构造 ChatRepo。
func NewChatRepo(db *gorm.DB) *ChatRepo {
	return &ChatRepo{db: db}
}

// FindConversationByKey 按唯一键取会话；不存在返回 ErrNotFound。
func (r *ChatRepo) FindConversationByKey(ctx context.Context, key string) (*model.Conversation, error) {
	var c model.Conversation
	err := r.db.WithContext(ctx).Where("conversation_key = ?", key).Take(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ConversationByID 按 ID 取会话；不存在返回 ErrNotFound。
func (r *ChatRepo) ConversationByID(ctx context.Context, id uint64) (*model.Conversation, error) {
	var c model.Conversation
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreatedConversationsToday 统计用户今日（本地时区自然日）发起的会话数。
// 每日新建上限按 creator_id 计数（PRD 4.1.6.1），不把别人发起的会话算进来。
func (r *ChatRepo) CreatedConversationsToday(ctx context.Context, userID uint64, now time.Time) (int64, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Conversation{}).
		Where("creator_id = ? AND created_at >= ?", userID, start).
		Count(&n).Error
	return n, err
}

// CreateConversation 幂等创建会话与两名成员。
//
// INSERT IGNORE + 按 key 查回：并发发起同一会话时只落一条记录。
// 返回 (会话, 是否本次新建)。
func (r *ChatRepo) CreateConversation(ctx context.Context, creator, peer uint64) (*model.Conversation, bool, error) {
	key := ConversationKey(creator, peer)
	var c model.Conversation
	var created bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			"INSERT IGNORE INTO conversations (type, conversation_key, creator_id) VALUES (?, ?, ?)",
			model.ConversationTypeSingle, key, creator,
		)
		if res.Error != nil {
			return res.Error
		}
		created = res.RowsAffected == 1

		if err := tx.Where("conversation_key = ?", key).Take(&c).Error; err != nil {
			return err
		}
		members := tx.Exec(
			"INSERT IGNORE INTO conversation_members (conversation_id, user_id) VALUES (?, ?), (?, ?)",
			c.ID, creator, c.ID, peer,
		)
		return members.Error
	})
	if err != nil {
		return nil, false, err
	}
	return &c, created, nil
}

// Members 取会话成员。
func (r *ChatRepo) Members(ctx context.Context, conversationID uint64) ([]model.ConversationMember, error) {
	var rows []model.ConversationMember
	err := r.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("user_id ASC").
		Find(&rows).Error
	return rows, err
}

// MemberByUser 取调用者的成员记录；不是成员返回 ErrNotFound。
func (r *ChatRepo) MemberByUser(ctx context.Context, conversationID, userID uint64) (*model.ConversationMember, error) {
	var m model.ConversationMember
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// SendMessage 在一个事务内完成：seq 原子分配 → 落消息 → 更新会话摘要 →
// 对方未读数 +1（免打扰也计数，仅提醒方式不同）。
//
// seq 用 LAST_INSERT_ID(expr) 在行锁内分配（TECH_DESIGN 11.6.2），
// 并发发送严格递增、事务失败随事务回滚，不产生空洞。
// client_msg_id 唯一索引兜底幂等：并发重复提交时后者插入失败，由服务层查回首次结果。
func (r *ChatRepo) SendMessage(ctx context.Context, msg *model.Message, preview string, peerUserID uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(
			"UPDATE conversations SET seq_counter = LAST_INSERT_ID(seq_counter + 1) WHERE id = ?",
			msg.ConversationID,
		).Error; err != nil {
			return err
		}
		var seq int64
		if err := tx.Raw("SELECT LAST_INSERT_ID()").Scan(&seq).Error; err != nil {
			return err
		}
		msg.Seq = uint64(seq)

		if err := tx.Create(msg).Error; err != nil {
			return err
		}
		if err := tx.Exec(
			"UPDATE conversations SET last_message_id = ?, last_message_at = NOW(), last_message_preview = ?, message_count = message_count + 1 WHERE id = ?",
			msg.ID, preview, msg.ConversationID,
		).Error; err != nil {
			return err
		}
		return tx.Exec(
			"UPDATE conversation_members SET unread_count = unread_count + 1 WHERE conversation_id = ? AND user_id = ?",
			msg.ConversationID, peerUserID,
		).Error
	})
}

// MessageByClientMsgID 按幂等键查首次发送的消息；不存在返回 ErrNotFound。
func (r *ChatRepo) MessageByClientMsgID(ctx context.Context, senderID uint64, clientMsgID string) (*model.Message, error) {
	var m model.Message
	err := r.db.WithContext(ctx).
		Where("sender_id = ? AND client_msg_id = ?", senderID, clientMsgID).
		Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MessagesBefore 历史消息：seq 小于 cursor 的最近 limit+1 条，降序。
// id 传 0 表示从最新开始。
func (r *ChatRepo) MessagesBefore(ctx context.Context, conversationID, beforeID uint64, limit int) ([]model.Message, error) {
	q := r.db.WithContext(ctx).Model(&model.Message{}).
		Where("conversation_id = ? AND status = ?", conversationID, model.MessageStatusNormal)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	var rows []model.Message
	err := q.Order("seq DESC").Limit(limit + 1).Find(&rows).Error
	return rows, err
}

// MessagesAfterSeq 断线补齐：seq 大于 afterSeq 的消息，升序。
// MessagesAfterSeq 断线补齐：seq 大于 afterSeq 的消息，升序。
// 取 limit+1 条：第 limit+1 条的存在即 hasMore 依据，由服务层截断。
func (r *ChatRepo) MessagesAfterSeq(ctx context.Context, conversationID, afterSeq uint64, limit int) ([]model.Message, error) {
	var rows []model.Message
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND seq > ? AND status = ?", conversationID, afterSeq, model.MessageStatusNormal).
		Order("seq ASC").Limit(limit + 1).Find(&rows).Error
	return rows, err
}

// MarkRead 更新自己的已读游标与未读数。
//
// 单调递增：新值不小于当前值才生效（API_SPEC 4.8）；越界由服务层截断。
// 返回是否实际前移（用于决定是否推送 message.read）。
func (r *ChatRepo) MarkRead(ctx context.Context, conversationID, userID, lastReadSeq uint64) (bool, error) {
	var applied bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(
			"UPDATE conversation_members SET last_read_seq = ?, unread_count = 0 WHERE conversation_id = ? AND user_id = ? AND last_read_seq < ?",
			lastReadSeq, conversationID, userID, lastReadSeq,
		)
		if res.Error != nil {
			return res.Error
		}
		applied = res.RowsAffected == 1
		return nil
	})
	return applied, err
}

// PeerLastReadSeq 取对方的已读游标。
func (r *ChatRepo) PeerLastReadSeq(ctx context.Context, conversationID, userID uint64) (uint64, error) {
	var seq uint64
	err := r.db.WithContext(ctx).Model(&model.ConversationMember{}).
		Where("conversation_id = ? AND user_id <> ?", conversationID, userID).
		Pluck("last_read_seq", &seq).Error
	return seq, err
}

// SetMuted 设置免打扰。
func (r *ChatRepo) SetMuted(ctx context.Context, conversationID, userID uint64, muted bool) error {
	v := 0
	if muted {
		v = 1
	}
	return r.db.WithContext(ctx).Model(&model.ConversationMember{}).
		Where("conversation_id = ? AND user_id = ?", conversationID, userID).
		Update("is_muted", v).Error
}

// ConversationListFilter 会话列表查询条件。
type ConversationListFilter struct {
	UserID   uint64
	CursorAt *time.Time // 上一页最后一条的 last_message_at
	CursorID uint64     // 同刻并列时的次级游标
	Limit    int
}

// ConversationListRow 是会话列表行：会话本体 + 调用者的成员状态。
// 成员状态在 JOIN 里一并取出，避免列表页逐会话补查（N+1）。
type ConversationListRow struct {
	model.Conversation
	UnreadCount uint32
	IsMuted     bool
}

// ListConversations 会话列表：按 last_message_at 倒序，无消息（NULL）排最后。
func (r *ChatRepo) ListConversations(ctx context.Context, f ConversationListFilter) ([]ConversationListRow, error) {
	q := r.db.WithContext(ctx).Table("conversations").
		Select("conversations.*, m.unread_count AS unread_count, m.is_muted AS is_muted").
		Joins("JOIN conversation_members m ON m.conversation_id = conversations.id AND m.user_id = ?", f.UserID)
	if f.CursorAt != nil {
		// (NULL 排最后)：有游标时只取"更新或同刻更晚 id"的记录
		q = q.Where(
			"conversations.last_message_at < ? OR (conversations.last_message_at = ? AND conversations.id < ?) OR conversations.last_message_at IS NULL",
			f.CursorAt, f.CursorAt, f.CursorID,
		)
	}
	var rows []ConversationListRow
	err := q.
		Order("conversations.last_message_at IS NULL ASC, conversations.last_message_at DESC, conversations.id DESC").
		Limit(f.Limit + 1).
		Find(&rows).Error
	return rows, err
}

// LastMessagesByIDs 批量取消息（会话列表摘要用，避免逐会话查询）。
func (r *ChatRepo) LastMessagesByIDs(ctx context.Context, ids []uint64) (map[uint64]model.Message, error) {
	out := make(map[uint64]model.Message, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.Message
	if err := r.db.WithContext(ctx).
		Where("id IN ?", ids).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, m := range rows {
		out[m.ID] = m
	}
	return out, nil
}

// LastMessageOf 取会话最后一条消息（会话列表摘要用）。
func (r *ChatRepo) LastMessageOf(ctx context.Context, conversationID, lastMessageID uint64) (*model.Message, error) {
	if lastMessageID == 0 {
		return nil, ErrNotFound
	}
	var m model.Message
	err := r.db.WithContext(ctx).
		Where("id = ? AND conversation_id = ?", lastMessageID, conversationID).
		Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// HasConversationMessageWithContent 判断用户是否是"包含 content 为指定值
// 的消息"的会话成员。聊天图片签名 URL 的成员校验用（API_SPEC 4.7）。
func (r *ChatRepo) HasConversationMessageWithContent(ctx context.Context, userID uint64, content string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Message{}).
		Joins("JOIN conversation_members cm ON cm.conversation_id = messages.conversation_id AND cm.user_id = ?", userID).
		Where("messages.content = ? AND messages.type = ?", content, model.MessageTypeImage).
		Limit(1).Count(&n).Error
	return n > 0, err
}

// MessageByID 按全局 ID 取消息；不存在返回 ErrNotFound。
func (r *ChatRepo) MessageByID(ctx context.Context, id uint64) (*model.Message, error) {
	var m model.Message
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
