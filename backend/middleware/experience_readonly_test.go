package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"doc/config"

	"github.com/gin-gonic/gin"
)

func TestExperienceReadOnly_AllowsNormalMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.GlobalConfig = &config.Config{}
	defer func() { config.GlobalConfig = nil }()

	r := gin.New()
	called := false
	r.Use(ExperienceReadOnly())
	r.POST("/api/customer/create", func(c *gin.Context) {
		called = true
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/customer/create", nil))
	if w.Code != http.StatusOK || !called {
		t.Fatalf("normal mode should allow handler: status=%d called=%v", w.Code, called)
	}
}

func TestExperienceReadOnly_BlocksBusinessWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	config.GlobalConfig = &config.Config{}
	config.GlobalConfig.Database.Mode = "experience"
	defer func() { config.GlobalConfig = nil }()

	r := gin.New()
	called := false
	r.Use(ExperienceReadOnly())
	r.POST("/api/customer/create", func(c *gin.Context) {
		called = true
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/customer/create", nil))
	if w.Code != http.StatusLocked || called {
		t.Fatalf("experience write should be blocked: status=%d called=%v", w.Code, called)
	}
}

func TestExperienceReadOnly_AllowsAuditAndReads(t *testing.T) {
	if !experienceWriteAllowed(http.MethodPost, "/api/share/audit/consent-letter-view") {
		t.Error("audit endpoint should be allowed")
	}
	if experienceWriteAllowed(http.MethodPost, "/api/login-attack") {
		t.Error("lookalike login path should be blocked")
	}
	if experienceWriteAllowed(http.MethodPost, "/api/share/upload-signature") {
		t.Error("share signature upload should be blocked")
	}
	if !experienceWriteAllowed(http.MethodGet, "/api/customer/list") {
		t.Error("GET should be allowed")
	}
}
