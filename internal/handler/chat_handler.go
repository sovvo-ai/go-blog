package handler

import (
	"github.com/gin-gonic/gin"

	"go-blog/internal/dto"
	"go-blog/internal/errs"
	"go-blog/internal/middleware"
	"go-blog/internal/model"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
)

// ChatHandler 处理单聊 HTTP 接口（API_SPEC 4.8）。
type ChatHandler struct {
	svc *service.ChatService
}

// NewChatHandler 构造 ChatHandler。
func NewChatHandler(svc *service.ChatService) *ChatHandler {
	return &ChatHandler{svc: svc}
}

// ListConversations 处理 GET /conversations。
func (h *ChatHandler) ListConversations(c *gin.Context) {
	limit := queryInt(c, "limit", 20)
	items, next, hasMore, err := h.svc.List(c.Request.Context(), middleware.UIDOf(c), c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]dto.ConversationListItem, 0, len(items))
	for _, it := range items {
		item := dto.ConversationListItem{
			Id:          uidString(it.Conversation.ID),
			Type:        "single",
			UnreadCount: int(it.UnreadCount),
			IsMuted:     it.IsMuted,
			Peer: dto.ChatPeer{
				Id:       uidString(it.Peer.ID),
				Nickname: it.Peer.DisplayName(),
				Online:   it.PeerOnline,
			},
		}
		if it.Peer.AvatarURL != "" {
			item.Peer.AvatarUrl = &it.Peer.AvatarURL
		}
		if it.LastMessage != nil {
			item.LastMessage = &dto.ChatLastMessage{
				Id:        uidString(it.LastMessage.ID),
				Preview:   it.Conversation.LastMessagePreview,
				SenderId:  uidString(it.LastMessage.SenderID),
				CreatedAt: it.LastMessage.CreatedAt.UTC(),
			}
		}
		out = append(out, item)
	}
	response.OK(c, dto.ConversationListData{Items: out, NextCursor: nullString(next), HasMore: hasMore})
}

// CreateConversation 处理 POST /conversations。
// 幂等：已存在返回 200 + created=false；新建 201 + created=true。
func (h *ChatHandler) CreateConversation(c *gin.Context) {
	var req dto.ConversationCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求体无效"))
		return
	}
	peerID := parseUintPath(req.PeerId)
	if peerID == 0 {
		response.Error(c, errs.BadRequest("无效的对方用户 ID"))
		return
	}
	conv, created, err := h.svc.Create(c.Request.Context(), middleware.UIDOf(c), peerID)
	if err != nil {
		response.Error(c, err)
		return
	}
	data := dto.ConversationCreateData{Id: uidString(conv.ID), Type: "single", Created: created}
	if created {
		response.Created(c, data)
		return
	}
	response.OK(c, data)
}

// ListMessages 处理 GET /conversations/{id}/messages。
func (h *ChatHandler) ListMessages(c *gin.Context) {
	convID := parseUintPath(c.Param("id"))
	if convID == 0 {
		response.Error(c, errs.BadRequest("无效的会话 ID"))
		return
	}
	limit := queryInt(c, "limit", 30)

	var (
		beforeID = parseUintPath(c.Query("before_id"))
		afterSeq *uint64
	)
	if raw := c.Query("after_seq"); raw != "" {
		v := parseUintPath(raw)
		afterSeq = &v
	}
	if beforeID > 0 && afterSeq != nil {
		response.Error(c, errs.BadRequest("before_id 与 after_seq 不能同时使用"))
		return
	}

	msgs, peerLastRead, hasMore, err := h.svc.History(c.Request.Context(), middleware.UIDOf(c), convID, beforeID, afterSeq, limit)
	if err != nil {
		response.Error(c, err)
		return
	}
	items := make([]dto.ChatMessage, 0, len(msgs))
	for i := range msgs {
		items = append(items, chatMessageDTO(&msgs[i]))
	}
	response.OK(c, dto.MessageListData{Items: items, HasMore: hasMore, PeerLastReadSeq: int(peerLastRead)})
}

// SendMessage 处理 POST /conversations/{id}/messages。
// 首次发送 201；client_msg_id 幂等命中 200。
func (h *ChatHandler) SendMessage(c *gin.Context) {
	convID := parseUintPath(c.Param("id"))
	if convID == 0 {
		response.Error(c, errs.BadRequest("无效的会话 ID"))
		return
	}
	var req dto.MessageSendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求体无效"))
		return
	}
	if req.ClientMsgId == "" || len(req.ClientMsgId) > 64 {
		response.Error(c, errs.BadRequest("client_msg_id 必须为 1–64 字符"))
		return
	}
	var msgType uint8
	switch req.Type {
	case "text":
		msgType = model.MessageTypeText
	case "image":
		msgType = model.MessageTypeImage
	default:
		response.Error(c, errs.BadRequest("不支持的消息类型"))
		return
	}

	msg, created, err := h.svc.Send(c.Request.Context(), middleware.UIDOf(c), convID, msgType, req.Content, req.ClientMsgId)
	if err != nil {
		response.Error(c, err)
		return
	}
	data := dto.MessageSendData{
		Id:        uidString(msg.ID),
		Seq:       int(msg.Seq),
		CreatedAt: msg.CreatedAt.UTC(),
	}
	if created {
		response.Created(c, data)
		return
	}
	response.OK(c, data)
}

// MarkRead 处理 PUT /conversations/{id}/read。
func (h *ChatHandler) MarkRead(c *gin.Context) {
	convID := parseUintPath(c.Param("id"))
	if convID == 0 {
		response.Error(c, errs.BadRequest("无效的会话 ID"))
		return
	}
	var req dto.ConversationReadRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.LastReadSeq < 0 {
		response.Error(c, errs.BadRequest("请求体无效"))
		return
	}
	if _, err := h.svc.MarkRead(c.Request.Context(), middleware.UIDOf(c), convID, uint64(req.LastReadSeq)); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// SetMute 处理 PUT /conversations/{id}/mute。
func (h *ChatHandler) SetMute(c *gin.Context) {
	convID := parseUintPath(c.Param("id"))
	if convID == 0 {
		response.Error(c, errs.BadRequest("无效的会话 ID"))
		return
	}
	var req dto.ConversationMuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errs.BadRequest("请求体无效"))
		return
	}
	if err := h.svc.SetMuted(c.Request.Context(), middleware.UIDOf(c), convID, req.Muted); err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

// chatMessageDTO 消息模型转 DTO。
func chatMessageDTO(m *model.Message) dto.ChatMessage {
	return dto.ChatMessage{
		Id:        uidString(m.ID),
		Seq:       int(m.Seq),
		SenderId:  uidString(m.SenderID),
		Type:      msgDTOType(m.Type),
		Content:   m.Content,
		Status:    dto.ChatMessageStatus("normal"),
		CreatedAt: m.CreatedAt.UTC(),
	}
}

func msgDTOType(t uint8) dto.ChatMessageType {
	if t == model.MessageTypeImage {
		return "image"
	}
	return "text"
}
