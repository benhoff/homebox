package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/aicapturephoto"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/aicapturesession"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/aicapturesessionitem"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
)

var (
	ErrAICaptureInvalidState  = errors.New("capture session is not in the required state")
	ErrAICapturePhotoLimit    = errors.New("capture session photo limit reached")
	ErrAICapturePhotoCount    = errors.New("capture session photo count does not match")
	ErrAICaptureDraftConflict = errors.New("capture session draft was changed elsewhere")
	ErrAICaptureSessionLimit  = errors.New("too many active capture sessions")
)

type AICaptureSessionRepository struct {
	db          *ent.Client
	attachments *AttachmentRepo
}

type AICapturePhotoRecord struct {
	ID            uuid.UUID `json:"id"`
	ClientPhotoID uuid.UUID `json:"clientPhotoId"`
	Position      int       `json:"position"`
	OriginalName  string    `json:"originalName"`
	Path          string    `json:"-"`
	MIMEType      string    `json:"mimeType"`
	SizeBytes     int64     `json:"sizeBytes"`
	ContentHash   string    `json:"-"`
	CreatedAt     time.Time `json:"createdAt"`
}

type AICaptureSessionItemRecord struct {
	ClientID         string     `json:"clientId"`
	EntityID         *uuid.UUID `json:"entityId,omitempty"`
	Status           string     `json:"status"`
	UploadedPhotoIDs string     `json:"-"`
	ErrorCode        string     `json:"errorCode,omitempty"`
}

type AICaptureSessionRecord struct {
	ID                   uuid.UUID
	GroupID              uuid.UUID
	UserID               uuid.UUID
	LocationID           *uuid.UUID
	LocationNameSnapshot string
	Status               string
	DraftJSON            string
	DraftRevision        int
	AnalysisAttempts     int
	PhotoCount           int
	WorkerLeaseUntil     *time.Time
	ErrorCode            string
	ErrorMessage         string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	FinishedAt           *time.Time
	AnalyzedAt           *time.Time
	CompletedAt          *time.Time
	ExpiresAt            time.Time
	Photos               []AICapturePhotoRecord
	Items                []AICaptureSessionItemRecord
}

func mapAICapturePhoto(row *ent.AICapturePhoto) AICapturePhotoRecord {
	return AICapturePhotoRecord{
		ID:            row.ID,
		ClientPhotoID: row.ClientPhotoID,
		Position:      row.Position,
		OriginalName:  row.OriginalName,
		Path:          row.Path,
		MIMEType:      row.MimeType,
		SizeBytes:     row.SizeBytes,
		ContentHash:   row.ContentHash,
		CreatedAt:     row.CreatedAt,
	}
}

func mapAICaptureSessionItem(row *ent.AICaptureSessionItem) AICaptureSessionItemRecord {
	return AICaptureSessionItemRecord{
		ClientID:         row.ClientID,
		EntityID:         row.EntityID,
		Status:           row.Status.String(),
		UploadedPhotoIDs: row.UploadedPhotoIds,
		ErrorCode:        row.ErrorCode,
	}
}

func mapAICaptureSession(row *ent.AICaptureSession) AICaptureSessionRecord {
	out := AICaptureSessionRecord{
		ID:                   row.ID,
		GroupID:              row.GroupID,
		UserID:               row.UserID,
		LocationID:           row.LocationID,
		LocationNameSnapshot: row.LocationNameSnapshot,
		Status:               row.Status.String(),
		DraftJSON:            row.DraftJSON,
		DraftRevision:        row.DraftRevision,
		AnalysisAttempts:     row.AnalysisAttempts,
		PhotoCount:           row.PhotoCount,
		WorkerLeaseUntil:     row.WorkerLeaseUntil,
		ErrorCode:            row.ErrorCode,
		ErrorMessage:         row.ErrorMessage,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
		FinishedAt:           row.FinishedAt,
		AnalyzedAt:           row.AnalyzedAt,
		CompletedAt:          row.CompletedAt,
		ExpiresAt:            row.ExpiresAt,
	}
	if row.Edges.Photos != nil {
		out.Photos = make([]AICapturePhotoRecord, len(row.Edges.Photos))
		for i, photo := range row.Edges.Photos {
			out.Photos[i] = mapAICapturePhoto(photo)
		}
	}
	if row.Edges.Items != nil {
		out.Items = make([]AICaptureSessionItemRecord, len(row.Edges.Items))
		for i, item := range row.Edges.Items {
			out.Items[i] = mapAICaptureSessionItem(item)
		}
	}
	return out
}

