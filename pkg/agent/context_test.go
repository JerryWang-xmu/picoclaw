package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// TestSkillFileCache_WithinInterval_ReturnsCached tests that checks within minInterval return cached result.
func TestSkillFileCache_WithinInterval_ReturnsCached(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillRoot, 0755); err != nil {
		t.Fatalf("failed to create skill root: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(skillRoot, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Build initial cache with actual file mtime
	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("failed to stat test file: %v", err)
	}
	filesAtCache := map[string]time.Time{
		testFile: info.ModTime(),
	}

	cache := NewSkillFileCache(1 * time.Second)

	// First check - should perform actual check
	changed1 := cache.Check([]string{skillRoot}, filesAtCache)
	if changed1 {
		t.Error("first check should return false (no changes)")
	}

	// Immediately check again - should return cached result (no change)
	// Modify the file but within interval - should not be detected
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	changed2 := cache.Check([]string{skillRoot}, filesAtCache)
	if changed2 {
		t.Error("check within interval should return cached result (false)")
	}
}

// TestSkillFileCache_AfterInterval_Rechecks tests that checks after minInterval perform actual check.
func TestSkillFileCache_AfterInterval_Rechecks(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillRoot, 0755); err != nil {
		t.Fatalf("failed to create skill root: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(skillRoot, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Build initial cache with current mtime
	info, _ := os.Stat(testFile)
	filesAtCache := map[string]time.Time{
		testFile: info.ModTime(),
	}

	// Use a very short interval for testing
	cache := NewSkillFileCache(10 * time.Millisecond)

	// First check - should perform actual check
	changed1 := cache.Check([]string{skillRoot}, filesAtCache)
	if changed1 {
		t.Error("first check should return false (no changes)")
	}

	// Wait for interval to pass
	time.Sleep(20 * time.Millisecond)

	// Check again after interval - should perform actual check
	changed2 := cache.Check([]string{skillRoot}, filesAtCache)
	if changed2 {
		t.Error("check after interval with no changes should return false")
	}
}

// TestSkillFileCache_FileChanged_Detected tests that file changes are detected after interval.
func TestSkillFileCache_FileChanged_Detected(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillRoot, 0755); err != nil {
		t.Fatalf("failed to create skill root: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(skillRoot, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Build initial cache with current mtime
	info, _ := os.Stat(testFile)
	filesAtCache := map[string]time.Time{
		testFile: info.ModTime(),
	}

	// Use a very short interval for testing
	cache := NewSkillFileCache(10 * time.Millisecond)

	// First check - should perform actual check
	changed1 := cache.Check([]string{skillRoot}, filesAtCache)
	if changed1 {
		t.Error("first check should return false (no changes)")
	}

	// Wait for interval to pass
	time.Sleep(20 * time.Millisecond)

	// Modify the file
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	// Check after interval - should detect change
	changed2 := cache.Check([]string{skillRoot}, filesAtCache)
	if !changed2 {
		t.Error("check after interval should detect file modification")
	}
}

// TestSkillFileCache_NewFile_Detected tests that new files are detected.
func TestSkillFileCache_NewFile_Detected(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillRoot, 0755); err != nil {
		t.Fatalf("failed to create skill root: %v", err)
	}

	// Create initial test file
	testFile1 := filepath.Join(skillRoot, "test1.txt")
	if err := os.WriteFile(testFile1, []byte("test1"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Build initial cache
	info, _ := os.Stat(testFile1)
	filesAtCache := map[string]time.Time{
		testFile1: info.ModTime(),
	}

	// Use a very short interval for testing
	cache := NewSkillFileCache(10 * time.Millisecond)

	// First check
	changed1 := cache.Check([]string{skillRoot}, filesAtCache)
	if changed1 {
		t.Error("first check should return false")
	}

	// Wait for interval to pass
	time.Sleep(20 * time.Millisecond)

	// Create a new file
	testFile2 := filepath.Join(skillRoot, "test2.txt")
	if err := os.WriteFile(testFile2, []byte("test2"), 0644); err != nil {
		t.Fatalf("failed to create second test file: %v", err)
	}

	// Check after interval - should detect new file
	changed2 := cache.Check([]string{skillRoot}, filesAtCache)
	if !changed2 {
		t.Error("check after interval should detect new file")
	}
}

// TestSkillFileCache_FileDeleted_Detected tests that file deletions are detected.
func TestSkillFileCache_FileDeleted_Detected(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillRoot, 0755); err != nil {
		t.Fatalf("failed to create skill root: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(skillRoot, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Build initial cache
	info, _ := os.Stat(testFile)
	filesAtCache := map[string]time.Time{
		testFile: info.ModTime(),
	}

	// Use a very short interval for testing
	cache := NewSkillFileCache(10 * time.Millisecond)

	// First check
	changed1 := cache.Check([]string{skillRoot}, filesAtCache)
	if changed1 {
		t.Error("first check should return false")
	}

	// Wait for interval to pass
	time.Sleep(20 * time.Millisecond)

	// Delete the file
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("failed to remove test file: %v", err)
	}

	// Check after interval - should detect deletion
	changed2 := cache.Check([]string{skillRoot}, filesAtCache)
	if !changed2 {
		t.Error("check after interval should detect file deletion")
	}
}

// TestSkillFileCache_ForceCheck tests that ForceCheck bypasses interval.
func TestSkillFileCache_ForceCheck(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillRoot, 0755); err != nil {
		t.Fatalf("failed to create skill root: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(skillRoot, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Build initial cache with actual file mtime
	info, _ := os.Stat(testFile)
	filesAtCache := map[string]time.Time{
		testFile: info.ModTime(),
	}

	// Use a long interval
	cache := NewSkillFileCache(1 * time.Hour)

	// First check
	changed1 := cache.Check([]string{skillRoot}, filesAtCache)
	if changed1 {
		t.Error("first check should return false")
	}

	// Modify the file
	if err := os.WriteFile(testFile, []byte("modified"), 0644); err != nil {
		t.Fatalf("failed to modify test file: %v", err)
	}

	// Normal check within interval - should return cached result (false)
	changed2 := cache.Check([]string{skillRoot}, filesAtCache)
	if changed2 {
		t.Error("check within interval should return cached result")
	}

	// Force check with updated mtime - should bypass interval and detect no change from new baseline
	info, _ = os.Stat(testFile)
	filesAtCache[testFile] = info.ModTime()
	changed3 := cache.ForceCheck([]string{skillRoot}, filesAtCache)
	if changed3 {
		t.Error("force check with updated mtime should return false (no change from new baseline)")
	}

	// Delete the file and force check - should detect deletion
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("failed to remove test file: %v", err)
	}
	changed4 := cache.ForceCheck([]string{skillRoot}, filesAtCache)
	if !changed4 {
		t.Error("force check should detect file deletion")
	}
}

// TestSkillFileCache_NilCache_ReturnsTrue tests that nil cache returns true.
func TestSkillFileCache_NilCache_ReturnsTrue(t *testing.T) {
	tmpDir := t.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills")

	cache := NewSkillFileCache(1 * time.Second)

	// Check with nil filesAtCache should return true
	changed := cache.Check([]string{skillRoot}, nil)
	if !changed {
		t.Error("check with nil filesAtCache should return true")
	}
}

// BenchmarkSkillFileCache benchmarks the cache performance.
func BenchmarkSkillFileCache(b *testing.B) {
	tmpDir := b.TempDir()
	skillRoot := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillRoot, 0755); err != nil {
		b.Fatalf("failed to create skill root: %v", err)
	}

	// Create multiple test files
	filesAtCache := make(map[string]time.Time)
	for i := 0; i < 10; i++ {
		testFile := filepath.Join(skillRoot, fmt.Sprintf("test%d.txt", i))
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			b.Fatalf("failed to create test file: %v", err)
		}
		info, _ := os.Stat(testFile)
		filesAtCache[testFile] = info.ModTime()
	}

	cache := NewSkillFileCache(1 * time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.Check([]string{skillRoot}, filesAtCache)
	}
}

// BenchmarkSanitizeHistory_1000Msgs benchmarks the performance of
// sanitizeHistoryForProvider with 1000 messages to verify O(n) optimization.
func BenchmarkSanitizeHistory_1000Msgs(b *testing.B) {
	// Generate a realistic conversation history with 1000 messages
	// Pattern: user -> assistant with tools -> tool results -> assistant
	history := make([]providers.Message, 0, 1000)
	for i := 0; i < 200; i++ {
		toolID1 := fmt.Sprintf("tool_%d_a", i)
		toolID2 := fmt.Sprintf("tool_%d_b", i)
		history = append(history, msg("user", fmt.Sprintf("request %d", i)))
		history = append(history, assistantWithTools(toolID1, toolID2))
		history = append(history, toolResult(toolID1))
		history = append(history, toolResult(toolID2))
		history = append(history, msg("assistant", fmt.Sprintf("response %d", i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sanitizeHistoryForProvider(history)
	}
}

func msg(role, content string) providers.Message {
	return providers.Message{Role: role, Content: content}
}

func assistantWithTools(toolIDs ...string) providers.Message {
	calls := make([]providers.ToolCall, len(toolIDs))
	for i, id := range toolIDs {
		calls[i] = providers.ToolCall{ID: id, Type: "function"}
	}
	return providers.Message{Role: "assistant", ToolCalls: calls}
}

func toolResult(id string) providers.Message {
	return providers.Message{Role: "tool", Content: "result", ToolCallID: id}
}

func TestSanitizeHistoryForProvider_EmptyHistory(t *testing.T) {
	result := sanitizeHistoryForProvider(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d messages", len(result))
	}

	result = sanitizeHistoryForProvider([]providers.Message{})
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d messages", len(result))
	}
}

func TestSanitizeHistoryForProvider_SingleToolCall(t *testing.T) {
	history := []providers.Message{
		msg("user", "hello"),
		assistantWithTools("A"),
		toolResult("A"),
		msg("assistant", "done"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	assertRoles(t, result, "user", "assistant", "tool", "assistant")
}

func TestSanitizeHistoryForProvider_MultiToolCalls(t *testing.T) {
	history := []providers.Message{
		msg("user", "do two things"),
		assistantWithTools("A", "B"),
		toolResult("A"),
		toolResult("B"),
		msg("assistant", "both done"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user", "assistant", "tool", "tool", "assistant")
}

func TestSanitizeHistoryForProvider_AssistantToolCallAfterPlainAssistant(t *testing.T) {
	history := []providers.Message{
		msg("user", "hi"),
		msg("assistant", "thinking"),
		assistantWithTools("A"),
		toolResult("A"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user", "assistant")
}

func TestSanitizeHistoryForProvider_OrphanedLeadingTool(t *testing.T) {
	history := []providers.Message{
		toolResult("A"),
		msg("user", "hello"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user")
}

func TestSanitizeHistoryForProvider_ToolAfterUserDropped(t *testing.T) {
	history := []providers.Message{
		msg("user", "hello"),
		toolResult("A"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user")
}

func TestSanitizeHistoryForProvider_ToolAfterAssistantNoToolCalls(t *testing.T) {
	history := []providers.Message{
		msg("user", "hello"),
		msg("assistant", "hi"),
		toolResult("A"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user", "assistant")
}

func TestSanitizeHistoryForProvider_AssistantToolCallAtStart(t *testing.T) {
	history := []providers.Message{
		assistantWithTools("A"),
		toolResult("A"),
		msg("user", "hello"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user")
}

func TestSanitizeHistoryForProvider_MultiToolCallsThenNewRound(t *testing.T) {
	history := []providers.Message{
		msg("user", "do two things"),
		assistantWithTools("A", "B"),
		toolResult("A"),
		toolResult("B"),
		msg("assistant", "done"),
		msg("user", "hi"),
		assistantWithTools("C"),
		toolResult("C"),
		msg("assistant", "done again"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 9 {
		t.Fatalf("expected 9 messages, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user", "assistant", "tool", "tool", "assistant", "user", "assistant", "tool", "assistant")
}

func TestSanitizeHistoryForProvider_ConsecutiveMultiToolRounds(t *testing.T) {
	history := []providers.Message{
		msg("user", "start"),
		assistantWithTools("A", "B"),
		toolResult("A"),
		toolResult("B"),
		assistantWithTools("C", "D"),
		toolResult("C"),
		toolResult("D"),
		msg("assistant", "all done"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 8 {
		t.Fatalf("expected 8 messages, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user", "assistant", "tool", "tool", "assistant", "tool", "tool", "assistant")
}

func TestSanitizeHistoryForProvider_PlainConversation(t *testing.T) {
	history := []providers.Message{
		msg("user", "hello"),
		msg("assistant", "hi"),
		msg("user", "how are you"),
		msg("assistant", "fine"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	assertRoles(t, result, "user", "assistant", "user", "assistant")
}

func TestSanitizeHistoryForProvider_DuplicateToolResults(t *testing.T) {
	history := []providers.Message{
		msg("user", "do something"),
		assistantWithTools("A", "B"),
		toolResult("A"),
		toolResult("B"),
		toolResult("A"), // duplicate
		toolResult("B"), // duplicate
		msg("assistant", "done"),
	}

	result := sanitizeHistoryForProvider(history)
	if len(result) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user", "assistant", "tool", "tool", "assistant")
	// Verify the kept tool results have the correct IDs
	if result[2].ToolCallID != "A" {
		t.Errorf("expected tool result A, got %q", result[2].ToolCallID)
	}
	if result[3].ToolCallID != "B" {
		t.Errorf("expected tool result B, got %q", result[3].ToolCallID)
	}
}

func roles(msgs []providers.Message) []string {
	r := make([]string, len(msgs))
	for i, m := range msgs {
		r[i] = m.Role
	}
	return r
}

func assertRoles(t *testing.T, msgs []providers.Message, expected ...string) {
	t.Helper()
	if len(msgs) != len(expected) {
		t.Fatalf("role count mismatch: got %v, want %v", roles(msgs), expected)
	}
	for i, exp := range expected {
		if msgs[i].Role != exp {
			t.Errorf("message[%d]: got role %q, want %q", i, msgs[i].Role, exp)
		}
	}
}

// TestSanitizeHistoryForProvider_IncompleteToolResults tests the forward validation
// that ensures assistant messages with tool_calls have ALL matching tool results.
// This fixes the DeepSeek error: "An assistant message with 'tool_calls' must be
// followed by tool messages responding to each 'tool_call_id'."
func TestSanitizeHistoryForProvider_IncompleteToolResults(t *testing.T) {
	// Assistant expects tool results for both A and B, but only A is present
	history := []providers.Message{
		msg("user", "do two things"),
		assistantWithTools("A", "B"),
		toolResult("A"),
		// toolResult("B") is missing - this would cause DeepSeek to fail
		msg("user", "next question"),
		msg("assistant", "answer"),
	}

	result := sanitizeHistoryForProvider(history)
	// The assistant message with incomplete tool results should be dropped,
	// along with its partial tool result. The remaining messages are:
	// user ("do two things"), user ("next question"), assistant ("answer")
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user", "user", "assistant")
}

// TestSanitizeHistoryForProvider_MissingAllToolResults tests the case where
// an assistant message has tool_calls but no tool results follow at all.
func TestSanitizeHistoryForProvider_MissingAllToolResults(t *testing.T) {
	history := []providers.Message{
		msg("user", "do something"),
		assistantWithTools("A"),
		// No tool results at all
		msg("user", "hello"),
		msg("assistant", "hi"),
	}

	result := sanitizeHistoryForProvider(history)
	// The assistant message with no tool results should be dropped.
	// Remaining: user ("do something"), user ("hello"), assistant ("hi")
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user", "user", "assistant")
}

// TestSanitizeHistoryForProvider_PartialToolResultsInMiddle tests that
// incomplete tool results in the middle of a conversation are properly handled.
func TestSanitizeHistoryForProvider_PartialToolResultsInMiddle(t *testing.T) {
	history := []providers.Message{
		msg("user", "first"),
		assistantWithTools("A"),
		toolResult("A"),
		msg("assistant", "done"),
		msg("user", "second"),
		assistantWithTools("B", "C"),
		toolResult("B"),
		// toolResult("C") is missing
		msg("user", "third"),
		assistantWithTools("D"),
		toolResult("D"),
		msg("assistant", "all done"),
	}

	result := sanitizeHistoryForProvider(history)
	// First round is complete (user, assistant+tools, tool, assistant),
	// second round is incomplete and dropped (assistant+tools, partial tool),
	// third round is complete (user, assistant+tools, tool, assistant).
	// Remaining: user, assistant, tool, assistant, user, user, assistant, tool, assistant
	if len(result) != 9 {
		t.Fatalf("expected 9 messages, got %d: %+v", len(result), roles(result))
	}
	assertRoles(t, result, "user", "assistant", "tool", "assistant", "user", "user", "assistant", "tool", "assistant")
}
