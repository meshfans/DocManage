package utils

import "encoding/json"

// ParseFormData 解析 contract.form_json，返回 map[string]interface{}。
// 兼容两种存储格式：
//   - 数组格式：[{"field_id": 22, "value": ...}, ...]
//   - 对象格式：{"field_22": {...}, ...}
// 返回值可直接传给 PDF 渲染等需要 map 格式的函数。
func ParseFormData(formJSON string) map[string]interface{} {
	if formJSON == "" {
		return make(map[string]interface{})
	}

	var data interface{}
	if err := json.Unmarshal([]byte(formJSON), &data); err != nil {
		return make(map[string]interface{})
	}

	// 数组格式
	if arr, ok := data.([]interface{}); ok {
		result := make(map[string]interface{})
		for _, item := range arr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if fieldID, exists := itemMap["field_id"]; exists {
					if fid, ok := fieldID.(float64); ok {
						key := FormatFieldKey(int64(fid))
						result[key] = itemMap
					}
				}
			}
		}
		return result
	}

	// 对象格式
	if obj, ok := data.(map[string]interface{}); ok {
		return obj
	}

	return make(map[string]interface{})
}

// FormatFieldKey 生成字段 map 的 key，格式为 "field_{id}"。
func FormatFieldKey(fieldID int64) string {
	return "field_" + itoa(fieldID)
}

// itoa 将非负整数转为十进制字符串（不依赖 strconv）
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
