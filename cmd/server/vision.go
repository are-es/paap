package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dolvin/paap/internal/db"
)

// ── Vision Image Types ────────────────────────────────────

type visionImage struct {
	msgIdx    int
	blockIdx  int
	b64Data   string // raw base64 (no data: prefix)
	mediaType string // image/png, image/jpeg, etc.
	url       string // for URL-based images (OpenAI https://)
}

// ── Supported MIME Types ──────────────────────────────────

var visionSupportedMIME = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// ── Hard Limits ───────────────────────────────────────────

const (
	visionMaxBase64Bytes = 20 * 1024 * 1024 // 20 MB hard ceiling
	visionDefaultMaxMB   = 5                // 5 MB auto-resize target
	visionDefaultTimeout = 30000            // 30s per image
	visionDefaultConc    = 3                // max parallel vision calls
)

// ── HTTP Client for vision calls (reuse PAAP internals) ───

var visionHTTPClient = &http.Client{
	Timeout: 120 * time.Second,
}

// ── Data URI Parsing ──────────────────────────────────────

// parseDataURI extracts media_type and base64 data from "data:image/png;base64,iVBOR..."
func parseDataURI(dataURI string) (mediaType, b64Data string) {
	if !strings.HasPrefix(dataURI, "data:") {
		return "", ""
	}
	commaIdx := strings.Index(dataURI, ",")
	if commaIdx < 0 {
		return "", ""
	}
	header := dataURI[5:commaIdx] // "image/png;base64"
	data := dataURI[commaIdx+1:]

	semicolonIdx := strings.Index(header, ";")
	if semicolonIdx >= 0 {
		mediaType = header[:semicolonIdx]
	} else {
		mediaType = header
	}
	return mediaType, data
}

// ── Image Detection: OpenAI Format ────────────────────────

func detectImagesOpenAI(messages []interface{}) ([]visionImage, []interface{}) {
	var images []visionImage
	modified := make([]interface{}, len(messages))

	for i, msg := range messages {
		mm, ok := msg.(map[string]interface{})
		if !ok {
			modified[i] = msg
			continue
		}
		content, ok := mm["content"].([]interface{})
		if !ok {
			modified[i] = msg
			continue
		}

		newContent := make([]interface{}, len(content))
		copy(newContent, content)
		hasImages := false

		for j, block := range content {
			bm, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if bm["type"] != "image_url" {
				continue
			}
			imageURL, ok := bm["image_url"].(map[string]interface{})
			if !ok {
				continue
			}
			url, _ := imageURL["url"].(string)
			if url == "" {
				continue
			}

			hasImages = true
			vi := visionImage{msgIdx: i, blockIdx: j}

			if strings.HasPrefix(url, "data:") {
				vi.mediaType, vi.b64Data = parseDataURI(url)
			} else {
				vi.url = url // https:// — download later
			}

			images = append(images, vi)
			// Replace with placeholder
			newContent[j] = map[string]interface{}{
				"type": "text",
				"text": fmt.Sprintf("[Image %d - processing...]", len(images)),
			}
		}

		if hasImages {
			newMsg := make(map[string]interface{})
			for k, v := range mm {
				newMsg[k] = v
			}
			newMsg["content"] = newContent
			modified[i] = newMsg
		} else {
			modified[i] = msg
		}
	}
	return images, modified
}

// ── Image Detection: Anthropic Format ─────────────────────

func detectImagesAnthropic(messages []interface{}) ([]visionImage, []interface{}) {
	var images []visionImage
	modified := make([]interface{}, len(messages))

	for i, msg := range messages {
		mm, ok := msg.(map[string]interface{})
		if !ok {
			modified[i] = msg
			continue
		}
		content, ok := mm["content"].([]interface{})
		if !ok {
			modified[i] = msg
			continue
		}

		newContent := make([]interface{}, len(content))
		copy(newContent, content)
		hasImages := false

		for j, block := range content {
			bm, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			if bm["type"] != "image" {
				continue
			}
			source, ok := bm["source"].(map[string]interface{})
			if !ok {
				continue
			}
			sourceType, _ := source["type"].(string)
			if sourceType != "base64" {
				continue
			}
			mediaType, _ := source["media_type"].(string)
			data, _ := source["data"].(string)
			if data == "" {
				continue
			}

			hasImages = true
			images = append(images, visionImage{
				msgIdx:    i,
				blockIdx:  j,
				b64Data:   data,
				mediaType: mediaType,
			})
			// Replace with placeholder
			newContent[j] = map[string]interface{}{
				"type": "text",
				"text": fmt.Sprintf("[Image %d - processing...]", len(images)),
			}
		}

		if hasImages {
			newMsg := make(map[string]interface{})
			for k, v := range mm {
				newMsg[k] = v
			}
			newMsg["content"] = newContent
			modified[i] = newMsg
		} else {
			modified[i] = msg
		}
	}
	return images, modified
}

