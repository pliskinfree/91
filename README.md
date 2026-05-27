# 视频聚合站

把夸克 / 115 / PikPak / 联通沃盘 / OneDrive 作为存储后端的视频聚合前台。按 `video-site-implementation-plan.md` 的设计实现。

- 前端：React 18 + Vite + TypeScript
- 后端：Go 1.23，SQLite（纯 Go 驱动，无 CGO），ffmpeg 生成 teaser 和封面
- 网盘接入：夸克自研 + 115driver SDK + PikPak 自研（参考 OpenList）+ wopan-sdk-go SDK + OneDrive（OpenList 在线续期 + Microsoft Graph 文件接口）
- 爬虫接入：91 爬虫（`91VideoSpider/spider_91porn.py`，由凌晨流水线触发，拉一页视频 + 封面到本地）

## 当前功能

- 前台需要登录后访问，支持首页、列表页、搜索、分类/标签筛选、分页、详情播放和相关推荐。
- 首页"随机推荐"从最近 200 个视频里随机抽 12 个展示；"最新视频"按发布时间（即视频入库时刻）倒序展示最新 12 个。从详情页返回首页时不会刷新，保持之前看到的内容。手机端首页每个板块显示 8 个视频。
- 列表页默认每页 24 个视频；选择具体标签筛选时每页显示 12 个。电脑端每行 4 个卡片，手机端每行 2 个。列表页会记住筛选、分页和滚动位置。
- 视频卡片支持封面、画质标签、时长、移动端点按预览。
- 播放页显示来源网盘类型，提供点赞、点踩、标签编辑和 **不再展示**。不再展示是全局隐藏：写入数据库后，该视频不会再出现在首页、列表、相关推荐中，详情接口也会返回 404。
- 全站支持两套主题：**暗黑 + 暖橙**（默认）和 **奶油白 + 樱花粉**，在管理后台 → 外观 切换。所有访客共用一套主题，写入 SQLite 永久保存；前端通过 `<html data-theme>` 属性热切换 CSS 变量，无需重载页面。
- 管理后台支持网盘管理、视频管理、标签管理、外观（主题）和运行时 Teaser 生成开关。
- 管理后台登录带 IP 封禁保护：同一 IP 在 30 分钟内登录失败超过 3 次会被永久封禁，封禁记录写入 SQLite。
- 视频管理支持按网盘筛选、每页 100 条分页、每个网盘的 Teaser 已生成/待生成/失败统计、单条或全量重生 teaser、编辑标题/作者/分类/标签等元数据。
- 标签管理支持创建标签并自动分类已有视频；内置规则会把常见番号污染归并到 `AV` 等系统标签，降低标签列表噪声。
- 115 生成 teaser 时会顺序取链并分段生成，降低 CDN 403 / WAF 风控导致的大量失败概率；遇到疑似风控会进入冷却并保留任务为 `pending`。
- 115 扫描会跳过名为 `影视` 的目录及其全部子目录文件；这些文件不会新增到目录、不会计入扫描统计，已入库的同源文件会在后续扫描中清理。

## 前端 UI

- 两套主题：**暗黑 + 暖橙**（默认）走深邃灰阶 + 渐变橙色主色；**奶油白 + 樱花粉**走柔和奶白底 + 樱花粉主色 + 深咖紫文本。两套都覆盖前台所有页面和管理后台。
- 主题通过 `<html data-theme>` 属性切换，所有颜色都走 `tokens.css` 里的 CSS 变量；切换不重载页面。
- 导航栏 sticky + 毛玻璃效果；手机端汉堡菜单。
- 视频卡片 hover 上浮 + 阴影 + 缩略图微缩放；手机端改为按压缩放反馈。
- 搜索框聚焦时主色发光环；标签使用圆形药丸样式。
- 后台管理：渐变品牌标识、圆角导航、卡片阴影、模态框毛玻璃背景。
- 全局自定义滚动条会跟随主题颜色。
- 只展示有实际功能的 UI 元素，无占位链接。

## 快速开始

### 环境要求

