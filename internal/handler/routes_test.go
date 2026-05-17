package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	RegisterRoutes(router, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", resp.Code, http.StatusOK)
	}
	if got := resp.Body.String(); got != "{\"status\":\"ok\"}" {
		t.Fatalf("GET /healthz body = %q, want %q", got, `{"status":"ok"}`)
	}
}
