package spider91

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/video-site/backend/internal/catalog"
)

// 默认 author 标签，便于在前端筛选 spider91 来源的视频。
const DefaultAuthor = "91porn"

// DefaultTag 是所有 spider91 来源视频自动打的固定标签。
// backend 启动时会调用 CreateTagAndClassify 确保 tags 表里有 source="system"
// 的对应行，避免被孤儿标签清理掉。
const DefaultTag = "91porn"

// DefaultTargetNew 是凌晨任务默认的"凑够这么多新视频"目标数。
const DefaultTargetNew = 15

// 视频下载、列表页请求的 UA 沿用爬虫脚本里那一套，避免触发 Cloudflare 风控。
const downloadUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

// CrawlerConfig 是 Crawler 的依赖注入。
type CrawlerConfig struct {
	// Driver 是已挂载的 spider91 driver；crawler 用它的 VideoPath / ThumbPath 写入文件。
	Driver *Driver
	// Catalog 用于查重和入库。
	Catalog *catalog.Catalog
	// PythonPath 是用来跑爬虫脚本的解释器，通常是 "python3"。
	PythonPath string
	// ScriptPath 是 spider_91porn.py 的绝对路径。
	ScriptPath string
	// WorkDir 是跑 Python 时的 cwd；为空表示沿用当前进程工作目录。
	WorkDir string
	// CommonThumbDir 是 backend 的 data/previews/thumbs 目录；
	// crawler 会把封面再复制一份到 <CommonThumbDir>/<videoID>.jpg，
	// 让 /p/thumb/{videoID} 路由命中本地文件。
	CommonThumbDir string
	// HTTPClient 用于下载视频和封面；为空时使用内置默认 client。
	HTTPClient *http.Client
	// ProxyURL 可选的下载代理 URL（如 "http://127.0.0.1:7890"）。
	// 不为空则用它作为 HTTP/HTTPS 代理；为空则走 http.ProxyFromEnvironment（读 HTTPS_PROXY / HTTP_PROXY / NO_PROXY）。
	// 91porn CDN 节点位于海外，国内服务器直连通常很慢，需要走代理。
	ProxyURL string
	// SpiderTimeout 限制单次爬虫脚本运行时间。
	SpiderTimeout time.Duration
	// DownloadTimeout 限制单条视频/封面下载的耗时。
	DownloadTimeout time.Duration

	// OnNewVideo 是新视频成功入库后的回调，用于触发 teaser worker。
	OnNewVideo func(v *catalog.Video)
}

// Crawler 把 Python 爬虫产出包装成 catalog 入库流程。
type Crawler struct {
	cfg CrawlerConfig
	// runMu 保证同一个 Crawler 实例不会并发跑两次。
	runMu sync.Mutex
}

// NewCrawler 构造 Crawler。
func NewCrawler(cfg CrawlerConfig) *Crawler {
	if cfg.SpiderTimeout <= 0 {
		cfg.SpiderTimeout = 15 * time.Minute
	}
	if cfg.DownloadTimeout <= 0 {
		cfg.DownloadTimeout = 30 * time.Minute
	}
	if cfg.HTTPClient == nil {
		// 选 proxy 函数：显式 ProxyURL > 环境变量 > 直连
		proxyFn := http.ProxyFromEnvironment
		if strings.TrimSpace(cfg.ProxyURL) != "" {
			if u, err := url.Parse(cfg.ProxyURL); err == nil {
				proxyFn = http.ProxyURL(u)
			} else {
				log.Printf("[spider91] invalid proxy URL %q, falling back to env: %v", cfg.ProxyURL, err)
			}
		}
		cfg.HTTPClient = &http.Client{
			// 不限制总下载时长，靠 ctx 控制；只挡 dial / handshake / header
			Timeout: 0,
			Transport: &http.Transport{
				Proxy:                 proxyFn,
				ResponseHeaderTimeout: 60 * time.Second,
				MaxIdleConns:          10,
				IdleConnTimeout:       90 * time.Second,
			},
		}
	}
	return &Crawler{cfg: cfg}
}

