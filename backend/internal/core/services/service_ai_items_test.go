package services

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/attachment"
	"github.com/sysadminsmedia/homebox/backend/internal/data/repo"
	"github.com/sysadminsmedia/homebox/backend/internal/sys/config"
)

func TestAIItemServiceReanalyzeReturnsUnpersistedPreview(t *testing.T) {
	locationType, err := tRepos.EntityTypes.GetDefault(tCtx, tGroup.ID, true)
	require.NoError(t, err)
	itemType, err := tRepos.EntityTypes.GetDefault(tCtx, tGroup.ID, false)
	require.NoError(t, err)
	location, err := tRepos.Entities.Create(tCtx, tGroup.ID, repo.EntityCreate{
		Name: "AI item preview location", EntityTypeID: locationType.ID,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tRepos.Entities.Delete(tCtx, location.ID) })
	item, err := tRepos.Entities.Create(tCtx, tGroup.ID, repo.EntityCreate{
		Name: "Old item name", Description: "Current description", Quantity: 1,
		ParentID: location.ID, EntityTypeID: itemType.ID,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tRepos.Entities.Delete(tCtx, item.ID) })

	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	require.NoError(t, err)
	withPhoto, err := tSvc.Entities.AttachmentAdd(tCtx, item.ID, "item.png", attachment.TypePhoto, true, bytes.NewReader(png))
	require.NoError(t, err)
	require.Len(t, withPhoto.Attachments, 1)

	var got chatCompletionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		content := fmt.Sprintf("I reviewed the existing item.\n\n```json\n{\"items\":[{\"clientId\":%q,\"name\":\"Gray polo\",\"quantity\":1,\"description\":\"Gray short-sleeve polo shirt\",\"manufacturer\":\"Banana Republic\",\"modelNumber\":\"\",\"entityTypeId\":%q,\"tagIds\":[],\"photoIndexes\":[0],\"needsReview\":false}],\"warnings\":[]}\n```", item.ID.String(), itemType.ID.String())
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		}))
	}))
	defer server.Close()

	svc := NewAIItemService(tRepos, NewAICaptureService(config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "qwen-model", Timeout: time.Second, MaxPhotos: 8, MaxItems: 25,
	}))
	preview, err := svc.Reanalyze(tCtx, item.ID, "")
	require.NoError(t, err)
	assert.Equal(t, AICaptureProviderDefault, preview.Provider)
	assert.Equal(t, 1, preview.PhotoCount)
	assert.Equal(t, "Gray polo", preview.Item.Name)
	assert.Equal(t, "Banana Republic", preview.Item.Manufacturer)
	assert.Equal(t, []string{withPhoto.Attachments[0].ID.String()}, preview.Item.PhotoIDs)
	assert.Empty(t, preview.Warnings)

	stored, err := tRepos.Entities.GetOneByGroup(tCtx, tGroup.ID, item.ID)
	require.NoError(t, err)
	assert.Equal(t, "Old item name", stored.Name, "preview must not persist Qwen's suggestion")
	requestJSON, err := json.Marshal(got.Messages[1].Content)
	require.NoError(t, err)
	assert.Contains(t, string(requestJSON), "REVIEWED ITEM")
	assert.NotContains(t, string(requestJSON), "Old item name")
	assert.NotContains(t, string(requestJSON), "Current description")
	assert.NotContains(t, string(requestJSON), "This is a correction request")
	assert.NotContains(t, string(requestJSON), "User instruction")
	assert.Contains(t, string(requestJSON), "data:image/png;base64,")
}
