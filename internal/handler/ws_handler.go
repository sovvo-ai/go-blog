package handler

import (
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"go-blog/internal/dto"
	"go-blog/internal/errs"
	"go-blog/internal/middleware"
	"go-blog/internal/pkg/response"
	"go-blog/internal/service"
	"go-blog/internal/ws"
)

// WsHandler 处理 WebSocket 票据签发与连接升级。
type WsHandler struct {
	tickets *ws.TicketStore
	hub     *ws.Hub
	users   service.WsStatusReader
	log     *slog.Logger
}

// NewWsHandler 构造 WsHandler。
func NewWsHandler(tickets *ws.TicketStore, hub *ws.Hub, users service.WsStatusReader, log *slog.Logger) *WsHandler {
	return &WsHandler{tickets: tickets, hub: hub, users: users, log: log}
}

// Ticket 处理 POST /ws/ticket：签发一次性票据（60 秒有效）。
func (h *WsHandler) Ticket(c *gin.Context) {
	ticket, err := h.tickets.Issue(c.Request.Context(), middleware.UIDOf(c))
	if err != nil {
		response.Error(c, errs.Internal(err))
		return
	}
	response.OK(c, dto.WsTicketData{Ticket: ticket, ExpiresIn: 60})
}

// Connect 处理 GET /ws：消费票据、校验状态、注册连接并启动双泵。
//
// 握手失败以 WebSocket 关闭码表达：4001 票据无效或过期，4003 账号被禁用。
func (h *WsHandler) Connect(c *gin.Context) {
	ticket := c.Query("ticket")
	if ticket == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 ticket"})
		return
	}
	uid, err := h.tickets.Consume(c.Request.Context(), ticket)
	if err != nil {
		h.log.Error("ws_ticket_consume_failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "票据服务不可用"})
		return
	}
	if uid == 0 {
		h.closeEarly(c, "4001", "票据无效或过期")
		return
	}

	u, err := h.users.ByID(c.Request.Context(), uid)
	if err != nil {
		h.closeEarly(c, "4001", "票据无效或过期")
		return
	}
	if !u.IsActive() {
		// 禁用用户不允许建立连接（PRD 68：禁用立即生效）
		h.closeEarly(c, "4003", "账号被禁用")
		return
	}

	conn, err := websocket.Accept(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 12) // V1.0 无上行消息，4KB 上限足够丢弃任何帧

	client := ws.NewClient(uid, h.hub)
	h.hub.Register(client)

	// 双泵挂在请求 ctx 上：HTTP 连接断开时随之退出
	ctx := c.Request.Context()
	go client.WritePump(ctx, conn)
	client.ReadPump(ctx, conn) // 读泵阻塞到连接结束
}

// closeEarly 在升级前拒绝连接：以 HTTP 状态响应，并用
// X-Ws-Close-Code 头告知语义化关闭码（4001/4003）。
func (h *WsHandler) closeEarly(c *gin.Context, code string, msg string) {
	c.Header("X-Ws-Close-Code", code)
	c.JSON(http.StatusUnauthorized, gin.H{"error": msg})
}
