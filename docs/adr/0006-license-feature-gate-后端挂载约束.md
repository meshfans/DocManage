# License Feature Gate 后端挂载约束

- 状态：Accepted
- 日期：2026-10-02
- 决策人：（授权门禁统一基准 C1/C2/C5 与 DocCRM/DocManage/DocWMS 一并推进）

## 背景与问题陈述

DocManageTrail 接入 license 授权体系（`pkg/license/sdk` + `middleware.FeatureGate`）后，
所有需要 license feature 授权的后端端点必须挂 `FeatureGate(key)` 中间件。

历史上 AI 配置 11 个端点（`/api/ai-configs/**`、`/api/ai-meta`）就是用
**内联中间件** 逐条挂 `aiGate` 的（见 `backend/server/combined_server.go:295`）。
注释解释："用内联中间件而非 `protected.Group(...)`，保持 `protected.<METHOD>` 的字面写法，
权限种子对账才能对上。"

问题：**未来新增 `ai` / `rag` / `ocr` 等业务端点时容易漏挂 `aiGate`**，而
现有 `grep protected.GET.*ai-configs` 类检查无法在 PR 阶段发现这种遗漏——
审计 / 防护全靠人工。

## 决策驱动

不做选项会带来的代价：

1. **静默越权**：用户无 license 也能调 `/api/ai-configs/create` 创建自己的 LLM 接入，
   直接绕过授权体系设计意图。
2. **审计盲区**：seed 与 seed_rbac 的 permission 表里 AI 相关权限码都是「按路由」登记的，
   不挂 FeatureGate 意味着这条路由「隐形」——前端 i18n / 运维脚本都看不到。
3. **跨项目漂移**：DocCRM / DocManage / DocWMS 已有同款 gate，统一约束利于同步维护。

## 考虑的选项

### 选项 A：内联 aiGate（现行做法）
- 优点：路由字面 + permission_seed 一一对应肉眼可读
- 缺点：新增端点漏挂率高

### 选项 B：`protected.Group("/ai-configs", aiGate, ...)`
- 优点：新增端点自动被 gate 守护
- 缺点：与 permission_seed 注册路径不一致（seed 是 `/api/ai-configs/:id`），对账会漂移

### 选项 C：CI 静态检查 + CONTRIBUTING 文档化（采纳）
- 优点：保留 A 的可读性，加 B 的硬性
- 缺点：依赖开发者读 CONTRIBUTING + CI 检查通过

## 决策结果

**采用选项 C**：

1. **PR 检查清单**（在 `.github/pull_request_template.md` 或贡献指南里固化）：
   - 新增 `ai` / `rag` / `ocr` 相关端点 → 必须挂 `aiGate / ragGate / ocrGate`
   - `grep -nE "protected\.(GET|POST|PUT|DELETE).*ai-configs|ai-meta"` 必须含 `aiGate`
   - 后端 `backend/server/combined_server.go` 中挂载位不可被注释掉

2. **代码注释锁定**（已在 `combined_server.go:295` 写明）：
   ```
   // 2026-10-02 M-5 修复：新增 ai 相关端点必须挂 aiGate（详见 docs/adr/0006）。
   //   当前 aiGate 挂在 11 个端点：list/detail/create/update/delete/set-default/test/key/multimodal/test-inline/ai-meta。
   ```

3. **新 endpoint 准入流程**：
   - 设计师填「所属 feature module」字段
   - 评审者对照 `pkg/license/sdk/feature.go BusinessModuleKeys`（rag/ocr/ai）确认 key
   - 实现者按 module key 挂对应 Gate

## 后果

正面：
- 漏挂率从「依赖人脑」降为「CI 拒绝合入」
- 新人 onboarding 路径清晰：docs/adr/0006 → combined_server.go:295 注释 → middleware/feature_gate.go

负面：
- 增 1 个 grep 检查步骤（约 5s CI 耗时）

中性：
- 跨项目（DocCRM/DocManage/DocWMS/DocManageTrail）需要保持 ADR 同步修订
