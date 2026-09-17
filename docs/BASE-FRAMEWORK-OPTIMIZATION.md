# 底座优化建议

> 本文档总结 DocCRM 项目在实际使用中发现并修复的底座问题及解决方案，供底座团队改进参考。
> 来源：[HANDOVER_2026-09-17.md](handover/HANDOVER_2026-09-17.md) 整理

---

## 1. Data Scope 安全漏洞：scope=nil 退化为"看全部"

### 问题描述
中间件 `DataScopeMiddleware` 在 DB 查询失败时，会将 `scope=nil` 注入到 context：
```go
// middleware/data_scope.go:62-66
if err != nil {
    utils.Warn("[data_scope] 加载 user=%d scope 失败: %v", userID, err)
    c.Set("data_scope", (*database.UserDataScope)(nil))  // nil scope
    c.Next()
    return
}
```

多个 handler 将 `scope == nil` 等同于 `scope.DataScope == "all"`，导致 DB 抖动期间任意登录用户可看到全量数据。

### 涉及 handler（8 个）
- `crm_activity.go` / `crm_lead.go` / `crm_contact.go` / `crm_dashboard.go`
- `crm_opportunity.go` / `customer.go` / `media.go` / `reminder.go`

### 解决方案
新增 `EnsureListDataScope` helper 函数，将 `scope==nil` 拆出来单独处理：

```go
// handlers/data_scope_helpers.go:42-62
func EnsureListDataScope(c *gin.Context) bool {
    if IsAdminUser(c) {
        return true
    }
    scope := middleware.GetDataScope(c)
    if scope == nil {
        username := c.GetString("username")
        utils.LogError("[data_scope] scope=nil，拒绝 list 请求以防止全量泄露: username=%s, path=%s, ip=%s",
            username, c.Request.URL.Path, c.ClientIP())
        utils.Err(c, utils.CodeInternal, "数据权限未就绪，请稍后重试（data_scope 加载失败）")
        return false
    }
    if scope.DataScope == "" {
        utils.LogError("[data_scope] scope.DataScope 为空，异常...")
        utils.Err(c, utils.CodeInternal, "数据权限配置异常，请联系管理员")
        return false
    }
    return true
}
```

### 底座优化建议
- [ ] **中间件层**：DataScopeMiddleware 加载失败时不要注入 nil，改为注入一个 `data_scope = "self"` 的兜底 scope（仅看自己创建的数据）
- [ ] **或者**：在所有 list handler 入口统一加 `EnsureListDataScope` guard（见上述方案）

---

## 2. JWT 黑名单未校验

### 问题描述
底座 JWT 中间件只校验 token 签名和有效期，未校验黑名单：
```go
// 原实现 - middleware/jwt.go
claims, err := jwtUtils.ValidateToken(tokenString)
if err != nil {
    // 直接拒绝，但未检查黑名单
    utils.Unauthorized(c)
    c.Abort()
    return
}
```

导致 admin 撤销角色 / 用户 logout 后，旧 token 仍 24h 有效。

### 解决方案
在 `ValidateToken` 之前同步检查黑名单：
```go
// middleware/jwt.go:48-57
// P0 修复（2026-06-28）：JWT 黑名单校验。
// 检查放在 ValidateToken 之前：避免对已吊销 token 跑昂贵的签名校验。
if utils.IsTokenRevoked(tokenString) {
    recordReject(jwtRejectRevoked)
    utils.Unauthorized(c, "token 已失效，请重新登录")
    c.Abort()
    return
}
```

### 底座优化建议
- [ ] JWT 中间件在校验签名之前先检查黑名单（性能优化 + 安全双重保证）
- [ ] 黑名单检查失败时返回明确错误信息 "token 已失效，请重新登录"

---

## 3. 枚举字段读-改-写竞态

### 问题描述
CRM 元数据（客户分级/来源/标签）使用 handler 读-改-写模式：
```go
// 原实现 - crm_customer_meta.go handler
m, _ := database.GetCustomerCRMMeta(id)
m.Level = req.Level  // 修改字段
database.UpsertCustomerCRMMeta(m)  // 整行覆盖
```

并发场景下后写覆盖先写，导致数据丢失（如标签被清空）。

