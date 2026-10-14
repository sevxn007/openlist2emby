# openlist2emby Emby API 兼容性清单

本文档记录面向 Vidhub 等 Emby 客户端所需的接口。`[x]` 表示已经实现并有
仓库测试覆盖，`[ ]` 表示尚未实现。后续每完成一项，都在本清单对应项前打勾。

接口来源：Emby Server REST API 4.9.5.0。当前服务同时识别 `/emby`、
`/mediabrowser` 及独立 Emby 监听端口上的无前缀路径。

## 已实现：认证与服务器发现

- [x] `GET /System/Info/Public`：返回服务器名称、版本和地址，供客户端发现服务器。
- [x] `GET|POST|HEAD /System/Ping`：服务器存活探测。
- [x] `POST /Users/AuthenticateByName`：通过 OpenList 验证已授权账号，并将 Emby 设备令牌绑定到其本地身份；不支持开启两步验证的账号。
- [x] `GET /Users/Public`：仅返回所有已启用、可登录的本地用户。
- [x] `GET /Users/{UserId}`：仅允许读取当前设备令牌所属用户的资料和能力策略。
- [x] `GET /System/Info`、`GET /System/Endpoint`：返回已认证客户端所需的服务器信息。
- [x] `POST /Sessions/Logout`：撤销当前 Emby 设备令牌。

## 已实现：媒体库浏览

- [x] `GET /Users/{UserId}/Views`：获取电影、电视剧、音乐媒体库视图。
- [x] `GET /Users/{UserId}/Items/Root`：获取虚拟媒体根目录。
- [x] `GET /Users/{UserId}/Items`：按父级、类型、搜索词和分页浏览媒体项目。
- [x] `GET /Users/{UserId}/Items/Latest`：获取最新媒体；电视剧媒体库按每部剧最新入库单集的时间倒序返回剧条目。
- [x] `GET /Users/{UserId}/Items/Resume`：与 Manager 首页使用同一份“继续观看”查询，合并用户实际播放且尚未看完的续播项目和每部已开始电视剧的动态下一集，并按剧去重；动态下一集保持真实的零播放进度。支持类型、媒体库或剧集范围及分页过滤。
- [x] 音乐媒体库根目录默认返回本地 `Audio` 歌曲列表，同时保留按艺术家和专辑筛选能力，不要求客户端先进入艺术家或专辑层级。
- [x] `GET /Artists`、`GET /Artists/AlbumArtists`：按 Emby `QueryResult_BaseItemDto` 返回当前用户可访问的本地音乐艺术家。
- [x] 列表请求显式包含 `Fields=MediaSources` 时返回音频 `MediaSources`，其中 `RunTimeTicks` 与歌曲条目的时长一致。
- [x] 音频与专辑响应返回 Emby 标准的 `AlbumId`、`AlbumPrimaryImageTag`、`ArtistItems`、`AlbumArtists`；音轨没有独立封面时使用 `PrimaryImageItemId` 和 `PrimaryImageTag` 继承专辑主图，第三方客户端可按标准关系加载本地封面。
- [x] `GET /Users/{UserId}/Items?ArtistIds=...`、`AlbumArtistIds=...`：按本地艺术家、专辑和歌曲父子关系筛选，每位歌手或专辑艺术家只返回自己的专辑与歌曲。
- [x] `GET /Users/{UserId}/Items/{Id}`、`GET /Items/{Id}`：获取单个媒体项目详情。
- [x] `GET /Users/{UserId}/Items/{Id}/Children`、`GET /Items/{Id}/Children`：获取媒体库、电视剧和季的直接子项。
- [x] `GET /Items/{Id}/People`：从本地 SQLite 返回电影、电视剧、季或单集的中文演员与职员，使用 Emby `BaseItemPerson` 字段；单集人员由本地季级演员与该集客串演员、单集职员合并并按 TMDB 人员 ID 去重，浏览请求不访问 TMDB。
- [x] `GET /Users/{UserId}/Items/{Id}`、`GET /Items/{Id}`：详情响应内返回带本地头像哈希 `PrimaryImageTag` 的 `People`；季集列表只批量读取本地人员关系，不产生 TMDB 请求。
- [x] `GET|HEAD /Persons/{Id}/Images/Primary`、`GET|HEAD /Items/{Id}/Images/Primary?tag=person:{Id}`：免鉴权返回已缓存的本地人物头像，兼容不同 Emby 客户端的人员取图方式；匿名读取不会触发 TMDB 网络请求。
- [x] `GET /Users/{UserId}/Items?PersonIds=...`、`GET /Items?PersonIds=...`：通过本地人员关系按 TMDB 人员 ID 筛选当前用户有权访问的电影和电视剧；季或单集关系会归并到顶层电视剧。
- [x] `GET /Shows/{Id}/Seasons`、`GET /Shows/{Id}/Episodes`：获取电视剧季和剧集层级。
- [x] `GET /Shows/NextUp[?SeriesId={Id}]`：从每部已开始电视剧中，按最高已看季集之后的首个未看单集实时派生下一集，支持跨季和扫描后新增单集，并排除第 0 季特别篇。支持 `UserId`、`SeriesId`、`ParentId`（包括媒体库 View ID）、`StartIndex`、`Limit`、`NextUpDateCutoff`、`EnableResumable`、`EnableRewatching` 和 `EnableTotalRecordCount`；全局结果按最近观看时间排序，重看模式从最近按日期观看的单集继续。
- [x] `GET /Search/Hints`：提供客户端搜索提示。
- [x] `GET|HEAD /Items/{Id}/Images/{Type}`、`GET|HEAD /Items/{Id}/Images/{Type}/0`：免鉴权返回本地海报、背景或剧照，支持 `MaxWidth`、`MaxHeight`、`Width`、`Height`、`Quality`、`Format` 和可选内容哈希 `Tag`，并提供 ETag、条件缓存及派生缩略图缓存；错误 `tag` 返回 404，缺失背景或单集图片时回退到可用主海报。
- [x] 媒体库拼图及其缩略图按内容哈希持久化缓存，命中时不再读取源海报，同一封面的并发请求共用一次生成。缓存目录中的 `library-cover-sources.json` 记录源图片哈希，启动、扫描及图片维护时保留源图片仍被引用的拼图；源图片不再被引用时清理对应缓存。
- [x] `GET /Items/{Id}/Images`：按 Emby `ImageInfo` 结构返回当前条目的本地图片类型、序号、尺寸和文件大小，并继续要求用户鉴权。

