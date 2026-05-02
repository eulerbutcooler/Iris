package bot

import (
	"context"
	"sync"
	"time"

	"github.com/eulerbutcooler/iris/services/iris-telegram/internal/ai"
	"github.com/eulerbutcooler/iris/services/iris-telegram/internal/iris"
)

// SessionState represents the current state of a user's Telegram conversation.
type SessionState int

const (
	StateIdle        SessionState = iota // waiting for a command
	StateAwaitLogin                      // waiting for the user to paste their JWT token
	StateDescribing                      // user is describing a relay in natural language
	StateConfirming                      // LLM produced a relay draft, waiting for confirm/edit/cancel
	StateEditing                         // user is refining an existing draft
	StateAwaitDelete                     // waiting for deletion confirmation of a specific relay
)

// Session holds all per-user state for one Telegram conversation.
type Session struct {
	State        SessionState
	Token        string                    // Iris JWT, set after /login
	DraftRelay   *iris.CreateRelayRequest  // relay being built
	DraftRelayID string                    // ID of the relay to delete (StateAwaitDelete)
	Conversation []ai.Message              // full LLM conversation history
	LastActivity time.Time
}

// SessionManager stores in-memory sessions keyed by Telegram user ID.
// Sessions are evicted after TTL of inactivity.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[int64]*Session
	ttl      time.Duration
}

// NewSessionManager creates a SessionManager with the given session TTL.
func NewSessionManager(ttl time.Duration) *SessionManager {
	return &SessionManager{
		sessions: make(map[int64]*Session),
		ttl:      ttl,
	}
}

// Get returns the session for a user, creating a new idle session if needed.
func (sm *SessionManager) Get(userID int64) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.sessions[userID]
	if !ok {
		s = &Session{State: StateIdle, LastActivity: time.Now()}
		sm.sessions[userID] = s
	}
	s.LastActivity = time.Now()
	return s
}

// Set overwrites the session for a user.
func (sm *SessionManager) Set(userID int64, s *Session) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	s.LastActivity = time.Now()
	sm.sessions[userID] = s
}

// Delete removes a session.
func (sm *SessionManager) Delete(userID int64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.sessions, userID)
}

// StartCleanup runs a goroutine that evicts stale sessions every 10 minutes.
func (sm *SessionManager) StartCleanup(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sm.evict()
			}
		}
	}()
}

func (sm *SessionManager) evict() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	cutoff := time.Now().Add(-sm.ttl)
	for id, s := range sm.sessions {
		if s.LastActivity.Before(cutoff) {
			delete(sm.sessions, id)
		}
	}
}
