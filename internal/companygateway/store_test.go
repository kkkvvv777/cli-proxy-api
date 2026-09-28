package companygateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

func accepted(s *Store, key string) bool {
	r := httptest.NewRequest("GET", "/v1/models", nil)
	r.Header.Set("Authorization", "Bearer "+key)
	_, err := s.Authenticate(context.Background(), r)
	return err == nil
}

func TestStoreHTTPFailureIsServerError(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(t.TempDir(), "missing", "users.json")
	router := gin.New()
	router.POST("/users", store.HandleCreateUser)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/users", strings.NewReader(`{"id":"e001","name":"One"}`)))
	if w.Code != 500 || strings.Contains(w.Body.String(), store.path) {
		t.Fatalf("unexpected storage error response: %d %s", w.Code, w.Body.String())
	}
	if len(store.List()) != 0 {
		t.Fatal("failed creation changed employee list")
	}
}

func TestKeyLifecycleAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	user, key, err := s.Create("E001", "Employee One")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "e001" || !accepted(s, key) || accepted(s, "invalid") {
		t.Fatal("authentication or identity mismatch")
	}
	disk, _ := os.ReadFile(s.path)
	public, _ := json.Marshal(s.List())
	if strings.Contains(string(disk), key) || strings.Contains(string(public), hashKey(key)) {
		t.Fatal("key exposed")
	}
	info, _ := os.Stat(s.path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	disabled := false
	if _, err := s.Update(user.ID, nil, &disabled); err != nil {
		t.Fatal(err)
	}
	if accepted(s, key) {
		t.Fatal("disabled key accepted")
	}
	_, rotated, err := s.Rotate(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted(s, key) || accepted(s, rotated) {
		t.Fatal("rotation enabled a disabled employee")
	}
	enabled := true
	_, err = s.Update(user.ID, nil, &enabled)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if accepted(reopened, key) || !accepted(reopened, rotated) {
		t.Fatal("restart lost rotation")
	}
	if err := reopened.Delete(user.ID); err != nil {
		t.Fatal(err)
	}
	if accepted(reopened, rotated) {
		t.Fatal("deleted key accepted")
	}
	reopened, err = NewStore(dir)
	if err != nil || len(reopened.List()) != 0 {
		t.Fatal("deletion not persisted", err)
	}
}

func TestFailedPersistenceRollsBack(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := s.Create("e001", "One")
	if err != nil {
		t.Fatal(err)
	}
	s.path = filepath.Join(t.TempDir(), "missing", "users.json")
	if _, _, err := s.Rotate("e001"); err == nil {
		t.Fatal("rotation unexpectedly succeeded")
	}
	disabled := false
	if _, err := s.Update("e001", nil, &disabled); err == nil {
		t.Fatal("update unexpectedly succeeded")
	}
	if err := s.Delete("e001"); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	if !accepted(s, key) {
		t.Fatal("failed transaction revoked key")
	}
}

func TestCorruptStoreFailsClosed(t *testing.T) {
	for _, content := range []string{"", "{", `{"version":99}`, `{"version":1,"users":[{"id":"bad","name":"Bad","key_hash":"invalid"}]}`} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, usersFileName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(dir); err == nil {
			t.Fatalf("accepted %q", content)
		}
	}
}

func TestConcurrentAuthentication(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := s.Create("e001", "One")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if !accepted(s, key) {
					t.Error("valid key rejected")
				}
			}
		}()
	}
	wg.Wait()
}
