# 配置规范

- 配置统一由环境变量加载，`.env` 只能本地使用且必须 gitignored；禁止代码硬编码密码、Token、真实域名密钥。
- 新增环境变量必须同步：配置结构、加载/校验、`.env.example`、README 和 Jenkins credentials 文档。
- 启动时校验环境变量，错误只指出字段和原因，不回显 secret 值。
- HTTP Origin、Cookie Secure、Redis DB、MySQL DSN、Bundle/Callback/Jenkins 配置必须在构造阶段验证。
- 站点备案默认值为 `长安休息室` / `浙ICP备17057726号-1`，但运行时可由后台设置覆盖；无公安联网备案号时不要虚构字段。