- Node.js 18+ 和 npm
- Go 1.23+
- ffmpeg 和 ffprobe（用于生成预览 teaser 和抽封面）

Windows 用户可以把 Go 和 ffmpeg 解压到 `%USERPROFILE%\tools\`，然后把 `\tools\go\bin` 和 `\tools\ffmpeg\bin` 加到 PATH 即可，不需要管理员权限。

### 运行

线上服务器以 **systemd** 守护两个进程常驻运行（推荐，开机自启 + 崩溃自愈 + 日志走 journalctl）；本地开发可用 `start.sh` 或手动启动。

#### 方式 A：systemd（生产 / 长跑）

仓库不直接提交 unit 文件；把下面两段写到 `/etc/systemd/system/` 即可。后端走预编译二进制，前端走 vite preview 提供 `dist/` 静态文件。

```ini
# /etc/systemd/system/video-site-backend.service
[Unit]
Description=Video Site Backend (Go server)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/root/myProject/91/backend
ExecStart=/root/myProject/91/backend/server
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
# spider91 / OneDrive / PikPak 等海外接口走本机 mihomo；按实际代理改
Environment=HTTPS_PROXY=http://127.0.0.1:7890
Environment=HTTP_PROXY=http://127.0.0.1:7890
Environment=NO_PROXY=127.0.0.1,localhost,::1
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
Environment=HOME=/root
LimitNOFILE=65536
StandardOutput=journal
StandardError=journal
SyslogIdentifier=video-site-backend

[Install]
WantedBy=multi-user.target
```

```ini
# /etc/systemd/system/video-site-frontend.service
[Unit]
Description=Video Site Frontend (Vite preview, serves dist/)
After=network-online.target video-site-backend.service
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/root/myProject/91
ExecStart=/usr/bin/npm run preview -- --host 0.0.0.0 --port 9191
Restart=on-failure
RestartSec=5
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
Environment=HOME=/root
Environment=NODE_ENV=production
StandardOutput=journal
StandardError=journal
SyslogIdentifier=video-site-frontend

[Install]
WantedBy=multi-user.target
```

首次部署：

```bash
# 1. 编译后端二进制 + 构建前端静态产物
cd /root/myProject/91
npm install && npm run build
cd backend && go build -o server ./cmd/server

# 2. 启用并启动两个 unit
sudo systemctl daemon-reload
sudo systemctl enable --now video-site-backend.service video-site-frontend.service
```

日常运维：

```bash
# 状态 / 重启 / 停止
systemctl status video-site-backend video-site-frontend
systemctl restart video-site-backend
systemctl stop video-site-frontend

# 实时日志
journalctl -u video-site-backend -f
journalctl -u video-site-frontend -f

# 改了 Go 代码 → 重编译 + 重启后端
cd /root/myProject/91/backend && go build -o server ./cmd/server && sudo systemctl restart video-site-backend

# 改了前端代码 → 重新 build + 重启前端
cd /root/myProject/91 && npm run build && sudo systemctl restart video-site-frontend
```

注意 systemd 模式下不要再用 `./start.sh` 来 start/stop —— 它会和 systemd 抢端口、抢进程；只在 systemd 出故障时当应急后路。

#### 方式 B：start.sh（本地开发 / 快速试跑）

```bash
npm install
./start.sh               # 前端 9191，后端 9192；默认 preview 模式（无热更新）
./start.sh --status      # 查看运行状态
./start.sh --restart     # 重启
./start.sh --stop        # 停止
```

需要前端热更新：`FRONTEND_MODE=dev ./start.sh --restart`。

也可以分两个终端手动启动：

```bash
# 前端
npm install
npm run build
npm run preview          # 监听 http://127.0.0.1:9191，无热更新