func withAICaptureSessionEdges(query *ent.AICaptureSessionQuery) *ent.AICaptureSessionQuery {
	return query.
		WithPhotos(func(q *ent.AICapturePhotoQuery) { q.Order(ent.Asc(aicapturephoto.FieldPosition)) }).
		WithItems(func(q *ent.AICaptureSessionItemQuery) { q.Order(ent.Asc(aicapturesessionitem.FieldCreatedAt)) })
}

func (r *AICaptureSessionRepository) Create(ctx context.Context, gid, uid, locationID uuid.UUID, locationName string, maxActive int) (AICaptureSessionRecord, error) {
	if maxActive <= 0 {
		maxActive = 5
	}
	active, err := r.db.AICaptureSession.Query().Where(
		aicapturesession.GroupID(gid),
		aicapturesession.UserID(uid),
		aicapturesession.StatusNEQ(aicapturesession.StatusCompleted),
	).Count(ctx)
	if err != nil {
		return AICaptureSessionRecord{}, err
	}
	if active >= maxActive {
		return AICaptureSessionRecord{}, ErrAICaptureSessionLimit
	}
	row, err := r.db.AICaptureSession.Create().
		SetGroupID(gid).
		SetUserID(uid).
		SetLocationID(locationID).
		SetLocationNameSnapshot(locationName).
		SetExpiresAt(time.Now().Add(30 * 24 * time.Hour)).
		Save(ctx)
	if err != nil {
		return AICaptureSessionRecord{}, err
	}
	return mapAICaptureSession(row), nil
}

func (r *AICaptureSessionRepository) List(ctx context.Context, gid, uid uuid.UUID) ([]AICaptureSessionRecord, error) {
	rows, err := withAICaptureSessionEdges(r.db.AICaptureSession.Query()).
		Where(aicapturesession.GroupID(gid), aicapturesession.UserID(uid)).
		Order(ent.Desc(aicapturesession.FieldUpdatedAt)).
		Limit(50).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AICaptureSessionRecord, len(rows))
	for i, row := range rows {
		out[i] = mapAICaptureSession(row)
	}
	return out, nil
}

func (r *AICaptureSessionRepository) Get(ctx context.Context, gid, uid, id uuid.UUID) (AICaptureSessionRecord, error) {
	row, err := withAICaptureSessionEdges(r.db.AICaptureSession.Query()).Where(
		aicapturesession.ID(id), aicapturesession.GroupID(gid), aicapturesession.UserID(uid),
	).Only(ctx)
	if err != nil {
		return AICaptureSessionRecord{}, err
	}
	return mapAICaptureSession(row), nil
}

func (r *AICaptureSessionRepository) getInternal(ctx context.Context, id uuid.UUID) (AICaptureSessionRecord, error) {
	row, err := withAICaptureSessionEdges(r.db.AICaptureSession.Query()).Where(aicapturesession.ID(id)).Only(ctx)
	if err != nil {
		return AICaptureSessionRecord{}, err
	}
	return mapAICaptureSession(row), nil
}

func (r *AICaptureSessionRepository) UpdateLocation(ctx context.Context, gid, uid, id, locationID uuid.UUID, locationName string) error {
	updated, err := r.db.AICaptureSession.Update().Where(
		aicapturesession.ID(id), aicapturesession.GroupID(gid), aicapturesession.UserID(uid),
		aicapturesession.StatusIn(aicapturesession.StatusCapturing, aicapturesession.StatusAnalysisFailed, aicapturesession.StatusReadyForReview),
	).SetLocationID(locationID).
		SetLocationNameSnapshot(locationName).
		SetExpiresAt(time.Now().Add(30 * 24 * time.Hour)).
		ClearErrorCode().ClearErrorMessage().
		Save(ctx)
	if err != nil {
		return err
	}
	if updated == 0 {
		return ErrAICaptureInvalidState
	}
	return nil
}

