// Package debug 提供 config.json["debug"] 顶层字段的解密 + 时间窗口判定。
//
// 设计目的：
//   - 允许运维通过下发一段 base64 密文到 config.json 顶层 "debug" 字段，
//     在限定的时间窗口内远程打开本进程的 /debug/pprof/* 路由。
//   - 解密失败 / 字段缺失 / 窗口外 → 等同 debug=false（不 panic、不 Fatal）。
//
// 协议（与签发端 LMP 后端严格保持一致，改动需前后端同时升级；
// 2026-09-23 用真实下发信封穷举组合实证）：
//
//	envelope_b64 = base64.URLEncoding(iv(12) || ciphertext || tag(16))  // URL-safe
//	aes_key      = HKDF-SHA256(salt=app_id, ikm=password, info="lmp-client-request", L=32)
//	plaintext    = AES-256-GCM-Open(aes_key, nonce, ciphertext, aad=nil)
//	JSON         = {"starttime": RFC3339, "endtime": RFC3339, "issued": int64}
//
// 与 pkg/license 的关系：
//   - 复用 crypto.DeriveAesKey 原语；base64/GCM 在本包 openDebugEnvelope 独立实现
//     （license 的 crypto.AesGcmDecrypt 硬编码 StdEncoding + 强制 AAD，与本协议不兼容）
//   - HKDF info 为 "lmp-client-request"（签发端决定），与 license 的 "lmp-license-v1" 不同密钥族
//   - debug 信封不签 RSA（运维内部通道，不抗抵赖）
//
// 强耦合约束：
//   - HKDF ikm = license.payload.password，没有 license 解析成功就没有 debug 信封密钥
//   - license_key 为空 → cfg.Password 为空 → DebugConfig 自动降级为 Source=absent
//   - 因此：未启用 license 的环境**永远**无法启用 debug（这是设计意图，不是 bug）
//
// 时间窗口语义（IsInWindow）：
//   - 区间为闭区间 [start, end]，端点包含
//   - start == 0 / end == 0 / end < start → 视为非法窗口，Source=bad_window
//   - 端点包含意味着：endtime 计算为 now+3d 时，到点仍有 1 秒窗口（按 Unix 秒取整）
//
// 日志策略（避免泄露环境特征）：
//   - Active=true → utils.Debug 打印 issued + window（console+file 必打；非 debug 模式不写文件）
//   - Active=false → utils.Debug 打印 source（console 可见，不写 info 文件）
//   - pprof 挂载成功由 combined_server 单独 utils.Info 打印一次（info 级别，写入日志文件）
//
// 使用方式（main.go）：
//
//	outcome := debug.ResolveDebugMode(debug.DebugConfig{...})
//	if outcome.Active {
//	    registerPprofRoutes(router)
//	}
package debug