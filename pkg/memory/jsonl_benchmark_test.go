package memory

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// msgForBenchmark is a representative message for benchmarking
var msgForBenchmark = providers.Message{
	Role:    "assistant",
	Content: "This is a test message with some content that represents a typical LLM response message.",
	ToolCalls: []providers.ToolCall{
		{
			ID:   "call_123",
			Type: "function",
			Function: &providers.FunctionCall{
				Name:      "test_function",
				Arguments: `{"arg1": "value1", "arg2": 42}`,
			},
		},
	},
}

// BenchmarkMarshalJSON benchmarks the standard json.Marshal approach
// This is the baseline showing allocations per operation
func BenchmarkMarshalJSON(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		line, err := json.Marshal(msgForBenchmark)
		if err != nil {
			b.Fatal(err)
		}
		line = append(line, '\n')
		_ = line
	}
}

// BenchmarkEncoderPooled benchmarks using json.Encoder with pooled buffer
// This demonstrates the allocation reduction from buffer pooling
func BenchmarkEncoderPooled(b *testing.B) {
	pool := NewBufferPool()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := pool.Get()

		enc := json.NewEncoder(buf)
		if err := enc.Encode(msgForBenchmark); err != nil {
			b.Fatal(err)
		}

		// Get the bytes (includes newline from Encode)
		_ = buf.Bytes()

		pool.Put(buf)
	}
}

// BenchmarkEncoderPooledParallel benchmarks pooled encoder under concurrent load
func BenchmarkEncoderPooledParallel(b *testing.B) {
	pool := NewBufferPool()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()

			enc := json.NewEncoder(buf)
			if err := enc.Encode(msgForBenchmark); err != nil {
				b.Fatal(err)
			}

			_ = buf.Bytes()
			pool.Put(buf)
		}
	})
}

// BenchmarkMarshalParallel benchmarks json.Marshal under concurrent load
func BenchmarkMarshalParallel(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			line, err := json.Marshal(msgForBenchmark)
			if err != nil {
				b.Fatal(err)
			}
			line = append(line, '\n')
			_ = line
		}
	})
}

// BenchmarkSmallMessage compares approaches for small messages
func BenchmarkSmallMessage_Marshal(b *testing.B) {
	msg := providers.Message{
		Role:    "user",
		Content: "Hello",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		line, _ := json.Marshal(msg)
		line = append(line, '\n')
		_ = line
	}
}

func BenchmarkSmallMessage_Pooled(b *testing.B) {
	pool := NewBufferPool()
	msg := providers.Message{
		Role:    "user",
		Content: "Hello",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		enc := json.NewEncoder(buf)
		enc.Encode(msg)
		_ = buf.Bytes()
		pool.Put(buf)
	}
}

// BenchmarkLargeMessage compares approaches for large messages (tool results)
func BenchmarkLargeMessage_Marshal(b *testing.B) {
	// Simulate a large tool result
	largeContent := make([]byte, 10000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	msg := providers.Message{
		Role:    "tool",
		Content: string(largeContent),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		line, _ := json.Marshal(msg)
		line = append(line, '\n')
		_ = line
	}
}

func BenchmarkLargeMessage_Pooled(b *testing.B) {
	pool := NewBufferPool()
	largeContent := make([]byte, 10000)
	for i := range largeContent {
		largeContent[i] = 'x'
	}
	msg := providers.Message{
		Role:    "tool",
		Content: string(largeContent),
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		enc := json.NewEncoder(buf)
		enc.Encode(msg)
		_ = buf.Bytes()
		pool.Put(buf)
	}
}

// BenchmarkBufferPoolGetPut benchmarks just the pool operations
func BenchmarkBufferPoolGetPut(b *testing.B) {
	pool := NewBufferPool()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		buf.WriteString("test data")
		pool.Put(buf)
	}
}

// BenchmarkBufferPoolGetPutParallel benchmarks pool operations under concurrency
func BenchmarkBufferPoolGetPutParallel(b *testing.B) {
	pool := NewBufferPool()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			buf.WriteString("test data")
			pool.Put(buf)
		}
	})
}

// BenchmarkEncodeToBuffer benchmarks encoding directly to a pre-allocated buffer
// This simulates the optimal case where we already have a buffer
func BenchmarkEncodeToBuffer(b *testing.B) {
	buf := &bytes.Buffer{}
	buf.Grow(4096)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		enc := json.NewEncoder(buf)
		enc.Encode(msgForBenchmark)
		_ = buf.Bytes()
	}
}
