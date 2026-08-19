# ADR-0001：采用 SQLite 作为主存储

- 状态：Accepted
- 日期：2026-08-19
- 决策人：DocManage 项目组

## 背景与问题陈述

DocManage 定位为"中小律所/团队"自托管的合同/客户管理系统，部署约束：

- 客户以"单事务所 / 单服务器"为主，无需跨节点分片
- 部署目标：Windows 桌面服务器 / 单机 Linux / 内网环境，无法依赖外部 MySQL / PostgreSQL
- 数据量级：单实例 10 万级业务记录、5 万级媒体文件（< 50 GB）
- 备份诉求：单文件可拷贝、可加密、可传到 OSS

需要决定：**PostgreSQL / MySQL / SQLite** 哪个作为默认存储后端。

## 决策驱动

- 零外部依赖：客户运维技能参差，部署"开箱即用"
- 单文件备份：可直接 `cp doc.db backup/` 或纳入 WORM 存储
- 读写延迟可接受：业务高峰 < 100 QPS，单事务 < 50ms
- 牺牲：跨节点扩展性、复杂 JOIN 多表大数据分析（不进入本期范围）

## 考虑的选项

### 选项 A：PostgreSQL

- 优点：成熟、强大、生态完整、维护工具丰富
- 缺点：客户端必须安装并配置；中小客户运维阻力大；备份需 `pg_dump` 链路

### 选项 B：MySQL

- 优点：与选项 A 类似，运维工具更普及
- 缺点：同样存在客户端依赖；与 SQLite 的"单文件"优势差距巨大

### 选项 C：SQLite（已选）

- 优点：单文件、零进程依赖、`database/sql` 标准接口、与 Go 集成最自然
- 缺点：单写并发（≥ 100 写/秒需 WAL 优化）；不适合跨节点
- 适配点：写并发通过 `journal_mode=WAL` + `SetMaxOpenConns` 限制，已在
  `database/db.go` InitDatabase 中配置；连接池等待计数 `db_wait_count_total`
  进入 Round 16 指标用于监控

## 决策结果

**采用 SQLite 3 + WAL 模式**。配置：

- `journal_mode=WAL`：读写并发
- `synchronous=NORMAL`：权衡 fsync 性能（默认 FULL 太慢）
- `_busy_timeout=5000`：写锁等待 5s，避免请求快速失败
- `cache_size=-20000`：20 MB page cache
- `foreign_keys=ON`：与 PostgreSQL 行为对齐

读写隔离由 `sql.Tx` + app-level mutex 补充（如 audit_log 哈希链全局锁）。

## 后果

### 正面

- 部署：单文件 `doc.db` 即可，整库随 WORM 备份
- 性能：业务场景下 P99 < 20ms
- 一致性：WAL + 事务隔离满足 L4 合规
- 成本：零数据库授权费用

### 负面

- 跨节点扩展成本高：未来如需多副本，需引入 Litestream / LiteFS
- 备份方式仅支持整库拷贝：增量备份（WAL shipping）需自行实现
- 大表（> 1 亿行）需 careful schema 设计：本期不会触发

### 中性

- 后续如需 Postgres 兼容，SQL 写法需 review（避免 SQLite-only 函数）
- 测试要 in-process 跑（不能用 testcontainers 等容器化方案）

## 后续

- ADR 触发条件：业务量级达到 1 亿行 / 1000 QPS 时，复评 PostgreSQL
- WAL 文件目录 `backend/bin/data/` 已纳入 `.gitignore`
