# ADR（Architecture Decision Records）

本目录记录 DocManage 后端架构上的关键决策，用于：

- **新成员 onboarding**：一眼看懂"为什么这么写"，避免重构者重蹈覆辙
- **决策可追溯**：每个 ADR 独立编号，状态变更（Proposed / Accepted / Deprecated）有据可查
- **避免反复争论**：明确"之前的考量"，让讨论聚焦于"是否仍然成立"

## 格式

每篇 ADR 用 MADR（Markdown ADR）模板：

```
# 标题（决策短语）

- 状态：Proposed / Accepted / Deprecated / Superseded by ADR-0007
- 日期：YYYY-MM-DD
- 决策人：（可选）

## 背景与问题陈述
（业务约束 / 触发条件）

## 决策驱动
（不做的选项会带来的代价）

## 考虑的选项
（多个候选 + 简短评估）

## 决策结果
（最终选了什么 + 关键理由）

## 后果
（正面 / 负面 / 中性影响）
```

## 索引

| 编号 | 标题 | 状态 |
|---|---|---|
| [ADR-0001](./0001-采用-sqlite-作为主存储.md) | 采用 SQLite 作为主存储 | Accepted |
| [ADR-0002](./0002-自研-websocket-hub.md) | 自研 WebSocket Hub（不引入第三方库） | Accepted |
| [ADR-0003](./0003-不用-orm-手写-sql.md) | 不用 ORM，手写 SQL | Accepted |
| [ADR-0004](./0004-手写-prometheus-指标库.md) | 手写 Prometheus 指标库（不引入官方 SDK） | Accepted |
| [ADR-0005](./0005-审计日志-append-only-哈希链.md) | 审计日志：append-only 哈希链（SM3） | Accepted |

> 编号严格递增。Supersede 旧决策时保留旧文件，新文件引用 `Superseded by ADR-XXXX`。
