package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/aicapturesessionitem"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/attachment"
	"github.com/sysadminsmedia/homebox/backend/internal/data/repo"
	"github.com/sysadminsmedia/homebox/backend/internal/sys/config"
)

const (
	AICaptureErrorInvalidState      = "INVALID_STATE"
	AICaptureErrorSessionFull       = "SESSION_FULL"
	AICaptureErrorGroupFull         = "CAPTURE_GROUP_FULL"
	AICaptureErrorPhotoMismatch     = "PHOTO_COUNT_MISMATCH"
	AICaptureErrorRevisionMismatch  = "CAPTURE_REVISION_MISMATCH"
	AICaptureErrorLocationMissing   = "LOCATION_MISSING"
	AICaptureErrorAnalysisFailed    = "ANALYSIS_UPSTREAM_FAILED"
	AICaptureErrorDraftConflict     = "DRAFT_REVISION_CONFLICT"
	AICaptureErrorSubmitFailed      = "SUBMISSION_FAILED"
	AICaptureErrorSubmitInterrupted = "SUBMISSION_INTERRUPTED"
)

const aiCaptureSubmissionStaleAfter = 5 * time.Minute

type AICaptureSessionPhoto struct {
	ID             string    `json:"id"`
	ClientPhotoID  string    `json:"clientPhotoId"`
	Position       int       `json:"position"`
	CaptureGroupID *string   `json:"captureGroupId" extensions:"x-nullable"`
	OriginalName   string    `json:"originalName"`
	MIMEType       string    `json:"mimeType"`
	SizeBytes      int64     `json:"sizeBytes"`
	CreatedAt      time.Time `json:"createdAt"`
}

type AICaptureCreatedItem struct {
	ID       string `json:"id"`
	ClientID string `json:"clientId"`
	Name     string `json:"name"`
}

type AICaptureItemSubmission struct {
	ClientID  string  `json:"clientId"`
	Status    string  `json:"status"`
	EntityID  *string `json:"entityId,omitempty" extensions:"x-nullable"`
	ErrorCode string  `json:"errorCode,omitempty"`
}

type AICaptureSessionOut struct {
	ID                    string                       `json:"id"`
	Status                string                       `json:"status"`
	Location              *AICaptureOption             `json:"location,omitempty"`
	PhotoCount            int                          `json:"photoCount"`
	UploadedPhotoCount    int                          `json:"uploadedPhotoCount"`
	Photos                []AICaptureSessionPhoto      `json:"photos"`
	AnalysisAttempts      int                          `json:"analysisAttempts"`
	AnalysisNextAttemptAt *time.Time                   `json:"analysisNextAttemptAt,omitempty"`
	DraftRevision         int                          `json:"draftRevision"`
	CaptureRevision       int                          `json:"captureRevision"`
	Draft                 *AICaptureDraft              `json:"draft,omitempty"`
	CreatedItems          []AICaptureCreatedItem       `json:"createdItems"`
	Submissions           []AICaptureItemSubmission    `json:"submissions"`
	ErrorCode             string                       `json:"errorCode,omitempty"`
	ErrorMessage          string                       `json:"errorMessage,omitempty"`
	CreatedAt             time.Time                    `json:"createdAt"`
	UpdatedAt             time.Time                    `json:"updatedAt"`
	FinishedAt            *time.Time                   `json:"finishedAt,omitempty"`
	AnalyzedAt            *time.Time                   `json:"analyzedAt,omitempty"`
	CompletedAt           *time.Time                   `json:"completedAt,omitempty"`
	ExpiresAt             time.Time                    `json:"expiresAt"`
	Reanalysis            *AICaptureReanalysisBatchOut `json:"reanalysis,omitempty"`
}

type AICaptureReanalysisOut struct {
	Item     AICaptureItem `json:"item"`
	Provider string        `json:"provider"`
	Warnings []string      `json:"warnings"`
}

type AICaptureReanalysisBatchOut struct {
	Status           string                            `json:"status"`
	Provider         string                            `json:"provider"`
	Total            int                               `json:"total"`
	Completed        int                               `json:"completed"`
	Attempts         int                               `json:"attempts"`
	ClientIDs        []string                          `json:"clientIds"`
	PendingClientIDs []string                          `json:"pendingClientIds"`
	Suggestions      map[string]AICaptureReanalysisOut `json:"suggestions"`
	Errors           map[string]string                 `json:"errors"`
	NextAttemptAt    *time.Time                        `json:"nextAttemptAt,omitempty"`
	CreatedAt        time.Time                         `json:"createdAt"`
	UpdatedAt        time.Time                         `json:"updatedAt"`
	CompletedAt      *time.Time                        `json:"completedAt,omitempty"`
}

type aiCaptureReanalysisState struct {
	Status      string                            `json:"status"`
	Provider    string                            `json:"provider"`
	Instruction string                            `json:"instruction,omitempty"`
	Revision    int                               `json:"revision"`
	Items       []AICaptureItem                   `json:"items"`
	Current     int                               `json:"current"`
	Attempts    int                               `json:"attempts"`
	Suggestions map[string]AICaptureReanalysisOut `json:"suggestions"`
	Errors      map[string]string                 `json:"errors"`
	CreatedAt   time.Time                         `json:"createdAt"`
	UpdatedAt   time.Time                         `json:"updatedAt"`
	CompletedAt *time.Time                        `json:"completedAt,omitempty"`
}

type AICaptureSessionService struct {
	repos    *repo.AllRepos
	ai       *AICaptureService
	entities *EntityService
	config   config.AIConfig
}

func NewAICaptureSessionService(repos *repo.AllRepos, ai *AICaptureService, entities *EntityService, cfg config.AIConfig) *AICaptureSessionService {
	return &AICaptureSessionService{repos: repos, ai: ai, entities: entities, config: cfg}
}

