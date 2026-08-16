package v1

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/hay-kot/httpkit/errchain"
	"github.com/hay-kot/httpkit/server"
	"github.com/sysadminsmedia/homebox/backend/internal/core/services"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/data/repo"
	"github.com/sysadminsmedia/homebox/backend/internal/sys/validate"
)

type aiCaptureSessionCreate struct {
	LocationID uuid.UUID `json:"locationId"`
}

type aiCaptureSessionLocationUpdate struct {
	LocationID uuid.UUID `json:"locationId"`
}

type aiCaptureSessionFinish struct {
	ExpectedPhotoCount      int `json:"expectedPhotoCount"`
	ExpectedCaptureRevision int `json:"expectedCaptureRevision"`
}

type aiCaptureSessionPhotoUpdate struct {
	CaptureGroupID *uuid.UUID `json:"captureGroupId" extensions:"x-nullable"`
}

type aiCaptureSessionDraftUpdate struct {
	Revision int                     `json:"revision"`
	Draft    services.AICaptureDraft `json:"draft"`
}

type aiCaptureSessionCorrection struct {
	Revision    int    `json:"revision"`
	Instruction string `json:"instruction"`
}

type aiCaptureSessionReanalysis struct {
	Revision    int    `json:"revision"`
	ClientID    string `json:"clientId"`
	Provider    string `json:"provider"`
	Instruction string `json:"instruction"`
}

type aiCaptureSessionSubmit struct {
	Revision int `json:"revision"`
}

func aiCaptureSessionRequestError(err error) error {
	switch {
	case ent.IsNotFound(err):
		return validate.NewRequestError(errors.New("capture session not found"), http.StatusNotFound)
	case errors.Is(err, services.ErrAIDisabled):
		return validate.NewRequestError(err, http.StatusServiceUnavailable)
	case errors.Is(err, repo.ErrAICaptureSessionLimit):
		return validate.NewRequestError(fmt.Errorf("%s: finish or delete an existing session first", services.AICaptureErrorSessionFull), http.StatusConflict)
	case errors.Is(err, repo.ErrAICapturePhotoLimit):
		return validate.NewRequestError(fmt.Errorf("%s: this session has reached its photo limit", services.AICaptureErrorSessionFull), http.StatusUnprocessableEntity)
	case errors.Is(err, repo.ErrAICapturePhotoCount):
		return validate.NewRequestError(fmt.Errorf("%s: wait for every photo to upload before finishing", services.AICaptureErrorPhotoMismatch), http.StatusConflict)
	case errors.Is(err, repo.ErrAICaptureRevision):
		return validate.NewRequestError(fmt.Errorf("%s: reload the latest photo grouping before finishing", services.AICaptureErrorRevisionMismatch), http.StatusConflict)
	case errors.Is(err, repo.ErrAICaptureDraftConflict):
		return validate.NewRequestError(fmt.Errorf("%s: reload the latest draft before saving", services.AICaptureErrorDraftConflict), http.StatusConflict)
	case errors.Is(err, repo.ErrAICaptureInvalidState):
		return validate.NewRequestError(fmt.Errorf("%s: that action is unavailable for the current session", services.AICaptureErrorInvalidState), http.StatusConflict)
	case errors.Is(err, services.ErrAIInvalidRequest):
		return validate.NewRequestError(err, http.StatusUnprocessableEntity)
	case errors.Is(err, services.ErrAIProvider):
		return validate.NewRequestError(err, http.StatusServiceUnavailable)
	case errors.Is(err, services.ErrAIUpstream):
		return validate.NewRequestError(errors.New("the AI provider could not update this session"), http.StatusBadGateway)
	case strings.Contains(err.Error(), services.AICaptureErrorLocationMissing):
		return validate.NewRequestError(err, http.StatusUnprocessableEntity)
	default:
		return validate.NewRequestError(err, http.StatusInternalServerError)
	}
}

func decodeAICaptureSessionBody(r *http.Request, target any) error {
	if err := server.Decode(r, target); err != nil {
		return validate.NewRequestError(errors.New("invalid request body"), http.StatusBadRequest)
	}
	return nil
}

// HandleAICaptureSessionsCreate godoc
//
//	@Summary	Create an AI capture session
//	@Tags		AI Capture Sessions
//	@Accept		json
//	@Produce	json
//	@Param		payload	body		aiCaptureSessionCreate	true	"Selected location"
//	@Success	201		{object}	services.AICaptureSessionOut
//	@Router		/v1/ai/capture/sessions [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionsCreate() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		var body aiCaptureSessionCreate
		if err := decodeAICaptureSessionBody(r, &body); err != nil {
			return err
		}
		if body.LocationID == uuid.Nil {
			return validate.NewRequestError(errors.New("select a location"), http.StatusUnprocessableEntity)
		}
		out, err := ctrl.svc.AICaptureSessions.Create(services.NewContext(r.Context()), body.LocationID)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusCreated, out)
	}
}

