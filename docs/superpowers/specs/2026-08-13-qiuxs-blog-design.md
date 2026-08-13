# qiuxs-blog MVP 设计规格

- 状态：已完成设计确认，等待书面规格复核
- 日期：2026-08-13
- 公开域名：`https://qiuxs.com`
- 管理域名：`https://blog-admin.qiuxs.com`

## 1. 目标

建设一个面向技术文章的个人博客。管理员在网页中获得接近语雀的写作体验，文章正文以 Markdown 保存；公开站由 Astro 在发布时生成纯静态文件，线上不运行 SSR，也不在读者请求中查询 MySQL。

系统需要满足以下核心约束：

- 云服务器性能有限，只承担 Go 内容服务、MySQL、Redis、Admin 静态文件、图片签名跳转以及 Nginx 静态文件服务。
- 家庭 Jenkins 有稳定公网入口，负责构建和部署。
- 公开站在后台、数据库或 Jenkins 故障时继续提供上一个成功版本。
- 图片复用现有 `go-file-server`，文件保存在私有阿里云 OSS。
- 站点必须在所有公开页面底部展示可配置的 ICP 备案信息。

## 2. MVP 范围

### 2.1 包含

- 单管理员登录、Redis Session 和登录失败限流。
- 文章、标签、草稿自动保存、手动版本、版本恢复、取消发布和软删除。
- Milkdown 专注画布、整篇 GFM Markdown 粘贴、源码模式和完整文章预览。
- GFS 图片直传、媒体登记、随机公开键和私有 OSS 读取跳转。
- 可动态配置的 Referer 域名白名单和“允许空 Referer”开关。
- 单 Jenkins 构建器配置、连接测试、构建触发和 HMAC 回调。
- 不可变整站 Release、串行发布、失败重试和发布记录。
- Astro 静态首页、文章列表、文章详情、标签、归档、关于、搜索和 404 页面。
- Pagefind、RSS、Sitemap、SEO、默认 OG 图以及深浅主题。
- rsync 版本目录部署、原子软链接切换和旧版本保留。

### 2.2 不包含

- 多管理员、角色权限、公开注册、OAuth 和密码找回。
- 评论、点赞、浏览量、邮件订阅和公开用户系统。
- 分类树、系列文章和定时发布。
- Mermaid、数学公式、脚注、提示块、MDX 和自定义文章组件。
- 多构建器或 GitHub Actions、GitLab CI 等插件体系。
- 媒体永久删除和复杂防盗链规则。
- SSR、公开内容 API 和浏览器端 Markdown 正文渲染。
- 自动部署回滚。首版保留历史产物，由管理员执行人工回滚。

## 3. 仓库结构与技术栈

所有应用位于同一个 Git 仓库，目录直接位于仓库根部：

```text
qiuxs-blog/
├── admin/          React + Vite + Milkdown 管理后台
├── service/        Go 内容与发布服务
├── site/           Astro 静态公开站
├── contracts/      OpenAPI、Release Bundle Schema 和 Markdown 固定样例
├── deploy/         Jenkins、Nginx、构建镜像和部署配置
└── docs/           设计与项目文档
```

构建环境固定如下：

| 应用 | 环境 | 版本与方式 |
| --- | --- | --- |
| `admin/` | Jenkins 宿主机 | Node.js `20.19.4`，React + Vite，使用 lockfile 安装 |
| `service/` | Jenkins 宿主机 | Go `1.25.7`，`CGO_ENABLED=0` 编译 Linux 可执行文件 |
| `site/` | Docker | Node.js `22.20.0` 固定镜像，Astro 6 静态构建 |

`site` 构建镜像同时提供 pnpm、OpenSSH 和 rsync，避免 Jenkins 宿主机为静态站安装额外工具。三个应用独立构建和部署；内容发布只触发 `site` 流水线。

Go 服务使用 Gin，MySQL 保存持久数据；Redis 保存主键序列、Session、限流状态和短期幂等数据。OpenAPI 是 Admin API 的接口真源，React 客户端由契约生成。

## 4. 运行架构与域名