# 后端（另开终端）
cd backend
go run ./cmd/server      # 默认监听 127.0.0.1:9192，依赖已 vendor 入库，无需 go mod tidy
```

#### 启动后

首次启动后端会自动生成：

- `backend/config.yaml`（从 `config.example.yaml` 复制）
- `backend/data/video-site.db`（SQLite）
- `backend/data/previews/`（teaser 和封面本地目录）

Vite dev / preview server 都已配置把 `/api`、`/p`、`/admin/api` 反代到 `127.0.0.1:9192`。浏览器访问 `http://127.0.0.1:9191/` 进入前台，`/admin` 进入管理后台（默认 `admin` / `admin123`，请在 `backend/config.yaml` 里改）。如果本地已经存在旧的 `backend/config.yaml`，请确认 `server.listen` 与 Vite 代理端口一致。

## 目录

```
.
├─ src/                       React 前端
├─ backend/                   Go 后端（单体服务）
│  └─ vendor/                 Go 依赖全量源码，入库，支持完全离线构建
├─ 91VideoSpider/             91 爬虫脚本（Python，spider91 drive 调用）
├─ OpenList-4.2.1/            OpenList 完整源码，网盘协议对接参考
├─ tests/                     前端纯逻辑测试
├─ start.sh                   本地前后端启动脚本
├─ video-site-implementation-plan.md    完整的设计和实现记录
└─ README.md
```

### 依赖管理

所有 Go 依赖都已通过 `go mod vendor` 打包进 `backend/vendor/` 并入库。别人 clone 仓库后，**无需联网**，直接 `go run ./cmd/server` 就能编译运行。

升级依赖的流程：

```bash
cd backend
go get github.com/SheltonZhu/115driver@<新版本>
go mod tidy
go mod vendor        # 把新依赖同步到 vendor 目录
git add vendor/      # 入库
```

### `vendor-refs/` 要不要在意？

不需要。它只存 OpenList 源码作协议参考，删除或保留都不影响项目编译。

## 加一个网盘

1. 登录 `/admin` → 网盘管理 → 新建
2. 选类型（夸克 / 115 / PikPak / 沃盘 / OneDrive），填名称 + 凭证
3. 保存后会自动触发一次扫描
4. 在 `/admin/videos` 里看扫到了多少视频
5. 侧栏底部 **Teaser 生成** 开关开着，就会按配置给每个视频生成封面和多段 teaser

各网盘的凭证字段：

| 类型 | 凭证字段 | 获取方式 |
|---|---|---|
| 夸克 | `cookie` | pan.quark.cn 登录后 F12 拷 Cookie |
| 115 | `cookie` | 115.com 登录后拷 Cookie（`UID=...; CID=...; SEID=...; KID=...`） |
| PikPak | `username`、`password`，可选 `refresh_token`、`captcha_token`、`device_id`、`platform`、`disable_media_link` | 参考 OpenList PikPak driver；首次登录成功会自动回写 token |
| 沃盘 | `access_token`、`refresh_token`、可选 `family_id` | 第一版只能手动粘贴 token；后续会加扫码/短信登录 |
| OneDrive | `refresh_token`，可选 `access_token`、`api_url_address`、`region`、`is_sharepoint`、`site_id` | 按 OpenList 默认方式调用 `https://api.oplist.org/onedrive/renewapi` 在线刷新 token；`rootId` / `scanRootId` 默认填 `root`，SharePoint 需填 `is_sharepoint=true` 和 `site_id` |
| 91 爬虫 | 可选 `target_new`、`crawl_hour`、`proxy`、`python_path`、`script_path` | 详见下文「91 爬虫源」 |

## 视频播放路径

不同网盘的视频字节走法不同，这影响：用户带宽来源、客户端 IP 暴露、backend 出站流量。

| 网盘 | 用户播放 `/p/stream/<driveID>/<fileID>` | ffmpeg 生成 teaser |
|---|---|---|
| **115、PikPak** | **302 重定向直连 CDN**（视频字节不过 backend）| 进程内本地代理转发 Range 请求（加必要请求头） |
| OneDrive、夸克、沃盘 | backend 反代字节（需要后端 Cookie / Authorization 鉴权）| 同左 |
| spider91、localupload | backend 直接 ServeFile 本地文件 | 直接读本地 mp4 |