func (r *AICaptureSessionRepository) CreatePhoto(ctx context.Context, gid, uid, sessionID, photoID, clientPhotoID uuid.UUID, position, maxPhotos int, name, mimeType, blobPath string, size int64, hash string) (AICapturePhotoRecord, bool, error) {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return AICapturePhotoRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	session, err := tx.AICaptureSession.Query().Where(
		aicapturesession.ID(sessionID), aicapturesession.GroupID(gid), aicapturesession.UserID(uid),
	).Only(ctx)
	if err != nil {
		return AICapturePhotoRecord{}, false, err
	}
	if session.Status != aicapturesession.StatusCapturing {
		return AICapturePhotoRecord{}, false, ErrAICaptureInvalidState
	}
	existing, err := tx.AICapturePhoto.Query().Where(
		aicapturephoto.SessionID(sessionID), aicapturephoto.ClientPhotoID(clientPhotoID),
	).Only(ctx)
	if err == nil {
		if err := tx.Commit(); err != nil {
			return AICapturePhotoRecord{}, false, err
		}
		return mapAICapturePhoto(existing), true, nil
	}
	if !ent.IsNotFound(err) {
		return AICapturePhotoRecord{}, false, err
	}
	count, err := tx.AICapturePhoto.Query().Where(aicapturephoto.SessionID(sessionID)).Count(ctx)
	if err != nil {
		return AICapturePhotoRecord{}, false, err
	}
	if count >= maxPhotos {
		return AICapturePhotoRecord{}, false, ErrAICapturePhotoLimit
	}
	row, err := tx.AICapturePhoto.Create().
		SetID(photoID).
		SetSessionID(sessionID).
		SetClientPhotoID(clientPhotoID).
		SetPosition(position).
		SetOriginalName(name).
		SetPath(blobPath).
		SetMimeType(mimeType).
		SetSizeBytes(size).
		SetContentHash(hash).
		Save(ctx)
	if err != nil {
		return AICapturePhotoRecord{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return AICapturePhotoRecord{}, false, err
	}
	return mapAICapturePhoto(row), false, nil
}

func (r *AICaptureSessionRepository) DeletePhotoRow(ctx context.Context, gid, uid, sessionID, photoID uuid.UUID) (AICapturePhotoRecord, error) {
	session, err := r.Get(ctx, gid, uid, sessionID)
	if err != nil {
		return AICapturePhotoRecord{}, err
	}
	if session.Status != aicapturesession.StatusCapturing.String() {
		return AICapturePhotoRecord{}, ErrAICaptureInvalidState
	}
	row, err := r.db.AICapturePhoto.Query().Where(
		aicapturephoto.ID(photoID), aicapturephoto.SessionID(sessionID),
	).Only(ctx)
	if err != nil {
		return AICapturePhotoRecord{}, err
	}
	if err := r.db.AICapturePhoto.DeleteOneID(photoID).Exec(ctx); err != nil {
		return AICapturePhotoRecord{}, err
	}
	return mapAICapturePhoto(row), nil
}

func (r *AICaptureSessionRepository) Finish(ctx context.Context, gid, uid, id uuid.UUID, expected int) error {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := tx.AICaptureSession.Query().Where(
		aicapturesession.ID(id), aicapturesession.GroupID(gid), aicapturesession.UserID(uid),
	).Only(ctx)
	if err != nil {
		return err
	}
	if row.Status != aicapturesession.StatusCapturing {
		if row.Status == aicapturesession.StatusQueued || row.Status == aicapturesession.StatusAnalyzing || row.Status == aicapturesession.StatusReadyForReview {
			return tx.Commit()
		}
		return ErrAICaptureInvalidState
	}
	count, err := tx.AICapturePhoto.Query().Where(aicapturephoto.SessionID(id)).Count(ctx)
	if err != nil {
		return err
	}
	if count == 0 || count != expected {
		return ErrAICapturePhotoCount
	}
	now := time.Now()
	updated, err := tx.AICaptureSession.Update().Where(
		aicapturesession.ID(id), aicapturesession.StatusEQ(aicapturesession.StatusCapturing),
	).SetStatus(aicapturesession.StatusQueued).
		SetPhotoCount(count).
		SetFinishedAt(now).
		SetExpiresAt(now.Add(30 * 24 * time.Hour)).
		Save(ctx)
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrAICaptureInvalidState
	}
	return tx.Commit()
}