// CrawlResult 汇总一次 RunOnce 的结果。
type CrawlResult struct {
	// TargetNew 是本次 RunOnce 的目标新增数（来自 drive.Credentials.target_new）。
	TargetNew    int
	// TotalEntries 是 Python 输出 JSON 里的视频条数（已被 spider 端去重过的新视频）。
	TotalEntries int
	// NewVideos 是真正下载完并入库的新视频数。
	NewVideos    int
	// Skipped 是 Go 侧二次校验时发现已存在的（理论上 Python 侧已经过滤过，正常情况下应为 0）。
	Skipped      int
	// Failed 是下载或入库失败的条数。
	Failed       int
	// SeenSnapshot 调用 Python 时实际写出的已知 viewkey 数量。
	SeenSnapshot int
	StartedAt    time.Time
	FinishedAt   time.Time
	OutputJSON   string
	SeenFile     string
}

// spiderVideoEntry 对应 spider_91porn.py 输出 JSON 中的单条视频。
type spiderVideoEntry struct {
	Title    string `json:"title"`
	ThumbURL string `json:"thumb_url"`
	VideoURL string `json:"video_url"`
	Viewkey  string `json:"viewkey"`
	DetailURL string `json:"detail_url"`
}

type spiderOutput struct {
	CrawlTime    string             `json:"crawl_time"`
	PagesCrawled int                `json:"pages_crawled"`
	TotalVideos  int                `json:"total_videos"`
	Successful   int                `json:"successful"`
	Failed       int                `json:"failed"`
	Videos       []spiderVideoEntry `json:"videos"`
}