func (svc *AICaptureSessionService) mapOut(record repo.AICaptureSessionRecord) (AICaptureSessionOut, error) {
	out := AICaptureSessionOut{
		ID:                    record.ID.String(),
		Status:                record.Status,
		PhotoCount:            max(record.PhotoCount, len(record.Photos)),
		UploadedPhotoCount:    len(record.Photos),
		Photos:                make([]AICaptureSessionPhoto, len(record.Photos)),
		AnalysisAttempts:      record.AnalysisAttempts,
		AnalysisNextAttemptAt: record.AnalysisNextAttemptAt,
		DraftRevision:         record.DraftRevision,
		CaptureRevision:       record.CaptureRevision,
		CreatedItems:          []AICaptureCreatedItem{},
		Submissions:           []AICaptureItemSubmission{},
		ErrorCode:             record.ErrorCode,
		ErrorMessage:          record.ErrorMessage,
		CreatedAt:             record.CreatedAt,
		UpdatedAt:             record.UpdatedAt,
		FinishedAt:            record.FinishedAt,
		AnalyzedAt:            record.AnalyzedAt,
		CompletedAt:           record.CompletedAt,
		ExpiresAt:             record.ExpiresAt,
	}
	if record.ReanalysisJSON != "" {
		var state aiCaptureReanalysisState
		if err := json.Unmarshal([]byte(record.ReanalysisJSON), &state); err != nil {
			return AICaptureSessionOut{}, fmt.Errorf("decode capture reanalysis: %w", err)
		}
		if state.Suggestions == nil {
			state.Suggestions = map[string]AICaptureReanalysisOut{}
		}
		if state.Errors == nil {
			state.Errors = map[string]string{}
		}
		pending := make([]string, 0, max(0, len(state.Items)-state.Current))
		clientIDs := make([]string, 0, len(state.Items))
		for _, item := range state.Items {
			clientIDs = append(clientIDs, item.ClientID)
		}
		for index := max(0, state.Current); index < len(state.Items); index++ {
			pending = append(pending, state.Items[index].ClientID)
		}
		out.Reanalysis = &AICaptureReanalysisBatchOut{
			Status: record.ReanalysisStatus, Provider: state.Provider,
			Total: len(state.Items), Completed: min(max(state.Current, 0), len(state.Items)),
			Attempts: state.Attempts, ClientIDs: clientIDs, PendingClientIDs: pending,
			Suggestions: state.Suggestions, Errors: state.Errors,
			NextAttemptAt: record.ReanalysisNextAttemptAt,
			CreatedAt:     state.CreatedAt, UpdatedAt: state.UpdatedAt, CompletedAt: state.CompletedAt,
		}
	}
	if record.LocationID != nil {
		out.Location = &AICaptureOption{ID: record.LocationID.String(), Name: record.LocationNameSnapshot}
	}
	for i, photo := range record.Photos {
		var captureGroupID *string
		if photo.CaptureGroupID != nil {
			value := photo.CaptureGroupID.String()
			captureGroupID = &value
		}
		out.Photos[i] = AICaptureSessionPhoto{
			ID: photo.ID.String(), ClientPhotoID: photo.ClientPhotoID.String(), Position: photo.Position,
			CaptureGroupID: captureGroupID, OriginalName: photo.OriginalName, MIMEType: photo.MIMEType,
			SizeBytes: photo.SizeBytes, CreatedAt: photo.CreatedAt,
		}
	}
	var draft *AICaptureDraft
	if record.DraftJSON != "" {
		draft = &AICaptureDraft{}
		if err := json.Unmarshal([]byte(record.DraftJSON), draft); err != nil {
			return AICaptureSessionOut{}, fmt.Errorf("decode capture draft: %w", err)
		}
		for index := range draft.Items {
			draft.Items[index].MoveDisposition = sanitizeMoveDisposition(draft.Items[index].MoveDisposition)
			draft.Items[index].MoveDispositionNote = truncate(strings.TrimSpace(draft.Items[index].MoveDispositionNote), 500)
		}
		out.Draft = draft
	}
	names := map[string]string{}
	if draft != nil {
		for _, item := range draft.Items {
			names[item.ClientID] = item.Name
		}
	}
	for _, item := range record.Items {
		var entityID *string
		if item.EntityID != nil {
			value := item.EntityID.String()
			entityID = &value
		}
		out.Submissions = append(out.Submissions, AICaptureItemSubmission{
			ClientID: item.ClientID, Status: item.Status, EntityID: entityID, ErrorCode: item.ErrorCode,
		})
		if item.EntityID != nil {
			out.CreatedItems = append(out.CreatedItems, AICaptureCreatedItem{
				ID: item.EntityID.String(), ClientID: item.ClientID, Name: names[item.ClientID],
			})
		}
	}
	return out, nil
}

func (svc *AICaptureSessionService) Create(ctx Context, locationID uuid.UUID) (AICaptureSessionOut, error) {
	if svc.ai == nil || !svc.ai.IsEnabled() {
		return AICaptureSessionOut{}, ErrAIDisabled
	}
	location, err := svc.repos.Entities.GetOneByGroup(ctx, ctx.GID, locationID)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if location.EntityType == nil || !location.EntityType.IsLocation {
		return AICaptureSessionOut{}, fmt.Errorf("%s: select a valid location", AICaptureErrorLocationMissing)
	}
	record, err := svc.repos.AICaptureSessions.Create(ctx, ctx.GID, ctx.UID, locationID, location.Name, 5)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	return svc.mapOut(record)
}

func (svc *AICaptureSessionService) List(ctx Context) ([]AICaptureSessionOut, error) {
	records, err := svc.repos.AICaptureSessions.List(ctx, ctx.GID, ctx.UID)
	if err != nil {
		return nil, err
	}
	out := make([]AICaptureSessionOut, 0, len(records))
	for _, record := range records {
		mapped, err := svc.mapOut(record)
		if err != nil {
			return nil, err
		}
		out = append(out, mapped)
	}
	return out, nil
}

func (svc *AICaptureSessionService) Get(ctx Context, id uuid.UUID) (AICaptureSessionOut, error) {
	record, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	return svc.mapOut(record)
}

func (svc *AICaptureSessionService) UpdateLocation(ctx Context, id, locationID uuid.UUID) (AICaptureSessionOut, error) {
	location, err := svc.repos.Entities.GetOneByGroup(ctx, ctx.GID, locationID)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if location.EntityType == nil || !location.EntityType.IsLocation {
		return AICaptureSessionOut{}, fmt.Errorf("%s: select a valid location", AICaptureErrorLocationMissing)
	}
	if err := svc.repos.AICaptureSessions.UpdateLocation(ctx, ctx.GID, ctx.UID, id, locationID, location.Name); err != nil {
		return AICaptureSessionOut{}, err
	}
	return svc.Get(ctx, id)
}

func cleanCaptureFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." {
		name = "capture.jpg"
	}
	return truncate(name, 255)
}

func (svc *AICaptureSessionService) AddPhoto(ctx Context, sessionID, clientPhotoID uuid.UUID, position int, captureGroupID *uuid.UUID, name, mimeType string, data []byte) (AICaptureSessionPhoto, error) {
	photoID, blobPath, hash := svc.repos.AICaptureSessions.NewPhotoStorage(data, ctx.GID, sessionID)
	record, existing, err := svc.repos.AICaptureSessions.CreatePhoto(
		ctx, ctx.GID, ctx.UID, sessionID, photoID, clientPhotoID, position,
		svc.ai.MaxSessionPhotos(), svc.ai.MaxPhotos(), captureGroupID,
		cleanCaptureFilename(name), mimeType, blobPath, int64(len(data)), hash,
	)
	if err != nil {
		return AICaptureSessionPhoto{}, err
	}
	if !existing {
		if err := svc.repos.AICaptureSessions.WriteBlob(ctx, blobPath, mimeType, data); err != nil {
			_, _ = svc.repos.AICaptureSessions.DeletePhotoRow(ctx, ctx.GID, ctx.UID, sessionID, photoID)
			return AICaptureSessionPhoto{}, err
		}
	}
	var groupValue *string
	if record.CaptureGroupID != nil {
		value := record.CaptureGroupID.String()
		groupValue = &value
	}
	return AICaptureSessionPhoto{
		ID: record.ID.String(), ClientPhotoID: record.ClientPhotoID.String(), Position: record.Position,
		CaptureGroupID: groupValue, OriginalName: record.OriginalName, MIMEType: record.MIMEType,
		SizeBytes: record.SizeBytes, CreatedAt: record.CreatedAt,
	}, nil
}

func (svc *AICaptureSessionService) UpdatePhotoGroup(ctx Context, sessionID, photoID uuid.UUID, captureGroupID *uuid.UUID) (AICaptureSessionPhoto, error) {
	record, err := svc.repos.AICaptureSessions.UpdatePhotoGroup(
		ctx, ctx.GID, ctx.UID, sessionID, photoID, svc.ai.MaxPhotos(), captureGroupID,
	)
	if err != nil {
		return AICaptureSessionPhoto{}, err
	}
	var groupValue *string
	if record.CaptureGroupID != nil {
		value := record.CaptureGroupID.String()
		groupValue = &value
	}
	return AICaptureSessionPhoto{
		ID: record.ID.String(), ClientPhotoID: record.ClientPhotoID.String(), Position: record.Position,
		CaptureGroupID: groupValue, OriginalName: record.OriginalName, MIMEType: record.MIMEType,
		SizeBytes: record.SizeBytes, CreatedAt: record.CreatedAt,
	}, nil
}

