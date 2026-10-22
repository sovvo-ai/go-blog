// Package errs 定义业务错误码与业务错误类型（TECH_DESIGN 5.2 / 5.4）。
//
// 分段规则见 TECH_DESIGN 5.2，新增码必须先在文档登记再落地。
package errs

// 通用错误码：10000–10999
const (
	CodeOK              = 0
	CodeInvalidParam    = 10001
	CodeUnauthorized    = 10002
	CodeForbidden       = 10003
	CodeNotFound        = 10004
	CodeTooManyRequests = 10005
	CodeCSRFInvalid     = 10006
)

// 认证与用户错误码：20000–20999
const (
	CodeEmailTaken     = 20001 // 邮箱已注册
	CodeUsernameTaken  = 20002 // 用户名已占用
	CodeBadCredentials = 20003 // 凭证错误（账号不存在与密码错误共用）
	CodeNotVerified    = 20004 // 账号未验证
	CodeDisabled       = 20005 // 账号已禁用
	CodeAccountLocked  = 20006 // 账号已锁定
	CodeVerifyExpired  = 20007 // 验证链接失效
	CodeResetExpired   = 20008 // 重置链接失效
	CodeOldPasswordBad = 20009 // 原密码错误
)

// 文章错误码：30000–30999
const (
	CodeArticleNotFound     = 30001 // 文章不存在
	CodeInvalidArticleState = 30002 // 状态不允许此操作
	CodeSlugConflict        = 30003 // slug 冲突
	CodeEditConflict        = 30004 // 乐观锁冲突
	CodeTooManyTags         = 30005 // 标签数量超限
	CodeArticleDeleted      = 30006 // 文章已删除（410）
)

// 互动错误码：40000–40999
const (
	CodeCommentDisabled  = 40002 // 文章已关闭评论
	CodeParentCommentBad = 40003 // 父评论不合法（不存在/跨文章/未通过审核）
)

// 聊天错误码：50000–50999（TECH_DESIGN 5.2）
const (
	CodeConversationNotFound  = 50001 // 会话不存在
	CodeNotConversationMember = 50002 // 不是会话成员
	CodePeerUnavailable       = 50003 // 对方账号不可用（禁用/不存在）
	CodeConversationTooMany   = 50004 // 发起会话过于频繁（每日上限）
)

// 管理端错误码：60000–60999
const (
	CodeCannotDisableSelf = 60001 // 不能禁用自己
	CodeLastAdmin         = 60002 // 不能撤销最后一个管理员
	CodeReasonRequired    = 60003 // 审核理由必填
)

// 系统错误码：11000–11999
const (
	CodeInternal           = 11001
	CodeDBError            = 11002
	CodeCacheUnavailable   = 11003
	CodeSearchUnavailable  = 11004
	CodeStorageUnavailable = 11005
	CodeNotReady           = 11006
)
