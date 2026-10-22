package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/repository"
)

// 聊天业务规则（PRD 4.1.6.1 / API_SPEC 4.8）。
const (
	// 每日新建会话上限。
	ChatDailyConversationLimit = 20
	// 文本消息长度上限。
	ChatTextMaxRunes = 2000
	// 会话列表摘要预览长度。
	ChatPreviewMaxRunes = 128
)

// ChatStore 是聊天服务的数据访问能力（仓储实现）。
type ChatStore interface {
	FindConversationByKey(ctx context.Context, key string) (*model.Conversation, error)
	ConversationByID(ctx context.Context, id uint64) (*model.Conversation, error)
	CreatedConversationsToday(ctx context.Context, userID uint64, now time.Time) (int64, error)
	CreateConversation(ctx context.Context, creator, peer uint64) (*model.Conversation, bool, error)
	MemberByUser(ctx context.Context, conversationID, userID uint64) (*model.ConversationMember, error)
	SendMessage(ctx context.Context, msg *model.Message, preview string, peerUserID uint64) error
	MessageByClientMsgID(ctx context.Context, senderID uint64, clientMsgID string) (*model.Message, error)
	MessagesBefore(ctx context.Context, conversationID, beforeID uint64, limit int) ([]model.Message, error)
	MessagesAfterSeq(ctx context.Context, conversationID, afterSeq uint64, limit int) ([]model.Message, error)
	MarkRead(ctx context.Context, conversationID, userID, lastReadSeq uint64) (bool, error)
	PeerLastReadSeq(ctx context.Context, conversationID, userID uint64) (uint64, error)
	SetMuted(ctx context.Context, conversationID, userID uint64, muted bool) error
	ListConversations(ctx context.Context, f repository.ConversationListFilter) ([]repository.ConversationListRow, error)
	LastMessagesByIDs(ctx context.Context, ids []uint64) (map[uint64]model.Message, error)
}

// ChatUsers 是聊天服务读取用户的能力（校验对方状态、组装会话列表）。
type ChatUsers interface {
	ByIDs(ctx context.Context, ids []uint64) (map[uint64]model.User, error)
}

// NotificationPusher 向在线用户推送 notification.new 帧（TECH_DESIGN 11.2）。
type NotificationPusher interface {
	PushToUser(userID uint64, v any)
}

// NotificationFrame 组装 notification.new 帧的 data。
func NotificationFrame(n *model.Notification) map[string]any {
	return map[string]any{"type": "notification.new", "data": n}
}

// WsStatusReader 是 WebSocket 握手期校验用户状态的能力。
type WsStatusReader interface {
	ByID(ctx context.Context, id uint64) (*model.User, error)
}

// ChatNotifier 是聊天服务对外推送的能力（WebSocket Hub 实现）。
type ChatNotifier interface {
	// PushToUser 向用户的全部在线连接推送一帧；用户离线时静默丢弃
	//（离线消息靠断线补齐恢复，推送只是加速）。
	PushToUser(userID uint64, v any)
	// Online 用户当前是否有任一在线连接（会话列表的 online 字段）。
	Online(userID uint64) bool
}

// ChatService 单聊业务。
type ChatService struct {
	store ChatStore
	users ChatUsers
	notif ChatNotifier
	now   func() time.Time
}

// NewChatService 构造 ChatService。
func NewChatService(store ChatStore, users ChatUsers, notif ChatNotifier) *ChatService {
	return &ChatService{store: store, users: users, notif: notif, now: time.Now}
}

// ---- 错误 ----

func errConversationNotFound() *errs.BusinessError {
	return errs.New(errs.CodeConversationNotFound, 404, "会话不存在")
}

func errNotMember() *errs.BusinessError {
	return errs.New(errs.CodeNotConversationMember, 403, "你不是该会话的成员")
}

func errPeerUnavailable() *errs.BusinessError {
	return errs.New(errs.CodePeerUnavailable, 400, "对方账号不可用")
}

func errConversationTooMany() *errs.BusinessError {
	return errs.New(errs.CodeConversationTooMany, 429, "今日发起会话已达上限")
}

// ---- 会话 ----

