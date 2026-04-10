package memory

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/providers"
)

const (
	// numLockShards is the fixed number of mutexes used to serialize
	// per-session access. Using a sharded array instead of a map keeps
	// memory bounded regardless of how many sessions are created over
	// the lifetime of the process — important for a long-running daemon.
	numLockShards = 64

	// maxLineSize is the maximum size of a single JSON line in a .jsonl
	// file. Tool results (read_file, web search, etc.) can be large, so
	// we set a generous limit. The scanner starts at 64 KB and grows
	// only as needed up to this cap.
	maxLineSize = 10 * 1024 * 1024 // 10 MB

	// defaultBatchCount is the default number of messages to buffer
	// before triggering an fsync in Periodic mode.
	defaultBatchCount = 100

	// defaultBatchInterval is the default maximum time to wait
	// before triggering an fsync in Periodic mode.
	defaultBatchInterval = 100 * time.Millisecond
)

// SyncMode controls when fsync is called for durability.
type SyncMode int

const (
	// SyncModeAlways syncs on every write (original behavior, safest but slowest).
	SyncModeAlways SyncMode = iota
	// SyncModePeriodic syncs when buffer reaches count threshold or time threshold.
	SyncModePeriodic
	// SyncModeOnClose only syncs on Close (fastest but riskiest).
	SyncModeOnClose
)

// sessionBuffer holds buffered writes for a single session.
type sessionBuffer struct {
	buf           *bytes.Buffer
	count         int
	lastFlushTime time.Time
}

// bufferPool is a sync.Pool for reusing byte buffers.
var bufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// sessionMeta holds per-session metadata stored in a .meta.json file.
type sessionMeta struct {
	Key       string    `json:"key"`
	Summary   string    `json:"summary"`
	Skip      int       `json:"skip"`
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JSONLStore implements Store using append-only JSONL files.
//
// Each session is stored as two files:
//
//	{sanitized_key}.jsonl      — one JSON-encoded message per line, append-only
//	{sanitized_key}.meta.json  — session metadata (summary, logical truncation offset)
//
// Messages are never physically deleted from the JSONL file. Instead,
// TruncateHistory records a "skip" offset in the metadata file and
// GetHistory ignores lines before that offset. This keeps all writes
// append-only, which is both fast and crash-safe.
type JSONLStore struct {
	dir                 string
	locks               [numLockShards]sync.Mutex
	syncMode            SyncMode
	batchCountThreshold int
	batchTimeThreshold  time.Duration
	buffers             map[string]*sessionBuffer
	buffersMu           sync.Mutex
}

// NewJSONLStore creates a new JSONL-backed store rooted at dir.
// Default sync mode is SyncModePeriodic with 100 messages or 100ms thresholds.
func NewJSONLStore(dir string, opts ...StoreOption) (*JSONLStore, error) {
	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("memory: create directory: %w", err)
	}

	store := &JSONLStore{
		dir:                 dir,
		syncMode:            SyncModePeriodic,
		batchCountThreshold: defaultBatchCount,
		batchTimeThreshold:  defaultBatchInterval,
		buffers:             make(map[string]*sessionBuffer),
	}

	for _, opt := range opts {
		opt(store)
	}

	return store, nil
}

// StoreOption is a functional option for configuring JSONLStore.
type StoreOption func(*JSONLStore)

// WithSyncMode sets the sync mode for the store.
func WithSyncMode(mode SyncMode) StoreOption {
	return func(s *JSONLStore) {
		s.syncMode = mode
	}
}

// WithBatchThresholds sets the batch count and time thresholds for Periodic mode.
func WithBatchThresholds(count int, interval time.Duration) StoreOption {
	return func(s *JSONLStore) {
		s.batchCountThreshold = count
		s.batchTimeThreshold = interval
	}
}

// sessionLock returns a mutex for the given session key.
// Keys are mapped to a fixed pool of shards via FNV hash, so
// memory usage is O(1) regardless of total session count.
func (s *JSONLStore) sessionLock(key string) *sync.Mutex {
	h := fnv.New32a()
	h.Write([]byte(key))
	return &s.locks[h.Sum32()%numLockShards]
}

func (s *JSONLStore) jsonlPath(key string) string {
	return filepath.Join(s.dir, sanitizeKey(key)+".jsonl")
}

func (s *JSONLStore) metaPath(key string) string {
	return filepath.Join(s.dir, sanitizeKey(key)+".meta.json")
}

