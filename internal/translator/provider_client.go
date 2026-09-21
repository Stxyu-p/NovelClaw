package translator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"novelclaw/internal/config"
)

const maxProviderResponse = 10 << 20

type ProviderHTTPError struct {
	Status   int
	Provider string
}

func (e *ProviderHTTPError) Error() string {
	hint := "ผู้ให้บริการตอบกลับผิดพลาด ลองใหม่ภายหลัง"
	switch e.Status {
	case 401, 403:
		hint = "ตรวจ API Key และสิทธิ์ใช้งานของผู้ให้บริการ"
	case 404:
		hint = "ตรวจ URL, Protocol และชื่อโมเดลในตั้งค่า"
	case 429:
		hint = "ถึงขีดจำกัดการใช้งาน รอสักครู่แล้วลองใหม่"
	}
	return fmt.Sprintf("%s (HTTP %d): %s", e.Provider, e.Status, hint)
}
func readProviderBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maxProviderResponse+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxProviderResponse {
		return nil, fmt.Errorf("provider response exceeds 10 MiB")
	}
	return body, nil
}

func providerRoot(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/messages", "/models"} {
		base = strings.TrimSuffix(base, suffix)
	}
	return base
}

func providerEndpoint(base, suffix string) string {
	return providerRoot(base) + "/" + strings.TrimLeft(suffix, "/")
}
func (c *Client) FetchModels(ctx context.Context) ([]string, error) {
	return c.FetchModelsFor(ctx, "")
}

type ProviderModel struct {
	ID   string `json:"id"`
	Free bool   `json:"free"`
}

func (c *Client) FetchModelCatalogFor(ctx context.Context, providerID string) ([]ProviderModel, error) {
	provider := c.cfg.ProviderRuntime(providerID)
	if provider.BaseURL == "" {
		return nil, fmt.Errorf("provider %s has no base URL", provider.ID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, providerEndpoint(provider.BaseURL, "models"), nil)
	if err != nil {
		return nil, err
	}
	applyProviderAuth(req, provider, false)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s models request failed: %w", provider.Name, err)
	}
	defer resp.Body.Close()
	body, err := readProviderBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &ProviderHTTPError{Status: resp.StatusCode, Provider: provider.Name}
	}
	var raw struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%s returned invalid models JSON: %w", provider.Name, err)
	}
	models := make([]ProviderModel, 0, len(raw.Data))
	seen := make(map[string]struct{}, len(raw.Data))
	for _, item := range raw.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if provider.ID == "opencode-zen" && !zenModelSupported(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, ProviderModel{ID: id, Free: providerModelIsFree(provider.ID, id, item.Pricing.Prompt, item.Pricing.Completion)})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func (c *Client) FetchModelsFor(ctx context.Context, providerID string) ([]string, error) {
	catalog, err := c.FetchModelCatalogFor(ctx, providerID)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(catalog))
	for _, model := range catalog {
		models = append(models, model.ID)
	}
	return models, nil
}

func providerModelIsFree(providerID, modelID, promptPrice, completionPrice string) bool {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "openrouter/free" || strings.HasSuffix(id, ":free") || strings.HasSuffix(id, "-free") {
		return true
	}
	if providerID == "opencode-zen" && id == "big-pickle" {
		return true
	}
	if promptPrice == "" || completionPrice == "" {
		return false
	}
	prompt, promptErr := strconv.ParseFloat(promptPrice, 64)
	completion, completionErr := strconv.ParseFloat(completionPrice, 64)
	return promptErr == nil && completionErr == nil && prompt == 0 && completion == 0
}

