package handlers

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	companyGatewayVerifiedKey = "company.gateway.verified"
	requestPathOverrideKey    = "cliproxy.request.path.override"
)

// CompanyGatewayVerified reports whether the company-gateway middleware has
// authenticated the current request. It is intentionally a request marker,
// so the compatibility layer cannot affect ordinary CLIProxyAPI traffic.
func CompanyGatewayVerified(c *gin.Context) bool {
	if c == nil {
		return false
	}
	value, _ := c.Get(companyGatewayVerifiedKey)
	verified, _ := value.(bool)
	return verified
}

// WithRequestPath overrides the inbound route path used by provider
// executors. This is needed when a compatibility adapter accepts one public
// protocol and internally executes another endpoint.
func WithRequestPath(ctx context.Context, path string) context.Context {
	return context.WithValue(ctx, requestPathOverrideKey, strings.TrimSpace(path))
}

func requestPathOverride(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	path, _ := ctx.Value(requestPathOverrideKey).(string)
	return strings.TrimSpace(path)
}
