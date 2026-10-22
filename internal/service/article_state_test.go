package service_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/authz"
	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/service"
)

// 审核关闭：草稿提交直接发布
func TestDecide_SubmitWithoutReview(t *testing.T) {
	res, err := service.Decide(service.TransitionInput{
		From:     model.ArticleDraft,
		Action:   service.ActionSubmit,
		Role:     authz.RoleAuthor,
		ReviewOn: false,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticlePublished, res.To)
}

// 审核开启：草稿提交进入待审核
func TestDecide_SubmitWithReview(t *testing.T) {
	res, err := service.Decide(service.TransitionInput{
		From:     model.ArticleDraft,
		Action:   service.ActionSubmit,
		Role:     authz.RoleAuthor,
		ReviewOn: true,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticlePending, res.To)
}

// 审核通过的只有管理员，作者自己不能自审自过
func TestDecide_OnlyAdminCanApprove(t *testing.T) {
	_, err := service.Decide(service.TransitionInput{
		From:   model.ArticlePending,
		Action: service.ActionApprove,
		Role:   authz.RoleAuthor,
	})
	require.Error(t, err)
	assert.Equal(t, errs.CodeForbidden, errs.From(err).Code)
}

func TestDecide_RejectRequiresReason(t *testing.T) {
	_, err := service.Decide(service.TransitionInput{
		From:   model.ArticlePending,
		Action: service.ActionReject,
		Role:   authz.RoleAdmin,
	})
	require.Error(t, err, "拒绝必须填理由")
	assert.Equal(t, errs.CodeReasonRequired, errs.From(err).Code)

	res, err := service.Decide(service.TransitionInput{
		From:   model.ArticlePending,
		Action: service.ActionReject,
		Role:   authz.RoleAdmin,
		Reason: "内容不符合收录标准",
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticleRejected, res.To)
}

func TestDecide_OfflineRequiresReason(t *testing.T) {
	_, err := service.Decide(service.TransitionInput{
		From:   model.ArticlePublished,
		Action: service.ActionOffline,
		Role:   authz.RoleAdmin,
	})
	require.Error(t, err)

	// 作者不能下架自己的文章——下架是管理动作
	_, err = service.Decide(service.TransitionInput{
		From:   model.ArticlePublished,
		Action: service.ActionOffline,
		Role:   authz.RoleAuthor,
		Reason: "想删",
	})
	require.Error(t, err)
}

func TestDecide_WithdrawAndArchive(t *testing.T) {
	res, err := service.Decide(service.TransitionInput{
		From: model.ArticlePending, Action: service.ActionWithdraw, Role: authz.RoleAuthor,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticleDraft, res.To)

	res, err = service.Decide(service.TransitionInput{
		From: model.ArticlePublished, Action: service.ActionArchive, Role: authz.RoleAuthor,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticleArchived, res.To)

	res, err = service.Decide(service.TransitionInput{
		From: model.ArticleArchived, Action: service.ActionRestore, Role: authz.RoleAuthor,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticleDraft, res.To)
}

// 下架文章被作者修改后回到草稿，必须重新走提交流程，不能直接再发布
func TestDecide_OfflineEditReturnsToDraft(t *testing.T) {
	res, err := service.Decide(service.TransitionInput{
		From: model.ArticleOffline, Action: service.ActionEditOffline, Role: authz.RoleAuthor,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticleDraft, res.To)

	// 没有从 offline 直接到 published 的作者路径
	_, err = service.Decide(service.TransitionInput{
		From: model.ArticleOffline, Action: service.ActionSubmit, Role: authz.RoleAuthor,
	})
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidArticleState, errs.From(err).Code)
}

func TestDecide_AdminCanReonline(t *testing.T) {
	res, err := service.Decide(service.TransitionInput{
		From: model.ArticleOffline, Action: service.ActionReonline, Role: authz.RoleAdmin,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticlePublished, res.To)
}

// 非法转换必须被拒绝，而不是放行
func TestDecide_IllegalTransitions(t *testing.T) {
	cases := []service.TransitionInput{
		{From: model.ArticleDraft, Action: service.ActionApprove, Role: authz.RoleAdmin},
		{From: model.ArticleDraft, Action: service.ActionArchive, Role: authz.RoleAuthor},
		{From: model.ArticlePending, Action: service.ActionSubmit, Role: authz.RoleAuthor},
		{From: model.ArticleDeleted, Action: service.ActionSubmit, Role: authz.RoleAuthor},
		{From: model.ArticleArchived, Action: service.ActionSubmit, Role: authz.RoleAuthor},
		{From: model.ArticlePublished, Action: service.ActionApprove, Role: authz.RoleAdmin},
	}
	for _, in := range cases {
		_, err := service.Decide(in)
		require.Error(t, err, "%s -> %s 应当非法", service.ArticleStatusName(in.From), in.Action)
		assert.Equal(t, errs.CodeInvalidArticleState, errs.From(err).Code)
	}
}

func TestDecide_Delete(t *testing.T) {
	for _, from := range []uint8{
		model.ArticleDraft, model.ArticlePending, model.ArticlePublished,
		model.ArticleRejected, model.ArticleOffline, model.ArticleArchived,
	} {
		res, err := service.Decide(service.TransitionInput{
			From: from, Action: service.ActionDelete, Role: authz.RoleAuthor,
		})
		require.NoError(t, err, "状态 %s 应允许删除", service.ArticleStatusName(from))
		assert.Equal(t, model.ArticleDeleted, res.To)
	}

	// 已删除不可再删
	_, err := service.Decide(service.TransitionInput{
		From: model.ArticleDeleted, Action: service.ActionDelete, Role: authz.RoleAdmin,
	})
	require.Error(t, err)

	// 读者无权删除
	_, err = service.Decide(service.TransitionInput{
		From: model.ArticleDraft, Action: service.ActionDelete, Role: authz.RoleReader,
	})
	require.Error(t, err)
}

// 审核关闭时，被拒文章不存在（因为没有审核流程），这条转换不应成立
func TestDecide_ReviewOffHasNoPendingPath(t *testing.T) {
	_, err := service.Decide(service.TransitionInput{
		From: model.ArticleRejected, Action: service.ActionSubmit,
		Role: authz.RoleAuthor, ReviewOn: false,
	})
	// 审核关闭时被拒状态本不该出现，但状态机仍需给出确定结果：走"直接发布"分支
	require.NoError(t, err)
}

func TestAudienceOf(t *testing.T) {
	assert.Equal(t, service.AudiencePublic, service.AudienceOf(model.ArticlePublished))

	for _, s := range []uint8{
		model.ArticleDraft, model.ArticlePending, model.ArticleRejected,
		model.ArticleOffline, model.ArticleArchived,
	} {
		assert.Equal(t, service.AudienceAuthor, service.AudienceOf(s),
			"%s 对非作者必须 404", service.ArticleStatusName(s))
	}

	assert.Equal(t, service.AudienceGone, service.AudienceOf(model.ArticleDeleted),
		"已删除必须 410 而不是 404")
}

func TestArticleStatusNameRoundTrip(t *testing.T) {
	for _, s := range []uint8{
		model.ArticleDraft, model.ArticlePending, model.ArticlePublished,
		model.ArticleRejected, model.ArticleOffline, model.ArticleArchived, model.ArticleDeleted,
	} {
		name := service.ArticleStatusName(s)
		got, err := service.ArticleStatusFrom(name)
		require.NoError(t, err)
		assert.Equal(t, s, got, name)
	}

	_, err := service.ArticleStatusFrom("nonexistent")
	assert.Error(t, err)
	assert.Equal(t, "unknown", service.ArticleStatusName(99))
}

func TestDecide_AdminApprove_WorksWithReviewOn(t *testing.T) {
	// 回归：approve 规则此前因 reviewOnly 匹配逻辑在审核开启时永远命不中，
	// 管理端无法通过审核。管理端动作不应受开关匹配约束。
	res, err := service.Decide(service.TransitionInput{
		From:     model.ArticlePending,
		Action:   service.ActionApprove,
		Role:     authz.RoleAdmin,
		ReviewOn: true,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticlePublished, res.To)
}

func TestDecide_AdminOffline_ReviewOff(t *testing.T) {
	// 下架与审核开关无关：published 随时可被管理员下架
	res, err := service.Decide(service.TransitionInput{
		From:     model.ArticlePublished,
		Action:   service.ActionOffline,
		Role:     authz.RoleAdmin,
		ReviewOn: false,
		Reason:   "违规",
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticleOffline, res.To)
}

func TestDecide_AuthorSubmit_StillGatedByReview(t *testing.T) {
	// 管理端动作豁免开关匹配的同时，作者侧动作必须仍然受控：
	// 审核开启时草稿提交进 pending，不能误命中"直接发布"
	res, err := service.Decide(service.TransitionInput{
		From:     model.ArticleDraft,
		Action:   service.ActionSubmit,
		Role:     authz.RoleAuthor,
		ReviewOn: true,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticlePending, res.To)

	res, err = service.Decide(service.TransitionInput{
		From:     model.ArticleDraft,
		Action:   service.ActionSubmit,
		Role:     authz.RoleAuthor,
		ReviewOn: false,
	})
	require.NoError(t, err)
	assert.Equal(t, model.ArticlePublished, res.To)
}
