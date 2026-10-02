# SQL 全局跟踪（debug only）

> 2026-10-02 L-7 补文档：原 `backend/database/sql_debug.go` 用 `//go:build sqlite_trace` 隔离，
> 不写文档无人知道怎么用。本篇记录如何打开 SQL 全局 trace。

## 适用场景

- 排查"为什么这条 SQL 被发了"——例如某次请求耗时突增，需要拿到真实 SQL（带参数展开）
- 验证 ORM / 手写 SQL 是否走了预期路径
- 性能回归基线——trace 后做 EXPLAIN QUERY PLAN

**严禁在生产开启**：trace 回调会打印每条 SQL 到日志，**含参数值**，PII / 凭据 / 业务数据全部暴露。

## 启用步骤

### 1. 构建带 trace tag 的二进制

```bash
cd backend
go build -tags sqlite_trace -o bin/doc-server-trace .
```

或本地运行：

```bash
go run -tags sqlite_trace ./
```

不写 `-tags sqlite_trace` → `sql_debug.go` 不编译 → 全无副作用。

### 2. 启动期开启 trace driver

构建产物跑起来之前 export：

```bash
# Linux / macOS
export SQL_DEBUG=1
./doc-server-trace

# Windows PowerShell
$env:SQL_DEBUG = "1"
./doc-server-trace.exe
```

不设 `SQL_DEBUG=1` → 走原 `sqlite3` driver，无 trace。

### 3. 抓 SQL 输出

trace 走 `utils.Debug`，所以：

- `cfg.Log.Level = "debug"` → 落到日志文件 + console
- `cfg.Log.Level = "info"` → 只 console，不写文件（避免日志膨胀）

输出格式：

```
[SQL TRACE] SELECT id, name FROM ai_config WHERE deleted_at IS NULL AND provider = ?
[SQL TRACE] INSERT INTO customer (...) VALUES (?, ?, ?, ...)
```

参数用 `?` 占位，trace driver 内部展开为实际值（`info.ExpandedSQL`）。

## 已过滤的事件

下列内部查询默认不打印（避免日志噪声）：

- `PRAGMA ...` （sqlite 内部元数据查询）
- `SELECT 'sqlite_...'` （sqlite 自检）
- `--` 开头（注释）

## 文件位置

| 文件 | 作用 |
|---|---|
| [backend/database/sql_debug.go](file:///F:/Code/DocManageTrail/backend/database/sql_debug.go) | build tag 隔离的 trace driver 注册 |
| `backend/database/sqlite.go` | 原 sqlite3 driver 注册（默认路径） |

## 替代方案

如不能改 build tag，**短期**方案：

- 在 [backend/database/sqlite.go](file:///F:/Code/DocManageTrail/backend/database/sqlite.go) 的 `RegisterDriver` 后手动塞 `SetTrace`
- 不推荐：污染主代码路径，下次升级需 revert

**长期**方案：把 `SQL_DEBUG` 与 `utils.Debug` 联动（build 时始终编译，运行时按 env 切换），
已在 ADR-XXXX（待补）讨论。

## 关闭 trace

- unset `SQL_DEBUG` / 重启服务 / 换回默认构建
- 确认 `info` 级别日志不再膨胀

## 已知坑

1. **trace 自身有开销**——`SetTrace` 回调每个 stmt 都调一次，本地测大概 +5% CPU
2. **长查询展开失败**——某些 bound 参数展开失败时回退到原 SQL（带 `?`）
3. **PRAGMA 过滤依赖前缀匹配**——`PRAGMA_FOO`（无空格）不会被过滤，但实践中很少见