**302 直连** 意味着：
- 浏览器拿到签名 CDN URL 后自己访问，**视频字节不流经服务器**，省服务器带宽。
- 用户端到该网盘 CDN 节点的网络质量决定播放速度（国内用户对 PikPak 海外节点尤其敏感）。
- 客户端 IP 暴露给该网盘 CDN（和 OpenList 行为一致）。
- 链接有过期时间（115 / PikPak 都是几分钟到 10 分钟），超长暂停后 Range 续传 403 时刷新页面会自动重新取链。

**backend 反代** 意味着：
- 字节经过 backend 出站，受服务器带宽和 backend 的代理策略影响。
- 客户端 IP 不暴露给上游网盘。
- backend 持有的 Cookie / Authorization 才能拉到下载流（这些字段不能交给浏览器）。

**ffmpeg 走本地代理** 是 backend 内部行为：teaser worker 启动一个临时的 `127.0.0.1:<随机端口>` HTTP server，给 ffmpeg 一个本地 URL，本地代理把 Range 请求转发到 CDN 并自动加 UA / Cookie。这样：
- 签名 URL 不出现在 ffmpeg 命令行里（避免日志泄漏）
- 多段抽帧时 ffmpeg 总是访问同一个本地 URL，但每个 Range 都会被代理转换成新一次取链请求，避开 CDN 对同一签名 URL 多段并发的风控

### 115 说明

115 的下载直链对同一个 CDN URL 的多段随机读取比较敏感，尤其是大文件生成多段 teaser 时，容易出现 `403 Forbidden`、WAF 阻断、`moov atom not found` 或 `partial file`。后端对 115 做了专门处理：

- **用户播放**：和 PikPak 一样走 302 直连 115 CDN，浏览器自己请求 CDN（不经过 backend）。客户端 IP 暴露给 115 CDN，链接 `e=` 过期时刷新页面即可重新签。
- **取流优先用移动端下载接口**，失败再回退到原 chrome 下载接口（仅影响 backend 内部取链阶段，比如 ffmpeg 抽 teaser 时）。
- **生成 teaser** 不再让 ffmpeg 同时打开多个 115 直链；每个 3 秒片段单独取链、单独生成本地小片段，最后在本地 concat。
- **ffmpeg 访问 115 CDN** 会经过进程内本地代理转发 Range 请求，避免直接暴露签名 URL，并统一处理必要请求头。
- 如果 115 返回 403 / 405 / WAF 阻断 / `moov atom not found` / `partial file` 等疑似临时风控错误，当前网盘的封面/teaser worker 会进入默认 5 分钟冷却，当前任务保持 `pending`，避免继续请求导致更多失败。

管理后台的"重生失败 teaser"会把 `failed` 重置为 `pending` 并入队。一次性重生大量 115 视频仍可能触发上游风控；建议点一次后观察日志，如果出现 `transient media source error until=...`，等待冷却结束再继续，不要反复点击。

### PikPak 说明

PikPak 视频流采用 **302 重定向直连**（和 OpenList 一致）：浏览器请求 `/p/stream/<driveID>/<fileID>` 时，backend 调用 PikPak API 拿到签名直链，直接 `302 Location: <PikPak CDN URL>` 出去，视频字节走浏览器 ↔ CDN 直连，**不经过 backend**。

带来的影响：

- 服务器带宽不消耗在视频字节上，单纯做"取链"中介。
- 网盘所在 CDN 节点的可用性 / 速度直接决定播放体验：能直连 PikPak CDN 的客户端走得很快，反之则慢。这一点对国内访问尤其敏感。
- 客户端 IP 暴露给 PikPak CDN（与 OpenList 行为一致）。
- 签名链接在客户端缓存，大概 10 分钟左右过期；超长暂停后 Range 续传 403 时刷新页面会自动重新取链。

`disable_media_link` 字段的取舍仍然有效：

- 默认 `true`：用 `web_content_link` 原始下载链接，CDN 节点偏慢，但稳定。
- `false`：改用 `usage=CACHE` 返回的 media/cache 链接，通常更快（当前服务器实测原 `/p/stream` 反代模式下约 8.9 MiB/s vs `true` 模式 ~3 MiB/s），但 media/cache 节点偶尔有波动。改成 302 直连后，浏览器侧的下载速度只看本地到 PikPak CDN 的网络，与 backend 出站策略（如 sing-box TUN）无关。

