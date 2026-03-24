package agent

import (
	"runtime"
	"testing"
)

// BenchmarkTurnStateMemory benchmarks memory usage for 1000 turnState instances
func BenchmarkTurnStateMemory(b *testing.B) {
	// Pre-allocate slice to hold pointers and prevent GC during measurement
	const numInstances = 1000

	b.Run("Allocate1000", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Force GC before measurement
			runtime.GC()

			var m1 runtime.MemStats
			runtime.ReadMemStats(&m1)

			// Allocate 1000 turnState instances
			instances := make([]*turnState, numInstances)
			for j := 0; j < numInstances; j++ {
				instances[j] = &turnState{}
			}

			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)

			// Calculate memory used
			allocBytes := m2.TotalAlloc - m1.TotalAlloc
			perInstance := allocBytes / numInstances

			b.ReportMetric(float64(perInstance), "bytes/instance")

			// Prevent compiler from optimizing away
			_ = instances
		}
	})

	b.Run("AllocateAndUse", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			runtime.GC()

			var m1 runtime.MemStats
			runtime.ReadMemStats(&m1)

			instances := make([]*turnState, numInstances)
			for j := 0; j < numInstances; j++ {
				ts := &turnState{}
				// Simulate typical field usage
				ts.turnID = "test-turn-id"
				ts.agentID = "test-agent"
				ts.sessionKey = "test-session"
				ts.depth = 1
				ts.iteration = 2
				instances[j] = ts
			}

			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)

			allocBytes := m2.TotalAlloc - m1.TotalAlloc
			perInstance := allocBytes / numInstances

			b.ReportMetric(float64(perInstance), "bytes/instance")

			_ = instances
		}
	})
}

// BenchmarkTurnStateFieldAccess benchmarks field access patterns
func BenchmarkTurnStateFieldAccess(b *testing.B) {
	ts := &turnState{
		turnID:     "test",
		agentID:    "agent",
		sessionKey: "session",
		depth:      1,
		iteration:  5,
	}

	b.Run("ReadStringFields", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ts.turnID
			_ = ts.agentID
			_ = ts.sessionKey
		}
	})

	b.Run("ReadIntFields", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ts.depth
			_ = ts.iteration
		}
	})

	b.Run("ReadBoolFields", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ts.gracefulInterrupt
			_ = ts.gracefulTerminalUsed
			_ = ts.hardAbort
			_ = ts.critical
		}
	})

	b.Run("MixedAccess", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = ts.turnID
			_ = ts.depth
			_ = ts.gracefulInterrupt
			_ = ts.agentID
			_ = ts.iteration
		}
	})
}