// sanitizeKey converts a session key to a safe filename component.
// Mirrors pkg/session.sanitizeFilename so that migration paths match.
// Replaces ':' with '_' (session key separator) and '/' and '\' with '_'
// so composite IDs (e.g. Telegram forum "chatID/threadID", Slack "channel/thread_ts")
// do not create subdirectories or break on Windows.
func sanitizeKey(key string) string {
	s := strings.ReplaceAll(key, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

// readMeta loads the metadata file for a session.
// Returns a zero-value sessionMeta if the file does not exist.
func (s *JSONLStore) readMeta(key string) (sessionMeta, error) {
	data, err := os.ReadFile(s.metaPath(key))
	if os.IsNotExist(err) {
		return sessionMeta{Key: key}, nil
	}
	if err != nil {
		return sessionMeta{}, fmt.Errorf("memory: read meta: %w", err)
	}
	var meta sessionMeta
	err = json.Unmarshal(data, &meta)
	if err != nil {
		return sessionMeta{}, fmt.Errorf("memory: decode meta: %w", err)
	}
	return meta, nil
}

// writeMeta atomically writes the metadata file using the project's
// standard WriteFileAtomic (temp + fsync + rename).
func (s *JSONLStore) writeMeta(key string, meta sessionMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: encode meta: %w", err)
	}
	return fileutil.WriteFileAtomic(s.metaPath(key), data, 0o644)
}

// readMessages reads valid JSON lines from a .jsonl file, skipping
// the first `skip` lines without unmarshaling them. This avoids the
// cost of json.Unmarshal on logically truncated messages.
// Malformed trailing lines (e.g. from a crash) are silently skipped.
func readMessages(path string, skip int) ([]providers.Message, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []providers.Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("memory: open jsonl: %w", err)
	}
	defer f.Close()

	var msgs []providers.Message
	scanner := bufio.NewScanner(f)
	// Allow large lines for tool results (read_file, web search, etc.).
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	lineNum := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lineNum++
		if lineNum <= skip {
			continue
		}
		var msg providers.Message
		if err := json.Unmarshal(line, &msg); err != nil {
			// Corrupt line — likely a partial write from a crash.
			// Log so operators know data was skipped, but don't
			// fail the entire read; this is the standard JSONL
			// recovery pattern.
			log.Printf("memory: skipping corrupt line %d in %s: %v",
				lineNum, filepath.Base(path), err)
			continue
		}
		msgs = append(msgs, msg)
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("memory: scan jsonl: %w", scanner.Err())
	}

	if msgs == nil {
		msgs = []providers.Message{}
	}
	return msgs, nil
}

// countLines counts the total number of non-empty lines in a .jsonl file.
// Used by TruncateHistory to reconcile a stale meta.Count without
// the overhead of unmarshaling every message.
func countLines(path string) (int, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("memory: open jsonl: %w", err)
	}
	defer f.Close()

	n := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			n++
		}
	}
	return n, scanner.Err()
}

func (s *JSONLStore) AddMessage(
	_ context.Context, sessionKey, role, content string,
) error {
	return s.addMsg(sessionKey, providers.Message{
		Role:    role,
		Content: content,
	})
}

func (s *JSONLStore) AddFullMessage(
	_ context.Context, sessionKey string, msg providers.Message,
) error {
	return s.addMsg(sessionKey, msg)
}

// getBuffer gets or creates a buffer for the given session.
// Must be called with session lock held.
func (s *JSONLStore) getBuffer(sessionKey string) *sessionBuffer {
	s.buffersMu.Lock()
	defer s.buffersMu.Unlock()

	if buf, ok := s.buffers[sessionKey]; ok {
		return buf
	}

	buf := &sessionBuffer{
		buf:           bufferPool.Get().(*bytes.Buffer),
		lastFlushTime: time.Now(),
	}
	buf.buf.Reset()
	s.buffers[sessionKey] = buf
	return buf
}

// shouldFlush checks if the buffer should be flushed based on sync mode and thresholds.
// Must be called with session lock held.
func (s *JSONLStore) shouldFlush(buf *sessionBuffer) bool {
	switch s.syncMode {
	case SyncModeAlways:
		return true
	case SyncModeOnClose:
		return false
	case SyncModePeriodic:
		if buf.count >= s.batchCountThreshold {
			return true
		}
		if time.Since(buf.lastFlushTime) >= s.batchTimeThreshold {
			return true
		}
		return false
	}
	return true
}