### 解决方案
新增 `UpdateCustomerCRMFields` 单 SQL 原子更新：
```go
// database/crm_customer_meta.go:129-190
func UpdateCustomerCRMFields(customerID int64,
    hasLevel bool, levelVal string,
    hasSource bool, sourceVal string,
    hasTags bool, tagsVal []string,
    operatorUserID int64) error {
    // 未提供的列保持原值（COALESCE 跳过）
    _, err = DB.Exec(`
        UPDATE customer_crm_meta SET
            level    = COALESCE(?, level),
            source   = COALESCE(?, source),
            tags     = COALESCE(?, tags),
            updated_at = ?
        WHERE customer_id = ?
    `, levelArg, sourceArg, tagsArg, now, customerID)
    return err
}
```

### 底座优化建议
- [ ] 提供 `PartialUpdate` 通用能力：单 SQL 原子更新，仅修改提供的字段
- [ ] 或者：推荐业务表使用行级锁（`SELECT ... FOR UPDATE`）确保读-改-写原子性

---

## 4. 分页快捷模式吞数据

### 问题描述
活动列表为"性能优化"添加快捷分支：
```go
// 原实现 - handlers/crm_activity.go
if page == 1 && pageSize == 10 {
    // 直接 LIMIT 200，不走正常分页
    list, total, err = database.GetTimelineByTarget(filter)
} else {
    list, total, err = database.ListActivities(filter)
}
```

前端默认 `pageSize=10`，导致被吞掉分页，一次性返回全部数据。

### 解决方案
删除快捷分支，统一走正常分页：
```go
// 修复后 - handlers/crm_activity.go:48-56
// 2026-09-17 修复：去掉原"快捷模式"（page==1&&pageSize==10 时直接走 GetTimelineByTarget LIMIT 200），
// 否则前端默认 pageSize=10 时会被吞掉分页，一次性返回全部数据。
filter := database.ActivityListFilter{
    Page: page, PageSize: pageSize,
    ...
}
```

### 底座优化建议
- [ ] 避免在 handler 层添加"快捷分支"——分页语义必须对所有参数组合一致
- [ ] 如需性能优化，应在 SQL 层优化索引，而非破坏分页语义

---

## 5. 标签字典无权限过滤

### 问题描述
标签字典 `ListAllCustomerTags` 之前不过滤 data_scope：
```go
// 原实现 - database/crm_customer_meta.go
func ListAllCustomerTags(limit int, extraWhere string, ...) ([]string, error) {
    // 之前没有 data_scope 过滤，所有用户看到相同的标签字典
    // 导致跨部门/跨业务线的标签泄露
}
```

### 解决方案
新增 `ListCustomerTags` handler + data_scope 过滤：
```go
// handlers/crm_customer_meta.go:213-247
func (h *CustomerMetaHandler) ListCustomerTags(c *gin.Context) {
    if !EnsureListDataScope(c) {
        return
    }
    scope := middleware.GetDataScope(c)
    // admin / scope=all → 不过滤
    // 其他 scope 走 BuildWhereSQL 过滤
    if scope != nil && scope.DataScope != "all" && !IsAdminUser(c) {
        w, args, _ := database.BuildWhereSQL(scope, database.FilterOpts{
            TableAlias: "c",
            OwnerCol:   "owner_user_id",
            DeptCol:    "department_id",
        })
        extraWhere = w
        extraArgs = args
    }
    tags, err := database.ListAllCustomerTags(10000, extraWhere, extraArgs)
}
```

### 底座优化建议
- [ ] 任何聚合/字典端点必须强制 data_scope 过滤
- [ ] 提供聚合查询的标准 `BuildWhereSQL` 模式，避免遗漏

---

## 6. 业务字典长尾截断

### 问题描述
标签字典 LIMIT 默认 10000，可能截断长尾标签：
```go
// 原实现 - database/crm_customer_meta.go
if limit <= 0 || limit > 50000 {
    limit = 50000
}
// 无警告日志，长尾标签静默丢失
```

### 解决方案
预查 COUNT，超限时 `log.Printf` 警告：
```go
// 修复后 - database/crm_customer_meta.go:365-371
var total int
countSQL := `SELECT COUNT(*) FROM customer_crm_meta m
    JOIN customer c ON c.id = m.customer_id WHERE ` + whereSQL
if err := DB.QueryRow(countSQL, args...).Scan(&total); err == nil && total > limit {
    log.Printf("[warn] ListAllCustomerTags: %d rows exceed limit %d, some tags may be missing", total, limit)
}
```

### 底座优化建议
- [ ] 聚合类查询超出 LIMIT 时必须记录 warn 日志
- [ ] 考虑加 5min 缓存（Redis）减少 DB 查询

---

## 7. 前端 Timeline 滚动分页竞态