// Create 发起单聊。幂等：已存在直接返回 created=false。
// 规则：不能与自己会话；对方必须是 active 用户；每日新建上限 20。
func (s *ChatService) Create(ctx context.Context, uid, peerID uint64) (*model.Conversation, bool, error) {
	if uid == peerID {
		return nil, false, errs.BadRequest("不能与自己发起会话")
	}
	users, err := s.users.ByIDs(ctx, []uint64{peerID})
	if err != nil {
		return nil, false, errs.Internal(err)
	}
	peer, ok := users[peerID]
	if !ok || !peer.IsActive() {
		return nil, false, errPeerUnavailable()
	}

	key := repository.ConversationKey(uid, peerID)
	if c, err := s.store.FindConversationByKey(ctx, key); err == nil {
		return c, false, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, false, errs.Internal(err)
	}

	n, err := s.store.CreatedConversationsToday(ctx, uid, s.now())
	if err != nil {
		return nil, false, errs.Internal(err)
	}
	if n >= ChatDailyConversationLimit {
		return nil, false, errConversationTooMany()
	}

	c, created, err := s.store.CreateConversation(ctx, uid, peerID)
	if err != nil {
		return nil, false, errs.Internal(err)
	}
	return c, created, nil
}

// ConversationSummary 会话列表项的业务视图。
type ConversationSummary struct {
	Conversation *model.Conversation
	Peer         *model.User
	LastMessage  *model.Message
	UnreadCount  uint32
	IsMuted      bool
	PeerOnline   bool
}

// List 会话列表：按 last_message_at 倒序，无消息排最后。
// 游标 = base64("unix_nano:id")，取自上一页最后一条。
func (s *ChatService) List(ctx context.Context, uid uint64, cursor string, limit int) ([]ConversationSummary, string, bool, error) {
	f := repository.ConversationListFilter{UserID: uid, Limit: limit}
	if cursor != "" {
		at, id, err := decodeConversationCursor(cursor)
		if err != nil {
			return nil, "", false, errs.BadRequest("游标无效")
		}
		f.CursorAt, f.CursorID = at, id
	}
	rows, err := s.store.ListConversations(ctx, f)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	// 三类数据全部批量取（成员状态已由 JOIN 带出），列表页零逐会话查询
	peerIDs := make([]uint64, 0, len(rows))
	lastIDs := make([]uint64, 0, len(rows))
	for i := range rows {
		if peerID, ok := peerOf(rows[i].ConversationKey, uid); ok {
			peerIDs = append(peerIDs, peerID)
		}
		if rows[i].LastMessageID > 0 {
			lastIDs = append(lastIDs, rows[i].LastMessageID)
		}
	}
	users, err := s.users.ByIDs(ctx, peerIDs)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}
	lastMsgs, err := s.store.LastMessagesByIDs(ctx, lastIDs)
	if err != nil {
		return nil, "", false, errs.Internal(err)
	}

	out := make([]ConversationSummary, 0, len(rows))
	var next string
	for i := range rows {
		row := &rows[i]
		peerID, ok := peerOf(row.ConversationKey, uid)
		if !ok {
			continue
		}
		peer := users[peerID]

		sum := ConversationSummary{
			Conversation: &row.Conversation,
			Peer:         &peer,
			PeerOnline:   s.notif.Online(peerID),
			UnreadCount:  row.UnreadCount,
			IsMuted:      row.IsMuted,
		}
		if lm, ok := lastMsgs[row.LastMessageID]; ok {
			sum.LastMessage = &lm
		}
		out = append(out, sum)
	}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		if last.LastMessageAt != nil {
			next = encodeConversationCursor(*last.LastMessageAt, last.ID)
		}
	}
	return out, next, hasMore, nil
}

// ---- 消息 ----

