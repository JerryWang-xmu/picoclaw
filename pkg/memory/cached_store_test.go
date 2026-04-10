package memory

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/stretchr/testify/require"
)

// mockStore is a mock Store implementation for testing cache behavior
type mockStore struct {
	mu       sync.RWMutex
	data     map[string][]providers.Message
	summary  map[string]string
	getCount atomic.Int32
}

func newMockStore() *mockStore {
	return &mockStore{
		data:    make(map[string][]providers.Message),
		summary: make(map[string]string),
	}
}

func (m *mockStore) AddMessage(_ context.Context, sessionKey, role, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[sessionKey] = append(m.data[sessionKey], providers.Message{
		Role:    role,
		Content: content,
	})
	return nil
}

func (m *mockStore) AddFullMessage(_ context.Context, sessionKey string, msg providers.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[sessionKey] = append(m.data[sessionKey], msg)
	return nil
}

func (m *mockStore) GetHistory(_ context.Context, sessionKey string) ([]providers.Message, error) {
	m.getCount.Add(1)
	m.mu.RLock()
	defer m.mu.RUnlock()
	msgs := m.data[sessionKey]
	if msgs == nil {
		return []providers.Message{}, nil
	}
	result := make([]providers.Message, len(msgs))
	copy(result, msgs)
	return result, nil
}

func (m *mockStore) GetSummary(_ context.Context, sessionKey string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.summary[sessionKey], nil
}

func (m *mockStore) SetSummary(_ context.Context, sessionKey, summary string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summary[sessionKey] = summary
	return nil
}

func (m *mockStore) TruncateHistory(_ context.Context, sessionKey string, keepLast int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	msgs := m.data[sessionKey]
	if keepLast <= 0 {
		m.data[sessionKey] = []providers.Message{}
	} else if keepLast < len(msgs) {
		m.data[sessionKey] = msgs[len(msgs)-keepLast:]
	}
	return nil
}

func (m *mockStore) SetHistory(_ context.Context, sessionKey string, history []providers.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[sessionKey] = history
	return nil
}

func (m *mockStore) Compact(_ context.Context, _ string) error {
	return nil
}

func (m *mockStore) Close() error {
	return nil
}

func (m *mockStore) ListSessions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}

func (m *mockStore) GetGetCount() int {
	return int(m.getCount.Load())
}

func TestCachedStore_BasicOperations(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "test-session"

	// Test AddMessage and GetHistory
	err := cache.AddMessage(ctx, sessionKey, "user", "hello")
	require.NoError(t, err)

	history, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "hello", history[0].Content)

	// Second read should hit cache (no additional underlying store call)
	initialCount := mock.GetGetCount()
	history2, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Equal(t, history, history2)
	require.Equal(t, initialCount, mock.GetGetCount(), "Cache should prevent additional store calls")
}

func TestCachedStore_CacheUpdateOnWrite(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "test-session"

	// Add initial message
	err := cache.AddMessage(ctx, sessionKey, "user", "hello")
	require.NoError(t, err)

	// Read to populate cache
	history, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Len(t, history, 1)

	initialCount := mock.GetGetCount()

	// Add another message - should update cache proactively
	err = cache.AddMessage(ctx, sessionKey, "assistant", "hi there")
	require.NoError(t, err)

	// Read again - should hit cache since it was updated (not invalidated)
	history, err = cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, initialCount, mock.GetGetCount(), "Cache should be updated on write, not invalidated")
}

func TestCachedStore_CacheUpdateOnAddFullMessage(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "test-session"

	// Add initial message
	err := cache.AddMessage(ctx, sessionKey, "user", "hello")
	require.NoError(t, err)

	// Read to populate cache
	_, err = cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	initialCount := mock.GetGetCount()

	// Add full message - should update cache proactively
	msg := providers.Message{
		Role:    "assistant",
		Content: "full message",
	}
	err = cache.AddFullMessage(ctx, sessionKey, msg)
	require.NoError(t, err)

	// Read again - should hit cache since it was updated (not invalidated)
	history, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "full message", history[1].Content)
	require.Equal(t, initialCount, mock.GetGetCount(), "Cache should be updated on AddFullMessage, not invalidated")
}

