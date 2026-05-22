// Package spider91 把 91porn 爬虫的产物（本地下载好的视频和封面）
// 包装成一个 drives.Drive 实现，让它跟其它网盘一样可以挂载到 catalog 上。
//
// 与其它 drive 不同的是：
//   - 数据来源不是云盘 API，而是 Python 子进程跑 spider_91porn.py 后下载到本地
//   - StreamURL 直接返回本地文件路径，由 api.handleSpider91Video 用 http.ServeFile 服务
//   - List/Stat 用于 GC 兜底（按 viewkey 列出 videos/ 目录）
package spider91

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/video-site/backend/internal/drives"
)

// Kind 是该 drive 的类型代号，写到 catalog.drives.kind。
const Kind = "spider91"

// Config 创建 Driver 所需的配置。
type Config struct {
	// ID 是 catalog 中的 drive id，driver 用它隔离每个 spider91 实例的本地目录。
	ID string
	// RootDir 是该 drive 在磁盘上的根目录，driver 会在下面创建 videos/ 和 thumbs/。
	// 一般由 backend 拼成 <data_dir>/spider91/<driveID>/。
	RootDir string
}

// Driver 实现 drives.Drive。
type Driver struct {
	id      string
	rootDir string
}

// New 构造一个 Driver。
func New(c Config) *Driver {
	return &Driver{
		id:      c.ID,
		rootDir: c.RootDir,
	}
}

// Kind 返回 "spider91"。
func (d *Driver) Kind() string { return Kind }

// ID 返回 catalog 中的 drive id。
func (d *Driver) ID() string { return d.id }

// RootID 返回根目录的逻辑 ID。spider91 没有真正的目录结构，
// 这里固定返回 "/" 占位，调用方实际不会用它去 List 子目录。
func (d *Driver) RootID() string { return "/" }

// Init 确保 rootDir/videos 和 rootDir/thumbs 存在。
func (d *Driver) Init(ctx context.Context) error {
	if strings.TrimSpace(d.rootDir) == "" {
		return errors.New("spider91: empty rootDir")
	}
	for _, sub := range []string{"videos", "thumbs"} {
		if err := os.MkdirAll(filepath.Join(d.rootDir, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// VideosDir 返回视频文件存放目录的绝对路径。
func (d *Driver) VideosDir() string { return filepath.Join(d.rootDir, "videos") }

// ThumbsDir 返回封面文件存放目录的绝对路径。
func (d *Driver) ThumbsDir() string { return filepath.Join(d.rootDir, "thumbs") }

// RootDir 返回 driver 的存储根。
func (d *Driver) RootDir() string { return d.rootDir }

// VideoPath 返回某个 viewkey 对应的视频文件绝对路径，并校验路径不会逃出 videos/ 目录。
func (d *Driver) VideoPath(fileID string) (string, error) {
	return safeJoin(d.VideosDir(), fileID)
}

// ThumbPath 返回某个 viewkey 对应的封面文件绝对路径。
func (d *Driver) ThumbPath(fileID string) (string, error) {
	return safeJoin(d.ThumbsDir(), fileID)
}

// List 列出 videos/ 目录下的视频文件，便于上层做 GC 兜底；
// dirID 当前会被忽略，spider91 没有目录树。
func (d *Driver) List(ctx context.Context, dirID string) ([]drives.Entry, error) {
	entries, err := os.ReadDir(d.VideosDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]drives.Entry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, drives.Entry{
			ID:      e.Name(),
			Name:    e.Name(),
			Size:    info.Size(),
			IsDir:   false,
			ModTime: info.ModTime(),
		})
	}
	return out, nil
}

// Stat 查询单个 viewkey 视频文件的元数据。
func (d *Driver) Stat(ctx context.Context, fileID string) (*drives.Entry, error) {
	path, err := d.VideoPath(fileID)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return &drives.Entry{
		ID:      fileID,
		Name:    fileID,
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}

// StreamURL 返回本地视频文件路径，给 ffmpeg / 上层服务使用。
// 注意：proxy.serve 不能直接处理本地路径，回放要走 api.handleSpider91Video。
// teaser/封面 worker 通过 localPreviewLink 兜底走本地文件，刚好兼容 path 形式的 URL。
func (d *Driver) StreamURL(ctx context.Context, fileID string) (*drives.StreamLink, error) {
	path, err := d.VideoPath(fileID)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() || info.Size() == 0 {
		return nil, os.ErrNotExist
	}
	return &drives.StreamLink{
		URL:     path,
		Expires: time.Now().Add(24 * time.Hour),
	}, nil
}

// Upload 不支持：上传由 crawler 自己完成，不通过 Drive 接口。
func (d *Driver) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (string, error) {
	return "", drives.ErrNotSupported
}

// EnsureDir 不支持。
func (d *Driver) EnsureDir(ctx context.Context, pathFromRoot string) (string, error) {
	return "", drives.ErrNotSupported
}

// safeJoin 把 fileID 拼到 root 下，保证最终路径不会逃出 root。
// fileID 必须是单纯的文件名（不含 / 或 .. 等组件）。
func safeJoin(root, fileID string) (string, error) {
	id := strings.TrimSpace(fileID)
	if id == "" || filepath.Base(id) != id {
		return "", errors.New("spider91: invalid file id")
	}
	if root == "" {
		return "", errors.New("spider91: empty root dir")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	pathAbs, err := filepath.Abs(filepath.Join(rootAbs, id))
	if err != nil {
		return "", err
	}
	if pathAbs != rootAbs && !strings.HasPrefix(pathAbs, rootAbs+string(os.PathSeparator)) {
		return "", errors.New("spider91: file id escapes root")
	}
	return pathAbs, nil
}

var _ drives.Drive = (*Driver)(nil)