func (c *Client) Complete(ctx context.Context, systemPrompt, userPrompt, modelName string, temperature float64) (string, error) {
	provider := c.cfg.ActiveProvider()
	if modelName == "" {
		modelName = provider.Model
	}
	if modelName == "" {
		return "", fmt.Errorf("no model selected for provider %s", provider.Name)
	}
	if provider.KeyRequired && provider.APIKey == "" {
		return "", fmt.Errorf("%s requires an API key", provider.Name)
	}
	if temperature <= 0 {
		temperature = c.cfg.GetTemperature()
	}
	return c.completeForProvider(ctx, provider, systemPrompt, userPrompt, modelName, temperature)
}
func (c *Client) completeForProvider(ctx context.Context, provider config.ActiveProvider, systemPrompt, userPrompt, model string, temperature float64) (string, error) {
	protocol := provider.Protocol
	if protocol == config.ProtocolOpenCodeZen {
		protocol = zenProtocolForModel(model)
	}
	switch protocol {
	case config.ProtocolOpenAIResponses:
		return c.completeOpenAIResponses(ctx, provider, systemPrompt, userPrompt, model)
	case config.ProtocolAnthropic:
		return c.completeAnthropic(ctx, provider, systemPrompt, userPrompt, model, false)
	case config.ProtocolOpenAIChat, "":
		return c.completeOpenAIChat(ctx, provider, systemPrompt, userPrompt, model, temperature)
	case "zen-anthropic":
		return c.completeAnthropic(ctx, provider, systemPrompt, userPrompt, model, true)
	default:
		return "", fmt.Errorf("unsupported provider protocol %q", protocol)
	}
}

func zenProtocolForModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(m, "gpt-"), strings.HasPrefix(m, "grok-"), strings.HasPrefix(m, "muse-"):
		return config.ProtocolOpenAIResponses
	case strings.HasPrefix(m, "claude-"), strings.HasPrefix(m, "qwen3."):
		return "zen-anthropic"
	case strings.HasPrefix(m, "gemini-"):
		return "zen-gemini-unsupported"
	default:
		return config.ProtocolOpenAIChat
	}
}

func zenModelSupported(model string) bool {
	return zenProtocolForModel(model) != "zen-gemini-unsupported"
}
func applyProviderAuth(req *http.Request, provider config.ActiveProvider, forceBearer bool) {
	if provider.APIKey == "" {
		return
	}
	if provider.Protocol == config.ProtocolAnthropic && !forceBearer {
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		return
	}
	req.Header.Set("Authorization", "Bearer "+provider.APIKey)
}

func compactBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 1200 {
		text = text[:1200] + "…"
	}
	return text
}

func retryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if value := strings.TrimSpace(resp.Header.Get("Retry-After")); value != "" {
			if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
				return time.Duration(min(seconds, 60)) * time.Second
			}
			if date, err := http.ParseTime(value); err == nil {
				return min(max(time.Until(date), 0), 60*time.Second)
			}
		}
	}
	return time.Duration(attempt*1500) * time.Millisecond
}
func waitRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) doProviderJSON(ctx context.Context, provider config.ActiveProvider, endpoint string, payload []byte, forceBearer bool) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		applyProviderAuth(req, provider, forceBearer)
		if provider.ID == "openrouter" {
			req.Header.Set("HTTP-Referer", "http://localhost:4890")
			req.Header.Set("X-Title", "NovelClaw")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%s request failed: %w", provider.Name, err)
			if attempt < 3 && waitRetry(ctx, retryDelay(nil, attempt)) == nil {
				continue
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, lastErr
		}
		body, readErr := readProviderBody(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if attempt < 3 && waitRetry(ctx, retryDelay(resp, attempt)) == nil {
				continue
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, lastErr
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, nil
		}

		lastErr = &ProviderHTTPError{Status: resp.StatusCode, Provider: provider.Name}
		if (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < 3 {
			if err := waitRetry(ctx, retryDelay(resp, attempt)); err == nil {
				continue
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, lastErr
	}
	return nil, lastErr
}

func (c *Client) completeOpenAIChat(ctx context.Context, provider config.ActiveProvider, systemPrompt, userPrompt, model string, temperature float64) (string, error) {
	payload := ChatCompletionRequest{
		Model:       model,
		Messages:    []LLMMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}},
		Temperature: temperature,
		MaxTokens:   c.cfg.GetMaxTokens(),
		Stream:      false,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body, err := c.doProviderJSON(ctx, provider, providerEndpoint(provider.BaseURL, "chat/completions"), data, false)
	if err != nil {
		return "", err
	}
	var response ChatCompletionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("%s returned invalid chat JSON: %w", provider.Name, err)
	}
	if response.Error != nil {
		return "", fmt.Errorf("%s API error: %s", provider.Name, response.Error.Message)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("%s returned no completion choices", provider.Name)
	}
	if reason := response.Choices[0].FinishReason; reason != "" && reason != "stop" {
		return "", fmt.Errorf("%s: คำตอบยังไม่สมบูรณ์ (%s) ลองเพิ่มจำนวน token หรือเปลี่ยนโมเดล", provider.Name, reason)
	}
	text := strings.TrimSpace(response.Choices[0].Message.Content)
	if text == "" {
		return "", fmt.Errorf("%s returned an empty completion", provider.Name)
	}
	return text, nil
}

func (c *Client) completeOpenAIResponses(ctx context.Context, provider config.ActiveProvider, systemPrompt, userPrompt, model string) (string, error) {
	payload := map[string]interface{}{
		"model":             model,
		"instructions":      systemPrompt,
		"input":             userPrompt,
		"max_output_tokens": c.cfg.GetMaxTokens(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body, err := c.doProviderJSON(ctx, provider, providerEndpoint(provider.BaseURL, "responses"), data, false)
	if err != nil {
		return "", err
	}
	return parseResponsesText(provider.Name, body)
}
func parseResponsesText(providerName string, body []byte) (string, error) {
	var response struct {
		Status     string `json:"status"`
		OutputText string `json:"output_text"`
		Output     []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("%s returned invalid Responses JSON: %w", providerName, err)
	}
	if response.Error != nil {
		return "", fmt.Errorf("%s API error: %s", providerName, response.Error.Message)
	}
	if response.Status != "" && response.Status != "completed" {
		return "", fmt.Errorf("%s: คำตอบยังไม่สมบูรณ์ (%s)", providerName, response.Status)
	}
	if text := strings.TrimSpace(response.OutputText); text != "" {
		return text, nil
	}
	var out strings.Builder
	for _, item := range response.Output {
		for _, part := range item.Content {
			if (part.Type == "output_text" || part.Type == "text") && part.Text != "" {
				out.WriteString(part.Text)
			}
		}
	}
	if text := strings.TrimSpace(out.String()); text != "" {
		return text, nil
	}
	return "", fmt.Errorf("%s returned an empty response", providerName)
}
func (c *Client) completeAnthropic(ctx context.Context, provider config.ActiveProvider, systemPrompt, userPrompt, model string, forceBearer bool) (string, error) {
	payload := map[string]interface{}{
		"model":      model,
		"max_tokens": c.cfg.GetMaxTokens(),
		"system":     systemPrompt,
		"messages":   []map[string]string{{"role": "user", "content": userPrompt}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body, err := c.doProviderJSON(ctx, provider, providerEndpoint(provider.BaseURL, "messages"), data, forceBearer)
	if err != nil {
		return "", err
	}
	var response struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("%s returned invalid Messages JSON: %w", provider.Name, err)
	}
	if response.Error != nil {
		return "", fmt.Errorf("%s API error: %s", provider.Name, response.Error.Message)
	}
	var out strings.Builder
	if reason := response.StopReason; reason != "" && reason != "end_turn" && reason != "stop_sequence" {
		return "", fmt.Errorf("%s: คำตอบยังไม่สมบูรณ์ (%s)", provider.Name, reason)
	}
	for _, block := range response.Content {
		if block.Type == "text" {
			out.WriteString(block.Text)
		}
	}
	if text := strings.TrimSpace(out.String()); text != "" {
		return text, nil
	}
	return "", fmt.Errorf("%s returned an empty message", provider.Name)
}
