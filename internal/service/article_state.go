package service

import (
	"fmt"

	"go-blog/internal/authz"
	"go-blog/internal/errs"
	"go-blog/internal/model"
)

// ArticleAction 是文章状态机的操作。
type ArticleAction string

// 状态机操作集合，与 PRD 3.2 转换表一一对应。
const (
	ActionSubmit      ArticleAction = "submit"       // 提交（发布或送审）
	ActionApprove     ArticleAction = "approve"      // 管理员审核通过
	ActionReject      ArticleAction = "reject"       // 管理员审核拒绝
	ActionWithdraw    ArticleAction = "withdraw"     // 作者撤回
	ActionArchive     ArticleAction = "archive"      // 作者归档
	ActionRestore     ArticleAction = "restore"      // 归档恢复为草稿
	ActionOffline     ArticleAction = "offline"      // 管理员下架
	ActionReonline    ArticleAction = "reonline"     // 管理员重新上架
	ActionDelete      ArticleAction = "delete"       // 软删除
	ActionEditOffline ArticleAction = "edit_offline" // 下架文章由作者修改，回到草稿
)

// transition 描述一条合法转换。
type transition struct {
	from          uint8
	action        ArticleAction
	to            uint8
	roles         []authz.Role // 允许发起的角色
	requireReason bool         // 是否必须填写理由（拒绝/下架）
	reviewOnly    bool         // 是否仅在审核开启时成立
}

// transitions 是状态机的唯一事实来源。
//
// 设计原则：只列出允许的转换，其余一律拒绝。
// "任何状态都能改成任何状态"的实现会让审核与下架流程形同虚设。
var transitions = []transition{
	// 草稿：提交即发布（审核关闭）或送审（审核开启）
	{from: model.ArticleDraft, action: ActionSubmit, to: model.ArticlePublished, roles: []authz.Role{authz.RoleAuthor}},
	{from: model.ArticleDraft, action: ActionSubmit, to: model.ArticlePending, roles: []authz.Role{authz.RoleAuthor}, reviewOnly: true},

	// 待审核：管理员通过/拒绝，作者撤回
	{from: model.ArticlePending, action: ActionApprove, to: model.ArticlePublished, roles: []authz.Role{authz.RoleAdmin}},
	{from: model.ArticlePending, action: ActionReject, to: model.ArticleRejected, roles: []authz.Role{authz.RoleAdmin}, requireReason: true},
	{from: model.ArticlePending, action: ActionWithdraw, to: model.ArticleDraft, roles: []authz.Role{authz.RoleAuthor}},

	// 已拒绝：作者改完重新走提交流程
	{from: model.ArticleRejected, action: ActionSubmit, to: model.ArticlePublished, roles: []authz.Role{authz.RoleAuthor}},
	{from: model.ArticleRejected, action: ActionSubmit, to: model.ArticlePending, roles: []authz.Role{authz.RoleAuthor}, reviewOnly: true},

	// 已发布：管理员下架、作者归档
	{from: model.ArticlePublished, action: ActionOffline, to: model.ArticleOffline, roles: []authz.Role{authz.RoleAdmin}, requireReason: true},
	{from: model.ArticlePublished, action: ActionArchive, to: model.ArticleArchived, roles: []authz.Role{authz.RoleAuthor}},

	// 下架：作者修改回到草稿重新走流程；管理员可直接重新上架
	{from: model.ArticleOffline, action: ActionEditOffline, to: model.ArticleDraft, roles: []authz.Role{authz.RoleAuthor}},
	{from: model.ArticleOffline, action: ActionReonline, to: model.ArticlePublished, roles: []authz.Role{authz.RoleAdmin}},

	// 归档：恢复为草稿
	{from: model.ArticleArchived, action: ActionRestore, to: model.ArticleDraft, roles: []authz.Role{authz.RoleAuthor}},
}

// deletedFrom 列出允许软删除的源状态。
// 已删除的文章不再可删（幂等由查询层处理，状态机不接受重复删除）。
var deletedFrom = map[uint8]bool{
	model.ArticleDraft:     true,
	model.ArticlePending:   true,
	model.ArticlePublished: true,
	model.ArticleRejected:  true,
	model.ArticleOffline:   true,
	model.ArticleArchived:  true,
}

// TransitionInput 是判定一次状态转换所需的上下文。
type TransitionInput struct {
	From         uint8
	Action       ArticleAction
	Role         authz.Role
	ReviewOn     bool   // 审核策略是否开启
	Reason       string // 拒绝/下架理由
	IsAuthorSelf bool   // 操作者是否为作者本人（资源归属校验由 service 完成）
}