func (r *AICaptureSessionRepository) ClaimQueued(ctx context.Context, lease time.Duration) (AICaptureSessionRecord, bool, error) {
	now := time.Now()
	_, err := r.db.AICaptureSession.Update().Where(
		aicapturesession.StatusEQ(aicapturesession.StatusAnalyzing),
		aicapturesession.WorkerLeaseUntilLT(now),
	).SetStatus(aicapturesession.StatusQueued).ClearWorkerLeaseUntil().Save(ctx)
	if err != nil {
		return AICaptureSessionRecord{}, false, err
	}
	row, err := r.db.AICaptureSession.Query().Where(aicapturesession.StatusEQ(aicapturesession.StatusQueued)).
		Order(ent.Asc(aicapturesession.FieldUpdatedAt)).First(ctx)
	if ent.IsNotFound(err) {
		return AICaptureSessionRecord{}, false, nil
	}
	if err != nil {
		return AICaptureSessionRecord{}, false, err
	}
	updated, err := r.db.AICaptureSession.Update().Where(
		aicapturesession.ID(row.ID), aicapturesession.StatusEQ(aicapturesession.StatusQueued),
	).SetStatus(aicapturesession.StatusAnalyzing).
		SetWorkerLeaseUntil(now.Add(lease)).
		AddAnalysisAttempts(1).
		ClearErrorCode().ClearErrorMessage().
		Save(ctx)
	if err != nil {
		return AICaptureSessionRecord{}, false, err
	}
	if updated != 1 {
		return AICaptureSessionRecord{}, false, nil
	}
	claimed, err := r.getInternal(ctx, row.ID)
	return claimed, err == nil, err
}

func (r *AICaptureSessionRepository) SetAnalysisReady(ctx context.Context, id uuid.UUID, draftJSON string) error {
	now := time.Now()
	updated, err := r.db.AICaptureSession.Update().Where(
		aicapturesession.ID(id), aicapturesession.StatusEQ(aicapturesession.StatusAnalyzing),
	).SetStatus(aicapturesession.StatusReadyForReview).
		SetDraftJSON(draftJSON).
		AddDraftRevision(1).
		SetAnalyzedAt(now).
		SetExpiresAt(now.Add(30 * 24 * time.Hour)).
		ClearWorkerLeaseUntil().ClearErrorCode().ClearErrorMessage().
		Save(ctx)
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrAICaptureInvalidState
	}
	return nil
}

func (r *AICaptureSessionRepository) SetAnalysisError(ctx context.Context, id uuid.UUID, retry bool, code, message string) error {
	status := aicapturesession.StatusAnalysisFailed
	if retry {
		status = aicapturesession.StatusQueued
	}
	return r.db.AICaptureSession.UpdateOneID(id).
		Where(aicapturesession.StatusEQ(aicapturesession.StatusAnalyzing)).
		SetStatus(status).
		SetErrorCode(code).
		SetErrorMessage(message).
		ClearWorkerLeaseUntil().
		Exec(ctx)
}

func (r *AICaptureSessionRepository) RetryAnalysis(ctx context.Context, gid, uid, id uuid.UUID) error {
	updated, err := r.db.AICaptureSession.Update().Where(
		aicapturesession.ID(id), aicapturesession.GroupID(gid), aicapturesession.UserID(uid),
		aicapturesession.StatusEQ(aicapturesession.StatusAnalysisFailed),
	).SetStatus(aicapturesession.StatusQueued).
		SetAnalysisAttempts(0).
		ClearErrorCode().ClearErrorMessage().
		Save(ctx)
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrAICaptureInvalidState
	}
	return nil
}

