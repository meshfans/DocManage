package database

// 默认系统配置（单一事实来源：代码）。
//
// 业务系统调用 GetSystemConfig(key) 时：
//   - DB 有值且非空 → 用 DB 值（用户覆盖）
//   - DB 空 / 报错 → 走 DEFAULT_CONFIGS[key].Value（代码兜底）
//   - 都没有 → 返回 ""
//
// 添加新配置 key 只需在这里加一行，无需改 SQL/迁移。

type ConfigDefault struct {
	Value       string // 兜底值（DB 无覆盖时使用）
	Category    string // 业务分类：用于前端"业务配置"页分组展示
	Description string // 中文说明（前端 hover tooltip 展示）
}

var DEFAULT_CONFIGS = map[string]ConfigDefault{
	// ========== 企业基本信息（用于 PDF 头部、印章生成、合同字段、签章等） ==========
	"company_name":       {"示例科技有限公司", "企业信息", "公司完整名称（用于 PDF 头部、印章、合同等）"},
	"company_short_name": {"示例公司", "企业信息", "公司简称（短）"},
	"tax_id":             {"91110000XXXXXXXXXX", "企业信息", "统一社会信用代码 / 税号（18 位）"},
	"legal_person":       {"张三", "企业信息", "法人代表姓名"},
	"legal_person_id":    {"", "企业信息", "法人代表身份证号"},
	"registered_address": {"", "企业信息", "公司注册地址"},
	"company_size":       {"medium", "企业信息", "公司规模：small / medium / large"},
	"industry":           {"互联网", "企业信息", "所属行业"},
	"contact_phone":      {"", "企业信息", "联系电话"},
	"contact_email":      {"", "企业信息", "联系邮箱"},
	"contact_website":    {"", "企业信息", "公司网站"},



	// ========== 未来扩展预留（备份 / 通知等） ==========
	// "backup_retention_days":        {"7",   "备份",   "备份保留天数"},
	// "notification_email_enabled":   {"false", "通知", "启用邮件通知"},
}

// GetConfigDefaultValue 简化：只取 Value，未注册 key 返回 ""。
func GetConfigDefaultValue(key string) string {
	if v, ok := DEFAULT_CONFIGS[key]; ok {
		return v.Value
	}
	return ""
}

// AllConfigDefaults 列出全部（用于前端"业务配置"页首次渲染 / 审计）。
func AllConfigDefaults() map[string]ConfigDefault {
	out := make(map[string]ConfigDefault, len(DEFAULT_CONFIGS))
	for k, v := range DEFAULT_CONFIGS {
		out[k] = v
	}
	return out
}
