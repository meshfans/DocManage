# ADR-0005：审计日志采用 append-only 哈希链（SM3）

- 状态：Accepted
- 日期：2026-08-19

## 背景与问题陈述

DocManage 处理法律合同 / 客户敏感信息，受 L4 司法合规约束。所有"谁在何时对什么做了什么"的操作必须可追溯、不可篡改。

传统方案：

- **写日志**（text file / DB row）— 任何拿到 DB 写权限的人可改
- **签名 + 时间戳**（TSA）— 需外部 TSA 服务，部署成本高
- **区块链** — 部署复杂、性能低

本项目希望：

- 单部署实例（无外部依赖）
- 整库备份后仍能验证日志完整性
- 写入性能可接受（业务事件 < 1000/s）

## 决策驱动

- L4 合规：审计日志必须能"事后验真"
- 部署独立：单进程可启动，不依赖外部时间戳服务
- 司法可读：哈希链与原始记录可在法庭上展示，无需第三方证明
- 加密合规：国密 SM3（项目方客户多为政务/国企）

## 考虑的选项

### 选项 A：纯 DB 行 + 时间戳

- 优点：实现简单
- 缺点：DB 写权限即可改；时间戳可伪造

### 选项 B：外部 RFC 3161 TSA 签名

- 优点：司法级权威
- 缺点：需外部服务，无法离线部署

### 选项 C：append-only 哈希链（已选）

- 优点：单进程可生成可验证链；改一行 = 整链断裂
- 缺点：每次写入需要等前一行的 hash（串行化）；需要 DB 触发器兜底

## 决策结果

**单进程全局 SM3 哈希链 + DB 触发器兜底 append-only**。

数据结构（`audit_log` 表）：

```
id              INTEGER PRIMARY KEY
target_type     TEXT       -- media / signature / contract / auth / ...
target_id       INTEGER
action          TEXT
actor_id        INTEGER
actor_ip        TEXT
user_agent      TEXT
detail          TEXT       -- JSON
hash_sm3        TEXT       -- 本行 SM3(prev_hash + fields)
prev_hash       TEXT       -- 上一行 hash_sm3（首行 = 'GENESIS'）
created_at      INTEGER    -- unix seconds
```

链算法：

```
hash_sm3 = SM3(
  L%020d|prev_hash
  L%020d|target_type
  L%020d|target_id
  L%020d|action
  L%020d|actor_id
  L%020d|actor_ip
  L%020d|user_agent
  L%020d|created_at
  L%020d|detail
)
```

- **length-prefix 编码**：避免字段拼接歧义（与 GIT 树对象哈希一致）
- **全局 mutex `auditLogMu` + 单一事务**：保证 prev_hash 顺序
- **DB 触发器 `trg_audit_log_no_update` / `trg_audit_log_no_delete`**：兜底 append-only
- **重算验证**：`VerifyAuditChain()` 遍历全表逐行重算；失败 → 断裂点 id

### 验证

- 正常：`POST /api/audit/reconcile` → 200 `{ok: true}`
- 篡改：触发器 DROP 后手动 UPDATE → reconcile 报 500，HTTP Header `X-Audit-Broken-At: 12`

## 后果

### 正面

- 单部署无外部依赖
- 整库可拷备份（审计 + 业务同库）
- 司法 L4 合规（哈希链 + 触发器双保险）
- 验证一次 < 100ms（10 万行测试）

### 负面

- 写入串行化：高并发场景下 audit_log 写入是瓶颈（实测 1000 行/s ≈ 200ms/批）
- BREAKING CHANGE 升级：算法变更需清空表（详见 `database/audit_log.go` 函数头注释）
- 字段扩展受 length-prefix 限制：新增字段需重新计算全部 hash

### 中性

- 与 ADR-0001 联动：SQLite 触发器 + 事务隔离保证一致性
- 与 ADR-0003 联动：审计 INSERT 强依赖 SQL 显式事务
- 与 ADR-0004 联动：审计重构字段进入 `business_events_total` 标签

## 后续

- 触发条件：审计写入 > 1 万行/秒 → 评估 sharding 按 target_type 分段链
- 触发条件：合规要求跨实例验证 → 引入"周日级别 checkpoint"（整链 hash 提交外部 TSA）
- 触发条件：业务需要"按 target 单独验证" → 评估 per-target 链 + 全局锚点