## 已实现：多用户授权与状态隔离

- [x] Emby 用户 ID 使用本地身份的稳定 ID。
- [x] 每个 Emby 设备令牌只可访问其所属用户路径；不匹配的 `{UserId}` 返回 404。
- [x] 媒体库视图、浏览、搜索、图片、播放信息与流地址均按当前用户的媒体库授权过滤；未授权项目返回 404。
- [x] 继续观看、已观看、播放次数、收藏和续播位置按用户隔离。
- [x] 禁用本地访问时撤销对应身份会话与 Emby 设备令牌；本地角色和媒体库授权修改在后续请求生效。OpenList 密码、账号状态或两步验证变化不会自动撤销已有会话。

## 已实现：播放与会话

- [x] `GET|POST /Items/{Id}/PlaybackInfo` 及用户路径下的 `PlaybackInfo`：返回直连播放信息。
- [x] `GET|HEAD /Videos/{Id}/stream*`、`GET|HEAD /Audio/{Id}/stream*`：解析 OpenList 地址并返回 302 直连。
- [x] `GET /Audio/{Id}/Lyrics`：将 SQLite 中本地保存的原歌词或翻译歌词按 Emby 时间轴歌词结构返回，浏览和播放期间不访问外部音乐服务。
- [x] `POST /Sessions/Playing`：为当前 Emby 用户记录会话开始播放。
- [x] `POST /Sessions/Playing/Progress`：为当前 Emby 用户记录播放进度。
- [x] `POST /Sessions/Playing/Stopped`：为当前 Emby 用户记录停止播放并按完成度标记已观看。
- [x] `POST /Sessions/Playing/Ping`、`POST /Sessions/Capabilities/Full`：兼容客户端会话保活和能力上报。

## 已实现：用户观看状态

- [x] `POST /Users/{UserId}/PlayedItems/{Id}`：将项目标记为已观看，首次标记时增加一次播放次数并清零续播位置；电视剧级联全部季和集，季级联全部集。`SeasonId` 查询参数会限定到该电视剧的直接子季；VidHub 未携带该参数时，复用同一用户和客户端最近浏览的当前季。
- [x] `DELETE /Users/{UserId}/PlayedItems/{Id}`：将项目标记为未观看并清零续播位置；电视剧和季按相同层级向下级联，并支持相同的 VidHub 当前季语义。
- [x] `POST /Users/{UserId}/PlayedItems/{Id}/Delete`：标记未观看的兼容别名，支持 VidHub 当前季操作。
- [x] `POST /Users/{UserId}/PlayingItems/{Id}`：记录指定用户开始播放。
- [x] `DELETE /Users/{UserId}/PlayingItems/{Id}`：记录指定用户停止播放，并复用 90% 完成规则。
- [x] `POST /Users/{UserId}/PlayingItems/{Id}/Progress`：记录指定用户播放进度。
- [x] `POST /Users/{UserId}/PlayingItems/{Id}/Delete`：停止播放的兼容别名。
- [x] `POST /Users/{UserId}/Items/{ItemId}/UserData`：部分更新续播位置、已观看、收藏和播放次数，未提交字段保持不变；提交季或电视剧的 `Played` 时复用相同级联规则，携带电视剧 ID 与 `SeasonId` 时限定到 VidHub 当前季。
- [x] 季和电视剧的 `UserData.UnplayedItemCount` 按当前用户的本地剧集状态计算；标记单集后自动重新计算所属季和电视剧的已观看状态。

