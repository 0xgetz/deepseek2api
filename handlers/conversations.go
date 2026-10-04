package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"sync"
	"time"

	"github.com/0xgetz/deepseek2api/deepseek"
)

// Conversation is one live DeepSeek session plus the bookkeeping needed to
// continue it (the last assistant message id and cumulative usage).
type Conversation struct {
	mu        sync.Mutex
	Client    *deepseek.Client
	SessionID string
	ParentID  int64
	HasParent bool
	Turn      int
	LastUsage int64
	HasUsage  bool
	LastUsed  time.Time
}

// Lock guards the conversation for one completion round trip.
func (c *Conversation) Lock() { c.mu.Lock() }
func (c *Conversation) Unlock() {
	c.LastUsed = time.Now()
	c.mu.Unlock()
}

// ConvStore maps derived conversation keys and DeepSeek session ids to
// conversations, and removes idle sessions upstream after a TTL.
type ConvStore struct {
	ttl       time.Duration
	maxSize   int
	mu        sync.Mutex
	byPrefix  map[string]*Conversation
	bySession map[string]*Conversation
}

func NewConvStore(ttl time.Duration, maxSize int) *ConvStore {
	return &ConvStore{
		ttl:       ttl,
		maxSize:   maxSize,
		byPrefix:  make(map[string]*Conversation),
		bySession: make(map[string]*Conversation),
	}
}

func (s *ConvStore) ByPrefix(key string) *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byPrefix[key]
}

func (s *ConvStore) BySession(sessionID string) *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bySession[sessionID]
}

// Register stores a conversation under a derived prefix key; the session-id
// mapping is kept in every case so conversation_id passthrough keeps working.
func (s *ConvStore) Register(prefixKey string, conv *Conversation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bySession[conv.SessionID] = conv
	if prefixKey != "" {
		s.byPrefix[prefixKey] = conv
	}
	for len(s.bySession) > s.maxSize {
		var oldestKey string
		var oldest *Conversation
		for key, c := range s.bySession {
			if oldest == nil || c.LastUsed.Before(oldest.LastUsed) {
				oldestKey, oldest = key, c
			}
		}
		s.removeLocked(oldestKey, oldest)
	}
}

// removeLocked drops a conversation from both maps; callers hold s.mu.
func (s *ConvStore) removeLocked(sessionKey string, conv *Conversation) {
	delete(s.bySession, sessionKey)
	for k, v := range s.byPrefix {
		if v == conv {
			delete(s.byPrefix, k)
		}
	}
}

// SweepLoop deletes DeepSeek sessions that have been idle past the TTL so the
// account's session list stays clean.
func (s *ConvStore) SweepLoop(ctx context.Context) {
	interval := s.ttl / 6
	if interval < time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

func (s *ConvStore) sweepOnce(ctx context.Context) {
	s.mu.Lock()
	var expired []*Conversation
	for key, c := range s.bySession {
		if time.Since(c.LastUsed) >= s.ttl {
			expired = append(expired, c)
			s.removeLocked(key, c)
		}
	}
	s.mu.Unlock()

	for _, conv := range expired {
		dctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := conv.Client.DeleteSession(dctx, conv.SessionID)
		cancel()
		if err != nil {
			log.Printf("conversation cleanup: delete session %s failed: %v", conv.SessionID, err)
			// Queue for a retry on the next sweep; do not hammer upstream.
			s.mu.Lock()
			conv.LastUsed = time.Now().Add(-s.ttl)
			s.bySession[conv.SessionID] = conv
			s.mu.Unlock()
		}
		// The upstream API rate-limits bursts of session operations.
		time.Sleep(1500 * time.Millisecond)
	}
}

// PrefixKey derives the cache key for "messages except the last one", so a
// client that appends one exchange per request keeps hitting the same
// DeepSeek session. The model is part of the key: switching models starts a
// new session, matching DeepSeek's per-session model. Image references are
// fingerprinted too, so image turns cache correctly.
func PrefixKey(messages []chatMessage, model string) string {
	h := sha256.New()
	for _, m := range messages {
		h.Write([]byte(m.Role))
		h.Write([]byte{0x1f})
		h.Write([]byte(contentFingerprint(m.Content)))
		h.Write([]byte{0x1e})
	}
	h.Write([]byte{0x00})
	h.Write([]byte(model))
	return hex.EncodeToString(h.Sum(nil)[:16])
}
