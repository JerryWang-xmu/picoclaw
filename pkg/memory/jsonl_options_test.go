package memory

import (
	"testing"
	"time"
)

func TestSyncModeDefaults(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir)
	if err != nil {
		t.Fatalf("NewJSONLStore() error = %v", err)
	}

	// Verify default is SyncModePeriodic for performance
	if store.syncMode != SyncModePeriodic {
		t.Errorf("default syncMode = %v, want SyncModePeriodic (%v)", store.syncMode, SyncModePeriodic)
	}
}

func TestWithSyncModeAlways(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONLStore(dir, WithSyncMode(SyncModeAlways))
	if err != nil {
		t.Fatalf("NewJSONLStore() error = %v", err)
	}

	if store.syncMode != SyncModeAlways {
		t.Errorf("syncMode = %v, want SyncModeAlways (%v)", store.syncMode, SyncModeAlways)
	}
}

func TestWithBatchThresholds(t *testing.T) {
	dir := t.TempDir()
	customCount := 50
	customInterval := 200 * time.Millisecond

	store, err := NewJSONLStore(dir,
		WithSyncMode(SyncModePeriodic),
		WithBatchThresholds(customCount, customInterval),
	)
	if err != nil {
		t.Fatalf("NewJSONLStore() error = %v", err)
	}

	if store.batchCountThreshold != customCount {
		t.Errorf("batchCountThreshold = %d, want %d", store.batchCountThreshold, customCount)
	}

	if store.batchTimeThreshold != customInterval {
		t.Errorf("batchTimeThreshold = %v, want %v", store.batchTimeThreshold, customInterval)
	}
}
