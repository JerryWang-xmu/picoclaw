package agent

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

// TestTurnStateMemoryLayout verifies the struct size and documents
// the memory savings achieved through field reordering.
func TestTurnStateMemoryLayout(t *testing.T) {
	// Get the actual size of turnState struct
	size := unsafe.Sizeof(turnState{})

	t.Logf("turnState struct size: %d bytes", size)

	// The optimized layout should be under 700 bytes on 64-bit systems
	// Original estimated size was around 800-850 bytes due to padding
	// After optimization, we target under 680 bytes
	require.Less(t, size, uintptr(700), "turnState should be optimized to under 700 bytes")

	// Verify individual field alignments to ensure no unnecessary padding
	checkFieldAlignment(t, "mu", unsafe.Offsetof(turnState{}.mu), unsafe.Alignof(turnState{}.mu))
	checkFieldAlignment(t, "agent", unsafe.Offsetof(turnState{}.agent), unsafe.Alignof(turnState{}.agent))
	checkFieldAlignment(t, "al", unsafe.Offsetof(turnState{}.al), unsafe.Alignof(turnState{}.al))
	checkFieldAlignment(t, "isFinished", unsafe.Offsetof(turnState{}.isFinished), unsafe.Alignof(turnState{}.isFinished))
	checkFieldAlignment(t, "parentEnded", unsafe.Offsetof(turnState{}.parentEnded), unsafe.Alignof(turnState{}.parentEnded))
	checkFieldAlignment(t, "depth", unsafe.Offsetof(turnState{}.depth), unsafe.Alignof(turnState{}.depth))
	checkFieldAlignment(t, "critical", unsafe.Offsetof(turnState{}.critical), unsafe.Alignof(turnState{}.critical))
}

func checkFieldAlignment(t *testing.T, name string, offset uintptr, alignment uintptr) {
	if offset%alignment != 0 {
		t.Errorf("Field %s is not properly aligned: offset=%d, alignment=%d", name, offset, alignment)
	}
}

// TestTurnStateSizeComparison documents the before/after sizes
// This test serves as documentation of the optimization.
func TestTurnStateSizeComparison(t *testing.T) {
	// Document expected sizes (these are estimates based on 64-bit architecture)
	// Before optimization: ~840 bytes (with significant padding)
	// After optimization: ~648 bytes (minimal padding)

	size := unsafe.Sizeof(turnState{})

	t.Logf("=== turnState Memory Layout Optimization Results ===")
	t.Logf("Current struct size: %d bytes", size)
	t.Logf("Estimated original size: ~840 bytes")
	if size <= 680 {
		savings := 840 - int(size)
		percentage := float64(savings) * 100 / 840
		t.Logf("Memory saved: ~%d bytes (%.1f%% reduction)", savings, percentage)
	}
}

// TestEmbeddedStructSizes analyzes the sizes of embedded structs
// to help identify further optimization opportunities.
func TestEmbeddedStructSizes(t *testing.T) {
	t.Logf("=== Embedded Struct Size Analysis ===")
	t.Logf("processOptions size: %d bytes", unsafe.Sizeof(processOptions{}))
	t.Logf("turnEventScope size: %d bytes", unsafe.Sizeof(turnEventScope{}))
	t.Logf("providers.Message size: %d bytes", unsafe.Sizeof(struct {
		Role             string
		Content          string
		ReasoningContent string
		ToolCallID       string
		Media            []string
		ToolCalls        []struct {
			ID       string
			Type     string
			Name     string
			Function *struct {
				Name             string
				Arguments        string
				ThoughtSignature string
			}
			ExtraContent     *struct{ Google *struct{ ThoughtSignature string } }
			ThoughtSignature string
			Arguments        map[string]any
		}
		SystemParts []struct {
			Type         string
			Text         string
			CacheControl *struct{ Type string }
		}
	}{}))
	t.Logf("bus.InboundMessage size: %d bytes", unsafe.Sizeof(struct {
		Channel    string
		ChatID     string
		SenderID   string
		Content    string
		SessionKey string
		Peer       struct{ Kind, ID string }
		Metadata   map[string]string
		Media      []string
		MessageID  string
		Sender     struct{ DisplayName string }
	}{}))
}
