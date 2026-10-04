package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/0xgetz/deepseek2api/deepseek"
)

// sseWriter emits OpenAI chat.completion.chunk frames.
type sseWriter struct {
	w            http.ResponseWriter
	flusher      http.Flusher
	id           string
	model        string
	created      int64
	conversation string
	opened       bool
	failed       bool
}

func newSSEWriter(w http.ResponseWriter, id, model string, created int64, conversation string) *sseWriter {
	return &sseWriter{w: w, id: id, model: model, created: created, conversation: conversation}
}

func (s *sseWriter) open() error {
	h := s.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	f, ok := s.w.(http.Flusher)
	if !ok {
		s.failed = true
		return errNoFlusher
	}
	s.flusher = f
	s.opened = true

	first := map[string]any{
		"role": "assistant",
	}
	return s.send(first, nil)
}

func (s *sseWriter) delta(kind deepseek.DeltaKind, text string) {
	if s.failed || !s.opened || text == "" {
		return
	}
	d := map[string]any{"content": text}
	if kind == deepseek.DeltaThink {
		d = map[string]any{"reasoning_content": text}
	}
	if err := s.send(d, nil); err != nil {
		s.failed = true
	}
}

func (s *sseWriter) finish(usage map[string]int, includeUsage bool) {
	if s.failed {
		return
	}
	stop := "stop"
	if err := s.send(map[string]any{}, &stop); err != nil {
		s.failed = true
		return
	}
	if includeUsage && usage != nil {
		frame := map[string]any{
			"id":              s.id,
			"object":          "chat.completion.chunk",
			"created":         s.created,
			"model":           s.model,
			"conversation_id": s.conversation,
			"choices":         []any{},
			"usage":           usage,
		}
		s.writeFrame(frame)
	}
	s.writeRaw("data: [DONE]\n\n")
}

// errorText reports an upstream failure on a stream that already started.
func (s *sseWriter) errorText(msg string) {
	if s.failed || !s.opened {
		return
	}
	stop := "stop"
	if err := s.send(map[string]any{"error": msg}, &stop); err != nil {
		s.failed = true
		return
	}
	s.writeRaw("data: [DONE]\n\n")
}

func (s *sseWriter) send(delta map[string]any, finish *string) error {
	frame := map[string]any{
		"id":              s.id,
		"object":          "chat.completion.chunk",
		"created":         s.created,
		"model":           s.model,
		"conversation_id": s.conversation,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finish,
		}},
	}
	return s.writeFrame(frame)
}

func (s *sseWriter) writeFrame(frame map[string]any) error {
	raw, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return s.writeRaw("data: " + string(raw) + "\n\n")
}

func (s *sseWriter) writeRaw(str string) error {
	if _, err := s.w.Write([]byte(str)); err != nil {
		s.failed = true
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}

type flusherError struct{}

func (flusherError) Error() string { return "response writer does not support flushing" }

var errNoFlusher = flusherError{}
