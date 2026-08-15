# Astro 静态站规范

- `site/` 必须保持 `output: "static"`；禁止 SSR adapter、运行时内容 API 和浏览器端 Markdown 解析。
- 构建输入只能是经过 schema、ID、备案字段、URL 和 checksum 校验的 Release Bundle。
- Markdown 在构建期渲染；拒绝 raw HTML、危险 URL、MDX、Mermaid、公式和自定义组件。
- 图片保留 `/img/proxy/{publicKey}` 稳定地址，不把 OSS 私有签名 URL写入公开内容。
- 每个公共模板都要渲染非空备案名称/备案号，并链接 `https://beian.miit.gov.cn/`。
- Node 固定 `20.19.4`；只保留纯 Bundle/Markdown/content/产物 gate 测试，不做 UI 自动验收。