// TransitionResult 是判定结果。
type TransitionResult struct {
	To            uint8
	RequireReason bool
}

// Decide 判定转换是否合法。非法转换返回业务错误，由 handler 统一输出。
//
// 权限分两层：这里校验"角色能不能做这个动作"，
// service 层还要校验"这条文章归不归你"——两者缺一不可。
func Decide(in TransitionInput) (TransitionResult, error) {
	// 删除单独处理：允许的状态多，且不区分审核开关
	if in.Action == ActionDelete {
		if !deletedFrom[in.From] {
			return TransitionResult{}, invalidState(in.From, in.Action)
		}
		if !in.Role.AtLeast(authz.RoleAuthor) {
			return TransitionResult{}, errs.Forbidden("没有权限删除文章")
		}
		return TransitionResult{To: model.ArticleDeleted}, nil
	}

	// 同一 (源状态, 操作) 可能存在两条规则，分别对应审核开启与关闭。
	// 作者侧动作必须按当前审核策略精确匹配，否则审核开启时会误命中"直接发布"分支。
	// 管理端动作（approve/reject/offline/reonline）不受开关匹配约束：
	// pending/offline 状态本身只在特定开关组合下才会出现，from 状态匹配即可。
	var chosen *transition
	for i := range transitions {
		tr := transitions[i]
		if tr.from != in.From || tr.action != in.Action {
			continue
		}
		if !isAdminAction(in.Action) && tr.reviewOnly != in.ReviewOn {
			continue
		}
		chosen = &tr
		break
	}
	if chosen == nil {
		return TransitionResult{}, invalidState(in.From, in.Action)
	}

	if !hasRole(chosen.roles, in.Role) {
		return TransitionResult{}, errs.Forbidden("当前角色不能执行该操作")
	}
	if chosen.requireReason && in.Reason == "" {
		return TransitionResult{}, errs.New(errs.CodeReasonRequired, 400, "必须填写理由")
	}
	return TransitionResult{To: chosen.to, RequireReason: chosen.requireReason}, nil
}

func hasRole(roles []authz.Role, role authz.Role) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

func invalidState(from uint8, action ArticleAction) error {
	return errs.New(errs.CodeInvalidArticleState, 409,
		fmt.Sprintf("当前状态（%s）不支持操作 %s", ArticleStatusName(from), action))
}

// ArticleStatusName 返回契约中的状态名。
func ArticleStatusName(status uint8) string {
	switch status {
	case model.ArticleDraft:
		return "draft"
	case model.ArticlePending:
		return "pending"
	case model.ArticlePublished:
		return "published"
	case model.ArticleRejected:
		return "rejected"
	case model.ArticleOffline:
		return "offline"
	case model.ArticleArchived:
		return "archived"
	case model.ArticleDeleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// ArticleStatusFrom 把契约中的状态名解析为数值。
func ArticleStatusFrom(name string) (uint8, error) {
	switch name {
	case "draft":
		return model.ArticleDraft, nil
	case "pending":
		return model.ArticlePending, nil
	case "published":
		return model.ArticlePublished, nil
	case "rejected":
		return model.ArticleRejected, nil
	case "offline":
		return model.ArticleOffline, nil
	case "archived":
		return model.ArticleArchived, nil
	case "deleted":
		return model.ArticleDeleted, nil
	default:
		return 0, fmt.Errorf("未知文章状态: %q", name)
	}
}

// ArticleAudience 描述某个状态对外的可见性（PRD 2.2.1）。
type ArticleAudience int

// 可见性分类。
const (
	AudiencePublic ArticleAudience = iota // 任何人可见
	AudienceAuthor                        // 仅作者本人可见，其他人 404
	AudienceGone                          // 已删除，任何人 410
)

// AudienceOf 返回文章状态对应的可见性策略。
func AudienceOf(status uint8) ArticleAudience {
	switch status {
	case model.ArticlePublished:
		return AudiencePublic
	case model.ArticleDeleted:
		return AudienceGone
	default:
		// 草稿、待审、被拒、下架、归档：对非作者一律 404，不泄露存在性。
		// 注意管理员在公开接口上也拿不到，只能走后台接口。
		return AudienceAuthor
	}
}

// isAdminAction 判断是否为管理端专属动作。
func isAdminAction(a ArticleAction) bool {
	switch a {
	case ActionApprove, ActionReject, ActionOffline, ActionReonline:
		return true
	}
	return false
}
