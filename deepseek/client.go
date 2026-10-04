package deepseek

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://chat.deepseek.com"

// Client talks to chat.deepseek.com on behalf of one browser account.
// The only credential is the web client's bearer token (localStorage
// userToken); no cookies are required.
type Client struct {
	BaseURL   string
	Token     string
	DeviceID  string
	Version   string
	Locale    string
	TZOffset  int // seconds east of UTC, as the web client sends it
	UserAgent string
	HTTP      *http.Client
}

func NewClient(token string) *Client {
	_, offset := time.Now().Zone()
	return &Client{
		BaseURL:   defaultBaseURL,
		Token:     token,
		DeviceID:  newUUID(),
		Version:   "2.5.0",
		Locale:    "zh_CN",
		TZOffset:  offset,
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36",
		HTTP: &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          20,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
			},
		},
	}
}

// APIError is a structured upstream failure.
type APIError struct {
	HTTPStatus int
	Code       int
	Msg        string
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("deepseek api: http %d, code %d: %s", e.HTTPStatus, e.Code, e.Msg)
	}
	return fmt.Sprintf("deepseek api: http %d: %s", e.HTTPStatus, e.Msg)
}

type envelope struct {
	Code int          `json:"code"`
	Msg  string       `json:"msg"`
	Data *bizEnvelope `json:"data"`
}

type bizEnvelope struct {
	BizCode int             `json:"biz_code"`
	BizMsg  string          `json:"biz_msg"`
	BizData json.RawMessage `json:"biz_data"`
}

func (c *Client) newRequest(ctx context.Context, method, path string, body []byte, extra map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("authorization", "Bearer "+c.Token)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "*/*")
	req.Header.Set("origin", c.BaseURL)
	req.Header.Set("referer", c.BaseURL+"/")
	req.Header.Set("user-agent", c.UserAgent)
	req.Header.Set("x-client-bundle-id", "com.deepseek.chat")
	req.Header.Set("x-client-locale", c.Locale)
	req.Header.Set("x-client-platform", "web")
	req.Header.Set("x-client-timezone-offset", strconv.Itoa(c.TZOffset))
	req.Header.Set("x-client-version", c.Version)
	req.Header.Set("x-device-id", c.DeviceID)
	req.Header.Set("x-device-model", "")
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	return req, nil
}

// post sends a JSON body and decodes the standard {code,msg,data} envelope.
func (c *Client) post(ctx context.Context, path string, body any, extra map[string]string, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodPost, path, raw, extra)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{HTTPStatus: resp.StatusCode, Msg: truncate(string(data), 300)}
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("deepseek: bad envelope from %s: %w", path, err)
	}
	if env.Code != 0 {
		return &APIError{HTTPStatus: resp.StatusCode, Code: env.Code, Msg: env.Msg}
	}
	if out != nil && env.Data != nil && len(env.Data.BizData) > 0 {
		if err := json.Unmarshal(env.Data.BizData, out); err != nil {
			return fmt.Errorf("deepseek: bad biz_data from %s: %w", path, err)
		}
	}
	return nil
}

// CreatePowChallenge fetches a proof-of-work challenge for /chat/completion.
func (c *Client) CreatePowChallenge(ctx context.Context) (*PowChallenge, error) {
	return c.CreatePowChallengeFor(ctx, "/api/v0/chat/completion")
}

// CreatePowChallengeFor fetches a proof-of-work challenge for any target path
// (upload endpoints have their own).
func (c *Client) CreatePowChallengeFor(ctx context.Context, targetPath string) (*PowChallenge, error) {
	var out struct {
		Challenge PowChallenge `json:"challenge"`
	}
	if err := c.post(ctx, "/api/v0/chat/create_pow_challenge",
		map[string]string{"target_path": targetPath}, nil, &out); err != nil {
		return nil, err
	}
	return &out.Challenge, nil
}

