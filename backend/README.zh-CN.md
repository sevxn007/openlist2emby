# openlist2emby 后端

[English](README.md) | [简体中文](README.zh-CN.md)

后端是面向单个 OpenList 实例的独立 Go 服务，负责 OpenList 身份绑定、Manager 身份验证、SQLite 持久化、OpenList
管理员访问、TMDB/TVDB/音乐集成、媒体库索引、Emby 兼容 API、持久化任务执行，以及 `/api/manager/*` HTTP 契约。

## 架构

依赖方向由外向内：HTTP 传输层调用应用服务；领域模块和任务模块定义行为；SQLite 或外部客户端实现所需端口。

```text
cmd/server/                     组合根与进程生命周期
internal/
  domain/                       核心模型与领域错误
  app/                          设置、配置、代理、媒体和音乐用例
  medialibrary/                 媒体库模型、索引、扫描和播放解析
  emby/                         Emby API、路由、播放及由 GORM 管理的设备状态
  task/                         任务注册表、调度器、工作线程和状态机
  repository/sqlite/             SQLite 存储及架构管理
  audio/tagger/                  进程内封面和歌词写入器
  client/music/                  音乐搜索、封面和歌词适配器
  client/openlist/               OpenList HTTP 与文件传输适配器
  client/tmdb/                   TMDB 适配器与响应映射
  client/tvdb/                   TVDB 适配器与响应映射
  metadata/                      数据源选择、优先级和查询匹配
  transport/httpapi/             路由、会话中间件、DTO 和静态文件
  config/                        命令行参数与环境配置
```

`cmd/server` 负责组装存储、客户端、应用服务、任务注册表、任务管理器和 HTTP 服务器。`internal/app` 管理请求级用例。
`internal/task` 管理持久化任务行为，不依赖 HTTP 或 SQLite。`internal/repository/sqlite` 实现应用和任务存储契约。

## OpenList 身份与本地授权

OpenList 是唯一的凭据权威来源。身份客户端封装登录、当前用户验证和管理员用户查询，设置 10 秒超时、拒绝重定向、限制响应大小并清理错误信息。OpenList
用户令牌仅在登录期间由服务端使用，不会为新会话持久化。不会存储账号密码或密码哈希。

Manager 角色（`admin`、`media`）、启用状态和媒体库授权均为本地权限，不会从上游角色复制。身份绑定使用实例 ID 和 OpenList 用户
ID，绝不使用用户名。新身份在获得明确授权之前一律拒绝访问。目录代理、任务、设置和授权管理仅限管理员使用；媒体用户只能访问获授权的媒体库及其操作。

浏览器会话采用滑动式 24 小时有效期；Emby 会话采用滑动式 30 天（30×24
小时）有效期。每次本地身份验证成功后，过期时间都会延长至当前时间加上对应有效期，但不会缩短已有的更晚过期时间。已验证的浏览器响应会刷新
HTTP-only 会话 Cookie，使其与会话期限一致。会话过期或删除后必须重新登录，绝不会恢复会话。没有后台保活，也不会刷新 OpenList
JWT。

OpenList 登录和 `/api/me` 仅在登录时建立身份。后续请求会验证本地会话类型、有效期、访问启用状态和实例绑定，并重新加载本地角色及授权，而不联系
OpenList 执行身份验证。OpenList 令牌过期或服务不可用不会使既有会话失效；但实际的上游资源操作仍可能失败，现有权限不会因此改变。OpenList
密码变更、账号删除或禁用、OTP 变更都不会自动撤销现有本地会话；如需立即撤销，请在本地禁用账号、撤销访问权限或注销设备。用户名会在下次登录时更新。浏览器登录接受
`otp_code`；Emby 登录拒绝启用双重验证的账号。设备注销会撤销关联的身份会话。不会保留密码或密码哈希；OpenList
身份验证不可用时，新登录仍会失败。身份会话保存的是本地授权和有效期状态，而不是 OpenList 用户令牌。

### 全新安装

没有设置记录时，`/api/manager/status` 会返回 `setup_required: true`。`POST /api/manager/setup` 接受：

