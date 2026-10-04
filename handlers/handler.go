package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/0xgetz/deepseek2api/config"
	"github.com/0xgetz/deepseek2api/deepseek"
)

// ModelInfo maps a served model id to DeepSeek web parameters.
type ModelInfo struct {
	ID       string
	Aliases  []string
	Thinking bool
	Search   bool
}

var models = []ModelInfo{
	{ID: "deepseek-chat", Aliases: []string{"deepseek-v3"}},
	{ID: "deepseek-reasoner", Aliases: []string{"deepseek-r1"}, Thinking: true},
	{ID: "deepseek-search", Search: true},
	{ID: "deepseek-reasoner-search", Thinking: true, Search: true},
}

func lookupModel(name string) (*ModelInfo, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for i := range models {
		if models[i].ID == name {
			return &models[i], true
		}
		for _, a := range models[i].Aliases {
			if a == name {
				return &models[i], true
			}
		}
	}
	return nil, false
}

// resolveModel matches a requested model id, falling back to heuristics for
// unknown deepseek-* names so downstream clients that hardcode future or
// renamed ids (deepseek-v4.1-flash & co) keep working.
func resolveModel(name string) (*ModelInfo, bool) {
	if m, ok := lookupModel(name); ok {
		return m, true
	}
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, "deepseek") {
		return nil, false
	}
	thinking := strings.Contains(lower, "reasoner") || strings.Contains(lower, "think") || strings.Contains(lower, "-r1")
	search := strings.Contains(lower, "search")
	for i := range models {
		if models[i].Thinking == thinking && models[i].Search == search {
			return &models[i], true
		}
	}
	return nil, false
}

// AccountPool round-robins the configured DeepSeek tokens.
type AccountPool struct {
	clients []*deepseek.Client
	next    atomic.Uint64
}

func NewPool(tokens []string) *AccountPool {
	p := &AccountPool{}
	for _, t := range tokens {
		p.clients = append(p.clients, deepseek.NewClient(t))
	}
	return p
}

func (p *AccountPool) Pick() *deepseek.Client {
	return p.clients[p.next.Add(1)%uint64(len(p.clients))]
}

// Handler serves the OpenAI-compatible API.
type Handler struct {
	cfg   *config.Config
	pool  *AccountPool
	convs *ConvStore
}

func New(cfg *config.Config, pool *AccountPool, convs *ConvStore) *Handler {
	return &Handler{cfg: cfg, pool: pool, convs: convs}
}

// ---- OpenAI wire types ----

type chatMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Stream         bool          `json:"stream"`
	ConversationID string        `json:"conversation_id"` // non-standard: continue a session
	StreamOptions  *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}

type openAIError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, errType, msg string) {
	var e openAIError
	e.Error.Message = msg
	e.Error.Type = errType
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(e)
}

// Auth guards /v1/* with the proxy API key.
func (h *Handler) Auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := ""
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			key = strings.TrimPrefix(auth, "Bearer ")
		}
		if key == "" {
			key = r.Header.Get("X-Api-Key")
		}
		if key == "" || key != h.cfg.ProxyAPIKey {
			writeError(w, http.StatusUnauthorized, "authentication_error", "invalid proxy API key")
			return
		}
		next(w, r)
	}
}