// CreateSession opens a chat session and returns its id.
func (c *Client) CreateSession(ctx context.Context) (string, error) {
	var out struct {
		ChatSession struct {
			ID string `json:"id"`
		} `json:"chat_session"`
	}
	if err := c.post(ctx, "/api/v0/chat_session/create", map[string]any{}, nil, &out); err != nil {
		return "", err
	}
	if out.ChatSession.ID == "" {
		return "", fmt.Errorf("deepseek: empty session id")
	}
	return out.ChatSession.ID, nil
}

// DeleteSession removes a chat session from the account.
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	return c.post(ctx, "/api/v0/chat_session/delete",
		map[string]string{"chat_session_id": sessionID}, nil, nil)
}

// FileInfo is one entry of the file upload / fetch_files API.
type FileInfo struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"`
}

// UploadFile uploads a file (e.g. an image) and returns its info. Uploads
// carry their own proof-of-work with target_path = the upload endpoint.
func (c *Client) UploadFile(ctx context.Context, filename, contentType string, data []byte) (*FileInfo, error) {
	const uploadPath = "/api/v0/file/upload_file"
	challenge, err := c.CreatePowChallengeFor(ctx, uploadPath)
	if err != nil {
		return nil, fmt.Errorf("fetch pow challenge: %w", err)
	}
	answer, err := challenge.Solve()
	if err != nil {
		return nil, fmt.Errorf("solve pow: %w", err)
	}
	powHeader, err := powResponseHeader(challenge, answer)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeMIMEString(filename)))
	h.Set("Content-Type", contentType)
	fw, err := mw.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	req, err := c.newRequest(ctx, http.MethodPost, uploadPath, buf.Bytes(),
		map[string]string{
			"content-type":      mw.FormDataContentType(),
			"x-ds-pow-response": powHeader,
		})
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{HTTPStatus: resp.StatusCode, Msg: truncate(string(body), 300)}
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("deepseek: bad upload envelope: %w", err)
	}
	if env.Code != 0 {
		return nil, &APIError{HTTPStatus: resp.StatusCode, Code: env.Code, Msg: env.Msg}
	}
	info := &FileInfo{}
	if env.Data != nil && len(env.Data.BizData) > 0 {
		if err := json.Unmarshal(env.Data.BizData, info); err != nil || info.ID == "" {
			var wrapped struct {
				File FileInfo `json:"file"`
			}
			if json.Unmarshal(env.Data.BizData, &wrapped) == nil && wrapped.File.ID != "" {
				info = &wrapped.File
			}
		}
	}
	if info.ID == "" {
		return nil, fmt.Errorf("deepseek: upload response has no file id")
	}
	return info, nil
}

var errFileNotParseable = fmt.Errorf("deepseek: file parse failed")

