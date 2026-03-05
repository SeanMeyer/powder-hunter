package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/seanmeyer/powder-hunter/domain"
)

// Poster publishes storm evaluations to Discord. PostBriefing opens a new thread
// with the briefing summary and returns its ID; PostDetail adds a per-region
// detail message to an existing thread; PostUpdate appends a re-evaluation update.
type Poster interface {
	PostBriefing(ctx context.Context, bp BriefingPost) (threadID string, err error)
	PostDetail(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error
	PostUpdate(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error
}

// WebhookClient sends payloads to a Discord forum-channel webhook.
type WebhookClient struct {
	webhookURL string
	client     *http.Client
	maxRetries int
}

// NewWebhookClient constructs a client for the given Discord webhook URL.
// Passing nil for client falls back to http.DefaultClient.
func NewWebhookClient(webhookURL string, client *http.Client) *WebhookClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &WebhookClient{
		webhookURL: webhookURL,
		client:     client,
		maxRetries: 3,
	}
}

// threadResponse is the subset of Discord's message response we need.
type threadResponse struct {
	// Discord forum webhook responses include the created thread's channel_id.
	ChannelID string `json:"channel_id"`
}

// PostBriefing formats and posts a storm briefing, opening a forum thread.
// Returns the thread ID (Discord channel_id) so the pipeline can store it for updates.
func (w *WebhookClient) PostBriefing(ctx context.Context, bp BriefingPost) (string, error) {
	payload := FormatBriefing(bp)
	url := w.webhookURL + "?wait=true"
	body, err := w.postWithRetry(ctx, url, payload)
	if err != nil {
		return "", fmt.Errorf("post storm briefing for %s: %w", bp.MacroRegionName, err)
	}
	var resp threadResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse discord response for briefing %s: %w", bp.MacroRegionName, err)
	}
	if resp.ChannelID == "" {
		return "", fmt.Errorf("discord response missing channel_id for briefing %s", bp.MacroRegionName)
	}
	return resp.ChannelID, nil
}

// PostUpdate posts a follow-up message into an existing forum thread.
func (w *WebhookClient) PostUpdate(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error {
	payload := FormatUpdate(eval, region)

	// ?thread_id routes the message into the existing thread rather than creating a new one.
	url := fmt.Sprintf("%s?thread_id=%s", w.webhookURL, threadID)

	if _, err := w.postWithRetry(ctx, url, payload); err != nil {
		return fmt.Errorf("post update for region %s thread %s: %w", region.ID, threadID, err)
	}
	return nil
}

// PostDetail posts a per-region detail message into an existing forum thread.
func (w *WebhookClient) PostDetail(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error {
	payload := FormatDetail(eval, region)
	url := fmt.Sprintf("%s?thread_id=%s", w.webhookURL, threadID)
	if _, err := w.postWithRetry(ctx, url, payload); err != nil {
		return fmt.Errorf("post detail for region %s thread %s: %w", region.ID, threadID, err)
	}
	return nil
}

// postWithRetry serializes payload and POST it to url, retrying on 429 and 5xx responses.
// Returns the raw response body on success.
func (w *WebhookClient) postWithRetry(ctx context.Context, url string, payload WebhookPayload) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= w.maxRetries; attempt++ {
		body, retry, err := w.doPost(ctx, url, data)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			break
		}
		// Avoid hammering Discord between retries.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * time.Second):
		}
	}
	return nil, lastErr
}

// doPost performs a single HTTP POST and returns (body, shouldRetry, error).
func (w *WebhookClient) doPost(ctx context.Context, url string, data []byte) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		// Network-level errors are transient.
		return nil, true, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf("read discord response body: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		// Respect Discord's rate-limit window before retrying.
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			if secs, parseErr := strconv.ParseFloat(retryAfter, 64); parseErr == nil {
				select {
				case <-ctx.Done():
					return nil, false, ctx.Err()
				case <-time.After(time.Duration(secs * float64(time.Second))):
				}
			}
		}
		return nil, true, fmt.Errorf("discord rate limited (429): %s", string(body))

	case resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("discord server error %d: %s", resp.StatusCode, string(body))

	case resp.StatusCode >= 400:
		// Client errors (bad payload, wrong URL) are not retryable.
		return nil, false, fmt.Errorf("discord client error %d: %s", resp.StatusCode, string(body))
	}

	return body, false, nil
}
