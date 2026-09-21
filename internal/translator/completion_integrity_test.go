package translator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"novelclaw/internal/config"
	"strings"
	"testing"
	"time"
)

func TestRetryAfterSupportsDateAndCapsWait(t *testing.T) {
	for _, value := range []string{"120", time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)} {
		response := &http.Response{Header: http.Header{"Retry-After": []string{value}}}
		if delay := retryDelay(response, 1); delay != 60*time.Second {
			t.Fatalf("delay=%v", delay)
		}
	}
}

func TestRejectIncompleteProviderOutput(t *testing.T) {
	for _, tc := range []struct{ name, protocol, body string }{
		{"chat", config.ProtocolOpenAIChat, `{"choices":[{"finish_reason":"length","message":{"content":"partial translation"}}]}`},
		{"responses", config.ProtocolOpenAIResponses, `{"status":"incomplete","output_text":"partial translation"}`},
		{"anthropic", config.ProtocolAnthropic, `{"stop_reason":"max_tokens","content":[{"type":"text","text":"partial translation"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(tc.body)) }))
			defer server.Close()
			cfg := testProviderConfig(t, "custom", server.URL, "", "model", tc.protocol)
			out, err := NewClient(cfg).Complete(context.Background(), "system", "source", "model", 0.3)
			if err == nil || out != "" {
				t.Fatalf("incomplete output accepted: %q %v", out, err)
			}
		})
	}
}

func TestAuthenticationFailureDoesNotRetryFallbackOrExposeBody(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(401)
		w.Write([]byte("echoed-private-credential"))
	}))
	defer server.Close()
	cfg := testProviderConfig(t, "custom", server.URL, "", "first", config.ProtocolOpenAIChat)
	_, _, err := NewClient(cfg).CompleteWithFallback(context.Background(), "sys", "src", []string{"first", "second"}, 0.3)
	if calls != 1 || err == nil || strings.Contains(err.Error(), "echoed-private-credential") {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestModelForbiddenStillTriesConfiguredFallback(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(403)
			return
		}
		w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"คำแปลครบถ้วน"}}]}`))
	}))
	defer server.Close()
	cfg := testProviderConfig(t, "custom", server.URL, "", "first", config.ProtocolOpenAIChat)
	out, used, err := NewClient(cfg).CompleteWithFallback(context.Background(), "sys", "src", []string{"first", "second"}, 0.3)
	if err != nil || used != "second" || calls != 2 || out == "" {
		t.Fatalf("fallback failed: calls=%d model=%s err=%v", calls, used, err)
	}
}

func TestProviderCancellationDuringRateLimitWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
		cancel()
	}))
	defer server.Close()
	cfg := testProviderConfig(t, "custom", server.URL, "", "model", config.ProtocolOpenAIChat)
	_, err := NewClient(cfg).Complete(ctx, "sys", "src", "model", 0.3)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestProviderRejectsOversizedBody(t *testing.T) {
	_, err := readProviderBody(strings.NewReader(strings.Repeat("x", maxProviderResponse+1)))
	if err == nil {
		t.Fatal("oversized body accepted")
	}
}

func TestUnfinishedReasoningNeverBecomesTranslation(t *testing.T) {
	title, paragraphs := ParseTranslationOutput("<think>internal unfinished reasoning")
	if title != "" || len(paragraphs) != 0 {
		t.Fatal("reasoning returned as novel text")
	}
}