// HandleAICaptureSessionsList godoc
//
//	@Summary	List the current user's AI capture sessions
//	@Tags		AI Capture Sessions
//	@Produce	json
//	@Success	200	{object}	Results[services.AICaptureSessionOut]
//	@Router		/v1/ai/capture/sessions [GET]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionsList() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		out, err := ctrl.svc.AICaptureSessions.List(services.NewContext(r.Context()))
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusOK, WrapResults(out))
	}
}

// HandleAICaptureSessionGet godoc
//
//	@Summary	Get an AI capture session
//	@Tags		AI Capture Sessions
//	@Produce	json
//	@Param		sessionId	path		string	true	"Capture session ID"
//	@Success	200			{object}	services.AICaptureSessionOut
//	@Router		/v1/ai/capture/sessions/{sessionId} [GET]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionGet() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		out, err := ctrl.svc.AICaptureSessions.Get(services.NewContext(r.Context()), id)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusOK, out)
	}
}

// HandleAICaptureSessionUpdate godoc
//
//	@Summary	Change an AI capture session location
//	@Tags		AI Capture Sessions
//	@Accept		json
//	@Produce	json
//	@Param		sessionId	path		string					true	"Capture session ID"
//	@Param		payload		body		aiCaptureSessionLocationUpdate	true	"Replacement location"
//	@Success	200			{object}	services.AICaptureSessionOut
//	@Router		/v1/ai/capture/sessions/{sessionId} [PATCH]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionUpdate() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		var body aiCaptureSessionLocationUpdate
		if err := decodeAICaptureSessionBody(r, &body); err != nil {
			return err
		}
		out, err := ctrl.svc.AICaptureSessions.UpdateLocation(services.NewContext(r.Context()), id, body.LocationID)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusOK, out)
	}
}

// HandleAICaptureSessionDelete godoc
//
//	@Summary	Delete an AI capture session
//	@Tags		AI Capture Sessions
//	@Param		sessionId	path	string	true	"Capture session ID"
//	@Success	204
//	@Router		/v1/ai/capture/sessions/{sessionId} [DELETE]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionDelete() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		err = ctrl.svc.AICaptureSessions.Delete(services.NewContext(r.Context()), id)
		if err != nil && !ent.IsNotFound(err) {
			return aiCaptureSessionRequestError(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

// HandleAICaptureSessionPhotoCreate godoc
//
//	@Summary	Upload one photo to an AI capture session
//	@Tags		AI Capture Sessions
//	@Accept		multipart/form-data
//	@Produce	json
//	@Param		sessionId		path		string	true	"Capture session ID"
//	@Param		file			formData	file	true	"Normalized image"
//	@Param		clientPhotoId	formData	string	true	"Client idempotency UUID"
//	@Param		position		formData	int		true	"Capture order"
//	@Param		captureGroupId	formData	string	false	"Same-item group UUID"
//	@Success	201				{object}	services.AICaptureSessionPhoto
//	@Router		/v1/ai/capture/sessions/{sessionId}/photos [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionPhotoCreate() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		sessionID, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		if err := r.ParseMultipartForm(ctrl.maxParseMemory << 20); err != nil {
			return multipartFormError(err)
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			return validate.NewRequestError(errors.New("photo file is required"), http.StatusUnprocessableEntity)
		}
		_ = file.Close()
		photo, err := readAICapturePhoto(header, ctrl.maxUploadSize<<20)
		if err != nil {
			return validate.NewRequestError(err, http.StatusUnprocessableEntity)
		}
		clientPhotoID, err := uuid.Parse(strings.TrimSpace(r.FormValue("clientPhotoId")))
		if err != nil {
			return validate.NewRequestError(errors.New("clientPhotoId must be a UUID"), http.StatusUnprocessableEntity)
		}
		position, err := strconv.Atoi(r.FormValue("position"))
		if err != nil || position < 0 {
			return validate.NewRequestError(errors.New("position must be non-negative"), http.StatusUnprocessableEntity)
		}
		var captureGroupID *uuid.UUID
		if rawGroupID := strings.TrimSpace(r.FormValue("captureGroupId")); rawGroupID != "" {
			parsed, err := uuid.Parse(rawGroupID)
			if err != nil {
				return validate.NewRequestError(errors.New("captureGroupId must be a UUID"), http.StatusUnprocessableEntity)
			}
			captureGroupID = &parsed
		}
		out, err := ctrl.svc.AICaptureSessions.AddPhoto(
			services.NewContext(r.Context()), sessionID, clientPhotoID, position, captureGroupID,
			header.Filename, photo.MIMEType, photo.Data,
		)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusCreated, out)
	}
}

