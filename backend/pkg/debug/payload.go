package debug

// DebugPayload 是 config.json["debug"] 顶层字段解密 + 验签通过后的明文结构。
//
// 字段顺序与 JSON tag 与签发端（LMP 后端）严格一致。
// 字段语义：
//   - StartTime : debug 模式生效起点（RFC3339，UTC）
//   - EndTime   : debug 模式生效终点（RFC3339，UTC）
//   - Issued    : 信封签发 Unix 秒（用于日志溯源，与控制无关）
//
// 注意：
//   - 字段缺失（JSON 中不存在）或空字符串 → time.Parse 失败 → SourceDecodeFailed，Active=false
//   - EndTime 缺失或为空字符串 → Active=false（强制要求窗口边界）
type DebugPayload struct {
	StartTime string `json:"starttime"`
	EndTime   string `json:"endtime"`
	Issued    int64  `json:"issued"`
}