# Manual SQL lifecycle

- Append all unreleased DDL only to `develop/develop.sql`.
- Never edit a file under `releases/` after it has been committed.
- A future repository `server-release` skill validates a `vMAJOR.MINOR[.PATCH]` version, refuses to overwrite an existing release, copies the non-placeholder develop SQL to `releases/<version>.sql`, and resets develop.sql to its four-line header.
- The release skill is later adapted from `/Users/qiuxs/codes/qiuxs/account-book-cc-workspace/.claude/skills/server-release`.
- The release skill never connects to MySQL. The operator reviews and executes each release SQL manually.
- Do not add a migration library, migration CLI, Down script, automatic rollback, or startup DDL execution.
