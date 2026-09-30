# DocManageTrail — 合同与文档管理平台

> 📄 面向中小企业的三方合同 / 文档签署与归档管理平台，Go 后端 + Vue 3 前端，**单 exe 私有部署**。
> 📊 后端 Go ≈ 11,000 行 + 前端 Vue/TS ≈ 9,000 行；后端 ~113 protected API + 前端 22 个路由。
> 📜 许可证 [Apache License 2.0](./LICENSE)（前端模板 [vue-pure-admin-thin](https://github.com/pure-admin/vue-pure-admin-thin) 沿用上游 MIT，见 [frontend/LICENSE](./frontend/LICENSE)）。

---

## 📑 目录

- [一、产品简介](#一产品简介)
- [二、核心能力与适用场景](#二核心能力与适用场景)
- [三、产品亮点](#三产品亮点)
- [四、技术栈](#四技术栈)
- [五、子项目说明](#五子项目说明)
- [七、快速开始](#七快速开始)
- [八、目录结构](#八目录结构)
- [九、API 速览](#九api-速览)
- [十、构建与发布](#十构建与发布)
- [十一、许可证](#十一许可证)
- [十二、联系方式](#十二联系方式)

---

# 一、产品简介

## 1.1 一句话定位

**DocManageTrail 是一套「私有部署、聚焦三方合同」的企业文档管理平台**：把三方合同 PDF + 客户档案 + 媒体证据 + 定时提醒 + 自动化备份装进一台内网服务器。

**🚀 一键交付单文件 exe**：通过根目录 `package.ps1` 一条命令即可生成 `DocServer-<ver>-<date>.exe`，前端（Vue 3）+ 后端（Go 1.26 + Gin + SQLite）+ 三哈希存证 + RBAC v2/v3 + 备份 + 定时任务全部打包进单 exe，双击即跑，适合内网单机部署与快速分发。

## 1.2 系统功能说明

本系统已集成企业级基础功能模块，包括但不限于：

- **RBAC 权限管理**：API 级权限码 1:1 对应 `permission` 表，中间件按 `method+path` 自动校验；内置 `admin` / `manager` / `common` 三种角色 + 自定义角色。
- **`data_scope` 数据权限**：`role.data_scope` 提供 6 个模式（`all` / `dept_and_sub` / `dept` / `self_and_sub_dept` / `self` / `custom`），4 资源（合同 / 客户 / 媒体 / 备份）已接入，5 分钟缓存 + `singleflight` 防惊群 + `custom` 部门白名单。
- **用户管理**：CRUD + 部门调整 + 角色分配 + 扩展字段（邮箱 / 电话 / 工号 / 职位 / 状态）+ JWT + 改密 / 刷新 token 限流。
- **客户管理**：个人 / 企业双类型客户档案，统一社会信用代码（USCC）部分唯一索引，主负责人（`owner_user_id`）+ 部门归属 + 软删除 + 扩展 CRUD + 按类型检索。
- **数据备份与恢复**：全量 / 增量备份 + 链式保留（`keep_until` 索引自动清理）+ 备份清单三哈希（SM3 / SHA-256 / Combined）存证 + 月度自动演练 + 维护模式 P0（恢复期间拦截业务 API）+ 一键恢复。
- **定时任务调度**：基于 `robfig/cron/v3` 的 cron 调度框架 + 任务定义表 + 执行日志 + 变更审计 + 内置 `reminder_scan` / `backup` 等系统任务 + 全生命周期管理。

> 该系统架构设计可作为企业级后台管理系统的标准化开发基座，支持快速构建各类业务管理平台。

## 1.3 适合谁用

| 类型 | 适用度 | 典型场景 |
|------|--------|----------|
| **中小企业**（金融、租赁、教育、医疗、采购、HR、地产中介）| ⭐⭐⭐⭐⭐ | 月接收 50–5000 份第三方 PDF 合同 |
| **政企机关 / 央国企子公司** | ⭐⭐⭐⭐⭐ | 数据不能上公有云，需私有部署 |
| **集团总部对子公司的合规管控** | ⭐⭐⭐⭐ | 跨地域归档 + 总部审计 |
| **律所 / 会计师事务所** | ⭐⭐⭐⭐ | 批量归档客户合同 + 证据链 |
| **需要电子签 / 在线编辑的场景** | ❌ 不适用 | 可考虑联系作者定制购买私有版 |

---

# 二、核心能力与适用场景

## 2.1 业务能力 × 解决方案 + 后端模块

| # | 痛点 | 解决方案 |
|---|------|----------|
| 1 | **第三方合同 PDF 散落各处** | 三方合同管理系统：5 状态机（draft → signed → archived → expired/replaced）+ PDF 上传 + 三哈希存证 + 客户关联 |
| 2 | **公有云 SaaS「数据不在自己手里」** | 私有部署：单 exe 跑在内网，物理隔离互联网 |
| 3 | **合同被篡改无法自证** | 三哈希存证：每份入库时算 `SM3 + SHA-256 + Combined = SHA256(SM3 ∥ SHA256)` 写入 `third_party_contract` |
| 4 | **客户档案碎片化** | 个人 / 企业双类型客户档案，`customer_type` 字段一次填写跨合同复用，含 USCC 部分唯一索引 |
| 5 | **数据库丢失 / 损坏后无法恢复** | 全/增量备份 + 链式保留 + 三哈希存证 + 月度演练 + 维护模式（恢复期拦截业务 API）|
| 6 | **合同到期无人提醒** | 定时提醒：模板 + 订阅 + 扫描器 + 多接收人分发，到期前自动推送 |
| 7 | **多部门权限难管控** | RBAC v2（API gate 按 method+path 校验 permission_code）+ RBAC v3（数据级 data_scope 6 个模式 + custom 部门白名单）|
| 8 | **登录爆破 / 改密爆破** | 登录 / 改密 / 刷新 token 三类操作限流（按 IP + 账号）+ JWT 黑名单 GC |
| 9 | **审计追溯缺失** | `audit_log` 表 + SM3 哈希链 + DB 触发器禁止 UPDATE/DELETE |

**后端 14 个模块 ~113 个 protected API**（统一前缀 `/api`，鉴权由 JWT 中间件 + RBAC v2 API gate 保护）：

| 模块 | 端点 | 说明 |
|------|------|------|
| **认证** | 6 | 登录 / 登出 / 改密 / 刷新 / 用户信息 / 异步路由 |
| **用户** | 10 | CRUD + 部门 + 角色 |
| **部门** | 6 | 树形 CRUD + 用户列表 |
| **客户** | 10 | 个人 / 企业双类型 + 扩展 CRUD |
| **三方文档** | 9 | 5 状态机 + PDF 上传 / 下载 / 三哈希 / 批量下载 |
| **媒体库** | 16 | 照片 / 录像 / 音频 + 缩略图 + 哈希链 |
| **消息** | 7 | 站内信全 CRUD + 已读 / 未读 |
| **提醒** | 10 | 模板 + 订阅 + 日志 + 扫描 |
| **定时任务** | 11 | cron 调度 + 日志 + 审计 |
| **系统配置** | 4 | 公开 / 列表 / 更新 / 删除 |
| **系统** | 10 | 配置 / 维护模式 / 备份 / 恢复 / SSL / 关闭 |
| **备份** | 4 | 列表 / 详情 / 下载 / 删除 |
| **RBAC** | 7 | 角色 / 权限 / 检查 / 版本号 |
| **Hash** | 3 | SM3 / SHA-256 / Combined 计算 + 校验 + 算法列表 |
| 公开端点 | 1 | `GET /api/license/client-info` |
| 健康检查 | 1 | `GET /health` |

## 2.2 典型应用场景

- **消费金融小额贷款合同归档**：总部部署、分行接入、客户经理扫描 PDF 上传 → 三方合同；逾期纠纷时导出 PDF + 哈希自证。
- **医疗 / 教育知情同意书归档**：医院 / 学校内网部署、行政人员上传纸质签字 PDF 关联客户、合同到期前 30 / 15 / 7 天推送站内信。
- **采购合同合规管控**：业务员 / 采购经理 / 法务 三级权限隔离、每月自动备份 + 链式保留 + 月度演练。

---

# 三、产品亮点

## 3.1 与公有云电子签对比

| 维度 | 公有云电子签 | DocManageTrail |
|------|------------|----------------|
| **数据归属** | 服务商云端 | **企业内网**，数据自主可控 |
| **支持场景** | 在线电子签 + 模板 + 印章 + 证据 | 三方合同归档 + 客户 + 媒体证据（不做电子签）|
| **合规场景** | 不适合金融 / 政企敏感合同 | 私有部署，数据不出内网 |
| **计费模式** | 按份数收费（1–5 元 / 份）| 一次性买断 + 开源式部署，无份数限制 |
| **离线运行** | 不可，必须联网 | 完全离线可用，断网环境仍可归档 |
| **私有化定制** | 困难，需厂商配合 | 源码级可控，支持二次开发 |

## 3.2 技术亮点

1. **SM3 + SHA-256 + Combined 三哈希存证**：三方合同 / 媒体入库自动算三哈希写入数据库，校验 API 重算比对。
2. **RBAC v2 + v3 双层**：v2 每个 protected 路由 1:1 对应一条 `permission` 记录，中间件按 `method+path` 自动校验；v3 `role.data_scope` 6 个模式 + 30s 缓存 + `singleflight` 防惊群。
3. **WAL/DELETE 双模式 + 链式增量备份**：默认 `DELETE` journal（兼容性最好），可改 `WAL`（高并发 + 增量备份），链式保留 `keep_until` 索引 + 自动清理。
4. **维护模式 P0**：恢复期间拦截所有业务 API，仅放行 `/health` + `/api/system/maintenance`，避免数据漂移。
5. **限流 + JWT 黑名单 GC**：登录 / 改密 / 刷新 token 按 IP + 账号限流；JWT 黑名单后台 GC 清理已过期项避免内存累积。
6. **新消息提示音**：Web Audio API 机器合成「叮咚」音效，业务配置开关控制。
7. **业务配置按值类型渲染**：bool/enum/number/text 四种编辑组件，后端 `config_defaults.go` 一行注册。

## 3.3 交付形态

| 形态 | 适用 | 部署难度 |
|------|------|---------|
| **单文件 exe**（Go 含前端 + SQLite + 业务）| 中小企业首选 | ⭐ 双击即用 |
| **Docker 镜像** | 有运维团队的企业 | ⭐⭐ `docker-compose up` |
| **源码 + 二进制混合** | 集成商、ISV、二次开发 | ⭐⭐⭐ Git clone + `package.ps1` |

---

## 3.4 近期更新（2026-08-22 ~ 2026-09-04）

### 新功能（4 项）

| # | 功能 | 描述 |
|---|------|------|
| 1 | **新消息提示音** | Web Audio API 机器合成「叮咚」音效，业务配置 `notification_sound_enabled` 开关控制 |
| 2 | **业务配置按值类型渲染** | 后端 `config_defaults.go` 新增 `ValueType` + `EnumOptions`，前端按 bool/enum/number/text 渲染 4 种编辑组件 |
| 3 | **消息角标统一重构** | 抽 `useMessageNotice` composable，WS 多订阅 Set，删「任务」tab，角标唯一权威 |
| 4 | **体验版 config.mode=test** | 显式声明 `mode:"test"`。⚠️ 2026-09-30 更新：切换 `experience` 演示模式**已不能再从「系统配置」页操作**——对齐 DocCRM 2026-09-23 P0，admin 在 UI 不再持有该字段（`SaveConfigFile` 也不回写），否则可一键绕过 `ExperienceReadOnly` 只读拦截。现在只能改磁盘 `config.json` 的 `database.mode` 后重启，或用 `DB_MODE` 环境变量覆盖；`backend/bin/config.json` 的 `mode` 仅作本地开发基线 |

### 遗留功能（2026-07-22 ~ 2026-08-22）

| # | 功能 | 描述 |
|---|------|------|
| 1 | **WebSocket 实时推送** | 消息通知、合同状态变更实时推送 |
| 2 | **事件总线** | 服务间事件解耦 |
| 3 | **优雅关停** | SIGTERM/SIGINT 优雅退出 |
| 4 | **错误码字典** | 统一错误响应格式 |
| 5 | **审计日志 UI** | 前端审计日志查询界面 |
| 6 | **Docker 可观测性** | Prometheus + 告警规则部署 |
| 7 | **WORM 存储** | append-only + 路径锁 + DB 哈希记录 |
| 8 | **ADR 架构决策记录** | 5 篇架构决策文档 |
| 9 | **业务事件埋点** | 关键操作事件追踪 |
| 10 | **基础设施指标** | DB/WS/Scheduler Prometheus 指标 |
| 11 | **体验模式** | 免登录快速预览 |
| 12 | **登录自愈** | 失败后自动重试机制 |
| 13 | **User 扩展字段** | FailedLoginCount / LockedUntil |
| 14 | **审计哈希链** | SM3 append-only 审计链 |
| 15 | **4 条业务告警规则** | 合同到期/WORM失败等 |

### 安全修复（11 项）

| # | 修复 | 优先级 |
|---|------|--------|
| 1 | **JWT 算法白名单** | Critical |
| 2 | **JWT_SECRET env 强制** | High |
| 3 | **JWT issuer 校验** | High |
| 4 | **Secret 长度检查** | High |
| 5 | **Clock skew 容错** | High |
| 6 | **Refresh token rotation** | High |
| 7 | **登录锁定机制** | High |
| 8 | **JTI 防重放** | High |
| 9 | **Base64 上限限制** | High |
| 10 | **Refresh token snake_case** | High |
| 11 | **Admin DB-backed 校验** | High |

### RBAC 加固（8 项）

| # | 模块 | 修复 |
|---|------|------|
| 1 | SetConfig | admin gate |
| 2 | UploadSignature | 权限码校验 |
| 3 | GetUser | owner-only 限制 |
| 4 | CheckUsername | admin-only 限制 |
| 5 | reminder | 模块权限加固 |
| 6 | dept | 数据范围加固 |
| 7 | media | 数据范围加固 |
| 8 | GetSignature | owner 校验 |

---

# 四、技术栈

| 层 | 技术 | 版本 | 用途 |
|----|------|------|------|
| **后端** | Go | 1.26.2 | backend |
| | Gin | v1.12.0 | 路由 / 中间件 |
| | SQLite（mattn/go-sqlite3） | v1.14.22 | 嵌入式数据库（round7 移除 AES-256）|
| | emmansun/gmsm | v0.43.0 | 国密 SM3 |
| | golang-jwt/jwt v5 | v5.3.1 | 鉴权 + 黑名单 |
| | robfig/cron/v3 | v3.0.1 | 定时任务 |
| | sony/sonyflake | v1.3.0 | SnowID |
| **前端** | Vue 3 + TypeScript + Vite | 3.5.22 / 5.9.3 / 7.1.12 | SPA |
| | Element Plus | 2.11.5 | UI |
| | Pinia | 3.0.3 | store |
| | TailwindCSS | 4.1.16 | 样式 |
| | axios | 1.12.2 | HTTP |
| | Vitest + Playwright | 4.1.9 / 1.61.1 | 单元 / E2E |
| **模板** | 基于 [vue-pure-admin-thin](https://github.com/pure-admin/vue-pure-admin-thin) 6.2.0 改造 | | 非国际化版 |

---

# 五、子项目说明

```
DocManageTrail/
├── backend/                Go 1.26 业务后端 + 前端嵌入 + 单 exe 入口
├── frontend/               Vue 3 + TS 业务前端
├── package.ps1             一键发布（仅后端 Go + 前端 Vue）
├── go.work                 Go workspace
└── .trae/documents/        测试报告 + 交接文档
```

> **设计哲学**：业务代码全部在 `backend` + `frontend`；`backend/build.ps1` 自动 build 前端并把 `dist` 拷到 `backend/web/dist`，最后 `go build` 通过 `embed.FS` 把前端打入二进制，输出**单 exe**。

## 5.1 `backend/` — 业务后端

- **入口**：[backend/main.go](./backend/main.go)
- **配置**：[backend/bin/config.test-main.json](./backend/bin/config.test-main.json)（开发）/ [backend/bin/config.json](./backend/bin/config.json)（生产）
- **路由注册**：[backend/server/combined_server.go](./backend/server/combined_server.go)（~113 protected + 1 公开 + 1 健康检查）
- **Schema**：[backend/database/db.go](./backend/database/db.go)（**16 张表**）

```
backend/
├── main.go                          # 入口（DB + Scheduler + 黑名单 GC + JWT + RateLimit GC）
├── config/  database/  handlers/  services/  middleware/  models/  utils/  server/  web/
├── vendor/                          # 离线依赖
├── Dockerfile / docker-compose.yml
└── bin/                             # config.json + config.test-main.json + data/ + uploads/ + logs/ + backups/
```

**Schema 总览**（16 张表）：`users` / `messages` / `customer` / `system_config` / `department` / `scheduled_task` / `scheduled_task_log` / `scheduled_task_audit` / `reminder_template` / `reminder_subscription` / `reminder_log` / `media` / `third_party_contract` / `backup_manifest` / `role` / `permission`。


## 5.2 `frontend/` — 业务前端

Vue 3 + TS + Element Plus + Vite 7；版本 [frontend/package.json](./frontend/package.json) `6.2.0`。

- **入口**：[frontend/src/main.ts](./frontend/src/main.ts)
- **路由**：[frontend/src/router/modules/](./frontend/src/router/modules/)（9 个模块，22 个路由）
- **API 定义**：[frontend/src/api/](./frontend/src/api/)（13 个 API 客户端）

**页面（14 个 .vue）**：welcome/、login/、customer/（individual / enterprise / contracts）、contract/（ThirdPartyContractList / ThirdPartyContractDetail）、media-library/（MediaGrid / Filter / Uploader / Detail / Lightbox）、system/（user / department / config / backup / scheduledTask / reminder / audit）、rbac/（role / permission）、error/（403 / 404 / 500）。

---

# 七、快速开始

## 7.1 准备

| 依赖 | 版本 | 用途 |
|------|------|------|
| Go | 1.26+ | backend（`go.work` 声明 1.26.2）|
| Node.js | 20.19+ 或 22.13+ | frontend 构建 |
| pnpm | ≥9 | frontend 包管理 |
| GCC (MinGW / TDM-GCC) | Windows | CGO 编译（`go-sqlite3`）|


## 7.2 启动后端（开发模式）

按 [AGENTS.md](./AGENTS.md) 规范，**只通过 `run.ps1` 启动后端**：

```powershell
cd backend
.\run.ps1
```

`run.ps1` 会：1) 杀掉占用 8090 端口的旧进程；2) 设置 `CONFIG_FILE=bin/config.test-main.json`；3) 执行 `go run ./main.go`。

**端口与配置**：

| 项 | 值 |
|----|----|
| 监听地址 | `0.0.0.0:8090`（**非生产 8090**）|
| 配置文件 | `backend/bin/config.test-main.json` |
| 数据库文件 | `backend/bin/data/doc.db` |
| 上传 / WORM / 日志 / 备份 | `backend/bin/{uploads,uploads/worm,logs,backups}/` |

> 严禁 `cd backend && go run main.go`（会用默认 `config.json` 端口 8443，污染仓库根目录）。

## 7.3 启动前端（开发模式）

```powershell
cd frontend
pnpm install
pnpm dev
```

浏览器打开 `http://localhost:8848`（vite 默认端口）。

### 默认登录账号

种子用户由 [backend/database/db.go](./backend/database/db.go) 的 `seedDefaultUsers` 在首次启动时写入（仅当 `users` 表为空时触发）：

| 角色 | 用户名 | 密码 | 默认权限 |
|------|--------|------|----------|
| **超级管理员** | `admin` | `admin123` | `*:*:*` 通配符（全功能）|
| **普通用户** | `common` | `common123` | 消息 / 公开业务配置 / 自己信息 / 改密 |

> ⚠️ **生产环境部署前请立即修改默认密码！** 路径：登录后 → 「员工管理」→ 修改密码。
>
> 业务管理员 `manager` 角色由 [seed_rbac.go](./backend/database/seed_rbac.go) 写入 `role` 表，但默认**不创建用户**，需在「员工管理」手动创建并分配角色。

## 7.4 启动后验证

```bash
# 后端健康
curl http://localhost:8090/health
# {"status":"healthy","service":"doc-server","version":"1.0.0"}

# 登录
curl -X POST http://localhost:8090/api/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}'

# 用 token 访问三方合同列表
curl http://localhost:8090/api/third-party/contracts \
  -H "Authorization: Bearer <access_token>"
```

## 7.5 安全与运维说明

> 本节记录 Phase 4a/b、Round 19/20 引入的安全加固与运维要点。生产部署前必读。

### 7.5.1 JWT_SECRET 配置（生产强制）

自 Phase 4b 起，`JWT_SECRET` 通过环境变量注入，避免在配置文件中固化密钥：

| 优先级 | 来源 | 适用 |
|--------|------|------|
| 1 | 环境变量 `JWT_SECRET` | **生产强制**；`docker-compose.yml` 用 `${JWT_SECRET:?JWT_SECRET is required}` 强制注入 |
| 2 | `bin/config.json` / `bin/config.test-main.json` 的 `jwt.secret` 字段 | dev fallback（仅 `config.test-main.json` 保留）|
| 3 | `backend/run.ps1` 兜底 | 本地未设 env 且 config.json secret 为空时自动 export dev 密钥 + 红字警告 |

**行为**：

- env 非空 → 覆盖 `config.json` 任何 secret 值
- env 空 + `config.json` 有 secret → 用 `config.json`
- env 空 + `config.json` 空 → `panic("FATAL: JWT secret is required!")`，启动失败
- secret 长度 < 32 → warn 日志（仍可启动，但 prod 必查）

**生产部署**：

```bash
export JWT_SECRET=$(openssl rand -hex 32)
./doc-server
```

**Docker Compose**：必须在 `.env` 文件中设置 `JWT_SECRET=xxx`，否则启动失败。

### 7.5.2 JWT 升级期老 token 失效提示

Phase 4a 起 access/refresh token 必须含 `iss="doc-server"`。升级瞬间所有老 token（无 iss）全部失效，用户需重新登录。运维升级前必须广播：

- 升级时间窗口
- 升级后所有用户需重新登录
- 升级后 24 小时内 `auth.jwt.reject.invalid_issuer` 指标可能持续小量增长，属预期；如 24 小时后仍持续激增需检查前端 token 缓存策略

### 7.5.3 登录失败锁定

为防御密码爆破，登录失败 5 次后账号被临时锁定 15 分钟（常量定义于 [database/user.go](./backend/database/user.go) 的 `LoginMaxAttempts` / `LoginLockMinutes`）。

**响应字段**（登录失败时返回，前端可读取展示）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `remaining_attempts` | int | 剩余尝试次数（含本次失败后；触发锁定时为 0）|
| `locked_until` | int64 | 账号解锁时间（unix 秒；仅锁定状态下返回）|

> 用户不存在 / 账号禁用 / 旧密码错误 5 次前的失败响应**不**返回这些字段，避免泄漏账号存在性。

**监控指标**：`auth.login.locked`（账号锁定事件）、`auth.login.failed`、`auth.login.success`、`auth.login.unlocked`（从锁定恢复登录）。详见 [monitoring/alerts.yml](./backend/monitoring/alerts.yml) 的 `DocServerAccountLockoutsSpike` 告警规则。

### 7.5.4 Refresh Token 轮换

J.5 起刷新 token 时会**立即吊销旧 refresh token**，防止泄漏后无限续期：

- `POST /api/refresh-token` 成功响应返回新的 `access_token` + `refresh_token`
- 旧 `refresh_token` 加入黑名单（TTL = refresh token 剩余有效期）
- 同一 `refresh_token` 第二次调用会校验失败，返 `auth.token_invalid`

**前端实现要点**：每次刷新成功后立即用新 `refresh_token` 覆盖本地存储；并发请求场景需对 refresh 加锁避免重复使用。

### 7.5.5 WORM 失败审计与监控

Phase 1 / K.1 起，客户签名 / 三方合同上传后会调用 `services.LockOnce` 进入 WORM（Write Once Read Many）保护。LockOnce 失败时：

- 上报业务事件 `worm.lock.failed`（[handlers/customer.go](./backend/handlers/customer.go) / [handlers/third_party.go](./backend/handlers/third_party.go) 调用点）
- 写入 `audit_log`（target_type=`pdf_lock`）
- Prometheus 告警 `DocServerWORMLockFailed`（severity=critical，单次失败即触发）

**典型故障原因**：

1. `uploads/worm` 目录权限不足或磁盘满
2. 同一 snowid 重复上传（重复 LockOnce 返 `ErrAlreadyLocked`）
3. 文件路径冲突（DB 唯一索引拒绝）

排查步骤：查 `worm_record` 表 + `audit_log` pdf_lock 失败记录 + `uploads/worm` 目录磁盘 / 权限。

### 7.5.6 JWT 拒绝原因细分指标

K.7 起 JWT 中间件把校验失败按原因分类上报，便于运维观测升级期与攻击期 token 拒绝分布：

| 指标 label | 含义 |
|------------|------|
| `auth.jwt.reject.no_header` | 请求未带 `Authorization` 头 |
| `auth.jwt.reject.bad_format` | 头格式非 `Bearer <token>` |
| `auth.jwt.reject.revoked` | token 在黑名单中（logout / refresh rotation 后旧 token 重放）|
| `auth.jwt.reject.invalid_issuer` | iss 字段缺失或不等于 `doc-server`（Phase 4a 升级期高频）|
| `auth.jwt.reject.other` | 其他校验失败（签名错 / 过期 / 算法错 / token type 不符）|

告警规则：`DocServerJWTRejectionsSpike`（10 分钟 > 100 次任意原因）+ `DocServerJWTInvalidIssuerSpike`（10 分钟 > 50 次 iss 错误）。

---

# 八、目录结构

```
DocManageTrail/
├── README.md / AGENTS.md / go.work / package.ps1
├── backend/                           Go 1.26 业务后端
│   ├── main.go / go.mod / go.sum / vendor/
│   ├── Dockerfile / docker-compose.yml / Makefile / build.sh / build.ps1 / run.ps1
│   ├── config/  database/  handlers/  services/  middleware/  models/  utils/  server/  web/
│   └── bin/                            config.json + config.test-main.json + data/ + uploads/ + logs/ + backups/ + certs/
├── frontend/                          Vue 3 + TS + Element Plus
│   └── src/                           api/ components/ composables/ config/ directives/ hooks/ layout/ mock/ plugins/ router/modules/ store/ style/ utils/ views/
└── .trae/documents/                   Handoff + 测试报告（详见 .trae/documents/）
```

---

# 九、API 速览

后端共 **~113 protected + 1 公开 + 1 健康检查**，统一前缀 `/api`。

**公开端点（无需登录）**：`GET /health`、`GET /api/license/client-info`。

**业务核心（受 JWT + RBAC v2 + RBAC v3 保护）**：

| 资源 | 端点 | 路径前缀 |
|------|------|----------|
| 哈希 | 3 | `/api/hash/*` |
| 部门 | 6 | `/api/departments/*` |
| 用户 | 10 | `/api/users/*` |
| 客户 | 10 | `/api/customer/*` |
| 表单字段 | 4 | `/api/templates/fields` + `/api/fields/*` |
| 消息 | 7 | `/api/messages/*` |
| 提醒 | 10 | `/api/reminders/*` |
| 三方合同 | 9 | `/api/third-party/contracts/*` |
| 媒体 | 16 | `/api/media/*` |
| 定时任务 | 11 | `/api/scheduled-tasks/*` |
| 系统 | 10 | `/api/system/*` |
| 备份 | 4 | `/api/backup/*` |
| RBAC | 7 | `/api/rbac/*` |
| 业务配置 | 4 | `/api/system-config/*` |
| 认证 | 6 | `/api/login` + `/api/user/info` + `/api/logout` + `/api/change-password` + `/api/refresh-token` + `/api/get-async-routes` |

详见 [backend/server/combined_server.go](./backend/server/combined_server.go)。

---

# 十、构建与发布

## 10.1 一键发布（推荐）

```powershell
# 仓库根目录
.\package.ps1                       # 默认（Version="dev"）
.\package.ps1 -Version 1.2.0        # 指定版本号
.\package.ps1 -Clean                # 清理 bin\doc-server.exe + web\dist
.\package.ps1 -SkipInstall          # 跳过 pnpm install
```

**Pipeline**（4 步）：

```
[Step 0/4]  Clean (optional)
[Step 1/4]  frontend  pnpm install + pnpm build  -> frontend\dist\
[Step 2/4]  copy  frontend\dist  ->  backend\web\dist
[Step 3/4]  backend go build (-ldflags version) -> backend\bin\doc-server.exe
[Step 4/4]  copy  doc-server.exe -> dist\DocServer-<ver>-<yyyymmdd>.exe + .sha256
```

**输出**（`dist\` 目录）：

| 文件 | 说明 |
|------|------|
| `DocServer-<version>-<yyyymmdd>.exe` | 服务端（Go + 前端 embed.FS + SQLite）单 exe |
| `DocServer-<version>-<yyyymmdd>.exe.sha256` | 单独校验和 |
| `SHA256SUMS.txt` | 全部 sha256 汇总 |

## 10.2 后端 / 前端单独打包

```powershell
# 后端（cd backend）
.\build.ps1 -Version 1.2.0 -Clean -SkipFrontend -SkipInstall

# 前端（cd frontend）
pnpm dev               # 开发（http://localhost:8848）
pnpm build             # 打包到 ../backend/web/dist/
pnpm test              # vitest 单元测试
pnpm test:e2e          # Playwright E2E
pnpm typecheck         # tsc --noEmit + vue-tsc --noEmit
pnpm lint              # eslint + prettier + stylelint
```

`backend/build.ps1` 通过 `-ldflags` 把 `Version` / `GitCommit` / `BuildTime` 注入二进制（参见 [backend/main.go:19-23](./backend/main.go#L19-L23)）；运行时可通过 Go 代码读出。

---

# 十一、许可证

本项目采用 **[Apache License 2.0](./LICENSE)** 开源（[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)）。

**第三方组件的许可证例外**：

| 组件 | 许可证 | 说明 |
|------|--------|------|
| `frontend/` 模板代码 | **MIT**（[frontend/LICENSE](./frontend/LICENSE)）| 上游 [vue-pure-admin-thin](https://github.com/pure-admin/vue-pure-admin-thin) 为 MIT，本仓库前端基于它改造 |
| `backend/vendor/` 内的 Go 依赖 | 各依赖自带（MIT / BSD-3-Clause / Apache-2.0 等）| 详见各 vendor 目录下的 LICENSE 文件 |

**你可以自由地**：商用、修改、分发本项目；以 Apache-2.0 条款再许可。

**你需要做的**：在分发副本中附带 [LICENSE](./LICENSE) 全文；在修改的文件中显著标注「你修改过」；不要使用作者商标做背书；在专利诉讼发起时，相关专利许可自动终止。

---

# 十二、联系方式

| 渠道 | 地址 |
|------|------|
| **邮箱** | rfr@163.com |
| **官网** | [https://www.meshfans.com/](https://www.meshfans.com/) |

> 商务咨询、定制开发、技术支持、漏洞反馈均可通过邮箱联系。官网提供 Demo 申请、企业版 License 报价、最新版本下载与文档入口。
