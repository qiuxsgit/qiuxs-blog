# 数据库规范

- 使用 MySQL 8 + `database/sql`，禁止 ORM、query builder 和自动迁移。
- DDL 只提交到 `service/sqls/develop/develop.sql`；发布时由人工将其冻结到
  `service/sqls/releases/v<version>.sql`，然后重新建立 develop 占位文件。
- 所有主键为 `BIGINT NOT NULL`：禁止 `AUTO_INCREMENT`、禁止 `UNSIGNED`。主键由 Redis idgen 在 INSERT 前生成，key 使用 `idseq:<真实表名>`。
- **禁止外键约束**：DDL 中不得出现 `FOREIGN KEY` 或 `REFERENCES`。文章、媒体、Release 等参照完整性由应用层 service/repository 保证，关联列仍保留必要的普通索引。表统一使用 InnoDB、utf8mb4。
- DSN 必须启用 `parseTime=true`；时间排序使用 `created_at`/业务时间和稳定 tie-breaker，不用 Redis ID 表达时间先后。
- 更新判断不存在时，先查询或使用明确的条件诊断；不得把 MySQL `RowsAffected()==0` 直接当作不存在。
- 1062 必须按索引名区分 PRIMARY 和业务唯一键，不能把所有重复键都映射成同一领域错误。
- 每个查询都检查 query/scan/rows 错误并回滚事务；多表写入保持固定锁顺序和完整 rollback。
