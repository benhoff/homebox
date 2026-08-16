package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sysadminsmedia/homebox/backend/internal/sys/config"
)

func TestAICaptureAnalyzeOpenAICompatibleRequest(t *testing.T) {
	var got chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"clientId\":\"item-1\",\"name\":\"Cordless drill\",\"quantity\":1,\"description\":\"Blue drill\",\"manufacturer\":\"Makita\",\"modelNumber\":\"\",\"entityTypeId\":\"type-1\",\"tagIds\":[\"tag-1\",\"made-up\"],\"photoIndexes\":[0],\"needsReview\":false}],\"warnings\":[]}"}}]}`))
	}))
	defer server.Close()

	svc := NewAICaptureService(config.AIConfig{
		Enabled:         true,
		BaseURL:         server.URL + "/v1",
		APIKey:          "secret",
		Model:           "vision-model",
		ReasoningEffort: "none",
		Timeout:         time.Second,
		MaxPhotos:       4,
		MaxItems:        5,
	})

	draft, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos: []AICapturePhoto{{MIMEType: "image/jpeg", Data: []byte("photo")}},
		Context: AICaptureContext{
			Location:    AICaptureOption{ID: "location-1", Name: "Garage"},
			EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}},
			Tags:        []AICaptureOption{{ID: "tag-1", Name: "Tools"}},
		},
	})
	require.NoError(t, err)
	require.Len(t, draft.Items, 1)
	assert.Equal(t, "Cordless drill", draft.Items[0].Name)
	assert.Equal(t, []string{"tag-1"}, draft.Items[0].TagIDs)
	assert.Equal(t, "vision-model", got.Model)
	assert.Equal(t, "none", got.ReasoningEffort)
	require.Len(t, got.Messages, 2)
	userContent, err := json.Marshal(got.Messages[1].Content)
	require.NoError(t, err)
	assert.Contains(t, string(userContent), "data:image/jpeg;base64,")
	assert.Contains(t, string(userContent), "Garage")
}

func TestAICaptureAnalyzeWithGeminiProvider(t *testing.T) {
	var got chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1beta/openai/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer gemini-secret", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"clientId\":\"item-1\",\"name\":\"Drill\",\"quantity\":1,\"entityTypeId\":\"type-1\",\"tagIds\":[],\"photoIndexes\":[0],\"needsReview\":false}],\"warnings\":[]}"}}]}`))
	}))
	defer server.Close()

	svc := NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: "http://primary.invalid/v1", Model: "qwen-model", MaxPhotos: 4, MaxItems: 5,
		Gemini: config.AIProviderConfig{
			Enabled: true, Name: "Gemini", BaseURL: server.URL + "/v1beta/openai",
			APIKey: "gemini-secret", Model: "gemini-model",
		},
	})
	draft, err := svc.AnalyzeWithProvider(context.Background(), AICaptureProviderGemini, AICaptureRequest{
		Photos:  []AICapturePhoto{{MIMEType: "image/jpeg", Data: []byte("photo")}},
		Context: AICaptureContext{EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}}},
	})
	require.NoError(t, err)
	require.Len(t, draft.Items, 1)
	assert.Equal(t, "gemini-model", got.Model)
	assert.Equal(t, "Drill", draft.Items[0].Name)

	providers := svc.Providers()
	require.Len(t, providers, 2)
	assert.Equal(t, AICaptureProviderDefault, providers[0].ID)
	assert.Equal(t, AICaptureProviderGemini, providers[1].ID)
	assert.True(t, providers[1].Enabled)
}

func TestAICaptureAnalyzeReviewedItemUsesEveryView(t *testing.T) {
	var got chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"clientId\":\"changed\",\"name\":\"Cordless drill\",\"quantity\":1,\"entityTypeId\":\"type-1\",\"tagIds\":[],\"photoIndexes\":[0],\"needsReview\":false}],\"warnings\":[]}"}}]}`))
	}))
	defer server.Close()

	svc := NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "qwen-model", MaxPhotos: 4, MaxItems: 5,
	})
	draft, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos: []AICapturePhoto{
			{MIMEType: "image/jpeg", Data: []byte("front")},
			{MIMEType: "image/jpeg", Data: []byte("label")},
		},
		Context:    AICaptureContext{EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}}},
		Draft:      &AICaptureDraft{Items: []AICaptureItem{{ClientID: "kept", Name: "Drill", Quantity: 1}}},
		SingleItem: true,
	})
	require.NoError(t, err)
	require.Len(t, draft.Items, 1)
	assert.Equal(t, []int{0, 1}, draft.Items[0].PhotoIndexes)
	userContent, err := json.Marshal(got.Messages[1].Content)
	require.NoError(t, err)
	assert.Contains(t, string(userContent), "REVIEWED ITEM")
	assert.Contains(t, string(userContent), "do not treat the number of photos as quantity")
}

func TestAICaptureAnalyzeRejectsUnavailableProvider(t *testing.T) {
	svc := NewAICaptureService(config.AIConfig{Enabled: true})
	_, err := svc.AnalyzeWithProvider(context.Background(), AICaptureProviderGemini, AICaptureRequest{})
	assert.ErrorIs(t, err, ErrAIProvider)
}

func TestAICaptureAnalyzeSanitizesUnsafeDraft(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		content := "```json\n{\"items\":[{\"name\":\"\",\"quantity\":0,\"entityTypeId\":\"invalid\",\"tagIds\":[\"invalid\"],\"photoIndexes\":[99]}]}\n```"
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()

	svc := NewAICaptureService(config.AIConfig{
		Enabled:   true,
		BaseURL:   server.URL,
		Model:     "vision-model",
		MaxPhotos: 2,
		MaxItems:  5,
	})
	draft, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos:  []AICapturePhoto{{MIMEType: "image/png", Data: []byte("photo")}},
		Context: AICaptureContext{EntityTypes: []AICaptureOption{{ID: "default-type", Name: "Item"}}},
	})
	require.NoError(t, err)
	item := draft.Items[0]
	assert.Equal(t, "Unidentified item 1", item.Name)
	assert.Equal(t, float64(1), item.Quantity)
	assert.Equal(t, "default-type", item.EntityTypeID)
	assert.Equal(t, []int{0}, item.PhotoIndexes)
	assert.True(t, item.NeedsReview)
	assert.NotEmpty(t, item.ReviewReason)
}

