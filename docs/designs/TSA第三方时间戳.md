# 详细设计：TSA 第三方时间戳

> **任务编号**：todo.md #10  
> **优先级**：P2（按需升级）  
> **工时估算**：1 天  
> **法律效力**：92 → **95 分**

## 1. 目标

合同归档时，调用第三方 TSA（Time Stamp Authority）服务生成符合 RFC 3161 标准的时间戳令牌，强化司法举证效力，使归档时间具备**第三方公证**的法律效力。

## 2. 为什么需要 TSA？

| 现状 | 风险 | TSA 增强后 |
|------|------|-----------|
| 系统本地时间戳 | 自证困境，可篡改，无公信力 | 第三方公证，UTC 原子钟同步 |
| 自记录归档时间 | 法庭不认可自证时间 | RFC 3161 国际标准，法律可采信 |
| SM3 + WORM 存证 | 仅证明"未被篡改"，不证明"具体时间" | 同时证明"何时存在"+"未被篡改" |

## 3. 服务商候选

| 服务商 | 类型 | 价格 | 备注 |
|--------|------|------|------|
| **SSL.com** | 国际 | ~$0.03/次 | 最便宜，推荐 |
| **DigiCert TSA** | 国际 | ~$0.05/次 | 全球通用 |
| **CFCA** | 国内 | ~¥1-3/次 | 金融行业首选 |
| **天威诚信** | 国内 | 按量计费 | 商业化成熟 |
| **NTSC** | 国内官方 | ¥3/次 | 北京权威，最强证据 |

## 4. 成本对比（按月 1500 次合同测算）

| 服务商 | 单价 | 月成本 | 年成本 |
|--------|------|--------|--------|
| SSL.com | ¥0.21/次 | ¥315 | ¥3,780 |
| DigiCert TSA | ¥0.35/次 | ¥525 | ¥6,300 |
| GDCA（数安时代）| ¥0.5/次 | ¥750 | ¥9,000 |
| CFCA | ¥2/次 | ¥3,000 | ¥36,000 |

## 5. 推荐分级策略

| 合同类型 | 推荐服务商 | 理由 |
|---------|-----------|------|
| 普通合同（< 10万）| SSL.com / DigiCert | 国际标准 + 便宜 |
| 重要合同（10-100万）| CFCA / 天威诚信 | 国内认可度高 |
| 重大合同（> 100万）| NTSC / 联合信任 | 国家级，最强证据 |
| 涉外合同 | DigiCert / GlobalSign | 国际通用 |

## 6. 数据库设计

```sql
CREATE TABLE time_stamp_record (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    contract_type   TEXT    NOT NULL,        -- 'contract' / 'third_party'
    contract_id     INTEGER NOT NULL,
    file_hash       TEXT    NOT NULL,        -- SM3 / SHA256 哈希
    hash_algorithm  TEXT    NOT NULL,        -- 'SM3' / 'SHA256'
    tsa_provider    TEXT    NOT NULL,        -- 'CFCA' / 'DigiCert' 等
    tsa_token       BLOB    NOT NULL,        -- RFC 3161 令牌（DER 编码）
    tsa_serial      TEXT    NOT NULL,        -- 令牌序列号
    issued_at       INTEGER NOT NULL,        -- TSA 颁发时间（UTC 秒）
    verified_at     INTEGER DEFAULT 0,       -- 验证时间
    created_at      INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);
```

## 7. 后端改造

**新增文件**：`backend/services/tsa_service.go`

- TSA 客户端封装（申请 / 验证）
- `POST /api/third-party/contracts/:id/apply-tsa` - 申请时间戳
- `GET /api/third-party/contracts/:id/tsa-record` - 获取时间戳记录

**集成点**：
- 合同归档（archive API）时同步申请 TSA
- 证据包 ZIP 导出含 `.tsr` 令牌

## 8. 关键路径

```
合同归档（archive API）
  ↓
PDF 锁定（WORM 写入）✅
  ↓
计算 SM3 哈希 ✅
  ↓
[新] 提交哈希给 TSA → 获取时间戳令牌
  ↓
存储 time_stamp_record
  ↓
证据包导出含 .tsr 令牌
```

## 9. 失败降级

- TSA 服务不可用时，WORM 归档仍然完成，仅记录 `tsa_status='failed'`
- 后台重试任务（基于 scheduled_task）：每 6h 重试失败的时间戳申请

## 10. 预期收益

- ✅ 归档时间具备第三方公证法律效力
- ✅ 证据包含 `.tsr` 令牌可独立验证
- ✅ 提升系统从"内部可用"到"商业级司法证据系统"
