package repository

// 聊天仓储集成测试：跑在真实 MySQL 上，覆盖 PRD 5.1 必过场景的
// 数据层行为——client_msg_id 幂等、并发发送 seq 连续无重复、after_seq 补齐。
//
// 需要 MySQL；不可达时跳过（CI 无数据库环境不阻塞其他测试）。
// DSN 用环境变量 BLOG_TEST_DSN 覆盖，默认指向本地开发库。
//
// 测试数据用带时间戳的唯一键创建并在清理阶段删除，不污染业务数据。

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"go-blog/internal/model"
)

const itDSNDefault = "root:123456@tcp(127.0.0.1:3306)/blog?charset=utf8mb4&parseTime=true&loc=Local"

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("BLOG_TEST_DSN")
	if dsn == "" {
		dsn = itDSNDefault
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Skipf("MySQL 不可达，跳过集成测试: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Skipf("MySQL 不可达，跳过集成测试: %v", err)
	}
	if err := sqlDB.Ping(); err != nil {
		t.Skipf("MySQL 不可达，跳过集成测试: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// itFixture 一次测试的独立数据：两名临时用户 + 一个会话。
type itFixture struct {
	repo       *ChatRepo
	creatorUID uint64
	peerUID    uint64
	convID     uint64
}

func setupFixture(t *testing.T) *itFixture {
	t.Helper()
	db := openTestDB(t)
	repo := NewChatRepo(db)
	// base36 压短：username 列长有限，纳秒时间戳原样会超
	tag := strconv.FormatUint(uint64(time.Now().UnixNano()), 36)

	a := &model.User{Username: "it" + tag + "a", Email: "it" + tag + "a@test.local", Nickname: "A", Role: 2, Status: model.StatusActive}
	b := &model.User{Username: "it" + tag + "b", Email: "it" + tag + "b@test.local", Nickname: "B", Role: 2, Status: model.StatusActive}
	if err := db.Create(a).Error; err != nil {
		t.Fatalf("建临时用户失败: %v", err)
	}
	if err := db.Create(b).Error; err != nil {
		t.Fatalf("建临时用户失败: %v", err)
	}

	conv, created, err := repo.CreateConversation(context.Background(), a.ID, b.ID)
	if err != nil || !created {
		t.Fatalf("建会话失败: %v created=%v", err, created)
	}

	f := &itFixture{repo: repo, creatorUID: a.ID, peerUID: b.ID, convID: conv.ID}
	t.Cleanup(func() {
		db.Exec("DELETE FROM messages WHERE conversation_id = ?", f.convID)
		db.Exec("DELETE FROM conversation_members WHERE conversation_id = ?", f.convID)
		db.Exec("DELETE FROM conversations WHERE id = ?", f.convID)
		db.Exec("DELETE FROM users WHERE id IN (?, ?)", f.creatorUID, f.peerUID)
	})
	return f
}

// send 组装并发送一条文本消息。
func (f *itFixture) send(t *testing.T, ctx context.Context, senderID uint64, content, cmid string) (*model.Message, error) {
	t.Helper()
	msg := &model.Message{
		ConversationID: f.convID,
		SenderID:       senderID,
		Type:           model.MessageTypeText,
		Content:        content,
		Status:         model.MessageStatusNormal,
	}
	if cmid != "" {
		msg.ClientMsgID = &cmid
	}
	err := f.repo.SendMessage(ctx, msg, content, f.peerUID+f.creatorUID-senderID)
	return msg, err
}

// TestSendMessage_SeqContiguous 顺序发送 seq 严格连续（11.6.3：首次=1，无跳号）。
func TestSendMessage_SeqContiguous(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		msg, err := f.send(t, ctx, f.creatorUID, fmt.Sprintf("m%d", i), fmt.Sprintf("cmid-seq-%d", i))
		if err != nil {
			t.Fatalf("第 %d 条发送失败: %v", i, err)
		}
		if msg.Seq != uint64(i) {
			t.Fatalf("seq 应为 %d，实际 %d", i, msg.Seq)
		}
	}
}

// TestSendMessage_ClientMsgIdIdempotent 重复 client_msg_id 只落一条，可查回首次结果。
func TestSendMessage_ClientMsgIdIdempotent(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()

	first, err := f.send(t, ctx, f.creatorUID, "只发一次", "cmid-dup")
	if err != nil {
		t.Fatalf("首次发送失败: %v", err)
	}

	// 仓储层：唯一索引拦截重复插入
	_, err = f.send(t, ctx, f.creatorUID, "只发一次", "cmid-dup")
	if err == nil {
		t.Fatal("重复 client_msg_id 应被唯一索引拦截")
	}
	found, err := f.repo.MessageByClientMsgID(ctx, f.creatorUID, "cmid-dup")
	if err != nil {
		t.Fatalf("查回首次结果失败: %v", err)
	}
	if found.ID != first.ID || found.Seq != first.Seq {
		t.Fatalf("查回结果不一致: %d/%d vs %d/%d", found.ID, found.Seq, first.ID, first.Seq)
	}

	var n int64
	if err := openTestDB(t).Model(&model.Message{}).
		Where("sender_id = ? AND client_msg_id = ?", f.creatorUID, "cmid-dup").
		Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("应恰好 1 条记录: n=%d err=%v", n, err)
	}
}

// TestSendMessage_ConcurrentSeqUnique 并发发送：seq 严格唯一且连续无空洞。
// 直接压 LAST_INSERT_ID 行锁分配路径（TECH_DESIGN 11.6.2）。
func TestSendMessage_ConcurrentSeqUnique(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()
	const n = 10

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		seqs    = make([]uint64, 0, n)
		failure any
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg, err := f.send(t, ctx, f.creatorUID, fmt.Sprintf("c%d", i), fmt.Sprintf("cmid-cc-%d", i))
			if err != nil {
				mu.Lock()
				failure = fmt.Sprintf("并发发送 #%d 失败: %v", i, err)
				mu.Unlock()
				return
			}
			mu.Lock()
			seqs = append(seqs, msg.Seq)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if failure != nil {
		t.Fatal(failure)
	}

	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for i, s := range seqs {
		if s != uint64(i+1) {
			t.Fatalf("并发后 seq 应为 1..%d 连续，实际第 %d 个是 %d（全部：%v）", n, i, s, seqs)
		}
	}
}

// TestMessagesAfterSeq_Backfill 断线补齐：after_seq 之后升序返回，无重复无遗漏。
func TestMessagesAfterSeq_Backfill(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		if _, err := f.send(t, ctx, f.creatorUID, fmt.Sprintf("b%d", i), fmt.Sprintf("cmid-bf-%d", i)); err != nil {
			t.Fatalf("发送 #%d 失败: %v", i, err)
		}
	}
	rows, err := f.repo.MessagesAfterSeq(ctx, f.convID, 2, 50)
	if err != nil {
		t.Fatalf("补齐查询失败: %v", err)
	}
	got := make([]uint64, 0, len(rows))
	for _, r := range rows {
		got = append(got, r.Seq)
	}
	want := []uint64{3, 4, 5}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("补齐应返回 %v，实际 %v", want, got)
	}
}

