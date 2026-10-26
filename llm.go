package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Advisor is the seam the tests use to stand in for the real model call.
type Advisor interface {
	Advice(ctx context.Context, a Application) (string, error)
}

// LLM talks to any OpenAI-compatible chat-completions endpoint.
type LLM struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

func (l *LLM) Advice(ctx context.Context, a Application) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": l.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "You advise job seekers. Reply in three sentences, no preamble."},
			{"role": "user", "content": fmt.Sprintf(
				"Application: %s, role %s, status %s. Notes: %s. What is the next concrete step?",
				a.Company, a.Role, a.Status, a.Notes)},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+l.APIKey)

	resp, err := l.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1 * 1024 * 1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		// Never echo the upstream body: it can contain key fragments or provider internals.
		return "", fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("upstream returned no completion")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func NewLLM(baseURL, apiKey, model string) *LLM {
	return &LLM{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}
