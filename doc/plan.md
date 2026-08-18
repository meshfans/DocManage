# 可执行规划（基座脚手架增强）

> 范围：只保留当前阶段可独立落地、有明确改造点的项。  
> 状态基准：R14 TraceID + R15 探针 + R16 业务/基础设施指标 + Docker 部署 已完成。

---

## 当前阶段（剩余可立即执行）

### 可观测性收尾

- [ ] **审计日志查询 UI**：`audit_log` 表 + `database.AppendAudit` 已有，补 `handlers/audit.go` + `frontend/views/system/audit.vue`，支持按用户/时间/动作筛选与导出 CSV。
- [ ] **业务事件总线**：抽出 `services/eventbus.go`（channel-based publish/subscribe），与 WS Hub 解耦；业务模块可独立订阅事件做本地处理。

### 安全加固

- [ ] **API 签名 / 防重放**：timestamp + nonce + HMAC-SHA256 中间件，nonce 落 Redis/SQLite 短期缓存，附 `X-Signature` 头校验；默认关闭，按路由 opt-in。
- [ ] **密码策略**：长度 ≥ 10、复杂度正则、历史 5 次、90 天过期提示，配置 `backend/config/security.json`。
- [ ] **2FA / TOTP**：管理员登录强制；用 `pquerna/otp`，绑定页走 `handlers/auth_totp.go` + 前端扫码组件。
- [ ] **会话管理 UI**：用户查看"我的登录设备"，管理员在 `views/system/session.vue` 一键踢出；后端 JWT 黑名单落 `revoked_jwt` 表。
- [ ] **IP 白名单**：`middleware/accessctl.go` 扩展现有 origin 白名单，对 `/api/admin/*` 做 CIDR 匹配。

### 数据生命周期

- [ ] **软删除回收站 helper**：抽取 `database/softdelete.go`（`SoftDelete(model, id)` / `Restore(id)` / `Purge(olderThan)`），覆盖 customer / contract / media。
- [ ] **数据库迁移框架**：引入 `golang-migrate/migrate`，将 `database/init.sql` + `ALTER` 拼接替换为 `migrations/0001_init.up.sql` … 版本化管理。
- [ ] **字段级加密**：身份证 / 手机 / 银行卡 AES-256-GCM；密钥从 `config.security.master_key` 读取（启动时校验长度）。
- [ ] **多租户基线**：在核心业务表加 `tenant_id`，写一个 `middleware/tenant.go` 注入上下文，`data_scope_helpers.go` 自动追加行级过滤。

### 稳定性

- [ ] **熔断 / 降级**：基于 `sony/gobreaker` 包装 `services/breaker.go`，给第三方合同 / 备份上传 / 邮件 / Webhook 出站加熔断；降级时返回兜底响应 + `business_events_total{event="service.degraded"}`。
- [ ] **优雅关停补全**：`main.go` 注册 `signal.Notify`，按顺序关闭 HTTP server（drain in-flight）→ WS Hub → Scheduler → DB；每个阶段埋点 `shutdown.stage.duration_seconds`。
- [ ] **分布式锁**：`services/redislock.go`（或 SQLite advisory lock），Scheduler 启动前抢锁；多副本部署时只有持锁实例执行任务。
- [ ] **慢查询日志**：DB 层封装 `QueryWithTiming(threshold)`，超 200ms 自动记录 SQL + 调用栈；指标 `db_slow_queries_total`。

### DX / 复用体验

- [ ] **业务模块脚手架 CLI**：`cmd/scaffold/main.go`，`doc-manage scaffold module <name>` 生成 handler / db / permission / seed / 前端 CRUD 模板。
- [ ] **统一错误码字典**：`utils/errors.go` 维护 `ErrCode` 常量 + i18n 映射，前端 `utils/error.ts` 按 code 文案翻译。
- [ ] **OpenAPI 自动生成**：`cmd/genopenapi/main.go` 解析 handler 注释生成 `doc/openapi.yaml`，前端 codegen + Postman 自动同步。
- [ ] **Webhook 出站**：`services/webhook_outbound.go`，业务事件触发 HTTP 回调，签名 + 指数退避重试 + 死信表 `webhook_dead_letter`。

### 文档 / 部署

- [ ] **ADR 目录**：`docs/adr/0001-sqlite.md` / `0002-自研-websocket.md` / `0003-不用-orm.md` / `0004-手写-prometheus.md`。
- [ ] **业务接入手册集中**：将 `.trae/documents/` 的 handoff 系列索引化到 `doc/onboarding/`，配 mkdocs；保留 `.trae/documents/` 仅做当日交接。
- [ ] **CHANGELOG 自动生成**：`scripts/genchangelog.ps1` 从 commit message 聚合 breaking / feature / fix 三段，落到 `CHANGELOG.md`。
- [ ] **Postman 集合**：`doc/postman/observability.json` 起步，逐步覆盖 auth / customer / contract / backup。

---

## 下一阶段（评估后启动）

- [ ] **手写指标 → 官方库迁移**：触发条件 = 需要 OpenMetrics / Histogram quantile；保留 `business_events_total` 命名兼容。
- [ ] **WebSocket 多实例**：单实例瓶颈（连接数 / CPU）出现后评估 `redis-pubsub` / NATS 做实例间消息分发。
- [ ] **Prometheus 告警规则**：`backend/monitoring/alerts.yml` 覆盖 auth 失败突增、备份失败、WS 高连接、Scheduler 不在跑。
- [ ] **Grafana 仪表盘**：`backend/monitoring/grafana_dashboard.json`（5 面板：QPS/P99、WS 在线、DB 池、Scheduler、备份结果）。
- [ ] **日志聚合**：结构化日志 → Loki / ELK，TraceID 字段关联。
- [ ] **业务配置中心 Feature Flag**：`system_config` 增加 `flag.*` 命名空间 + 用户分群规则。

---

## 启动顺序（2 周）

| 周次 | 任务 | 交付 |
|---|---|---|
| 第 1 周 | 审计日志 UI + 业务事件总线 + 优雅关停补全 | 可观测性闭环收尾 |
| 第 1 周 | 错误码字典 + OpenAPI 生成 + ADR 目录 | DX 基线 |
| 第 2 周 | 软删除回收站 helper + 数据库迁移框架 | 数据生命周期基线 |
| 第 2 周 | 密码策略 + JWT 黑名单 + 会话管理 UI | 安全基线 |

> 注：API 签名、2FA、多租户、字段加密、Webhook 出站属于"独立可插入"模块，按业务接入顺序排期。