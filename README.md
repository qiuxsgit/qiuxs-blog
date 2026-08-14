# qiuxs-blog

个人博客项目。当前仓库已完成 Stage 3 的 Go 服务：健康检查、管理员会话、文章草稿和不可变修订、媒体与站点设置，以及不可变 Release、Jenkins 编排、Bundle 下载和签名回调。Release 服务只保存/校验发布状态，绝不部署文件；Stage 6 才负责 Jenkins、Nginx 与 SSH 的实际部署流水线。

- [服务端运行、配置与手工 SQL 指南](service/README.md)
- [Release、Jenkins 回调与运维指南](service/README.md#immutable-releases-and-jenkins-operation)
- [Stage 2 内容与媒体实现计划](docs/superpowers/plans/2026-08-13-service-content-media.md)
- [GFS 博客媒体契约](docs/contracts/gfs-blog-media.md)
- [Admin 管理端构建与部署说明](admin/README.md)
- [产品与架构设计](docs/superpowers/specs/2026-08-13-qiuxs-blog-design.md)
- [项目路线图](docs/superpowers/plans/2026-08-13-qiuxs-blog-roadmap.md)