func TestCachedStore_CacheInvalidationOnSetHistory(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "test-session"

	// Add initial messages
	for i := 0; i < 5; i++ {
		err := cache.AddMessage(ctx, sessionKey, "user", "msg")
		require.NoError(t, err)
	}

	// Read to populate cache
	_, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	initialCount := mock.GetGetCount()

	// SetHistory should invalidate cache
	newHistory := []providers.Message{
		{Role: "user", Content: "new1"},
		{Role: "assistant", Content: "new2"},
	}
	err = cache.SetHistory(ctx, sessionKey, newHistory)
	require.NoError(t, err)

	// Read again - should hit underlying store
	history, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, initialCount+1, mock.GetGetCount(), "Cache should be invalidated on SetHistory")
}

func TestCachedStore_CacheInvalidationOnTruncateHistory(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "test-session"

	// Add initial messages
	for i := 0; i < 10; i++ {
		err := cache.AddMessage(ctx, sessionKey, "user", "msg")
		require.NoError(t, err)
	}

	// Read to populate cache
	_, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	initialCount := mock.GetGetCount()

	// TruncateHistory should invalidate cache
	err = cache.TruncateHistory(ctx, sessionKey, 3)
	require.NoError(t, err)

	// Read again - should hit underlying store
	history, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Len(t, history, 3)
	require.Equal(t, initialCount+1, mock.GetGetCount(), "Cache should be invalidated on TruncateHistory")
}

func TestCachedStore_TTLExpiration(t *testing.T) {
	mock := newMockStore()
	// Use a very short TTL for testing
	cache := NewCachedStore(mock, 100*time.Millisecond, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "test-session"

	// Add message and read to populate cache
	err := cache.AddMessage(ctx, sessionKey, "user", "hello")
	require.NoError(t, err)

	_, err = cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	initialCount := mock.GetGetCount()

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Read again - should hit underlying store since cache expired
	_, err = cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Equal(t, initialCount+1, mock.GetGetCount(), "Cache should expire after TTL")
}

func TestCachedStore_LRUEviction(t *testing.T) {
	mock := newMockStore()
	// Small cache size for testing eviction
	cache := NewCachedStore(mock, 5*time.Minute, 3)
	defer cache.Close()

	ctx := context.Background()

	// Add messages for 4 different sessions (exceeds cache capacity)
	for i := 0; i < 4; i++ {
		sessionKey := "session-" + string(rune('a'+i))
		err := cache.AddMessage(ctx, sessionKey, "user", "msg")
		require.NoError(t, err)
	}

	// Read all sessions to populate cache
	for i := 0; i < 4; i++ {
		sessionKey := "session-" + string(rune('a'+i))
		_, err := cache.GetHistory(ctx, sessionKey)
		require.NoError(t, err)
	}
	initialCount := mock.GetGetCount()

	// Read first session again - should have been evicted (LRU)
	// session-a was read first, then session-b, c, d - so session-a is LRU
	_, err := cache.GetHistory(ctx, "session-a")
	require.NoError(t, err)
	require.Equal(t, initialCount+1, mock.GetGetCount(), "Oldest entry should have been evicted")

	// Now session-b is the oldest (LRU), session-a is most recent
	// Read session-b - should require store access
	_, err = cache.GetHistory(ctx, "session-b")
	require.NoError(t, err)
	require.Equal(t, initialCount+2, mock.GetGetCount(), "session-b should have been evicted")

	// session-c, session-d, and session-a should be in cache now
	// session-c is the oldest among remaining
	_, err = cache.GetHistory(ctx, "session-d")
	require.NoError(t, err)
	_, err = cache.GetHistory(ctx, "session-a")
	require.NoError(t, err)
	// Only 2 additional calls (for session-a and session-b)
	require.Equal(t, initialCount+2, mock.GetGetCount())
}

func TestCachedStore_ConcurrentAccess(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "concurrent-session"

	var wg sync.WaitGroup
	const numGoroutines = 20
	const numOperations = 50

	// First, populate some data
	for i := 0; i < 100; i++ {
		err := cache.AddMessage(ctx, sessionKey, "user", "msg")
		require.NoError(t, err)
	}

	// Concurrent writes and reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)

		// Writer
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_ = cache.AddMessage(ctx, sessionKey, "user", "msg")
			}
		}(i)

		// Reader
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				_, _ = cache.GetHistory(ctx, sessionKey)
			}
		}(i)
	}

	wg.Wait()

	// Verify final state has at least the initial messages
	history, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(history), 100, "Should have at least initial messages")
}

