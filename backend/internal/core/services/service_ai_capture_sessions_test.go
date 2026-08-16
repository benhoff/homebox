package services

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		ctx, tGroup.ID, tUser.ID, session.ID, photoID, uuid.New(), 0, 4, nil,
		"photo.jpg", "image/jpeg", path, 5, hash,
	)
	require.NoError(t, err)
	require.NoError(t, tRepos.AICaptureSessions.Finish(ctx, tGroup.ID, tUser.ID, session.ID, 1, 1))
	claimed, found, err := tRepos.AICaptureSessions.ClaimQueued(ctx, 3*time.Minute)
	require.NoError(t, err)
	require.True(t, found)
	draft := fmt.Sprintf(`{"items":[{"clientId":"item-1","name":"Drill","quantity":1,"entityTypeId":"%s","tagIds":[],"photoIds":["%s"],"needsReview":false}],"warnings":[]}`, itemType.ID, photoID)
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

	batches := partitionAICapturePhotos(photos)
	require.Len(t, batches, 3)
	assert.Equal(t, groupA.String(), batches[0].CaptureGroupID)
	assert.Equal(t, []int{0, 2}, []int{batches[0].Photos[0].Position, batches[0].Photos[1].Position})
	assert.Empty(t, batches[1].CaptureGroupID)
	assert.Equal(t, []int{1, 4}, []int{batches[1].Photos[0].Position, batches[1].Photos[1].Position})
	assert.Equal(t, groupB.String(), batches[2].CaptureGroupID)
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