```text
管理员浏览器
  └─ https://blog-admin.qiuxs.com
       ├─ /             Nginx → React Admin 静态文件
       └─ /api/*        Nginx → Go Service

读者浏览器
  └─ https://qiuxs.com
       ├─ /img/proxy/*  Nginx → Go Service
       └─ /*            Nginx → Astro 当前静态 Release

家庭 Jenkins
  ├─ 调用 Internal API 下载不可变 Release Bundle
  ├─ 在 Docker 中构建 site
  ├─ rsync 到云服务器 Release 目录
  └─ 回调 Go Service 报告阶段和结果

Go Service
  ├─ MySQL
  ├─ Redis
  ├─ Jenkins API
  └─ 本地计算 GFS 读写签名
```

Admin Session Cookie 只属于 `blog-admin.qiuxs.com`。根域名不暴露管理 API，仅公开图片解析路由。

## 5. 视觉与交互方向

### 5.1 公开站

视觉采用“冷峻工程日志索引”：炭黑背景、冷白正文、电光蓝强调色、精细低对比边框和克制网格。工程感来自秩序、信息层级和元数据，不使用字符雨、频繁故障动画或大面积黑客绿。

首页以个人介绍和最近文章为核心。文章列表不依赖封面，以标题、摘要、标签、日期和阅读时间建立层级。侧栏可展示当前关注主题、简短近况和站点统计。

文章详情页包含：

- 标题、摘要、发布日期、更新时间、字数、阅读时间和标签。
- 桌面端固定目录；窄屏隐藏侧栏，正文保持舒适宽度。
- 构建时语法高亮的代码块、复制按钮和横向滚动。
- 阅读进度、上一篇/下一篇和文章更新时间。
- 深色与浅色主题。首次参考系统偏好，之后保存用户选择且避免首屏闪烁。

### 5.2 Admin 编辑器

编辑页采用语雀式单栏“专注画布”：

- Milkdown 所见即所得模式默认开启。
- 标题下方折叠展示摘要、标签和可选封面。
- Markdown 源码和最终预览按需打开，不长期分屏。
- 顶部明确展示保存中、已保存、保存失败和版本冲突状态。
- 停止输入约 2 秒后自动保存。

完整预览位于 Admin SPA 的受保护路由，仅管理员 Session 可访问。它使用与公开站一致的 Markdown 约定、设计令牌、样式快照和固定测试样例，确保预览与发布页面在正文排版和代码表现上保持一致；预览不触发 Jenkins，也不产生公开 URL。预览渲染时将正文内的 `/img/proxy/*` 相对地址解析到 `https://qiuxs.com`，避免请求错误落到管理域名。

## 6. Admin 页面

首版路由：

```text
/login
/articles
/articles/new
/articles/{id}/edit
/articles/{id}/preview
/articles/{id}/versions
/publishing
/settings/site
/settings/builder
/settings/hotlink
```

文章列表展示标题、草稿更新时间、当前线上状态和最近发布结果，支持新建、编辑、取消发布、移入回收站和恢复。

设置页面负责：

- 站点名称、作者介绍、首页近况、关于页 Markdown、社交链接和 SEO 默认值。
- ICP 备案名称和备案号。
- Jenkins 地址、用户名、API Token 和 Job Name。
- 防盗链允许域名和允许空 Referer 开关。

站点设置修改后只保存为待发布配置。管理员从设置页或发布页执行“发布站点”，创建包含当前线上文章和最新站点配置的新 Release。

## 7. Markdown 约定

MySQL 中的 Markdown 原文是唯一内容真源，Milkdown 只是编辑界面。首版固定使用 GFM 基础语法：

- 标题、段落、粗体、斜体和删除线。
- 有序列表、无序列表和任务列表。
- 引用、链接、图片和表格。
- 行内代码和围栏代码块。

约束：

- 禁止原始 HTML，避免脚本、事件属性和样式污染。
- 首版不支持 Mermaid、公式、脚注、自定义提示块和 MDX。
- 代码围栏接受语言标识；首版不支持文件名、重点行和行号语法。
- 标题锚点由构建器生成；重复标题追加稳定序号。
- 外部链接增加安全属性，只允许受支持的 URL 协议。
- 正文图片使用相对稳定地址 `/img/proxy/{publicKey}`。

Shiki 在构建阶段完成代码高亮，正文不在浏览器中再次解析 Markdown。

## 8. 内容模型

### 8.0 全局主键规则

所有 MySQL 表统一使用应用层生成的有符号主键：