// TestMessagesAfterSeq_HasMore 积压超过 limit：第一页恰好 limit 条且 hasMore=true，
// 逐页拉取最终覆盖全部积压——防止"少报 hasMore 静默丢消息"的回归。
func TestMessagesAfterSeq_HasMore(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()
	const limit = 5
	for i := 1; i <= limit+5; i++ { // 积压 10 条 > limit 5
		if _, err := f.send(t, ctx, f.creatorUID, fmt.Sprintf("p%d", i), fmt.Sprintf("cmid-hm-%d", i)); err != nil {
			t.Fatalf("发送 #%d 失败: %v", i, err)
		}
	}

	var got []uint64
	afterSeq := uint64(0)
	for page := 0; page < 10; page++ {
		rows, err := f.repo.MessagesAfterSeq(ctx, f.convID, afterSeq, limit)
		if err != nil {
			t.Fatalf("补齐查询失败: %v", err)
		}
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		if page == 0 && !hasMore {
			t.Fatal("积压 10 条、limit=5：第一页必须 hasMore=true（少报会静默丢消息）")
		}
		if len(rows) == 0 && hasMore {
			t.Fatal("空页不应报告 hasMore")
		}
		for _, r := range rows {
			got = append(got, r.Seq)
		}
		if !hasMore {
			break
		}
		afterSeq = rows[len(rows)-1].Seq
	}
	// 全量覆盖：1..10 连续无重复无遗漏
	for i, s := range got {
		if s != uint64(i+1) {
			t.Fatalf("分页拉取应覆盖 seq 1..10 连续，第 %d 个是 %d（全部 %v）", i, s, got)
		}
	}
	if len(got) != 10 {
		t.Fatalf("应拉回 10 条，实际 %d", len(got))
	}
}

// TestMarkRead_Monotonic 已读游标单调：回退不生效，前移生效。
func TestMarkRead_Monotonic(t *testing.T) {
	f := setupFixture(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		if _, err := f.send(t, ctx, f.creatorUID, fmt.Sprintf("r%d", i), fmt.Sprintf("cmid-mr-%d", i)); err != nil {
			t.Fatalf("发送 #%d 失败: %v", i, err)
		}
	}
	if applied, err := f.repo.MarkRead(ctx, f.convID, f.peerUID, 2); err != nil || !applied {
		t.Fatalf("首次标记应生效: applied=%v err=%v", applied, err)
	}
	if applied, err := f.repo.MarkRead(ctx, f.convID, f.peerUID, 1); err != nil || applied {
		t.Fatalf("回退标记不应生效: applied=%v err=%v", applied, err)
	}
	if applied, err := f.repo.MarkRead(ctx, f.convID, f.peerUID, 3); err != nil || !applied {
		t.Fatalf("前移标记应生效: applied=%v err=%v", applied, err)
	}
}
