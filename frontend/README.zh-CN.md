# openlist2emby 前端

[English](README.md) | [简体中文](README.zh-CN.md)

React 界面提供首次设置、OpenList 身份登录、媒体库浏览与播放、元数据编辑、文件管理、用户授权、设置和后台任务。API 访问集中在
`src/shared/api`；服务端状态使用 TanStack Query 管理。用户可见的产品文案通过 `src/shared/i18n` 提供英文和简体中文版本。

## 开发

使用 Node.js 24 和 pnpm 11.7.0：

```bash
pnpm install --frozen-lockfile
cp .env.example .env
pnpm dev
```

`VITE_OPENLIST2EMBY_BASE` 用于设置开发环境后端源，默认值为 `http://127.0.0.1:8091`。Vite 在端口 `5174` 提供前端服务，并代理
`/api/manager/*`。生产构建使用由 Go 服务提供的同源 API。Emby 客户端使用“设置”中配置的独立监听器。

```bash
pnpm lint
pnpm test
pnpm build
pnpm preview
```

## 身份验证与用户访问

首次设置会验证现有 OpenList 管理员和集成密钥。登录时接受 OpenList 用户名、密码和可选验证码。浏览器会收到 HTTP-only 的
`openlist2emby_session` Cookie；密码、集成凭据和 OpenList 用户令牌绝不会保存在浏览器中。

管理员选择 OpenList 身份，并授予独立角色、启用访问权限以及媒体库 `view`/`edit` 权限。账号创建和凭据修改均应在 OpenList
中完成。媒体用户只能看到已授权的媒体库。

主题、语言、侧边栏状态、音频播放器偏好和媒体搜索历史保存在浏览器存储的 `openlist2emby.` 命名空间下。初始主题脚本和 React
主题提供器读取同一个 `openlist2emby.theme` 偏好值。

## 媒体元数据与任务

“设置”用于配置 TMDB 和 TVDB 凭据、可选的 TVDB 订阅 PIN 以及数据源优先级。电影和电视剧可保存 `tmdb_id`、`tvdb_id`，并设置
`metadata_source` 为 `auto`、`tmdb` 或 `tvdb`。显式刷新请求会使用这些字段，且要求拥有媒体库编辑权限。数据源出错时会保留已存元数据。

图片通过需要身份验证的本地图片端点提供。管理员可以通过持久化任务迁移元数据目录。音乐扫描使用网易云音乐、QQ
音乐和酷我音乐，并在本地缓存匹配到的元数据、歌词和封面。

扫描计划由媒体库设置管理。任务中心展示进度和运行历史，并支持取消任务、删除非运行中的任务，以及重试失败或已取消且仍已注册的任务。用户可以在音频项目详情页提交
`music.embed_metadata` 任务以嵌入音乐元数据；即使关闭浏览器，已提交的任务也会继续运行。