- DDL 使用 `id BIGINT NOT NULL`，禁止 `UNSIGNED` 和 `AUTO_INCREMENT`。
- Go 领域模型、Repository、Session 和 OpenAPI ID 字段统一使用 `int64`。
- `0` 和负数为未来特殊值保留；首版不赋予具体业务含义，也不由生成器发放。
- 共享的 Redis ID 生成器在 INSERT 前执行 `INCR("idseq:<真实表名>")`，真实表名必须是代码常量，不能来自外部输入。
- ID 公式为 `id = offset + (raw - 1) * step`。默认 `IDGEN_OFFSET=1`、`IDGEN_STEP=1`；必须满足 `1 <= offset <= step`。
- 所有 Repository 注入同一个共享生成器，通过“取号后插入”的统一边界创建实体，不能在 Repository 内自行构造生成器。
- `IDGEN_HEAL` 默认关闭。开启后仅 MySQL `PRIMARY` 主键冲突触发：查询该表 `MAX(id)`，按当前 offset/step 车道抬升 Redis 计数器并进行最多 5 次有界重试。
- MySQL 1062 必须根据错误中的索引名区分 `PRIMARY` 与业务唯一键；业务唯一键冲突不能触发主键自愈。
- Redis 取号失败时本次新增操作失败，不回退到 MySQL 自增或本地随机 ID。
- ID 不承诺连续，也不作为时间顺序。列表、历史版本和发布记录按 `created_at` 等显式时间字段排序，必要时用 `id` 作为同时间戳下的稳定次级排序键。

这一规则覆盖管理员、文章、修订、标签、关联表、媒体、设置、Release 和发布任务等全部持久化表。

### 8.0.1 SQL 脚本与人工执行

Go 服务不包含自动迁移库、迁移命令或启动时建表逻辑。数据库变更只以仓库内 SQL 文件交付：

```text
service/sqls/
├── develop/
│   └── develop.sql
└── releases/
    ├── v0.1.sql
    └── v<后续版本>.sql
```

- `develop/develop.sql` 是当前开发周期唯一可追加的 SQL 文件。
- `releases/v<版本>.sql` 是发布时从 `develop.sql` 冻结得到的只读历史文件；冻结后不得修改。
- 冻结完成后重新创建只含说明注释的 `develop/develop.sql` 占位文件，供下一开发周期追加。
- Release SQL 是只向前执行的人工操作脚本，不提供自动 Down、回滚或数据库连接逻辑。
- 服务构建、启动和健康检查都不执行 SQL；脚本最终由管理员人工审核并执行。
- 自动测试只静态校验脚本目录、版本命名和关键 DDL 约束，不连接真实 MySQL。
- 后续在本仓库实现 `server-release` 技能，参考 `/Users/qiuxs/codes/qiuxs/account-book-cc-workspace/.claude/skills/server-release`，负责发布时校验并冻结 `develop.sql`，但不得连接或修改数据库。

### 8.1 管理员

`admins` 保存单个管理员：

- `id`
- `username`，唯一
- `password_hash`，Argon2id
- `state`
- `last_login_at`
- `created_at`、`updated_at`

首个管理员通过命令行初始化，不提供网页注册。

### 8.2 文章与修订

`articles` 保存稳定身份：

- `id`
- `slug`，创建时由密码学安全随机源生成，12 位小写 URL-safe 字符，唯一且不可修改
- `draft_revision_id`
- `published_revision_id`，未发布时为空
- `state`：`active` 或 `trashed`
- 审计字段

公开地址固定为：

```text
https://qiuxs.com/posts/{slug}/
```

`article_revisions` 保存一次完整内容状态：

- `id`
- `article_id`
- `revision_no`
- `status`：`editing` 或 `frozen`
- `reason`：`draft`、`manual_version` 或 `publish_snapshot`
- `title`
- `summary`
- `cover_media_id`，可空
- `content_md`，MySQL `LONGTEXT`
- `content_hash`
- `lock_version`，乐观锁版本
- 审计字段

标题、摘要、封面、正文和标签都属于修订。每篇文章同一时间只有一个 `editing` 修订。

规则：

- 自动保存只更新当前 `editing` 修订并递增 `lock_version`，不制造历史版本。
- 客户端提交旧 `lock_version` 时返回 `409 Conflict`，不得静默覆盖。
- “创建版本”冻结当前修订并复制出新的可编辑草稿。
- “发布”冻结当前修订作为发布快照，并复制出新的可编辑草稿。
- 恢复历史版本时复制为新草稿，不修改旧版本。
- 首版不自动清理历史版本。

