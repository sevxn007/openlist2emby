# openlist2emby

[English](README.md) | [简体中文](README.zh-CN.md)

openlist2emby 可将一个 OpenList 实例中的文件夹映射为电影、电视剧和音乐媒体库，供 Emby
兼容客户端使用。项目包含网页界面，可用于浏览媒体库、播放、编辑元数据、管理文件、配置用户访问权限和后台任务。服务端使用 Go、React
和 SQLite 构建。

播放时，服务会解析当前 OpenList 下载地址并返回 HTTP 302 重定向。媒体流直接来自下载源；本服务不会代理或转码媒体内容。

## 功能特性

- 将多个 OpenList 文件夹索引为电影、电视剧或音乐媒体库。
- 使用可配置的 TMDB 和 TVDB 数据源搜索并刷新电影及电视剧元数据。
- 在本地缓存海报、演职员信息、歌词和元数据，供浏览时使用。
- 在网页界面或 Emby 兼容客户端中浏览和播放媒体库。
- 按用户记录播放进度、已观看状态和收藏。
- 为 OpenList 身份配置独立的角色和媒体库授权。
- 管理 OpenList 文件、批量重命名文件，以及为音乐文件嵌入封面和歌词。
- 安排媒体库扫描，并查看、取消、重试或删除后台任务。
- 可选使用 FFmpeg 截取视频画面作为电影和剧集封面。

## 使用 Docker 快速启动

在项目目录中运行：

```bash
docker compose up -d --build
```

打开 `http://127.0.0.1:8091` 并完成初始化设置，需要提供：

- OpenList 基础 URL 和管理员集成密钥。
- 一个已存在的 OpenList 管理员用户名和密码。
- 如果该账号启用了双重验证，还需提供验证码。

初始化会验证集成密钥和管理员身份，不会创建账号，也不会保存密码。如果 OpenList 运行在 Docker 宿主机上，请使用
`http://host.docker.internal:<端口>`，不要使用容器内的回环地址。

在“设置”中启用媒体库，按需配置元数据凭据，并创建包含一个或多个 OpenList 根文件夹的媒体库。在“用户”中授权用户后，他们才能登录。Emby
兼容客户端使用已授权的 OpenList 账号连接 `http://<服务器地址>:8096/emby`。Emby 登录不支持启用了双重验证的账号。

镜像名称为 `openlist2emby:latest`，并以非特权用户运行。Compose 卷 `openlist2emby-data` 会将 SQLite 数据库和媒体缓存保存在
`/data` 下。镜像中包含 FFmpeg 和 FFprobe。

### Docker 端口

`OPENLIST2EMBY_HTTP_PORT` 用于修改网页界面的宿主机端口。`OPENLIST2EMBY_EMBY_PORT` 用于发布 Emby
监听端口；修改此端口时，也要在“设置”中配置相同端口，并重新创建服务。

```bash
OPENLIST2EMBY_HTTP_PORT=9081 \
OPENLIST2EMBY_EMBY_PORT=8097 \
docker compose up -d
```

Compose 还支持 `OPENLIST2EMBY_TASK_WORKERS`（默认值 `1`）和 `OPENLIST2EMBY_TASK_RETENTION_DAYS`（默认值 `30`）。Compose
会在本地构建镜像，无需使用远程容器镜像仓库。

### 发布与更新 Docker 镜像

GitHub Actions 工作流会将多平台镜像发布到 GitHub Container Registry（GHCR）。向 `main` 分支推送代码时，会发布
`ghcr.io/<所有者>/openlist2emby:dev` 和一个对应提交的 `sha-...` 标签。发布 GitHub Release 时，会发布与 Release
标签同名的镜像；正式版本还会更新 `latest` 标签。

在服务器的 `.env` 文件中设置要部署的镜像标签：

```dotenv
OPENLIST2EMBY_IMAGE=ghcr.io/<所有者>/openlist2emby:dev
```

然后拉取镜像并重建服务，不在服务器本地构建：

```bash
docker compose pull openlist2emby
docker compose up -d --no-build openlist2emby
```

正式部署建议使用明确的版本标签，例如 `ghcr.io/<所有者>/openlist2emby:v1.2.3`，而不是 `dev` 或 `latest`。如果 GHCR
镜像包是私有的，请先在服务器使用具有读取 package 权限的 GitHub Token 登录。重建服务会保留 Compose 数据卷；升级前请先备份，不要运行
`docker compose down -v`。

## 身份验证与权限

OpenList 是唯一的凭据权威来源。openlist2emby 使用实例 ID 和不可变的 OpenList 用户 ID 绑定身份。用户名仅用于显示；密码、密码哈希和
OpenList 用户令牌均不会持久化保存。

新用户默认没有访问权限，管理员必须单独授予 `admin` 或 `media` 角色并启用访问权限。媒体用户只能查看和播放已授权的媒体库；
`edit` 授权还允许维护元数据和媒体图片。目录操作、设置、任务和用户授权仅限管理员使用，并通过服务端集成密钥执行。

