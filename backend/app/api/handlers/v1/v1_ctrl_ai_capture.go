package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/hay-kot/httpkit/errchain"
	"github.com/hay-kot/httpkit/server"
	"github.com/rs/zerolog/log"
	"github.com/sysadminsmedia/homebox/backend/internal/core/services"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/sys/validate"
)

const maxAICorrectionLength = 2000

// HandleAICaptureAnalyze godoc
//
//	@Summary	Analyze inventory photos with an OpenAI-compatible vision model
//	@Tags		AI Capture
//	@Accept		multipart/form-data
//	@Produce	json
//	@Param		photos		formData	file	true	"Inventory photos (repeat field for multiple photos)"
//	@Param		locationId	formData	string	true	"Selected HomeBox location UUID"
//	@Param		instruction	formData	string	false	"Correction request"
//	@Param		draft		formData	string	false	"Current AI draft JSON"
//	@Success	200			{object}	services.AICaptureDraft
//	@Failure	422			{object}	validate.ErrorResponse
//	@Failure	502			{object}	validate.ErrorResponse
//	@Router		/v1/ai/capture/analyze [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureAnalyze() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if ctrl.svc.AICapture == nil || !ctrl.svc.AICapture.IsEnabled() {
			return validate.NewRequestError(services.ErrAIDisabled, http.StatusServiceUnavailable)
		}
		if err := r.ParseMultipartForm(ctrl.maxParseMemory << 20); err != nil {
			return multipartFormError(err)
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}

		files := r.MultipartForm.File["photos"]
		if len(files) == 0 {
			return validate.NewRequestError(errors.New("at least one photo is required"), http.StatusUnprocessableEntity)
		}
		if len(files) > ctrl.svc.AICapture.MaxPhotos() {
			return validate.NewRequestError(
				fmt.Errorf("no more than %d photos are allowed", ctrl.svc.AICapture.MaxPhotos()),
				http.StatusUnprocessableEntity,
			)
		}

		locationID, err := uuid.Parse(strings.TrimSpace(r.FormValue("locationId")))
		if err != nil {
			return validate.NewRequestError(errors.New("select a valid location"), http.StatusUnprocessableEntity)
		}
		auth := services.NewContext(r.Context())
		location, err := ctrl.repo.Entities.GetOneByGroup(r.Context(), auth.GID, locationID)
		if err != nil {
			if ent.IsNotFound(err) {
				return validate.NewRequestError(errors.New("select a valid location"), http.StatusUnprocessableEntity)
			}
			return validate.NewRequestError(err, http.StatusInternalServerError)
		}
		if location.EntityType == nil || !location.EntityType.IsLocation {
			return validate.NewRequestError(errors.New("selected entity must be a location"), http.StatusUnprocessableEntity)
		}

		photos := make([]services.AICapturePhoto, 0, len(files))
		for _, header := range files {
			photo, err := readAICapturePhoto(header, ctrl.maxUploadSize<<20)
			if err != nil {
				return validate.NewRequestError(err, http.StatusUnprocessableEntity)
			}
			photos = append(photos, photo)
		}

		instruction := strings.TrimSpace(r.FormValue("instruction"))
		if len([]rune(instruction)) > maxAICorrectionLength {
			return validate.NewRequestError(errors.New("instruction must be at most 2000 characters"), http.StatusUnprocessableEntity)
		}

		var draft *services.AICaptureDraft
		if draftJSON := strings.TrimSpace(r.FormValue("draft")); draftJSON != "" {
			draft = &services.AICaptureDraft{}
			if err := json.Unmarshal([]byte(draftJSON), draft); err != nil {
				return validate.NewRequestError(errors.New("draft must be valid JSON"), http.StatusUnprocessableEntity)
			}
		}

		entityTypes, err := ctrl.repo.EntityTypes.GetAll(r.Context(), auth.GID)
		if err != nil {
			return validate.NewRequestError(err, http.StatusInternalServerError)
		}
		tags, err := ctrl.repo.Tags.GetAll(r.Context(), auth.GID)
		if err != nil {
			return validate.NewRequestError(err, http.StatusInternalServerError)
		}
		metadata := services.AICaptureContext{
			Location:    services.AICaptureOption{ID: location.ID.String(), Name: location.Name},
			EntityTypes: make([]services.AICaptureOption, 0, len(entityTypes)),
			Tags:        make([]services.AICaptureOption, 0, len(tags)),
		}
		for _, entityType := range entityTypes {
			if !entityType.IsLocation {
				metadata.EntityTypes = append(metadata.EntityTypes, services.AICaptureOption{
					ID: entityType.ID.String(), Name: entityType.Name,
				})
			}
		}
		if len(metadata.EntityTypes) == 0 {
			return validate.NewRequestError(errors.New("create an item entity type before using AI capture"), http.StatusConflict)
		}
		for _, tag := range tags {
			metadata.Tags = append(metadata.Tags, services.AICaptureOption{ID: tag.ID.String(), Name: tag.Name})
		}

		result, err := ctrl.svc.AICapture.Analyze(r.Context(), services.AICaptureRequest{
			Photos: photos, Context: metadata, Instruction: instruction, Draft: draft,
		})
		if err != nil {
			log.Error().Err(err).Msg("AI capture analysis failed")
			switch {
			case errors.Is(err, services.ErrAIDisabled):
				return validate.NewRequestError(services.ErrAIDisabled, http.StatusServiceUnavailable)
			case errors.Is(err, services.ErrAIInvalidRequest):
				return validate.NewRequestError(err, http.StatusUnprocessableEntity)
			default:
				return validate.NewRequestError(errors.New("the AI provider could not analyze these photos"), http.StatusBadGateway)
			}
		}

		return server.JSON(w, http.StatusOK, result)
	}
}

func readAICapturePhoto(header *multipart.FileHeader, maxBytes int64) (services.AICapturePhoto, error) {
	file, err := header.Open()
	if err != nil {
		return services.AICapturePhoto{}, errors.New("failed to read uploaded photo")
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return services.AICapturePhoto{}, errors.New("failed to read uploaded photo")
	}
	if int64(len(data)) > maxBytes {
		return services.AICapturePhoto{}, fmt.Errorf("photo %q exceeds the %d MB upload limit", header.Filename, maxBytes>>20)
	}
	if len(data) == 0 {
		return services.AICapturePhoto{}, fmt.Errorf("photo %q is empty", header.Filename)
	}

	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return services.AICapturePhoto{MIMEType: mimeType, Data: data}, nil
	default:
		return services.AICapturePhoto{}, fmt.Errorf("photo %q must be JPEG, PNG, GIF, or WebP", header.Filename)
	}
}