func (svc *AICaptureSessionService) DeletePhoto(ctx Context, sessionID, photoID uuid.UUID) error {
	record, err := svc.repos.AICaptureSessions.DeletePhotoRow(ctx, ctx.GID, ctx.UID, sessionID, photoID)
	if err != nil {
		return err
	}
	return svc.repos.AICaptureSessions.DeleteBlob(ctx, record.Path)
}

func (svc *AICaptureSessionService) ReadPhoto(ctx Context, sessionID, photoID uuid.UUID) (repo.AICapturePhotoRecord, []byte, error) {
	session, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, sessionID)
	if err != nil {
		return repo.AICapturePhotoRecord{}, nil, err
	}
	for _, photo := range session.Photos {
		if photo.ID == photoID {
			data, err := svc.repos.AICaptureSessions.ReadBlob(ctx, photo.Path)
			return photo, data, err
		}
	}
	return repo.AICapturePhotoRecord{}, nil, &ent.NotFoundError{}
}

func (svc *AICaptureSessionService) Finish(ctx Context, id uuid.UUID, expected, expectedRevision int) (AICaptureSessionOut, error) {
	if err := svc.repos.AICaptureSessions.Finish(ctx, ctx.GID, ctx.UID, id, expected, expectedRevision); err != nil {
		return AICaptureSessionOut{}, err
	}
	return svc.Get(ctx, id)
}

func (svc *AICaptureSessionService) RetryAnalysis(ctx Context, id uuid.UUID) (AICaptureSessionOut, error) {
	if err := svc.repos.AICaptureSessions.RetryAnalysis(ctx, ctx.GID, ctx.UID, id); err != nil {
		return AICaptureSessionOut{}, err
	}
	return svc.Get(ctx, id)
}

func (svc *AICaptureSessionService) metadata(ctx context.Context, record repo.AICaptureSessionRecord) (AICaptureContext, error) {
	if record.LocationID == nil {
		return AICaptureContext{}, fmt.Errorf("%s: the selected location no longer exists", AICaptureErrorLocationMissing)
	}
	entityTypes, err := svc.repos.EntityTypes.GetAll(ctx, record.GroupID)
	if err != nil {
		return AICaptureContext{}, err
	}
	tags, err := svc.repos.Tags.GetAll(ctx, record.GroupID)
	if err != nil {
		return AICaptureContext{}, err
	}
	metadata := AICaptureContext{
		Location: AICaptureOption{ID: record.LocationID.String(), Name: record.LocationNameSnapshot},
		Tags:     make([]AICaptureOption, 0, len(tags)),
	}
	for _, entityType := range entityTypes {
		if !entityType.IsLocation {
			metadata.EntityTypes = append(metadata.EntityTypes, AICaptureOption{ID: entityType.ID.String(), Name: entityType.Name})
		}
	}
	if len(metadata.EntityTypes) == 0 {
		return AICaptureContext{}, errors.New("create an item entity type before using AI capture")
	}
	for _, tag := range tags {
		metadata.Tags = append(metadata.Tags, AICaptureOption{ID: tag.ID.String(), Name: tag.Name})
	}
	return metadata, nil
}

func (svc *AICaptureSessionService) analysisPhotos(ctx context.Context, record repo.AICaptureSessionRecord) ([]AICapturePhoto, error) {
	return svc.analysisPhotosFor(ctx, record.Photos)
}

func (svc *AICaptureSessionService) analysisPhotosFor(ctx context.Context, records []repo.AICapturePhotoRecord) ([]AICapturePhoto, error) {
	photos := make([]AICapturePhoto, 0, len(records))
	for _, photo := range records {
		data, err := svc.repos.AICaptureSessions.ReadBlob(ctx, photo.Path)
		if err != nil {
			return nil, err
		}
		photos = append(photos, AICapturePhoto{MIMEType: photo.MIMEType, Data: data})
	}
	return photos, nil
}

type aiCaptureAnalysisBatch struct {
	CaptureGroupID string
	Photos         []repo.AICapturePhotoRecord
}

const aiCaptureMissingPhotoWarning = "Some photos could not be identified automatically and were added as separate items for review."

func partitionAICapturePhotos(photos []repo.AICapturePhotoRecord, maxUngrouped int) []aiCaptureAnalysisBatch {
	if maxUngrouped <= 0 {
		maxUngrouped = 8
	}
	batches := make([]aiCaptureAnalysisBatch, 0)
	batchByGroup := make(map[string]int)
	ungroupedBatch := -1
	for _, photo := range photos {
		groupID := ""
		if photo.CaptureGroupID != nil {
			groupID = photo.CaptureGroupID.String()
		}
		if groupID == "" {
			if ungroupedBatch < 0 || len(batches[ungroupedBatch].Photos) >= maxUngrouped {
				ungroupedBatch = len(batches)
				batches = append(batches, aiCaptureAnalysisBatch{})
			}
			batches[ungroupedBatch].Photos = append(batches[ungroupedBatch].Photos, photo)
			continue
		}
		batchIndex, exists := batchByGroup[groupID]
		if !exists {
			batchIndex = len(batches)
			batchByGroup[groupID] = batchIndex
			batches = append(batches, aiCaptureAnalysisBatch{CaptureGroupID: groupID})
		}
		batches[batchIndex].Photos = append(batches[batchIndex].Photos, photo)
	}
	return batches
}

func ensureUniqueAICaptureClientIDs(draft *AICaptureDraft) {
	used := make(map[string]struct{}, len(draft.Items))
	for index := range draft.Items {
		clientID := strings.TrimSpace(draft.Items[index].ClientID)
		if clientID == "" {
			clientID = fmt.Sprintf("item-%d", index+1)
		}
		if _, exists := used[clientID]; exists {
			for suffix := index + 1; ; suffix++ {
				candidate := fmt.Sprintf("item-%d", suffix)
				if _, taken := used[candidate]; !taken {
					clientID = candidate
					break
				}
			}
		}
		draft.Items[index].ClientID = clientID
		used[clientID] = struct{}{}
	}
}

func missingAICapturePhotoIndexes(draft AICaptureDraft, photoCount int) []int {
	assigned := make([]bool, photoCount)
	for _, item := range draft.Items {
		for _, index := range item.PhotoIndexes {
			if index >= 0 && index < photoCount {
				assigned[index] = true
			}
		}
	}
	missing := make([]int, 0)
	for index, covered := range assigned {
		if !covered {
			missing = append(missing, index)
		}
	}
	return missing
}

func appendAICaptureWarning(draft *AICaptureDraft, warning string) {
	if !slices.Contains(draft.Warnings, warning) {
		draft.Warnings = append(draft.Warnings, warning)
	}
}

func defaultAICaptureEntityTypeID(metadata AICaptureContext) string {
	if len(metadata.EntityTypes) == 0 {
		return ""
	}
	return metadata.EntityTypes[0].ID
}

// appendMissingAICapturePhotoPlaceholders is the final invariant at persistence
// boundaries: a provider or stale client draft must never make a capture photo
// disappear from review. Initial analysis tries the provider again first; this
// fallback keeps any remaining photo visible and individually reanalyzable.
func appendMissingAICapturePhotoPlaceholders(
	draft *AICaptureDraft,
	photos []repo.AICapturePhotoRecord,
	metadata AICaptureContext,
) int {
	assigned := make(map[string]struct{}, len(photos))
	for _, item := range draft.Items {
		for _, photoID := range item.PhotoIDs {
			assigned[photoID] = struct{}{}
		}
	}
	added := 0
	for _, photo := range photos {
		photoID := photo.ID.String()
		if _, covered := assigned[photoID]; covered {
			continue
		}
		draft.Items = append(draft.Items, AICaptureItem{
			ClientID:        "unidentified-photo-" + photoID,
			Name:            fmt.Sprintf("Unidentified item (photo %d)", photo.Position+1),
			Quantity:        1,
			EntityTypeID:    defaultAICaptureEntityTypeID(metadata),
			TagIDs:          []string{},
			PhotoIDs:        []string{photoID},
			MoveDisposition: AICaptureMoveDispositionUndecided,
			NeedsReview:     true,
			ReviewReason:    "The vision provider omitted this photo. Identify the item or resubmit it to AI.",
		})
		assigned[photoID] = struct{}{}
		added++
	}
	if added > 0 {
		appendAICaptureWarning(draft, aiCaptureMissingPhotoWarning)
		ensureUniqueAICaptureClientIDs(draft)
	}
	return added
}