// WaitFileReady polls fetch_files until the file finished parsing. Image
// understanding only works on parsed files.
func (c *Client) WaitFileReady(ctx context.Context, id string, timeout time.Duration) (*FileInfo, error) {
	deadline := time.Now().Add(timeout)
	for {
		files, err := c.FetchFiles(ctx, []string{id})
		if err == nil && len(files) > 0 {
			f := files[0]
			switch f.Status {
			case "SUCCESS":
				return &f, nil
			case "FAILED", "CONTENT_FILTER", "CONTENT_TOO_LONG", "CANCELLED":
				return nil, fmt.Errorf("%w: status %s", errFileNotParseable, f.Status)
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return nil, fmt.Errorf("deepseek: file poll failed: %w", err)
			}
			return nil, fmt.Errorf("deepseek: file parse timed out (status %s)", files[0].Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(600 * time.Millisecond):
		}
	}
}

// FetchFiles looks up file infos by ids.
func (c *Client) FetchFiles(ctx context.Context, ids []string) ([]FileInfo, error) {
	req, err := c.newRequest(ctx, http.MethodGet,
		"/api/v0/file/fetch_files?file_ids="+url.QueryEscape(strings.Join(ids, ",")), nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{HTTPStatus: resp.StatusCode, Msg: truncate(string(body), 300)}
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("deepseek: bad fetch_files envelope: %w", err)
	}
	if env.Code != 0 {
		return nil, &APIError{HTTPStatus: resp.StatusCode, Code: env.Code, Msg: env.Msg}
	}
	var out struct {
		Files []FileInfo `json:"files"`
	}
	if env.Data != nil && len(env.Data.BizData) > 0 {
		if err := json.Unmarshal(env.Data.BizData, &out); err != nil {
			return nil, fmt.Errorf("deepseek: bad fetch_files biz_data: %w", err)
		}
	}
	return out.Files, nil
}

// CompletionInput describes one completion turn.
type CompletionInput struct {
	SessionID       string
	ParentMessageID *int64  // nil for the first turn
	ModelType       *string // "default" for the first turn, nil afterwards
	Prompt          string
	RefFileIDs      []string // uploaded file ids (e.g. images) for this turn
	ThinkingEnabled bool
	SearchEnabled   bool
}

type completionPayload struct {
	ChatSessionID   string  `json:"chat_session_id"`
	ParentMessageID *int64  `json:"parent_message_id"`
	ModelType       *string `json:"model_type"`
	Prompt          string  `json:"prompt"`
	RefFileIDs      []any   `json:"ref_file_ids"`
	ThinkingEnabled bool    `json:"thinking_enabled"`
	SearchEnabled   bool    `json:"search_enabled"`
	Action          *string `json:"action"`
	Preempt         bool    `json:"preempt"`
}

// Completion runs the full three-step flow: fetch challenge, solve it, then
// stream /chat/completion, feeding frames to cb. It returns when the stream
// closes (normal end), the context is canceled, or an error occurs.
func (c *Client) Completion(ctx context.Context, in CompletionInput, cb *StreamCallbacks) error {
	challenge, err := c.CreatePowChallenge(ctx)
	if err != nil {
		return fmt.Errorf("fetch pow challenge: %w", err)
	}
	answer, err := challenge.Solve()
	if err != nil {
		return fmt.Errorf("solve pow: %w", err)
	}
	powHeader, err := powResponseHeader(challenge, answer)
	if err != nil {
		return err
	}
	refFileIDs := make([]any, 0, len(in.RefFileIDs))
	for _, id := range in.RefFileIDs {
		refFileIDs = append(refFileIDs, id)
	}
	payload := completionPayload{
		ChatSessionID:   in.SessionID,
		ParentMessageID: in.ParentMessageID,
		ModelType:       in.ModelType,
		Prompt:          in.Prompt,
		RefFileIDs:      refFileIDs,
		ThinkingEnabled: in.ThinkingEnabled,
		SearchEnabled:   in.SearchEnabled,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := c.newRequest(ctx, http.MethodPost, "/api/v0/chat/completion", raw,
		map[string]string{"x-ds-pow-response": powHeader})
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		apiErr := &APIError{HTTPStatus: resp.StatusCode, Msg: truncate(string(data), 300)}
		var env envelope
		if json.Unmarshal(data, &env) == nil && env.Code != 0 {
			apiErr.Code = env.Code
			apiErr.Msg = env.Msg
		}
		return apiErr
	}

	parser := &sseParser{cb: cb}
	return readSSE(resp.Body, parser.handle)
}

func powResponseHeader(ch *PowChallenge, answer uint64) (string, error) {
	obj := struct {
		Algorithm  string `json:"algorithm"`
		Challenge  string `json:"challenge"`
		Salt       string `json:"salt"`
		Answer     uint64 `json:"answer"`
		Signature  string `json:"signature"`
		TargetPath string `json:"target_path"`
	}{
		Algorithm:  ch.Algorithm,
		Challenge:  ch.Challenge,
		Salt:       ch.Salt,
		Answer:     answer,
		Signature:  ch.Signature,
		TargetPath: ch.TargetPath,
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// escapeMIMEString escapes a filename for use in a Content-Disposition header.
func escapeMIMEString(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\r", " ", "\n", " ").Replace(s)
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
