package companygateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v7/sdk/access"
)

const (
	accessProviderName = "company-api-key"
	defaultDataDir     = "data"
	usersFileName      = "company-users.json"
	auditFilePrefix    = "company-audit-"
)

var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
var (
	errInvalidUser = errors.New("invalid employee")
	errUserExists  = errors.New("employee already exists")
	errUserMissing = errors.New("employee not found")
)

// User is the public employee identity stored by the gateway.
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	KeyPrefix string    `json:"key_prefix"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type persistedUser struct {
	User
	KeyHash string `json:"key_hash"`
}

type usersFile struct {
	Version int             `json:"version"`
	Users   []persistedUser `json:"users"`
}

// Store persists employee API keys in a local JSON file.
type Store struct {
	mu     sync.RWMutex
	path   string
	users  map[string]persistedUser
	byHash map[string]string
}

// NewStore opens or creates the employee directory.
func NewStore(dataDir string) (*Store, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create company gateway data directory: %w", err)
	}

	store := &Store{
		path:   filepath.Join(dataDir, usersFileName),
		users:  make(map[string]persistedUser),
		byHash: make(map[string]string),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.persistLocked()
	}
	if err != nil {
		return fmt.Errorf("read company users: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return errors.New("company users file is empty")
	}

	var file usersFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse company users: %w", err)
	}
	if file.Version != 1 {
		return errors.New("unsupported company users file version")
	}
	hashes := make(map[string]bool, len(file.Users))
	for _, user := range file.Users {
		user.ID = normalizeID(user.ID)
		user.Name = strings.TrimSpace(user.Name)
		user.KeyHash = strings.ToLower(strings.TrimSpace(user.KeyHash))
		hash, errHash := hex.DecodeString(user.KeyHash)
		if !validID.MatchString(user.ID) || user.Name == "" || len(user.Name) > 200 || errHash != nil || len(hash) != sha256.Size {
			return errors.New("invalid company user record")
		}
		if _, exists := s.users[user.ID]; exists {
			return errors.New("duplicate employee ID")
		}
		if hashes[user.KeyHash] {
			return errors.New("duplicate employee key hash")
		}
		hashes[user.KeyHash] = true
		if user.CreatedAt.IsZero() {
			user.CreatedAt = user.UpdatedAt
		}
		if user.UpdatedAt.IsZero() {
			user.UpdatedAt = user.CreatedAt
		}
		s.users[user.ID] = user
		if user.Enabled {
			s.byHash[user.KeyHash] = user.ID
		}
	}
	return nil
}

func (s *Store) persistLocked() error {
	items := make([]persistedUser, 0, len(s.users))
	for _, user := range s.users {
		items = append(items, user)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	data, err := json.MarshalIndent(usersFile{Version: 1, Users: items}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode company users: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".company-users-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write company users: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("replace company users: %w", err)
	}
	return nil
}

// List returns employee records without secret material.
func (s *Store) List() []User {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, publicUser(user))
	}
	sort.Slice(users, func(i, j int) bool { return users[i].ID < users[j].ID })
	return users
}

// Create creates an employee and returns the generated key once.
func (s *Store) Create(id, name string) (User, string, error) {
	if s == nil {
		return User{}, "", errors.New("company user store is nil")
	}
	id = normalizeID(id)
	name = strings.TrimSpace(name)
	if !validID.MatchString(id) || name == "" || len(name) > 200 {
		return User{}, "", fmt.Errorf("%w: id must start with a letter or digit and contain 1-64 ASCII letters, digits, dots, underscores or hyphens; name must be 1-200 bytes", errInvalidUser)
	}

	key, err := generateKey()
	if err != nil {
		return User{}, "", err
	}
	now := time.Now().UTC()
	user := persistedUser{
		User: User{
			ID:        id,
			Name:      name,
			KeyPrefix: key[:minInt(12, len(key))],
			Enabled:   true,
			CreatedAt: now,
			UpdatedAt: now,
		},
		KeyHash: hashKey(key),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.users[id]; exists {
		return User{}, "", errUserExists
	}
	s.users[id] = user
	s.byHash[user.KeyHash] = id
	if err := s.persistLocked(); err != nil {
		delete(s.users, id)
		delete(s.byHash, user.KeyHash)
		return User{}, "", err
	}
	return publicUser(user), key, nil
}

// Update changes an employee's display name or enabled state.
func (s *Store) Update(id string, name *string, enabled *bool) (User, error) {
	id = normalizeID(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	user, exists := s.users[id]
	if !exists {
		return User{}, errUserMissing
	}
	previous := user
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" || len(trimmed) > 200 {
			return User{}, fmt.Errorf("%w: name must be 1-200 bytes", errInvalidUser)
		}
		user.Name = trimmed
	}
	if enabled != nil && user.Enabled != *enabled {
		user.Enabled = *enabled
		if user.Enabled {
			s.byHash[user.KeyHash] = user.ID
		} else {
			delete(s.byHash, user.KeyHash)
		}
	}
	user.UpdatedAt = time.Now().UTC()
	s.users[id] = user
	if err := s.persistLocked(); err != nil {
		s.users[id] = previous
		delete(s.byHash, user.KeyHash)
		if previous.Enabled {
			s.byHash[previous.KeyHash] = id
		}
		return User{}, err
	}
	return publicUser(user), nil
}

// Rotate replaces an employee's key and returns the new key once.
func (s *Store) Rotate(id string) (User, string, error) {
	id = normalizeID(id)
	key, err := generateKey()
	if err != nil {
		return User{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	user, exists := s.users[id]
	if !exists {
		return User{}, "", errUserMissing
	}
	previous := user
	delete(s.byHash, user.KeyHash)
	user.KeyHash = hashKey(key)
	user.KeyPrefix = key[:minInt(12, len(key))]
	user.UpdatedAt = time.Now().UTC()
	s.users[id] = user
	if user.Enabled {
		s.byHash[user.KeyHash] = user.ID
	}
	if err := s.persistLocked(); err != nil {
		delete(s.byHash, user.KeyHash)
		s.users[id] = previous
		if previous.Enabled {
			s.byHash[previous.KeyHash] = id
		}
		return User{}, "", err
	}
	return publicUser(user), key, nil
}

// Delete removes an employee and immediately revokes the key.
func (s *Store) Delete(id string) error {
	id = normalizeID(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	user, exists := s.users[id]
	if !exists {
		return errUserMissing
	}
	delete(s.users, id)
	delete(s.byHash, user.KeyHash)
	if err := s.persistLocked(); err != nil {
		s.users[id] = user
		if user.Enabled {
			s.byHash[user.KeyHash] = id
		}
		return err
	}
	return nil
}

// Authenticate implements the request access provider used by the proxy.
func (s *Store) Authenticate(_ context.Context, r *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if s == nil || r == nil {
		return nil, sdkaccess.NewNotHandledError()
	}
	key, source := requestKey(r)
	if key == "" {
		return nil, sdkaccess.NewNoCredentialsError()
	}
	hash := hashKey(key)
	s.mu.RLock()
	id, ok := s.byHash[hash]
	user := s.users[id]
	s.mu.RUnlock()
	if !ok || !user.Enabled {
		return nil, sdkaccess.NewInvalidCredentialError()
	}
	return &sdkaccess.Result{
		Provider:  accessProviderName,
		Principal: user.ID,
		Metadata: map[string]string{
			"source":             source,
			"company_user_id":    user.ID,
			"company_user_name":  user.Name,
			"company_key_prefix": user.KeyPrefix,
		},
	}, nil
}

func (s *Store) Identifier() string { return accessProviderName }

func requestKey(r *http.Request) (string, string) {
	if value := strings.TrimSpace(r.Header.Get("Authorization")); value != "" {
		parts := strings.SplitN(value, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return strings.TrimSpace(parts[1]), "authorization"
		}
		return value, "authorization"
	}
	for _, header := range []struct {
		name   string
		source string
	}{
		{"X-Api-Key", "x-api-key"},
		{"X-Goog-Api-Key", "x-goog-api-key"},
	} {
		if value := strings.TrimSpace(r.Header.Get(header.name)); value != "" {
			return value, header.source
		}
	}
	return "", ""
}

func generateKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate company API key: %w", err)
	}
	return "cpa_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func normalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func publicUser(user persistedUser) User {
	user.KeyHash = ""
	return user.User
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// HandleListUsers exposes users to the authenticated management API.
func (s *Store) HandleListUsers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"users": s.List()})
}

type userRequest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled"`
}

