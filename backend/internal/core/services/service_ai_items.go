package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent/attachment"
	"github.com/sysadminsmedia/homebox/backend/internal/data/repo"
)

const maxAIItemPhotoBytes = 20 << 20

type AIItemReanalysisOut struct {
	Item       AICaptureItem `json:"item"`
	Provider   string        `json:"provider"`
	Warnings   []string      `json:"warnings"`
	PhotoCount int           `json:"photoCount"`
}

type AIItemService struct {
	repos *repo.AllRepos
	ai    *AICaptureService
}

func NewAIItemService(repos *repo.AllRepos, ai *AICaptureService) *AIItemService {
	return &AIItemService{repos: repos, ai: ai}
}

func (svc *AIItemService) metadata(ctx context.Context, gid uuid.UUID, item repo.EntityOut) (AICaptureContext, error) {
	entityTypes, err := svc.repos.EntityTypes.GetAll(ctx, gid)
	if err != nil {
		return AICaptureContext{}, err
	}
	tags, err := svc.repos.Tags.GetAll(ctx, gid)
	if err != nil {
		return AICaptureContext{}, err
	}
	metadata := AICaptureContext{
		EntityTypes: make([]AICaptureOption, 0, len(entityTypes)),
		Tags:        make([]AICaptureOption, 0, len(tags)),
	}
	if item.Location != nil {
		metadata.Location = AICaptureOption{ID: item.Location.ID.String(), Name: item.Location.Name}
	}
	for _, entityType := range entityTypes {
		if !entityType.IsLocation {
			metadata.EntityTypes = append(metadata.EntityTypes, AICaptureOption{ID: entityType.ID.String(), Name: entityType.Name})
		}
	}
	if len(metadata.EntityTypes) == 0 {
		return AICaptureContext{}, errors.New("create an item entity type before using AI reanalysis")
	}
	for _, tag := range tags {
		metadata.Tags = append(metadata.Tags, AICaptureOption{ID: tag.ID.String(), Name: tag.Name})
	}
	return metadata, nil
}

func (svc *AIItemService) Reanalyze(ctx Context, id uuid.UUID, instruction string) (AIItemReanalysisOut, error) {
	if svc == nil || svc.ai == nil || !svc.ai.IsEnabled() {
		return AIItemReanalysisOut{}, ErrAIDisabled
	}
	item, err := svc.repos.Entities.GetOneByGroup(ctx, ctx.GID, id)
	if err != nil {
		return AIItemReanalysisOut{}, err
	}
	if item.EntityType == nil || item.EntityType.IsLocation {
		return AIItemReanalysisOut{}, fmt.Errorf("%w: select an inventory item", ErrAIInvalidRequest)
	}
	if len([]rune(instruction)) > 2000 {
		return AIItemReanalysisOut{}, fmt.Errorf("%w: instruction must be at most 2000 characters", ErrAIInvalidRequest)
	}

	photoAttachments := slices.Clone(item.Attachments)
	slices.SortStableFunc(photoAttachments, func(left, right repo.ItemAttachment) int {
		if left.Primary != right.Primary {
			if left.Primary {
				return -1
			}
			return 1
		}
		return left.CreatedAt.Compare(right.CreatedAt)
	})
	photos := make([]AICapturePhoto, 0, min(len(photoAttachments), svc.ai.MaxPhotos()))
	photoIDs := make([]string, 0, cap(photos))
	warnings := []string{}
	photosOmittedByLimit := false
	for _, photo := range photoAttachments {
		if photo.Type != attachment.TypePhoto.String() || photo.MimeType == repo.MimeTypeLinkURL {
			continue
		}
		if len(photos) >= svc.ai.MaxPhotos() {
			photosOmittedByLimit = true
			continue
		}
		data, readErr := svc.repos.Attachments.ReadBlob(ctx, photo.Path, maxAIItemPhotoBytes)
		if readErr != nil {
			return AIItemReanalysisOut{}, fmt.Errorf("read item photo: %w", readErr)
		}
		mimeType := http.DetectContentType(data)
		switch mimeType {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
			photos = append(photos, AICapturePhoto{MIMEType: mimeType, Data: data})
			photoIDs = append(photoIDs, photo.ID.String())
		default:
			warnings = append(warnings, fmt.Sprintf("Photo %q was skipped because its format is not supported by AI analysis.", photo.Title))
		}
	}
	if len(photos) == 0 {
		return AIItemReanalysisOut{}, fmt.Errorf("%w: add a JPEG, PNG, GIF, or WebP photo before reanalyzing", ErrAIInvalidRequest)
	}
	if photosOmittedByLimit {
		warnings = append(warnings, fmt.Sprintf("Only the first %d supported photos were analyzed.", svc.ai.MaxPhotos()))
	}

	metadata, err := svc.metadata(ctx, ctx.GID, item)
	if err != nil {
		return AIItemReanalysisOut{}, err
	}
	result, err := svc.ai.Analyze(ctx, AICaptureRequest{
		Photos: photos, Context: metadata, SingleItem: true,
		Instruction: strings.TrimSpace(instruction),
	})
	if err != nil {
		return AIItemReanalysisOut{}, err
	}
	suggestion := result.Items[0]
	suggestion.ClientID = id.String()
	suggestion.PhotoIndexes = nil
	suggestion.PhotoIDs = photoIDs
	return AIItemReanalysisOut{
		Item: suggestion, Provider: AICaptureProviderDefault,
		Warnings: append(warnings, result.Warnings...), PhotoCount: len(photos),
	}, nil
}
