package ws

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func newTestHub() *Hub {
	return NewHub(discardLogger())
}

func TestOnline_MultiConn(t *testing.T) {
	h := newTestHub()
	if h.Online(1) {
		t.Fatal("无人在线时 Online 应为 false")
	}
	c1 := NewClient(1, h)
	c2 := NewClient(1, h) // 同一用户第二条连接（双端）
	c3 := NewClient(2, h)
	h.Register(c1)
	h.Register(c2)
	h.Register(c3)
	if !h.Online(1) || !h.Online(2) {
		t.Fatal("注册后应在线")
	}
	h.Unregister(c1)
	if !h.Online(1) {
		t.Fatal("还有第二条连接时用户仍应在线")
	}
	h.Unregister(c2)
	if h.Online(1) {
		t.Fatal("全部连接注销后用户应离线")
	}
}

func TestPushToUser_DeliversAndSkipsOffline(t *testing.T) {
	h := newTestHub()
	c := NewClient(7, h)
	h.Register(c)

	h.PushToUser(7, map[string]any{"type": "message.new", "data": map[string]any{"n": 1}})
	select {
	case raw := <-c.send:
		var f Frame
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("帧不可解析: %v", err)
		}
		if f.Type != "message.new" {
			t.Fatalf("帧类型错误: %s", f.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("在线用户应收到帧")
	}

	// 离线用户：静默无副作用（不应 panic、不应写入任何通道）
	h.PushToUser(999, map[string]any{"type": "x"})
}

func TestPushToUser_ConcurrentShutdownNoPanic(t *testing.T) {
	// 回归：关闭用 done 通道后，关闭与投递并发不再 panic
	//（旧实现 close(send) 后并发投递会 send on closed channel）
	h := newTestHub()
	c := NewClient(5, h)
	h.Register(c)
	c.shutdown()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.PushToUser(5, map[string]any{"type": "race"})
			_ = h.Online(5)
			h.Unregister(c)
		}()
	}
	wg.Wait()
}

func TestKick_PushesFrameThenCloses(t *testing.T) {
	h := newTestHub()
	c := NewClient(9, h)
	h.Register(c)

	h.Kick(9, "账号已被禁用")

	// system.kick 帧先进 send
	select {
	case raw := <-c.send:
		var f Frame
		_ = json.Unmarshal(raw, &f)
		if f.Type != "system.kick" {
			t.Fatalf("应推送 system.kick，实际 %s", f.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("未收到 system.kick")
	}

	// ~100ms 后连接进入关闭态
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-c.done:
			return // 被踢下线 ✓
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("Kick 后连接应进入关闭态")
}

func TestPushToUser_SlowConsumerShutdown(t *testing.T) {
	// 缓冲 64，灌满后继续推应触发 shutdown 而不是阻塞 Hub
	h := newTestHub()
	c := NewClient(11, h)
	h.Register(c)
	for i := 0; i < 70; i++ {
		h.PushToUser(11, map[string]any{"type": "flood"})
	}
	select {
	case <-c.done:
	default:
		t.Fatal("缓冲满后应请求关闭慢消费者连接")
	}
}