```json
{
  "openlist_base_url": "https://openlist.example.invalid",
  "openlist_admin_key": "<administrator-key>",
  "username": "<existing-openlist-administrator>",
  "password": "<openlist-password>",
  "otp_code": "<optional-verification-code>"
}
```

集成密钥和登录账号都必须对应一个已启用的 OpenList 管理员。设置过程会以原子方式保存设置、实例身份和首个已授权管理员，但不会创建账号。重复执行设置会被拒绝。

### Manager 身份验证与授权 API

响应继续使用 `{code,message,data}` 信封。

- `POST /api/manager/auth/login`：接受 `username`、`password` 和可选的 `otp_code`。返回用户信息并设置 HTTP-only
  `openlist2emby_session` Cookie，绝不返回 OpenList 令牌。身份验证失败返回 401；缺少 openlist2emby 访问权限返回 403；上游不可用返回
  503；上游限流返回 429。
- `POST /api/manager/auth/logout`：删除会话并使 Cookie 过期。
- `GET /api/manager/users`：返回已授权用户和保留的未绑定记录，包含彼此独立的 `enabled` 状态以及上游 `active`/`disabled`/
  `unavailable`/`unbound` 状态。上游缺失的用户会标记为不可用。
- `GET /api/manager/openlist/users?page=1&per_page=50`：仅限管理员调用，分页返回 OpenList 用户白名单（`id`、`username`、
  `role`、`disabled`、`otp`），并返回 `content` 和 `total`。页大小为 1–100。上游列表可能不含 OTP 状态；只有 `/api/me`
  是登录双重验证策略的权威来源。
- `POST /api/manager/user-authorizations`：仅限管理员调用；接受 `openlist_user_id`、`role`、`enabled`、`library_grants`
  。重复绑定会被拒绝。媒体角色必须至少具有一个媒体库授权。
- `PUT /api/manager/users/{id}`：只接受 `role`、`enabled`、`library_grants`；拒绝所有未知字段，包括值为 null 的已废弃
  `username`/`password` 字段。
- `POST /api/manager/users` 和 `DELETE /api/manager/users/{id}` 是已废弃的 JSON 404 端点。账号增删改查由 OpenList 负责。

撤销本地访问权限会删除身份会话并撤销 Emby
凭据。角色或媒体库权限变更会在下一次请求时生效。不能禁用或降级最后一个仍可用且已绑定的管理员；管理员不能禁用自己的访问权限。上游暂时不可用时，会阻止依赖上游可用状态的授权更新。

## OpenList 集成访问

文件系统操作、元数据任务、扫描和播放 URL 解析使用服务端配置的管理员密钥。不会使用用户令牌来扩展普通用户的目录权限。响应仅暴露脱敏后的密钥；将秘密字段留空的更新会保留现有值。在线修改基础
URL 会被拒绝；要切换到其他实例，必须全新安装。

## SQLite 架构

默认数据库为 `./data/openlist2emby.db`。服务启动时会为全新安装初始化当前架构。请使用空数据库和空元数据目录。预发布版本之间不保证架构兼容，启动过程绝不会自动删除运行时数据。

任务子系统使用以下表：

- `tasks`：可复用的任务定义、负载快照、可选的 Cron/时区设置、`next_run_at`、所有权和软删除状态。
- `task_runs`：手动及计划任务队列项、进度、尝试次数、租约、取消状态、错误、结果和时间戳。
- `task_run_items`：有序的逐对象操作、进度、上游任务状态、安全错误和时间戳。删除所属任务运行记录时会一并删除这些行。

媒体库和 Emby 兼容功能使用以下内容：

- `settings.media_library_enabled`：显式启用功能的开关。
- `media_item_images`、`media_people` 和 `media_item_people`：本地化图片、中文人物元数据和媒体条目演职员关系。
- `media_library_roots`：每个媒体库中有序的根文件夹。
- `media_libraries` 和 `media_items`：电影、电视剧和音乐索引。
- `identity_provider`、`user_authorizations`、`sessions` 和 `user_library_grants`：身份绑定、有效 Manager 会话以及媒体库
  view/edit 授权。
- `media_user_data`：每位用户的续播位置、已观看状态、播放次数和收藏。
- `emby_tokens`：按设备存储的访问令牌哈希；`emby_state`：稳定的服务器标识符。