teaser / 封面生成走的是 backend 内部取流路径，不受 302 重定向影响（仍由 backend 拉数据后喂给 ffmpeg）。

### OneDrive 说明

OneDrive 当前采用 OpenList 在线 API 的续期方式，不要求用户提供 Azure 应用的 `client_id` / `client_secret` / `redirect_uri`。配置时至少填 `refresh_token`；如使用 OpenList 代刷获得的 token，可把 refresh token 填到本项目。普通 OneDrive 的 `rootId` / `scanRootId` 推荐填 `root`，SharePoint 文档库需额外设置 `is_sharepoint=true` 和 `site_id`。

### 91 爬虫源

91 爬虫不是真正的网盘，而是把 `91VideoSpider/spider_91porn.py` 包装成一种 drive：每天凌晨自动跑一次脚本，从 91porn 本月最热第 1 页起翻页，跳过已经爬过的 viewkey，凑够指定数量的新视频后停止；下载视频和封面到本地，再以 `spider91` 类型的 drive 接入到现有的视频列表 / 详情 / 标签 / teaser 流水线。

**部署前置条件**：

1. 服务器装好 Python 3 + 依赖：
   ```bash
   pip install requests beautifulsoup4 lxml
   ```
2. 91porn 的 CDN 节点（cdn77.org / btc620.com 等）位于海外，国内服务器直连下载通常只有几 KB/s。**必须经过代理**，可以两种方式之一：
   - 全局：让 backend 进程能拿到 `HTTPS_PROXY` 环境变量（如 `export HTTPS_PROXY=http://127.0.0.1:7890`），然后 `./start.sh --restart`
   - 单 drive：在管理后台 spider91 drive 的 `proxy` 字段里填 `http://127.0.0.1:7890`，覆盖环境变量

   实测通过本地 mihomo HTTP 代理，下载速度约 12-15 MB/s，15 个视频（约 1.2 GB）端到端 2-3 分钟跑完。

**配置方式**：在 `/admin/drives` 新建，类型选 "91 爬虫"，所有字段都有合理默认值，可以直接保存：

| 字段 | 默认值 | 说明 |
|---|---|---|
| `target_new` | `15` | 每次爬取的新视频数。从 page 1 起翻页，跳过已知 viewkey，凑够这么多个新视频后停止 |
| `crawl_hour` | `0` | **已废弃**：旧版每天按这个整点触发爬取，现在统一由 `nightly.cron_hour` 调度（默认 01:00）。字段保留仅为兼容旧 yaml |
| `proxy` | `（空）` | 下载代理 URL，如 `http://127.0.0.1:7890`；留空时回退到 backend 进程的 `HTTPS_PROXY` 环境变量 |
| `python_path` | `python3` | 解释器路径，可填绝对路径 |
| `script_path` | （自动定位） | 脚本绝对路径；不填时从仓库结构里推断 `91VideoSpider/spider_91porn.py` |

服务启动时会自动从 `backend/` 父目录推断 `script_path`，所以正常运行 `cd backend && go run ./cmd/server` 时不需要手填。

**管理后台 UI 适配**：`spider91` 行的"状态"列显示 `已就绪`/`错误`（不会出现"未配置凭证"），"扫描根"列改成显示 `上次抓取 N 小时前`，操作里的 `重扫` 按钮变成 `立即抓取`（点击后立刻触发一次完整流程，不受 12 小时间隔约束）。

**目录结构**：

```
backend/data/spider91/<driveID>/
├─ videos/<viewkey>.mp4    # 下载下来的视频文件（后缀按直链 URL 推断）
├─ thumbs/<viewkey>.jpg    # 下载下来的封面（也会复制一份到 backend/data/previews/thumbs/）
└─ .crawl/                 # 每次爬虫输出的 JSON 和已知 viewkey 列表，带时间戳，便于排查
```

**触发逻辑**：

