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
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"clientId\":\"item-1\",\"name\":\"Cordless drill\",\"quantity\":1,\"description\":\"Blue drill\",\"manufacturer\":\"Makita\",\"modelNumber\":\"\",\"entityTypeId\":\"type-1\",\"tagIds\":[\"tag-1\",\"made-up\"],\"photoIndexes\":[0],\"moveDisposition\":\"trash\",\"moveDispositionNote\":\"model decided\",\"needsReview\":false}],\"warnings\":[]}"}}]}`))
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
	assert.Equal(t, AICaptureMoveDispositionUndecided, draft.Items[0].MoveDisposition)
	assert.Empty(t, draft.Items[0].MoveDispositionNote)
	assert.Equal(t, "vision-model", got.Model)
	assert.Equal(t, "none", got.ReasoningEffort)
	assert.Equal(t, "json_object", got.ResponseFormat["type"])
	schema, ok := got.ResponseFormat["schema"].(map[string]any)
	require.True(t, ok, "the primary Qwen request must include llama.cpp's schema member")
	assert.Equal(t, "object", schema["type"])
	assert.Equal(t, false, schema["additionalProperties"])
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, properties, "items")
	assert.Contains(t, properties, "warnings")
	require.Len(t, got.Messages, 2)
	userContent, err := json.Marshal(got.Messages[1].Content)
	require.NoError(t, err)
	assert.Contains(t, string(userContent), "data:image/jpeg;base64,")
	assert.Contains(t, string(userContent), "Garage")
}

func TestAICaptureAnalyzeSeparatesImagesForLlamaCpp(t *testing.T) {
	var got chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"clientId\":\"item-1\",\"name\":\"Two views\",\"quantity\":1,\"description\":\"\",\"manufacturer\":\"\",\"modelNumber\":\"\",\"entityTypeId\":\"type-1\",\"tagIds\":[],\"photoIndexes\":[0,1],\"needsReview\":false}],\"warnings\":[]}"}}]}`))
	}))
	defer server.Close()

	svc := NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "qwen-model", MaxPhotos: 4, MaxItems: 5,
	})
	_, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos: []AICapturePhoto{
			{MIMEType: "image/jpeg", Data: []byte("photo-zero")},
			{MIMEType: "image/png", Data: []byte("photo-one")},
		},
		Context: AICaptureContext{EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}}},
	})
	require.NoError(t, err)

	rawContent, err := json.Marshal(got.Messages[1].Content)
	require.NoError(t, err)
	var content []chatContent
	require.NoError(t, json.Unmarshal(rawContent, &content))

	// Regression for llama.cpp #24303: consecutive image_url parts may be merged,
	// which made Qwen describe the next photo twice and omit the photo before it.
	// The exact text/image alternation is intentional and must not be collapsed.
	require.Len(t, content, 5)
	assert.Equal(t, "text", content[0].Type)
	assert.Equal(t, "text", content[1].Type)
	assert.Equal(t, "Photo index 0 follows as a separate image. Do not merge it with adjacent photos.", content[1].Text)
	assert.Equal(t, "image_url", content[2].Type)
	require.NotNil(t, content[2].ImageURL)
	assert.Equal(t, "data:image/jpeg;base64,cGhvdG8temVybw==", content[2].ImageURL.URL)
	assert.Equal(t, "text", content[3].Type)
	assert.Equal(t, "Photo index 1 follows as a separate image. Do not merge it with adjacent photos.", content[3].Text)
	assert.Equal(t, "image_url", content[4].Type)
	require.NotNil(t, content[4].ImageURL)
	assert.Equal(t, "data:image/png;base64,cGhvdG8tb25l", content[4].ImageURL.URL)
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
	assert.Equal(t, "json_object", got.ResponseFormat["type"])
	assert.NotContains(t, got.ResponseFormat, "schema", "secondary providers retain the standard OpenAI request")

	providers := svc.Providers()
	require.Len(t, providers, 2)
	assert.Equal(t, AICaptureProviderDefault, providers[0].ID)
	assert.Equal(t, AICaptureProviderGemini, providers[1].ID)
	assert.True(t, providers[1].Enabled)
}

func TestExtractSingleJSONObject(t *testing.T) {
	const observedRegression = "Based on the user instruction and the visible item, here is the corrected JSON response.\n\n```json\n{\n  \"name\": \"Grey Banana Republic Polo Shirt\"\n}\n```"
	tests := []struct {
		name     string
		response string
		wantJSON string
		wantErr  error
	}{
		{name: "bare object", response: ` {"items":[],"warnings":[]} `, wantJSON: `{"items":[],"warnings":[]}`},
		{name: "JSON fence at start", response: "```json\n{\"items\":[],\"warnings\":[]}\n```", wantJSON: `{"items":[],"warnings":[]}`},
		{name: "observed explanatory prose before fence", response: observedRegression, wantJSON: `{"name":"Grey Banana Republic Polo Shirt"}`},
		{name: "prose after fence", response: "```json\n{\"items\":[]}\n```\nThat is the draft.", wantJSON: `{"items":[]}`},
		{name: "unlabelled fence", response: "```\n{\"items\":[]}\n```", wantJSON: `{"items":[]}`},
		{name: "valid JSON and non-JSON fences", response: "```text\nnot JSON\n```\n```json\n{\"items\":[]}\n```", wantJSON: `{"items":[]}`},
		{name: "two valid objects", response: "```json\n{\"items\":[1]}\n```\n```json\n{\"items\":[2]}\n```", wantErr: errAIAmbiguousJSON},
		{name: "two identical valid objects", response: "```json\n{\"items\":[]}\n```\n```json\n{\"items\":[]}\n```", wantErr: errAIAmbiguousJSON},
		{name: "malformed fenced JSON", response: "```json\n{\"items\":\n```", wantErr: errAINoJSONObject},
		{name: "unfenced JSON in prose", response: `The answer is {"items":[]}.`, wantErr: errAINoJSONObject},
		{name: "array", response: `[{"items":[]}]`, wantErr: errAINoJSONObject},
		{name: "scalar", response: `"item"`, wantErr: errAINoJSONObject},
		{name: "null", response: `null`, wantErr: errAINoJSONObject},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractSingleJSONObject(tc.response)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tc.wantJSON, string(got))
		})
	}
}