// RunOnce 执行一次"跑爬虫 → 下载 → 入库"流程：
//   1. 从 catalog 拉取本 drive 已存在的 viewkey 列表，写到临时文件
//   2. 启动 python，传 --target-new=targetNew --seen-viewkeys-file=<tmp>，
//      让 Python 从 page 1 起翻页，跳过已知 viewkey，凑够 targetNew 个新视频后退出
//   3. 解析 JSON，按顺序下载视频和封面（视频文件后缀按 URL 真实后缀决定）
//   4. 入库 + 触发 OnNewVideo 回调（让 backend 把新视频塞进 teaser worker）
//
// targetNew <= 0 会被规范化成 spider91DefaultTargetNew（15）。
func (c *Crawler) RunOnce(ctx context.Context, targetNew int) (*CrawlResult, error) {
	c.runMu.Lock()
	defer c.runMu.Unlock()

	if c.cfg.Driver == nil {
		return nil, errors.New("spider91 crawler: driver not set")
	}
	if c.cfg.Catalog == nil {
		return nil, errors.New("spider91 crawler: catalog not set")
	}
	if strings.TrimSpace(c.cfg.PythonPath) == "" || strings.TrimSpace(c.cfg.ScriptPath) == "" {
		return nil, errors.New("spider91 crawler: python_path / script_path required")
	}
	if _, err := os.Stat(c.cfg.ScriptPath); err != nil {
		return nil, fmt.Errorf("spider91 crawler: script not found: %w", err)
	}
	if targetNew <= 0 {
		targetNew = DefaultTargetNew
	}

	if err := c.cfg.Driver.Init(ctx); err != nil {
		return nil, fmt.Errorf("spider91 crawler: driver init: %w", err)
	}

	result := &CrawlResult{TargetNew: targetNew, StartedAt: time.Now()}
	defer func() { result.FinishedAt = time.Now() }()

	// 1. 准备 .crawl/ 目录 + 已知 viewkey 列表
	//
	// 关键：路径必须用绝对路径，因为 Python 子进程的 cwd 我们设成了脚本所在目录
	// （为了让 Python 用 site-packages 里的 requests 等），传相对路径会被 Python
	// 当作相对它自己的 cwd 来解释，落在错的目录下，Go 这边再回头找又找不到。
	rootDir, err := filepath.Abs(c.cfg.Driver.RootDir())
	if err != nil {
		return result, fmt.Errorf("spider91 crawler: abs root dir: %w", err)
	}
	crawlDir := filepath.Join(rootDir, ".crawl")
	if err := os.MkdirAll(crawlDir, 0o755); err != nil {
		return result, fmt.Errorf("spider91 crawler: mkdir crawl: %w", err)
	}
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	outputPath := filepath.Join(crawlDir, fmt.Sprintf("target-%d-%s.json", targetNew, timestamp))
	seenPath := filepath.Join(crawlDir, fmt.Sprintf("seen-%s.txt", timestamp))
	result.OutputJSON = outputPath
	result.SeenFile = seenPath

	seenCount, err := c.writeSeenViewkeys(ctx, seenPath)
	if err != nil {
		return result, fmt.Errorf("spider91 crawler: build seen list: %w", err)
	}
	result.SeenSnapshot = seenCount

	// 2. 跑 Python 爬虫（target_new 模式）
	if err := c.runSpiderTargetNew(ctx, targetNew, seenPath, outputPath); err != nil {
		return result, fmt.Errorf("spider91 crawler: spider run: %w", err)
	}

	// 3. 解析 JSON
	spec, err := readSpiderOutput(outputPath)
	if err != nil {
		return result, fmt.Errorf("spider91 crawler: parse output: %w", err)
	}
	result.TotalEntries = len(spec.Videos)

	// 4. 顺序处理每条；保留二次去重作防御性兜底
	for _, item := range spec.Videos {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if result.NewVideos >= targetNew {
			// Python 侧已经按 target_new 控制了输出，这里再兜底一次防止脚本表现异常
			break
		}
		viewkey := strings.TrimSpace(item.Viewkey)
		if viewkey == "" || strings.TrimSpace(item.VideoURL) == "" {
			result.Failed++
			continue
		}

		videoID := buildVideoID(c.cfg.Driver.ID(), viewkey)
		// viewkey 已经入库 → 跳过（防御性，正常 Python 端已经过滤）
		if existing, _ := c.cfg.Catalog.GetVideo(ctx, videoID); existing != nil {
			result.Skipped++
			continue
		}
		if err := c.processOne(ctx, videoID, item); err != nil {
			log.Printf("[spider91] drive=%s viewkey=%s failed: %v", c.cfg.Driver.ID(), viewkey, err)
			result.Failed++
			continue
		}
		result.NewVideos++
	}
	return result, nil
}