// HandleCreateUser creates a key and returns the secret exactly once.
func (s *Store) HandleCreateUser(c *gin.Context) {
	var req userRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	user, key, err := s.Create(req.ID, req.Name)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user, "api_key": key})
}

// HandleUpdateUser updates metadata or activation state.
func (s *Store) HandleUpdateUser(c *gin.Context) {
	var req struct {
		Name    *string `json:"name"`
		Enabled *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	user, err := s.Update(c.Param("id"), req.Name, req.Enabled)
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

// HandleRotateUser rotates an employee key.
func (s *Store) HandleRotateUser(c *gin.Context) {
	user, key, err := s.Rotate(c.Param("id"))
	if err != nil {
		writeStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user, "api_key": key})
}

// HandleDeleteUser revokes and removes an employee.
func (s *Store) HandleDeleteUser(c *gin.Context) {
	if err := s.Delete(c.Param("id")); err != nil {
		writeStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func writeStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errUserMissing):
		c.JSON(http.StatusNotFound, gin.H{"error": "company user not found"})
	case errors.Is(err, errUserExists):
		c.JSON(http.StatusConflict, gin.H{"error": "company user already exists"})
	case errors.Is(err, errInvalidUser):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "employee storage unavailable; check server disk and permissions"})
	}
}
