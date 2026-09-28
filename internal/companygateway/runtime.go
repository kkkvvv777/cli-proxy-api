package companygateway

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

const verifiedKey = "company.gateway.verified"

// Gateway belongs to one server, avoiding process-global employee registries.
type Gateway struct {
	Users *Store
	Audit *AuditStore
}

func Open(configPath, dataDir string) (*Gateway, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = defaultDataDir
	}
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(filepath.Dir(configPath), dataDir)
	}
	users, err := NewStore(dataDir)
	if err != nil {
		return nil, err
	}
	audit, err := NewAuditStore(filepath.Join(dataDir, "audit"))
	if err != nil {
		return nil, err
	}
	return &Gateway{Users: users, Audit: audit}, nil
}

func Verified(c *gin.Context) bool {
	value, _ := c.Get(verifiedKey)
	return value == true
}

func modelPath(path string) bool {
	return path == "/v1" || strings.HasPrefix(path, "/v1/") ||
		strings.HasPrefix(path, "/v1beta") || strings.HasPrefix(path, "/openai/v1") ||
		strings.HasPrefix(path, "/backend-api/codex")
}

// Middleware restricts company mode to its tested HTTP/SSE API surface and
// authenticates before legacy/plugin access providers can allow a shared key.
func (g *Gateway) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !modelPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		start := time.Now()
		entry := AuditEntry{Kind: "request", Timestamp: start.UTC(), UserID: "unknown",
			RequestID: uuid.NewString(), Endpoint: c.Request.Method + " " + c.Request.URL.Path}
		c.Header("X-Request-Id", entry.RequestID)
		logging.SkipGinRequestLogging(c)
		c.Request = c.Request.WithContext(logging.WithMetadataOnly(logging.WithRequestID(c.Request.Context(), entry.RequestID)))
		var observer *RequestAudit
		defer func() {
			recovered := recover()
			entry.RequestedModel, entry.Stream = handlers.RequestBodyMetadata(c)
			if recovered != nil {
				entry.StatusCode, entry.Failed = 500, true
				entry.LatencyMS = time.Since(start).Milliseconds()
				g.Audit.record(entry)
				panic(recovered)
			}
			entry.StatusCode = c.Writer.Status()
			entry.Failed = entry.StatusCode >= 400 || c.Request.Context().Err() != nil
			if observer != nil {
				entry.Failed = entry.Failed || observer.Failed()
			}
			entry.LatencyMS = time.Since(start).Milliseconds()
			g.Audit.record(entry)
		}()
		result, errAuth := g.Users.Authenticate(c.Request.Context(), c.Request)
		if errAuth != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": gin.H{"message": "Invalid or missing employee API key", "type": "authentication_error"}})
			return
		}
		entry.UserID = result.Metadata["company_user_id"]
		entry.UserName = result.Metadata["company_user_name"]
		allowed := c.Request.Method == "GET" && c.Request.URL.Path == "/v1/models"
		if c.Request.Method == "POST" {
			switch c.Request.URL.Path {
			case "/v1/chat/completions", "/v1/completions", "/v1/responses", "/v1/responses/compact":
				allowed = true
			}
		}
		if !allowed {
			c.AbortWithStatusJSON(404, gin.H{"error": gin.H{"message": "Endpoint not enabled in company mode", "type": "invalid_request_error"}})
			return
		}
		c.Set("userApiKey", "company:"+result.Principal)
		c.Set("accessProvider", result.Provider)
		c.Set("accessMetadata", result.Metadata)
		c.Set(verifiedKey, true)
		observer = &RequestAudit{Store: g.Audit, Base: entry}
		c.Request = c.Request.WithContext(coreusage.WithRequestPlugin(c.Request.Context(), observer))
		c.Next()
	}
}

// RegisterManagement receives only the existing management-authenticated group.
func (g *Gateway) RegisterManagement(group *gin.RouterGroup) {
	group.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
		c.Next()
	})
	group.GET("/users", g.Users.HandleListUsers)
	group.POST("/users", g.Users.HandleCreateUser)
	group.PATCH("/users/:id", g.Users.HandleUpdateUser)
	group.POST("/users/:id/rotate", g.Users.HandleRotateUser)
	group.DELETE("/users/:id", g.Users.HandleDeleteUser)
	group.GET("/audit", g.Audit.HandleListAudit)
}