// ListModels serves GET /v1/models.
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	type model struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}
	now := time.Now().Unix()
	out := struct {
		Object string  `json:"object"`
		Data   []model `json:"data"`
	}{Object: "list"}
	for _, m := range models {
		out.Data = append(out.Data, model{ID: m.ID, Object: "model", Created: now, OwnedBy: "deepseek"})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ChatCompletion serves POST /v1/chat/completions.
func (h *Handler) ChatCompletion(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "bad JSON body: "+err.Error())
		return
	}

	modelName := req.Model
	if modelName == "" {
		modelName = h.cfg.DefaultModel
	}
	m, ok := resolveModel(modelName)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("unknown model %q; available: deepseek-chat, deepseek-reasoner, deepseek-search, deepseek-reasoner-search", req.Model))
		return
	}

	// The last user message is the prompt; the rest is context that either the
	// conversation cache or the upstream session already holds.
	var systemParts []string
	var lastUserText string
	var lastUserImages []imageRef
	hasUser := false
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system", "developer":
			if text := contentText(msg.Content); text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			hasUser = true
			lastUserText = contentText(msg.Content)
			lastUserImages = extractImages(msg.Content)
		}
	}
	if !hasUser || (strings.TrimSpace(lastUserText) == "" && len(lastUserImages) == 0) {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "messages must contain a user message with text or images")
		return
	}
	prompt := lastUserText

	conv, fresh, err := h.conversation(&req, m)
	if err != nil {
		log.Printf("chat: create session failed: %v", err)
		status, msg := mapUpstreamError(err)
		writeError(w, status, "api_error", msg)
		return
	}

	conv.Lock()
	defer conv.Unlock()

	if fresh && len(systemParts) > 0 {
		prompt = "[System]\n" + strings.Join(systemParts, "\n\n") + "\n[/System]\n\n" + prompt
	}

	// Images ride along as uploaded files referenced by ref_file_ids. Only the
	// last user message's images are sent; older ones are already in the
	// upstream session context.
	var refFileIDs []string
	if len(lastUserImages) > 0 {
		ids, err := uploadImages(r.Context(), conv.Client, lastUserImages)
		if err != nil {
			log.Printf("chat: image upload failed: %v", err)
			writeError(w, http.StatusBadGateway, "api_error", "image upload failed: "+err.Error())
			return
		}
		refFileIDs = ids
	}

	input := deepseek.CompletionInput{
		SessionID:       conv.SessionID,
		ThinkingEnabled: m.Thinking,
		SearchEnabled:   m.Search,
		Prompt:          prompt,
		RefFileIDs:      refFileIDs,
	}
	if conv.HasParent {
		parent := conv.ParentID
		input.ParentMessageID = &parent
		input.ModelType = nil
	} else {
		defaultModel := "default"
		input.ModelType = &defaultModel
	}

	completionID := "chatcmpl-" + randHex(16)
	createdAt := time.Now().Unix()

	var reply, thinking strings.Builder
	var usageTotal int64 = -1

	cb := &deepseek.StreamCallbacks{
		OnReady: func(requestID, responseID int64) {
			conv.ParentID = responseID
			conv.HasParent = true
			conv.Turn++
		},
		OnDelta: func(kind deepseek.DeltaKind, text string) {
			if kind == deepseek.DeltaThink {
				thinking.WriteString(text)
			} else {
				reply.WriteString(text)
			}
		},
		OnUsage: func(accumulated int64) {
			usageTotal = accumulated
		},
	}

	var stream *sseWriter
	if req.Stream {
		stream = newSSEWriter(w, completionID, modelName, createdAt, conv.SessionID)
		if err := stream.open(); err != nil {
			log.Printf("chat: client went away before stream start: %v", err)
			return
		}
		onDelta := cb.OnDelta
		cb.OnDelta = func(kind deepseek.DeltaKind, text string) {
			onDelta(kind, text)
			stream.delta(kind, text)
		}
	}

	err = conv.Client.Completion(r.Context(), input, cb)

	// Register the continuation key even on partial streams: the ready event
	// already advanced the parent message id, so follow-ups can chain.
	key := PrefixKey(append(append([]chatMessage{}, req.Messages...),
		chatMessage{Role: "assistant", Content: json.RawMessage(mustJSON(reply.String()))}), m.ID)
	h.convs.Register(key, conv)

	if err != nil {
		if r.Context().Err() != nil {
			return // client gone; upstream message completes on its own
		}
		log.Printf("chat: upstream completion failed: %v", err)
		if req.Stream {
			stream.errorText(err.Error())
			return
		}
		status, msg := mapUpstreamError(err)
		writeError(w, status, "api_error", msg)
		return
	}

	usage := h.buildUsage(conv, usageTotal, reply.Len())
	if req.Stream {
		stream.finish(usage, req.StreamOptions != nil && req.StreamOptions.IncludeUsage)
		log.Printf("chat %s %s conv=%s turn=%d stream %s", completionID, modelName, conv.SessionID, conv.Turn, time.Since(start).Round(time.Millisecond))
		return
	}

	message := map[string]any{"role": "assistant", "content": reply.String()}
	if t := thinking.String(); t != "" {
		message["reasoning_content"] = t
	}
	resp := map[string]any{
		"id":              completionID,
		"object":          "chat.completion",
		"created":         createdAt,
		"model":           modelName,
		"conversation_id": conv.SessionID,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": "stop",
		}},
		"usage": usage,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	log.Printf("chat %s %s conv=%s turn=%d %s", completionID, modelName, conv.SessionID, conv.Turn, time.Since(start).Round(time.Millisecond))
}

// conversation finds a usable conversation or creates a new DeepSeek session.
func (h *Handler) conversation(req *chatRequest, m *ModelInfo) (*Conversation, bool, error) {
	if req.ConversationID != "" {
		if c := h.convs.BySession(req.ConversationID); c != nil {
			return c, false, nil
		}
	} else {
		key := PrefixKey(req.Messages[:len(req.Messages)-1], m.ID)
		if c := h.convs.ByPrefix(key); c != nil {
			return c, false, nil
		}
	}
	client := h.pool.Pick()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sessionID, err := client.CreateSession(ctx)
	if err != nil {
		return nil, false, err
	}
	return &Conversation{Client: client, SessionID: sessionID, LastUsed: time.Now()}, true, nil
}

func (h *Handler) buildUsage(conv *Conversation, total int64, replyChars int) map[string]int {
	var span int64
	if conv.HasUsage {
		span = total - conv.LastUsage
	} else {
		span = total
	}
	if total >= 0 {
		conv.LastUsage = total
		conv.HasUsage = true
	}
	if span < 0 {
		span = 0
	}
	completion := int64(replyChars / 4)
	if span == 0 {
		span = completion // upstream gave no usage frame; fall back to an estimate
	}
	prompt := span - completion
	if prompt < 0 {
		prompt = 0
	}
	return map[string]int{
		"prompt_tokens":     int(prompt),
		"completion_tokens": int(completion),
		"total_tokens":      int(span),
	}
}

func mapUpstreamError(err error) (int, string) {
	var ae *deepseek.APIError
	if errors.As(err, &ae) {
		switch {
		case ae.HTTPStatus == 401 || ae.HTTPStatus == 403:
			return http.StatusUnauthorized, "deepseek rejected the configured token (invalid or expired)"
		case ae.HTTPStatus == 429:
			return http.StatusTooManyRequests, "deepseek rate limited this account; retry later"
		default:
			return http.StatusBadGateway, "deepseek upstream error: " + ae.Error()
		}
	}
	return http.StatusBadGateway, "deepseek upstream error: " + err.Error()
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func mustJSON(s string) []byte {
	b, _ := json.Marshal(s)
	return b
}
