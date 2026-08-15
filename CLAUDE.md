# qiuxs-blog 编码协作规则

## 项目边界

这是一个三部分仓库：

| 目录 | 职责 | 技术栈 |
|---|---|---|
| `service/` | 管理 API、媒体代理、Release/Jenkins 回调 | Go 1.25.7、Gin、`database/sql`、MySQL、Redis |
| `admin/` | 在线 Markdown 管理后台 | React、TypeScript、Vite、Node 20.19.4 |
| `site/` | Release Bundle 构建出的公开静态站 | Astro static、TypeScript、Node 20.19.4 |
| `deploy/` | Jenkins、Nginx、systemd、原子部署脚本 | POSIX shell、Jenkins、Nginx |

公共 API 契约位于 `contracts/openapi/admin-v1.yaml`；Release Bundle 契约位于
`contracts/release-bundle-v1.schema.json`。修改跨端字段时先同步契约，再更新 Service、Admin、Site。

## 开始工作前

- 先运行 `git status --short`，只暂存本任务文件，不使用 `git add -A`。
- 先读相关计划、契约和本目录规则；没有代码地图时用 `rg --files`、`rg` 定位，禁止盲目改动。
- 不覆盖用户已有改动，不执行 `git reset --hard`、`git checkout --`。
- 结构性变更（新增/删除 API、路由、页面、领域目录、契约字段）要同步计划或文档。

## 测试策略

- Service：使用 `sqlmock`、`miniredis`、`httptest` 和 fake/stub；不依赖真实 MySQL、Redis、GFS、Jenkins 或 OSS。
- Admin/Site：只编写纯函数、API adapter、缓存、Bundle/Markdown/产物检查测试；不编写 React/DOM/Milkdown/UI 单元测试，不添加 Playwright/e2e，不做自动 UI 验收。
- 页面、响应式、交互和真实部署由负责人手动验收。
- 所有测试必须可离线运行；测试不得写入真实凭据、访问生产域名或发起真实 SSH。

## 质量门禁

```sh
# Service
(cd service && GOTOOLCHAIN=go1.25.7 gofmt -w . && go test ./... && go vet ./...)

# Admin/Site（使用 Node 20.19.4）
(cd admin && npm ci && make test && make build)
(cd site && npm ci && npm test && npm run check && npm run build)

# 部署脚本
make test-deploy
git diff --check
```

完成前必须确认生成文件无漂移、工作区无意外改动，并说明未执行的真实环境操作。

## 安全和部署

- 密钥、Token、`.env`、SSH 私钥和真实服务器信息不得提交；示例文件只能放占位符。
- Service 部署目标是 `root@blogweb1:/web/deploy/blog`；Admin/Site 部署目标是 `root@ngx1:/web/deploy/{blog-admin,blog-site}`。
- 静态发布使用 `releases/<id>` + 原子 `current` symlink；失败不得改变旧版本。
- 真实 Jenkins/SSH 发布必须由用户明确触发，本地验证只使用 fake/参数检查。

## 提交规范

- 提交信息使用 `<type>(<scope>): <imperative summary>`，例如 `feat(service): add article lifecycle`。
- 一个提交聚焦一个可回滚主题；规则、代码、测试可以同提交，但不要混入无关格式化。
- 不推送远程分支，除非用户明确要求。