// HandleAICaptureSessionPhotoUpdate godoc
//
//	@Summary	Reassign or clear a photo's same-item group
//	@Tags		AI Capture Sessions
//	@Accept		json
//	@Produce	json
//	@Param		sessionId	path		string					true	"Capture session ID"
//	@Param		photoId		path		string					true	"Capture photo ID"
//	@Param		payload		body		aiCaptureSessionPhotoUpdate	true	"Nullable same-item group UUID"
//	@Success	200			{object}	services.AICaptureSessionPhoto
//	@Router		/v1/ai/capture/sessions/{sessionId}/photos/{photoId} [PATCH]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionPhotoUpdate() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		sessionID, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		photoID, err := ctrl.routeUUID(r, "photoId")
		if err != nil {
			return err
		}
		var body aiCaptureSessionPhotoUpdate
		if err := decodeAICaptureSessionBody(r, &body); err != nil {
			return err
		}
		out, err := ctrl.svc.AICaptureSessions.UpdatePhotoGroup(
			services.NewContext(r.Context()), sessionID, photoID, body.CaptureGroupID,
		)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusOK, out)
	}
}

// HandleAICaptureSessionPhotoGet godoc
//
//	@Summary	Read an authorized AI capture session photo
//	@Tags		AI Capture Sessions
//	@Produce	image/jpeg,image/png,image/gif,image/webp
//	@Param		sessionId	path	string	true	"Capture session ID"
//	@Param		photoId		path	string	true	"Capture photo ID"
//	@Success	200			{file}	file
//	@Router		/v1/ai/capture/sessions/{sessionId}/photos/{photoId} [GET]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionPhotoGet() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		sessionID, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		photoID, err := ctrl.routeUUID(r, "photoId")
		if err != nil {
			return err
		}
		photo, data, err := ctrl.svc.AICaptureSessions.ReadPhoto(services.NewContext(r.Context()), sessionID, photoID)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		w.Header().Set("Content-Type", photo.MIMEType)
		w.Header().Set("Cache-Control", "private, max-age=300")
		http.ServeContent(w, r, photo.OriginalName, photo.CreatedAt, bytes.NewReader(data))
		return nil
	}
}

// HandleAICaptureSessionPhotoDelete godoc
//
//	@Summary	Delete a photo from a capturing session
//	@Tags		AI Capture Sessions
//	@Param		sessionId	path	string	true	"Capture session ID"
//	@Param		photoId		path	string	true	"Capture photo ID"
//	@Success	204
//	@Router		/v1/ai/capture/sessions/{sessionId}/photos/{photoId} [DELETE]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionPhotoDelete() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		sessionID, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		photoID, err := ctrl.routeUUID(r, "photoId")
		if err != nil {
			return err
		}
		if err := ctrl.svc.AICaptureSessions.DeletePhoto(services.NewContext(r.Context()), sessionID, photoID); err != nil {
			return aiCaptureSessionRequestError(err)
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

// HandleAICaptureSessionFinish godoc
//
//	@Summary	Seal an AI capture session and queue analysis
//	@Tags		AI Capture Sessions
//	@Accept		json
//	@Produce	json
//	@Param		sessionId	path		string				 true	"Capture session ID"
//	@Param		payload		body		aiCaptureSessionFinish true	"Expected uploaded count"
//	@Success	202			{object}	services.AICaptureSessionOut
//	@Router		/v1/ai/capture/sessions/{sessionId}/finish [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionFinish() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		var body aiCaptureSessionFinish
		if err := decodeAICaptureSessionBody(r, &body); err != nil {
			return err
		}
		out, err := ctrl.svc.AICaptureSessions.Finish(
			services.NewContext(r.Context()), id, body.ExpectedPhotoCount, body.ExpectedCaptureRevision,
		)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusAccepted, out)
	}
}

