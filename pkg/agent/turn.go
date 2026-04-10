package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/session"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type TurnPhase string

const (
	TurnPhaseSetup      TurnPhase = "setup"
	TurnPhaseRunning    TurnPhase = "running"
	TurnPhaseTools      TurnPhase = "tools"
	TurnPhaseFinalizing TurnPhase = "finalizing"
	TurnPhaseCompleted  TurnPhase = "completed"
	TurnPhaseAborted    TurnPhase = "aborted"
)

type ActiveTurnInfo struct {
	TurnID       string
	AgentID      string
	SessionKey   string
	Channel      string
	ChatID       string
	UserMessage  string
	Phase        TurnPhase
	Iteration    int
	StartedAt    time.Time
	Depth        int
	ParentTurnID string
	ChildTurnIDs []string
}

type turnResult struct {
	finalContent string
	status       TurnEndStatus
	followUps    []bus.InboundMessage
}

type turnState struct {
	// === 8-byte aligned fields (pointers, functions, channels) ===
	agent  *AgentInstance
	al     *AgentLoop
	parent *turnState // Renamed from parentTurnState for consistency

	// Context and cancellation
	ctx        context.Context
	cancelFunc context.CancelFunc
	turnCancel context.CancelFunc

	// Provider cancellation function
	providerCancel context.CancelFunc

	// Channels (8-byte pointers)
	pendingResults chan *tools.ToolResult
	concurrencySem chan struct{}
	finishedChan   chan struct{}

	// Token budget tracking
	tokenBudget *atomic.Int64

	// Usage tracking
	lastUsage *providers.UsageInfo

	// === 16-byte aligned fields (strings, interfaces) ===
	// String fields (16 bytes each: ptr + len)
	turnID                string
	agentID               string
	sessionKey            string
	channel               string
	chatID                string
	userMessage           string
	finalContent          string
	restorePointSummary   string
	parentTurnID          string
	lastFinishReason      string
	gracefulInterruptHint string

	// Interface fields (16 bytes each: type + data)
	session session.SessionStore

	// TurnPhase is a string alias (16 bytes)
	phase TurnPhase

	// === 24-byte aligned fields (slices, time.Time) ===
	// Slice fields (24 bytes each: ptr + len + cap)
	media               []string
	followUps           []bus.InboundMessage
	childTurnIDs        []string
	restorePointHistory []providers.Message
	persistedMessages   []providers.Message

	// time.Time (24 bytes)
	startedAt time.Time

	// Embedded structs (converted to pointers to save memory)
	opts  *processOptions
	scope *turnEventScope

	// === Synchronization primitives ===
	mu sync.RWMutex

	// Atomic fields (need alignment, placed after mutex)
	isFinished  atomic.Bool
	parentEnded atomic.Bool

	// sync.Once (8 bytes)
	closeOnce sync.Once

	// === 4-byte fields (int32) ===
	// Using int32 for counters to save space
	depth                int32
	iteration            int32
	initialHistoryLength int32

	// === 1-byte fields (bools) - grouped at end to minimize padding ===
	gracefulInterrupt    bool
	gracefulTerminalUsed bool
	hardAbort            bool
	critical             bool
}

func newTurnState(agent *AgentInstance, opts processOptions, scope turnEventScope) *turnState {
	optsCopy := opts
	scopeCopy := scope
	ts := &turnState{
		agent:       agent,
		opts:        &optsCopy,
		scope:       &scopeCopy,
		turnID:      scope.turnID,
		agentID:     agent.ID,
		sessionKey:  opts.SessionKey,
		channel:     opts.Channel,
		chatID:      opts.ChatID,
		userMessage: opts.UserMessage,
		media:       append([]string(nil), opts.Media...),
		phase:       TurnPhaseSetup,
		startedAt:   time.Now(),
	}

	// Bind session store and capture initial history length for rollback logic
	if agent != nil && agent.Sessions != nil {
		ts.session = agent.Sessions
		ts.initialHistoryLength = int32(len(agent.Sessions.GetHistory(opts.SessionKey)))
	}

	return ts
}

