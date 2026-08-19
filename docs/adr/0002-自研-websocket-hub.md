# ADR-0002：自研 WebSocket Hub（不引入第三方库）

- 状态：Accepted
- 日期：2026-08-19

## 背景与问题陈述

DocManage 需要实时推送"铃铛通知 / 新消息 / 任务状态变更"等业务事件给登录用户。
终态要求：

- 单进程支撑 500 在线用户
- 消息延迟 < 100ms
- 客户端断线自动重连 + 事件不丢（关键事件）
- 灰度部署：可同时运行新/旧版本

需要决定：用什么 WebSocket 实现方式。

## 决策驱动

- 依赖透明：第三方库升级引入 breaking change 时希望可控
- 业务逻辑简单：核心需求是"广播 + 按 user_id 路由"，goroutine + channel 已足够
- 内存可控：第三方库（如 gorilla/websocket）的连接池抽象不符合本项目"按用户上线状态"语义
- 学习成本：项目成员维护代码的频率 > 引入新库的频率

## 考虑的选项

### 选项 A：gorilla/websocket

- 优点：成熟、文档多、连接器齐全
- 缺点：仅提供 connect/IO 层，hub/broadcast/heartbeat 仍需自写

### 选项 B：nhooyr.io/websocket（现 coder/websocket）

- 优点：context-aware、心跳机制完善
- 缺点：API 风格特殊（基于 context），与项目 gin 体系不直接对接

### 选项 C：gobwas/ws

- 优点：zero-allocation 写、纯协议层
- 缺点：零拷贝 API 不易理解，hub 逻辑仍需自写

### 选项 D：自研（已选）

- 优点：完全契合业务，仅依赖 `nhooyr.io/websocket`（RFC 6455 协议层）
- 缺点：需自写 hub / broadcast / heartbeat / reconnect；测试覆盖度需自负责

## 决策结果

**自研 Hub + 协议层用 `nhooyr.io/websocket`**。

设计要点（`backend/handlers/websocket.go`）：

- **Hub 模型**：单 Hub + 多 Client，`map[userID][]*Client`
- **上线/下线**：登录后注册，握手时校验 JWT
- **广播**：分 4 类（user / role / broadcast / tenant）
- **心跳**：服务端每 30s 推 ping，客户端 60s 内未回 pong 视为断线
- **错误处理**：写失败立即踢出（避免累积半连接）
- **指标**：Round 16 新增 `ws_clients_connected` / `ws_messages_sent_total` / `ws_messages_failed_total`

## 后果

### 正面

- 依赖最小：仅 1 个三方库（RFC 6455 协议层）
- 业务贴合度高：Hub 结构直接映射业务 user-session 概念
- 问题定位快：所有 panic 都在自家代码

### 负面

- 跨实例扩展未实现：当前单进程 Hub，多副本需引入 redis-pubsub
- Hub 锁重入需谨慎：Round 16 修复了 1 处锁重入 bug
- 与 RFC 6455 子协议边界（permessage-deflate 等）需自实现

### 中性

- 当前无 message persistence：客户端断线期间消息丢弃（除非业务层另行补发）
- 灰度路由：Nginx 端按 cookie sticky 即可，无需 Hub 改造

## 后续

- 触发条件：单实例 ws 连接 > 5k 或 CPU 成为瓶颈 → 评估 redis-pubsub
- 触发条件：业务需要"消息必达" → 评估 outbox 模式 + 客户端拉取补偿
