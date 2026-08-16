package services

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sysadminsmedia/homebox/backend/internal/data/repo"
)

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