### 8.3 标签

`tags` 保存当前标签名称和稳定 slug；`article_revision_tags` 除 `tag_id` 外还保存当时的 `tag_name` 与 `tag_slug` 快照。这样标签后来改名时，旧版本、Release Bundle 和恢复操作仍能得到当时的完整标签状态。

首版只有标签，不包含分类树和系列文章。

### 8.4 媒体

`media` 至少包含：

- `id`
- `public_key`，格式为 `m_` 加高熵随机字符串，唯一且不可枚举
- `gfs_file_id`
- `original_name`
- `mime_type`
- `file_size`
- `width`、`height`
- `state`
- 审计字段

`article_revision_media` 保存修订对媒体的引用及用途 `content` 或 `cover`。每次保存修订时，服务端解析受支持的 `/img/proxy/*` 地址并同步引用关系；正文不暴露 GFS 自增 ID。

移除图片时不立即删除 OSS 对象，因为历史修订可能仍引用。首版不提供永久删除，只保留未来孤立媒体清理的扩展点。

### 8.5 Release 与发布任务

`releases` 是不可变整站快照，包含站点设置快照、状态、内容校验和时间信息。`release_articles` 明确记录 Release 中每篇文章对应的冻结修订 ID。

`publish_jobs` 保存：

- Release ID 和构建器 ID。
- 状态：`pending`、`queued`、`building`、`deploying`、`success` 或 `failed`。
- Jenkins Build Number。
- 当前阶段、错误摘要和时间字段。

单行 `site_state` 保存 `current_release_id` 和 `active_publish_job_id`。创建发布时用事务及 `SELECT ... FOR UPDATE` 实现全局串行锁。

### 8.6 配置

`site_settings` 保存需要进入 Release 的可发布站点配置。备案字段为：

- `filing_name`：默认 `长安休息室`，Admin 可修改，必填。
- `filing_number`：默认 `浙ICP备17057726号-1`，Admin 可修改，必填。

备案链接固定为工信部备案系统 `https://beian.miit.gov.cn/`，首版没有公安联网备案字段。

`builder_config` 首版只保存一个 Jenkins 构建器：名称、HTTPS 地址、用户名、加密 Token、Job Name 和启用状态。

`referer_allowlist` 保存规范化主机名及启用状态；独立单行 `hotlink_settings` 保存 `allow_empty_referer` 开关。两者属于即时生效的运行配置，不等待静态站发布。

## 9. 文章生命周期

```text
新建草稿
  → 自动保存
  → 手动版本（可选）
  → 发布快照
  → Jenkins 构建与部署
  → 发布成功

已发布文章
  → 编辑新草稿，线上版本不变
  → 再次发布后替换线上版本
  → 取消发布后从下一次成功 Release 中移除
  → 成功取消发布后才能移入回收站
```

发布失败不会修改 `published_revision_id`、`current_release_id` 或公开站文件。构建期间可以继续编辑草稿，但不能创建第二个发布任务。对失败任务执行“重新发布”时复用同一个不可变 Release 并创建新的 Job；若要发布失败后继续编辑的内容，则创建一个新的 Release。

未发布草稿可移入回收站。已发布文章必须先成功取消发布；首版 Admin 不提供永久删除。

## 10. 认证与安全

- 单管理员使用用户名和密码登录，密码使用 Argon2id 哈希。
- Session 存入 Redis；浏览器 Cookie 使用 `HttpOnly`、`Secure` 和 `SameSite=Strict`。
- 登录失败按账号和 IP 在 Redis 中限流。
- Admin 状态修改接口校验 `Origin`，不开放跨域调用。
- Jenkins API Token 使用服务端环境主密钥通过 AES-GCM 加密后存入 MySQL。
- 配置查询永不返回 Token 明文；编辑页面留空表示不修改现有 Token。
- Jenkins 构建用户只拥有读取并触发指定 Job 的最小权限。
- Jenkins 地址只接受无 userinfo、query 和 fragment 的 HTTPS 基础 URL。
- Release Bundle 使用独立 Bearer Token；服务端和 Jenkins Credentials 分别持有同一凭证。
- Jenkins 回调使用 HMAC-SHA256、时间戳和 nonce，服务端校验时间窗口并通过 Redis 防重放。
- 日志禁止记录密码、Session、Jenkins Token、Bundle Token、GFS Secret 和签名参数。

