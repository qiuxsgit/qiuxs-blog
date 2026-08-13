# qiuxs-blog MVP 实施路线图

> 本路线图把已确认的架构规格拆成可独立评审和验收的实施计划。每个阶段完成后再进入下一阶段，避免跨 Service、Admin、Site 和部署同时修改。

**规格来源：** `docs/superpowers/specs/2026-08-13-qiuxs-blog-design.md`

## 阶段顺序

### 1. Service 基础与管理员认证

**详细计划：** `docs/superpowers/plans/2026-08-13-service-foundation-auth.md`

交付可独立运行的 Go 服务：配置加载、OpenAPI 生成、人工执行的首版 MySQL SQL 脚本、共享 Redis 主键生成器、Redis Session、Argon2id 密码、登录限流、管理员初始化命令、登录/退出/当前用户接口和健康检查。

**验收出口：** 管理员表不使用 MySQL 自增，命令行通过 Redis 生成有符号 `BIGINT` 主键并创建唯一管理员；随后可通过 HTTP 登录并取得 Redis Session；服务重启后 Session 仍有效；错误密码受限流保护；所有测试通过。

### 2. Service 内容、修订、设置与媒体

交付文章身份、不可变修订、自动保存乐观锁、标签快照、版本恢复、软删除、站点设置、备案门禁、GFS 上传策略、媒体登记、媒体引用和 Referer 防盗链图片跳转。

**验收出口：** API 可以完成从新建文章、保存草稿、创建版本、恢复版本到预览数据读取的完整流程；粘贴图片可直传 GFS 并写入稳定随机媒体地址；防盗链配置即时生效。

### 3. Service Release 与 Jenkins 编排

交付串行发布锁、不可变整站 Release、版本化 Bundle、Jenkins 构建器配置与加密 Token、构建触发、HMAC 阶段回调、失败重试和 `release.json` 对账。

**验收出口：** 一个冻结 Release 在后续草稿变化后仍返回完全一致的 Bundle；Jenkins 回调幂等且可防重放；失败不改变当前线上指针。

### 4. React Admin

交付 React + Vite 管理 SPA：登录、文章列表、Milkdown 专注画布、GFM 源码模式、自动保存、冲突处理、完整预览、图片粘贴、版本历史、发布状态，以及站点、备案、Jenkins 和防盗链设置。

**验收出口：** 管理员可在浏览器完成登录、粘贴整篇 Markdown、上传图片、预览、创建版本和触发发布；Node.js `20.19.4` 构建通过。

### 5. Astro 静态站

交付 Astro 6 静态站：已确认的首页与文章详情视觉、文章/标签/归档/关于/404、GFM 安全渲染、Shiki、Pagefind、RSS、Sitemap、SEO、默认 OG 图、主题切换和全部页面备案组件。

**验收出口：** 固定 Bundle 可在 Node.js `22.20.0` Docker 镜像中生成完整 `dist/`；输出不包含 SSR；缺少备案信息时构建失败；桌面和移动视觉回归通过。

### 6. Jenkins、Nginx 与端到端发布

交付三个独立流水线、Site 构建镜像、Release 目录 rsync、原子软链接切换、Nginx 双域名路由、关键产物门禁、历史 Release 保留和 Playwright 端到端发布验证。

**验收出口：** Admin 构建使用宿主机 Node.js `20.19.4`，Service 使用宿主机 Go `1.25.7`，Site 使用 Docker Node.js `22.20.0`；任一构建或部署阶段失败时 `qiuxs.com` 保持上一成功版本。

## 跨阶段规则

- 每一阶段开始前从本路线图生成一份符合 `superpowers:writing-plans` 的详细计划。
- 每一阶段在独立工作区执行，优先使用 `superpowers:subagent-driven-development`。
- 所有功能使用 TDD：先看到目标测试失败，再实现最小代码使其通过。
- 每个可独立验收的任务结束后提交，禁止把多个领域塞入同一提交。
- OpenAPI、Release Bundle Schema 和 Markdown 固定样例属于跨应用契约，修改时必须同时更新契约测试。
- 所有 MySQL 表主键为 `BIGINT NOT NULL`，Go 与契约层使用 `int64`；禁止 `UNSIGNED` 和 `AUTO_INCREMENT`。新增实体统一通过共享 Redis `idgen` 取号，禁止 Repository 自建生成器或回退其它 ID 策略。
- Redis ID 只负责身份，不表达时序；列表必须按显式时间字段排序。主键自愈只处理 `PRIMARY` 冲突，不能吞掉业务唯一键冲突。
- 数据库变更只写入 `service/sqls/develop/develop.sql`；发布时由本仓库 `server-release` 技能冻结为 `service/sqls/releases/v<版本>.sql`，再重建 develop 占位文件。服务不自动迁移，SQL 由管理员人工执行。
- 任何阶段发现需要改变已确认架构时停止实现，先修改设计规格并重新获得确认。
