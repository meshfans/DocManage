# ADR-0004：手写 Prometheus 指标库（不引入官方 SDK）

- 状态：Accepted
- 日期：2026-08-19

## 背景与问题陈述

DocManage 后端 `/metrics` 端点（Round 15 引入）当前采用 **手写 Prometheus text 0.0.4 序列化** 实现，零三方依赖。

需要明确：**为什么不直接用 `github.com/prometheus/client_golang`**，
以及**什么情况下应迁移**。

## 决策驱动

- 零依赖：客户端环境常常内网部署，依赖解析 + module proxy 都是隐形成本
- 透明可读：运维 / DBA review 时一眼能看懂 `_sum` / `_count` 怎么算
- 教学价值：手写一遍才知道 Histogram bucket 边界、label cardinality 治理
- 性能边界：单实例 QPS < 1k，官方 SDK 与手写差距 < 1ms

## 考虑的选项

### 选项 A：官方 `prometheus/client_golang`

- 优点：OpenMetrics 兼容、完备 Histogram/Quantile、Profiling 工具集成
- 缺点：依赖大（pulls `protobuf`、`client_model` 等）、API 抽象多

### 选项 B：VictoriaMetrics `victoria-metrics/lib` 或 `prymitive/...`

- 优点：单文件、零依赖
- 缺点：API 风格与 Prometheus 不完全兼容

### 选项 C：手写（已选）

- 优点：完全可控、零依赖、可定制（业务事件命名 label 治理）
- 缺点：仅实现 text 0.0.4（无 OpenMetrics）、Histogram 实现需自负责

## 决策结果

**保留手写实现**，规范如下：

| 指标族 | 类型 | 命名 | 备注 |
|---|---|---|---|
| HTTP 请求 | CounterVec / HistogramVec | `http_*` | label 仅 `method`/`status`（无 path，防高基数） |
| 业务事件 | CounterVec | `business_events_total{event}` | event 名 = 后端 const 字典 |
| Runtime | Gauge / Counter | `go_*` / `process_*` | only-zero label |
| WS / DB / Scheduler | Gauge / Counter | `ws_*` / `db_*` / `scheduler_*` | zero label |
| 关停阶段 | HistogramVec | `shutdown_stage_duration_seconds{stage}` | stage ∈ 6 固定值 |

关键设计：

- **atomic.Uint64 bits 存浮点**：Counter / Gauge value 用 CAS 循环
- **Vec 内部 map + label-value 拼 key**：避免 reflection
- **必须先注册再暴露**：未注册 series 不会出现（避免 label 失控）
- **bucket 边界固定**：Histogram bucket 在常量里集中定义

## 后果

### 正面

- 依赖最小：仅标准库
- 部署简单：内网/离线环境无 module proxy 也能跑
- 错误可见：所有 tricky case（顺序、bucket、label）都在自家代码

### 负面

- 缺 OpenMetrics：Prometheus 2.x 默认仍兼容 text 0.0.4，但 Grafana 11+ 部分新图表需要 OMF
- 缺 Exemplar / Quantile：无法对接 tracing
- label cardinality 治理完全靠人工：易引入 `path` / `user_id` 标签失控

### 中性

- 与 ADR-0003 联动：业务事件 bus（Round 18）通过 PublishEvent → metrics subscriber 注入
- 与 ADR-0001 联动：DB 指标从 `*sql.DB.Stats()` 反射，不需 DB 层适配

## 后续

**迁移触发条件**（任一达成即评估官方 SDK）：

- 需要 OpenMetrics 1.0 输出（OMF）
- 需要 Exemplar / TraceID 关联
- 需要 Quantile 统计（如 P99/P999 服务等级目标）
- 需要 PromQL 内置函数（如 `histogram_quantile`）
- 业务标签列表稳定后想引入 `prometheus.NewHistogramVec` 减少维护成本

迁移路径：

1. 保留 `business_events_total` 命名（兼容现有 dashboard）
2. 引入 `client_golang/prometheus` 作为 `Register*` 工厂
3. 替换 `metrics.go` 内部实现，公开 API 保持不变
4. 备份 `/metrics` 输出做 diff 对比，确保字段一致