## 11. 图片上传与访问

### 11.1 上传

```text
编辑器捕获粘贴或拖入的图片
→ Admin API 为固定博客路径签发约 60 秒有效的 GFS 上传策略
→ 浏览器 multipart 直传 GFS /v1/upload
→ GFS 写入私有阿里云 OSS，返回 file_id 和元数据
→ Admin API 登记 media，生成 public_key
→ 编辑器插入 /img/proxy/{publicKey}
```

上传策略中的路径由 Go 服务固定为博客专属前缀，Admin 不能提交任意 `savePath`。服务端同时限制允许的图片 MIME、扩展名和文件大小，并验证 GFS 返回的实际元数据。未完成上传的 `blob:` 地址不得进入可发布修订。

封面走同一上传链路。正文保存稳定相对 URL；结构化封面字段保存 `media.id`。

### 11.2 公开读取

```text
GET https://qiuxs.com/img/proxy/{publicKey}
→ 校验空 Referer 开关或精确域名白名单
→ 查询有效 media
→ 本地计算短时 GFS 读取地址
→ 返回 302
→ GFS 返回临时 OSS 地址
→ 浏览器直接读取 OSS 图片
```

Go 服务不代理图片字节，也不为生成签名请求 GFS。

防盗链规则：

- `allow_empty_referer=true` 时允许空 Referer，默认开启。
- 非空 Referer 解析为规范化主机名后与启用白名单精确匹配。
- 默认白名单包含 `qiuxs.com` 和 `blog-admin.qiuxs.com`，后者用于草稿预览；管理员可在后台新增、禁用和删除域名。
- 配置修改后立即失效 Go 内存缓存，无需构建站点。
- 拒绝访问返回 `403`，媒体不存在或失效返回 `404`。
- Referer 可被伪造，此机制只用于普通防盗链，不作为严格访问鉴权。

图片代理响应使用 `Cache-Control: no-store`，避免 CDN 缓存成功的重定向而绕过 Referer 检查。

现有 GFS 的 OSS 临时地址有效期较短，因此 GFS 最后一跳必须从永久 `301` 调整为 `302` 或 `307`。不得永久缓存临时 OSS 签名 URL。

## 12. API 边界

Go 服务内部按领域拆分：

```text
service/internal/
├── auth/          管理员、密码和 Redis Session
├── idgen/         Redis 主键序列、分段车道和冲突自愈
├── article/       文章身份与生命周期
├── revision/      草稿、版本、恢复和乐观锁
├── tag/           标签管理
├── media/         上传策略、媒体登记和图片代理
├── release/       整站快照与内容包
├── builder/       Jenkins 配置、触发与回调
├── settings/      站点、备案和防盗链配置
└── platform/      MySQL、Redis、HTTP、日志和加密
```

Controller 不直接拼 SQL，也不跨模块修改表。`release` 负责发布编排，`builder` 封装 Jenkins 细节。

HTTP 接口分组：

```text
/api/admin/v1/*       Admin Session 鉴权
/api/internal/v1/*    Jenkins Bearer Token 或 HMAC 鉴权
/img/proxy/*          公开访问，执行 Referer 检查
```

Admin API 覆盖登录、文章、修订、标签、预览数据、媒体、发布记录和配置。Internal API 只负责下载 Release Bundle 和接收 Jenkins 回调。

## 13. Release Bundle

Jenkins 一次下载完整 gzip JSON 内容包：

```http
GET https://blog-admin.qiuxs.com/api/internal/v1/releases/{releaseId}/bundle
Authorization: Bearer <BUILD_TOKEN>
Accept-Encoding: gzip
```

逻辑结构：

```json
{
  "schemaVersion": 1,
  "releaseId": "rel_...",
  "generatedAt": "2026-08-13T12:00:00Z",
  "site": {},
  "tags": [],
  "articles": [],
  "checksum": "sha256:..."
}
```

`checksum` 对除自身外的规范化 `site`、`tags` 和 `articles` 内容计算。HTTP 同时返回与该值一致的 `ETag`。Bundle 只包含冻结修订和站点配置快照，不读取不断变化的草稿。

