package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/0xgetz/deepseek2api/deepseek"
)

const (
	maxImageSize = 20 << 20 // 20 MB per image
	maxImages    = 8        // per request
)

type imageRef struct {
	URL string // data:... or http(s)://...
}

type imagePart struct {
	Data        []byte
	ContentType string
	Extension   string
}

var imageHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

var mimeExtension = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/webp": "webp",
	"image/gif":  "gif",
	"image/bmp":  "bmp",
}

// contentFingerprint builds the conversation-prefix hash input for one
// message: its text plus a stable digest of every image reference. It never
// touches the network so history replay stays cheap.
func contentFingerprint(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return string(raw)
	}
	var b strings.Builder
	for _, p := range parts {
		var typ string
		json.Unmarshal(p["type"], &typ)
		switch typ {
		case "text":
			var t string
			if json.Unmarshal(p["text"], &t) == nil {
				b.WriteString(t)
			}
		case "image_url", "input_image":
			url := imageURLFromPart(p)
			if url == "" {
				continue
			}
			sum := sha256.Sum256([]byte(url))
			b.WriteString("\x03img:" + hex.EncodeToString(sum[:8]))
		}
	}
	return b.String()
}

// imageURLFromPart pulls the url out of an image part, accepting both the
// Chat Completions shape ({"image_url":{"url":...}}) and the Responses shape
// ({"image_url":"..."}).
func imageURLFromPart(part map[string]json.RawMessage) string {
	var iu json.RawMessage
	if v, ok := part["image_url"]; ok {
		iu = v
	} else if v, ok := part["url"]; ok {
		iu = v
	} else {
		return ""
	}
	var asObject struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(iu, &asObject) == nil && asObject.URL != "" {
		return asObject.URL
	}
	var asString string
	if json.Unmarshal(iu, &asString) == nil {
		return asString
	}
	return ""
}

// extractImages returns the image references of one message's content.
func extractImages(raw json.RawMessage) []imageRef {
	if len(raw) == 0 {
		return nil
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	var out []imageRef
	for _, p := range parts {
		var typ string
		json.Unmarshal(p["type"], &typ)
		if typ != "image_url" && typ != "input_image" {
			continue
		}
		if url := imageURLFromPart(p); url != "" {
			out = append(out, imageRef{URL: url})
		}
	}
	return out
}

// load materializes an image reference into bytes.
func (ir imageRef) load() (*imagePart, error) {
	if strings.HasPrefix(ir.URL, "data:") {
		return loadImageDataURL(ir.URL)
	}
	if !strings.HasPrefix(ir.URL, "http://") && !strings.HasPrefix(ir.URL, "https://") {
		return nil, fmt.Errorf("unsupported image url scheme: %.32s", ir.URL)
	}
	resp, err := imageHTTPClient.Get(ir.URL)
	if err != nil {
		return nil, fmt.Errorf("fetch image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image %s: http %d", truncate(ir.URL, 60), resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize+1))
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if len(data) > maxImageSize {
		return nil, fmt.Errorf("image larger than %d MB", maxImageSize>>20)
	}
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	if !strings.HasPrefix(ct, "image/") {
		ct = http.DetectContentType(data)
		if !strings.HasPrefix(ct, "image/") {
			return nil, fmt.Errorf("url is not an image (%s)", ct)
		}
	}
	return newImagePart(data, ct)
}

func loadImageDataURL(url string) (*imagePart, error) {
	comma := strings.Index(url, ",")
	if comma < 0 {
		return nil, fmt.Errorf("malformed data url")
	}
	meta := strings.ToLower(url[len("data:"):comma])
	payload := url[comma+1:]
	if !strings.Contains(meta, "base64") {
		return nil, fmt.Errorf("only base64 data urls are supported")
	}
	meta = strings.TrimPrefix(meta, "base64,") // "image/png;base64" -> "image/png"
	meta = strings.TrimSuffix(meta, ";base64")
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("bad base64 image data: %w", err)
	}
	if len(data) > maxImageSize {
		return nil, fmt.Errorf("image larger than %d MB", maxImageSize>>20)
	}
	ct := meta
	if !strings.HasPrefix(ct, "image/") {
		ct = http.DetectContentType(data)
	}
	return newImagePart(data, ct)
}

func newImagePart(data []byte, contentType string) (*imagePart, error) {
	contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	ext, ok := mimeExtension[contentType]
	if !ok {
		if strings.HasPrefix(contentType, "image/") {
			ext = strings.TrimPrefix(contentType, "image/")
		} else {
			return nil, fmt.Errorf("unsupported image type %q", contentType)
		}
	}
	return &imagePart{Data: data, ContentType: contentType, Extension: ext}, nil
}

// uploadImages uploads every reference and waits for parsing, returning the
// upstream file ids in order.
func uploadImages(ctx context.Context, client *deepseek.Client, refs []imageRef) ([]string, error) {
	if len(refs) > maxImages {
		return nil, fmt.Errorf("too many images: %d (max %d)", len(refs), maxImages)
	}
	ids := make([]string, 0, len(refs))
	for i, ref := range refs {
		part, err := ref.load()
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", i+1, err)
		}
		name := fmt.Sprintf("image-%s.%s", randHex(4), part.Extension)
		info, err := client.UploadFile(ctx, name, part.ContentType, part.Data)
		if err != nil {
			return nil, fmt.Errorf("image %d upload: %w", i+1, err)
		}
		if _, err := client.WaitFileReady(ctx, info.ID, 60*time.Second); err != nil {
			return nil, fmt.Errorf("image %d: %w", i+1, err)
		}
		ids = append(ids, info.ID)
	}
	return ids, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