// Send 发送消息。
//
// client_msg_id 幂等：先查首次结果直接返回；插入撞唯一索引也查回首次结果。
// 对方 disabled 或已不存在 → 50003（会话保留）。
func (s *ChatService) Send(ctx context.Context, uid, conversationID uint64, msgType uint8, content, clientMsgID string) (*model.Message, bool, error) {
	if _, err := s.store.MemberByUser(ctx, conversationID, uid); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			if _, cerr := s.store.ConversationByID(ctx, conversationID); cerr != nil {
				if errors.Is(cerr, repository.ErrNotFound) {
					return nil, false, errConversationNotFound()
				}
				return nil, false, errs.Internal(cerr)
			}
			return nil, false, errNotMember()
		}
		return nil, false, errs.Internal(err)
	}

	if msgType == model.MessageTypeText {
		if strings.TrimSpace(content) == "" {
			return nil, false, errs.BadRequest("消息内容不能为空")
		}
		if len([]rune(content)) > ChatTextMaxRunes {
			return nil, false, errs.BadRequest("消息长度超出限制")
		}
	} else if msgType == model.MessageTypeImage {
		// content 是 media_id（十进制字符串，API_SPEC 4.8 决策记录）
		if id, err := strconv.ParseUint(content, 10, 64); err != nil || id == 0 {
			return nil, false, errs.BadRequest("图片参数无效")
		}
	} else {
		return nil, false, errs.BadRequest("不支持的消息类型")
	}

	// 幂等：同 client_msg_id 的历史消息直接返回（200 路径）
	if existing, err := s.store.MessageByClientMsgID(ctx, uid, clientMsgID); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, false, errs.Internal(err)
	}

	// 对方必须仍是 active 用户（禁用/注销后该会话只读）
	peerID, err := s.peerIDOf(ctx, conversationID, uid)
	if err != nil {
		return nil, false, err
	}
	users, err := s.users.ByIDs(ctx, []uint64{peerID})
	if err != nil {
		return nil, false, errs.Internal(err)
	}
	peer, ok := users[peerID]
	if !ok || !peer.IsActive() {
		return nil, false, errPeerUnavailable()
	}

	msg := &model.Message{
		ConversationID: conversationID,
		SenderID:       uid,
		Type:           msgType,
		Content:        content,
		Status:         model.MessageStatusNormal,
	}
	if clientMsgID != "" {
		msg.ClientMsgID = &clientMsgID
	}
	err = s.store.SendMessage(ctx, msg, MessagePreview(msgType, content), peerID)
	if err != nil {
		// 并发重复 client_msg_id：唯一索引拦截，查回首次结果
		if isDuplicateEntry(err) {
			if existing, gerr := s.store.MessageByClientMsgID(ctx, uid, clientMsgID); gerr == nil {
				return existing, false, nil
			}
		}
		return nil, false, errs.Internal(err)
	}

	// 推送给对方的全部在线连接（先落库后推送）
	s.notif.PushToUser(peerID, NewMessageFrame(conversationID, msg))
	return msg, true, nil
}

// History 历史消息 / 断线补齐。
//
// beforeID（向上翻页，降序）与 afterSeq（补齐，升序）互斥。
func (s *ChatService) History(ctx context.Context, uid, conversationID, beforeID uint64, afterSeq *uint64, limit int) ([]model.Message, uint64, bool, error) {
	if _, err := s.store.MemberByUser(ctx, conversationID, uid); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			if _, cerr := s.store.ConversationByID(ctx, conversationID); cerr != nil && !errors.Is(cerr, repository.ErrNotFound) {
				return nil, 0, false, errs.Internal(cerr)
			}
			return nil, 0, false, errConversationNotFound()
		}
		return nil, 0, false, errs.Internal(err)
	}

	peerLastRead, err := s.store.PeerLastReadSeq(ctx, conversationID, uid)
	if err != nil {
		return nil, 0, false, errs.Internal(err)
	}

	var (
		rows    []model.Message
		hasMore bool
	)
	switch {
	case afterSeq != nil:
		rows, err = s.store.MessagesAfterSeq(ctx, conversationID, *afterSeq, limit)
		// 升序补齐（与 before_id 分支同一模式：多取 1 条判 hasMore 后截断）。
		// 补齐场景不回传游标，取满 limit 时无法精确判断是否恰好取完，
		// 宁过度报告 hasMore=true 让客户端多拉一轮空页，也不能少报静默丢消息。
		hasMore = len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
	case beforeID > 0:
		rows, err = s.store.MessagesBefore(ctx, conversationID, beforeID, limit)
		hasMore = len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
	default:
		rows, err = s.store.MessagesBefore(ctx, conversationID, 0, limit)
		hasMore = len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
	}
	if err != nil {
		return nil, 0, false, errs.Internal(err)
	}
	return rows, peerLastRead, hasMore, nil
}

// MarkRead 标记已读。越界按会话 seq_counter 截断；未前移返回 false（幂等）。
func (s *ChatService) MarkRead(ctx context.Context, uid, conversationID, lastReadSeq uint64) (bool, error) {
	if _, err := s.store.MemberByUser(ctx, conversationID, uid); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, errConversationNotFound()
		}
		return false, errs.Internal(err)
	}
	conv, err := s.store.ConversationByID(ctx, conversationID)
	if err != nil {
		return false, errs.Internal(err)
	}
	if lastReadSeq > conv.SeqCounter {
		lastReadSeq = conv.SeqCounter
	}
	applied, err := s.store.MarkRead(ctx, conversationID, uid, lastReadSeq)
	if err != nil {
		return false, errs.Internal(err)
	}
	if applied {
		peerID, perr := s.peerIDOf(ctx, conversationID, uid)
		if perr == nil {
			s.notif.PushToUser(peerID, NewReadFrame(conversationID, uid, lastReadSeq))
		}
	}
	return applied, nil
}