Astro 只依赖版本化 Bundle Schema，不依赖 Go 内部表结构。对个人博客规模采用全量 Bundle 和全量静态构建，不做增量构建。

## 14. 发布与 Jenkins

### 14.1 创建发布

Go 服务在事务中：

1. 锁定 `site_state`，确认不存在活动发布任务。
2. 按操作冻结目标文章修订，或记录取消发布、站点设置变更。
3. 以当前线上 Release 为基础生成新的整站 Release：文章发布使用本次冻结修订，取消发布从快照中移除目标文章，单独发布站点设置时沿用当前线上文章修订而不带入未发布草稿。
4. 写入站点配置快照、`release_articles` 和 `publish_jobs`。
5. 设置 `active_publish_job_id`。
6. 事务提交后调用 Jenkins `buildWithParameters`，只传 `RELEASE_ID`。

Jenkins 调用失败时 Job 标记失败并释放活动锁；Release 保留用于审计。失败重试创建新的 Job 记录，不覆盖原记录。

### 14.2 流水线

```text
触发 Job
→ 回调 building
→ 下载并校验 Release Bundle
→ Docker Node 22.20.0 中执行 Astro build
→ 生成 Pagefind、RSS、Sitemap 和 OG 资源
→ 检查关键 HTML、资源和 release.json
→ rsync 到 releases/{releaseId}.staging
→ 原子重命名为 releases/{releaseId}
→ 原子切换 current 软链接
→ 回调 success；任一步失败则回调 failed
```

Nginx 始终指向：

```text
/var/www/qiuxs-blog/current
```

构建产物布局：

```text
/var/www/qiuxs-blog/
├── releases/
│   ├── rel_xxx/
│   └── rel_yyy/
└── current -> releases/rel_yyy/
```

上传和检查完成前不切换 `current`。部署成功后保留最近若干 Release；具体保留数量是部署配置项，不影响内容模型。

### 14.3 回调与对账

Jenkins 至少在 `building`、`deploying` 和最终 `success`/`failed` 三个阶段回调：

```text
release_id
build_number
stage
status
error_summary
timestamp
nonce
signature
```

签名覆盖规范化请求体、timestamp 和 nonce。回调接口幂等，相同状态重复送达不会制造额外副作用。Jenkins 对最终回调执行重试。

每份静态产物包含 `release.json`，记录 Release ID、Bundle 校验和、Build Number 和部署时间。Go 服务在启动和创建新发布前读取本机 `current/release.json`：若文件表明某个已知 Release 已完成切换但最终回调丢失，且校验和与数据库一致，则完成安全对账；不一致时禁止新发布并要求人工处理。

只有部署成功或安全对账成功后，数据库才在事务中更新 `current_release_id` 和各文章的 `published_revision_id`。失败不改变线上指针。

## 15. 静态站输出

Astro 生成：

```text
/
/posts/
/posts/{randomSlug}/
/tags/
/tags/{tagSlug}/
/archive/
/about/
/404.html
/rss.xml
/sitemap-index.xml
```

构建阶段计算：

- 阅读时间、字数和文章目录。
- 上一篇与下一篇。
- 标签聚合和年份归档。
- Pagefind 搜索索引。
- RSS、Sitemap、Canonical、Open Graph、Twitter Card 和 `BlogPosting` JSON-LD。

封面是可选字段，不强制出现在首页或正文顶部。有封面时用于社交分享图；没有封面时按文章标题、标签和站点视觉生成默认 OG 图片。

浏览器端 JavaScript 仅用于主题切换、搜索面板、代码复制、目录高亮和阅读进度。正文、导航和搜索索引均不依赖运行时 API。

## 16. ICP 备案合规

所有公开页面底部必须由 Astro 直接输出以下格式：

```text
{filing_name} · {filing_number}
```

初始值：

```text
长安休息室 · 浙ICP备17057726号-1
```

备案号链接到 `https://beian.miit.gov.cn/`，使用普通可抓取链接并在新标签打开。要求：

- 首页、文章页、列表页、标签页、归档页、关于页和 404 页全部显示。
- 不依赖 JavaScript，也不能被桌面端或移动端样式隐藏。
- 文字颜色必须满足站点普通辅助文字的可读性要求。
- `filing_name` 和 `filing_number` 可在 Admin 站点设置中修改。
- 两个字段为空时，Go 服务禁止创建 Release。
- Astro 构建再次验证字段非空，并检查每种页面模板都包含备案组件；不满足时构建失败。