func sortAICaptureItemsByFirstPhoto(items []AICaptureItem, photos []repo.AICapturePhotoRecord) {
	positions := make(map[string]int, len(photos))
	for _, photo := range photos {
		positions[photo.ID.String()] = photo.Position
	}
	firstPosition := func(item AICaptureItem) int {
		first := int(^uint(0) >> 1)
		for _, photoID := range item.PhotoIDs {
			if position, ok := positions[photoID]; ok && position < first {
				first = position
			}
		}
		return first
	}
	slices.SortStableFunc(items, func(left, right AICaptureItem) int {
		return firstPosition(left) - firstPosition(right)
	})
}

func (svc *AICaptureSessionService) analyzeSession(ctx context.Context, record repo.AICaptureSessionRecord, metadata AICaptureContext) (AICaptureDraft, error) {
	out := AICaptureDraft{Items: []AICaptureItem{}, Warnings: []string{}}
	for _, batch := range partitionAICapturePhotos(record.Photos, svc.ai.MaxPhotos()) {
		photos, err := svc.analysisPhotosFor(ctx, batch.Photos)
		if err != nil {
			return AICaptureDraft{}, err
		}
		request := AICaptureRequest{Photos: photos, Context: metadata, CaptureGroupID: batch.CaptureGroupID}
		draft, err := svc.ai.Analyze(ctx, request)
		if err != nil && batch.CaptureGroupID != "" && errors.Is(err, ErrAIGroupContract) {
			request.Instruction = "Your previous response violated the same-item contract. Return exactly one item and copy captureGroupId exactly."
			draft, err = svc.ai.Analyze(ctx, request)
		}
		if err != nil {
			return AICaptureDraft{}, err
		}
		missing := missingAICapturePhotoIndexes(draft, len(batch.Photos))
		mapDraftPhotoIDs(&draft, batch.Photos)
		if batch.CaptureGroupID == "" {
			for _, missingIndex := range missing {
				recovered, recoverErr := svc.ai.Analyze(ctx, AICaptureRequest{
					Photos:     []AICapturePhoto{photos[missingIndex]},
					Context:    metadata,
					SingleItem: true,
					Instruction: "This separately captured inventory photo was omitted from the previous multi-photo result. " +
						"Return exactly one item for this photo.",
				})
				if recoverErr != nil {
					if errors.Is(recoverErr, ErrAIRetryable) {
						return AICaptureDraft{}, recoverErr
					}
					continue
				}
				mapDraftPhotoIDs(&recovered, batch.Photos[missingIndex:missingIndex+1])
				draft.Items = append(draft.Items, recovered.Items...)
				draft.Warnings = append(draft.Warnings, recovered.Warnings...)
			}
		}
		appendMissingAICapturePhotoPlaceholders(&draft, batch.Photos, metadata)
		if batch.CaptureGroupID != "" {
			draft.Items[0].ClientID = "group-" + batch.CaptureGroupID
		}
		out.Items = append(out.Items, draft.Items...)
		out.Warnings = append(out.Warnings, draft.Warnings...)
	}
	maxItems := svc.maxSessionItems()
	if len(out.Items) > maxItems {
		return AICaptureDraft{}, fmt.Errorf("%w: grouped analysis returned %d items, maximum is %d", ErrAIUpstream, len(out.Items), maxItems)
	}
	sortAICaptureItemsByFirstPhoto(out.Items, record.Photos)
	ensureUniqueAICaptureClientIDs(&out)
	return out, nil
}

func (svc *AICaptureSessionService) maxSessionItems() int {
	maxItems := svc.config.MaxItems
	if maxItems <= 0 {
		maxItems = 25
	}
	providerBatchSize := max(1, svc.ai.MaxPhotos())
	maxAnalysisBatches := max(1, (svc.ai.MaxSessionPhotos()+providerBatchSize-1)/providerBatchSize)
	return maxItems * maxAnalysisBatches
}

func mapDraftPhotoIDs(draft *AICaptureDraft, photos []repo.AICapturePhotoRecord) {
	for i := range draft.Items {
		item := &draft.Items[i]
		item.PhotoIDs = item.PhotoIDs[:0]
		for _, index := range item.PhotoIndexes {
			if index >= 0 && index < len(photos) {
				item.PhotoIDs = append(item.PhotoIDs, photos[index].ID.String())
			}
		}
		item.PhotoIndexes = nil
	}
}

func mapDraftPhotoIndexes(draft *AICaptureDraft, photos []repo.AICapturePhotoRecord) {
	indexes := make(map[string]int, len(photos))
	for index, photo := range photos {
		indexes[photo.ID.String()] = index
	}
	for i := range draft.Items {
		item := &draft.Items[i]
		item.PhotoIndexes = item.PhotoIndexes[:0]
		for _, id := range item.PhotoIDs {
			if index, ok := indexes[id]; ok {
				item.PhotoIndexes = append(item.PhotoIndexes, index)
			}
		}
	}
}

func validateDraftCaptureGroups(draft *AICaptureDraft, photos []repo.AICapturePhotoRecord) {
	groupPhotos := make(map[string]map[string]struct{})
	for _, photo := range photos {
		if photo.CaptureGroupID == nil {
			continue
		}
		groupID := photo.CaptureGroupID.String()
		if groupPhotos[groupID] == nil {
			groupPhotos[groupID] = make(map[string]struct{})
		}
		groupPhotos[groupID][photo.ID.String()] = struct{}{}
	}
	usedGroups := make(map[string]struct{})
	for index := range draft.Items {
		item := &draft.Items[index]
		if item.CaptureGroupID == "" {
			continue
		}
		expected, valid := groupPhotos[item.CaptureGroupID]
		_, duplicate := usedGroups[item.CaptureGroupID]
		if !valid || duplicate || len(expected) != len(item.PhotoIDs) {
			item.CaptureGroupID = ""
			continue
		}
		matches := true
		for _, photoID := range item.PhotoIDs {
			if _, ok := expected[photoID]; !ok {
				matches = false
				break
			}
		}
		if !matches {
			item.CaptureGroupID = ""
			continue
		}
		usedGroups[item.CaptureGroupID] = struct{}{}
	}
}

func (svc *AICaptureSessionService) RunNextAnalysis(ctx context.Context) error {
	if svc.ai == nil || !svc.ai.IsEnabled() {
		return nil
	}
	lease := svc.config.Timeout + time.Minute
	if lease < 3*time.Minute {
		lease = 3 * time.Minute
	}
	record, found, err := svc.repos.AICaptureSessions.ClaimQueued(ctx, lease)
	if err != nil || !found {
		return err
	}
	metadata, err := svc.metadata(ctx, record)
	if err == nil {
		var draft AICaptureDraft
		draft, err = svc.analyzeSession(ctx, record, metadata)
		if err == nil {
			encoded, marshalErr := json.Marshal(draft)
			if marshalErr != nil {
				err = marshalErr
			} else {
				err = svc.repos.AICaptureSessions.SetAnalysisReady(ctx, record.ID, string(encoded))
			}
		}
	}
	if err == nil {
		return nil
	}
	log.Error().Err(err).Str("session_id", record.ID.String()).Msg("AI capture session analysis failed")
	retry := errors.Is(err, ErrAIRetryable)
	var nextAttemptAt *time.Time
	code := AICaptureErrorAnalysisFailed
	message := "The vision provider could not analyze this session. Try again."
	if retry {
		next := time.Now().Add(aiRetryDelay(record.AnalysisAttempts))
		nextAttemptAt = &next
		message = "The vision provider is unavailable. Analysis will resume automatically."
	}
	if strings.Contains(err.Error(), AICaptureErrorLocationMissing) {
		code = AICaptureErrorLocationMissing
		message = "The selected location no longer exists. Choose another location and retry."
		retry = false
		nextAttemptAt = nil
	}
	return svc.repos.AICaptureSessions.SetAnalysisError(ctx, record.ID, retry, nextAttemptAt, code, message)
}

func aiRetryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}
	if attempt <= 1 {
		return delays[0]
	}
	return delays[min(attempt-1, len(delays)-1)]
}

func (svc *AICaptureSessionService) validateSessionDraft(ctx context.Context, record repo.AICaptureSessionRecord, draft AICaptureDraft) (AICaptureDraft, error) {
	metadata, err := svc.metadata(ctx, record)
	if err != nil {
		return AICaptureDraft{}, err
	}
	mapDraftPhotoIndexes(&draft, record.Photos)
	draft, err = sanitizeAICaptureDraft(draft, metadata, len(record.Photos), svc.maxSessionItems())
	if err != nil {
		return AICaptureDraft{}, err
	}
	mapDraftPhotoIDs(&draft, record.Photos)
	appendMissingAICapturePhotoPlaceholders(&draft, record.Photos, metadata)
	validateDraftCaptureGroups(&draft, record.Photos)
	return draft, nil
}

func completedAICaptureClientIDs(items []repo.AICaptureSessionItemRecord) map[string]struct{} {
	completed := make(map[string]struct{})
	for _, item := range items {
		if item.Status == aicapturesessionitem.StatusCompleted.String() {
			completed[item.ClientID] = struct{}{}
		}
	}
	return completed
}

func preserveCompletedAICaptureItems(record repo.AICaptureSessionRecord, incoming AICaptureDraft) (AICaptureDraft, error) {
	completed := completedAICaptureClientIDs(record.Items)
	if len(completed) == 0 {
		return incoming, nil
	}
	var saved AICaptureDraft
	if err := json.Unmarshal([]byte(record.DraftJSON), &saved); err != nil {
		return AICaptureDraft{}, err
	}
	incomingByID := make(map[string]AICaptureItem, len(incoming.Items))
	for _, item := range incoming.Items {
		incomingByID[item.ClientID] = item
	}
	merged := make([]AICaptureItem, 0, len(incoming.Items)+len(completed))
	used := make(map[string]struct{}, len(incoming.Items)+len(completed))
	for _, item := range saved.Items {
		if _, done := completed[item.ClientID]; done {
			merged = append(merged, item)
			used[item.ClientID] = struct{}{}
			continue
		}
		if updated, exists := incomingByID[item.ClientID]; exists {
			merged = append(merged, updated)
			used[item.ClientID] = struct{}{}
		}
	}
	for _, item := range incoming.Items {
		if _, exists := used[item.ClientID]; !exists {
			merged = append(merged, item)
		}
	}
	incoming.Items = merged
	return incoming, nil
}

func (svc *AICaptureSessionService) SaveDraft(ctx Context, id uuid.UUID, expectedRevision int, draft AICaptureDraft) (AICaptureSessionOut, error) {
	record, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	draft, err = preserveCompletedAICaptureItems(record, draft)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	draft, err = svc.validateSessionDraft(ctx, record, draft)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if err := svc.repos.AICaptureSessions.SaveDraft(ctx, ctx.GID, ctx.UID, id, expectedRevision, string(encoded)); err != nil {
		return AICaptureSessionOut{}, err
	}
	return svc.Get(ctx, id)
}

// ReanalyzeItem creates a provider suggestion without mutating the persisted
// draft. Every photo currently assigned to the reviewed item is sent together,
// and the one-item/grouping contract is enforced independently of the model.
func (svc *AICaptureSessionService) ReanalyzeItem(
	ctx Context,
	id uuid.UUID,
	expectedRevision int,
	clientID, providerID, instruction string,
) (AICaptureReanalysisOut, error) {
	record, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureReanalysisOut{}, err
	}
	if record.Status != "ready_for_review" || record.DraftRevision != expectedRevision {
		return AICaptureReanalysisOut{}, repo.ErrAICaptureDraftConflict
	}

	var current AICaptureDraft
	if err := json.Unmarshal([]byte(record.DraftJSON), &current); err != nil {
		return AICaptureReanalysisOut{}, err
	}
	itemIndex := slices.IndexFunc(current.Items, func(item AICaptureItem) bool {
		return item.ClientID == strings.TrimSpace(clientID)
	})
	if itemIndex < 0 {
		return AICaptureReanalysisOut{}, fmt.Errorf("%w: reviewed item was not found", ErrAIInvalidRequest)
	}
	return svc.reanalyzeItemRecord(ctx, record, current.Items[itemIndex], providerID, instruction)
}

func (svc *AICaptureSessionService) reanalyzeItemRecord(
	ctx context.Context,
	record repo.AICaptureSessionRecord,
	original AICaptureItem,
	providerID, instruction string,
) (AICaptureReanalysisOut, error) {
	photoIDs := make(map[string]struct{}, len(original.PhotoIDs))
	for _, photoID := range original.PhotoIDs {
		photoIDs[photoID] = struct{}{}
	}
	selectedRecords := make([]repo.AICapturePhotoRecord, 0, len(photoIDs))
	for _, photo := range record.Photos {
		if _, ok := photoIDs[photo.ID.String()]; ok {
			selectedRecords = append(selectedRecords, photo)
		}
	}
	if len(selectedRecords) == 0 || len(selectedRecords) != len(photoIDs) {
		return AICaptureReanalysisOut{}, fmt.Errorf("%w: reviewed item must have available assigned photos", ErrAIInvalidRequest)
	}
	photos, err := svc.analysisPhotosFor(ctx, selectedRecords)
	if err != nil {
		return AICaptureReanalysisOut{}, err
	}
	metadata, err := svc.metadata(ctx, record)
	if err != nil {
		return AICaptureReanalysisOut{}, err
	}

	promptDraft := AICaptureDraft{Items: []AICaptureItem{original}, Warnings: []string{}}
	mapDraftPhotoIndexes(&promptDraft, selectedRecords)
	promptDraft.Items[0].PhotoIDs = nil
	providerID = strings.TrimSpace(strings.ToLower(providerID))
	if providerID == "" {
		providerID = AICaptureProviderDefault
	}
	suggested, err := svc.ai.AnalyzeWithProvider(ctx, providerID, AICaptureRequest{
		Photos: photos, Context: metadata, Draft: &promptDraft,
		Instruction:    truncate(strings.TrimSpace(instruction), 2000),
		CaptureGroupID: original.CaptureGroupID, SingleItem: true,
	})
	if err != nil {
		return AICaptureReanalysisOut{}, err
	}
	mapDraftPhotoIDs(&suggested, selectedRecords)
	item := suggested.Items[0]
	item.ClientID = original.ClientID
	item.CaptureGroupID = original.CaptureGroupID
	item.MoveDisposition = original.MoveDisposition
	item.MoveDispositionNote = original.MoveDispositionNote
	item.PhotoIDs = slices.Clone(original.PhotoIDs)
	item.PhotoIndexes = nil
	return AICaptureReanalysisOut{Item: item, Provider: providerID, Warnings: suggested.Warnings}, nil
}

