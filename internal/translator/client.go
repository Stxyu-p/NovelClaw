package translator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"novelclaw/internal/config"
)

// LLMMessage represents a single chat message.
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest is the OpenAI-compatible chat payload used by the
// adapters that implement the Chat Completions protocol.
type ChatCompletionRequest struct {
	Model       string       `json:"model"`
	Messages    []LLMMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Stream      bool         `json:"stream"`
}

// ChatCompletionResponse is the minimal response shape NovelClaw needs.
type ChatCompletionResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Client routes translation requests through the active provider profile.
// Provider-specific URL/auth/protocol behavior lives in provider_client.go.
type Client struct {
	cfg        *config.AppConfig
	httpClient *http.Client
}

func NewClient(cfg *config.AppConfig) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 180 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 120 * time.Second,
				ForceAttemptHTTP2:     true,
				MaxIdleConns:          32,
				MaxIdleConnsPerHost:   8,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}
}

// CompleteWithFallback tries each model in order using one provider snapshot.
func (c *Client) CompleteWithFallback(ctx context.Context, systemPrompt, userPrompt string, models []string, temperature float64) (string, string, error) {
	return c.CompleteWithFallbackForProvider(ctx, c.cfg.ActiveProvider(), systemPrompt, userPrompt, models, temperature)
}

// CompleteWithFallbackForProvider pins BaseURL/key/protocol for the whole
// operation so changing Settings cannot move an in-flight job to another provider.
func (c *Client) CompleteWithFallbackForProvider(ctx context.Context, provider config.ActiveProvider, systemPrompt, userPrompt string, models []string, temperature float64) (string, string, error) {
	if strings.TrimSpace(provider.BaseURL) == "" {
		return "", "", fmt.Errorf("provider %s has no base URL", provider.Name)
	}
	if provider.KeyRequired && strings.TrimSpace(provider.APIKey) == "" {
		return "", "", fmt.Errorf("%s requires an API key", provider.Name)
	}
	if len(models) == 0 {
		models = []string{provider.Model}
	}
	if temperature <= 0 {
		temperature = c.cfg.GetTemperature()
	}
	var lastErr error
	for _, candidate := range models {
		modelName := strings.TrimSpace(candidate)
		if modelName == "" {
			modelName = strings.TrimSpace(provider.Model)
		}
		if modelName == "" {
			lastErr = fmt.Errorf("no model selected for provider %s", provider.Name)
			continue
		}
		out, err := c.completeForProvider(ctx, provider, systemPrompt, userPrompt, modelName, temperature)
		if err == nil {
			return out, modelName, nil
		}
		lastErr = err
		var statusErr *ProviderHTTPError
		if errors.As(err, &statusErr) && statusErr.Status == 401 {
			return "", "", err
		}
		if ctx.Err() != nil {
			return "", "", lastErr
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no fallback models configured")
	}
	return "", "", lastErr
}