func TestAICaptureAnalyzeAcceptsExplanationBeforeFencedDraft(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content := "Based on the user instruction and the visible item, here is the corrected JSON response.\n\n```json\n{\"items\":[{\"clientId\":\"item-1\",\"name\":\"Grey Banana Republic Polo Shirt\",\"quantity\":1,\"description\":\"Grey polo shirt\",\"manufacturer\":\"Banana Republic\",\"modelNumber\":\"\",\"entityTypeId\":\"type-1\",\"tagIds\":[],\"photoIndexes\":[0],\"needsReview\":false}],\"warnings\":[]}\n```"
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		}))
	}))
	defer server.Close()

	svc := NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "qwen-model", MaxPhotos: 4, MaxItems: 5,
	})
	draft, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos:  []AICapturePhoto{{MIMEType: "image/jpeg", Data: []byte("photo")}},
		Context: AICaptureContext{EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}}},
	})
	require.NoError(t, err)
	require.Len(t, draft.Items, 1)
	assert.Equal(t, "Grey Banana Republic Polo Shirt", draft.Items[0].Name)
}

func TestAICaptureAnalyzeRejectsAmbiguousFencedDrafts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		content := "```json\n{\"items\":[{\"name\":\"First\"}]}\n```\n```json\n{\"items\":[{\"name\":\"Second\"}]}\n```"
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		}))
	}))
	defer server.Close()

	svc := NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "qwen-model", MaxPhotos: 4, MaxItems: 5,
	})
	draft, err := svc.Analyze(context.Background(), AICaptureRequest{
		Photos:  []AICapturePhoto{{MIMEType: "image/jpeg", Data: []byte("photo")}},
		Context: AICaptureContext{EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}}},
	})
	assert.ErrorIs(t, err, ErrAIUpstream)
	assert.ErrorIs(t, err, errAIAmbiguousJSON)
	assert.Empty(t, draft.Items)
}

func TestAICaptureAnalyzeReviewedItemUsesEveryView(t *testing.T) {
	var got chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[{\"clientId\":\"kept\",\"name\":\"Cordless drill\",\"quantity\":1,\"entityTypeId\":\"type-1\",\"tagIds\":[],\"photoIndexes\":[0],\"moveDisposition\":\"trash\",\"moveDispositionNote\":\"model changed it\",\"needsReview\":false}],\"warnings\":[]}"}}]}`))
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
		Context: AICaptureContext{EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}}},
		Draft: &AICaptureDraft{Items: []AICaptureItem{{
			ClientID: "kept", Name: "Drill", Quantity: 1,
			MoveDisposition: AICaptureMoveDispositionSell, MoveDispositionNote: "List locally",
		}}},
		SingleItem: true,
	})
	require.NoError(t, err)
	require.Len(t, draft.Items, 1)
	assert.Equal(t, []int{0, 1}, draft.Items[0].PhotoIndexes)
	assert.Equal(t, AICaptureMoveDispositionSell, draft.Items[0].MoveDisposition)
	assert.Equal(t, "List locally", draft.Items[0].MoveDispositionNote)
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
	assert.Contains(t, aiCaptureSystemPrompt, "Every attached photo must be accounted for")
	assert.Contains(t, aiCaptureSystemPrompt, "move disposition")
	assert.NotContains(t, aiCaptureSystemPrompt, `"warrantyExpires"`)
	assert.NotContains(t, aiCaptureSystemPrompt, `"purchasePrice"`)
}

func TestSanitizeAICaptureDraftNormalizesManualMovePlanning(t *testing.T) {
	draft, err := sanitizeAICaptureDraft(AICaptureDraft{Items: []AICaptureItem{{
		Name: "Chair", Quantity: 1, EntityTypeID: "type-1", PhotoIndexes: []int{0},
		MoveDisposition: " SELL ", MoveDispositionNote: "  List locally  ",
	}}}, AICaptureContext{EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}}}, 1, 5)
	require.NoError(t, err)
	assert.Equal(t, AICaptureMoveDispositionSell, draft.Items[0].MoveDisposition)
	assert.Equal(t, "List locally", draft.Items[0].MoveDispositionNote)

	draft.Items[0].MoveDisposition = "model-invented-value"
	draft, err = sanitizeAICaptureDraft(draft, AICaptureContext{
		EntityTypes: []AICaptureOption{{ID: "type-1", Name: "Item"}},
	}, 1, 5)
	require.NoError(t, err)
	assert.Equal(t, AICaptureMoveDispositionUndecided, draft.Items[0].MoveDisposition)
}
