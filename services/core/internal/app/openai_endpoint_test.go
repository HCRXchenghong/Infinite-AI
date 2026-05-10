package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/seron-cheng/infinite-ai/services/shared/store"
)

func TestOpenAIEndpointCandidatesAddsV1Fallback(t *testing.T) {
	got := openAIEndpointCandidates("https://compatible.example.com", "/models")
	want := []string{
		"https://compatible.example.com/v1/models",
		"https://compatible.example.com/models",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("openAIEndpointCandidates() = %#v, want %#v", got, want)
	}
}

func TestOpenAIEndpointCandidatesDoesNotDuplicateV1(t *testing.T) {
	got := openAIEndpointCandidates("https://compatible.example.com/v1/", "/chat/completions")
	want := []string{"https://compatible.example.com/v1/chat/completions"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("openAIEndpointCandidates() = %#v, want %#v", got, want)
	}
}

func TestShouldTryNextOpenAIEndpointCandidate(t *testing.T) {
	if !shouldTryNextOpenAIEndpointCandidate(http.StatusNotFound, `{"error":{"message":"Invalid URL (GET /models)"}}`) {
		t.Fatalf("expected path-style error to try next candidate")
	}
	if shouldTryNextOpenAIEndpointCandidate(http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`) {
		t.Fatalf("expected auth error to stop on current candidate")
	}
}

func TestShouldTryOpenAIResponsesFallback(t *testing.T) {
	if !shouldTryOpenAIResponsesFallback(http.StatusInternalServerError, `{"error":{"message":"input is required"}}`) {
		t.Fatalf("expected responses fallback")
	}
	if shouldTryOpenAIResponsesFallback(http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`) {
		t.Fatalf("expected no responses fallback on auth error")
	}
}

func TestExtractOpenAIResponsesText(t *testing.T) {
	payload := openAIResponsesResponse{
		Output: []openAIResponsesOutputItem{
			{
				Type: "message",
				Content: []openAIResponsesContent{
					{Type: "output_text", Text: "OK"},
				},
			},
		},
	}
	if got := extractOpenAIResponsesText(payload); got != "OK" {
		t.Fatalf("extractOpenAIResponsesText() = %q, want %q", got, "OK")
	}
}

func TestOpenAIEndpointCandidatesWithPreferred(t *testing.T) {
	got := openAIEndpointCandidatesWithPreferred("https://compatible.example.com", "/responses", "https://compatible.example.com/v1/responses")
	want := []string{
		"https://compatible.example.com/v1/responses",
		"https://compatible.example.com/responses",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("openAIEndpointCandidatesWithPreferred() = %#v, want %#v", got, want)
	}
}

func TestOpenAIImageEndpointOrderPrefersOfficialAndDeduplicates(t *testing.T) {
	endpoints := []store.ModelEndpoint{
		{BaseURL: "https://compatible-a.example.com/v1", Secret: "same", Active: true},
		{BaseURL: "https://api.openai.com/v1", Secret: "official", Active: true},
		{BaseURL: "https://compatible-a.example.com/v1/", Secret: "same", Active: true},
		{BaseURL: "https://compatible-b.example.com", Secret: "fallback", Active: true},
		{BaseURL: "https://api.openai.com/v1", Secret: "official", Active: false},
	}

	got := openAIImageEndpointOrder(endpoints)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3: %#v", len(got), got)
	}
	if got[0].BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("expected official endpoint first, got %#v", got[0])
	}
	if got[1].BaseURL != "https://compatible-a.example.com/v1" || got[2].BaseURL != "https://compatible-b.example.com" {
		t.Fatalf("unexpected fallback order: %#v", got)
	}
}