// flushBuffer writes buffered data to disk and updates metadata.
// Must be called with session lock held.
func (s *JSONLStore) flushBuffer(sessionKey string, buf *sessionBuffer) error {
	if buf.buf.Len() == 0 {
		return nil
	}

	// Open file for append
	f, err := os.OpenFile(
		s.jsonlPath(sessionKey),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o644,
	)
	if err != nil {
		return fmt.Errorf("memory: open jsonl for append: %w", err)
	}

	// Write buffered data
	_, writeErr := f.Write(buf.buf.Bytes())
	if writeErr != nil {
		f.Close()
		return fmt.Errorf("memory: append messages: %w", writeErr)
	}

	// Sync to disk for durability
	if syncErr := f.Sync(); syncErr != nil {
		f.Close()
		return fmt.Errorf("memory: sync jsonl: %w", syncErr)
	}

	if closeErr := f.Close(); closeErr != nil {
		return fmt.Errorf("memory: close jsonl: %w", closeErr)
	}

	// Update metadata
	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}
	now := time.Now()
	if meta.Count == 0 && meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.Count += buf.count
	meta.UpdatedAt = now

	if err := s.writeMeta(sessionKey, meta); err != nil {
		return err
	}

	// Reset buffer
	buf.buf.Reset()
	buf.count = 0
	buf.lastFlushTime = now

	return nil
}

// addMsg is the shared implementation for AddMessage and AddFullMessage.
func (s *JSONLStore) addMsg(sessionKey string, msg providers.Message) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	// Serialize message using pooled buffer for zero-allocation encoding.
	buf := GetBuffer()
	enc := json.NewEncoder(buf)
	if err := enc.Encode(msg); err != nil {
		PutBuffer(buf)
		return fmt.Errorf("memory: marshal message: %w", err)
	}
	line := buf.Bytes()

	// Handle Always mode - immediate write
	if s.syncMode == SyncModeAlways {
		f, err := os.OpenFile(
			s.jsonlPath(sessionKey),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND,
			0o644,
		)
		if err != nil {
			PutBuffer(buf)
			return fmt.Errorf("memory: open jsonl for append: %w", err)
		}
		_, writeErr := f.Write(line)
		if writeErr != nil {
			f.Close()
			PutBuffer(buf)
			return fmt.Errorf("memory: append message: %w", writeErr)
		}
		if syncErr := f.Sync(); syncErr != nil {
			f.Close()
			PutBuffer(buf)
			return fmt.Errorf("memory: sync jsonl: %w", syncErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			PutBuffer(buf)
			return fmt.Errorf("memory: close jsonl: %w", closeErr)
		}

		// Return buffer to pool after use
		PutBuffer(buf)

		// Update metadata for single message
		meta, err := s.readMeta(sessionKey)
		if err != nil {
			return err
		}
		now := time.Now()
		if meta.Count == 0 && meta.CreatedAt.IsZero() {
			meta.CreatedAt = now
		}
		meta.Count++
		meta.UpdatedAt = now

		return s.writeMeta(sessionKey, meta)
	}

	// Handle Periodic and OnClose modes with buffering
	sessionBuf := s.getBuffer(sessionKey)
	sessionBuf.buf.Write(line)
	sessionBuf.count++

	// Return the temporary buffer to pool
	PutBuffer(buf)

	// Check if we should flush
	if s.shouldFlush(sessionBuf) {
		return s.flushBuffer(sessionKey, sessionBuf)
	}

	return nil
}

func (s *JSONLStore) GetHistory(
	_ context.Context, sessionKey string,
) ([]providers.Message, error) {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return nil, err
	}

	// Pass meta.Skip so readMessages skips those lines without
	// unmarshaling them — avoids wasted CPU on truncated messages.
	msgs, err := readMessages(s.jsonlPath(sessionKey), meta.Skip)
	if err != nil {
		return nil, err
	}

	return msgs, nil
}

func (s *JSONLStore) GetSummary(
	_ context.Context, sessionKey string,
) (string, error) {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return "", err
	}
	return meta.Summary, nil
}

func (s *JSONLStore) SetSummary(
	_ context.Context, sessionKey, summary string,
) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}
	now := time.Now()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.Summary = summary
	meta.UpdatedAt = now

	return s.writeMeta(sessionKey, meta)
}

