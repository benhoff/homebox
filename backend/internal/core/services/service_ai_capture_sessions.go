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
	AICaptureErrorInvalidState     = "INVALID_STATE"
	AICaptureErrorSessionFull      = "SESSION_FULL"
	AICaptureErrorPhotoMismatch    = "PHOTO_COUNT_MISMATCH"
	AICaptureErrorRevisionMismatch = "CAPTURE_REVISION_MISMATCH"
	AICaptureErrorLocationMissing  = "LOCATION_MISSING"
	AICaptureErrorAnalysisFailed   = "ANALYSIS_UPSTREAM_FAILED"
	AICaptureErrorDraftConflict    = "DRAFT_REVISION_CONFLICT"
	AICaptureErrorSubmitFailed     = "SUBMISSION_FAILED"
)

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
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AICaptureSessionOut struct {
	ID                 string                  `json:"id"`
	Status             string                  `json:"status"`
	Location           *AICaptureOption        `json:"location,omitempty"`
	PhotoCount         int                     `json:"photoCount"`
	UploadedPhotoCount int                     `json:"uploadedPhotoCount"`
	Photos             []AICaptureSessionPhoto `json:"photos"`
	AnalysisAttempts   int                     `json:"analysisAttempts"`
	DraftRevision      int                     `json:"draftRevision"`
	CaptureRevision    int                     `json:"captureRevision"`
	Draft              *AICaptureDraft         `json:"draft,omitempty"`
	CreatedItems       []AICaptureCreatedItem  `json:"createdItems"`
	ErrorCode          string                  `json:"errorCode,omitempty"`
	ErrorMessage       string                  `json:"errorMessage,omitempty"`
	CreatedAt          time.Time               `json:"createdAt"`
	UpdatedAt          time.Time               `json:"updatedAt"`
	FinishedAt         *time.Time              `json:"finishedAt,omitempty"`
	AnalyzedAt         *time.Time              `json:"analyzedAt,omitempty"`
	CompletedAt        *time.Time              `json:"completedAt,omitempty"`
	ExpiresAt          time.Time               `json:"expiresAt"`
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
		ID:                 record.ID.String(),
		Status:             record.Status,
		PhotoCount:         max(record.PhotoCount, len(record.Photos)),
		UploadedPhotoCount: len(record.Photos),
		Photos:             make([]AICaptureSessionPhoto, len(record.Photos)),
		AnalysisAttempts:   record.AnalysisAttempts,
		DraftRevision:      record.DraftRevision,
		CaptureRevision:    record.CaptureRevision,
		CreatedItems:       []AICaptureCreatedItem{},
		ErrorCode:          record.ErrorCode,
		ErrorMessage:       record.ErrorMessage,
		CreatedAt:          record.CreatedAt,
		UpdatedAt:          record.UpdatedAt,
		FinishedAt:         record.FinishedAt,
		AnalyzedAt:         record.AnalyzedAt,
		CompletedAt:        record.CompletedAt,
		ExpiresAt:          record.ExpiresAt,
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
		out.Draft = draft
	}
	names := map[string]string{}
	if draft != nil {
		for _, item := range draft.Items {
			names[item.ClientID] = item.Name
		}
	}
	for _, item := range record.Items {
		if item.EntityID != nil {
			out.CreatedItems = append(out.CreatedItems, AICaptureCreatedItem{ID: item.EntityID.String(), Name: names[item.ClientID]})
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
		ctx, ctx.GID, ctx.UID, sessionID, photoID, clientPhotoID, position, svc.ai.MaxPhotos(), captureGroupID,
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
	record, err := svc.repos.AICaptureSessions.UpdatePhotoGroup(ctx, ctx.GID, ctx.UID, sessionID, photoID, captureGroupID)
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

func partitionAICapturePhotos(photos []repo.AICapturePhotoRecord) []aiCaptureAnalysisBatch {
	batches := make([]aiCaptureAnalysisBatch, 0)
	batchByGroup := make(map[string]int)
	for _, photo := range photos {
		groupID := ""
		if photo.CaptureGroupID != nil {
			groupID = photo.CaptureGroupID.String()
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
	for _, batch := range partitionAICapturePhotos(record.Photos) {
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
		mapDraftPhotoIDs(&draft, batch.Photos)
		if batch.CaptureGroupID != "" {
			draft.Items[0].ClientID = "group-" + batch.CaptureGroupID
		}
		out.Items = append(out.Items, draft.Items...)
		out.Warnings = append(out.Warnings, draft.Warnings...)
	}
	maxItems := svc.config.MaxItems
	if maxItems <= 0 {
		maxItems = 25
	}
	if len(out.Items) > maxItems {
		return AICaptureDraft{}, fmt.Errorf("%w: grouped analysis returned %d items, maximum is %d", ErrAIUpstream, len(out.Items), maxItems)
	}
	sortAICaptureItemsByFirstPhoto(out.Items, record.Photos)
	ensureUniqueAICaptureClientIDs(&out)
	return out, nil
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
	retry := record.AnalysisAttempts < 3 && errors.Is(err, ErrAIUpstream) && !errors.Is(err, ErrAIGroupContract)
	code := AICaptureErrorAnalysisFailed
	message := "The vision provider could not analyze this session. Try again."
	if strings.Contains(err.Error(), AICaptureErrorLocationMissing) {
		code = AICaptureErrorLocationMissing
		message = "The selected location no longer exists. Choose another location and retry."
		retry = false
	}
	return svc.repos.AICaptureSessions.SetAnalysisError(ctx, record.ID, retry, code, message)
}

func (svc *AICaptureSessionService) validateSessionDraft(ctx context.Context, record repo.AICaptureSessionRecord, draft AICaptureDraft) (AICaptureDraft, error) {
	metadata, err := svc.metadata(ctx, record)
	if err != nil {
		return AICaptureDraft{}, err
	}
	mapDraftPhotoIndexes(&draft, record.Photos)
	draft, err = sanitizeAICaptureDraft(draft, metadata, len(record.Photos), svc.config.MaxItems)
	if err != nil {
		return AICaptureDraft{}, err
	}
	mapDraftPhotoIDs(&draft, record.Photos)
	validateDraftCaptureGroups(&draft, record.Photos)
	return draft, nil
}

func (svc *AICaptureSessionService) SaveDraft(ctx Context, id uuid.UUID, expectedRevision int, draft AICaptureDraft) (AICaptureSessionOut, error) {
	record, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
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

func (svc *AICaptureSessionService) Correct(ctx Context, id uuid.UUID, expectedRevision int, instruction string) (AICaptureSessionOut, error) {
	record, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if record.Status != "ready_for_review" || record.DraftRevision != expectedRevision {
		return AICaptureSessionOut{}, repo.ErrAICaptureDraftConflict
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
	allowedGroups := make([]string, 0)
	seenGroups := make(map[string]struct{})
	for _, item := range current.Items {
		groupsByClientID[item.ClientID] = item.CaptureGroupID
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
	}
	mapDraftPhotoIDs(&corrected, record.Photos)
	validateDraftCaptureGroups(&corrected, record.Photos)
	encoded, err := json.Marshal(corrected)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if err := svc.repos.AICaptureSessions.SaveDraft(ctx, ctx.GID, ctx.UID, id, expectedRevision, string(encoded)); err != nil {
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
		})
		if err != nil {
			return err
		}
		entityID = &created.ID
		if err := svc.repos.AICaptureSessions.SetSubmissionItemEntity(ctx, session.ID, item.ClientID, created.ID); err != nil {
			return err
		}
	}

	uploaded := decodeUploadedPhotoIDs(progress.UploadedPhotoIDs)
	photoByID := make(map[string]repo.AICapturePhotoRecord, len(session.Photos))
	for _, photo := range session.Photos {
		photoByID[photo.ID.String()] = photo
	}
	for index, photoID := range item.PhotoIDs {
		if _, ok := uploaded[photoID]; ok {
			continue
		}
		photo, ok := photoByID[photoID]
		if !ok {
			return fmt.Errorf("draft photo %s is unavailable", photoID)
		}
		data, err := svc.repos.AICaptureSessions.ReadBlob(ctx, photo.Path)
		if err != nil {
			return err
		}
		if _, err := svc.entities.AttachmentAdd(ctx, *entityID, photo.OriginalName, attachment.TypePhoto, index == 0, bytes.NewReader(data)); err != nil {
			return err
		}
		uploaded[photoID] = struct{}{}
		if err := svc.repos.AICaptureSessions.SetSubmissionItemPhotos(ctx, session.ID, item.ClientID, encodeUploadedPhotoIDs(uploaded)); err != nil {
			return err
		}
	}
	return svc.repos.AICaptureSessions.SetSubmissionItemStatus(ctx, session.ID, item.ClientID, aicapturesessionitem.StatusCompleted, "")
}

func (svc *AICaptureSessionService) Submit(ctx Context, id uuid.UUID, expectedRevision int) (AICaptureSessionOut, error) {
	session, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if session.LocationID == nil {
		return AICaptureSessionOut{}, fmt.Errorf("%s: select a replacement location", AICaptureErrorLocationMissing)
	}
	if err := svc.repos.AICaptureSessions.StartSubmitting(ctx, ctx.GID, ctx.UID, id, expectedRevision); err != nil {
		return AICaptureSessionOut{}, err
	}
	var draft AICaptureDraft
	if err := json.Unmarshal([]byte(session.DraftJSON), &draft); err != nil {
		_ = svc.repos.AICaptureSessions.SetSubmitFailed(ctx, id, AICaptureErrorSubmitFailed, "The saved draft could not be read.")
		return AICaptureSessionOut{}, err
	}
	for _, item := range draft.Items {
		if err := svc.submitItem(ctx, session, item); err != nil {
			_ = svc.repos.AICaptureSessions.SetSubmissionItemStatus(ctx, session.ID, item.ClientID, aicapturesessionitem.StatusFailed, AICaptureErrorSubmitFailed)
			_ = svc.repos.AICaptureSessions.SetSubmitFailed(ctx, id, AICaptureErrorSubmitFailed, "Some items or photos could not be saved. Retry is safe.")
			return svc.Get(ctx, id)
		}
	}
	if err := svc.repos.AICaptureSessions.SetCompleted(ctx, id); err != nil {
		return AICaptureSessionOut{}, err
	}
	completed, err := svc.repos.AICaptureSessions.Get(ctx, ctx.GID, ctx.UID, id)
	if err != nil {
		return AICaptureSessionOut{}, err
	}
	if err := svc.repos.AICaptureSessions.PurgeSessionPhotos(ctx, completed); err != nil {
		log.Warn().Err(err).Str("session_id", id.String()).Msg("failed to purge completed capture photos")
	}
	return svc.Get(ctx, id)
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
