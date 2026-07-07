package handlers

import (
	"encoding/json"
	"strings"
)

func FilterFields(data interface{}, fields string) interface{} {
	if fields == "" || fields == "true" || fields == "false" {
		return data
	}

	allowedFields := make(map[string]bool)
	for _, field := range strings.Split(fields, ",") {
		field = strings.TrimSpace(field)
		if field != "" {
			allowedFields[field] = true
		}
	}

	if len(allowedFields) == 0 {
		return data
	}

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return data
	}

	var rawData interface{}
	if err := json.Unmarshal(jsonBytes, &rawData); err != nil {
		return data
	}

	return filterRecursive(rawData, allowedFields)
}

func filterRecursive(data interface{}, allowedFields map[string]bool) interface{} {
	switch v := data.(type) {
	case []interface{}:
		result := make([]interface{}, 0, len(v))
		for _, item := range v {
			result = append(result, filterRecursive(item, allowedFields))
		}
		return result

	case map[string]interface{}:
		filtered := make(map[string]interface{})

		for key, value := range v {
			if key == "children" {
				filtered[key] = filterRecursive(value, allowedFields)
			} else if allowedFields[key] {
				filtered[key] = value
			}
		}

		return filtered

	default:
		return data
	}
}