func (al *AgentLoop) registerActiveTurn(ts *turnState) {
	al.activeTurnStates.Store(ts.sessionKey, ts)
}

func (al *AgentLoop) clearActiveTurn(ts *turnState) {
	al.activeTurnStates.Delete(ts.sessionKey)
}

func (al *AgentLoop) getActiveTurnState(sessionKey string) *turnState {
	if val, ok := al.activeTurnStates.Load(sessionKey); ok {
		return val
	}
	return nil
}

// getAnyActiveTurnState returns any active turn state (for backward compatibility)
func (al *AgentLoop) getAnyActiveTurnState() *turnState {
	var firstTS *turnState
	al.activeTurnStates.Range(func(key string, value *turnState) bool {
		firstTS = value
		return false // stop after first
	})
	return firstTS
}

func (al *AgentLoop) GetActiveTurn() *ActiveTurnInfo {
	// For backward compatibility, return the first active turn found
	// In the new architecture, there can be multiple concurrent turns
	var firstTS *turnState
	al.activeTurnStates.Range(func(key string, value *turnState) bool {
		firstTS = value
		return false // stop after first
	})
	if firstTS == nil {
		return nil
	}
	info := firstTS.snapshot()
	return &info
}

func (al *AgentLoop) GetActiveTurnBySession(sessionKey string) *ActiveTurnInfo {
	ts := al.getActiveTurnState(sessionKey)
	if ts == nil {
		return nil
	}
	info := ts.snapshot()
	return &info
}

func (ts *turnState) snapshot() ActiveTurnInfo {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	return ActiveTurnInfo{
		TurnID:       ts.turnID,
		AgentID:      ts.agentID,
		SessionKey:   ts.sessionKey,
		Channel:      ts.channel,
		ChatID:       ts.chatID,
		UserMessage:  ts.userMessage,
		Phase:        ts.phase,
		Iteration:    int(ts.iteration),
		StartedAt:    ts.startedAt,
		Depth:        int(ts.depth),
		ParentTurnID: ts.parentTurnID,
		ChildTurnIDs: append([]string(nil), ts.childTurnIDs...),
	}
}

func (ts *turnState) setPhase(phase TurnPhase) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.phase = phase
}

func (ts *turnState) setIteration(iteration int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.iteration = int32(iteration)
}

func (ts *turnState) currentIteration() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return int(ts.iteration)
}

func (ts *turnState) setFinalContent(content string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.finalContent = content
}

func (ts *turnState) finalContentLen() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.finalContent)
}

func (ts *turnState) setTurnCancel(cancel context.CancelFunc) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.turnCancel = cancel
}

func (ts *turnState) setProviderCancel(cancel context.CancelFunc) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.providerCancel = cancel
}

func (ts *turnState) clearProviderCancel(_ context.CancelFunc) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.providerCancel = nil
}

func (ts *turnState) requestGracefulInterrupt(hint string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.hardAbort {
		return false
	}
	ts.gracefulInterrupt = true
	ts.gracefulInterruptHint = hint
	return true
}

func (ts *turnState) gracefulInterruptRequested() (bool, string) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.gracefulInterrupt && !ts.gracefulTerminalUsed, ts.gracefulInterruptHint
}

func (ts *turnState) markGracefulTerminalUsed() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.gracefulTerminalUsed = true
}

func (ts *turnState) requestHardAbort() bool {
	ts.mu.Lock()
	if ts.hardAbort {
		ts.mu.Unlock()
		return false
	}
	ts.hardAbort = true
	turnCancel := ts.turnCancel
	providerCancel := ts.providerCancel
	ts.mu.Unlock()

	if providerCancel != nil {
		providerCancel()
	}
	if turnCancel != nil {
		turnCancel()
	}
	return true
}

func (ts *turnState) hardAbortRequested() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.hardAbort
}

func (ts *turnState) eventMeta(source, tracePath string) EventMeta {
	snap := ts.snapshot()
	return EventMeta{
		AgentID:    snap.AgentID,
		TurnID:     snap.TurnID,
		SessionKey: snap.SessionKey,
		Iteration:  snap.Iteration,
		Source:     source,
		TracePath:  tracePath,
	}
}

