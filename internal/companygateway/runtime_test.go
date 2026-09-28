package companygateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
)

func TestModelRequestBodyPassesThroughWithoutCompanySizeCap(t *testing.T) {
	g, err := Open(filepath.Join(t.TempDir(), "config.yaml"), "data")
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := g.Users.Create("body-test", "Body Test")
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(g.Middleware())
	paths := []string{"/v1/responses", "/v1/responses/compact", "/v1/chat/completions", "/v1/completions", "/v1/images/generations", "/v1/images/edits"}
	var forwardedBytes int
	for _, path := range paths {
		router.POST(path, func(c *gin.Context) {
			body, errRead := handlers.ReadRequestBody(c)
			if errRead != nil || !json.Valid(body) {
				c.Status(http.StatusBadRequest)
				return
			}
			forwardedBytes = len(body)
			c.Status(http.StatusNoContent)
		})
	}
	for _, path := range paths {
		for _, chunked := range []bool{false, true} {
			t.Run(path+fmtBool(chunked), func(t *testing.T) {
				// Metadata at the end must still be audited beyond both former caps.
				body := `{"input":"` + strings.Repeat("x", 65<<20) + `","model":"test","stream":true}`
				req := httptest.NewRequest("POST", path, strings.NewReader(body))
				if chunked {
					req.ContentLength = -1
				}
				req.Header.Set("Authorization", "Bearer "+key)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code != http.StatusNoContent || forwardedBytes != len(body) {
					t.Fatalf("status=%d bytes=%d want=%d", w.Code, forwardedBytes, len(body))
				}
				entries, errList := g.Audit.List(1, "body-test")
				if errList != nil || len(entries) != 1 || entries[0].RequestedModel != "test" || !entries[0].Stream {
					t.Fatalf("audit metadata missing: %v %v", entries, errList)
				}
			})
		}
	}
	t.Run("compressed body", func(t *testing.T) {
		encoder, errEncoder := zstd.NewWriter(nil)
		if errEncoder != nil {
			t.Fatal(errEncoder)
		}
		defer encoder.Close()
		body := `{"input":"` + strings.Repeat("x", 256<<10) + `","model":"compressed","stream":true}`
		req := httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(encoder.EncodeAll([]byte(body), nil)))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Encoding", "zstd")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		entries, errList := g.Audit.List(1, "body-test")
		if w.Code != 204 || forwardedBytes != len(body) || errList != nil || len(entries) != 1 ||
			entries[0].RequestedModel != "compressed" || !entries[0].Stream {
			t.Fatalf("compressed body or audit failed: status=%d entries=%v err=%v", w.Code, entries, errList)
		}
	})
}

func fmtBool(chunked bool) string {
	if chunked {
		return "/unknown-length"
	}
	return "/known-length"
}
