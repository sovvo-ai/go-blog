package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go-blog/internal/config"
	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/repository"
	"go-blog/internal/storage"
)

// MediaStore 是媒体记录的数据能力。
type MediaStore interface {
	Create(ctx context.Context, m *model.Media) error
	FindBySHA256(ctx context.Context, sum, purpose string) (*model.Media, error)
	ByID(ctx context.Context, id uint64) (*model.Media, error)
}

// ChatMembershipChecker 判断用户是否为包含指定图片消息的会话成员。
// 聊天图片（私有桶）签名 URL 的发放依据（API_SPEC 4.7）。
type ChatMembershipChecker interface {
	HasConversationMessageWithContent(ctx context.Context, userID uint64, content string) (bool, error)
}

// AvatarUpdater 只暴露"更新头像地址"这一能力，避免媒体服务拿到整个用户仓储。
type AvatarUpdater interface {
	UpdateAvatar(ctx context.Context, id uint64, url string) error
}

// AvatarResult 是头像上传结果。
type AvatarResult struct {
	MediaID  uint64
	URL      string         // 256 档地址
	Variants map[int]string // 尺寸 -> 地址
}

// MediaService 负责图片上传、处理与落库。
type MediaService struct {
	store    storage.Store
	media    MediaStore
	users    AvatarUpdater
	chatMemb ChatMembershipChecker
	cfg      *config.Config
	log      *slog.Logger
	now      func() time.Time
}

// NewMediaService 构造 MediaService。
func NewMediaService(store storage.Store, media MediaStore, users AvatarUpdater, chatMemb ChatMembershipChecker, cfg *config.Config, log *slog.Logger) *MediaService {
	return &MediaService{store: store, media: media, users: users, chatMemb: chatMemb, cfg: cfg, log: log, now: time.Now}
}

// MaxImageBytes 返回图片大小上限，供 handler 在读文件前先做一次拦截。
func (s *MediaService) MaxImageBytes() int64 {
	return int64(s.cfg.Storage.MaxImageMB) * 1024 * 1024
}

// UploadAvatar 处理头像：校验 → 生成三档 → 落存储 → 写库 → 更新用户头像地址。
func (s *MediaService) UploadAvatar(ctx context.Context, uid uint64, filename string, data []byte) (*AvatarResult, error) {
	maxBytes := int64(s.cfg.Storage.MaxImageMB) * 1024 * 1024
	processed, err := storage.ProcessAvatar(data, maxBytes)
	if err != nil {
		return nil, mapImageErr(err)
	}

	prefix, err := avatarKeyPrefix(uid, s.now())
	if err != nil {
		return nil, errs.Internal(err)
	}

	// 先全部写入存储，任一失败则回滚已写对象，避免留下孤儿文件
	keys := make(map[int]string, len(processed.Variants))
	written := make([]string, 0, len(processed.Variants))
	for _, v := range processed.Variants {
		key := prefix + "_" + strconv.Itoa(v.Size) + v.Ext
		if err := s.store.Put(ctx, key, v.Data, ""); err != nil {
			s.rollback(ctx, written)
			return nil, storageUnavailable(err)
		}
		keys[v.Size] = key
		written = append(written, key)
	}

	urls := make(map[int]string, len(keys))
	for size, key := range keys {
		urls[size] = s.store.URL(key)
	}

	variantsJSON, err := json.Marshal(keys)
	if err != nil {
		s.rollback(ctx, written)
		return nil, errs.Internal(err)
	}
	payload := string(variantsJSON)

	sum := sha256.Sum256(data)
	media := &model.Media{
		UploaderID:   uid,
		Bucket:       "local",
		ObjectKey:    keys[256],
		OriginalName: truncateRunes(filename, 255),
		MimeType:     mimeByFormat(processed.SourceFormat),
		Size:         int64(len(data)),
		Width:        processed.SourceWidth,
		Height:       processed.SourceHeight,
		SHA256:       hex.EncodeToString(sum[:]),
		Variants:     &payload,
		Purpose:      model.MediaPurposeAvatar,
		Status:       1,
	}
	if err := s.media.Create(ctx, media); err != nil {
		s.rollback(ctx, written)
		return nil, errs.Internal(err)
	}

	avatarURL := urls[256]
	if err := s.users.UpdateAvatar(ctx, uid, avatarURL); err != nil {
		// 媒体记录已写入，头像字段更新失败不致命：用户重传即可
		s.log.Error("update_avatar_url_failed",
			slog.String("uid", strconv.FormatUint(uid, 10)),
			slog.Any("error", err),
		)
		return nil, errs.Internal(err)
	}

	return &AvatarResult{MediaID: media.ID, URL: avatarURL, Variants: urls}, nil
}