浏览器会话使用 HTTP-only、SameSite=Lax 的 `openlist2emby_session` Cookie，采用滑动式 24 小时有效期。Emby 设备会话有效期为
30 天，并会在本地身份验证成功时续期。每次请求都会检查本地会话是否过期、当前访问权限、角色、媒体库授权和实例绑定。OpenList
仅在登录时验证身份，不会在后续会话请求中再次联系。上游账号发生变化不会自动撤销已建立的本地会话；如需立即退出，请撤销本地访问权限或设备会话。

初始化后，OpenList 地址不可更改。如需连接到另一个实例，必须重新安装。运行数据包含敏感的集成凭据和会话状态，请妥善保护数据库文件及其备份。

## 元数据与播放

电影和电视剧元数据设置支持 TMDB、TVDB、可选的 TVDB 订阅 PIN，以及数据源优先级。单个电影和电视剧可选择 `auto`、`tmdb` 或
`tvdb`，并保存对应服务商 ID。搜索支持包含发行年份的标题，例如 `Title (2006)`。凭据仅保存在服务端，API 响应会对其进行脱敏。

音乐扫描使用网易云音乐、QQ 音乐和酷我音乐。匹配到的歌曲元数据、封面和歌词会缓存在本地。浏览媒体库时不会查询外部元数据服务。外部服务提供的图片和元数据仍须遵守相应的署名及许可要求。

媒体库扫描会创建持久化的 `media.scan` 任务。新建媒体库默认每小时按 UTC 时间扫描一次，可在媒体库设置中修改计划。任务还支持音乐元数据嵌入（
`music.embed_metadata`）和本地元数据迁移（`media.metadata.relocate`）。关闭浏览器不会停止已提交的任务。

媒体库默认处于禁用状态。禁用后，媒体库界面会隐藏，媒体库和 Emby 端点会返回 HTTP 404。

## 开发

所需环境：

- Go 1.26.5 和 Node.js 24，版本记录在 `.tool-versions` 中。
- pnpm 11.7.0，版本记录在 `frontend/package.json` 中。
- 可访问的 OpenList 实例及其管理员集成密钥。
- 如需原生视频截图功能，需将 FFmpeg 和 FFprobe 配置在 `PATH` 中。

启动后端：

```bash
cd backend
go run ./cmd/server
```

在另一个终端启动前端：

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm dev
```

Vite 运行于 `http://127.0.0.1:5174`，并将 `/api/manager/*` 代理到 `http://127.0.0.1:8091`。如果后端使用其他地址，请在
`frontend/.env` 中设置 `VITE_OPENLIST2EMBY_BASE`；示例配置见 `frontend/.env.example`。Emby 监听器使用独立端口，并在“设置”中配置。

### 原生构建与运行

在项目根目录中运行：

```bash
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend build
go build -C backend -o openlist2emby ./cmd/server
./backend/openlist2emby \
  -addr :8091 \
  -db ./backend/data/openlist2emby.db \
  -static ./frontend/dist
```

Go 服务会在同一个监听器上提供网页界面和 Manager API。相对路径以进程工作目录为基准解析。

## 运行时配置

| 参数                   | 环境变量                            | 默认值                    | 说明                                                    |
|------------------------|-------------------------------------|---------------------------|---------------------------------------------------------|
| `-addr`                | `OPENLIST2EMBY_ADDR`                | `127.0.0.1:8091`          | HTTP 监听地址；Docker 使用 `:8091`                      |
| `-db`                  | `OPENLIST2EMBY_DB`                  | `./data/openlist2emby.db` | SQLite 数据库路径；Docker 使用 `/data/openlist2emby.db` |
| `-static`              | —                                   | 空                        | 已构建的前端目录                                        |
| `-task-workers`        | `OPENLIST2EMBY_TASK_WORKERS`        | `2`                       | 工作线程数，范围为 1 到 32                              |
| `-task-retention-days` | `OPENLIST2EMBY_TASK_RETENTION_DAYS` | `30`                      | 已完成任务的保留天数                                    |

命令行参数会覆盖环境变量提供的默认值。请在空数据库和空元数据目录中安装。当前 SQLite 架构为准；不保证兼容预发布版本的数据库。服务启动时不会自动清除现有数据。

## 仓库结构与验证

```text
openlist2emby/
├── backend/   # Go 服务、集成、SQLite 和任务引擎
└── frontend/  # React、Vite、TypeScript、Tailwind CSS 和 Radix UI
```

另请参阅[后端文档](backend/README.zh-CN.md)（[English](backend/README.md)）、[前端文档](frontend/README.zh-CN.md)（[English](frontend/README.md)）、[用户授权 API](USER_MANAGEMENT_API.md)
和 [Emby 兼容性说明](EMBY_API_COMPATIBILITY.md)。贡献指南见 [中文指南](AGENTS.zh-CN.md)（[English](AGENTS.md)）。

```bash
cd backend
go test ./...
go vet ./...
cd ../frontend
pnpm lint
pnpm test
pnpm build
```

## 许可证

本项目采用 [MIT 许可证](LICENSE)。第三方依赖与资源仍适用各自的许可证；公开发布前需完成其许可及署名要求的审查。