`emby` 包拥有自己的 SQLite 模型，并通过共享的 GORM 会话初始化上述两个 Emby 表。应用、任务、媒体库和 Emby 持久化均通过 GORM
和纯 Go SQLite 驱动完成。原生 SQLite 表达式仅用于连接 PRAGMA 设置和递归媒体条目层级查询。

SQLite 启用外键，忙等待超时为 5 秒，使用 WAL 日志模式，并且每个 Manager 进程只建立一个数据库连接。该设计面向使用本地 SQLite
文件的单个 Manager 进程，不面向多节点部署。

## 持久化任务模块

### 处理器注册表

只有编译进 Go 注册表的任务处理器可以运行。不支持任意脚本或任意 HTTP 任务。每个处理器都实现以下方法：

- `Descriptor`：任务类型名称、负载版本，以及手动执行、可调度和可取消能力。
- `Prepare`：在负载持久化之前严格验证并规范化数据。
- `Run`：通过限频进度报告器执行可取消任务。
- `PublicSummary`：提供安全的 API 视图，避免返回可能包含秘密的原始负载。

当前注册表包含三个负载版本为 1 的处理器：

- `music.embed_metadata`：支持手动执行和取消，不支持计划执行。
- `media.scan`：支持手动执行、计划执行和取消。
- `media.metadata.relocate`：支持手动执行和取消，不支持计划执行。

### 状态与投递语义

持久化状态机如下：

```text
queued -> running -> succeeded | failed | canceled
             |
             `-> queued (automatic retry or interrupted-worker recovery)