func TestAdaptOpenAIImageEditRequestForEndpointLimitsCompatibleMultiImage(t *testing.T) {
	request := imageGenerationRequest{
		ReferenceImages: []imageReferenceInput{
			{FileName: "one.png"},
			{FileName: "two.png"},
		},
	}

	compatible := adaptOpenAIImageEditRequestForEndpoint(store.ModelEndpoint{BaseURL: "https://compatible.example.com/v1"}, request)
	if len(compatible.ReferenceImages) != 1 {
		t.Fatalf("compatible reference count = %d, want 1", len(compatible.ReferenceImages))
	}

	official := adaptOpenAIImageEditRequestForEndpoint(store.ModelEndpoint{BaseURL: "https://api.openai.com/v1"}, request)
	if len(official.ReferenceImages) != 2 {
		t.Fatalf("official reference count = %d, want 2", len(official.ReferenceImages))
	}
}

func TestOpenAIEndpointAdaptationCache(t *testing.T) {
	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.4"}
	endpoint := store.ModelEndpoint{ID: "endpoint-1", BaseURL: "https://compatible.example.com"}
	server.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
		Kind: openAIAdapterResponses,
		URL:  "https://compatible.example.com/v1/responses",
	})

	got, ok := server.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat)
	if !ok {
		t.Fatalf("expected cached adaptation")
	}
	if got.Kind != openAIAdapterResponses || got.URL != "https://compatible.example.com/v1/responses" {
		t.Fatalf("cached adaptation = %#v", got)
	}
}

func TestOpenAIEndpointAdaptationCacheSurvivesEndpointIDChanges(t *testing.T) {
	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.4"}
	draftEndpoint := store.ModelEndpoint{BaseURL: "https://compatible.example.com"}
	savedEndpoint := store.ModelEndpoint{ID: "new-db-id", BaseURL: "https://compatible.example.com"}
	server.rememberOpenAIEndpointAdaptation(route, draftEndpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
		Kind: openAIAdapterResponses,
		URL:  "https://compatible.example.com/v1/responses",
	})

	got, ok := server.getOpenAIEndpointAdaptation(route, savedEndpoint, openAIAdapterOperationChat)
	if !ok {
		t.Fatalf("expected cached adaptation after endpoint ID changes")
	}
	if got.Kind != openAIAdapterResponses || got.URL != "https://compatible.example.com/v1/responses" {
		t.Fatalf("cached adaptation = %#v", got)
	}
}

func TestOpenAIResponsesFallbackUpdatesCacheAfterEmptyPreferred(t *testing.T) {
	requestCounts := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCounts[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/responses" {
			_, _ = w.Write([]byte(`{"object":"response","status":"completed","output":[]}`))
			return
		}
		if r.URL.Path == "/v1/responses" {
			_, _ = w.Write([]byte(`{"object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.4"}
	endpoint := store.ModelEndpoint{ID: "endpoint-1", BaseURL: upstream.URL, Secret: "test-key"}
	server.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
		Kind: openAIAdapterResponses,
		URL:  upstream.URL + "/responses",
	})

	got, err := server.callOpenAIResponsesCompatible(t.Context(), route, endpoint, []aiMessage{{Role: "user", Content: "Reply OK only."}})
	if err != nil {
		t.Fatalf("callOpenAIResponsesCompatible() error = %v", err)
	}
	if got != "OK" {
		t.Fatalf("callOpenAIResponsesCompatible() = %q, want OK", got)
	}
	if requestCounts["/responses"] != 1 || requestCounts["/v1/responses"] != 1 {
		t.Fatalf("requestCounts = %#v", requestCounts)
	}
	cached, ok := server.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat)
	if !ok || cached.URL != upstream.URL+"/v1/responses" {
		t.Fatalf("expected cache to move to /v1/responses, got ok=%v cached=%#v", ok, cached)
	}
}

func TestOpenAICompatibleCachedChatCompletionsFallsBackDirectlyToResponses(t *testing.T) {
	requestCounts := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCounts[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat/completions":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"input is required"}}`))
		case "/responses":
			_, _ = w.Write([]byte(`{"object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`))
		case "/v1/chat/completions":
			t.Fatalf("should not retry alternate chat completions endpoint after responses fallback signal")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.4"}
	endpoint := store.ModelEndpoint{ID: "endpoint-1", BaseURL: upstream.URL, Secret: "test-key"}
	server.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
		Kind: openAIAdapterChatCompletions,
		URL:  upstream.URL + "/chat/completions",
	})

	got, err := server.callOpenAICompatible(t.Context(), route, endpoint, []aiMessage{{Role: "user", Content: "Reply OK only."}})
	if err != nil {
		t.Fatalf("callOpenAICompatible() error = %v", err)
	}
	if got != "OK" {
		t.Fatalf("callOpenAICompatible() = %q, want OK", got)
	}
	if requestCounts["/chat/completions"] != 1 || requestCounts["/responses"] != 1 {
		t.Fatalf("requestCounts = %#v", requestCounts)
	}
	cached, ok := server.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat)
	if !ok || cached.Kind != openAIAdapterResponses || cached.URL != upstream.URL+"/responses" {
		t.Fatalf("expected cache to move to responses, got ok=%v cached=%#v", ok, cached)
	}
}