func TestCachedStore_ConcurrentMultipleSessions(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()

	var wg sync.WaitGroup
	const numSessions = 10
	const numGoroutinesPerSession = 5
	const numOperations = 20

	// First, populate each session with initial data
	for s := 0; s < numSessions; s++ {
		sessionKey := "session-" + string(rune('0'+s))
		for i := 0; i < 10; i++ {
			err := cache.AddMessage(ctx, sessionKey, "user", "msg")
			require.NoError(t, err)
		}
	}

	for s := 0; s < numSessions; s++ {
		sessionKey := "session-" + string(rune('0'+s))
		for g := 0; g < numGoroutinesPerSession; g++ {
			wg.Add(2)

			// Writer
			go func(sk string) {
				defer wg.Done()
				for i := 0; i < numOperations; i++ {
					_ = cache.AddMessage(ctx, sk, "user", "msg")
				}
			}(sessionKey)

			// Reader
			go func(sk string) {
				defer wg.Done()
				for i := 0; i < numOperations; i++ {
					_, _ = cache.GetHistory(ctx, sk)
				}
			}(sessionKey)
		}
	}

	wg.Wait()

	// Verify all sessions have at least initial messages
	for s := 0; s < numSessions; s++ {
		sessionKey := "session-" + string(rune('0'+s))
		history, err := cache.GetHistory(ctx, sessionKey)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(history), 10, "Session %s should have at least initial messages", sessionKey)
	}
}

func TestCachedStore_EmptySession(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()

	// GetHistory on non-existent session
	history, err := cache.GetHistory(ctx, "non-existent")
	require.NoError(t, err)
	require.NotNil(t, history)
	require.Len(t, history, 0)

	// Second read should hit cache
	initialCount := mock.GetGetCount()
	history2, err := cache.GetHistory(ctx, "non-existent")
	require.NoError(t, err)
	require.Equal(t, history, history2)
	require.Equal(t, initialCount, mock.GetGetCount())
}

func TestCachedStore_SummaryOperations(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "test-session"

	// Set and get summary
	err := cache.SetSummary(ctx, sessionKey, "test summary")
	require.NoError(t, err)

	summary, err := cache.GetSummary(ctx, sessionKey)
	require.NoError(t, err)
	require.Equal(t, "test summary", summary)

	// Update summary
	err = cache.SetSummary(ctx, sessionKey, "updated summary")
	require.NoError(t, err)

	summary, err = cache.GetSummary(ctx, sessionKey)
	require.NoError(t, err)
	require.Equal(t, "updated summary", summary)
}

func TestCachedStore_CompactPassthrough(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "test-session"

	// Add messages
	for i := 0; i < 10; i++ {
		err := cache.AddMessage(ctx, sessionKey, "user", "msg")
		require.NoError(t, err)
	}

	// Read to populate cache
	_, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	initialCount := mock.GetGetCount()

	// Compact should invalidate cache
	err = cache.Compact(ctx, sessionKey)
	require.NoError(t, err)

	// Read again - should hit underlying store
	_, err = cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Equal(t, initialCount+1, mock.GetGetCount(), "Cache should be invalidated on Compact")
}

func TestCachedStore_CacheHitRate(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "test-session"

	// Add messages
	for i := 0; i < 10; i++ {
		err := cache.AddMessage(ctx, sessionKey, "user", "msg")
		require.NoError(t, err)
	}

	// First read - cache miss
	_, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)

	// Multiple reads - should all be cache hits
	for i := 0; i < 100; i++ {
		_, err := cache.GetHistory(ctx, sessionKey)
		require.NoError(t, err)
	}

	// Should have only 2 underlying calls: initial write check + first read
	// Note: AddMessage doesn't call GetHistory, so only 1 read call
	require.Equal(t, 1, mock.GetGetCount(), "Should minimize underlying store calls")
}

