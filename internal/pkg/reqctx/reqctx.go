// Package reqctx 定义请求级上下文键，供 middleware、response 共用，
// 避免这两个包互相 import 形成环。
package reqctx

// GinRequestID 是 gin.Context 中存放请求 ID 的键。
const GinRequestID = "request_id"
