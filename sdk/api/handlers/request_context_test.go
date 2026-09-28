package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestPathOverrideWinsOverGinRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/v1/responses", func(c *gin.Context) {
		ctx := WithRequestPath(c.Request.Context(), "/v1/images/generations")
		ctx = context.WithValue(ctx, "gin", c)
		metadata := requestExecutionMetadata(ctx)
		if got := metadata["request_path"]; got != "/v1/images/generations" {
			t.Fatalf("request path = %v, want /v1/images/generations", got)
		}
	})
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
}
