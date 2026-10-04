package deepseek

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

// DeltaKind routes streamed text to the OpenAI channels.
type DeltaKind int

const (
	DeltaThink DeltaKind = iota // reasoning_content
	DeltaReply                  // content
)

// StreamCallbacks receive the parsed completion stream. All callbacks are
// invoked synchronously from the read loop.
type StreamCallbacks struct {
	// OnReady fires once per message with the ids assigned by the server.
	// responseID is the parent_message_id of the next turn in the session.
	OnReady func(requestID, responseID int64)
	// OnDelta fires for every text chunk of a THINK or RESPONSE fragment.
	OnDelta func(kind DeltaKind, text string)
	// OnUsage fires with the session-cumulative token usage.
	OnUsage func(accumulated int64)
}

// dsFragment mirrors one element of response.fragments.
type dsFragment struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

// sseParser implements the DeepSeek patch protocol carried over SSE:
//
//	data: {"v":{...}}                                   initial response dump
//	data: {"p":"response/fragments","o":"APPEND","v":[...]}   new fragment
//	data: {"p":"response/fragments/-1/content","o":"APPEND","v":"..."}
//	data: {"v":"..."}                                   continue last patch
//	data: {"p":"response/status","o":"SET","v":"FINISHED"}
//	data: {"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":N},...]}
type sseParser struct {
	cb        *StreamCallbacks
	lastPath  string
	fragTypes []string
}

func (p *sseParser) handle(event string, data []byte) (bool, error) {
	switch event {
	case "close":
		return true, nil
	case "ready":
		var r struct {
			RequestMessageID  int64 `json:"request_message_id"`
			ResponseMessageID int64 `json:"response_message_id"`
		}
		if err := json.Unmarshal(data, &r); err == nil && p.cb.OnReady != nil {
			p.cb.OnReady(r.RequestMessageID, r.ResponseMessageID)
		}
		return false, nil
	case "update_session", "title", "":
		return p.applyPatch(data)
	default:
		return false, nil
	}
}

func (p *sseParser) applyPatch(data []byte) (bool, error) {
	if len(data) == 0 {
		return false, nil
	}
	var pv struct {
		P *string         `json:"p"`
		O string          `json:"o"`
		V json.RawMessage `json:"v"`
	}
	if err := json.Unmarshal(data, &pv); err != nil {
		return false, nil // tolerate unknown frames
	}

	if pv.P == nil {
		var s string
		if err := json.Unmarshal(pv.V, &s); err == nil {
			p.appendText(p.lastPath, s)
			return false, nil
		}
		var dump struct {
			Response struct {
				Fragments []dsFragment `json:"fragments"`
			} `json:"response"`
		}
		if err := json.Unmarshal(pv.V, &dump); err == nil {
			for _, f := range dump.Response.Fragments {
				p.pushFragment(f)
			}
		}
		return false, nil
	}

	path := *pv.P
	p.lastPath = path
	switch {
	case path == "response/fragments" && pv.O == "APPEND":
		var frags []dsFragment
		if err := json.Unmarshal(pv.V, &frags); err == nil {
			for _, f := range frags {
				p.pushFragment(f)
			}
		}
	case path == "response" && pv.O == "BATCH":
		var subs []struct {
			P string          `json:"p"`
			V json.RawMessage `json:"v"`
		}
		if err := json.Unmarshal(pv.V, &subs); err == nil {
			for _, s := range subs {
				p.maybeUsage(s.P, s.V)
			}
		}
	case path == "response/accumulated_token_usage":
		p.maybeUsage(path, pv.V)
	case path == "response/status":
		// FINISHED arrives before the close event; nothing to do.
	case strings.HasSuffix(path, "/content"):
		var s string
		if err := json.Unmarshal(pv.V, &s); err == nil {
			p.appendText(path, s)
		}
	}
	return false, nil
}

func (p *sseParser) maybeUsage(path string, v json.RawMessage) {
	if path != "accumulated_token_usage" && !strings.HasSuffix(path, "/accumulated_token_usage") {
		return
	}
	var n int64
	if json.Unmarshal(v, &n) == nil && p.cb.OnUsage != nil {
		p.cb.OnUsage(n)
	}
}

func (p *sseParser) pushFragment(f dsFragment) {
	p.fragTypes = append(p.fragTypes, f.Type)
	p.lastPath = "response/fragments/-1/content"
	if f.Content != "" {
		p.appendText(p.lastPath, f.Content)
	}
}

func (p *sseParser) appendText(path, text string) {
	if text == "" || !strings.HasSuffix(path, "/content") || p.cb.OnDelta == nil {
		return
	}
	idx, ok := fragmentIndex(path, len(p.fragTypes))
	if !ok {
		return
	}
	switch p.fragTypes[idx] {
	case "THINK":
		p.cb.OnDelta(DeltaThink, text)
	case "RESPONSE":
		p.cb.OnDelta(DeltaReply, text)
	}
}

// fragmentIndex resolves the "response/fragments/<n>/..." index; -1 means last.
func fragmentIndex(path string, n int) (int, bool) {
	const marker = "response/fragments/"
	i := strings.Index(path, marker)
	if i < 0 {
		return 0, false
	}
	rest := path[i+len(marker):]
	j := strings.Index(rest, "/")
	if j < 0 {
		return 0, false
	}
	if rest[:j] == "-1" {
		return n - 1, n > 0
	}
	k, err := strconv.Atoi(rest[:j])
	if err != nil || k < 0 || k >= n {
		return 0, false
	}
	return k, true
}

// readSSE consumes an SSE body, dispatching complete frames to handle.
// It returns when handle reports stop, the body ends, or an error occurs.
func readSSE(r io.Reader, handle func(event string, data []byte) (bool, error)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var event string
	var dataLines []string

	dispatch := func() (bool, error) {
		if event == "" && len(dataLines) == 0 {
			return false, nil
		}
		ev := event
		data := []byte(strings.Join(dataLines, "\n"))
		event = ""
		dataLines = nil
		return handle(ev, data)
	}

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case line == "":
			stop, err := dispatch()
			if stop || err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			// comment / keep-alive
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[len("event:"):])
		case strings.HasPrefix(line, "data:"):
			v := line[len("data:"):]
			v = strings.TrimPrefix(v, " ")
			dataLines = append(dataLines, v)
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
