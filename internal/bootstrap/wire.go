//go:build wireinject
// +build wireinject

package bootstrap

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/wire"
	"github.com/redis/go-redis/v9"

	"go-blog/internal/config"
	"go-blog/internal/handler"
	"go-blog/internal/mailer"
	"go-blog/internal/middleware"
	"go-blog/internal/pkg/authcookie"
	"go-blog/internal/pkg/jwtutil"
	"go-blog/internal/pkg/markdown"
	"go-blog/internal/pkg/ratelimit"
	"go-blog/internal/repository"
	"go-blog/internal/router"
	"go-blog/internal/search"
	"go-blog/internal/service"
	"go-blog/internal/storage"
	"go-blog/internal/worker"
	"go-blog/internal/ws"
)

// InitApp 是编译期依赖装配入口，实现由 wire 生成到 wire_gen.go。
// 修改 provider 后需重新执行 `make wire`。
func InitApp(cfg *config.Config, log *slog.Logger) (*App, func(), error) {
	wire.Build(
		// 基础设施
		NewDB,
		NewRedis,
		NewDependencyChecks,
		provideVersion,
		provideJWTManager,
		provideMailer,
		authcookie.New,
		ratelimit.New,

		// 数据访问
		repository.NewUserRepo,
		repository.NewArticleRepo,
		repository.NewMediaRepo,
		repository.NewInteractionRepo,
		repository.NewSettingRepo,
		repository.NewAdminRepo,
		repository.NewChatRepo,
		wire.Bind(new(service.UserStore), new(*repository.UserRepo)),
		wire.Bind(new(middleware.UserReader), new(*repository.UserRepo)),
		wire.Bind(new(service.ArticleStore), new(*repository.ArticleRepo)),
		wire.Bind(new(service.MediaStore), new(*repository.MediaRepo)),
		wire.Bind(new(service.AvatarUpdater), new(*repository.UserRepo)),
		wire.Bind(new(service.IndexStore), new(*repository.ArticleRepo)),
		wire.Bind(new(service.SearchBriefStore), new(*repository.ArticleRepo)),
		wire.Bind(new(service.IndexQueue), new(*worker.Queue)),
		wire.Bind(new(service.InteractionStore), new(*repository.InteractionRepo)),
		wire.Bind(new(service.InteractionArticles), new(*service.ArticleService)),
		wire.Bind(new(service.CommentStore), new(*repository.InteractionRepo)),
		wire.Bind(new(service.NotificationWriter), new(*repository.InteractionRepo)),
		wire.Bind(new(service.NotificationStore), new(*repository.InteractionRepo)),
		wire.Bind(new(service.ReportStore), new(*repository.InteractionRepo)),
		wire.Bind(new(service.AdminStore), new(*repository.AdminRepo)),
		wire.Bind(new(service.AdminInteractionStore), new(*repository.InteractionRepo)),
		wire.Bind(new(service.AdminUserReader), new(*repository.UserRepo)),
		wire.Bind(new(service.SessionRevoker), new(*service.SessionStore)),
		wire.Bind(new(service.AuthCacheInvalidator), new(*middleware.Authenticator)),
		wire.Bind(new(service.MediaURLBuilder), new(storage.Store)),
		wire.Bind(new(service.SettingReader), new(*repository.SettingRepo)),
		wire.Bind(new(service.SettingStore), new(*repository.SettingRepo)),
		wire.Bind(new(service.CommentArticleStore), new(*repository.ArticleRepo)),
		wire.Bind(new(service.ChatStore), new(*repository.ChatRepo)),
		wire.Bind(new(service.ChatUsers), new(*repository.UserRepo)),
		wire.Bind(new(service.ChatMembershipChecker), new(*repository.ChatRepo)),
		wire.Bind(new(service.WsStatusReader), new(*repository.UserRepo)),
		wire.Bind(new(service.ReportChatStore), new(*repository.ChatRepo)),
		provideSearcher,
		provideStorage,
		markdown.New,

		// 领域服务
		service.NewSessionStore,
		service.NewOneTimeTokenStore,
		service.NewAuthService,
		service.NewUserService,
		provideArticleService,
		service.NewMediaService,
		service.NewIndexService,
		service.NewSearchService,
		service.NewInteractionService,
		service.NewCommentService,
		service.NewNotificationService,
		service.NewReportService,
		service.NewAdminService,
		service.NewChatService,
		ws.NewHub,
		ws.NewTicketStore,
		wire.Bind(new(service.ChatNotifier), new(*ws.Hub)),
		wire.Bind(new(service.UserKicker), new(*ws.Hub)),
		wire.Bind(new(service.NotificationPusher), new(*ws.Hub)),
		worker.NewQueue,
		provideWorkerServer,

		// 传输层
		middleware.NewAuthenticator,
		handler.NewHealthHandler,
		handler.NewAuthHandler,
		handler.NewUserHandler,
		handler.NewArticleHandler,
		handler.NewMediaHandler,
		handler.NewSearchHandler,
		handler.NewInteractionHandler,
		handler.NewCommentHandler,
		handler.NewNotificationHandler,
		handler.NewAdminHandler,
		handler.NewChatHandler,
		handler.NewWsHandler,
		provideRouterDeps,
		router.New,

		NewApp,
	)
	return nil, nil, nil
}

