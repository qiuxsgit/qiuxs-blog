# Go 编码规范

## 结构与依赖

- 入口在 `service/cmd/`；业务代码位于 `service/internal/<domain>/`。
- 依赖方向固定为 handler → service → repository；service 不依赖 Gin、HTTP 类型或 SQL 细节。
- Service 依赖接口，具体 MySQL/Redis 实现通过构造器注入；禁止包级可变状态。
- 所有 I/O 函数的第一个参数是 `context.Context`。

## 命名、错误和日志

- 包名短、小写；导出符号 PascalCase，非导出符号 camelCase；错误使用 `ErrXxx` 哨兵或具名错误类型。
- 不得无注释地用 `_` 丢弃错误；跨层包装使用 `fmt.Errorf("domain.Method: %w", err)`。
- handler 负责把领域错误映射为 Problem/status；service 不导入 `net/http` 或 Gin。
- 日志使用 `log/slog`；禁止把密码、Token、签名、原始 URL query、OSS 签名 URL 写入日志。
- `rows.Close`、`rows.Err`、HTTP response body close 必须处理；只读 cursor 的刻意忽略要有注释。

## 测试和格式

- 业务测试优先 table-driven + fake/stub；数据库使用 sqlmock，Redis 使用 miniredis。
- `gofmt -l .` 必须无输出，`go vet ./...` 必须通过；发布门禁使用 Go 1.25.7。
- 测试不连接真实基础设施；集成测试若存在必须显式 build tag。
