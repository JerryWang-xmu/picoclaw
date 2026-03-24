package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/stretchr/testify/require"
)

// TestSyncModeAlways tests that Always mode syncs on every write
func TestSyncModeAlways(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModeAlways))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Write a message
	err = store.AddMessage(ctx, "test-session", "user", "hello")
	require.NoError(t, err)

	// In Always mode, data should be immediately persisted
	// Verify by reading directly from file
	content, err := os.ReadFile(filepath.Join(dir, "test-session.jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(content), "hello")
}

// TestSyncModeOnClose tests that OnClose mode only syncs on close
func TestSyncModeOnClose(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModeOnClose))
	require.NoError(t, err)

	ctx := context.Background()

	// Write multiple messages
	for i := 0; i < 10; i++ {
		err = store.AddMessage(ctx, "test-session", "user", "message")
		require.NoError(t, err)
	}

	// Before close, file may not exist or be incomplete
	// After close, all data should be persisted
	err = store.Close()
	require.NoError(t, err)

	// Verify data is persisted
	history, err := store.GetHistory(ctx, "test-session")
	require.NoError(t, err)
	require.Len(t, history, 10)
}

// TestSyncModePeriodicByCount tests that Periodic mode syncs based on count threshold
func TestSyncModePeriodicByCount(t *testing.T) {
	dir := t.TempDir()
	// Use small threshold for testing
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(5, 100*time.Millisecond))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Write 4 messages - should not trigger sync yet
	for i := 0; i < 4; i++ {
		err = store.AddMessage(ctx, "test-session", "user", "message")
		require.NoError(t, err)
	}

	// Write 5th message - should trigger sync
	err = store.AddMessage(ctx, "test-session", "user", "message5")
	require.NoError(t, err)

	// Verify all messages are persisted
	history, err := store.GetHistory(ctx, "test-session")
	require.NoError(t, err)
	require.Len(t, history, 5)
}

// TestSyncModePeriodicByTime tests that Periodic mode syncs based on time threshold
func TestSyncModePeriodicByTime(t *testing.T) {
	dir := t.TempDir()
	// Use small time threshold for testing
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(100, 50*time.Millisecond))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Write a message
	err = store.AddMessage(ctx, "test-session", "user", "hello")
	require.NoError(t, err)

	// Wait for time threshold to pass
	time.Sleep(100 * time.Millisecond)

	// Write another message to trigger the time-based check
	err = store.AddMessage(ctx, "test-session", "user", "world")
	require.NoError(t, err)

	// Verify messages are persisted
	history, err := store.GetHistory(ctx, "test-session")
	require.NoError(t, err)
	require.Len(t, history, 2)
}

// TestSessionIsolation tests that buffers are isolated per session
func TestSessionIsolation(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(10, 100*time.Millisecond))
	require.NoError(t, err)

	ctx := context.Background()

	// Write to session1
	for i := 0; i < 5; i++ {
		err = store.AddMessage(ctx, "session1", "user", "msg")
		require.NoError(t, err)
	}

	// Write to session2
	for i := 0; i < 3; i++ {
		err = store.AddMessage(ctx, "session2", "user", "msg")
		require.NoError(t, err)
	}

	// Close to flush all buffers before verification
	err = store.Close()
	require.NoError(t, err)

	// Both should have their data persisted
	history1, err := store.GetHistory(ctx, "session1")
	require.NoError(t, err)
	require.Len(t, history1, 5)

	history2, err := store.GetHistory(ctx, "session2")
	require.NoError(t, err)
	require.Len(t, history2, 3)
}

// TestCloseFlushesAll tests that Close flushes all pending buffers
func TestCloseFlushesAll(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(100, time.Hour))
	require.NoError(t, err)

	ctx := context.Background()

	// Write to multiple sessions
	for i := 0; i < 5; i++ {
		err = store.AddMessage(ctx, "session1", "user", "msg")
		require.NoError(t, err)
		err = store.AddMessage(ctx, "session2", "user", "msg")
		require.NoError(t, err)
	}

	// Close should flush all
	err = store.Close()
	require.NoError(t, err)

	// Verify both sessions have data
	history1, err := store.GetHistory(ctx, "session1")
	require.NoError(t, err)
	require.Len(t, history1, 5)

	history2, err := store.GetHistory(ctx, "session2")
	require.NoError(t, err)
	require.Len(t, history2, 5)
}

// TestAddFullMessage tests batch writing with full message struct
func TestAddFullMessage(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModeAlways))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	msg := providers.Message{
		Role:    "assistant",
		Content: "test content",
	}

	err = store.AddFullMessage(ctx, "test-session", msg)
	require.NoError(t, err)

	history, err := store.GetHistory(ctx, "test-session")
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "assistant", history[0].Role)
	require.Equal(t, "test content", history[0].Content)
}

// TestConcurrentWrites tests thread safety of batch writing
func TestConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(50, 50*time.Millisecond))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Concurrent writes to same session
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				err := store.AddMessage(ctx, "shared-session", "user", "msg")
				require.NoError(t, err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all messages are persisted
	history, err := store.GetHistory(ctx, "shared-session")
	require.NoError(t, err)
	require.Len(t, history, 100)
}

// TestDefaultSyncMode tests that default mode is Periodic
func TestDefaultSyncMode(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir)
	require.NoError(t, err)
	defer store.Close()

	// Should use Periodic mode with default thresholds
	require.Equal(t, SyncModePeriodic, store.syncMode)
	require.Equal(t, defaultBatchCount, store.batchCountThreshold)
	require.Equal(t, defaultBatchInterval, store.batchTimeThreshold)
}

// TestBufferPoolReuse tests that sync.Pool reuses buffers
func TestBufferPoolReuse(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModePeriodic))
	require.NoError(t, err)

	ctx := context.Background()

	// Write messages to trigger buffer usage
	for i := 0; i < 20; i++ {
		err = store.AddMessage(ctx, "test-session", "user", "message content here")
		require.NoError(t, err)
	}

	// Close to flush buffers before verification
	err = store.Close()
	require.NoError(t, err)

	// The pool should have been used - we can't directly test this,
	// but we can verify the data is correct
	history, err := store.GetHistory(ctx, "test-session")
	require.NoError(t, err)
	require.Len(t, history, 20)
}

// TestPeriodicFlushOnNewWrite tests that periodic mode checks time on each write
func TestPeriodicFlushOnNewWrite(t *testing.T) {
	dir := t.TempDir()
	// Very long count threshold, short time threshold
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(10000, 30*time.Millisecond))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	// Write first message
	err = store.AddMessage(ctx, "test-session", "user", "first")
	require.NoError(t, err)

	// Wait for time threshold
	time.Sleep(50 * time.Millisecond)

	// Write second message - should trigger time-based flush
	err = store.AddMessage(ctx, "test-session", "user", "second")
	require.NoError(t, err)

	// Both should be persisted
	history, err := store.GetHistory(ctx, "test-session")
	require.NoError(t, err)
	require.Len(t, history, 2)
}

// TestSetHistoryWithBatchMode tests that SetHistory works with batch mode
func TestSetHistoryWithBatchMode(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(100, time.Hour))
	require.NoError(t, err)
	defer store.Close()

	ctx := context.Background()

	msgs := []providers.Message{
		{Role: "user", Content: "msg1"},
		{Role: "assistant", Content: "msg2"},
		{Role: "user", Content: "msg3"},
	}

	err = store.SetHistory(ctx, "test-session", msgs)
	require.NoError(t, err)

	history, err := store.GetHistory(ctx, "test-session")
	require.NoError(t, err)
	require.Len(t, history, 3)
}
