# ADR-0003：不用 ORM，手写 SQL

- 状态：Accepted
- 日期：2026-08-19

## 背景与问题陈述

DocManage 后端数据访问层（`backend/database/`）当前采用 **database/sql + 手写 SQL** 模式。
仓库内没有引入 GORM / ent / sqlx / squirrel 等 ORM / Query Builder 库。

需要明确：**为什么不用 ORM**，以及**什么情况下应当重新评估**。

## 决策驱动

- SQL 透明度：业务复杂查询（多表 JOIN、子查询、窗口函数）需显式可读
- 性能可控：避免 ORM 自动 N+1、隐藏的 prepared statement、reflection 成本
- 审计要求：append-only 哈希链（ADR-0005）依赖 SQL 层的显式 INSERT
- schema 演进：业务表迭代频繁，手写 migration 比 ORM auto-migrate 更可控

## 考虑的选项

### 选项 A：GORM

- 优点：生态最大、API 友好、auto-migrate
- 缺点：反射成本高、复杂 SQL 仍需 Raw、隐藏 N+1、`Pluck`/`Joins` API 容易误用
- 与 audit_log 哈希链冲突：需要事务内查 prev_hash → INSERT，GORM 的 hook 抽象会增加心智负担

### 选项 B：ent（Facebook）

- 优点：schema-first、代码生成、类型安全
- 缺点：学习曲线陡、生成代码量大、对非常规 SQL（审计/报表）不友好

### 选项 C：sqlx / squirrel

- 优点：轻量、补齐 database/sql 缺失（struct scan、query builder）
- 缺点：仍需开发者手写 SQL，且会引入新的"半 ORM"心智

### 选项 D：database/sql + 手写 SQL（已选）

- 优点：完全透明、零反射、易于 SQL 优化、易于 DBA review
- 缺点：boilerplate 多（scan/insert 需要写字段映射）

## 决策结果

**保留手写 SQL**，但通过以下方式减少 boilerplate：

- `database/db.go` 抽 `QueryRow` / `Exec` / `Query` 公共 helper
- `database/scan.go`（待补）提供 `scanStruct(rows, &dest)` 通用 scan
- 表结构变更走 `database/init.sql` + golang-migrate（计划中，ADR-0001 后续）
- 业务层 model 维持纯 struct，由 `database/*.go` 负责 SQL

## 后果

### 正面

- 性能：业务高峰热点查询（如 `customer/page`）走 query plan + 索引，阶段耗时可控
- 安全性：所有 SQL 显式，SQL 注入面小（参数化严格）
- 审计：审计表 INSERT 走显式事务，哈希链算法变更易追踪

### 负面

- 重复代码：每个查询写一遍 Scan
- 重构成本：表 schema 变更 → 需人工改 N 处 Scan 代码
- 新人上手需熟悉 SQL syntax

### 中性

- 与 ADR-0001 联动：SQLite-only 函数（`json_extract`、`IFNULL`）需注意兼容性
- 与 ADR-0005 联动：审计 append-only 强依赖 SQL 层

## 后续

- 触发条件：业务表超过 50 张、boilerplate 显著拉低迭代速度 → 评估 sqlx
- 触发条件：需要 compiled query 的强类型 → 评估 sqlc（不是 ORM，是 SQL → 代码生成）
- 不引入 ORM 的红线：审计表 INSERT 必须在显式事务内执行
