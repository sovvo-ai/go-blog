package service_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-blog/internal/config"
	"go-blog/internal/errs"
	"go-blog/internal/model"
	"go-blog/internal/repository"
	"go-blog/internal/service"
	"go-blog/internal/storage"
)

type fakeMediaStore struct {
	mu   sync.Mutex
	rows []*model.Media
	err  error
}

func (f *fakeMediaStore) Create(_ context.Context, m *model.Media) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	m.ID = uint64(len(f.rows) + 1)
	f.rows = append(f.rows, m)
	return nil
}

func (f *fakeMediaStore) FindBySHA256(_ context.Context, sum, purpose string) (*model.Media, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.rows {
		if m.SHA256 == sum && m.Purpose == purpose {
			return m, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeMediaStore) ByID(_ context.Context, id uint64) (*model.Media, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.rows {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, repository.ErrNotFound
}

type fakeAvatarUpdater struct {
	uid uint64
	url string
	err error
}

func (f *fakeAvatarUpdater) UpdateAvatar(_ context.Context, id uint64, url string) error {
	f.uid, f.url = id, url
	return f.err
}

func newMediaSvc(t *testing.T) (*service.MediaService, *storage.LocalStore, *fakeMediaStore, *fakeAvatarUpdater) {
	t.Helper()
	root := t.TempDir()
	st, err := storage.NewLocal(root, "http://test.local/media")
	require.NoError(t, err)

	media := &fakeMediaStore{}
	users := &fakeAvatarUpdater{}
	cfg := &config.Config{}
	cfg.Storage.Driver = "local"
	cfg.Storage.LocalRoot = root
	cfg.Storage.PublicBaseURL = "http://test.local/media"
	cfg.Storage.MaxImageMB = 2

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return service.NewMediaService(st, media, users, nil, cfg, log), st, media, users
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestUploadAvatar_WritesThreeSizesAndUpdatesProfile(t *testing.T) {
	svc, st, media, users := newMediaSvc(t)
	ctx := context.Background()

	res, err := svc.UploadAvatar(ctx, 7, "avatar.png", testPNG(t, 500, 300))
	require.NoError(t, err)

	// 三档齐全，URL 指向 256 档
	require.Len(t, res.Variants, 3)
	assert.Equal(t, res.Variants[256], res.URL)
	assert.Contains(t, res.URL, "/avatars/7/")
	assert.Contains(t, res.URL, "_256.png")
	assert.NotContains(t, res.URL, "C:", "URL 不能暴露磁盘路径")
	assert.NotContains(t, res.URL, "tmp")

	// 对象真的写进了存储，且能按 Key 读回
	for size, url := range res.Variants {
		key := strings.TrimPrefix(url, "http://test.local/media/")
		data, ct, err := st.Get(ctx, key)
		require.NoError(t, err, "size=%d", size)
		assert.NotEmpty(t, data)
		assert.Equal(t, "image/png", ct)

		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		require.NoError(t, err)
		assert.Equal(t, size, cfg.Width, "size=%d 的宽度应等于该档边长", size)
		assert.Equal(t, size, cfg.Height)
	}

	// 数据库只存 Key 与元数据
	require.Len(t, media.rows, 1)
	row := media.rows[0]
	assert.Equal(t, uint64(7), row.UploaderID)
	assert.Equal(t, model.MediaPurposeAvatar, row.Purpose)
	assert.True(t, strings.HasPrefix(row.ObjectKey, "avatars/7/"))
	assert.NotContains(t, row.ObjectKey, "C:", "object_key 不能是磁盘路径")
	assert.Equal(t, 500, row.Width)
	assert.Equal(t, 300, row.Height)
	require.NotNil(t, row.Variants)
	assert.Contains(t, *row.Variants, `"256"`)
	assert.Len(t, row.SHA256, 64)

	// 用户头像地址已更新为 256 档 URL
	assert.Equal(t, uint64(7), users.uid)
	assert.Equal(t, res.URL, users.url)
}

func TestUploadAvatar_RejectsNonImage(t *testing.T) {
	svc, _, media, users := newMediaSvc(t)

	_, err := svc.UploadAvatar(context.Background(), 7, "a.txt", []byte("not an image"))
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.From(err).Code)
	assert.Empty(t, media.rows, "校验失败不应写库")
	assert.Zero(t, users.uid, "校验失败不应改头像")
}

func TestUploadAvatar_RejectsOversize(t *testing.T) {
	svc, _, _, _ := newMediaSvc(t)

	// 2MB 上限，构造 3MB 数据
	big := make([]byte, 3<<20)
	_, err := svc.UploadAvatar(context.Background(), 7, "big.png", big)
	require.Error(t, err)
	assert.Equal(t, errs.CodeInvalidParam, errs.From(err).Code)
}

func TestUploadAvatar_RollsBackObjectsOnDBFailure(t *testing.T) {
	svc, st, media, _ := newMediaSvc(t)
	media.err = errors.New("db down")

	_, err := svc.UploadAvatar(context.Background(), 7, "a.png", testPNG(t, 100, 100))
	require.Error(t, err)
	assert.Equal(t, errs.CodeInternal, errs.From(err).Code)

	// 写库失败必须清理已写入的对象，否则会积累孤儿文件
	root := st.Root()
	entries := countFiles(t, root)
	assert.Zero(t, entries, "回滚后不应残留对象文件")
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	require.NoError(t, err)
	return count
}