// SetMuted 设置免打扰。
func (s *ChatService) SetMuted(ctx context.Context, uid, conversationID uint64, muted bool) error {
	if _, err := s.store.MemberByUser(ctx, conversationID, uid); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errConversationNotFound()
		}
		return errs.Internal(err)
	}
	return s.store.SetMuted(ctx, conversationID, uid, muted)
}

// ---- 内部 ----

// peerIDOf 取会话中除 uid 外另一名成员的 ID。
func (s *ChatService) peerIDOf(ctx context.Context, conversationID, uid uint64) (uint64, error) {
	// 单聊恰好两名成员，conversation_key 即成员集合
	conv, err := s.store.ConversationByID(ctx, conversationID)
	if err != nil {
		return 0, errs.Internal(err)
	}
	a, b, ok := splitConversationKey(conv.ConversationKey)
	if !ok {
		return 0, errs.Internal(fmt.Errorf("会话 %d 的 key 无法解析: %s", conversationID, conv.ConversationKey))
	}
	if a == uid {
		return b, nil
	}
	return a, nil
}

// splitConversationKey 解析 "c:a:b" 形式的会话键。
func splitConversationKey(key string) (a, b uint64, ok bool) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 || parts[0] != "c" {
		return 0, 0, false
	}
	a, errA := strconv.ParseUint(parts[1], 10, 64)
	b, errB := strconv.ParseUint(parts[2], 10, 64)
	return a, b, errA == nil && errB == nil
}

// peerOf 返回 uid 在该会话中的对方 ID。
func peerOf(key string, uid uint64) (uint64, bool) {
	a, b, ok := splitConversationKey(key)
	if !ok {
		return 0, false
	}
	if a == uid {
		return b, true
	}
	return a, true
}

// MessagePreview 生成会话列表摘要：文本取前 128 字符，图片用占位符。
func MessagePreview(msgType uint8, content string) string {
	if msgType == model.MessageTypeImage {
		return "[图片]"
	}
	return truncateRunes(content, ChatPreviewMaxRunes)
}

// msgTypeName 消息类型的对外名称。
func msgTypeName(t uint8) string {
	if t == model.MessageTypeImage {
		return "image"
	}
	return "text"
}

// ChatMessageFrame 是 message.new 下行帧的 data（TECH_DESIGN 11.2）。
type ChatMessageFrame struct {
	ConversationID uint64         `json:"conversation_id"`
	Message        ChatMessageDTO `json:"message"`
}

// ChatMessageDTO 是推送给客户端的消息体。
type ChatMessageDTO struct {
	ID        uint64    `json:"id"`
	Seq       uint64    `json:"seq"`
	SenderID  uint64    `json:"sender_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ChatReadFrame 是 message.read 下行帧的 data。
type ChatReadFrame struct {
	ConversationID uint64 `json:"conversation_id"`
	UserID         uint64 `json:"user_id"`
	LastReadSeq    uint64 `json:"last_read_seq"`
}

// NewMessageFrame 组装 message.new 帧。
func NewMessageFrame(conversationID uint64, m *model.Message) map[string]any {
	return map[string]any{
		"type": "message.new",
		"data": ChatMessageFrame{
			ConversationID: conversationID,
			Message: ChatMessageDTO{
				ID:        m.ID,
				Seq:       m.Seq,
				SenderID:  m.SenderID,
				Type:      msgTypeName(m.Type),
				Content:   m.Content,
				Status:    "normal",
				CreatedAt: m.CreatedAt,
			},
		},
	}
}

// NewReadFrame 组装 message.read 帧。
func NewReadFrame(conversationID, userID, lastReadSeq uint64) map[string]any {
	return map[string]any{
		"type": "message.read",
		"data": ChatReadFrame{
			ConversationID: conversationID,
			UserID:         userID,
			LastReadSeq:    lastReadSeq,
		},
	}
}

// isDuplicateEntry 判断是否唯一键冲突。
func isDuplicateEntry(err error) bool {
	return err != nil && errors.Is(err, gorm.ErrDuplicatedKey) ||
		(err != nil && strings.Contains(err.Error(), "Duplicate entry"))
}

// ---- 会话列表游标 ----

func encodeConversationCursor(at time.Time, id uint64) string {
	raw := strconv.FormatInt(at.UnixNano(), 10) + ":" + strconv.FormatUint(id, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeConversationCursor(cursor string) (*time.Time, uint64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, 0, err
	}
	parts := strings.Split(string(raw), ":")
	if len(parts) != 2 {
		return nil, 0, errors.New("cursor 格式无效")
	}
	nano, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, 0, err
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return nil, 0, err
	}
	at := time.Unix(0, nano)
	return &at, id, nil
}