// HandleAICaptureSessionRetryAnalysis godoc
//
//	@Summary	Retry failed AI capture analysis
//	@Tags		AI Capture Sessions
//	@Produce	json
//	@Param		sessionId	path		string	true	"Capture session ID"
//	@Success	202			{object}	services.AICaptureSessionOut
//	@Router		/v1/ai/capture/sessions/{sessionId}/retry-analysis [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionRetryAnalysis() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		out, err := ctrl.svc.AICaptureSessions.RetryAnalysis(services.NewContext(r.Context()), id)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusAccepted, out)
	}
}

// HandleAICaptureSessionDraftUpdate godoc
//
//	@Summary	Save an edited AI capture draft
//	@Tags		AI Capture Sessions
//	@Accept		json
//	@Produce	json
//	@Param		sessionId	path		string					true	"Capture session ID"
//	@Param		payload		body		aiCaptureSessionDraftUpdate	true	"Draft and expected revision"
//	@Success	200			{object}	services.AICaptureSessionOut
//	@Router		/v1/ai/capture/sessions/{sessionId}/draft [PUT]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionDraftUpdate() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		var body aiCaptureSessionDraftUpdate
		if err := decodeAICaptureSessionBody(r, &body); err != nil {
			return err
		}
		out, err := ctrl.svc.AICaptureSessions.SaveDraft(services.NewContext(r.Context()), id, body.Revision, body.Draft)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusOK, out)
	}
}

// HandleAICaptureSessionCorrection godoc
//
//	@Summary	Ask AI to correct a persisted capture draft
//	@Tags		AI Capture Sessions
//	@Accept		json
//	@Produce	json
//	@Param		sessionId	path		string					true	"Capture session ID"
//	@Param		payload		body		aiCaptureSessionCorrection	true	"Correction instruction"
//	@Success	200			{object}	services.AICaptureSessionOut
//	@Router		/v1/ai/capture/sessions/{sessionId}/corrections [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionCorrection() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		var body aiCaptureSessionCorrection
		if err := decodeAICaptureSessionBody(r, &body); err != nil {
			return err
		}
		if strings.TrimSpace(body.Instruction) == "" {
			return validate.NewRequestError(errors.New("instruction is required"), http.StatusUnprocessableEntity)
		}
		out, err := ctrl.svc.AICaptureSessions.Correct(services.NewContext(r.Context()), id, body.Revision, body.Instruction)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusOK, out)
	}
}

// HandleAICaptureSessionReanalysis godoc
//
//	@Summary	Preview provider reanalysis for one reviewed capture item
//	@Tags		AI Capture Sessions
//	@Accept		json
//	@Produce	json
//	@Param		sessionId	path		string					true	"Capture session ID"
//	@Param		payload		body		aiCaptureSessionReanalysis	true	"Reviewed item and provider"
//	@Success	200			{object}	services.AICaptureReanalysisOut
//	@Router		/v1/ai/capture/sessions/{sessionId}/reanalyze-item [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionReanalysis() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		var body aiCaptureSessionReanalysis
		if err := decodeAICaptureSessionBody(r, &body); err != nil {
			return err
		}
		if strings.TrimSpace(body.ClientID) == "" {
			return validate.NewRequestError(errors.New("clientId is required"), http.StatusUnprocessableEntity)
		}
		if len(body.Instruction) > 2000 {
			return validate.NewRequestError(errors.New("instruction must be at most 2000 characters"), http.StatusUnprocessableEntity)
		}
		out, err := ctrl.svc.AICaptureSessions.ReanalyzeItem(
			services.NewContext(r.Context()), id, body.Revision, body.ClientID, body.Provider, body.Instruction,
		)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusOK, out)
	}
}

// HandleAICaptureSessionSubmit godoc
//
//	@Summary	Create reviewed items from an AI capture session
//	@Tags		AI Capture Sessions
//	@Accept		json
//	@Produce	json
//	@Param		sessionId	path		string				true	"Capture session ID"
//	@Param		payload		body		aiCaptureSessionSubmit	true	"Expected draft revision"
//	@Success	200			{object}	services.AICaptureSessionOut
//	@Router		/v1/ai/capture/sessions/{sessionId}/submit [POST]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAICaptureSessionSubmit() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeUUID(r, "sessionId")
		if err != nil {
			return err
		}
		var body aiCaptureSessionSubmit
		if err := decodeAICaptureSessionBody(r, &body); err != nil {
			return err
		}
		out, err := ctrl.svc.AICaptureSessions.Submit(services.NewContext(r.Context()), id, body.Revision)
		if err != nil {
			return aiCaptureSessionRequestError(err)
		}
		return server.JSON(w, http.StatusOK, out)
	}
}