// ── Detect Request Format ─────────────────────────────────

func detectVisionFormat(messages []interface{}) string {
	for _, msg := range messages {
		mm, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := mm["content"].([]interface{})
		if !ok {
			continue
		}
		for _, block := range content {
			bm, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			// Anthropic format has top-level "system" or image blocks with "source"
			if bm["type"] == "image" {
				if _, ok := bm["source"].(map[string]interface{}); ok {
					return "anthropic"
				}
			}
			// OpenAI format has image_url blocks
			if bm["type"] == "image_url" {
				return "openai"
			}
		}
	}
	return "openai" // default
}

// ── URL Download ──────────────────────────────────────────

func visionDownloadImage(url string, timeoutMs int) (b64Data, mediaType string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; PAAP-Vision/1.0)")

	resp, err := visionHTTPClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}

	// Check Content-Length
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if size, err := strconv.ParseInt(cl, 10, 64); err == nil && size > int64(visionMaxBase64Bytes) {
			return "", "", fmt.Errorf("image too large: %d bytes (max %d)", size, visionMaxBase64Bytes)
		}
	}

	// Read with limit
	limitedReader := io.LimitReader(resp.Body, int64(visionMaxBase64Bytes)+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", "", fmt.Errorf("read body: %w", err)
	}
	if len(body) > visionMaxBase64Bytes {
		return "", "", fmt.Errorf("image too large: %d bytes (max %d)", len(body), visionMaxBase64Bytes)
	}

	// Detect MIME from content-type header or magic bytes
	mediaType = resp.Header.Get("Content-Type")
	if idx := strings.Index(mediaType, ";"); idx >= 0 {
		mediaType = mediaType[:idx]
	}
	mediaType = strings.TrimSpace(mediaType)
	if !visionSupportedMIME[mediaType] {
		mediaType = detectMIMEFromBytes(body)
	}

	b64Data = base64.StdEncoding.EncodeToString(body)
	return b64Data, mediaType, nil
}

func detectMIMEFromBytes(data []byte) string {
	if len(data) < 4 {
		return "image/jpeg" // default
	}
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	if data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return "image/gif"
	}
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return "image/jpeg"
}

// ── Vision Model Call ─────────────────────────────────────

func describeSingleImage(ctx context.Context, img visionImage, model, prompt string) (string, error) {
	// Build OpenAI chat completion request with image
	var content []map[string]interface{}

	if img.b64Data != "" && img.mediaType != "" {
		// Base64 image → data URI
		dataURI := fmt.Sprintf("data:%s;base64,%s", img.mediaType, img.b64Data)
		content = []map[string]interface{}{
			{"type": "text", "text": prompt},
			{"type": "image_url", "image_url": map[string]interface{}{"url": dataURI}},
		}
	} else if img.url != "" {
		content = []map[string]interface{}{
			{"type": "text", "text": prompt},
			{"type": "image_url", "image_url": map[string]interface{}{"url": img.url}},
		}
	} else {
		return "", fmt.Errorf("no image data available")
	}

	requestBody := map[string]interface{}{
		"model":    model,
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": content}},
		"stream":   false,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	// Determine PAAP's own port from environment or default
	paapPort := os.Getenv("PAAP_PORT")
	if paapPort == "" {
		paapPort = "9090"
	}

	reqURL := fmt.Sprintf("http://127.0.0.1:%s/v1/chat/completions", paapPort)
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Use internal auth — pass gateway key if available
	if gwKey := getActiveGatewayKey(); gwKey != "" {
		req.Header.Set("Authorization", "Bearer "+gwKey)
	}

	resp, err := visionHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("vision API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	// Extract text from choices[0].message.content
	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid choice format")
	}
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid message format")
	}
	contentStr, _ := message["content"].(string)
	if contentStr == "" {
		return "", fmt.Errorf("empty vision response")
	}

	return contentStr, nil
}

func describeImages(ctx context.Context, images []visionImage, model, prompt string, timeoutMs, maxConcurrent int) ([]string, error) {
	results := make([]string, len(images))
	errors := make([]error, len(images))

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for i, img := range images {
		wg.Add(1)
		go func(idx int, vi visionImage) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			imgCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
			defer cancel()

			desc, err := describeSingleImage(imgCtx, vi, model, prompt)
			if err != nil {
				errors[idx] = err
				log.Printf("[PAAP] [VISION] Image %d description failed: %v", idx+1, err)
				return
			}
			results[idx] = desc
		}(i, img)
	}
	wg.Wait()

	// Check if any succeeded
	anySuccess := false
	for _, r := range results {
		if r != "" {
			anySuccess = true
			break
		}
	}
	if !anySuccess {
		return results, fmt.Errorf("all vision calls failed")
	}
	return results, nil
}