func TestOpenAICompatibleGatewayFailureFallsBackToResponses(t *testing.T) {
	requestCounts := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCounts[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"upstream gateway failed"}}`))
		case "/v1/responses":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode responses payload: %v", err)
			}
			if _, ok := payload["input"]; !ok {
				t.Fatalf("responses payload missing input: %#v", payload)
			}
			_, _ = w.Write([]byte(`{"object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.4"}
	endpoint := store.ModelEndpoint{ID: "endpoint-1", BaseURL: upstream.URL, Secret: "test-key"}

	got, err := server.callOpenAICompatible(t.Context(), route, endpoint, []aiMessage{{Role: "user", Content: "Reply OK only."}})
	if err != nil {
		t.Fatalf("callOpenAICompatible() error = %v", err)
	}
	if got != "OK" {
		t.Fatalf("callOpenAICompatible() = %q, want OK", got)
	}
	if requestCounts["/v1/chat/completions"] != 1 || requestCounts["/v1/responses"] != 1 {
		t.Fatalf("requestCounts = %#v", requestCounts)
	}
	cached, ok := server.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat)
	if !ok || cached.Kind != openAIAdapterResponses || cached.URL != upstream.URL+"/v1/responses" {
		t.Fatalf("expected cache to move to responses, got ok=%v cached=%#v", ok, cached)
	}
}

func TestOpenAICompatibleGatewayFailureKeepsOriginalWhenResponsesUnsupported(t *testing.T) {
	requestCounts := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCounts[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = w.Write([]byte(`{"error":{"message":"gateway timeout"}}`))
		case "/v1/responses", "/responses":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"Invalid URL (POST /v1/responses)"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.4"}
	endpoint := store.ModelEndpoint{ID: "endpoint-1", BaseURL: upstream.URL, Secret: "test-key"}

	_, err := server.callOpenAICompatible(t.Context(), route, endpoint, []aiMessage{{Role: "user", Content: "Reply OK only."}})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "gateway timeout") {
		t.Fatalf("expected original chat error, got %v", err)
	}
	if requestCounts["/v1/chat/completions"] != 1 || requestCounts["/v1/responses"] != 1 || requestCounts["/responses"] != 1 {
		t.Fatalf("requestCounts = %#v", requestCounts)
	}
}

