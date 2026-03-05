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

// Poster publishes storm evaluations to Discord. PostNew opens a new thread and
// returns its ID; PostUpdate appends to an existing thread; PostGrouped opens a
// thread for a grouped multi-region storm alert.
type Poster interface {
	PostNew(ctx context.Context, eval domain.Evaluation, region domain.Region) (threadID string, err error)
	PostUpdate(ctx context.Context, eval domain.Evaluation, region domain.Region, threadID string) error
	PostGrouped(ctx context.Context, group GroupedPost) (threadID string, err error)
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

// PostNew formats and posts a new storm alert, opening a forum thread.
// Returns the thread ID (Discord channel_id) so the pipeline can store it for updates.
func (w *WebhookClient) PostNew(ctx context.Context, eval domain.Evaluation, region domain.Region) (string, error) {
	payload := FormatNewStorm(eval, region)

	// ?wait=true makes Discord return the created message so we can extract the thread ID.
	url := w.webhookURL + "?wait=true"

	body, err := w.postWithRetry(ctx, url, payload)
	if err != nil {
		return "", fmt.Errorf("post new storm for region %s: %w", region.ID, err)
	}

	var resp threadResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse discord response for region %s: %w", region.ID, err)
	}
	if resp.ChannelID == "" {
		return "", fmt.Errorf("discord response missing channel_id for region %s", region.ID)
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

// PostGrouped formats and posts a grouped storm alert covering multiple regions,
// opening a forum thread. Returns the thread ID for later updates.
func (w *WebhookClient) PostGrouped(ctx context.Context, group GroupedPost) (string, error) {
	payload := FormatGroupedStorm(group)
	url := w.webhookURL + "?wait=true"
	body, err := w.postWithRetry(ctx, url, payload)
	if err != nil {
		return "", fmt.Errorf("post grouped storm for %s: %w", group.MacroRegionName, err)
	}
	var resp threadResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse discord response for grouped %s: %w", group.MacroRegionName, err)
	}
	if resp.ChannelID == "" {
		return "", fmt.Errorf("discord response missing channel_id for grouped %s", group.MacroRegionName)
	}
	return resp.ChannelID, nil
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

	body, _ := io.ReadAll(resp.Body)

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
