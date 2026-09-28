package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/runtime/executor"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	"golang.org/x/crypto/bcrypt"
)

func TestCompanyManagementAndAccess(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	hash, err := bcrypt.GenerateFromPassword([]byte("admin-secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{CompanyGateway: config.CompanyGatewayConfig{Enabled: true, DataDir: "data"},
		RemoteManagement: config.RemoteManagement{SecretKey: string(hash)},
		SDKConfig:        config.SDKConfig{APIKeys: []string{"legacy-shared-key"}}}
	server := NewServer(cfg, auth.NewManager(nil, nil, nil), sdkaccess.NewManager(), filepath.Join(t.TempDir(), "config.yaml"))
	if server.companyInitError != nil {
		t.Fatal(server.companyInitError)
	}
	do := func(method, path, key, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w
	}
	created := do("POST", "/v0/management/company/users", "admin-secret", `{"id":"e001","name":"One"}`)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var result struct {
		Key string `json:"api_key"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, key string
		want      int
	}{
		{"/v1/models", result.Key, 200}, {"/v1/models", "legacy-shared-key", 401},
		{"/v0/management/company/users", result.Key, 401},
		{"/v0/management/auth-files", result.Key, 401},
		{"/v0/management/company/users", "admin-secret", 200},
		{"/management.html?company=1", "", http.StatusTemporaryRedirect},
	} {
		w := do("GET", tc.path, tc.key, "")
		if w.Code != tc.want {
			t.Errorf("%s got %d want %d: %s", tc.path, w.Code, tc.want, w.Body.String())
		}
	}
	disabled := do("PATCH", "/v0/management/company/users/e001", "admin-secret", `{"enabled":false}`)
	if disabled.Code != 200 {
		t.Fatal(disabled.Code, disabled.Body.String())
	}
	if w := do("GET", "/v1/models", result.Key, ""); w.Code != http.StatusUnauthorized {
		t.Fatal("disabled employee passed")
	}
	if server.requestLogger != nil {
		t.Fatal("body logger enabled in company mode")
	}
	if cfg.SDKConfig.RequestLog {
		t.Fatal("unexpected request log")
	}
}

func TestCompanyManagementUsesUnifiedAsset(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	dir := t.TempDir()
	t.Setenv("MANAGEMENT_STATIC_PATH", dir)
	cfg := &config.Config{CompanyGateway: config.CompanyGatewayConfig{Enabled: true, DataDir: "data"}}
	server := NewServer(cfg, auth.NewManager(nil, nil, nil), sdkaccess.NewManager(), filepath.Join(dir, "config.yaml"))
	if server.companyInitError != nil {
		t.Fatal(server.companyInitError)
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/management.html", nil))
	if w.Code != 503 {
		t.Fatalf("missing company build should not download an incompatible panel: %d", w.Code)
	}
	const panel = "<html><body><div id=\"root\">unified-management</div></body></html>"
	if err := os.WriteFile(filepath.Join(dir, "management.html"), []byte(panel), 0o600); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/management.html", nil))
	if w.Code != 200 || w.Body.String() != panel {
		t.Fatalf("panel should be served unmodified: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/management.html?company=1", nil))
	if w.Code != 307 || w.Header().Get("Location") != "/management.html#/config?field=apiKeys" {
		t.Fatal("legacy panel did not redirect into unified management")
	}
}

func TestCompanyRealExecutorAudit(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/fail=%t", stream, fail), func(t *testing.T) {
				t.Setenv("MANAGEMENT_PASSWORD", "")
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer upstream-secret" {
						t.Error("upstream credential not used")
					}
					if fail {
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = w.Write([]byte(`{"error":{"message":"private-upstream-error"}}`))
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = w.Write([]byte("data: {\"id\":\"test\",\"model\":\"actual-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"private-answer\"}}]}\n\n"))
						w.(http.Flusher).Flush()
						_, _ = w.Write([]byte("data: {\"model\":\"actual-model\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\ndata: [DONE]\n\n"))
					} else {
						_, _ = w.Write([]byte(`{"id":"test","model":"actual-model","choices":[{"index":0,"message":{"role":"assistant","content":"private-answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`))
					}
				}))
				defer upstream.Close()
				dir := t.TempDir()
				cfg := &config.Config{CompanyGateway: config.CompanyGatewayConfig{Enabled: true, DataDir: "data"}}
				manager := auth.NewManager(nil, nil, nil)
				manager.SetConfig(cfg)
				manager.RegisterExecutor(executor.NewOpenAICompatExecutor("company-test", cfg))
				credential := &auth.Auth{ID: t.Name(), Provider: "company-test", Status: auth.StatusActive,
					Attributes: map[string]string{"base_url": upstream.URL, "api_key": "upstream-secret"}}
				if _, err := manager.Register(context.Background(), credential); err != nil {
					t.Fatal(err)
				}
				registry.GetGlobalRegistry().RegisterClient(credential.ID, credential.Provider, []*registry.ModelInfo{{ID: "company-model"}})
				t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(credential.ID) })
				server := NewServer(cfg, manager, sdkaccess.NewManager(), filepath.Join(dir, "config.yaml"))
				if server.companyInitError != nil {
					t.Fatal(server.companyInitError)
				}
				// Exercise the real handler, auth scheduler, translator and HTTP executor.
				workers := 1
				if !fail {
					workers = 30
				}
				keys := make([]string, workers)
				for i := range keys {
					_, key, err := server.company.Users.Create(fmt.Sprintf("e%02d", i), "Employee")
					if err != nil {
						t.Fatal(err)
					}
					keys[i] = key
				}
				var wg sync.WaitGroup
				for _, key := range keys {
					wg.Add(1)
					go func() {
						defer wg.Done()
						body := fmt.Sprintf(`{"model":"company-model","messages":[{"role":"user","content":"private-prompt"}],"stream":%t}`, stream)
						req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
						req.Header.Set("Authorization", "Bearer "+key)
						w := httptest.NewRecorder()
						server.Handler().ServeHTTP(w, req)
						if (!fail && w.Code != 200) || (fail && w.Code != 401) {
							t.Errorf("unexpected HTTP %d: %s", w.Code, w.Body.String())
						}
					}()
				}
				wg.Wait()
				entries, err := server.company.Audit.List(1000, "")
				if err != nil {
					t.Fatal(err)
				}
				requests, attempts := 0, 0
				for _, entry := range entries {
					if entry.Failed != fail || entry.Stream != stream || entry.RequestedModel != "company-model" || entry.RequestID == "" {
						t.Errorf("invalid request metadata: %+v", entry)
					}
					if entry.Kind == "request" {
						requests++
					} else {
						attempts++
						if entry.Provider != "company-test" || (!fail && (entry.TotalTokens != 10 || entry.ResponseModel != "actual-model")) {
							t.Errorf("invalid upstream metadata: %+v", entry)
						}
					}
				}
				if requests != workers || attempts != workers {
					t.Fatalf("requests=%d attempts=%d want %d each", requests, attempts, workers)
				}
				files, _ := filepath.Glob(filepath.Join(dir, "data", "audit", "*.jsonl"))
				for _, file := range files {
					body, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					for _, secret := range append(keys, "private-prompt", "private-answer", "private-upstream-error", "upstream-secret") {
						if strings.Contains(string(body), secret) {
							t.Fatal("audit contains private data")
						}
					}
				}
			})
		}
	}
}