- 每天 `nightly.cron_hour`（默认 01:00）作为整条流水线 Phase 2 串行触发；详见上文「凌晨流水线」章节
- 管理后台点 spider91 drive 的「立即抓取」按钮：只跑该 drive 一次爬取（不连带 Phase 1 / Phase 3）
- 顶栏「立即跑全流程」按钮：跑完整 Phase 1 + 2 + 3
- `crawl_hour` 字段保留但已不参与调度判定；`last_crawl_at` 仅作 admin UI「上次抓取 N 小时前」显示用

**去重**：用 91porn 网站的 `viewkey` 作为唯一标识，配合 `videos.id = "spider91-<driveID>-<viewkey>"` 的拼接规则去重。每次爬取前 backend 会把 catalog 里已存在的 viewkey 列表写到 `.crawl/seen-<时间戳>.txt`，作为 `--seen-viewkeys-file` 传给 Python 脚本；脚本只会请求未见过 viewkey 的详情页。

**视频文件格式**：保存到磁盘时的扩展名按视频直链 URL 真实后缀决定（`.mp4` / `.webm` / `.mkv` / `.mov` / `.m4v` / `.flv` / `.avi`）；对 `.m3u8` 等流媒体清单回退到 `.mp4`。`videos.ext` 字段也会跟实际后缀保持一致。

**封面、标签和 teaser**：

- 封面直接用爬虫拿到的网站原图，不调用 ffmpeg 抽帧；入库时 `thumbnail_status` 直接置为 `ready`，封面 worker 不会处理 spider91 视频
- 所有 spider91 视频自动打 **`91porn`** 标签（`source=system`）。挂载 spider91 drive 时会自动建标签 + 给已入库的视频按 author 字段补打；新视频入库时直接带上
- teaser 走现有 ffmpeg 生成流水线（`Teaser 生成` 总开关开启时），mp4 下载完后 3-4 秒内生成

**风险和注意事项**：

- 视频直链带过期 token（`e=` 参数），爬完必须立刻下载，不能延后
- 91porn 有 Cloudflare 防护，连续访问可能触发 403；脚本内置 3-6 秒列表页延时和 2-5 秒详情页延时
- `target_new=15` 配合 page 上下文，单次任务大概要请求 15-30 个详情页（部分页面会是已爬过的 viewkey，会跳过详情页请求）；Python 阶段约 1 分钟，下载阶段在代理畅通时约 1.5 分钟
- 单条视频平均 100 MB，每天 15 个新视频约占 1.5 GB；运行一段时间后注意磁盘容量

### Spider91 → PikPak 自动迁移

只要管理后台同时挂着 spider91 drive 和 PikPak drive，spider91 爬下来的视频会按"**本地保留最近一次爬取的 15 个，更旧的上传到 PikPak**"的策略由独立 worker 处理；上传完后回放自动走 PikPak 302 直连，本地副本被删除。

- **保留策略**：每个 spider91 drive 的 `videos/` 目录按 mtime 降序，**最新 N=15 个文件被保留在本地不上传**（默认值，可通过 Migrator Config 调）；只有超过这 15 个之外的更旧文件才会被传到 PikPak。
  - 第一次爬完：本地 15 个，全部留下，PikPak 不增。
  - 第二次爬完：本地 30 个，最旧 15 个传到 PikPak + 删本地，本地剩最新 15 个。
  - 稳态：本地恒为 ≤15 个最新视频，PikPak 累积所有历史。