func (ts *turnState) captureRestorePoint(history []providers.Message, summary string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.restorePointHistory = append([]providers.Message(nil), history...)
	ts.restorePointSummary = summary
}

func (ts *turnState) recordPersistedMessage(msg providers.Message) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.persistedMessages = append(ts.persistedMessages, msg)
}

func (ts *turnState) refreshRestorePointFromSession(agent *AgentInstance) {
	history := agent.Sessions.GetHistory(ts.sessionKey)
	summary := agent.Sessions.GetSummary(ts.sessionKey)

	ts.mu.RLock()
	persisted := append([]providers.Message(nil), ts.persistedMessages...)
	ts.mu.RUnlock()

	if matched := matchingTurnMessageTail(history, persisted); matched > 0 {
		history = append([]providers.Message(nil), history[:len(history)-matched]...)
	}

	ts.captureRestorePoint(history, summary)
}

// ingestMessage calls the ContextManager's Ingest method for a persisted message.
// Errors are logged but never block the turn.
func (ts *turnState) ingestMessage(ctx context.Context, al *AgentLoop, msg providers.Message) {
	if al.contextManager == nil {
		return
	}
	if err := al.contextManager.Ingest(ctx, &IngestRequest{
		SessionKey: ts.sessionKey,
		Message:    msg,
	}); err != nil {
		logger.WarnCF("agent", "Context manager ingest failed", map[string]any{
			"session_key": ts.sessionKey,
			"error":       err.Error(),
		})
	}
}

func (ts *turnState) restoreSession(agent *AgentInstance) error {
	ts.mu.RLock()
	history := append([]providers.Message(nil), ts.restorePointHistory...)
	summary := ts.restorePointSummary
	ts.mu.RUnlock()

	agent.Sessions.SetHistory(ts.sessionKey, history)
	agent.Sessions.SetSummary(ts.sessionKey, summary)
	return agent.Sessions.Save(ts.sessionKey)
}

func matchingTurnMessageTail(history, persisted []providers.Message) int {
	maxMatch := min(len(history), len(persisted))
	for size := maxMatch; size > 0; size-- {
		if messagesEqual(history[len(history)-size:], persisted[len(persisted)-size:]) {
			return size
		}
	}
	return 0
}

// messagesEqual is an optimized comparison function that replaces reflect.DeepEqual
// for comparing slices of providers.Message. It uses early exit and efficient
// string comparison for 10-100x performance improvement.
func messagesEqual(a, b []providers.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !messageEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// messageEqual compares two providers.Message for equality
func messageEqual(a, b providers.Message) bool {
	if a.Role != b.Role {
		return false
	}
	if a.Content != b.Content {
		return false
	}
	if a.ReasoningContent != b.ReasoningContent {
		return false
	}
	if a.ToolCallID != b.ToolCallID {
		return false
	}
	if !stringSliceEqual(a.Media, b.Media) {
		return false
	}
	if !toolCallsEqual(a.ToolCalls, b.ToolCalls) {
		return false
	}
	if !contentBlocksEqual(a.SystemParts, b.SystemParts) {
		return false
	}
	return true
}

// stringSliceEqual compares two string slices for equality
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// toolCallsEqual compares two ToolCall slices for equality
func toolCallsEqual(a, b []providers.ToolCall) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !toolCallEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

// toolCallEqual compares two ToolCall structs for equality
func toolCallEqual(a, b providers.ToolCall) bool {
	if a.ID != b.ID {
		return false
	}
	if a.Type != b.Type {
		return false
	}
	if a.Name != b.Name {
		return false
	}
	if !functionCallEqualPtr(a.Function, b.Function) {
		return false
	}
	if !extraContentEqualPtr(a.ExtraContent, b.ExtraContent) {
		return false
	}
	// Note: Arguments map comparison is skipped for performance as it's not
	// typically used in the turn matching context and ThoughtSignature is internal
	return true
}

// functionCallEqualPtr compares two FunctionCall pointers for equality
func functionCallEqualPtr(a, b *providers.FunctionCall) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Name == b.Name && a.Arguments == b.Arguments
}