func TestCompanyCorruptionAndReloadFailClosed(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{CompanyGateway: config.CompanyGatewayConfig{Enabled: true, DataDir: "data"}}
	server := NewServer(cfg, auth.NewManager(nil, nil, nil), sdkaccess.NewManager(), path)
	next := cfg.CloneForRuntime()
	next.CompanyGateway.Enabled = false
	if !server.UpdateClientsContext(context.Background(), next) || !server.cfg.CompanyGateway.Enabled {
		t.Fatal("hot reload disabled company authentication")
	}
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/models", nil))
	if w.Code != 401 {
		t.Fatal("unauthenticated request accepted after reload")
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "data", "company-users.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	restarted := NewServer(cfg, auth.NewManager(nil, nil, nil), sdkaccess.NewManager(), path)
	if restarted.companyInitError == nil || restarted.Start() == nil {
		t.Fatal("corrupt employee data accepted")
	}
	w = httptest.NewRecorder()
	restarted.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/models", nil))
	if w.Code != 503 {
		t.Fatal("failed initialization did not fail closed")
	}
}

func TestCompanyAPIKeyOAuthFailover(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	var seen []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.Header.Get("X-Api-Key")
		}
		mu.Lock()
		seen = append(seen, token)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(token, "expired-api-fixture") {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"expired"}}`))
			return
		}
		if !strings.Contains(token, "oauth-fixture") {
			t.Errorf("unexpected upstream token source")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-fixture\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"company-claude\",\"content\":[],\"usage\":{\"input_tokens\":7,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer upstream.Close()
	cfg := &config.Config{CompanyGateway: config.CompanyGatewayConfig{Enabled: true, DataDir: "data"}}
	manager := auth.NewManager(nil, &codexSearchAPIKeyFirstSelector{}, nil)
	manager.SetConfig(cfg)
	manager.SetRetryConfig(1, 0, 3)
	manager.RegisterExecutor(executor.NewClaudeExecutor(cfg))
	credentials := []*auth.Auth{
		{ID: "company-api-fixture", Provider: "claude", Status: auth.StatusActive,
			Attributes: map[string]string{"base_url": upstream.URL, "api_key": "expired-api-fixture"}},
		{ID: "company-oauth-fixture", Provider: "claude", Status: auth.StatusActive,
			Attributes: map[string]string{"base_url": upstream.URL},
			Metadata:   map[string]any{"access_token": "oauth-fixture"}},
	}
	for _, credential := range credentials {
		if _, err := manager.Register(context.Background(), credential); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(credential.ID, "claude", []*registry.ModelInfo{{ID: "company-claude"}})
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(credential.ID) })
	}
	server := NewServer(cfg, manager, sdkaccess.NewManager(), filepath.Join(t.TempDir(), "config.yaml"))
	if server.companyInitError != nil {
		t.Fatal(server.companyInitError)
	}
	_, key, err := server.company.Users.Create("e1", "Employee")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"company-claude","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("failover HTTP %d: %s", w.Code, w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || !strings.Contains(seen[0], "expired-api-fixture") || !strings.Contains(seen[1], "oauth-fixture") {
		t.Fatalf("expected API key then OAuth, got %d attempts", len(seen))
	}
	entries, err := server.company.Audit.List(10, "e1")
	if err != nil || len(entries) != 3 {
		t.Fatalf("expected 2 attempts and request result: %v %+v", err, entries)
	}
	if entries[0].Kind != "request" || entries[0].Failed || entries[0].StatusCode != 200 ||
		entries[1].Failed || entries[1].TotalTokens != 10 ||
		!entries[2].Failed || entries[2].StatusCode != 401 {
		t.Fatalf("incorrect failover audit: %+v", entries)
	}
}
