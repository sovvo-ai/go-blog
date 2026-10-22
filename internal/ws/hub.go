// Package ws 提供 WebSocket 下行推送：票据、连接管理（Hub）、单客户端泵。
//
// V1.0 无上行消息：发消息、标记已读全部走 HTTP，WebSocket 只做下行
// （TECH_DESIGN 11.2）。多端支持：同一用户可有多个连接，推送遍历全部。
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Frame 统一 JSON 帧（TECH_DESIGN 11.2）。
type Frame struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
	TS   int64           `json:"ts"`
}

// Client 一条 WebSocket 连接。
//
// 关闭用 done 通道而不是 close(send)：Hub 会并发向 send 投递，
// close(send) 之后任何一次并发投递都会 panic（向已关闭通道发送）。
type Client struct {
	UserID uint64
	send   chan []byte
	hub    *Hub

	closeOnce sync.Once
	done      chan struct{}
}

// NewClient 构造连接对象（缓冲 64 帧，慢消费者由发送路径强制下线）。
func NewClient(userID uint64, hub *Hub) *Client {
	return &Client{
		UserID: userID,
		send:   make(chan []byte, 64),
		hub:    hub,
		done:   make(chan struct{}),
	}
}

// shutdown 请求本连接退出：写泵看到 done 关闭后负责关闭底层连接，
// 读泵随底层连接关闭而退出并注销。幂等。
func (c *Client) shutdown() {
	c.closeOnce.Do(func() { close(c.done) })
}

// Hub 连接注册表：uid → 该用户的全部连接。
type Hub struct {
	mu    sync.RWMutex
	conns map[uint64]map[*Client]struct{}

	log *slog.Logger
}

// NewHub 构造 Hub。
func NewHub(log *slog.Logger) *Hub {
	return &Hub{conns: make(map[uint64]map[*Client]struct{}), log: log}
}

// Register 注册连接。
func (h *Hub) Register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[c.UserID] == nil {
		h.conns[c.UserID] = make(map[*Client]struct{})
	}
	h.conns[c.UserID][c] = struct{}{}
}

// Unregister 注销连接；集合空则删除 uid 键。
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.conns[c.UserID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, c.UserID)
		}
	}
}

// Online 用户当前是否有任一在线连接。
func (h *Hub) Online(userID uint64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns[userID]) > 0
}

// PushToUser 向用户全部在线连接推送一帧；离线静默丢弃
// （离线消息靠断线补齐恢复，推送只是加速在线侧的到达）。
func (h *Hub) PushToUser(userID uint64, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		h.log.Error("ws_frame_marshal_failed", slog.Any("error", err))
		return
	}
	// 调用方传来的多数是完整帧（{type,data}，TECH_DESIGN 11.2），只需补 ts；
	// 若是裸数据则按 Frame 包一层。曾在两种路径间出现"帧套帧"（data.data），
	// 接收方 f.data.message 因此取不到值——这里统一判定，杜绝再次分层。
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err == nil {
		if _, hasType := probe["type"]; hasType {
			probe["ts"], _ = json.Marshal(time.Now().UnixMilli())
			if patched, perr := json.Marshal(probe); perr == nil {
				raw = patched
			}
			h.dispatch(userID, raw)
			return
		}
	}
	frame, err := json.Marshal(Frame{Type: frameType(raw), Data: raw, TS: time.Now().UnixMilli()})
	if err != nil {
		h.log.Error("ws_frame_marshal_failed", slog.Any("error", err))
		return
	}

	h.dispatch(userID, frame)
}

// dispatch 把已序列化的帧投递给用户全部在线连接。
func (h *Hub) dispatch(userID uint64, frame []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.conns[userID] {
		select {
		case c.send <- frame:
		case <-c.done:
			// 已在关闭流程，跳过
		default:
			// 发送缓冲满：丢弃并请求关闭该连接，让客户端重连补齐，
			// 避免慢消费者拖垮 Hub。
			c.shutdown()
		}
	}
}

// frameType 从 data 里取 type 字段；取不到用 unknown。
func frameType(raw []byte) string {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && probe.Type != "" {
		return probe.Type
	}
	return "unknown"
}

// Kick 推送 system.kick 并关闭该用户全部连接（禁用用户立即下线）。
func (h *Hub) Kick(userID uint64, reason string) {
	h.PushToUser(userID, map[string]any{
		"type": "system.kick",
		"data": map[string]any{"reason": reason},
	})
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.conns[userID]))
	for c := range h.conns[userID] {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		// 留出帧的发送时间再关闭
		time.AfterFunc(100*time.Millisecond, func() { c.shutdown() })
	}
}

// WritePump 写泵：把 send 通道的帧写出去，并周期性 ping 保活（30 秒）。
//
// 活性判定靠 Ping 写失败（10 秒写超时）：TCP 半开、客户端失联等情况下
// 最多约 40 秒内触发关闭，从而带动读泵退出、连接被清理。
func (c *Client) WritePump(ctx context.Context, conn *websocket.Conn) {
	defer func() {
		c.hub.Unregister(c)
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ping.C:
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(writeCtx)
			cancel()
			if err != nil {
				return
			}
		case frame, ok := <-c.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, frame)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// ReadPump 读泵：V1.0 无上行消息，任何上行帧都忽略。
//
// 读循环不带超时——coder/websocket 里 Read 的 ctx 过期会直接关闭连接，
// 而 Ping 的 Pong 在库内部被消费、不会从 Read 返回，纯接收的推送连接
// 永远等不到数据帧，定读超时等于每 90 秒强制断线一次。
// 死连接的清理由写泵的 Ping 写失败驱动（见 WritePump）。
func (c *Client) ReadPump(ctx context.Context, conn *websocket.Conn) {
	defer func() {
		c.hub.Unregister(c)
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}