// writeSeenViewkeys 把当前 drive 下已入库的 viewkey 写到 path，供 Python 脚本读取。
// file_id 在 spider91 中形如 "<viewkey>.mp4" / "<viewkey>.flv"，剥掉扩展名就是 viewkey。
func (c *Crawler) writeSeenViewkeys(ctx context.Context, path string) (int, error) {
	fileIDs, err := c.cfg.Catalog.ListVideoFileIDsByDrive(ctx, c.cfg.Driver.ID())
	if err != nil {
		return 0, err
	}
	seen := make(map[string]struct{}, len(fileIDs))
	for _, fid := range fileIDs {
		fid = strings.TrimSpace(fid)
		if fid == "" {
			continue
		}
		// 剥掉扩展名得到 viewkey；同一个 viewkey 不应该重复出现，map 顺手去重
		viewkey := strings.TrimSuffix(fid, filepath.Ext(fid))
		if viewkey == "" {
			continue
		}
		seen[viewkey] = struct{}{}
	}

	tmp := path + ".part"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	for viewkey := range seen {
		if _, err := f.WriteString(viewkey + "\n"); err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return 0, err
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return len(seen), nil
}

// runSpiderTargetNew 用 --target-new + --seen-viewkeys-file 模式调起 python 子进程。
func (c *Crawler) runSpiderTargetNew(ctx context.Context, targetNew int, seenPath, outputPath string) error {
	cmdCtx, cancel := context.WithTimeout(ctx, c.cfg.SpiderTimeout)
	defer cancel()

	args := []string{
		c.cfg.ScriptPath,
		"--target-new", fmt.Sprintf("%d", targetNew),
		"--seen-viewkeys-file", seenPath,
		"--output", outputPath,
		"--no-resume",
		"--quiet",
	}
	cmd := exec.CommandContext(cmdCtx, c.cfg.PythonPath, args...)
	if c.cfg.WorkDir != "" {
		cmd.Dir = c.cfg.WorkDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	log.Printf("[spider91] drive=%s exec %s --target-new=%d --seen=%s --output=%s",
		c.cfg.Driver.ID(), c.cfg.ScriptPath, targetNew, seenPath, outputPath)
	if err := cmd.Run(); err != nil {
		return err
	}
	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("output file not produced: %w", err)
	}
	return nil
}

// processOne 处理单个 viewkey：下载视频 + 封面 + 复制封面 + 入库。
// 任一步失败会清理已写入的临时文件，不留半成品。
func (c *Crawler) processOne(ctx context.Context, videoID string, item spiderVideoEntry) error {
	viewkey := item.Viewkey
	// 视频文件后缀按直链 URL 真实后缀来定，避免直链返回的不是 mp4 时存错容器。
	videoExt := detectVideoExt(item.VideoURL)
	videoFile := viewkey + videoExt
	// 封面后缀同理，但 91porn 的封面绝大多数是 jpg；URL 提示其它格式时尊重之。
	thumbFile := viewkey + detectThumbExt(item.ThumbURL)

	videoPath, err := c.cfg.Driver.VideoPath(videoFile)
	if err != nil {
		return err
	}
	thumbPath, err := c.cfg.Driver.ThumbPath(thumbFile)
	if err != nil {
		return err
	}

	// 视频先下载（必须）；失败直接退出。
	dlCtx, cancel := context.WithTimeout(ctx, c.cfg.DownloadTimeout)
	defer cancel()

	videoSize, err := c.downloadAtomic(dlCtx, item.VideoURL, videoPath, item.DetailURL)
	if err != nil {
		return fmt.Errorf("download video: %w", err)
	}

	// 封面下载失败不致命，记录后继续，让 thumbnail worker 兜底。
	thumbReady := false
	if strings.TrimSpace(item.ThumbURL) != "" {
		if _, err := c.downloadAtomic(dlCtx, item.ThumbURL, thumbPath, item.DetailURL); err != nil {
			log.Printf("[spider91] drive=%s viewkey=%s thumb download failed: %v", c.cfg.Driver.ID(), viewkey, err)
		} else {
			thumbReady = true
		}
	}

	// 把封面复制到 backend 的标准 thumbs 目录，让 /p/thumb/{videoID} 直接命中。
	if thumbReady && c.cfg.CommonThumbDir != "" {
		if err := os.MkdirAll(c.cfg.CommonThumbDir, 0o755); err != nil {
			log.Printf("[spider91] drive=%s mkdir common thumbs: %v", c.cfg.Driver.ID(), err)
			thumbReady = false
		} else {
			dst := filepath.Join(c.cfg.CommonThumbDir, videoID+".jpg")
			if err := copyFileAtomic(thumbPath, dst); err != nil {
				log.Printf("[spider91] drive=%s viewkey=%s copy thumb to common dir: %v", c.cfg.Driver.ID(), viewkey, err)
				thumbReady = false
			}
		}
	}

	// 入库
	now := time.Now()
	v := &catalog.Video{
		ID:            videoID,
		DriveID:       c.cfg.Driver.ID(),
		FileID:        videoFile,
		FileName:      videoFile,
		Title:         strings.TrimSpace(item.Title),
		Author:        DefaultAuthor,
		// 所有 spider91 视频统一打 "91porn" 标签，便于前台筛选；
		// UpsertVideo 会在新视频插入时自动 replaceVideoTags(... source="auto", createMissing=true)。
		Tags:          []string{DefaultTag},
		Ext:           strings.TrimPrefix(videoExt, "."),
		Quality:       "HD",
		Size:          videoSize,
		PreviewStatus: "pending",
		PublishedAt:   now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if v.Title == "" {
		v.Title = viewkey
	}
	if thumbReady {
		// 设了 ThumbnailURL 后 thumb worker 会跳过这条视频，
		// 不再尝试用 ffmpeg 抽帧（封面已经是网站原图）。
		v.ThumbnailURL = "/p/thumb/" + v.ID
	}
	if err := c.cfg.Catalog.UpsertVideo(ctx, v); err != nil {
		// 入库失败 → 把刚下载的文件清理掉，避免占盘且下次还要清
		_ = os.Remove(videoPath)
		_ = os.Remove(thumbPath)
		return fmt.Errorf("upsert video: %w", err)
	}
	if thumbReady {
		// UpsertVideo 路径上 thumbnail_status 默认 'pending'，
		// 这里再补一次确保为 'ready'。
		_ = c.cfg.Catalog.UpdateVideoMeta(ctx, v.ID, catalog.VideoMetaPatch{
			ThumbnailURL:    v.ThumbnailURL,
			ThumbnailStatus: "ready",
		})
	}
	if c.cfg.OnNewVideo != nil {
		c.cfg.OnNewVideo(v)
	}
	log.Printf("[spider91] drive=%s viewkey=%s ok title=%q size=%d", c.cfg.Driver.ID(), viewkey, v.Title, v.Size)
	return nil
}

// downloadAtomic 下载 url 到 dst，先写到 dst.part 再 rename，避免半截文件。
// 返回最终文件大小。
func (c *Crawler) downloadAtomic(ctx context.Context, src, dst, referer string) (int64, error) {
	if strings.TrimSpace(src) == "" {
		return 0, errors.New("empty url")
	}
	if _, err := url.Parse(src); err != nil {
		return 0, fmt.Errorf("parse url: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", downloadUA)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("http %d", resp.StatusCode)
	}

	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return 0, closeErr
	}
	if written <= 0 {
		_ = os.Remove(tmp)
		return 0, errors.New("empty body")
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return written, nil
}

// readSpiderOutput 读取 Python 写出的 JSON。
func readSpiderOutput(path string) (*spiderOutput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out spiderOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// copyFileAtomic 把 src 复制到 dst，先写 .part 再 rename。
func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// BuildVideoID 给定 driveID + viewkey，按统一规则生成 catalog 中 videos.id。
// 与 scanner 用法一致：<kind>-<driveID>-<fileID>。
func BuildVideoID(driveID, viewkey string) string {
	return buildVideoID(driveID, viewkey)
}

func buildVideoID(driveID, viewkey string) string {
	return Kind + "-" + driveID + "-" + viewkey
}

// detectVideoExt 从直链 URL 推断视频文件后缀。
//
// 91porn 直链路径形如 https://.../mp43/xxxx.mp4?st=...，path.Ext("xxxx.mp4") = ".mp4"。
// 但任何爬虫都可能拿到 .flv / .m3u8 / 没扩展名等情况；这里维护一个白名单：
//   - .mp4 / .webm / .mkv / .mov / .m4v / .flv / .avi → 直接用
//   - .m3u8 / .ts → 是流媒体清单，不能直接当单文件视频保存，回退到 .mp4，让上层察觉到下载结果异常
//   - 其它 → .mp4 兜底
func detectVideoExt(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u == nil {
		return ".mp4"
	}
	base := path.Base(u.Path)
	ext := strings.ToLower(path.Ext(base))
	switch ext {
	case ".mp4", ".webm", ".mkv", ".mov", ".m4v", ".flv", ".avi":
		return ext
	}
	return ".mp4"
}

// detectThumbExt 从封面 URL 推断后缀。默认 .jpg。
func detectThumbExt(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u == nil {
		return ".jpg"
	}
	base := path.Base(u.Path)
	ext := strings.ToLower(path.Ext(base))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return ext
	}
	return ".jpg"
}
