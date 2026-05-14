package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"code-rag-assistant/internal/config"
)

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

type chatStreamResponse struct {
	Choices []struct {
		Delta chatMessage `json:"delta"`
	} `json:"choices"`
}

func callLLM(ctx context.Context, cfg config.Config, system, user string) (string, error) {
	return callLLMWithModel(ctx, cfg, cfg.OpenAIModel, system, user)
}

func streamLLM(ctx context.Context, cfg config.Config, system, user string, onDelta func(string) error) (string, error) {
	if cfg.OpenAIAPIKey == "" {
		return "", errors.New("OPENAI_API_KEY is not configured")
	}
	body, err := json.Marshal(chatRequest{
		Model: cfg.OpenAIModel,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Stream: true,
	})
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(cfg.OpenAIBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: cfg.LLMTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("model stream request failed: status %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var answer strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var decoded chatStreamResponse
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			return answer.String(), err
		}
		for _, choice := range decoded.Choices {
			delta := choice.Delta.Content
			if delta == "" {
				continue
			}
			answer.WriteString(delta)
			if onDelta != nil {
				if err := onDelta(delta); err != nil {
					return answer.String(), err
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return answer.String(), err
	}
	content := strings.TrimSpace(answer.String())
	if content == "" {
		return "", errors.New("model returned empty stream")
	}
	return content, nil
}

func callLLMWithModel(ctx context.Context, cfg config.Config, model, system, user string) (string, error) {
	if cfg.OpenAIAPIKey == "" {
		return "", errors.New("OPENAI_API_KEY is not configured")
	}
	if strings.TrimSpace(model) == "" {
		model = cfg.OpenAIModel
	}
	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(cfg.OpenAIBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAIAPIKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: cfg.LLMTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("model request failed: status %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var decoded chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", err
	}
	if len(decoded.Choices) == 0 {
		return "", errors.New("model returned no choices")
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return "", errors.New("model returned empty content")
	}
	return content, nil
}