首版没有公安联网备案号及其展示区域。

## 17. 错误处理与可观测性

故障策略以保持公开站可用为第一目标：

- MySQL 或 Redis 故障：Admin 暂时不可用，现有静态博客继续工作。
- Redis 主键序列故障：拒绝新增持久化实体，不回退到其它 ID 策略；现有静态博客继续工作。
- GFS 或 OSS 故障：文字内容可读，图片返回明确错误。
- Jenkins 不可用：发布失败，草稿和线上 Release 不变。
- Astro、检查、rsync 或切换失败：不修改 `current`。
- 自动保存失败：保留编辑器本地内容并明确提示，不能误报成功。
- 乐观锁冲突：返回 `409`，允许复制本地 Markdown 或重新加载服务端草稿。
- 长时间没有最终回调：不根据超时擅自认定成功；管理员可人工终止，创建新发布前仍须执行 `release.json` 对账。

Go 服务输出结构化 JSON 日志，至少包含：

```text
request_id
admin_id
article_id
release_id
publish_job_id
jenkins_build_number
duration
result
```

服务提供独立存活和就绪检查。Admin 只展示裁剪后的错误摘要，详细错误保留在服务日志或 Jenkins。

## 18. 测试与发布检查

### 18.1 自动化测试

- `service`：领域单元测试和进程内流程测试。MySQL 边界使用 sqlmock，Redis 边界使用 miniredis，覆盖 Redis 主键生成/分段/冲突自愈、认证、乐观锁、Release、回调幂等和防重放；自动测试不得启动容器或连接已部署的 MySQL/Redis。
- `admin`：Vitest + React Testing Library，覆盖登录、整篇 Markdown 粘贴、自动保存、冲突、上传和发布状态。
- `site`：Markdown 固定样例、危险 HTML 拒绝、路由、RSS、Sitemap、OG、备案组件和 Bundle Schema 测试。
- Playwright：覆盖登录、编辑、预览、上传图片和创建发布任务的关键流程。
- 视觉回归：保存已确认首页与文章详情页的桌面、移动截图基线。
- GFS 契约测试：验证上传返回、媒体元数据、签名读取及临时重定向行为。
- 真实 MySQL、Redis 与网络部署只在第 6 阶段执行人工烟测，不作为自动测试依赖。

### 18.2 构建门禁

- Service 构建前确认 Go 精确为 `1.25.7`。
- Admin 构建前确认 Node.js 精确为 `20.19.4`。
- Site 容器确认 Node.js 精确为 `22.20.0`。
- Node 项目使用 lockfile 和冻结依赖安装。
- Site 部署前检查首页、至少一篇文章、404、静态资源、Pagefind、RSS、Sitemap、`release.json` 和 ICP 备案信息。

## 19. 验收标准

1. 管理员可以粘贴一篇包含 GFM、代码块和图片的完整 Markdown，刷新后已成功保存的内容不丢失。
2. 草稿预览与最终静态文章在正文排版、代码高亮和图片展示上保持一致。
3. 发布创建不可变 Release；构建期间继续编辑不会改变本次内容包。
4. Jenkins 使用 Node `22.20.0` 容器完成 Site 构建、检查、rsync、原子切换和回调。
5. Admin 使用 Jenkins Node `20.19.4` 构建，Service 使用 Go `1.25.7` 构建。
6. 构建或部署失败时，`qiuxs.com` 继续提供上一版本。
7. 公开文章请求不执行 SSR，不查询 MySQL，不在浏览器解析正文 Markdown。
8. 图片使用不可枚举稳定地址；允许空 Referer 和动态白名单，其他来源返回 `403`。
9. 备案名称和备案号可在 Admin 修改；缺失时发布被 Go 与 Astro 双重阻断。
10. 所有公开页面都静态展示 `长安休息室 · 浙ICP备17057726号-1` 或管理员后来发布的有效备案信息。
11. 首页和文章详情页在桌面与移动端符合已确认的冷峻工程日志视觉方向。
12. 自动化测试覆盖登录、文章修订、媒体、Release、Jenkins 回调和静态产物门禁。