func TestAICaptureAnalyzeEnforcesSameItemGroup(t *testing.T) {
	const captureGroupID = "73586682-83c3-43f7-8764-6470e39bd5b5"
	var got chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"clientId\":\"item-1\",\"name\":\"Cordless drill\",\"quantity\":1,\"entityTypeId\":\"type-1\",\"tagIds\":[],\"photoIndexes\":[0],\"captureGroupId\":\"73586682-83c3-43f7-8764-6470e39bd5b5\",\"needsReview\":false}],\"warnings\":[]}"}}]}`))
	}))
	defer server.Close()

	svc := NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "vision-model", MaxPhotos: 4, MaxItems: 5,
	})
	draft, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos: []AICapturePhoto{
			{MIMEType: "image/jpeg", Data: []byte("front")},
			{MIMEType: "image/jpeg", Data: []byte("label")},
		},
		Context:        AICaptureContext{EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}}},
		CaptureGroupID: captureGroupID,
	})
	require.NoError(t, err)
	require.Len(t, draft.Items, 1)
	assert.Equal(t, captureGroupID, draft.Items[0].CaptureGroupID)
	assert.Equal(t, []int{0, 1}, draft.Items[0].PhotoIndexes)
	userContent, err := json.Marshal(got.Messages[1].Content)
	require.NoError(t, err)
	assert.Contains(t, string(userContent), "different view of one physical inventory item")
	assert.Contains(t, string(userContent), captureGroupID)
}

func TestAICaptureAnalyzeRejectsDuplicateSameItemResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"name\":\"Drill\",\"quantity\":1},{\"name\":\"Drill\",\"quantity\":1}],\"warnings\":[]}"}}]}`))
	}))
	defer server.Close()

	svc := NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "vision-model", MaxPhotos: 4, MaxItems: 5,
	})
	_, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos:         []AICapturePhoto{{MIMEType: "image/jpeg", Data: []byte("front")}},
		Context:        AICaptureContext{EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}}},
		CaptureGroupID: "73586682-83c3-43f7-8764-6470e39bd5b5",
	})
	assert.ErrorIs(t, err, ErrAIGroupContract)
	assert.ErrorIs(t, err, ErrAIUpstream)
}

func TestAICaptureAnalyzeDisabled(t *testing.T) {
	svc := NewAICaptureService(config.AIConfig{})
	_, err := svc.Analyze(context.Background(), AICaptureRequest{})
	assert.ErrorIs(t, err, ErrAIDisabled)
}

func TestAICaptureAnalyzeClassifiesTemporaryProviderFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "starting", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	svc := NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "vision-model", MaxPhotos: 4, MaxItems: 5,
	})
	_, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos: []AICapturePhoto{{MIMEType: "image/jpeg", Data: []byte("photo")}},
	})
	assert.ErrorIs(t, err, ErrAIUpstream)
	assert.ErrorIs(t, err, ErrAIRetryable)
}

func TestAICaptureAnalyzeDoesNotRetryPermanentProviderFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()
	svc := NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "vision-model", MaxPhotos: 4, MaxItems: 5,
	})
	_, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos: []AICapturePhoto{{MIMEType: "image/jpeg", Data: []byte("photo")}},
	})
	assert.ErrorIs(t, err, ErrAIUpstream)
	assert.NotErrorIs(t, err, ErrAIRetryable)
}

func TestAICapturePromptLimitsAIToVisualMetadata(t *testing.T) {
	assert.Contains(t, aiCaptureSystemPrompt, "Only identify the item name")
	assert.Contains(t, aiCaptureSystemPrompt, "Never infer or return serial numbers")
	assert.NotContains(t, aiCaptureSystemPrompt, `"warrantyExpires"`)
	assert.NotContains(t, aiCaptureSystemPrompt, `"purchasePrice"`)
}
