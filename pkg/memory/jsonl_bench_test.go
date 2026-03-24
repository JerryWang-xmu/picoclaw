package memory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// BenchmarkAddMessageAlways benchmarks the Always sync mode (original behavior)
func BenchmarkAddMessageAlways(b *testing.B) {
	dir := b.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModeAlways))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := store.AddMessage(ctx, "bench-session", "user", "test message content")
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// BenchmarkAddMessagePeriodic benchmarks the Periodic sync mode (batch behavior)
func BenchmarkAddMessagePeriodic(b *testing.B) {
	dir := b.TempDir()
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(100, 100*time.Millisecond))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := store.AddMessage(ctx, "bench-session", "user", "test message content")
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// BenchmarkAddMessageOnClose benchmarks the OnClose sync mode
func BenchmarkAddMessageOnClose(b *testing.B) {
	dir := b.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModeOnClose))
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := store.AddMessage(ctx, "bench-session", "user", "test message content")
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	store.Close()
}

// BenchmarkAddMessageParallel benchmarks concurrent writes with batch mode
func BenchmarkAddMessageParallel(b *testing.B) {
	dir := b.TempDir()
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(100, 100*time.Millisecond))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sessionKey := fmt.Sprintf("session-%d", i%10)
			err := store.AddMessage(ctx, sessionKey, "user", "test message content")
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
	b.StopTimer()
}

// BenchmarkAddMessageParallelAlways benchmarks concurrent writes with always sync
func BenchmarkAddMessageParallelAlways(b *testing.B) {
	dir := b.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModeAlways))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			sessionKey := fmt.Sprintf("session-%d", i%10)
			err := store.AddMessage(ctx, sessionKey, "user", "test message content")
			if err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
	b.StopTimer()
}

// BenchmarkAddFullMessagePeriodic benchmarks batch writing with full message struct
func BenchmarkAddFullMessagePeriodic(b *testing.B) {
	dir := b.TempDir()
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(100, 100*time.Millisecond))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	msg := providers.Message{
		Role:    "assistant",
		Content: "test content here",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := store.AddFullMessage(ctx, "bench-session", msg)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// BenchmarkAddFullMessageAlways benchmarks always sync with full message struct
func BenchmarkAddFullMessageAlways(b *testing.B) {
	dir := b.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModeAlways))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	msg := providers.Message{
		Role:    "assistant",
		Content: "test content here",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := store.AddFullMessage(ctx, "bench-session", msg)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// BenchmarkMixedSessions benchmarks performance with multiple concurrent sessions
func BenchmarkMixedSessions(b *testing.B) {
	dir := b.TempDir()
	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(50, 50*time.Millisecond))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sessionKey := fmt.Sprintf("session-%d", i%20)
		err := store.AddMessage(ctx, sessionKey, "user", "test message")
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// BenchmarkMixedSessionsAlways benchmarks multiple sessions with always sync
func BenchmarkMixedSessionsAlways(b *testing.B) {
	dir := b.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModeAlways))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sessionKey := fmt.Sprintf("session-%d", i%20)
		err := store.AddMessage(ctx, sessionKey, "user", "test message")
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}