func (r *AICaptureSessionRepository) SaveDraft(ctx context.Context, gid, uid, id uuid.UUID, expectedRevision int, draftJSON string) error {
	updated, err := r.db.AICaptureSession.Update().Where(
		aicapturesession.ID(id), aicapturesession.GroupID(gid), aicapturesession.UserID(uid),
		aicapturesession.StatusEQ(aicapturesession.StatusReadyForReview),
		aicapturesession.DraftRevisionEQ(expectedRevision),
	).SetDraftJSON(draftJSON).
		AddDraftRevision(1).
		SetExpiresAt(time.Now().Add(30 * 24 * time.Hour)).
		Save(ctx)
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrAICaptureDraftConflict
	}
	return nil
}

func (r *AICaptureSessionRepository) StartSubmitting(ctx context.Context, gid, uid, id uuid.UUID, expectedRevision int) error {
	updated, err := r.db.AICaptureSession.Update().Where(
		aicapturesession.ID(id), aicapturesession.GroupID(gid), aicapturesession.UserID(uid),
		aicapturesession.StatusEQ(aicapturesession.StatusReadyForReview),
		aicapturesession.DraftRevisionEQ(expectedRevision),
	).SetStatus(aicapturesession.StatusSubmitting).Save(ctx)
	if err != nil {
		return err
	}
	if updated != 1 {
		return ErrAICaptureDraftConflict
	}
	return nil
}

func (r *AICaptureSessionRepository) SetSubmitFailed(ctx context.Context, id uuid.UUID, code, message string) error {
	return r.db.AICaptureSession.UpdateOneID(id).
		Where(aicapturesession.StatusEQ(aicapturesession.StatusSubmitting)).
		SetStatus(aicapturesession.StatusReadyForReview).
		SetErrorCode(code).SetErrorMessage(message).
		Exec(ctx)
}

func (r *AICaptureSessionRepository) SetCompleted(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	return r.db.AICaptureSession.UpdateOneID(id).
		Where(aicapturesession.StatusEQ(aicapturesession.StatusSubmitting)).
		SetStatus(aicapturesession.StatusCompleted).
		SetCompletedAt(now).
		ClearErrorCode().ClearErrorMessage().
		Exec(ctx)
}

func (r *AICaptureSessionRepository) GetOrCreateSubmissionItem(ctx context.Context, sessionID uuid.UUID, clientID string) (AICaptureSessionItemRecord, error) {
	row, err := r.db.AICaptureSessionItem.Query().Where(
		aicapturesessionitem.SessionID(sessionID), aicapturesessionitem.ClientID(clientID),
	).Only(ctx)
	if err == nil {
		return mapAICaptureSessionItem(row), nil
	}
	if !ent.IsNotFound(err) {
		return AICaptureSessionItemRecord{}, err
	}
	row, err = r.db.AICaptureSessionItem.Create().SetSessionID(sessionID).SetClientID(clientID).Save(ctx)
	if ent.IsConstraintError(err) {
		row, err = r.db.AICaptureSessionItem.Query().Where(
			aicapturesessionitem.SessionID(sessionID), aicapturesessionitem.ClientID(clientID),
		).Only(ctx)
	}
	if err != nil {
		return AICaptureSessionItemRecord{}, err
	}
	return mapAICaptureSessionItem(row), nil
}

func (r *AICaptureSessionRepository) SetSubmissionItemEntity(ctx context.Context, sessionID uuid.UUID, clientID string, entityID uuid.UUID) error {
	return r.db.AICaptureSessionItem.Update().Where(
		aicapturesessionitem.SessionID(sessionID), aicapturesessionitem.ClientID(clientID),
	).SetEntityID(entityID).SetStatus(aicapturesessionitem.StatusAttaching).ClearErrorCode().Exec(ctx)
}

func (r *AICaptureSessionRepository) SetSubmissionItemPhotos(ctx context.Context, sessionID uuid.UUID, clientID, uploadedJSON string) error {
	return r.db.AICaptureSessionItem.Update().Where(
		aicapturesessionitem.SessionID(sessionID), aicapturesessionitem.ClientID(clientID),
	).SetUploadedPhotoIds(uploadedJSON).SetStatus(aicapturesessionitem.StatusAttaching).Exec(ctx)
}

