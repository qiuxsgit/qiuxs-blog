# React Admin 编码规范

- 使用 TypeScript 严格类型；API 只能经 `src/api` adapter，不能在页面散落 `fetch`。
- 先更新 OpenAPI，再重新生成 `src/api/generated/admin.ts`；生成文件禁止手改。
- 所有请求透传 `AbortSignal`，统一处理 Problem、requestId、401 session 过期和响应 shape 校验。
- 纯函数优先：缓存、转换、校验、错误映射单独放在可测试模块。
- 遵循用户测试策略：只写纯函数/API adapter/cache 测试；不增加 React/DOM/UI/Playwright/e2e 测试，页面由人工验收。
- Node 固定 `20.19.4`；`npm ci` 使用锁文件，构建前执行 API 生成并检查无 diff。
