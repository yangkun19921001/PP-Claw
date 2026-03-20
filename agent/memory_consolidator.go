package agent

import (
	"context"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/yangkun19921001/PP-Claw/session"
	"go.uber.org/zap"
)

const maxConsolidationRounds = 5

// MemoryConsolidator performs token-based memory consolidation.
// It estimates the token usage of a session and triggers multi-round
// consolidation until the session fits within half the context window.
type MemoryConsolidator struct {
	store               *MemoryStore
	chatModel           einomodel.ToolCallingChatModel
	contextWindowTokens int
	sessions            *session.Manager
	buildSystemPrompt   func() string
	getToolDefs         func() []map[string]any
	locks               sync.Map // session_key -> *sync.Mutex
	logger              *zap.Logger
}

// NewMemoryConsolidator creates a new MemoryConsolidator.
func NewMemoryConsolidator(
	store *MemoryStore,
	chatModel einomodel.ToolCallingChatModel,
	contextWindowTokens int,
	sessions *session.Manager,
	buildSystemPrompt func() string,
	getToolDefs func() []map[string]any,
	logger *zap.Logger,
) *MemoryConsolidator {
	if contextWindowTokens <= 0 {
		contextWindowTokens = 65536
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &MemoryConsolidator{
		store:               store,
		chatModel:           chatModel,
		contextWindowTokens: contextWindowTokens,
		sessions:            sessions,
		buildSystemPrompt:   buildSystemPrompt,
		getToolDefs:         getToolDefs,
		logger:              logger,
	}
}

// MaybeConsolidateByTokens checks if the session exceeds the token budget
// and performs iterative consolidation (up to maxConsolidationRounds).
func (mc *MemoryConsolidator) MaybeConsolidateByTokens(ctx context.Context, sess *session.Session, memoryWindow int) {
	if sess == nil || len(sess.Messages) == 0 {
		return
	}

	// Per-session lock to avoid concurrent consolidation
	lockVal, _ := mc.locks.LoadOrStore(sess.Key, &sync.Mutex{})
	mu := lockVal.(*sync.Mutex)
	if !mu.TryLock() {
		return // another consolidation in progress
	}
	defer mu.Unlock()

	target := mc.contextWindowTokens / 2

	for round := 0; round < maxConsolidationRounds; round++ {
		tokens := mc.EstimateSessionPromptTokens(sess)
		if tokens <= target {
			mc.logger.Debug("Token-based consolidation: within budget",
				zap.String("session", sess.Key),
				zap.Int("tokens", tokens),
				zap.Int("target", target),
				zap.Int("round", round),
			)
			return
		}

		tokensToRemove := tokens - target
		idx, removed, ok := mc.PickConsolidationBoundary(sess, tokensToRemove)
		if !ok || removed == 0 {
			mc.logger.Warn("Token-based consolidation: cannot find boundary",
				zap.String("session", sess.Key),
				zap.Int("tokens_to_remove", tokensToRemove),
			)
			return
		}

		mc.logger.Info("Token-based consolidation round",
			zap.String("session", sess.Key),
			zap.Int("round", round+1),
			zap.Int("current_tokens", tokens),
			zap.Int("target", target),
			zap.Int("boundary_idx", idx),
			zap.Int("tokens_removed", removed),
		)

		// Consolidate messages up to boundary
		newOffset, consolidated := mc.store.Consolidate(ctx, sess.Messages, false, len(sess.Messages)-idx, sess.LastConsolidated)
		if consolidated {
			sess.LastConsolidated = newOffset
			mc.sessions.Save(sess)
		}
	}
}

// EstimateSessionPromptTokens estimates the total token count for a session prompt.
// Uses chars/4 heuristic.
func (mc *MemoryConsolidator) EstimateSessionPromptTokens(sess *session.Session) int {
	totalChars := 0

	// System prompt
	if mc.buildSystemPrompt != nil {
		totalChars += len(mc.buildSystemPrompt())
	}

	// History messages
	for _, msg := range sess.Messages {
		if content, ok := msg["content"].(string); ok {
			totalChars += len(content)
		}
		// Role overhead
		totalChars += 10
	}

	// Tool definitions (rough estimate)
	if mc.getToolDefs != nil {
		defs := mc.getToolDefs()
		for range defs {
			totalChars += 200 // ~50 tokens per tool def
		}
	}

	return totalChars / 4
}

// PickConsolidationBoundary finds a user turn boundary to remove approximately
// tokensToRemove tokens from the beginning of unconsolidated messages.
// Returns (boundary index from start, estimated tokens removed, ok).
func (mc *MemoryConsolidator) PickConsolidationBoundary(sess *session.Session, tokensToRemove int) (int, int, bool) {
	if sess == nil || tokensToRemove <= 0 {
		return 0, 0, false
	}

	start := sess.LastConsolidated
	if start >= len(sess.Messages) {
		return 0, 0, false
	}

	accumulatedTokens := 0
	lastUserBoundary := -1
	lastBoundaryTokens := 0

	for i := start; i < len(sess.Messages); i++ {
		msg := sess.Messages[i]
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		accumulatedTokens += (len(content) + 10) / 4

		// Track user message boundaries
		if role == "user" && i > start {
			lastUserBoundary = i
			lastBoundaryTokens = accumulatedTokens
		}

		if accumulatedTokens >= tokensToRemove {
			// Try to use the last user boundary for clean cut
			if lastUserBoundary > start {
				return lastUserBoundary, lastBoundaryTokens, true
			}
			return i + 1, accumulatedTokens, true
		}
	}

	// Didn't reach target, use whatever boundary we found
	if lastUserBoundary > start {
		return lastUserBoundary, lastBoundaryTokens, true
	}
	return 0, 0, false
}