// rollback 尽力删除已写入的对象。删除失败只记日志：不能因为清理失败而掩盖原始错误。
func (s *MediaService) rollback(ctx context.Context, keys []string) {
	for _, key := range keys {
		if err := s.store.Delete(ctx, key); err != nil {
			s.log.Warn("rollback_object_failed", slog.String("key", key), slog.Any("error", err))
		}
	}
}

// avatarKeyPrefix 生成形如 avatars/12/20261021T171500-a1b2c3 的前缀。
//
// 每次上传用新前缀而不是覆盖旧文件：便于 CDN/浏览器缓存与回滚，
// 旧文件由清理任务按"失引用"回收（TECH_DESIGN 10.9）。
func avatarKeyPrefix(uid uint64, now time.Time) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机后缀失败: %w", err)
	}
	return fmt.Sprintf("avatars/%d/%s-%s", uid, now.Format("20060102T150405"), hex.EncodeToString(buf)), nil
}

func mapImageErr(err error) error {
	switch {
	case err == nil:
		return nil
	case isErr(err, storage.ErrTooLarge):
		return errs.BadRequest("图片超过大小限制").WithData(fieldErrors("file", "图片超过大小限制"))
	case isErr(err, storage.ErrUnsupportedFormat):
		return errs.BadRequest("仅支持 jpg / png / gif / webp 格式").WithData(fieldErrors("file", "图片格式不支持"))
	case isErr(err, storage.ErrCorruptImage):
		return errs.BadRequest("图片已损坏，无法解析").WithData(fieldErrors("file", "图片无法解码"))
	default:
		return errs.Internal(err)
	}
}

func storageUnavailable(cause error) error {
	return errs.Unavailable(errs.CodeStorageUnavailable, "存储服务暂时不可用，请稍后重试", cause)
}