### 问题描述
前端滚动分页 + target 切换场景：
```vue
<!-- 原实现 - Timeline.vue -->
watch(
  () => [props.targetType, props.targetId],
  async () => {
    resetState();  // 重置状态
    await loadList();
    await setupObserver();
  }
);
// 问题：旧请求的回调可能在 resetState 后才返回，导致状态错乱
```

### 解决方案
使用 `reqSeq` 序列号过滤过期响应：
```vue
// 修复后 - frontend/src/views/crm/activity/Timeline.vue:40-68
let reqSeq = 0;

const loadList = async (append = false) => {
  const currentSeq = ++reqSeq;  // 捕获本次请求的序列号
  // ...
  const res = await listActivities({...});
  // 过期响应过滤——若序列号已被 resetState/increment 重置，忽略回调
  if (currentSeq !== reqSeq) return;
  // 处理数据...
};

watch(
  () => [props.targetType, props.targetId],
  async () => {
    reqSeq++;  // 切换 target 时 invalidate 所有旧请求
    resetState();
    await loadList();
  }
);
```

### 底座优化建议
- [ ] 提供通用的 `useAsyncRequest` composable，支持请求序列号取消
- [ ] 或者：前端路由切换时自动取消所有进行中的请求

---

## 8. 请求错误吞噬

### 问题描述
前端 `loadList().then(setupObserver)` 吞掉错误：
```vue
// 原实现
loadList().then(() => {
  setupObserver();
});
// 问题：loadList 失败时 setupObserver 不执行，滚动分页失效
```

### 解决方案
改用 `.finally()` 确保 observer 始终设置：
```vue
// 修复后 - Timeline.vue:152-154
loadList().finally(setupObserver);  // C19：无论成功失败都 setupObserver
```

### 底座优化建议
- [ ] 前端 HTTP 封装在 `.catch()` 中统一处理错误，不应在业务层吞掉错误
- [ ] `.then()` 只处理成功路径，`.finally()` 用于清理/设置逻辑

---

## 9. 字段硬编码 vs 反射

### 问题描述
`enrichCustomersWithOwner` 硬编码 23 个字段：
```go
// 原实现 - handlers/customer.go
m["phone"] = c.Phone
m["address"] = c.Address
// ... 23 个字段手写
// 问题：新加字段需要同步修改代码
```

### 解决方案
使用 `json.Marshal/Unmarshal` 自动反射：
```go
// 修复后 - handlers/customer.go
var raw map[string]interface{}
if err := json.Unmarshal(data, &raw); err != nil {
    return customers, total, err
}
for i := range customers {
    var m map[string]interface{}
    data, _ := json.Marshal(customers[i])
    json.Unmarshal(data, &m)
    customers[i].Extra = m
}
```

### 底座优化建议
- [ ] 提供通用的 `StructToMap` / `MapToStruct` 工具函数
- [ ] 或使用 `reflect` 包提供通用字段拷贝能力

---

## 10. LEFT JOIN NULLIF 陷阱

### 问题描述
线索详情 JOIN 客户时，空字符串覆盖 NULL：
```sql
-- 原实现 - database/crm_lead.go
LEFT JOIN customer c ON ...
COALESCE(c.real_name, '')  -- 空字符串不是 NULL
```

导致客户名为空串时，线索详情显示为空而非"企业客户"。

### 解决方案
```sql
-- 修复后
COALESCE(NULLIF(c.real_name, ''), '企业客户') AS customer_name
```

### 底座优化建议
- [ ] 提供 SQL 规范：字符串 JOIN 结果统一用 `NULLIF(col, '')` 处理空字符串
- [ ] 在代码审查清单中增加此项检查

---

## 附录：问题分类汇总

| # | 类别 | 问题 | 严重度 |
|---|------|------|--------|
| 1 | 安全 | scope=nil 退化为"看全部" | P0 |
| 2 | 安全 | JWT 黑名单未校验 | P0 |
| 3 | 数据 | 枚举字段读-改-写竞态 | P1 |
| 4 | 正确性 | 分页快捷模式吞数据 | P1 |
| 5 | 安全 | 标签字典无权限过滤 | P1 |
| 6 | 可观测性 | 业务字典长尾截断无警告 | P2 |
| 7 | 前端 | Timeline 滚动分页竞态 | P2 |
| 8 | 前端 | 请求错误吞噬 | P2 |
| 9 | 可维护性 | 字段硬编码 | P3 |
| 10 | 正确性 | LEFT JOIN NULLIF 陷阱 | P3 |


缺少功能：
1.用户头像上传展示
2.llm 接入
3.多数据库切换 sqlite，mysql，postgresql
4.多租户支持 x
5.多语言支持 x