- **目标 PikPak drive 选择**：`spider91_upload_drive_id` 全局设置；admin 可通过 `PUT /admin/api/settings` 显式指定。**未设置时会自动选取唯一的 PikPak drive**；如果有多个 PikPak drive，必须在管理后台显式选定其中一个，否则迁移不会发生。
- **PikPak 目录**：用该 PikPak drive 的 `rootId` 作为上传父目录。建议在 PikPak Web 端预先建一个空的子目录（比如 `/91Spider/`），把这个目录的 file ID 填到 PikPak drive 的 `rootId`，这样既能让自动迁移落到这个子目录，也能让该 PikPak drive 的扫描根只看这个子目录，不会和 115 等其它网盘内容重叠。
- **PikPak 文件名**：上传时使用 `<视频标题>-<viewkey后8位>.<ext>` 格式（方案 B）。标题被 sanitize 过：去控制字符、非法字符 `/ \ : * ? " < > |` 替成空格、折叠空白、首尾去点号、按 unicode 截断 80 字符。catalog 的 `file_name` 同步更新成上传名，下次 PikPak 扫盘时按 `(file_name, size)` 也能匹配上。
- **触发节奏**：迁移作为凌晨流水线的 Phase 3 触发 —— Phase 2 spider91 爬取 + 所有新视频 teaser 生成完毕后才进入；详见上文「凌晨流水线」章节。每天最多触发一次（除非管理员点「立即跑全流程」）。触发不等于上传 —— 是否上传由"本地是否超过 15 个"决定。
- **catalog 改写**：上传成功后事务性地把视频行的 `drive_id` / `file_id` / `file_name` / `content_hash` 改成 PikPak 的；视频自身的 `id`（`spider91-<driveID>-<viewkey>`）保持不变，所以 `video_tags`、`views`、`likes`、`91porn` 标签等关联数据全部保留。改写后再次扫盘时，scanner 通过 `(content_hash)` 或 `(file_name, size)` 现成的 `findDuplicate` 兜底逻辑认出来，不会重复入库。
- **本地清理**：迁移成功立即删本地 mp4 + thumb（封面已复制到 `backend/data/previews/thumbs/`，前端展示不受影响）。每轮 worker 末尾还有一道防御性兜底 —— 扫所有本地文件，对 catalog 中 `drive_id` 已迁走但本地仍有残留的孤儿做清理（正常路径不会触发）。
- **去重 seen 文件**：crawler 每次跑前会写一份 "已知 viewkey" 文件喂给 Python 脚本，让它跳过已爬过的详情页。这个列表按 `id LIKE 'spider91-<driveID>-%'` 查（不依赖 `drive_id`），所以 spider91 视频被迁到 PikPak 后还能被认出来，**不会重复爬**。
- **失败处理**：上传失败时本地文件保留、catalog 行保持原样；下次轮询会重试。账户超额或永久错误目前没有特殊标记，watch 日志（`[spider91migrate]`）即可。
- **不开 PikPak？** 不指定 `spider91_upload_drive_id` 也不挂 PikPak drive 时，spider91 视频继续从本地服务（`/p/spider91/<videoID>`），跟以前一样工作；磁盘会持续增长，需要手动管理。

## 凌晨流水线

所有定时任务统一进一条流水线，每天 `nightly.cron_hour`（默认 1 即 01:00）触发一次：

```
01:00     ▶ NightlyJob 启动
          ├─ Phase 1  扫所有非 spider91 / 非 localupload 网盘
          │           为每个 drive 串行：scan → 检测新增视频 → 检测被删视频
          │           （本项目数据库 + 本地封面 / teaser 一并清理；扫描 errors > 0
          │            时跳过删除清理避免误删）→ 入队封面 + teaser（如该 drive
          │            的 teaser_enabled 为 true）
          │           等所有 drive 的封面 + teaser 队列 idle
          │
          ├─ Phase 2  仅当存在 spider91 drive：串行跑 91 爬虫，新视频入队 teaser
          │           等 teaser 队列 idle
          │
          └─ Phase 3  spider91 → 云盘迁移（一次完整 sweep；本地保留最新 15 个，
                      更旧的传到 spider91_upload_drive_id 指定的 PikPak 或 115）
```

**关键属性**：

- **6 小时软超时**：`nightly.max_duration` 控制单轮总耗时上限。到点时正在跑的 phase 跑完，后续 phase 不再启动 —— 不强 kill 任何 ffmpeg / 上传。
- **当天去重**：流水线启动后会把当天日期写入 `settings.nightly.last_run_date`；同一天 01:00 的下一次 tick 看到日期相同就跳过，进程崩溃 + 重启也不会重复跑。
- **失败处理**：单个 drive 扫描失败、单条 teaser 生成失败、单条迁移失败都不会阻塞流水线；都通过日志可观测，下次流水线再试。teaser `failed` 状态需管理员手动「重生失败 teaser」恢复。
- **每 drive 的 teaser 开关**：`drives.teaser_enabled` 字段在 Phase 1 / Phase 2 入队时被尊重；关闭时 teaser worker 不会被入队，封面仍然生成。
- **手动触发**：`/admin/drives` 顶栏「立即跑全流程」按钮（POST `/admin/api/jobs/nightly/run`）忽略时间窗立即跑一遍，已在跑时被 `runMu.TryLock` 丢弃。

