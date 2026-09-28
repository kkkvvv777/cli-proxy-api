package companygateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// AuditEntry intentionally excludes bodies, credentials, headers and upstream URLs.
type AuditEntry struct {
	Kind           string    `json:"kind"`
	Timestamp      time.Time `json:"timestamp"`
	UserID         string    `json:"user_id"`
	UserName       string    `json:"user_name"`
	Model          string    `json:"model"`
	RequestedModel string    `json:"requested_model,omitempty"`
	Provider       string    `json:"provider"`
	ResponseModel  string    `json:"response_model,omitempty"`
	StatusCode     int       `json:"status_code"`
	Failed         bool      `json:"failed"`
	InputTokens    int64     `json:"input_tokens"`
	OutputTokens   int64     `json:"output_tokens"`
	TotalTokens    int64     `json:"total_tokens"`
	LatencyMS      int64     `json:"latency_ms"`
	TTFTMS         int64     `json:"ttft_ms"`
	Stream         bool      `json:"stream"`
	RequestID      string    `json:"request_id,omitempty"`
	Endpoint       string    `json:"endpoint,omitempty"`
}

type AuditStore struct {
	dir       string
	mu        sync.Mutex
	lastError bool
}

// RequestAudit is request-scoped and never retains request/response bodies.
type RequestAudit struct {
	Store      *AuditStore
	Base       AuditEntry
	mu         sync.Mutex
	lastFailed bool
}

func (r *RequestAudit) Failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastFailed
}

func NewAuditStore(dir string) (*AuditStore, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("audit directory is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create audit directory: %w", err)
	}
	return &AuditStore{dir: dir}, nil
}

func (r *RequestAudit) HandleUsage(ctx context.Context, record coreusage.Record) {
	if r == nil || r.Store == nil {
		return
	}
	entry := r.Base
	entry.Kind = "attempt"
	entry.Timestamp = record.RequestedAt.UTC()
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	entry.Model = strings.TrimSpace(record.Model)
	if entry.RequestedModel == "" {
		entry.RequestedModel = strings.TrimSpace(record.Alias)
		if entry.RequestedModel == "" {
			entry.RequestedModel = coreusage.RequestedModelAliasFromContext(ctx)
		}
	}
	entry.Provider = strings.TrimSpace(record.Provider)
	entry.ResponseModel = strings.TrimSpace(record.ResponseModel)
	entry.StatusCode = record.Fail.StatusCode
	if entry.StatusCode <= 0 {
		entry.StatusCode = logging.GetResponseStatus(ctx)
	}
	if entry.StatusCode <= 0 {
		entry.StatusCode = http.StatusOK
		if record.Failed {
			entry.StatusCode = http.StatusInternalServerError
		}
	}
	entry.Failed = record.Failed || entry.StatusCode >= http.StatusBadRequest
	r.mu.Lock()
	r.lastFailed = entry.Failed
	r.mu.Unlock()
	detail := coreusage.EnsureTokenBreakdownForProvider(record.Detail, record.Provider, record.ExecutorType)
	entry.InputTokens = detail.TokenBreakdown.Input.TotalTokens
	entry.OutputTokens = detail.TokenBreakdown.Output.TotalTokens
	entry.TotalTokens = detail.TokenBreakdown.TotalTokens
	entry.LatencyMS = record.Latency.Milliseconds()
	entry.TTFTMS = record.TTFT.Milliseconds()
	entry.Stream = record.Stream || coreusage.StreamFromContext(ctx)
	if entry.RequestID == "" {
		entry.RequestID = strings.TrimSpace(logging.GetRequestID(ctx))
	}
	r.Store.record(entry)
}

func (s *AuditStore) record(entry AuditEntry) {
	if err := s.append(entry); err != nil {
		log.WithError(err).Error("company audit unavailable")
	}
}

func (s *AuditStore) Healthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.lastError
}

func (s *AuditStore) append(entry AuditEntry) (err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() { s.lastError = err != nil }()

	day := time.Now().UTC().Format("2006-01-02")
	matches, err := filepath.Glob(filepath.Join(s.dir, auditFilePrefix+day+"-*.jsonl"))
	if err != nil {
		return err
	}
	sort.Strings(matches)
	sequence := 0
	path := filepath.Join(s.dir, fmt.Sprintf("%s%s-%06d.jsonl", auditFilePrefix, day, sequence))
	if len(matches) > 0 {
		path = matches[len(matches)-1]
		info, errStat := os.Stat(path)
		if errStat != nil {
			return errStat
		}
		if info.Size() >= 10<<20 {
			if _, errScan := fmt.Sscanf(filepath.Base(path), auditFilePrefix+day+"-%06d.jsonl", &sequence); errScan != nil {
				return errScan
			}
			path = filepath.Join(s.dir, fmt.Sprintf("%s%s-%06d.jsonl", auditFilePrefix, day, sequence+1))
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err = file.Write(append(data, '\n')); err == nil {
		err = file.Sync()
	}
	return err
}

func (s *AuditStore) List(limit int, userID string) ([]AuditEntry, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("limit must be 1-1000")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	paths, err := filepath.Glob(filepath.Join(s.dir, auditFilePrefix+"*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))

	entries := make([]AuditEntry, 0, limit)
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		lines := make([]AuditEntry, limit)
		count := 0
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			var entry AuditEntry
			if json.Unmarshal(scanner.Bytes(), &entry) != nil {
				continue
			}
			if userID == "" || strings.EqualFold(userID, entry.UserID) {
				lines[count%limit] = entry
				count++
			}
		}
		errRead := errors.Join(scanner.Err(), file.Close())
		if errRead != nil {
			return nil, errRead
		}
		for i := count - 1; i >= 0 && i >= count-limit && len(entries) < limit; i-- {
			entries = append(entries, lines[i%limit])
		}
		if len(entries) == limit {
			break
		}
	}
	return entries, nil
}

func (s *AuditStore) HandleListAudit(c *gin.Context) {
	limit := 100
	if value := c.Query("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 1000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be 1-1000"})
			return
		}
		limit = parsed
	}
	entries, err := s.List(limit, strings.TrimSpace(c.Query("user_id")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "audit unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries, "healthy": s.Healthy()})
}