func TestCachedStore_MultipleSessionsIsolation(t *testing.T) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()

	// Add messages to different sessions
	err := cache.AddMessage(ctx, "session-a", "user", "msg-a")
	require.NoError(t, err)
	err = cache.AddMessage(ctx, "session-b", "user", "msg-b")
	require.NoError(t, err)

	// Read both sessions
	historyA, err := cache.GetHistory(ctx, "session-a")
	require.NoError(t, err)
	historyB, err := cache.GetHistory(ctx, "session-b")
	require.NoError(t, err)

	// Verify isolation
	require.Len(t, historyA, 1)
	require.Len(t, historyB, 1)
	require.Equal(t, "msg-a", historyA[0].Content)
	require.Equal(t, "msg-b", historyB[0].Content)

	// Add to session-a should only update session-a's cache (not affect session-b)
	err = cache.AddMessage(ctx, "session-a", "user", "msg-a2")
	require.NoError(t, err)

	// session-b should still be cached (unchanged)
	initialCount := mock.GetGetCount()
	_, err = cache.GetHistory(ctx, "session-b")
	require.NoError(t, err)
	require.Equal(t, initialCount, mock.GetGetCount(), "session-b should still be cached")

	// session-a should also be cached (updated proactively)
	_, err = cache.GetHistory(ctx, "session-a")
	require.NoError(t, err)
	require.Equal(t, initialCount, mock.GetGetCount(), "session-a should also be cached (updated not invalidated)")

	// Verify session-a has the new message
	historyA, err = cache.GetHistory(ctx, "session-a")
	require.NoError(t, err)
	require.Len(t, historyA, 2)
	require.Equal(t, "msg-a2", historyA[1].Content)
}

func BenchmarkCachedStore_GetHistory_CacheHit(b *testing.B) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 1000)
	defer cache.Close()

	ctx := context.Background()

	// Populate with messages
	for i := 0; i < 100; i++ {
		_ = cache.AddMessage(ctx, "bench", "user", "message content")
	}

	// Populate cache
	_, _ = cache.GetHistory(ctx, "bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.GetHistory(ctx, "bench")
	}
}

func BenchmarkCachedStore_GetHistory_CacheMiss(b *testing.B) {
	mock := newMockStore()
	cache := NewCachedStore(mock, 5*time.Minute, 1000)
	defer cache.Close()

	ctx := context.Background()

	// Populate with messages
	for i := 0; i < 100; i++ {
		_ = cache.AddMessage(ctx, "bench", "user", "message content")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use different session keys to ensure cache misses
		sessionKey := "bench-" + string(rune('a'+i%26))
		_, _ = cache.GetHistory(ctx, sessionKey)
	}
}

// Integration test with real JSONLStore
func TestCachedStore_WithJSONLStore(t *testing.T) {
	dir := t.TempDir()
	underlying, err := NewJSONLStore(dir, WithSyncMode(SyncModeAlways))
	require.NoError(t, err)

	cache := NewCachedStore(underlying, 5*time.Minute, 100)
	defer cache.Close()

	ctx := context.Background()
	sessionKey := "integration-test"

	// Add messages
	for i := 0; i < 10; i++ {
		err := cache.AddMessage(ctx, sessionKey, "user", "message-"+string(rune('0'+i)))
		require.NoError(t, err)
	}

	// First read - cache miss
	history1, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Len(t, history1, 10)

	// Second read - cache hit
	history2, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Equal(t, history1, history2)

	// Add new message - should update cache proactively
	err = cache.AddMessage(ctx, sessionKey, "assistant", "response")
	require.NoError(t, err)

	// Read again - should hit cache and get updated data
	history3, err := cache.GetHistory(ctx, sessionKey)
	require.NoError(t, err)
	require.Len(t, history3, 11)
	require.Equal(t, "response", history3[10].Content)
}

// Benchmark comparison with and without cache
func BenchmarkCachedStore_JSONLBackend_CacheHit(b *testing.B) {
	dir := b.TempDir()
	underlying, _ := NewJSONLStore(dir, WithSyncMode(SyncModeAlways))
	cache := NewCachedStore(underlying, 5*time.Minute, 1000)
	defer cache.Close()

	ctx := context.Background()

	// Populate with 100 messages
	for i := 0; i < 100; i++ {
		_ = cache.AddMessage(ctx, "bench", "user", "message content")
	}

	// Populate cache
	_, _ = cache.GetHistory(ctx, "bench")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.GetHistory(ctx, "bench")
	}
}