func (r *AICaptureSessionRepository) SetSubmissionItemStatus(ctx context.Context, sessionID uuid.UUID, clientID string, status aicapturesessionitem.Status, errorCode string) error {
	update := r.db.AICaptureSessionItem.Update().Where(
		aicapturesessionitem.SessionID(sessionID), aicapturesessionitem.ClientID(clientID),
	).SetStatus(status)
	if errorCode == "" {
		update.ClearErrorCode()
	} else {
		update.SetErrorCode(errorCode)
	}
	return update.Exec(ctx)
}

func (r *AICaptureSessionRepository) Delete(ctx context.Context, gid, uid, id uuid.UUID) (AICaptureSessionRecord, error) {
	row, err := r.Get(ctx, gid, uid, id)
	if err != nil {
		return AICaptureSessionRecord{}, err
	}
	_, err = r.db.AICaptureSession.Delete().Where(
		aicapturesession.ID(id), aicapturesession.GroupID(gid), aicapturesession.UserID(uid),
	).Exec(ctx)
	return row, err
}

func (r *AICaptureSessionRepository) ListExpired(ctx context.Context, now time.Time) ([]AICaptureSessionRecord, error) {
	rows, err := withAICaptureSessionEdges(r.db.AICaptureSession.Query()).Where(
		aicapturesession.ExpiresAtLT(now), aicapturesession.StatusNEQ(aicapturesession.StatusCompleted),
	).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AICaptureSessionRecord, len(rows))
	for i, row := range rows {
		out[i] = mapAICaptureSession(row)
	}
	return out, nil
}

func (r *AICaptureSessionRepository) DeleteInternal(ctx context.Context, id uuid.UUID) error {
	return r.db.AICaptureSession.DeleteOneID(id).Exec(ctx)
}

func (r *AICaptureSessionRepository) captureBlobPath(gid, sessionID, photoID uuid.UUID) string {
	return fmt.Sprintf("%s/ai-capture/%s/%s", gid, sessionID, photoID)
}

func (r *AICaptureSessionRepository) NewPhotoStorage(data []byte, gid, sessionID uuid.UUID) (uuid.UUID, string, string) {
	id := uuid.New()
	hash := sha256.Sum256(data)
	return id, r.captureBlobPath(gid, sessionID, id), hex.EncodeToString(hash[:])
}

func (r *AICaptureSessionRepository) WriteBlob(ctx context.Context, relativePath, mimeType string, data []byte) error {
	bucket, err := blob.OpenBucket(ctx, r.attachments.GetConnString())
	if err != nil {
		return err
	}
	defer bucket.Close()
	return bucket.WriteAll(ctx, r.attachments.GetFullPath(relativePath), data, &blob.WriterOptions{ContentType: mimeType})
}

func (r *AICaptureSessionRepository) ReadBlob(ctx context.Context, relativePath string) ([]byte, error) {
	bucket, err := blob.OpenBucket(ctx, r.attachments.GetConnString())
	if err != nil {
		return nil, err
	}
	defer bucket.Close()
	return bucket.ReadAll(ctx, r.attachments.GetFullPath(relativePath))
}

func (r *AICaptureSessionRepository) DeleteBlob(ctx context.Context, relativePath string) error {
	if relativePath == "" {
		return nil
	}
	bucket, err := blob.OpenBucket(ctx, r.attachments.GetConnString())
	if err != nil {
		return err
	}
	defer bucket.Close()
	err = bucket.Delete(ctx, r.attachments.GetFullPath(relativePath))
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil
	}
	return err
}

func (r *AICaptureSessionRepository) PurgeSessionPhotos(ctx context.Context, session AICaptureSessionRecord) error {
	for _, photo := range session.Photos {
		if err := r.DeleteBlob(ctx, photo.Path); err != nil {
			return err
		}
	}
	_, err := r.db.AICapturePhoto.Delete().Where(aicapturephoto.SessionID(session.ID)).Exec(ctx)
	return err
}
