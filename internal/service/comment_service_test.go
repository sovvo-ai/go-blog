package service

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/repository"
)

// ---- 评论测试替身 ----

type fakeCommentArticleStore struct {
	articles map[uint64]*model.Article
	users    map[uint64]model.User
}

func (f *fakeCommentArticleStore) ByID(_ context.Context, id uint64) (*model.Article, error) {
	a, ok := f.articles[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return a, nil
}

func (f *fakeCommentArticleStore) UsersByIDs(_ context.Context, ids []uint64) (map[uint64]model.User, error) {
	out := map[uint64]model.User{}
	for _, id := range ids {
		if u, ok := f.users[id]; ok {
			out[id] = u
		}
	}
	return out, nil
}

type fakeCommentStore struct {
	comments map[uint64]*model.Comment
	nextID   uint64
	created  []*model.Comment
}

func (f *fakeCommentStore) CreateComment(_ context.Context, c *model.Comment, _ bool) error {
	f.nextID++
	c.ID = f.nextID
	f.comments[c.ID] = c
	f.created = append(f.created, c)
	return nil
}

func (f *fakeCommentStore) CommentByID(_ context.Context, id uint64) (*model.Comment, error) {
	c, ok := f.comments[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return c, nil
}

func (f *fakeCommentStore) RootComments(context.Context, uint64, uint64, uint64, int) ([]model.Comment, error) {
	return nil, nil
}
func (f *fakeCommentStore) RepliesByRoots(context.Context, []uint64, uint64, int) (map[uint64][]model.Comment, error) {
	return nil, nil
}
func (f *fakeCommentStore) ListReplies(context.Context, uint64, uint64, uint64, int) ([]model.Comment, error) {
	return nil, nil
}
func (f *fakeCommentStore) SoftDeleteComment(context.Context, uint64) (int, error) { return 0, nil }
func (f *fakeCommentStore) ApproveComment(context.Context, uint64) (*model.Comment, error) {
	return nil, nil
}
func (f *fakeCommentStore) RejectComment(context.Context, uint64) (*model.Comment, error) {
	return nil, nil
}

type fakeSettingReader struct{ values map[string]bool }

func (f *fakeSettingReader) GetBool(_ context.Context, key string, def bool) (bool, error) {
	if v, ok := f.values[key]; ok {
		return v, nil
	}
	return def, nil
}

type fakeNotificationStore struct{ created []*model.Notification }

func (f *fakeNotificationStore) CreateNotification(_ context.Context, n *model.Notification) error {
	f.created = append(f.created, n)
	return nil
}

func newCommentService(articles *fakeCommentArticleStore, store *fakeCommentStore, review bool) (*CommentService, *fakeNotificationStore) {
	notify := &fakeNotificationStore{}
	settings := &fakeSettingReader{values: map[string]bool{"review.comment": review}}
	svc := NewCommentService(store, articles, settings, notify, nil, slog.Default())
	return svc, notify
}

// ---- 用例 ----

func TestCommentCreate_Root_ReviewOn(t *testing.T) {
	articles := &fakeCommentArticleStore{
		articles: map[uint64]*model.Article{
			1: {ID: 1, Slug: "a", Status: model.ArticlePublished, AllowComment: 1, AuthorID: 9},
		},
		users: map[uint64]model.User{9: {ID: 9, Nickname: "作者"}},
	}
	store := &fakeCommentStore{comments: map[uint64]*model.Comment{}}
	svc, notify := newCommentService(articles, store, true)

	c, err := svc.Create(context.Background(), 5, 1, CommentInput{Content: "好文章"})
	require.NoError(t, err)
	assert.Equal(t, model.CommentPending, c.Status, "审核开启时初始为 pending")
	assert.Equal(t, uint64(0), c.RootID)
	// 主楼 → 通知作者
	require.Len(t, notify.created, 1)
	assert.Equal(t, uint64(9), notify.created[0].UserID)
}

func TestCommentCreate_Root_ReviewOff(t *testing.T) {
	articles := &fakeCommentArticleStore{
		articles: map[uint64]*model.Article{
			1: {ID: 1, Slug: "a", Status: model.ArticlePublished, AllowComment: 1, AuthorID: 9},
		},
		users: map[uint64]model.User{},
	}
	store := &fakeCommentStore{comments: map[uint64]*model.Comment{}}
	svc, _ := newCommentService(articles, store, false)

	c, err := svc.Create(context.Background(), 5, 1, CommentInput{Content: "好文章"})
	require.NoError(t, err)
	assert.Equal(t, model.CommentApproved, c.Status, "审核关闭时直接 approved")
}

func TestCommentCreate_CommentDisabled(t *testing.T) {
	articles := &fakeCommentArticleStore{
		articles: map[uint64]*model.Article{
			1: {ID: 1, Status: model.ArticlePublished, AllowComment: 0},
		},
		users: map[uint64]model.User{},
	}
	svc, _ := newCommentService(articles, &fakeCommentStore{comments: map[uint64]*model.Comment{}}, true)

	_, err := svc.Create(context.Background(), 5, 1, CommentInput{Content: "x"})
	require.Error(t, err)
	assert.Equal(t, errs.CodeCommentDisabled, errs.From(err).Code)
}

func TestCommentCreate_ParentNotApproved(t *testing.T) {
	articles := &fakeCommentArticleStore{
		articles: map[uint64]*model.Article{
			1: {ID: 1, Slug: "a", Status: model.ArticlePublished, AllowComment: 1, AuthorID: 9},
		},
		users: map[uint64]model.User{},
	}
	store := &fakeCommentStore{comments: map[uint64]*model.Comment{
		1: {ID: 1, ArticleID: 1, Status: model.CommentPending},
	}}
	svc, _ := newCommentService(articles, store, true)

	_, err := svc.Create(context.Background(), 5, 1, CommentInput{Content: "回复", ParentID: 1})
	require.Error(t, err)
	assert.Equal(t, errs.CodeParentCommentBad, errs.From(err).Code)
}

func TestCommentCreate_ParentFromOtherArticle(t *testing.T) {
	articles := &fakeCommentArticleStore{
		articles: map[uint64]*model.Article{
			1: {ID: 1, Slug: "a", Status: model.ArticlePublished, AllowComment: 1, AuthorID: 9},
		},
		users: map[uint64]model.User{},
	}
	store := &fakeCommentStore{comments: map[uint64]*model.Comment{
		1: {ID: 1, ArticleID: 999, Status: model.CommentApproved},
	}}
	svc, _ := newCommentService(articles, store, true)

	_, err := svc.Create(context.Background(), 5, 1, CommentInput{Content: "回复", ParentID: 1})
	require.Error(t, err)
	assert.Equal(t, errs.CodeParentCommentBad, errs.From(err).Code)
}

func TestCommentCreate_ReplyDerivesRoot(t *testing.T) {
	articles := &fakeCommentArticleStore{
		articles: map[uint64]*model.Article{
			1: {ID: 1, Slug: "a", Status: model.ArticlePublished, AllowComment: 1, AuthorID: 9},
		},
		users: map[uint64]model.User{9: {ID: 9, Nickname: "作者"}},
	}
	store := &fakeCommentStore{comments: map[uint64]*model.Comment{
		1: {ID: 1, ArticleID: 1, Status: model.CommentApproved},
	}}
	svc, _ := newCommentService(articles, store, false)

	c, err := svc.Create(context.Background(), 5, 1, CommentInput{Content: "回复", ParentID: 1, ReplyToUserID: 9})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), c.RootID, "回复主楼时 root_id 应为主楼 ID")
	assert.Equal(t, uint64(1), c.ParentID)
}

func TestCommentDelete_OnlyOwnerOrAdmin(t *testing.T) {
	store := &fakeCommentStore{comments: map[uint64]*model.Comment{
		1: {ID: 1, UserID: 7, ArticleID: 1},
	}}
	svc, _ := newCommentService(&fakeCommentArticleStore{articles: map[uint64]*model.Article{}, users: map[uint64]model.User{}}, store, true)

	// 他人不能删
	err := svc.Delete(context.Background(), 1, 8, 0)
	require.Error(t, err)
	assert.Equal(t, errs.CodeForbidden, errs.From(err).Code)

	// 本人可删
	require.NoError(t, svc.Delete(context.Background(), 1, 7, 0))
}