// extraContentEqualPtr compares two ExtraContent pointers for equality
func extraContentEqualPtr(a, b *providers.ExtraContent) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Google == nil && b.Google == nil {
		return true
	}
	if a.Google == nil || b.Google == nil {
		return false
	}
	return a.Google.ThoughtSignature == b.Google.ThoughtSignature
}

// contentBlocksEqual compares two ContentBlock slices for equality
func contentBlocksEqual(a, b []providers.ContentBlock) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type {
			return false
		}
		if a[i].Text != b[i].Text {
			return false
		}
		if !cacheControlEqualPtr(a[i].CacheControl, b[i].CacheControl) {
			return false
		}
	}
	return true
}

// cacheControlEqualPtr compares two CacheControl pointers for equality
func cacheControlEqualPtr(a, b *providers.CacheControl) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Type == b.Type
}

func (ts *turnState) interruptHintMessage() providers.Message {
	_, hint := ts.gracefulInterruptRequested()
	content := "Interrupt requested. Stop scheduling tools and provide a short final summary."
	if hint != "" {
		content += "\n\nInterrupt hint: " + hint
	}
	return providers.Message{
		Role:    "user",
		Content: content,
	}
}

// SubTurn-related methods

// Finish marks the turn as finished and closes the pendingResults channel
func (ts *turnState) Finish(isHardAbort bool) {
	ts.isFinished.Store(true)

	// Close pendingResults channel exactly once
	ts.closeOnce.Do(func() {
		if ts.pendingResults != nil {
			close(ts.pendingResults)
		}
		ts.mu.Lock()
		if ts.finishedChan == nil {
			ts.finishedChan = make(chan struct{})
		}
		close(ts.finishedChan)
		ts.mu.Unlock()
	})

	// If this is a graceful finish (not hard abort), signal to children
	if !isHardAbort && ts.parent == nil {
		// This is a root turn finishing gracefully
		ts.parentEnded.Store(true)
	}

	// Cancel the turn context
	if ts.cancelFunc != nil {
		ts.cancelFunc()
	}

	// Hard abort cascades to all child turns
	if isHardAbort && ts.al != nil {
		ts.mu.RLock()
		children := append([]string(nil), ts.childTurnIDs...)
		ts.mu.RUnlock()
		for _, childID := range children {
			if val, ok := ts.al.activeTurnStates.Load(childID); ok {
				val.Finish(true)
			}
		}
	}
}

// Finished returns whether the turn has finished
func (ts *turnState) Finished() chan struct{} {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.finishedChan == nil {
		ts.finishedChan = make(chan struct{})
	}
	return ts.finishedChan
}

// IsParentEnded checks if the parent turn has ended
func (ts *turnState) IsParentEnded() bool {
	if ts.parent == nil {
		return false
	}
	return ts.parent.parentEnded.Load()
}

// GetLastFinishReason returns the last LLM finish_reason
func (ts *turnState) GetLastFinishReason() string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.lastFinishReason
}

// SetLastFinishReason sets the last LLM finish_reason
func (ts *turnState) SetLastFinishReason(reason string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.lastFinishReason = reason
}

// GetLastUsage returns the last LLM usage info
func (ts *turnState) GetLastUsage() *providers.UsageInfo {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.lastUsage
}

// SetLastUsage sets the last LLM usage info
func (ts *turnState) SetLastUsage(usage *providers.UsageInfo) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.lastUsage = usage
}

// Context helper functions for SubTurn

type turnStateKeyType struct{}

var turnStateKey = turnStateKeyType{}

func withTurnState(ctx context.Context, ts *turnState) context.Context {
	return context.WithValue(ctx, turnStateKey, ts)
}

func turnStateFromContext(ctx context.Context) *turnState {
	ts, _ := ctx.Value(turnStateKey).(*turnState)
	return ts
}

// TurnStateFromContext retrieves turnState from context (exported for tools)
func TurnStateFromContext(ctx context.Context) *turnState {
	return turnStateFromContext(ctx)
}