```

任务至少执行一次。任务被中断后可能会再次运行同一个已持久化负载，因此处理器必须能够容忍重复执行。一次运行通常最多尝试三次：首次执行加两次自动重试。可重试的网络故障、超时、上游
HTTP 408/429 和上游 5xx 响应的退避时间分别为 30 秒和 2 分钟。验证失败、不支持的媒体及其他不可重试错误会立即失败。

取消排队中的任务会立即生效。取消运行中的任务会记录取消请求并取消进程内运行上下文；该机制是协作式取消，不会强制终止 Go
协程或正在进行的本地写入。只有任务类型仍在注册表中时，失败或已取消的运行才可手动重试；重试会创建一条通过 `retry_of_run_id`
关联的新运行记录。

### 工作线程与租约

- 默认工作线程池包含 2 个工作线程，可配置为 1 到 32 个。
- 领取任务时会以原子方式将状态从 `queued` 改为 `running`，并分配 60 秒租约。
- 工作线程每 15 秒续租一次，并在同一时间点检查取消请求。
- 服务启动时及任务执行期间都会恢复已过期的租约。有剩余尝试次数的任务会在 30 秒后重新入队；尝试次数耗尽时任务会变为
  `failed`。
- 优雅关闭会停止接收 HTTP 请求、取消工作线程，并将活动任务释放回队列，且不消耗一次尝试次数。
- 除非阶段发生变化或进度达到 100%，否则进度写入频率最多为每秒两次。

关闭浏览器或断开 SSE 请求只会停止客户端等待，不会取消已持久化的任务。

### Cron 调度

没有 Cron 表达式的定义属于手动任务，调度器不会选取它们。任何已保存的定义都可以通过其 `/run` 端点立即排队执行一次。为手动任务定义添加
Cron 表达式和时区，会将同一条记录转换为计划任务。

计划任务使用标准五字段 Cron 表达式：

```text
minute hour day-of-month month day-of-week
```

不启用秒字段和 `@every` 表达式。每个计划还必须指定有效的 IANA 时区，例如 `UTC` 或 `Asia/Shanghai`。`next_run_at` 以 UTC
持久化。

调度器会将到期计划直接推进到当前时间之后的下一个执行点。因此，停机期间最多补执行一次。每个计划最多只能有一个运行中任务和一个排队任务；只要排队任务仍存在，后续到期触发就会合并。工作线程不会并发执行同一计划的两个任务。

禁用计划会取消其排队任务。删除计划会软删除定义并取消排队任务；已经运行的任务允许完成。

### 保留期限

终态任务默认在 30 天后删除。任务管理器启动时会执行一次清理，之后每 24 小时清理一次。任务定义会一直保留，直到被软删除。使用
`OPENLIST2EMBY_TASK_RETENTION_DAYS` 或 `-task-retention-days` 修改终态任务的保留期限。

### 音乐元数据任务

音乐元数据提供方通过共享提供方接口注册。网易云音乐、QQ
音乐和酷我目前实现了相同的搜索、封面和歌词契约。媒体库扫描会并发查询所有已注册提供方，再根据规范化后的标题、艺人和专辑匹配度对合并结果排序；得分相同时保留提供方的注册顺序。增加其他提供方无需修改扫描匹配器或
Manager 音乐响应类型。

`music.embed_metadata` 接受音乐库中的音频条目 ID。后端验证条目并解析当前 OpenList 媒体路径，然后才会保存路径、来源标识、嵌入选项和
OpenList 基础 URL 快照。任务负载绝不保存 OpenList 管理员密钥或 Manager 会话令牌。

处理器执行时会读取当前管理员密钥。如果创建任务后配置的 OpenList
地址发生变化，任务会被判定为不可重试并拒绝执行。否则，处理器会下载音频文件，将选定封面和歌词写入原生容器标签，再覆盖上传至相同路径。

`POST /api/manager/music/embed` 保持原有 JSON 和 SSE 响应兼容。内部会创建同一种持久化任务并等待其结果。请求断开不会取消任务。

### 媒体元数据迁移任务

`media.metadata.relocate` 会将本地元数据缓存迁移到“设置”中选择的目录。该任务通过现有任务引擎报告进度，并协调媒体库图片及元数据访问与迁移操作。这是文件系统操作，不是
SQLite 架构迁移。

### 媒体库扫描任务

`media.scan` 会递归索引媒体库中配置的每个 OpenList 根目录，并将内容归类为电影、电视剧或音乐。媒体库至少需要一个根目录；根目录必须是绝对路径、不能重复且不能互相包含。每次列出目录时都会要求
OpenList 刷新。在发现大型目录树期间，尚无法计算索引百分比，任务会报告累计扫描目录数和文件数；随后在索引支持的文件时报告百分比进度。

电影和电视剧扫描会解析标题、季和集，并使用配置的 TMDB 或 TVDB
数据源补充元数据。在源文件修改时间或数据源选择发生变化之前，已有条目会复用已存元数据；手动编辑过的元数据在后续扫描中保持锁定。音乐扫描会将每个音频文件作为单独歌曲索引。扫描成功后会提交新一代索引，并删除不再存在的条目。结果会记录新增、更新、删除、目录数、文件数、元数据失败数、截图成功数和截图失败数。如果电影或电视剧媒体库启用了视频截图，扫描会在元数据本地化之后增加
`screenshots` 进度阶段，并发截取画面数不超过 2。单张截图失败不会导致扫描失败。

每个电影或电视剧媒体库都可以启用 `hide_people_without_images`。启用后，Manager 和 Emby 人物响应会隐藏没有本地缓存头像（本地化模式）或没有
TMDB 头像 URL（远程模式）的演员和工作人员。此设置只改变展示，不会删除人物或关系。如果 TMDB
没有某集的静态剧照，扫描会先用所属电视剧的背景图作为该集主封面，直到后续元数据刷新提供剧照。

每个媒体库恰好拥有一个 `media.scan` 计划任务。新建媒体库会同时创建启用状态为 true、Cron 表达式为 `0 * * * *`、时区为 `UTC`
的计划；管理员可以通过媒体库 API 修改 Cron 表达式、时区或启用状态。删除媒体库会软删除计划并取消排队任务。当前架构会在创建媒体库时同时创建计划。

远程元数据源仅在扫描、搜索或显式刷新元数据时使用。中文媒体文字、人物关系、海报、背景、单集剧照和人物头像都会持久化到本地。浏览、季集列表、
`PersonIds` 和 Emby 图片请求不会联系元数据提供方。默认缓存目录为 SQLite 数据库旁的 `media` 目录（Docker 中为
`/data/media`）。

人物作品查询（`GET /api/manager/media-library/items?person_id=…` 和 Emby `PersonIds`
）使用已持久化的本地关系，不会回退到远程作品列表。电视剧或季的演职员关系返回电视剧；单集关系返回单集，但如果某人同时是该电视剧或其任一季的演员，则会抑制该人物在单集中的演员关系。明确标记为非演员的单集演职员关系仍会显示。电影关系返回电影。多个匹配人物采用
OR 语义；去重在计数、排序和分页之前完成。媒体库权限、父级范围和条目类型过滤仍严格执行：Emby 的 `IncludeItemTypes=Series`
绝不包含客串单集；`Episode` 也绝不包含电视剧或已抑制的常驻演员单集。跨层级查询时，Emby 调用方应传入 `Recursive=true`
；人物查询不会隐式扩展非递归父级范围。

Manager 单集列表条目可能包含 `series_id` 和 `series_name`；`name`
仍是单集标题。人物作品可包含电影、电视剧和单集卡片，并支持加载后续分页。缺失的本地演职员信息须使用现有元数据刷新流程补齐；打开人物详情不会触发元数据刷新。合并到演员关系中的角色遵循演员规则；不会解析角色文本来推断职责。

任务负载保存媒体库 ID 和 OpenList 基础 URL 快照，不保存管理员密钥。处理器运行时读取当前密钥；如果媒体库功能已禁用或
OpenList 地址发生变化，则拒绝执行。

## 媒体库与 Emby 兼容性

媒体库需通过 Manager 设置中的 `media_library_enabled` 显式启用，默认禁用。禁用时，Manager 媒体库调用和所有 Emby 兼容路径都会返回
HTTP 404。启用后，经过身份验证的 Manager API 可创建媒体库、浏览索引条目、启动 `media.scan` 任务，并通过 HTTP 302 重定向打开媒体。

推荐的 Emby 服务器地址为 `<openlist2emby-origin>/emby`。为兼容客户端，服务还识别 `/mediabrowser` 以及常见的无前缀 Emby
路径。Emby 客户端使用已授权的 OpenList 账号登录，但会获得专属随机设备令牌，而不是 Manager 会话 Cookie 或 OpenList
管理员密钥。数据库只存储令牌哈希；注销会撤销该令牌。

`POST /Users/{UserId}/Items/{Id}/HideFromResume?Hide=true|false`
会为当前已验证用户隐藏或恢复一个条目。该用户级标志会持久化；继续观看和续播查询会排除被隐藏的候选项，但不会清除播放进度、已观看状态、播放次数、收藏或历史记录。隐藏的下一集不会让剧集列表悄然跳到更后面的集数。

播放响应会声明使用直连串流并明确禁用转码。每次视频或音频串流请求都会通过新的 OpenList `/api/fs/get`
请求解析已索引的媒体路径，然后将客户端重定向到返回的 `raw_url`，使用 HTTP 302 和 `Cache-Control: no-store`。openlist2emby
绝不代理媒体内容，也不进行转码。

Manager 媒体库路由如下：

| 方法                   | 路径                                                           | 用途                                                                                    |
|------------------------|----------------------------------------------------------------|-----------------------------------------------------------------------------------------|
| `GET`、`POST`          | `/api/manager/media-libraries`                                 | 列出当前用户可见的媒体库或创建媒体库；管理员可通过 `include_hidden=true` 列出全部媒体库 |
| `GET`、`PUT`、`DELETE` | `/api/manager/media-libraries/{id}`                            | 读取、替换或删除单个媒体库                                                              |
| `GET`、`PUT`           | `/api/manager/media-libraries/{id}/scan-schedule`              | 读取或替换媒体库唯一的 Cron 扫描计划                                                    |
| `POST`                 | `/api/manager/media-libraries/{id}/scan`                       | 将一次 `media.scan` 运行加入队列                                                        |
| `GET`                  | `/api/manager/media-library/items`                             | 搜索或浏览已索引条目                                                                    |
| `GET`                  | `/api/manager/media-library/items/{id}`                        | 读取单个已索引条目                                                                      |
| `GET`                  | `/api/manager/media-library/continue-watching`                 | 合并实际续播条目与每部已开始电视剧动态推导出的一个下一集                                |
| `GET`                  | `/api/manager/media-library/items/{id}/next-episode`           | 为单集、季或电视剧解析下一集                                                            |
| `PUT`                  | `/api/manager/media-library/items/{id}/played`                 | 将条目标记为已观看或未观看，同时保留无关用户状态                                        |
| `PUT`                  | `/api/manager/media-library/items/{id}/resume-visibility`      | 设置 `{ "hidden": true/false }`，不清除播放进度；与 Emby 共用                           |
| `DELETE`               | `/api/manager/media-library/items/{id}/resume`                 | 仅清除实际续播位置                                                                      |
| `PUT`                  | `/api/manager/media-library/items/{id}/metadata`               | 替换手动编辑的条目元数据（仅管理员）                                                    |
| `POST`                 | `/api/manager/media-library/items/{id}/metadata/refresh`       | 使用 `metadata_source`、`tmdb_id` 和 `tvdb_id` 刷新电影或电视剧元数据（仅管理员）       |
| `PUT`                  | `/api/manager/media-library/items/{id}/images/{type}`          | 上传主图或背景图（仅管理员）                                                            |
| `POST`                 | `/api/manager/media-library/items/{id}/images/primary/capture` | 截取视频画面作为主图（仅管理员）                                                        |
| `GET`                  | `/api/manager/media-library/items/{id}/stream`                 | 解析新的 OpenList URL 并返回 HTTP 302                                                   |

媒体库创建和更新请求使用 `root_paths`，其顺序即扫描顺序。`root_path` 字段接受单个路径，并作为首个路径返回。电影和电视剧请求还可以设置
`video_screenshot_enabled` 和 `video_screenshot_percent`；创建时默认分别为 `false` 和 `10`
，更新时省略字段会保留当前值，音乐媒体库始终禁用此功能。百分比必须在 1 到 99 之间。`playback_completion_percent`
控制停止播放后将条目标记为已观看的完成阈值；默认值为 `90`，有效范围是 1 到 100，更新时省略该字段会保留当前值。

视频截图会使用 `ffprobe` 读取时长，并由 FFmpeg 在配置百分比处生成有大小限制的 JPEG。原生部署时，这两个可执行文件都必须位于
`PATH` 中。缺少它们不会阻止服务启动，但启用此功能或手动截取画面时会返回 HTTP 503。API 错误中绝不会包含签名 OpenList URL 或
FFmpeg 命令输出。

```json
{
  "name": "Movies",
  "content_type": "movies",
  "root_paths": ["/Media/Movies", "/Media/Archive"]
}
```

## 任务 API

每个任务端点都要求有效的 Manager 身份会话 Cookie。响应使用标准 Manager 信封：

```json
{
  "code": 200,
  "message": "success",
  "data": {}
}
```

| 方法     | 路径                                    | 用途                                                                                                                                                  |
|----------|-----------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------|
| `GET`    | `/api/manager/tasks/types`              | 列出已注册处理器的描述信息                                                                                                                            |
| `GET`    | `/api/manager/tasks/schedules`          | 列出未删除的任务定义及其最近一次运行；可选按 `type` 筛选                                                                                              |
| `POST`   | `/api/manager/tasks/schedules`          | 创建手动或计划任务定义；返回 HTTP 201                                                                                                                 |
| `GET`    | `/api/manager/tasks/schedules/{id}`     | 读取单个任务定义                                                                                                                                      |
| `PUT`    | `/api/manager/tasks/schedules/{id}`     | 替换单个任务定义                                                                                                                                      |
| `DELETE` | `/api/manager/tasks/schedules/{id}`     | 软删除任务定义                                                                                                                                        |
| `POST`   | `/api/manager/tasks/schedules/{id}/run` | 立即将一次运行加入队列；返回 HTTP 202                                                                                                                 |
| `GET`    | `/api/manager/tasks/runs`               | 筛选并分页返回运行历史                                                                                                                                |
| `POST`   | `/api/manager/tasks/runs`               | 将一次手动运行加入队列；返回 HTTP 202                                                                                                                 |
| `GET`    | `/api/manager/tasks/runs/{id}`          | 读取运行状态、进度、结果或安全错误                                                                                                                    |
| `DELETE` | `/api/manager/tasks/runs/completed`     | 仅管理员：删除所有页面及类型中状态为成功或已取消的运行，无视列表筛选；返回 `{ "deleted": count }`（无匹配时为零）。排队中、运行中和失败的运行会保留。 |
| `DELETE` | `/api/manager/tasks/runs/{id}`          | 删除排队中或已终止的运行；运行中时返回 HTTP 409                                                                                                       |
| `GET`    | `/api/manager/tasks/runs/{id}/items`    | 筛选并分页返回有序的逐项进度                                                                                                                          |
| `POST`   | `/api/manager/tasks/runs/{id}/cancel`   | 请求取消运行                                                                                                                                          |
| `POST`   | `/api/manager/tasks/runs/{id}/retry`    | 根据失败或已取消的运行排入一次新运行；返回 HTTP 202                                                                                                   |

运行列表筛选参数均为可选：

```http
GET /api/manager/tasks/runs?status=queued,running&type=music.embed_metadata&page=1&per_page=20
```

`page` 默认为 1，`per_page` 默认为 20，页大小上限为 100。

逐项运行筛选接受以逗号分隔的 `status` 和 `action` 值。条目响应会提供筛选后分页的总数，以及整个运行未筛选的汇总信息。

使用以下负载创建一次手动音乐元数据任务：

```json
{
  "type": "music.embed_metadata",
  "payload": {
    "item_id": 42,
    "source": "netease",
    "picture_id": "<picture-id>",
    "lyric_id": "<lyric-id>",
    "embed_cover": true,
    "embed_lyrics": true
  }
}
```

任务定义的创建和更新请求使用相同的通用结构：

```json
{
  "name": "Example schedule",
  "type": "<registered-schedulable-type>",
  "cron_expression": "0 3 * * *",
  "timezone": "Asia/Shanghai",
  "enabled": true,
  "payload": {}
}
```

对于手动任务定义，请将 `cron_expression` 和 `timezone` 设为空值，并将 `enabled` 设为 `false`。Cron 为空时，后端也会强制设置这些值。

公开的计划任务响应会返回 `parameters`，公开的运行响应会返回 `subject`；两者都来自处理器的 `PublicSummary`
。不会返回已持久化的原始负载、管理员密钥或会话令牌。

## 其他 API 区域

- 状态与设置：`/api/manager/status`、`/api/manager/setup`
- 本地会话：`/api/manager/auth/login`、`/api/manager/auth/logout` 和 `/api/manager/auth/me`
- 设置与连接测试：`/api/manager/settings*`
- TMDB 媒体操作：`/api/manager/media/*`
- 音乐搜索与嵌入式标签：`/api/manager/music/*`
- 媒体库管理与播放重定向：`/api/manager/media-libraries*` 和 `/api/manager/media-library/*`
- OpenList 文件系统代理：`/api/manager/openlist/fs/*`
    - `POST /api/manager/openlist/fs/mkdir`（仅限 Manager 管理员）：使用服务端集成密钥，将 `{ "path": "/Media/New folder" }`
      转发给 OpenList `/api/fs/mkdir`。返回现有代理 JSON 信封；不会覆盖已有内容，也不会自动重命名。

## 配置

从 `backend/` 目录运行：

| 参数                   | 环境变量                            | 默认值                    | 验证规则                 |
|------------------------|-------------------------------------|---------------------------|--------------------------|
| `-addr`                | `OPENLIST2EMBY_ADDR`                | `127.0.0.1:8091`          | Go HTTP 监听地址         |
| `-db`                  | `OPENLIST2EMBY_DB`                  | `./data/openlist2emby.db` | SQLite 路径              |
| `-static`              | -                                   | 空                        | 已存在的前端 `dist` 目录 |
| `-task-workers`        | `OPENLIST2EMBY_TASK_WORKERS`        | `2`                       | 1 到 32 之间的整数       |
| `-task-retention-days` | `OPENLIST2EMBY_TASK_RETENTION_DAYS` | `30`                      | 正整数                   |

命令行参数会覆盖由环境变量提供的默认值。相对数据库路径和静态文件路径都以进程工作目录为基准解析。

## 开发与验证

默认开发监听地址为 `http://127.0.0.1:8091`。Vite 运行于 `http://127.0.0.1:5174`，并将 `/api/manager/*` 请求代理到该监听地址。Emby
客户端使用“设置”中配置的独立监听器。

```bash
go run ./cmd/server
go test ./...
go test -race ./...
go vet ./...
```

若要从 `backend/` 目录通过同一个端口提供已构建的前端和 API：

```bash
go run ./cmd/server -static ../frontend/dist
```

HTTP 服务器会为非 API 路径提供前端资源，并在客户端路由无法匹配文件时回退到 `index.html`。

### 元数据来源与发行年份

元数据设置支持 TMDB 和 TVDB 凭据，以及有序的数据源列表（默认先 TMDB、后
TVDB）。凭据输入为空时会保留已存值；显式清除标记会删除凭据。凭据保留在服务端，响应会进行脱敏。TVDB 使用官方 API，并支持可选订阅
PIN 和 Aired Order。使用数据源前，请为项目配置已获授权的凭据。

电影和电视剧条目包含 `tmdb_id`、`tvdb_id`、`metadata_source`（`auto`、`tmdb` 或 `tvdb`）和 `resolved_metadata_source`
。自动选择使用已配置数据源中第一个无歧义的匹配结果。身份验证、网络或限流错误会终止选择，不会悄然使用其他数据源替换现有元数据。有中文时优先使用中文数据，否则回退到原始语言。

搜索支持使用分隔符附带发行年份的标题，例如 `Title (2006)`、`Title（2006）`、`Title[2006]` 或 `Title.2006`
。系统会单独搜索标题，再按发行年份筛选结果（电视剧使用首播年份）。不带标题的年份仍会作为标题查询。扫描使用相同的年份解析器。

`POST /api/manager/media-library/items/{id}/metadata/refresh` 接受可选的 `tmdb_id`、`tvdb_id` 和 `metadata_source`
。省略时会使用通过元数据编辑端点保存的 ID。仍需具备媒体库编辑授权。提供方失败时会保留现有元数据；手动图片和已锁定字段也会保留。Emby
会保留 Episode 展示类型并提供真实的提供方标识。季编号可为一至四位数字。

不支持元数据绑定写入和 `classic_short_order` 刷新参数，提交时会被拒绝。绝不会按文件顺序推断数据源
ID。架构初始化会包含数据源字段和人物身份，但不会清除运行时数据。

从 TMDB 和 TheTVDB 获取的元数据及图片仍须遵守各自的署名和许可要求。

### 图片选择

只有管理员可以编辑元数据、上传或截取图片，以及搜索或选择远程图片。媒体库授权仅允许浏览和播放；`edit` 授权不启用编辑。
`editable_library_ids` 始终为空数组。

- `GET /api/manager/media/image-sources` 按已配置的优先顺序返回 `{sources: [...]}`，不包含凭据。
- `GET /api/manager/media/images` 接受 `source`（`tmdb` 或 `tvdb`）、`type`（`movie` 或 `tv`）、`id`（电影或 **电视剧**的数据源
  ID）、`image_type`（`primary` 或 `backdrop`），以及可选的 `season`/`episode` 编号。响应为 `{images: [...]}`，每项包含
  `source`、`image_type`、`url`、`thumbnail_url`，以及可选的 `width`、`height`、`language`。没有受支持图片时返回空数组；电视剧子项请求不会回退到电视剧本身的图片。
  +- `POST /api/manager/media-library/items/{id}/images/{type}/select` 接受相同的数据源身份字段以及所选 `url`。服务端会验证该
  URL 仍在提供方候选列表中，然后通过现有资源本地化流程仅替换所选图片。JSON 信封和更新后的条目响应与现有图片上传 API 一致。

+

+编辑器会暂存远程图片选择，直到用户保存。会先刷新元数据，再选择远程图片；这样可保留已选图片而不覆盖刚刷新的文本。无需更改数据库架构。
*** End Patch
PATCH

apply_patch <<'PATCH'
*** Begin Patch
*** Update File: backend/README.md
@@

# openlist2emby Backend

+

+[English](README.md) | [简体中文](README.zh-CN.md)
*** End Patch
PATCH
