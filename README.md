# qiuxs-blog

个人博客项目。仓库包含 Go 服务、React 管理端和 Astro 静态公开站点。服务负责后台编辑、媒体代理、不可变 Release、Jenkins 编排、Bundle 下载和签名回调；公开站点只在构建时读取 Bundle，生成静态 HTML，不使用 SSR 或运行时内容 API。Jenkins/Nginx/SSH 部署配置已提供，但真实发布仍由运维人员在 Jenkins 中触发。

- [服务端运行、配置与手工 SQL 指南](service/README.md)
- [Release、Jenkins 回调与运维指南](service/README.md#immutable-releases-and-jenkins-operation)
- [Stage 2 内容与媒体实现计划](docs/superpowers/plans/2026-08-13-service-content-media.md)
- [GFS 博客媒体契约](docs/contracts/gfs-blog-media.md)
- [Admin 管理端构建与部署说明](admin/README.md)
- [公开站点构建与静态产物说明](site/README.md)
- [Jenkins、Nginx 与首次部署手册](deploy/README.md)
- [产品与架构设计](docs/superpowers/specs/2026-08-13-qiuxs-blog-design.md)
- [项目路线图](docs/superpowers/plans/2026-08-13-qiuxs-blog-roadmap.md)
