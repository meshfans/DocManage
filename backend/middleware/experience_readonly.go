package middleware

import (
	"net/http"

	"doc/config"

	"github.com/gin-gonic/gin"
)

// ExperienceReadOnly blocks business mutations while keeping authentication
// and read APIs available. Used for customer demo/trial deployments where
// data must not be modified.
//
// Behavior:
//   - config.IsExperienceMode() == false → pass-through (no-op)
//   - mode == "experience"
//     → GET / HEAD / OPTIONS: always allowed
//     → whitelist POST paths: login / logout / refresh-token / share audit consent
//     → all other mutations → 423 Locked + "体验环境不允许修改业务数据"
func ExperienceReadOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.IsExperienceMode() || experienceWriteAllowed(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusLocked, gin.H{
			"success": false,
			"message": "体验环境不允许修改业务数据",
		})
	}
}

func experienceWriteAllowed(method, path string) bool {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return true
	}
	// Whitelist: non-business-mutation POST/PUT/DELETE endpoints that need to
	// work during experience mode. Share audit pages are public and safe.
	for _, allowedPath := range []string{
		"/api/login",
		"/api/logout",
		"/api/refresh-token",
		"/api/share/audit/consent-letter-view",
	} {
		if path == allowedPath {
			return true
		}
	}
	return false
}
