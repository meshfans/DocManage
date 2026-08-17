package middleware

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"doc/utils"

	"github.com/gin-gonic/gin"
)

const tokenParam = "token"

func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		method := c.Request.Method
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()

		// 安全过滤：移除 query 中的 token 参数，避免 JWT 落入访问日志
		sanitizedQuery := sanitizeQuery(tokenParam, query)

		if sanitizedQuery != "" {
			path = path + "?" + sanitizedQuery
		}

		c.Next()
		
		endTime := time.Now()
		latency := endTime.Sub(startTime)
		
		statusCode := c.Writer.Status()
		
		var userID interface{}
		if id, exists := c.Get("user_id"); exists {
			userID = id
		} else {
			userID = "-"
		}
		
		var username interface{}
		if name, exists := c.Get("username"); exists {
			username = name
		} else {
			username = "-"
		}
		
		logDetails := fmt.Sprintf(
			"%s | %3d | %13v | %15s | %-7s | %s | user=%v(%s)",
			endTime.Format("2006-01-02 15:04:05"),
			statusCode,
			latency,
			clientIP,
			method,
			path,
			userID,
			username,
		)
		
		if statusCode >= 500 {
			utils.LogError("%s", logDetails)
		} else if statusCode >= 400 {
			utils.Warn("%s", logDetails)
		} else {
			utils.Info("%s", logDetails)
		}
		
		if len(c.Errors) > 0 {
			for _, e := range c.Errors {
				utils.LogError("Request error: %s", e.Error())
			}
		}
		
		if statusCode >= 400 {
			body, _ := io.ReadAll(c.Request.Body)
			utils.Warn("Request body: %s", sanitizeBody(body))
		}
		
		_ = userAgent
	}
}

func sanitizeBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	
	s := string(body)
	
	sensitiveFields := []string{"password", "token", "secret", "key", "authorization"}
	for _, field := range sensitiveFields {
		if strings.Contains(strings.ToLower(s), field) {
			s = maskSensitiveData(s, field)
		}
	}
	
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

// sanitizeQuery 移除 URL query 中的指定参数，避免敏感值落入访问日志。
//
// 失败兜底：url.ParseQuery 解析失败（极少见，如 % 编码错乱）时返回 rawQuery 原样，
// 而不是返回空串——空串会让 path 后无 ?，丢失全部 query 信息反而更危险，
// 至少保留原始路径便于事后排查。
func sanitizeQuery(fieldName, rawQuery string) string {
	if rawQuery == "" || fieldName == "" {
		return rawQuery
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		utils.LogError("[logger] sanitizeQuery 解析失败，原样记录: err=%v, rawQuery=%q", err, rawQuery)
		return rawQuery
	}
	if !values.Has(fieldName) {
		return rawQuery
	}
	values.Set(fieldName, "[MASKED]")
	return values.Encode()
}

func maskSensitiveData(data, field string) string {
	fieldLower := strings.ToLower(field)
	lines := strings.Split(data, "\n")
	
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), fieldLower) {
			lines[i] = maskLine(line, field)
		}
	}
	
	return strings.Join(lines, "\n")
}

func maskLine(line, field string) string {
	fieldLower := strings.ToLower(field)
	fieldLen := len(field)
	
	for {
		idx := strings.Index(strings.ToLower(line), fieldLower)
		if idx == -1 {
			break
		}
		
		start := idx
		end := idx + fieldLen
		
		for end < len(line) && (line[end] == ':' || line[end] == '=' || line[end] == ' ' || line[end] == '"') {
			end++
		}
		
		valueEnd := end
		for valueEnd < len(line) && line[valueEnd] != ',' && line[valueEnd] != '"' && line[valueEnd] != '\n' {
			valueEnd++
		}
		
		masked := line[:start] + field + ": [MASKED]" + line[valueEnd:]
		line = masked
	}
	
	return line
}
