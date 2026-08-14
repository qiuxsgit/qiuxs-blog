# qiuxs-blog Admin

这是个人博客的 React 管理 SPA。它只生成静态文件，部署到 Nginx 的 `/web/deploy/blog-admin`；登录会话由同源的 `blog-api` 维护，浏览器请求使用 same-origin cookies。

## 开发与构建

项目固定使用 Node `20.19.4`（见 `.nvmrc`），建议先执行 `nvm use 20.19.4`。首次安装使用 `npm ci`，API 客户端由仓库内的 `contracts/openapi/admin-v1.yaml` 生成：

```bash
make version-check
make install
make build
```

`make test` 只运行 TypeScript 检查和 Node/Vitest 纯函数测试；本项目不包含 UI 单元测试、Playwright 或 e2e。页面、键盘操作、响应式布局和实际部署后的交互由项目负责人手动验收。

`make build` 要求生成后的 `src/api/generated/admin.ts` 没有未提交差异，随后构建并检查 `dist/`：必须有带 hash 的 JS/CSS、所有资源引用可解析、不得包含 source map、绝对本机路径、受保护的服务端环境变量或超过 2 MiB 的单文件。`dist/` 和测试报告均被 Git 忽略。

## 页面与 API 操作组

- `/login`：管理员登录；其余页面受会话保护。
- `/articles`：文章列表，按 active/trashed 查询，创建、回收站、恢复。
- `/articles/new`、`/articles/:articleId/edit`：Markdown 编辑器，草稿保存、标签管理、媒体注册。
- `/articles/:articleId/preview`：安全 Markdown 预览。
- `/articles/:articleId/versions`：不可变版本列表、创建版本、恢复版本。
- `/publishing`：Release 列表、创建发布、查看状态、失败重试。
- `/settings/site`：站点名称、作者、SEO 与备案信息；备案默认值为“长安休息室”和“浙ICP备17057726号-1”，后台可修改；不包含公安联网备案号。
- `/settings/builder`：Jenkins 地址、Job 名称、token 配置状态、测试连接。
- `/settings/hotlink`：允许域名动态配置；空 Referer 允许，后续可扩展盗链规则。

API 操作覆盖 session/me、articles/draft/preview/trash、tags、versions/restore、media upload-policy/register、site/hotlink/builder settings、builder test，以及 releases/get/retry。具体请求和响应以 OpenAPI 为准。

## 媒体与发布边界

编辑器上传通过管理端取得 upload policy 后直传 GFS（`go-file-server`/阿里云 OSS），再向博客 API 注册元数据；管理端不保存 OSS 密钥。文章中的图片使用稳定的 `https://qiuxs.com/img/proxy/{id}` 地址，由博客服务端代理签名访问，避免把 OSS 设为公开读。

Admin 的 Stage 6 交付物是 `dist/` 静态目录，发布流水线将其以原子 release 方式同步到 `root@ngx1:/web/deploy/blog-admin`。Jenkins、Nginx、Service SSH 和运行时密钥配置不放在本目录。
