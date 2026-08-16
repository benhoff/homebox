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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/aicapturesession"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/attachment"
	"github.com/sysadminsmedia/homebox/backend/internal/data/repo"
	"github.com/sysadminsmedia/homebox/backend/internal/sys/config"
)

func TestAIRetryDelayUsesBoundedBackoff(t *testing.T) {
	assert.Equal(t, 5*time.Second, aiRetryDelay(1))
	assert.Equal(t, 15*time.Second, aiRetryDelay(2))
	assert.Equal(t, 5*time.Minute, aiRetryDelay(6))
	assert.Equal(t, 5*time.Minute, aiRetryDelay(100))
}

func TestRunNextReanalysisWaitsForProviderAndResumesBatch(t *testing.T) {
	ctx := tCtx
	locationType, err := tRepos.EntityTypes.GetDefault(ctx, tGroup.ID, true)
	require.NoError(t, err)
	itemType, err := tRepos.EntityTypes.GetDefault(ctx, tGroup.ID, false)
	require.NoError(t, err)
	location, err := tRepos.Entities.Create(ctx, tGroup.ID, repo.EntityCreate{
		Name: "Reanalysis queue test", EntityTypeID: locationType.ID,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tRepos.Entities.Delete(ctx, location.ID) })

	available := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !available {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":"{\"items\":[{\"clientId\":\"item-1\",\"name\":\"Recovered drill\",\"quantity\":1,\"entityTypeId\":\"%s\",\"tagIds\":[],\"photoIndexes\":[0],\"needsReview\":false}],\"warnings\":[]}"}}]}`, itemType.ID)
	}))
	defer server.Close()
	cfg := config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "vision-model", Timeout: time.Second, MaxPhotos: 4, MaxItems: 5,
	}
	svc := NewAICaptureSessionService(tRepos, NewAICaptureService(cfg), tSvc.Entities, cfg)

	session, err := tRepos.AICaptureSessions.Create(ctx, tGroup.ID, tUser.ID, location.ID, location.Name, 5)
	require.NoError(t, err)
	t.Cleanup(func() {
		stored, getErr := tRepos.AICaptureSessions.Get(ctx, tGroup.ID, tUser.ID, session.ID)
		if getErr == nil {
			_ = tRepos.AICaptureSessions.PurgeSessionPhotos(ctx, stored)
		}
		_, _ = tRepos.AICaptureSessions.Delete(ctx, tGroup.ID, tUser.ID, session.ID)
	})
	photoID, path, hash := tRepos.AICaptureSessions.NewPhotoStorage([]byte("photo"), tGroup.ID, session.ID)
	require.NoError(t, tRepos.AICaptureSessions.WriteBlob(ctx, path, "image/jpeg", []byte("photo")))
	_, _, err = tRepos.AICaptureSessions.CreatePhoto(
		ctx, tGroup.ID, tUser.ID, session.ID, photoID, uuid.New(), 0, 4, 4, nil,
		"photo.jpg", "image/jpeg", path, 5, hash,
	)
	require.NoError(t, err)
	require.NoError(t, tRepos.AICaptureSessions.Finish(ctx, tGroup.ID, tUser.ID, session.ID, 1, 1))
	claimed, found, err := tRepos.AICaptureSessions.ClaimQueued(ctx, 3*time.Minute)
	require.NoError(t, err)
	require.True(t, found)
	draft := fmt.Sprintf(`{"items":[{"clientId":"item-1","name":"Drill","quantity":1,"entityTypeId":"%s","tagIds":[],"photoIds":["%s"],"moveDisposition":"sell","moveDispositionNote":"List locally","needsReview":false}],"warnings":[]}`, itemType.ID, photoID)
	require.NoError(t, tRepos.AICaptureSessions.SetAnalysisReady(ctx, claimed.ID, draft))

	queued, err := svc.QueueReanalysis(ctx, session.ID, 1, []string{"item-1"}, AICaptureProviderDefault, "")
	require.NoError(t, err)
	require.NotNil(t, queued.Reanalysis)
	assert.Equal(t, repo.AICaptureReanalysisQueued, queued.Reanalysis.Status)

	require.NoError(t, svc.RunNextReanalysis(ctx))
	waiting, err := svc.Get(ctx, session.ID)
	require.NoError(t, err)
	require.NotNil(t, waiting.Reanalysis)
	assert.Equal(t, repo.AICaptureReanalysisWaiting, waiting.Reanalysis.Status)
	require.NotNil(t, waiting.Reanalysis.NextAttemptAt)

	available = true
	_, err = tClient.AICaptureSession.UpdateOneID(session.ID).
		SetReanalysisNextAttemptAt(time.Now().Add(-time.Second)).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, svc.RunNextReanalysis(ctx))
	completed, err := svc.Get(ctx, session.ID)
	require.NoError(t, err)
	require.NotNil(t, completed.Reanalysis)
	assert.Equal(t, repo.AICaptureReanalysisCompleted, completed.Reanalysis.Status)
	assert.Equal(t, 1, completed.Reanalysis.Completed)
	assert.Equal(t, "Recovered drill", completed.Reanalysis.Suggestions["item-1"].Item.Name)
	assert.Equal(t, AICaptureMoveDispositionSell, completed.Reanalysis.Suggestions["item-1"].Item.MoveDisposition)
	assert.Equal(t, "List locally", completed.Reanalysis.Suggestions["item-1"].Item.MoveDispositionNote)
}

func TestAICaptureMoveFieldsPersistDispositionAndOptionalNotes(t *testing.T) {
	fields := aiCaptureMoveFields(AICaptureItem{
		MoveDisposition:     AICaptureMoveDispositionGiveAway,
		MoveDispositionNote: "  Jamie will collect it  ",
	})
	require.Len(t, fields, 2)
	assert.Equal(t, "Move disposition", fields[0].Name)
	assert.Equal(t, "Give away", fields[0].TextValue)
	assert.Equal(t, "Move planning notes", fields[1].Name)
	assert.Equal(t, "Jamie will collect it", fields[1].TextValue)

	fields = aiCaptureMoveFields(AICaptureItem{MoveDisposition: "not-valid"})
	require.Len(t, fields, 1)
	assert.Equal(t, "Undecided", fields[0].TextValue)
}

func TestSubmitItemsCreatesSelectedItemsAndPreservesCompletedDraftRows(t *testing.T) {
	ctx := tCtx
	locationType, err := tRepos.EntityTypes.GetDefault(ctx, tGroup.ID, true)
	require.NoError(t, err)
	itemType, err := tRepos.EntityTypes.GetDefault(ctx, tGroup.ID, false)
	require.NoError(t, err)
	location, err := tRepos.Entities.Create(ctx, tGroup.ID, repo.EntityCreate{
		Name: "Partial review test", EntityTypeID: locationType.ID,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tRepos.Entities.Delete(ctx, location.ID) })

	cfg := config.AIConfig{Enabled: true, MaxPhotos: 8, MaxSessionPhotos: 24, MaxItems: 25}
	svc := NewAICaptureSessionService(tRepos, NewAICaptureService(cfg), tSvc.Entities, cfg)
	session, err := tRepos.AICaptureSessions.Create(ctx, tGroup.ID, tUser.ID, location.ID, location.Name, 5)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = tRepos.AICaptureSessions.Delete(ctx, tGroup.ID, tUser.ID, session.ID)
	})
	draft := AICaptureDraft{Items: []AICaptureItem{
		{ClientID: "item-1", Name: "Drill", Quantity: 1, EntityTypeID: itemType.ID.String(), MoveDisposition: AICaptureMoveDispositionKeep},
		{ClientID: "item-2", Name: "Saw", Quantity: 1, EntityTypeID: itemType.ID.String(), MoveDisposition: AICaptureMoveDispositionSell},
	}, Warnings: []string{}}
	encoded, err := json.Marshal(draft)
	require.NoError(t, err)
	_, err = tClient.AICaptureSession.UpdateOneID(session.ID).
		SetStatus(aicapturesession.StatusReadyForReview).
		SetDraftJSON(string(encoded)).
		SetDraftRevision(1).
		Save(ctx)
	require.NoError(t, err)

	partial, err := svc.SubmitItems(ctx, session.ID, 1, []string{"item-1"})
	require.NoError(t, err)
	assert.Equal(t, aicapturesession.StatusReadyForReview.String(), partial.Status)
	require.Len(t, partial.CreatedItems, 1)
	assert.Equal(t, "item-1", partial.CreatedItems[0].ClientID)
	t.Cleanup(func() {
		if id, parseErr := uuid.Parse(partial.CreatedItems[0].ID); parseErr == nil {
			_ = tRepos.Entities.Delete(ctx, id)
		}
	})

	partial.Draft.Items[0].Name = "Changed after creation"
	partial.Draft.Items[1].Name = "Updated saw"
	saved, err := svc.SaveDraft(ctx, session.ID, partial.DraftRevision, *partial.Draft)
	require.NoError(t, err)
	assert.Equal(t, "Drill", saved.Draft.Items[0].Name)
	assert.Equal(t, "Updated saw", saved.Draft.Items[1].Name)

	completed, err := svc.SubmitItems(ctx, session.ID, saved.DraftRevision, []string{"item-2"})
	require.NoError(t, err)
	assert.Equal(t, aicapturesession.StatusCompleted.String(), completed.Status)
	require.Len(t, completed.CreatedItems, 2)
	for _, item := range completed.CreatedItems {
		if item.ClientID == "item-1" {
			continue
		}
		itemID, parseErr := uuid.Parse(item.ID)
		require.NoError(t, parseErr)
		t.Cleanup(func() { _ = tRepos.Entities.Delete(ctx, itemID) })
	}
}

func TestSubmitItemsReconcilesPhotoWrittenBeforeProgressUpdate(t *testing.T) {
	ctx := tCtx
	locationType, err := tRepos.EntityTypes.GetDefault(ctx, tGroup.ID, true)
	require.NoError(t, err)
	itemType, err := tRepos.EntityTypes.GetDefault(ctx, tGroup.ID, false)
	require.NoError(t, err)
	location, err := tRepos.Entities.Create(ctx, tGroup.ID, repo.EntityCreate{
		Name: "Interrupted submit test", EntityTypeID: locationType.ID,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tRepos.Entities.Delete(ctx, location.ID) })

	session, err := tRepos.AICaptureSessions.Create(ctx, tGroup.ID, tUser.ID, location.ID, location.Name, 5)
	require.NoError(t, err)
	t.Cleanup(func() {
		stored, getErr := tRepos.AICaptureSessions.Get(ctx, tGroup.ID, tUser.ID, session.ID)
		if getErr == nil {
			_ = tRepos.AICaptureSessions.PurgeSessionPhotos(ctx, stored)
		}
		_, _ = tRepos.AICaptureSessions.Delete(ctx, tGroup.ID, tUser.ID, session.ID)
	})

	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	require.NoError(t, err)
	photoID, path, hash := tRepos.AICaptureSessions.NewPhotoStorage(png, tGroup.ID, session.ID)
	require.NoError(t, tRepos.AICaptureSessions.WriteBlob(ctx, path, "image/png", png))
	_, _, err = tRepos.AICaptureSessions.CreatePhoto(
		ctx, tGroup.ID, tUser.ID, session.ID, photoID, uuid.New(), 0, 8, 24, nil,
		"interrupted.png", "image/png", path, int64(len(png)), hash,
	)
	require.NoError(t, err)
	draft := AICaptureDraft{Items: []AICaptureItem{{
		ClientID: "item-1", Name: "Recovered item", Quantity: 1, EntityTypeID: itemType.ID.String(),
		PhotoIDs: []string{photoID.String()}, MoveDisposition: AICaptureMoveDispositionUndecided,
	}}, Warnings: []string{}}
	encoded, err := json.Marshal(draft)
	require.NoError(t, err)
	_, err = tClient.AICaptureSession.UpdateOneID(session.ID).
		SetStatus(aicapturesession.StatusReadyForReview).
		SetDraftJSON(string(encoded)).
		SetDraftRevision(1).
		Save(ctx)
	require.NoError(t, err)

	entity, err := tRepos.Entities.Create(ctx, tGroup.ID, repo.EntityCreate{
		ParentID: location.ID, Name: "Recovered item", Quantity: 1, EntityTypeID: itemType.ID,
	})
	require.NoError(t, err)
	_, err = tRepos.AICaptureSessions.GetOrCreateSubmissionItem(ctx, session.ID, "item-1")
	require.NoError(t, err)
	require.NoError(t, tRepos.AICaptureSessions.SetSubmissionItemEntity(ctx, session.ID, "item-1", entity.ID))
	withPhoto, err := tSvc.Entities.AttachmentAdd(
		ctx, entity.ID, "interrupted.png", attachment.TypePhoto, true, bytes.NewReader(png),
	)
	require.NoError(t, err)
	require.Len(t, withPhoto.Attachments, 1)

	cfg := config.AIConfig{Enabled: true, MaxPhotos: 8, MaxSessionPhotos: 24, MaxItems: 25}
	svc := NewAICaptureSessionService(tRepos, NewAICaptureService(cfg), tSvc.Entities, cfg)
	completed, err := svc.SubmitItems(ctx, session.ID, 1, []string{"item-1"})
	require.NoError(t, err)
	assert.Equal(t, aicapturesession.StatusCompleted.String(), completed.Status)
	require.Len(t, completed.Submissions, 1)
	assert.Equal(t, "completed", completed.Submissions[0].Status)

	storedEntity, err := tRepos.Entities.GetOneByGroup(ctx, tGroup.ID, entity.ID)
	require.NoError(t, err)
	assert.Len(t, storedEntity.Attachments, 1, "retry must reuse the photo written before cancellation")
}

func TestPartitionAICapturePhotosKeepsGroupsAndUngroupedBatchInFirstPhotoOrder(t *testing.T) {
	groupA := uuid.New()
	groupB := uuid.New()
	photos := []repo.AICapturePhotoRecord{
		{ID: uuid.New(), Position: 0, CaptureGroupID: &groupA},
		{ID: uuid.New(), Position: 1},
		{ID: uuid.New(), Position: 2, CaptureGroupID: &groupA},
		{ID: uuid.New(), Position: 3, CaptureGroupID: &groupB},
		{ID: uuid.New(), Position: 4},
	}

	batches := partitionAICapturePhotos(photos, 8)
	require.Len(t, batches, 3)
	assert.Equal(t, groupA.String(), batches[0].CaptureGroupID)
	assert.Equal(t, []int{0, 2}, []int{batches[0].Photos[0].Position, batches[0].Photos[1].Position})
	assert.Empty(t, batches[1].CaptureGroupID)
	assert.Equal(t, []int{1, 4}, []int{batches[1].Photos[0].Position, batches[1].Photos[1].Position})
	assert.Equal(t, groupB.String(), batches[2].CaptureGroupID)
}

func TestPartitionAICapturePhotosChunksUngroupedProviderRequests(t *testing.T) {
	photos := make([]repo.AICapturePhotoRecord, 0, 5)
	for position := range 5 {
		photos = append(photos, repo.AICapturePhotoRecord{ID: uuid.New(), Position: position})
	}

	batches := partitionAICapturePhotos(photos, 2)
	require.Len(t, batches, 3)
	assert.Equal(t, []int{0, 1}, []int{batches[0].Photos[0].Position, batches[0].Photos[1].Position})
	assert.Equal(t, []int{2, 3}, []int{batches[1].Photos[0].Position, batches[1].Photos[1].Position})
	assert.Equal(t, 4, batches[2].Photos[0].Position)
}

func TestAnalyzeSessionRecoversOrSurfacesEveryOmittedPhoto(t *testing.T) {
	ctx := tCtx
	locationType, err := tRepos.EntityTypes.GetDefault(ctx, tGroup.ID, true)
	require.NoError(t, err)
	itemType, err := tRepos.EntityTypes.GetDefault(ctx, tGroup.ID, false)
	require.NoError(t, err)
	location, err := tRepos.Entities.Create(ctx, tGroup.ID, repo.EntityCreate{
		Name: "Photo coverage test", EntityTypeID: locationType.ID,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tRepos.Entities.Delete(ctx, location.ID) })

	providerCalls := 0
	retryableRecoveryFailure := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls++
		if providerCalls == 2 {
			if retryableRecoveryFailure {
				http.Error(w, "provider is starting", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, "cannot identify this image", http.StatusBadRequest)
			return
		}
		name := "First item"
		if providerCalls == 3 {
			name = "Recovered item"
		}
		content := fmt.Sprintf(
			`{"items":[{"clientId":"item-1","name":%q,"quantity":1,"entityTypeId":%q,"tagIds":[],"photoIndexes":[0],"needsReview":false}],"warnings":[]}`,
			name, itemType.ID.String(),
		)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := config.AIConfig{
		Enabled: true, BaseURL: server.URL, Model: "vision-model", Timeout: time.Second,
		MaxPhotos: 8, MaxSessionPhotos: 24, MaxItems: 25,
	}
	svc := NewAICaptureSessionService(tRepos, NewAICaptureService(cfg), tSvc.Entities, cfg)

	session, err := tRepos.AICaptureSessions.Create(ctx, tGroup.ID, tUser.ID, location.ID, location.Name, 5)
	require.NoError(t, err)
	t.Cleanup(func() {
		stored, getErr := tRepos.AICaptureSessions.Get(ctx, tGroup.ID, tUser.ID, session.ID)
		if getErr == nil {
			_ = tRepos.AICaptureSessions.PurgeSessionPhotos(ctx, stored)
		}
		_, _ = tRepos.AICaptureSessions.Delete(ctx, tGroup.ID, tUser.ID, session.ID)
	})
	photoIDs := make([]uuid.UUID, 0, 3)
	for position := range 3 {
		data := []byte(fmt.Sprintf("photo-%d", position))
		photoID, path, hash := tRepos.AICaptureSessions.NewPhotoStorage(data, tGroup.ID, session.ID)
		require.NoError(t, tRepos.AICaptureSessions.WriteBlob(ctx, path, "image/jpeg", data))
		_, _, err = tRepos.AICaptureSessions.CreatePhoto(
			ctx, tGroup.ID, tUser.ID, session.ID, photoID, uuid.New(), position, 8, 24, nil,
			fmt.Sprintf("photo-%d.jpg", position), "image/jpeg", path, int64(len(data)), hash,
		)
		require.NoError(t, err)
		photoIDs = append(photoIDs, photoID)
	}
	record, err := tRepos.AICaptureSessions.Get(ctx, tGroup.ID, tUser.ID, session.ID)
	require.NoError(t, err)
	metadata, err := svc.metadata(ctx, record)
	require.NoError(t, err)

	draft, err := svc.analyzeSession(ctx, record, metadata)
	require.NoError(t, err)
	assert.Equal(t, 3, providerCalls)
	require.Len(t, draft.Items, 3)
	assert.Equal(t, "First item", draft.Items[0].Name)
	assert.Equal(t, []string{photoIDs[0].String()}, draft.Items[0].PhotoIDs)
	assert.Equal(t, "Unidentified item (photo 2)", draft.Items[1].Name)
	assert.Equal(t, []string{photoIDs[1].String()}, draft.Items[1].PhotoIDs)
	assert.True(t, draft.Items[1].NeedsReview)
	assert.Equal(t, "Recovered item", draft.Items[2].Name)
	assert.Equal(t, []string{photoIDs[2].String()}, draft.Items[2].PhotoIDs)
	assert.Contains(t, draft.Warnings, aiCaptureMissingPhotoWarning)

	repaired, err := svc.validateSessionDraft(ctx, record, AICaptureDraft{Items: []AICaptureItem{{
		ClientID: "existing-item", Name: "Existing item", Quantity: 1,
		EntityTypeID: itemType.ID.String(), TagIDs: []string{}, PhotoIDs: []string{photoIDs[0].String()},
	}}})
	require.NoError(t, err)
	require.Len(t, repaired.Items, 3)
	assert.Equal(t, []string{photoIDs[1].String()}, repaired.Items[1].PhotoIDs)
	assert.Equal(t, []string{photoIDs[2].String()}, repaired.Items[2].PhotoIDs)

	providerCalls = 0
	retryableRecoveryFailure = true
	_, err = svc.analyzeSession(ctx, record, metadata)
	assert.ErrorIs(t, err, ErrAIRetryable)
	assert.Equal(t, 2, providerCalls)
}

func TestValidateDraftCaptureGroupsClearsChangedOrDuplicateAssignments(t *testing.T) {
	groupID := uuid.New()
	photoA := uuid.New()
	photoB := uuid.New()
	photos := []repo.AICapturePhotoRecord{
		{ID: photoA, CaptureGroupID: &groupID},
		{ID: photoB, CaptureGroupID: &groupID},
	}
	draft := AICaptureDraft{Items: []AICaptureItem{
		{ClientID: "kept", CaptureGroupID: groupID.String(), PhotoIDs: []string{photoA.String(), photoB.String()}},
		{ClientID: "duplicate", CaptureGroupID: groupID.String(), PhotoIDs: []string{photoA.String(), photoB.String()}},
		{ClientID: "split", CaptureGroupID: groupID.String(), PhotoIDs: []string{photoA.String()}},
	}}

	validateDraftCaptureGroups(&draft, photos)
	assert.Equal(t, groupID.String(), draft.Items[0].CaptureGroupID)
	assert.Empty(t, draft.Items[1].CaptureGroupID)
	assert.Empty(t, draft.Items[2].CaptureGroupID)
}

func TestSortAICaptureItemsByFirstPhoto(t *testing.T) {
	first := uuid.New()
	second := uuid.New()
	third := uuid.New()
	photos := []repo.AICapturePhotoRecord{
		{ID: first, Position: 0},
		{ID: second, Position: 1},
		{ID: third, Position: 2},
	}
	items := []AICaptureItem{
		{ClientID: "third", PhotoIDs: []string{third.String()}},
		{ClientID: "first", PhotoIDs: []string{first.String(), second.String()}},
		{ClientID: "second", PhotoIDs: []string{second.String()}},
	}

	sortAICaptureItemsByFirstPhoto(items, photos)
	assert.Equal(t, []string{"first", "second", "third"}, []string{items[0].ClientID, items[1].ClientID, items[2].ClientID})
}
