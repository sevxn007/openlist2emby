# openlist2emby 用户授权 API

OpenList 是唯一账号和密码来源。openlist2emby 只管理本地访问权限、角色、
媒体库授权和会话，不创建 OpenList 账号或保存密码。身份绑定使用实例标识
和不可变的 OpenList 用户 ID，用户名仅用于展示。

Manager API 返回 `{code,message,data}` JSON 信封。

## 登录与当前身份

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| `POST` | `/api/manager/auth/login` | 提交 `username`、`password` 和可选 `otp_code`，返回用户信息并设置 HTTP-only `openlist2emby_session` Cookie |
| `GET` | `/api/manager/auth/me` | 返回当前本地会话对应的身份和权限 |
| `POST` | `/api/manager/auth/logout` | 撤销当前会话并清除 Cookie |

登录时由 OpenList 验证身份；后续请求验证本地会话及当前访问权限。
浏览器会话在成功认证时续期 24 小时，Emby 设备会话续期 30 天。
过期或撤销的会话不会恢复。OpenList 账号变化不会自动撤销已建立的本地会话。

## 管理员授权

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| `GET` | `/api/manager/openlist/users?page=1&per_page=50` | 分页查询 OpenList 用户，页大小为 1–100 |
| `GET` | `/api/manager/users` | 列出本地授权用户及独立的访问状态和上游状态 |
| `POST` | `/api/manager/user-authorizations` | 为 OpenList 用户创建本地授权 |
| `PUT` | `/api/manager/users/{id}` | 更新 `role`、`enabled` 和 `library_grants` |

创建授权提交 `openlist_user_id`、`role`、`enabled`、`library_grants`。
角色为 `admin` 或 `media`；媒体用户必须至少有一个有效的媒体库授权。
`view` 允许浏览和播放，`edit` 额外允许元数据与图片维护。

用户修改接口拒绝未知字段，包括 `username` 和 `password`。账号创建、
重命名、密码修改和删除均在 OpenList 中完成。本地用户创建与删除接口
返回 HTTP 404，不提供凭据管理。

## 权限约束

- 新用户默认没有 openlist2emby 访问权限，重复身份绑定会被拒绝。
- 禁用本地访问会撤销对应身份会话和 Emby 凭据。
- 角色与媒体库权限修改在后续请求生效。
- 不允许禁用或降级最后一个可用管理员，管理员不能禁用自身访问。
- 文件操作、设置、任务和用户授权仅管理员可用，目录操作使用服务端集成密钥。
- Emby 登录不支持开启两步验证的 OpenList 账号。
- 更换 OpenList 实例需要全新安装，不能沿用另一实例的身份授权。
