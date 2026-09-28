package companygateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/api/handlers"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func TestAuditPrivacyRestartAndRotation(t *testing.T) {
	dir := t.TempDir()
	store, err := NewAuditStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	request := &RequestAudit{Store: store, Base: AuditEntry{UserID: "e001", UserName: "One", RequestID: "request-1"}}
	record := coreusage.Record{Provider: "openai", Model: "model", Stream: true, APIKey: "secret-key",
		Fail: coreusage.Failure{StatusCode: 429, Body: "secret-prompt"}, Failed: true,
		Detail: coreusage.Detail{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}}
	request.HandleUsage(context.Background(), record)
	paths, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	body, _ := os.ReadFile(paths[0])
	if strings.Contains(string(body), "secret") {
		t.Fatal("sensitive data persisted")
	}
	file, err := os.OpenFile(paths[0], os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Truncate(10 << 20); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	request.HandleUsage(context.Background(), record)
	paths, _ = filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(paths) != 2 {
		t.Fatalf("rotation: %v", paths)
	}
	reopened, err := NewAuditStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	items, err := reopened.List(1, "e001")
	if err != nil || len(items) != 1 {
		t.Fatal(err, items)
	}
	if !items[0].Failed || !items[0].Stream || items[0].TotalTokens != 12 || items[0].RequestID != "request-1" {
		t.Fatalf("%+v", items[0])
	}
}

func TestThirtyConcurrentRequests(t *testing.T) {
	g, err := Open(filepath.Join(t.TempDir(), "config.yaml"), "data")
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := g.Users.Create("employee", "Employee")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(g.Middleware())
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		coreusage.PublishRecord(c.Request.Context(), coreusage.Record{Provider: "mock", Model: "model", Stream: true, Detail: coreusage.Detail{InputTokens: 7, OutputTokens: 3, TotalTokens: 10}})
		c.Data(200, "text/event-stream", []byte("data: [DONE]\n\n"))
	})
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"model","stream":true,"messages":[{"role":"user","content":"secret-prompt"}]}`))
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Errorf("HTTP %d", w.Code)
			}
		}()
	}
	wg.Wait()
	items, err := g.Audit.List(100, "employee")
	if err != nil || len(items) != 60 {
		t.Fatalf("expected 30 requests + 30 attempts, got %d: %v", len(items), err)
	}
	ids := map[string]int{}
	var tokens int64
	for _, item := range items {
		ids[item.RequestID]++
		tokens += item.TotalTokens
	}
	if len(ids) != 30 || tokens != 300 {
		t.Fatalf("IDs %d tokens %d", len(ids), tokens)
	}
	for id, n := range ids {
		if n != 2 {
			t.Fatalf("request %s has %d records", id, n)
		}
	}
}

func TestAuditIncludesEarlyFailures(t *testing.T) {
	g, err := Open(filepath.Join(t.TempDir(), "config.yaml"), "data")
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := g.Users.Create("e1", "One")
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(g.Middleware())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		body, errRead := handlers.ReadRequestBody(c)
		if errRead != nil || !json.Valid(body) {
			c.Status(400)
			return
		}
		c.Status(503)
	})
	for _, tc := range []struct {
		key, body string
		status    int
	}{
		{"bad", `{}`, 401}, {key, `{`, 400}, {key, `{"model":"missing"}`, 503},
	} {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+tc.key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d", w.Code, tc.status)
		}
	}
	items, err := g.Audit.List(10, "")
	if err != nil || len(items) != 3 {
		t.Fatal(err, len(items))
	}
	for _, item := range items {
		if !item.Failed || item.Kind != "request" {
			t.Fatalf("%+v", item)
		}
	}
}

func TestAuditDiskFailureReported(t *testing.T) {
	store, err := NewAuditStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.dir = filepath.Join(store.dir, "missing")
	err = store.append(AuditEntry{Timestamp: time.Now()})
	if err == nil || store.Healthy() {
		t.Fatal("failure not reported")
	}
}

func TestBoundedRecentAudit(t *testing.T) {
	store, err := NewAuditStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := store.append(AuditEntry{UserID: "one", RequestID: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.List(3, "one")
	if err != nil || len(items) != 3 || items[0].RequestID != "19" || items[2].RequestID != "17" {
		t.Fatal(err, items)
	}
}