func TestOpenAICompatiblePrefersStreamingAndCaches(t *testing.T) {
	nonStreamCalls := 0
	streamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["stream"] == true {
			streamCalls++
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		nonStreamCalls++
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("<html><body><center><h1>504 Gateway Time-out</h1></center><hr><center>alb</center></body></html>"))
	}))
	defer upstream.Close()

	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.4"}
	endpoint := store.ModelEndpoint{ID: "endpoint-1", BaseURL: upstream.URL, Secret: "test-key"}

	got, err := server.callOpenAICompatible(t.Context(), route, endpoint, []aiMessage{{Role: "user", Content: "Reply hello."}})
	if err != nil {
		t.Fatalf("callOpenAICompatible() error = %v", err)
	}
	if got != "Hello" {
		t.Fatalf("callOpenAICompatible() = %q, want Hello", got)
	}
	if nonStreamCalls != 0 || streamCalls != 1 {
		t.Fatalf("calls after first request: nonStream=%d stream=%d", nonStreamCalls, streamCalls)
	}
	cached, ok := server.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat)
	if !ok || cached.Kind != openAIAdapterChatCompletionsStream {
		t.Fatalf("expected streaming adaptation cache, got ok=%v cached=%#v", ok, cached)
	}

	got, err = server.callOpenAICompatible(t.Context(), route, endpoint, []aiMessage{{Role: "user", Content: "Reply hello again."}})
	if err != nil {
		t.Fatalf("second callOpenAICompatible() error = %v", err)
	}
	if got != "Hello" {
		t.Fatalf("second callOpenAICompatible() = %q, want Hello", got)
	}
	if nonStreamCalls != 0 || streamCalls != 2 {
		t.Fatalf("cached stream was not used: nonStream=%d stream=%d", nonStreamCalls, streamCalls)
	}
}

func TestOpenAICompatibleCachedChatGatewayTimeoutFallsBackToStreaming(t *testing.T) {
	nonStreamCalls := 0
	streamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["stream"] == true {
			streamCalls++
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		nonStreamCalls++
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("<html><body><h1>504 Gateway Time-out</h1><center>alb</center></body></html>"))
	}))
	defer upstream.Close()

	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.4"}
	endpoint := store.ModelEndpoint{ID: "endpoint-1", BaseURL: upstream.URL, Secret: "test-key"}
	server.rememberOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat, openAIEndpointAdaptation{
		Kind: openAIAdapterChatCompletions,
		URL:  upstream.URL + "/chat/completions",
	})

	got, err := server.callOpenAICompatible(t.Context(), route, endpoint, []aiMessage{{Role: "user", Content: "Reply OK only."}})
	if err != nil {
		t.Fatalf("callOpenAICompatible() error = %v", err)
	}
	if got != "OK" {
		t.Fatalf("callOpenAICompatible() = %q, want OK", got)
	}
	if nonStreamCalls != 1 || streamCalls != 1 {
		t.Fatalf("expected cached non-stream then streaming fallback, nonStream=%d stream=%d", nonStreamCalls, streamCalls)
	}
	cached, ok := server.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat)
	if !ok || cached.Kind != openAIAdapterChatCompletionsStream {
		t.Fatalf("expected streaming adaptation cache, got ok=%v cached=%#v", ok, cached)
	}
}

func TestOpenAICompatibleStreamingInputRequiredFallsBackToResponses(t *testing.T) {
	requestCounts := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCounts[r.URL.Path]++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"input is required"}}`))
		case "/v1/responses":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode responses payload: %v", err)
			}
			if _, ok := payload["input"]; !ok {
				t.Fatalf("responses payload missing input: %#v", payload)
			}
			_, _ = w.Write([]byte(`{"object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	server := &Server{}
	route := &store.ModelRoute{Slug: "test", Protocol: "openai", UpstreamModel: "gpt-5.4"}
	endpoint := store.ModelEndpoint{ID: "endpoint-1", BaseURL: upstream.URL, Secret: "test-key"}

	got, err := server.callOpenAICompatible(t.Context(), route, endpoint, []aiMessage{{Role: "user", Content: "Reply OK only."}})
	if err != nil {
		t.Fatalf("callOpenAICompatible() error = %v", err)
	}
	if got != "OK" {
		t.Fatalf("callOpenAICompatible() = %q, want OK", got)
	}
	if requestCounts["/v1/chat/completions"] != 1 || requestCounts["/v1/responses"] != 1 {
		t.Fatalf("requestCounts = %#v", requestCounts)
	}
	cached, ok := server.getOpenAIEndpointAdaptation(route, endpoint, openAIAdapterOperationChat)
	if !ok || cached.Kind != openAIAdapterResponses || cached.URL != upstream.URL+"/v1/responses" {
		t.Fatalf("expected responses adaptation cache, got ok=%v cached=%#v", ok, cached)
	}
}
