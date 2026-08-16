package repo

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
)

func TestAICaptureSessionRepository_DurableCaptureLifecycle(t *testing.T) {
	ctx := context.Background()
	locationType := useContainerEntityType(t)
	location, err := tRepos.Entities.Create(ctx, tGroup.ID, EntityCreate{
		Name:         "AI capture test location",
		EntityTypeID: locationType.ID,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tRepos.Entities.Delete(ctx, location.ID) })

	session, err := tRepos.AICaptureSessions.Create(ctx, tGroup.ID, tUser.ID, location.ID, location.Name, 5)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = tRepos.AICaptureSessions.Delete(ctx, tGroup.ID, tUser.ID, session.ID) })
	assert.Equal(t, "capturing", session.Status)

	_, err = tRepos.AICaptureSessions.Get(ctx, tGroup.ID, uuid.New(), session.ID)
	assert.True(t, ent.IsNotFound(err), "sessions must be private to their owner")

	clientPhotoID := uuid.New()
	groupA := uuid.New()
	groupB := uuid.New()
	photoID, path, hash := tRepos.AICaptureSessions.NewPhotoStorage([]byte("image"), tGroup.ID, session.ID)
	photo, existing, err := tRepos.AICaptureSessions.CreatePhoto(
		ctx, tGroup.ID, tUser.ID, session.ID, photoID, clientPhotoID, 0, 8,
		&groupA, "photo.jpg", "image/jpeg", path, 5, hash,
	)
	require.NoError(t, err)
	assert.False(t, existing)
	assert.Equal(t, photoID, photo.ID, "the persisted id must match its temporary blob path")

	duplicate, existing, err := tRepos.AICaptureSessions.CreatePhoto(
		ctx, tGroup.ID, tUser.ID, session.ID, uuid.New(), clientPhotoID, 0, 8,
		&groupB, "photo.jpg", "image/jpeg", path, 5, hash,
	)
	require.NoError(t, err)
	assert.True(t, existing)
	assert.Equal(t, photo.ID, duplicate.ID, "replaying a client photo id must be idempotent")
	assert.Equal(t, &groupA, duplicate.CaptureGroupID, "a replay must not silently move an existing photo")

	afterUpload, err := tRepos.AICaptureSessions.Get(ctx, tGroup.ID, tUser.ID, session.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, afterUpload.CaptureRevision)
	regrouped, err := tRepos.AICaptureSessions.UpdatePhotoGroup(ctx, tGroup.ID, tUser.ID, session.ID, photo.ID, &groupB)
	require.NoError(t, err)
	assert.Equal(t, &groupB, regrouped.CaptureGroupID)
	afterRegroup, err := tRepos.AICaptureSessions.Get(ctx, tGroup.ID, tUser.ID, session.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, afterRegroup.CaptureRevision)

	err = tRepos.AICaptureSessions.Finish(ctx, tGroup.ID, tUser.ID, session.ID, 2, 2)
	assert.ErrorIs(t, err, ErrAICapturePhotoCount)
	err = tRepos.AICaptureSessions.Finish(ctx, tGroup.ID, tUser.ID, session.ID, 1, 1)
	assert.ErrorIs(t, err, ErrAICaptureRevision)
	require.NoError(t, tRepos.AICaptureSessions.Finish(ctx, tGroup.ID, tUser.ID, session.ID, 1, 2))

	queued, err := tRepos.AICaptureSessions.Get(ctx, tGroup.ID, tUser.ID, session.ID)
	require.NoError(t, err)
	assert.Equal(t, "queued", queued.Status)
	assert.Equal(t, 1, queued.PhotoCount)

	_, _, err = tRepos.AICaptureSessions.CreatePhoto(
		ctx, tGroup.ID, tUser.ID, session.ID, uuid.New(), uuid.New(), 1, 8,
		nil, "late.jpg", "image/jpeg", "late", 1, "hash",
	)
	assert.True(t, errors.Is(err, ErrAICaptureInvalidState))
}