func provideVersion(cfg *config.Config) string { return cfg.App.Version }

func provideJWTManager(cfg *config.Config) (*jwtutil.Manager, error) {
	return jwtutil.New(cfg.JWT.Secret, cfg.JWT.Issuer, time.Duration(cfg.JWT.AccessTTLMin)*time.Minute)
}

func provideMailer(cfg *config.Config, log *slog.Logger) (mailer.Mailer, error) {
	return mailer.New(cfg, log)
}

// provideArticleService 注入文章服务的业务开关。
// 审核策略默认关闭（PRD 3.4 / D2），阶段 5 接入管理端设置后改为读配置表。
// provideStorage 按配置返回存储驱动。
// driver=s3 在 config.Validate 阶段就会失败，这里只处理已支持的实现。
func provideStorage(cfg *config.Config) (storage.Store, error) {
	switch cfg.Storage.Driver {
	case "local":
		return storage.NewLocal(cfg.Storage.LocalRoot, cfg.Storage.PublicBaseURL)
	default:
		return nil, fmt.Errorf("未实现的存储驱动: %s", cfg.Storage.Driver)
	}
}

func provideArticleService(store service.ArticleStore, queue service.IndexQueue, md *markdown.Renderer, settings service.SettingReader, log *slog.Logger) *service.ArticleService {
	svc := service.NewArticleService(store, queue, md, false, log)
	// 审核开关实时读 settings，管理端改配置立即生效
	svc.SetSettingReader(settings)
	return svc
}

// provideSearcher 按配置返回检索实现。
// 未配置 host 时返回 Unavailable：写入静默成功、查询返回 ErrUnavailable，
// 由 SearchService 降级为"空结果 + degraded"，而不是让搜索接口报错。
func provideSearcher(cfg *config.Config) search.Searcher {
	if !cfg.Search.Enabled() {
		return search.Unavailable{}
	}
	return search.NewMeili(cfg.Search.Host, cfg.Search.APIKey, cfg.Search.Index)
}

// provideWorkerServer 在启用检索时构造 worker，否则返回 nil（不启动后台消费）。
func provideWorkerServer(
	cfg *config.Config,
	rdb *redis.Client,
	index *service.IndexService,
	log *slog.Logger,
) (*worker.Server, error) {
	if !cfg.Search.Enabled() {
		return nil, nil
	}
	srv := worker.NewServer(rdb, index, log)
	if err := srv.RegisterPeriodic(); err != nil {
		return nil, err
	}
	return srv, nil
}

func provideRouterDeps(
	health *handler.HealthHandler,
	auth *handler.AuthHandler,
	user *handler.UserHandler,
	article *handler.ArticleHandler,
	media *handler.MediaHandler,
	searchH *handler.SearchHandler,
	interaction *handler.InteractionHandler,
	comment *handler.CommentHandler,
	notification *handler.NotificationHandler,
	admin *handler.AdminHandler,
	chat *handler.ChatHandler,
	wsH *handler.WsHandler,
	authn *middleware.Authenticator,
	cookie *authcookie.Manager,
	limiter *ratelimit.Limiter,
) router.Deps {
	return router.Deps{Health: health, Auth: auth, User: user, Article: article, Media: media, Search: searchH, Interaction: interaction, Comment: comment, Notification: notification, Admin: admin, Chat: chat, Ws: wsH, Authn: authn, Cookie: cookie,
		Limiter: limiter}
}
