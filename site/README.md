# qiuxs.com 静态站点

站点使用 Astro static 输出。构建阶段读取 Release Bundle，Markdown 只在构建时渲染；浏览器不会请求 Service 内容 API，也不运行 SSR。

## 本地构建

使用 Node `v20.19.4`：

```sh
npm ci
npm test
npm run check
npm run build
```

默认使用仓库中的 `contracts/fixtures/release-bundle.v1.json`。发布构建时由 Jenkins 注入 `BLOG_BUNDLE_PATH`，指向已校验的 Release Bundle。生成的 `dist/` 可直接同步到 Nginx 的 `/web/deploy/blog-site/current`。

产物包含首页、文章、标签、归档、关于、404、RSS、Sitemap 和备案链接；媒体仍使用 `/img/proxy/{publicKey}`，由 Nginx 转发到 Service 的代理路由。

前端页面不设置 UI 自动化测试；仓库保留纯 Bundle/Markdown/内容索引和静态产物检查，页面由上线前人工验收。