**已不存在的旧调度**（替代关系）：

| 旧机制 | 替代为 |
|---|---|
| `scanLoop`：每 6h 在 02–07 点窗口扫一次 | Phase 1 |
| `crawlerLoop`：spider91 drive 按 `crawl_hour` 每分钟轮询 | Phase 2 |
| `Migrator.Run`：每 60 秒 + 爬完立即触发的迁移 worker | Phase 3 |

`config.yaml` 里 `scanner.interval_seconds` 字段保留但已不生效；spider91 drive 的 `crawl_hour` 凭证字段保留但已不参与调度，`last_crawl_at` 仅作 admin UI 显示「上次抓取 N 小时前」用。

## Teaser 和封面生成策略

- 封面：固定从第 5 秒抽一帧 jpg，不再为封面单独探测视频时长
- Teaser：每段固定 3 秒；30 秒以下最多 3 段，30 秒及以上固定 4 段；长视频在 20% 到 80% 区间均匀取段
- 生成的封面和 teaser 都只保存在本地 `backend/data/previews/`，不会回写到网盘；旧数据中的 `preview_file_id` 会被忽略
- 极短视频会按可容纳的完整 3 秒片段数自动降级
- 30 秒以下短视频会尽量生成多段 teaser，但只要生成到至少 1 个有效片段就会视为成功，避免短视频随机切点无有效视频流时反复失败
- 首次失败的任务标 `preview_status = failed`，不再自动重试；管理后台可手动重新生成
- 封面或 teaser 生成遇到明确频率限制（如 429）时，对应 worker 固定冷却 5 分钟。
- 服务启动或网盘重新挂载时，如果 Teaser 开关已开启，会自动把历史 `pending` 任务重新入队，避免重启后停在"待生成"。
- 115 使用顺序分段生成：每段独立取链、独立转码，最后本地拼接，避免同一 115 CDN 链接被多输入并发读取。
- OneDrive 直链生成 teaser 时可能触发 Microsoft 429 限流；后端会识别这类错误并让当前网盘进入冷却期，保留任务为 `pending`，避免连续请求触发更严重限流。
- 115 直链生成 teaser 时如果触发 403 / WAF / 截断数据等临时错误，也会让当前网盘进入冷却期，保留任务为 `pending`。
- 详见 plan 15.12 节

## 常用管理能力

- `/admin/drives`：新增/编辑/删除网盘，触发扫描。
- `/admin/videos`：按网盘查看视频、分页浏览、查看各网盘 Teaser 统计、编辑元数据、重生 teaser。
- `/admin/tags`：新增标签并自动匹配已有视频。
- 播放页：视频信息会显示来源网盘类型；"不再展示"是全局隐藏功能。当前没有恢复入口，如需恢复可直接把数据库中对应视频的 `hidden` 字段改回 `0`，后续可在管理后台补恢复 UI。

## 验证

```bash
npm run lint
npm run build
node --test tests/previewIntent.test.ts

cd backend
go test ./... -count=1
```

## 部署到 Linux

```bash
# 本机交叉编译
cd backend
GOOS=linux GOARCH=amd64 go build -o video-server ./cmd/server

# 目标服务器
sudo apt install ffmpeg
scp video-server user@host:/opt/video-site/
# 配 systemd + nginx 反代到 /、/api、/p、/admin
```

完整部署方式见 plan 15.10 节。

## 贡献

任何代码改动请保持和 `video-site-implementation-plan.md` 同步；重要的设计决策追加到第 14 节（实现备注）或第 15 节（后端）。