## 已实现：用户媒体状态

- [x] `POST /Users/{UserId}/FavoriteItems/{Id}`：将项目加入收藏，并返回更新后的用户媒体状态。
- [x] `DELETE /Users/{UserId}/FavoriteItems/{Id}`：取消收藏，并保持观看进度、已观看状态和播放次数不变。
- [x] `POST /Users/{UserId}/FavoriteItems/{Id}/Delete`：取消收藏的兼容别名。
- [x] `GET /Users/{UserId}/Items?Filters=IsFavorite`：只返回当前用户收藏且有权访问的项目。

## 演职人员头像过滤

电影和电视剧媒体库可开启 `hide_people_without_images`。开启后，Emby
人员列表、详情和批量集响应只返回有本地缓存头像（本地化模式）或 TMDB
头像地址（远程模式）的人员；关闭时保持原有完整演职人员列表。该设置只
影响展示，不删除人员、关系或头像缓存。

## 暂不纳入

以下 OpenAPI 分组不属于当前 Vidhub 直连播放范围，暂不实现：服务器管理、插件、
直播电视、DLNA、同步下载、转码、播放列表、个人评分和远程图片提供者。未支持的
接口保持返回标准 Emby 404，不伪造成功响应。

## Manager 用户管理接口

- [x] `GET /api/manager/users`：获取用户及其已授权媒体库列表，仅管理员可调用。
- [x] `GET /api/manager/openlist/users`：分页查询 OpenList 用户，仅管理员可调用。
- [x] `POST /api/manager/user-authorizations`：为 OpenList 身份授予本地访问、角色和媒体库权限，仅管理员可调用。
- [x] `PUT /api/manager/users/{id}`：修改角色、启用状态和媒体库授权，仅管理员可调用；拒绝用户名、密码及其他未知字段。
- [x] `POST /api/manager/users`、`DELETE /api/manager/users/{id}`：返回 HTTP 404；账号及凭据管理由 OpenList 负责。

## Manager 媒体库接口

- [x] `PUT /api/manager/media-library/items/{id}/metadata`：编辑已索引条目的文字信息、元数据来源与提供者 ID、海报及背景来源地址；图片下载和校验成功后保存为本地资源，管理员或对应媒体库编辑者可调用。
- [x] `POST /api/manager/media-library/items/{id}/metadata/refresh`：按 `metadata_source`、`tmdb_id` 和 `tvdb_id` 从 TMDB 或 TVDB 更新电影或电视剧元数据；管理员或对应媒体库编辑者可调用，不改变文件位置和用户观看状态。
- [x] `GET /api/manager/media-library/items/{id}/people`：从本地 SQLite 获取电影、电视剧、季或单集的演职人员；所有已登录用户仅可读取自己获授权媒体库内的条目。
- [x] `GET /api/manager/media-library/items?person_id={tmdbPersonId}`：按 TMDB 人员 ID 返回当前用户可访问、已入库的电影和电视剧；不返回 TMDB 未入库作品。
- [x] `GET|HEAD /api/manager/media-library/items/{id}/images/{type}`：按当前用户授权返回本地媒体图片及缓存头。
- [x] `GET|HEAD /api/manager/media-library/people/{id}/images/primary`：按当前用户授权返回本地人物头像。
- [x] `POST /api/manager/media-library/storage/migrations`：创建“媒体元数据迁移”后台任务，复制并校验缓存后切换本地元数据目录。

## 本地元数据行为

- [x] 电影与电视剧支持配置 TMDB、TVDB 凭据及来源优先级，条目可选择自动、TMDB 或 TVDB；扫描时将中文文字信息、媒体人员关系、主海报、单背景、单集剧照和人物头像写入 SQLite 与受控本地目录。
- [x] 音乐库按单个音频文件建立 `Audio` 项，扫描时使用内置的网易云音乐、QQ 音乐和酷我音乐来源保存歌曲名、艺人、专辑、时长、歌词和封面；元数据匹配完成后再建立最终艺术家、专辑和歌曲父子关系，并将匹配封面同步到专辑与艺术家层级；低置信匹配保留文件名推断结果并计入元数据失败数。
- [x] 媒体浏览、季集列表、人员筛选和图片读取只访问本地 SQLite 与文件缓存，不即时访问 TMDB。
- [x] 音乐浏览、音频详情、歌词和封面只读取本地 SQLite 与图片缓存，不即时访问外部音乐服务。
- [x] 原图按内容哈希标记保存，派生 JPEG/PNG 缩略图按规范化尺寸参数缓存；GIF 只支持无缩放原样返回。
- [x] 默认目录为数据库旁的 `media` 目录；Docker 默认使用 `/data/media`，由现有 `/data` 数据卷持久化。