func (s *JSONLStore) TruncateHistory(
	_ context.Context, sessionKey string, keepLast int,
) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}

	// Always reconcile meta.Count with the actual line count on disk.
	// A crash between the JSONL append and the meta update in addMsg
	// leaves meta.Count stale (e.g. file has 101 lines but meta says
	// 100). Counting lines is cheap — no unmarshal, just a scan — and
	// TruncateHistory is not a hot path, so always re-count.
	n, countErr := countLines(s.jsonlPath(sessionKey))
	if countErr != nil {
		return countErr
	}
	meta.Count = n

	if keepLast <= 0 {
		meta.Skip = meta.Count
	} else {
		effective := meta.Count - meta.Skip
		if keepLast < effective {
			meta.Skip = meta.Count - keepLast
		}
	}
	meta.UpdatedAt = time.Now()

	return s.writeMeta(sessionKey, meta)
}

func (s *JSONLStore) SetHistory(
	_ context.Context,
	sessionKey string,
	history []providers.Message,
) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}
	now := time.Now()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.Skip = 0
	meta.Count = len(history)
	meta.UpdatedAt = now

	// Write meta BEFORE rewriting the JSONL file. If we crash between
	// the two writes, meta has Skip=0 and the old file is still intact,
	// so GetHistory reads from line 1 — returning "too many" messages
	// rather than losing data. The next SetHistory call corrects this.
	err = s.writeMeta(sessionKey, meta)
	if err != nil {
		return err
	}

	return s.rewriteJSONL(sessionKey, history)
}

// Compact physically rewrites the JSONL file, dropping all logically
// skipped lines. This reclaims disk space that accumulates after
// repeated TruncateHistory calls.
//
// It is safe to call at any time; if there is nothing to compact
// (skip == 0) the method returns immediately.
func (s *JSONLStore) Compact(
	_ context.Context, sessionKey string,
) error {
	l := s.sessionLock(sessionKey)
	l.Lock()
	defer l.Unlock()

	meta, err := s.readMeta(sessionKey)
	if err != nil {
		return err
	}
	if meta.Skip == 0 {
		return nil
	}

	// Read only the active messages, skipping truncated lines
	// without unmarshaling them.
	active, err := readMessages(s.jsonlPath(sessionKey), meta.Skip)
	if err != nil {
		return err
	}

	// Write meta BEFORE rewriting the JSONL file. If the process
	// crashes between the two writes, meta has Skip=0 and the old
	// (uncompacted) file is still intact, so GetHistory reads from
	// line 1 — returning previously-truncated messages rather than
	// losing data. The next Compact or TruncateHistory corrects this.
	meta.Skip = 0
	meta.Count = len(active)
	meta.UpdatedAt = time.Now()

	err = s.writeMeta(sessionKey, meta)
	if err != nil {
		return err
	}

	return s.rewriteJSONL(sessionKey, active)
}

// rewriteJSONL atomically replaces the JSONL file with the given messages
// using the project's standard WriteFileAtomic (temp + fsync + rename).
func (s *JSONLStore) rewriteJSONL(
	sessionKey string, msgs []providers.Message,
) error {
	buf := GetBuffer()
	defer PutBuffer(buf)

	enc := json.NewEncoder(buf)
	for _, msg := range msgs {
		if err := enc.Encode(msg); err != nil {
			return fmt.Errorf("memory: encode message: %w", err)
		}
	}
	return fileutil.WriteFileAtomic(s.jsonlPath(sessionKey), buf.Bytes(), 0o644)
}

// ListSessions returns all known session keys by reading .meta.json files.
func (s *JSONLStore) ListSessions() []string {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var keys []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		// Read the meta file to get the original key
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue
		}
		var meta sessionMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if meta.Key != "" {
			keys = append(keys, meta.Key)
		}
	}
	return keys
}

func (s *JSONLStore) Close() error {
	s.buffersMu.Lock()
	defer s.buffersMu.Unlock()

	// Flush all pending buffers
	for sessionKey, buf := range s.buffers {
		// Need to acquire session lock for each flush
		l := s.sessionLock(sessionKey)
		l.Lock()

		if err := s.flushBuffer(sessionKey, buf); err != nil {
			l.Unlock()
			return fmt.Errorf("memory: flush buffer for session %s: %w", sessionKey, err)
		}

		// Return buffer to pool
		bufferPool.Put(buf.buf)
		l.Unlock()
	}

	// Clear the buffers map
	s.buffers = make(map[string]*sessionBuffer)
	return nil
}
