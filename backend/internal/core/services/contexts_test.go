package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/sysadminsmedia/homebox/backend/internal/data/repo"
)

func Test_SetAuthContext(t *testing.T) {
	user := &repo.UserOut{
		ID: uuid.New(),
	}

	token := uuid.New().String()

	ctx := SetUserCtx(context.Background(), user, token)

	ctxUser := UseUserCtx(ctx)

	assert.NotNil(t, ctxUser)
	assert.Equal(t, user.ID, ctxUser.ID)

	ctxUserToken := UseTokenCtx(ctx)
	assert.NotEmpty(t, ctxUserToken)
}

func Test_SetAuthContext_Nulls(t *testing.T) {
	ctx := SetUserCtx(context.Background(), nil, "")

	ctxUser := UseUserCtx(ctx)

	assert.Nil(t, ctxUser)

	ctxUserToken := UseTokenCtx(ctx)
	assert.Empty(t, ctxUserToken)
}

func TestNewDetachedContextPreservesAuthWithoutClientCancellation(t *testing.T) {
	user := &repo.UserOut{ID: uuid.New(), DefaultGroupID: uuid.New()}
	tenantID := uuid.New()
	requestCtx, cancel := context.WithCancel(context.Background())
	requestCtx = SetUserCtx(requestCtx, user, "request-token")
	requestCtx = SetTenantCtx(requestCtx, tenantID)

	detached := NewDetachedContext(requestCtx)
	cancel()

	assert.NoError(t, detached.Err())
	assert.Equal(t, user.ID, detached.UID)
	assert.Equal(t, tenantID, detached.GID)
	assert.Equal(t, "request-token", UseTokenCtx(detached))
}