func mimeByFormat(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

func truncateRunes(s string, n int) string {
	// 只保留文件名部分，去掉客户端可能带上的目录路径
	s = filepath.Base(strings.ReplaceAll(s, "\\", "/"))
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ChatImageMaxBytes 聊天图片上限：5MB（PRD 4.1.6.1）。
const ChatImageMaxBytes = 5 << 20

// ChatImageLongEdge 聊天图片长边上限。
const ChatImageLongEdge = 1920

// ImageUploadResult 是通用图片上传结果。
type ImageUploadResult struct {
	MediaID  uint64
	URL      string // 公开桶为永久地址；chat 为换取签名地址的端点
	Width    int
	Height   int
	Variants map[string]string // V1.0 同步单档输出，恒为空 map
}

// UploadImage 通用图片上传（POST /uploads/images）。
//
// 规则（API_SPEC 4.7）：魔数判真实类型；sha256 命中已有记录直接复用；
// gif 保留原格式，其余等比压缩长边 ≤1920。purpose=chat 走私有前缀，
// 对外不暴露永久 URL。
func (s *MediaService) UploadImage(ctx context.Context, uid uint64, purpose, filename string, data []byte) (*ImageUploadResult, error) {
	switch purpose {
	case model.MediaPurposeArticle, model.MediaPurposeAvatar, model.MediaPurposeChat:
	default:
		return nil, errs.BadRequest("不支持的 purpose")
	}

	maxBytes := s.MaxImageBytes()
	if purpose == model.MediaPurposeChat {
		maxBytes = ChatImageMaxBytes
	}
	_, out, err := storage.ProcessImage(data, maxBytes, ChatImageLongEdge)
	if err != nil {
		return nil, mapImageErr(err)
	}

	// 秒传：同内容已存在则复用记录
	sum := sha256.Sum256(data)
	sumHex := hex.EncodeToString(sum[:])
	if existing, err := s.media.FindBySHA256(ctx, sumHex, purpose); err == nil {
		return &ImageUploadResult{
			MediaID:  existing.ID,
			URL:      s.uploadURLFor(existing),
			Width:    existing.Width,
			Height:   existing.Height,
			Variants: map[string]string{},
		}, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, errs.Internal(err)
	}

	prefix, perr := uploadKeyPrefix(uid, s.now(), purpose)
	if perr != nil {
		return nil, errs.Internal(perr)
	}
	key := prefix + out.Ext
	if err := s.store.Put(ctx, key, out.Data, ""); err != nil {
		return nil, storageUnavailable(err)
	}

	mimeType := "image/jpeg"
	if out.Ext == ".png" {
		mimeType = "image/png"
	} else if out.Ext == ".gif" {
		mimeType = "image/gif"
	}
	media := &model.Media{
		UploaderID:   uid,
		Bucket:       "local",
		ObjectKey:    key,
		OriginalName: truncateRunes(filename, 255),
		MimeType:     mimeType,
		Size:         int64(len(out.Data)),
		Width:        out.Width,
		Height:       out.Height,
		SHA256:       sumHex,
		Purpose:      purpose,
		Status:       1,
	}
	if err := s.media.Create(ctx, media); err != nil {
		_ = s.store.Delete(ctx, key)
		return nil, errs.Internal(err)
	}
	return &ImageUploadResult{
		MediaID:  media.ID,
		URL:      s.uploadURLFor(media),
		Width:    media.Width,
		Height:   media.Height,
		Variants: map[string]string{},
	}, nil
}

// uploadURLFor 上传响应里的 URL：
// 公开桶给永久地址；chat 不给永久地址，返回换取签名地址的端点路径。
func (s *MediaService) uploadURLFor(m *model.Media) string {
	if m.Purpose == model.MediaPurposeChat {
		return "/api/v1/media/" + strconv.FormatUint(m.ID, 10) + "/url"
	}
	return s.store.URL(m.ObjectKey)
}

// MediaAccess 换取访问地址（GET /media/{id}/url）。
//
// 公开桶直接给永久 URL（expires_at 为空）；聊天图片校验调用者
// 是包含该图片的会话成员后签发 1 小时签名 URL，非成员 403。
func (s *MediaService) MediaAccess(ctx context.Context, uid, mediaID uint64) (url string, expiresAt *time.Time, err error) {
	var m *model.Media
	m, err = s.media.ByID(ctx, mediaID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil, errs.NotFound("媒体不存在")
		}
		return "", nil, errs.Internal(err)
	}
	if m.Purpose != model.MediaPurposeChat {
		return s.store.URL(m.ObjectKey), nil, nil
	}
	if s.chatMemb == nil {
		return "", nil, errs.Internal(errors.New("chat membership checker 未装配"))
	}
	// 消息 content 存 media_id（API_SPEC 4.8 决策记录），按它判成员
	ok, err := s.chatMemb.HasConversationMessageWithContent(ctx, uid, strconv.FormatUint(m.ID, 10))
	if err != nil {
		return "", nil, errs.Internal(err)
	}
	if !ok {
		s.log.Warn("chat_media_membership_denied",
			slog.Uint64("uid", uid),
			slog.Uint64("media_id", m.ID),
			slog.String("purpose", m.Purpose),
		)
		return "", nil, errs.Forbidden("无权访问该图片")
	}
	signed, exp := s.SignChatURL(m.ObjectKey, time.Now().Add(time.Hour))
	return signed, &exp, nil
}

// ---- 聊天图片签名 URL（HMAC-SHA256，1 小时有效） ----

// SignChatURL 为 chat 前缀对象签发带过期时间的访问 URL。
// 密钥复用 jwt.secret（同为服务端持有的高熵密钥，已在配置层强制 ≥32 字符）。
func (s *MediaService) SignChatURL(key string, exp time.Time) (string, time.Time) {
	sig := chatSignature(key, exp.Unix(), s.cfg.JWT.Secret)
	u := s.store.URL(key) + "?exp=" + strconv.FormatInt(exp.Unix(), 10) + "&sig=" + sig
	return u, exp
}

// VerifyChatSignature 校验签名与有效期。Serve 对 chat 前缀对象强制调用。
func (s *MediaService) VerifyChatSignature(key string, exp int64, sig string) bool {
	if exp < time.Now().Unix() {
		return false
	}
	return hmac.Equal([]byte(chatSignature(key, exp, s.cfg.JWT.Secret)), []byte(sig))
}

func chatSignature(key string, exp int64, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%s|%d", key, exp)
	return hex.EncodeToString(mac.Sum(nil))
}

// uploadKeyPrefix 通用上传的 Key 前缀：{purpose}/{yyyy}/{mm}/{uid}/{rand}。
// chat 前缀即私有语义：Serve 只对 chat/ 前缀强制验签。
func uploadKeyPrefix(uid uint64, now time.Time, purpose string) (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成对象随机名失败: %w", err)
	}
	return fmt.Sprintf("%s/%04d/%02d/%d/%s", purpose, now.Year(), int(now.Month()), uid, hex.EncodeToString(buf)), nil
}
