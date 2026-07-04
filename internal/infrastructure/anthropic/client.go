package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	apiEndpoint     = "https://api.anthropic.com/v1/messages"
	apiVersion      = "2023-06-01"
	defaultModel    = "claude-haiku-4-5-20251001"
	defaultMaxTokens = 800
)

// ContentBlock represents a message content block.
type ContentBlock struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	Source *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source,omitempty"`
}

// MessageRequest is the Anthropic Messages API request body.
type MessageRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	Messages  []MessageParam `json:"messages"`
}

// MessageParam is a single message in the conversation.
type MessageParam struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// MessageResponse is the Anthropic Messages API response body.
type MessageResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Client is an Anthropic API client.
type Client struct {
	apiKey     string
	httpClient *http.Client
	model      string
}

// NewClient creates a new Anthropic client.
func NewClient(apiKey string) *Client {
	if apiKey == "" {
		return nil
	}
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		model: defaultModel,
	}
}

// SetModel overrides the default model.
func (c *Client) SetModel(model string) {
	c.model = model
}

// SendMessage sends a multimodal message and returns the assistant's text.
func (c *Client) SendMessage(ctx context.Context, prompt string, imageBlocks []ContentBlock) (string, error) {
	if c == nil {
		return "", fmt.Errorf("anthropic client not initialized")
	}

	content := make([]ContentBlock, 0, len(imageBlocks)+1)
	content = append(content, imageBlocks...)
	content = append(content, ContentBlock{Type: "text", Text: prompt})

	reqBody := MessageRequest{
		Model:     c.model,
		MaxTokens: defaultMaxTokens,
		Messages: []MessageParam{{
			Role:    "user",
			Content: content,
		}},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	var msgResp MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if msgResp.Error != nil {
		return "", fmt.Errorf("anthropic error %s: %s", msgResp.Error.Type, msgResp.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic status %d", resp.StatusCode)
	}
	if len(msgResp.Content) == 0 {
		return "", fmt.Errorf("empty content")
	}
	return msgResp.Content[0].Text, nil
}