// ── Get Active Gateway Key for Internal Calls ─────────────

func getActiveGatewayKey() string {
	var key string
	err := db.DB.QueryRow("SELECT key FROM gateway_keys WHERE is_active=1 ORDER BY created_at DESC LIMIT 1").Scan(&key)
	if err != nil {
		return ""
	}
	return key
}

// ── Main Entry: Apply Vision Tool ─────────────────────────

func applyVisionTool(rawBody map[string]interface{}) map[string]interface{} {
	// 1. Check settings
	enabled := getSettingStrCached("vision_enabled", "false")
	if enabled != "true" {
		return rawBody
	}

	visionModel := getSettingStrCached("vision_model", "")
	if visionModel == "" {
		log.Printf("[PAAP] [VISION] Enabled but no vision_model configured — skipping")
		return rawBody
	}

	visionPrompt := getSettingStrCached("vision_prompt", "Describe this image in detail. Focus on all visible text, UI elements, layout, colors, and any other relevant information.")
	timeoutMs := visionDefaultTimeout
	if t := getSettingStrCached("vision_timeout_ms", ""); t != "" {
		if parsed, err := strconv.Atoi(t); err == nil && parsed > 0 {
			timeoutMs = parsed
		}
	}
	maxConc := visionDefaultConc
	if c := getSettingStrCached("vision_max_concurrent", ""); c != "" {
		if parsed, err := strconv.Atoi(c); err == nil && parsed > 0 {
			maxConc = parsed
		}
	}

	// 2. Get messages
	messages, ok := rawBody["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return rawBody
	}

	// 3. Detect format and extract images
	format := detectVisionFormat(messages)
	var images []visionImage
	var modifiedMessages []interface{}

	if format == "anthropic" {
		images, modifiedMessages = detectImagesAnthropic(messages)
	} else {
		images, modifiedMessages = detectImagesOpenAI(messages)
	}

	if len(images) == 0 {
		return rawBody // no images found
	}

	log.Printf("[PAAP] [VISION] Detected %d image(s) in %s format — describing via %s", len(images), format, visionModel)

	// 4. Download URL-based images
	for i := range images {
		if images[i].url != "" {
			b64, mime, err := visionDownloadImage(images[i].url, timeoutMs)
			if err != nil {
				log.Printf("[PAAP] [VISION] Failed to download image URL: %v — skipping", err)
				continue
			}
			images[i].b64Data = b64
			images[i].mediaType = mime
			images[i].url = ""
		}
	}

	// 5. Validate — filter out images without data
	validImages := make([]visionImage, 0, len(images))
	validIndices := make([]int, 0, len(images))
	for i, img := range images {
		if img.b64Data != "" && img.mediaType != "" {
			// Check MIME
			if !visionSupportedMIME[img.mediaType] {
				log.Printf("[PAAP] [VISION] Unsupported MIME %s for image %d — skipping", img.mediaType, i+1)
				continue
			}
			// Check size
			b64Len := len(img.b64Data)
			decodedLen := b64Len * 3 / 4 // approximate
			if decodedLen > visionMaxBase64Bytes {
				log.Printf("[PAAP] [VISION] Image %d too large (%d bytes) — skipping", i+1, decodedLen)
				continue
			}
			validImages = append(validImages, img)
			validIndices = append(validIndices, i)
		}
	}

	if len(validImages) == 0 {
		log.Printf("[PAAP] [VISION] No valid images after filtering — returning original request")
		return rawBody
	}

	// 6. Call vision model
	ctx := context.Background()
	descriptions, err := describeImages(ctx, validImages, visionModel, visionPrompt, timeoutMs, maxConc)
	if err != nil {
		log.Printf("[PAAP] [VISION] Vision calls failed: %v — returning original request (fail-open)", err)
		return rawBody
	}

	// 7. Replace placeholders with descriptions
	for i, desc := range descriptions {
		if desc == "" {
			continue
		}
		vi := validImages[i]
		msg := modifiedMessages[vi.msgIdx].(map[string]interface{})
		content := msg["content"].([]interface{})
		content[vi.blockIdx] = map[string]interface{}{
			"type": "text",
			"text": fmt.Sprintf("[Image Description]: %s", desc),
		}
		msg["content"] = content
	}

	// 8. Return modified body
	result := make(map[string]interface{})
	for k, v := range rawBody {
		result[k] = v
	}
	result["messages"] = modifiedMessages
	return result
}
