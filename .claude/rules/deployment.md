# Jenkins、Nginx 和部署规范

- Jenkins 必须验证 Go 1.25.7、Node 20.19.4；凭据使用 Jenkins credentials，不落 Git。
- Service 通过 `root@blogweb1` 发布到 `/web/deploy/blog`；Admin/Site 通过 `root@ngx1` 发布到对应静态根目录。
- 部署脚本必须校验 host/root/release token，使用临时 staging、传输后检查，再原子切换 `current`。
- 任何 transfer、健康检查、Bundle 校验或 callback 失败都必须保留旧 current，并清理 staging。
- Nginx Admin 只代理 `/api/`；公开域名只代理 `/img/proxy/`，公开域名的 `/api/` 必须明确拒绝；代理 location 使用 `^~` 防止静态正则抢匹配。
- HTML 使用 no-cache，带 hash 的静态资源 immutable；拒绝 dotfiles；限制 body、连接和读取超时。
- Release callback 必须携带正确的 `releaseId`、`publishJobId`、`buildNumber`、stage/status，按 Service canonical JSON/HMAC 规则签名；不记录 Token、nonce、签名或 query。
- 真实 SSH/Jenkins/Nginx reload 是人工触发操作；自动化测试只能使用 fake SSH、临时目录和静态契约检查。