func (svc *AICaptureSessionService) QueueReanalysis(
	ctx Context,
	id uuid.UUID,
	expectedRevision int,
	clientIDs []string,
	providerID, instruction string,
) (AICaptureSessionOut, error) {
	record, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if record.Status != "ready_for_review" || record.DraftRevision != expectedRevision {
		return AICaptureSessionOut{}, repo.ErrAICaptureDraftConflict
	}
	providerID = strings.TrimSpace(strings.ToLower(providerID))
	if providerID == "" {
		providerID = AICaptureProviderDefault
	}
	if _, err := svc.ai.provider(providerID); err != nil {
		return AICaptureSessionOut{}, err
	}
	maxItems := svc.maxSessionItems()
	if len(clientIDs) == 0 || len(clientIDs) > maxItems {
		return AICaptureSessionOut{}, fmt.Errorf("%w: select between 1 and %d reviewed items", ErrAIInvalidRequest, maxItems)
	}
	if len(instruction) > 2000 {
		return AICaptureSessionOut{}, fmt.Errorf("%w: instruction must be at most 2000 characters", ErrAIInvalidRequest)
	}
	var draft AICaptureDraft
	if err := json.Unmarshal([]byte(record.DraftJSON), &draft); err != nil {
		return AICaptureSessionOut{}, err
	}
	seen := make(map[string]struct{}, len(clientIDs))
	items := make([]AICaptureItem, 0, len(clientIDs))
	for _, requestedID := range clientIDs {
		clientID := strings.TrimSpace(requestedID)
		if clientID == "" {
			return AICaptureSessionOut{}, fmt.Errorf("%w: every clientId is required", ErrAIInvalidRequest)
		}
		if _, exists := seen[clientID]; exists {
			continue
		}
		seen[clientID] = struct{}{}
		itemIndex := slices.IndexFunc(draft.Items, func(item AICaptureItem) bool { return item.ClientID == clientID })
		if itemIndex < 0 {
			return AICaptureSessionOut{}, fmt.Errorf("%w: reviewed item %s was not found", ErrAIInvalidRequest, clientID)
		}
		if len(draft.Items[itemIndex].PhotoIDs) == 0 {
			return AICaptureSessionOut{}, fmt.Errorf("%w: reviewed item %s has no assigned photos", ErrAIInvalidRequest, clientID)
		}
		if len(draft.Items[itemIndex].PhotoIDs) > svc.ai.MaxPhotos() {
			return AICaptureSessionOut{}, fmt.Errorf(
				"%w: reviewed item %s has %d assigned photos; reduce it to %d before reanalysis",
				ErrAIInvalidRequest, clientID, len(draft.Items[itemIndex].PhotoIDs), svc.ai.MaxPhotos(),
			)
		}
		items = append(items, draft.Items[itemIndex])
	}
	now := time.Now()
	state := aiCaptureReanalysisState{
		Status: repo.AICaptureReanalysisQueued, Provider: providerID,
		Instruction: strings.TrimSpace(instruction), Revision: expectedRevision,
		Items: items, Suggestions: map[string]AICaptureReanalysisOut{}, Errors: map[string]string{},
		CreatedAt: now, UpdatedAt: now,
	}
	if record.ReanalysisJSON != "" {
		var previous aiCaptureReanalysisState
		if json.Unmarshal([]byte(record.ReanalysisJSON), &previous) == nil {
			state.Suggestions = previous.Suggestions
			state.Errors = previous.Errors
			if state.Suggestions == nil {
				state.Suggestions = map[string]AICaptureReanalysisOut{}
			}
			if state.Errors == nil {
				state.Errors = map[string]string{}
			}
		}
	}
	for _, item := range items {
		delete(state.Suggestions, item.ClientID)
		delete(state.Errors, item.ClientID)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if err := svc.repos.AICaptureSessions.QueueReanalysis(ctx, ctx.GID, ctx.UID, id, expectedRevision, string(encoded)); err != nil {
		return AICaptureSessionOut{}, err
	}
	return svc.Get(ctx, id)
}

func (svc *AICaptureSessionService) DismissReanalysis(
	ctx Context,
	id uuid.UUID,
	clientID string,
) (AICaptureSessionOut, error) {
	record, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if record.ReanalysisStatus != repo.AICaptureReanalysisCompleted || record.ReanalysisJSON == "" {
		return AICaptureSessionOut{}, repo.ErrAICaptureInvalidState
	}
	var state aiCaptureReanalysisState
	if err := json.Unmarshal([]byte(record.ReanalysisJSON), &state); err != nil {
		return AICaptureSessionOut{}, err
	}
	clientID = strings.TrimSpace(clientID)
	delete(state.Suggestions, clientID)
	delete(state.Errors, clientID)
	state.UpdatedAt = time.Now()
	encoded, err := json.Marshal(state)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if err := svc.repos.AICaptureSessions.SaveReanalysisResults(ctx, ctx.GID, ctx.UID, id, string(encoded)); err != nil {
		return AICaptureSessionOut{}, err
	}
	return svc.Get(ctx, id)
}

func (svc *AICaptureSessionService) RunNextReanalysis(ctx context.Context) error {
	if svc.ai == nil || !svc.ai.IsEnabled() {
		return nil
	}
	lease := svc.config.Timeout + time.Minute
	if lease < 3*time.Minute {
		lease = 3 * time.Minute
	}
	record, found, err := svc.repos.AICaptureSessions.ClaimQueuedReanalysis(ctx, lease)
	if err != nil || !found {
		return err
	}
	var state aiCaptureReanalysisState
	if err := json.Unmarshal([]byte(record.ReanalysisJSON), &state); err != nil {
		return svc.finishBrokenReanalysis(ctx, record.ID, "The saved reanalysis request could not be read.")
	}
	if state.Current < 0 || state.Current >= len(state.Items) {
		return svc.finishBrokenReanalysis(ctx, record.ID, "The saved reanalysis request has no pending item.")
	}
	state.Status = repo.AICaptureReanalysisProcessing
	state.Attempts++
	state.UpdatedAt = time.Now()
	clientID := state.Items[state.Current].ClientID
	suggestion, analyzeErr := svc.reanalyzeItemRecord(ctx, record, state.Items[state.Current], state.Provider, state.Instruction)
	if analyzeErr == nil {
		if state.Suggestions == nil {
			state.Suggestions = map[string]AICaptureReanalysisOut{}
		}
		if state.Errors == nil {
			state.Errors = map[string]string{}
		}
		state.Suggestions[clientID] = suggestion
		delete(state.Errors, clientID)
		state.Current++
		state.Attempts = 0
		return svc.advanceReanalysis(ctx, record.ID, &state)
	}
	log.Error().Err(analyzeErr).Str("session_id", record.ID.String()).Str("client_id", clientID).Msg("AI capture item reanalysis failed")
	if errors.Is(analyzeErr, ErrAIRetryable) {
		next := time.Now().Add(aiRetryDelay(state.Attempts))
		state.Status = repo.AICaptureReanalysisWaiting
		state.UpdatedAt = time.Now()
		encoded, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return svc.repos.AICaptureSessions.SetReanalysisState(ctx, record.ID, state.Status, string(encoded), &next)
	}
	if state.Errors == nil {
		state.Errors = map[string]string{}
	}
	state.Errors[clientID] = "The vision provider could not analyze this item."
	delete(state.Suggestions, clientID)
	state.Current++
	state.Attempts = 0
	return svc.advanceReanalysis(ctx, record.ID, &state)
}

func (svc *AICaptureSessionService) advanceReanalysis(ctx context.Context, id uuid.UUID, state *aiCaptureReanalysisState) error {
	state.Status = repo.AICaptureReanalysisQueued
	state.UpdatedAt = time.Now()
	if state.Current >= len(state.Items) {
		state.Status = repo.AICaptureReanalysisCompleted
		completedAt := state.UpdatedAt
		state.CompletedAt = &completedAt
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return svc.repos.AICaptureSessions.SetReanalysisState(ctx, id, state.Status, string(encoded), nil)
}

func (svc *AICaptureSessionService) finishBrokenReanalysis(ctx context.Context, id uuid.UUID, message string) error {
	now := time.Now()
	state := aiCaptureReanalysisState{
		Status: repo.AICaptureReanalysisCompleted, Suggestions: map[string]AICaptureReanalysisOut{},
		Errors: map[string]string{"_job": message}, CreatedAt: now, UpdatedAt: now, CompletedAt: &now,
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return svc.repos.AICaptureSessions.SetReanalysisState(ctx, id, state.Status, string(encoded), nil)
}

func (svc *AICaptureSessionService) Correct(ctx Context, id uuid.UUID, expectedRevision int, instruction string) (AICaptureSessionOut, error) {
	record, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if record.Status != "ready_for_review" || record.DraftRevision != expectedRevision {
		return AICaptureSessionOut{}, repo.ErrAICaptureDraftConflict
	}
	if record.ReanalysisStatus == repo.AICaptureReanalysisQueued ||
		record.ReanalysisStatus == repo.AICaptureReanalysisProcessing ||
		record.ReanalysisStatus == repo.AICaptureReanalysisWaiting {
		return AICaptureSessionOut{}, repo.ErrAICaptureReanalysisActive
	}
	var current AICaptureDraft
	if err := json.Unmarshal([]byte(record.DraftJSON), &current); err != nil {
		return AICaptureSessionOut{}, err
	}
	metadata, err := svc.metadata(ctx, record)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	photos, err := svc.analysisPhotos(ctx, record)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	mapDraftPhotoIndexes(&current, record.Photos)
	groupsByClientID := make(map[string]string, len(current.Items))
	movePlansByClientID := make(map[string][2]string, len(current.Items))
	allowedGroups := make([]string, 0)
	seenGroups := make(map[string]struct{})
	for _, item := range current.Items {
		groupsByClientID[item.ClientID] = item.CaptureGroupID
		movePlansByClientID[item.ClientID] = [2]string{item.MoveDisposition, item.MoveDispositionNote}
		if item.CaptureGroupID != "" {
			if _, seen := seenGroups[item.CaptureGroupID]; !seen {
				seenGroups[item.CaptureGroupID] = struct{}{}
				allowedGroups = append(allowedGroups, item.CaptureGroupID)
			}
		}
	}
	corrected, err := svc.ai.Analyze(ctx, AICaptureRequest{
		Photos: photos, Context: metadata, Draft: &current, Instruction: truncate(strings.TrimSpace(instruction), 2000),
		AllowedCaptureGroups: allowedGroups,
	})
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	for index := range corrected.Items {
		if corrected.Items[index].CaptureGroupID == "" {
			corrected.Items[index].CaptureGroupID = groupsByClientID[corrected.Items[index].ClientID]
		}
		if movePlan, ok := movePlansByClientID[corrected.Items[index].ClientID]; ok {
			corrected.Items[index].MoveDisposition = movePlan[0]
			corrected.Items[index].MoveDispositionNote = movePlan[1]
		}
	}
	mapDraftPhotoIDs(&corrected, record.Photos)
	corrected, err = preserveCompletedAICaptureItems(record, corrected)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	appendMissingAICapturePhotoPlaceholders(&corrected, record.Photos, metadata)
	validateDraftCaptureGroups(&corrected, record.Photos)
	encoded, err := json.Marshal(corrected)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if err := svc.repos.AICaptureSessions.SaveCorrection(ctx, ctx.GID, ctx.UID, id, expectedRevision, string(encoded)); err != nil {
		return AICaptureSessionOut{}, err
	}
	return svc.Get(ctx, id)
}

func decodeUploadedPhotoIDs(value string) map[string]struct{} {
	ids := []string{}
	_ = json.Unmarshal([]byte(value), &ids)
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

func encodeUploadedPhotoIDs(ids map[string]struct{}) string {
	values := make([]string, 0, len(ids))
	for id := range ids {
		values = append(values, id)
	}
	slices.Sort(values)
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

func aiCaptureMoveFields(item AICaptureItem) []repo.EntityFieldData {
	dispositionLabels := map[string]string{
		AICaptureMoveDispositionUndecided: "Undecided",
		AICaptureMoveDispositionKeep:      "Keep for move",
		AICaptureMoveDispositionSell:      "Sell",
		AICaptureMoveDispositionGiveAway:  "Give away",
		AICaptureMoveDispositionDonate:    "Donate",
		AICaptureMoveDispositionRecycle:   "Recycle",
		AICaptureMoveDispositionTrash:     "Trash",
	}
	disposition := sanitizeMoveDisposition(item.MoveDisposition)
	fields := []repo.EntityFieldData{{
		Type: "text", Name: "Move disposition", TextValue: dispositionLabels[disposition],
	}}
	if note := truncate(strings.TrimSpace(item.MoveDispositionNote), 500); note != "" {
		fields = append(fields, repo.EntityFieldData{
			Type: "text", Name: "Move planning notes", TextValue: note,
		})
	}
	return fields
}

func capturePhotoAttachmentCounts(entity repo.EntityOut) map[string]int {
	counts := make(map[string]int)
	for _, itemAttachment := range entity.Attachments {
		if itemAttachment.Type == attachment.TypePhoto.String() {
			counts[itemAttachment.Title]++
		}
	}
	return counts
}

func (svc *AICaptureSessionService) submitItem(ctx Context, session repo.AICaptureSessionRecord, item AICaptureItem) error {
	progress, err := svc.repos.AICaptureSessions.GetOrCreateSubmissionItem(ctx, session.ID, item.ClientID)
	if err != nil {
		return err
	}
	if progress.Status == aicapturesessionitem.StatusCompleted.String() {
		return nil
	}
	entityID := progress.EntityID
	if entityID == nil {
		entityTypeID, err := uuid.Parse(item.EntityTypeID)
		if err != nil {
			return err
		}
		tagIDs := make([]uuid.UUID, 0, len(item.TagIDs))
		for _, value := range item.TagIDs {
			id, err := uuid.Parse(value)
			if err != nil {
				return err
			}
			tagIDs = append(tagIDs, id)
		}
		if session.LocationID == nil {
			return errors.New("selected location no longer exists")
		}
		created, err := svc.entities.Create(ctx, repo.EntityCreate{
			ParentID: *session.LocationID, Name: item.Name, Quantity: item.Quantity, Description: item.Description,
			Manufacturer: item.Manufacturer, ModelNumber: item.ModelNumber, EntityTypeID: entityTypeID, TagIDs: tagIDs,
			Fields: aiCaptureMoveFields(item),
		})
		if err != nil {
			return err
		}
		entityID = &created.ID
		if err := svc.repos.AICaptureSessions.SetSubmissionItemEntity(ctx, session.ID, item.ClientID, created.ID); err != nil {
			return err
		}
	}

	photoByID := make(map[string]repo.AICapturePhotoRecord, len(session.Photos))
	for _, photo := range session.Photos {
		photoByID[photo.ID.String()] = photo
	}
	uploaded := decodeUploadedPhotoIDs(progress.UploadedPhotoIDs)
	recordedAttachmentCounts := make(map[string]int)
	for photoID := range uploaded {
		if photo, ok := photoByID[photoID]; ok {
			recordedAttachmentCounts[photo.OriginalName]++
		}
	}
	entity, err := svc.repos.Entities.GetOneByGroup(ctx, ctx.GID, *entityID)
	if err != nil {
		return err
	}
	existingAttachmentCounts := capturePhotoAttachmentCounts(entity)
	for index, photoID := range item.PhotoIDs {
		if _, ok := uploaded[photoID]; ok {
			continue
		}
		photo, ok := photoByID[photoID]
		if !ok {
			return fmt.Errorf("draft photo %s is unavailable", photoID)
		}
		if recordedAttachmentCounts[photo.OriginalName] < existingAttachmentCounts[photo.OriginalName] {
			uploaded[photoID] = struct{}{}
			recordedAttachmentCounts[photo.OriginalName]++
			if err := svc.repos.AICaptureSessions.SetSubmissionItemPhotos(ctx, session.ID, item.ClientID, encodeUploadedPhotoIDs(uploaded)); err != nil {
				return err
			}
			if err := svc.repos.AICaptureSessions.TouchSubmitting(ctx, session.ID); err != nil {
				return err
			}
			continue
		}
		data, err := svc.repos.AICaptureSessions.ReadBlob(ctx, photo.Path)
		if err != nil {
			return err
		}
		if _, err := svc.entities.AttachmentAdd(ctx, *entityID, photo.OriginalName, attachment.TypePhoto, index == 0, bytes.NewReader(data)); err != nil {
			return err
		}
		existingAttachmentCounts[photo.OriginalName]++
		uploaded[photoID] = struct{}{}
		recordedAttachmentCounts[photo.OriginalName]++
		if err := svc.repos.AICaptureSessions.SetSubmissionItemPhotos(ctx, session.ID, item.ClientID, encodeUploadedPhotoIDs(uploaded)); err != nil {
			return err
		}
		if err := svc.repos.AICaptureSessions.TouchSubmitting(ctx, session.ID); err != nil {
			return err
		}
	}
	return svc.repos.AICaptureSessions.SetSubmissionItemStatus(ctx, session.ID, item.ClientID, aicapturesessionitem.StatusCompleted, "")
}

func (svc *AICaptureSessionService) RecoverInterruptedSubmissions(ctx context.Context) error {
	count, err := svc.repos.AICaptureSessions.RecoverInterruptedSubmissions(
		ctx,
		time.Now().Add(-aiCaptureSubmissionStaleAfter),
		AICaptureErrorSubmitInterrupted,
		"Item creation was interrupted. Review the remaining items and retry; completed work will not be duplicated.",
	)
	if err == nil && count > 0 {
		log.Warn().Int("count", count).Msg("recovered interrupted AI capture submissions")
	}
	return err
}

func (svc *AICaptureSessionService) SubmitItems(ctx Context, id uuid.UUID, expectedRevision int, clientIDs []string) (AICaptureSessionOut, error) {
	session, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if session.LocationID == nil {
		return AICaptureSessionOut{}, fmt.Errorf("%s: select a replacement location", AICaptureErrorLocationMissing)
	}
	if session.ReanalysisStatus == repo.AICaptureReanalysisQueued ||
		session.ReanalysisStatus == repo.AICaptureReanalysisProcessing ||
		session.ReanalysisStatus == repo.AICaptureReanalysisWaiting {
		return AICaptureSessionOut{}, repo.ErrAICaptureReanalysisActive
	}
	var draft AICaptureDraft
	if err := json.Unmarshal([]byte(session.DraftJSON), &draft); err != nil {
		return AICaptureSessionOut{}, err
	}
	requested := make(map[string]struct{}, len(clientIDs))
	for _, clientID := range clientIDs {
		if value := strings.TrimSpace(clientID); value != "" {
			requested[value] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return AICaptureSessionOut{}, fmt.Errorf("%w: select at least one reviewed item", ErrAIInvalidRequest)
	}
	selected := make([]AICaptureItem, 0, len(requested))
	for _, item := range draft.Items {
		if _, ok := requested[item.ClientID]; ok {
			selected = append(selected, item)
			delete(requested, item.ClientID)
		}
	}
	if len(requested) > 0 {
		return AICaptureSessionOut{}, fmt.Errorf("%w: a selected reviewed item was not found", ErrAIInvalidRequest)
	}
	if err := svc.repos.AICaptureSessions.StartSubmitting(ctx, ctx.GID, ctx.UID, id, expectedRevision); err != nil {
		return AICaptureSessionOut{}, err
	}
	for _, item := range selected {
		if err := svc.repos.AICaptureSessions.TouchSubmitting(ctx, session.ID); err != nil {
			return AICaptureSessionOut{}, err
		}
		if err := svc.submitItem(ctx, session, item); err != nil {
			_ = svc.repos.AICaptureSessions.SetSubmissionItemStatus(ctx, session.ID, item.ClientID, aicapturesessionitem.StatusFailed, AICaptureErrorSubmitFailed)
			_ = svc.repos.AICaptureSessions.SetSubmitFailed(ctx, id, AICaptureErrorSubmitFailed, "Some items or photos could not be saved. Retry is safe.")
			return svc.Get(ctx, id)
		}
		if err := svc.repos.AICaptureSessions.TouchSubmitting(ctx, session.ID); err != nil {
			return AICaptureSessionOut{}, err
		}
	}
	updated, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	completedClientIDs := completedAICaptureClientIDs(updated.Items)
	allCompleted := len(draft.Items) > 0
	for _, item := range draft.Items {
		if _, ok := completedClientIDs[item.ClientID]; !ok {
			allCompleted = false
			break
		}
	}
	if !allCompleted {
		if err := svc.repos.AICaptureSessions.SetReadyAfterPartialSubmit(ctx, id); err != nil {
			return AICaptureSessionOut{}, err
		}
		return svc.Get(ctx, id)
	}
	if err := svc.repos.AICaptureSessions.SetCompleted(ctx, id); err != nil {
		return AICaptureSessionOut{}, err
	}
	completedSession, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if err := svc.repos.AICaptureSessions.PurgeSessionPhotos(ctx, completedSession); err != nil {
		log.Warn().Err(err).Str("session_id", id.String()).Msg("failed to purge completed capture photos")
	}
	return svc.Get(ctx, id)
}

func (svc *AICaptureSessionService) Submit(ctx Context, id uuid.UUID, expectedRevision int) (AICaptureSessionOut, error) {
	session, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	var draft AICaptureDraft
	if err := json.Unmarshal([]byte(session.DraftJSON), &draft); err != nil {
		return AICaptureSessionOut{}, err
	}
	clientIDs := make([]string, 0, len(draft.Items))
	for _, item := range draft.Items {
		clientIDs = append(clientIDs, item.ClientID)
	}
	return svc.SubmitItems(ctx, id, expectedRevision, clientIDs)
}

func (svc *AICaptureSessionService) Delete(ctx Context, id uuid.UUID) error {
	session, err := svc.repos.AICaptureSessions.Delete(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return err
	}
	for _, photo := range session.Photos {
		if err := svc.repos.AICaptureSessions.DeleteBlob(ctx, photo.Path); err != nil {
			log.Warn().Err(err).Str("session_id", id.String()).Msg("failed to delete capture photo")
		}
	}
	return nil
}

func (svc *AICaptureSessionService) PurgeExpired(ctx context.Context) error {
	sessions, err := svc.repos.AICaptureSessions.ListExpired(ctx, time.Now())
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if err := svc.repos.AICaptureSessions.PurgeSessionPhotos(ctx, session); err != nil {
			log.Warn().Err(err).Str("session_id", session.ID.String()).Msg("failed to purge expired capture photos")
			continue
		}
		if err := svc.repos.AICaptureSessions.DeleteInternal(ctx, session.ID); err != nil {
			log.Warn().Err(err).Str("session_id", session.ID.String()).Msg("failed to delete expired capture session")
		}
	}
	return nil
}
