package model

import "time"

// 会话类型。
const (
	ConversationTypeSingle uint8 = 1 // 单聊（V1.0 仅此值）
)

// 消息类型。
const (
	MessageTypeText   uint8 = 1
	MessageTypeImage  uint8 = 2
	MessageTypeSystem uint8 = 3 // 系统消息（V1.0 不产生，仅保留枚举）
)

// 消息状态。
const (
	MessageStatusNormal  uint8 = 1
	MessageStatusRecalled uint8 = 2 // 撤回（V1.1）
	MessageStatusDeleted uint8 = 3
)

// Conversation 对应 conversations 表。
//
// 单聊会话以 conversation_key = min(uid):max(uid) 保证唯一：
// (A,B) 与 (B,A) 落到同一条记录。last_* 三列是会话列表的摘要快照，
// 随每条消息在同一事务内更新。
type Conversation struct {
	ID                 uint64     `gorm:"column:id;primaryKey"`
	Type               uint8      `gorm:"column:type"`
	ConversationKey    string     `gorm:"column:conversation_key"`
	CreatorID          uint64     `gorm:"column:creator_id"`
	LastMessageID      uint64     `gorm:"column:last_message_id"`
	LastMessageAt      *time.Time `gorm:"column:last_message_at"`
	LastMessagePreview string     `gorm:"column:last_message_preview"`
	MessageCount       uint32     `gorm:"column:message_count"`
	SeqCounter         uint64     `gorm:"column:seq_counter"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

// TableName 指定表名。
func (Conversation) TableName() string { return "conversations" }

// ConversationMember 对应 conversation_members 表。
//
// 已读语义（DATABASE.md 4.15.1）：会话级游标 last_read_seq，
// 不做单条消息已读回执；unread_count 是展示快照，标记已读时归零。
type ConversationMember struct {
	ConversationID uint64     `gorm:"column:conversation_id;primaryKey"`
	UserID         uint64     `gorm:"column:user_id;primaryKey"`
	LastReadSeq    uint64     `gorm:"column:last_read_seq"`
	UnreadCount    uint32     `gorm:"column:unread_count"`
	IsMuted        uint8      `gorm:"column:is_muted"`
	JoinedAt       time.Time  `gorm:"column:joined_at"`
	LeftAt         *time.Time `gorm:"column:left_at"`
}

// TableName 指定表名。
func (ConversationMember) TableName() string { return "conversation_members" }

// Message 对应 messages 表。
//
// seq 是会话内单调递增序号（由 conversations.seq_counter 在事务内原子
// 分配），断线补齐与排序都以它为准，不用全局自增 id。
type Message struct {
	ID             uint64    `gorm:"column:id;primaryKey"`
	ConversationID uint64    `gorm:"column:conversation_id"`
	SenderID       uint64    `gorm:"column:sender_id"`
	Seq            uint64    `gorm:"column:seq"`
	Type           uint8     `gorm:"column:type"`
	Content        string    `gorm:"column:content"`
	ClientMsgID    *string   `gorm:"column:client_msg_id"`
	Status         uint8     `gorm:"column:status"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

// TableName 指定表名。
func (Message) TableName() string { return "messages" }
