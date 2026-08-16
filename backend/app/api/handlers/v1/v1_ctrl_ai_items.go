package v1

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hay-kot/httpkit/errchain"
	"github.com/hay-kot/httpkit/server"
	"github.com/sysadminsmedia/homebox/backend/internal/core/services"
	"github.com/sysadminsmedia/homebox/backend/internal/data/ent"
	"github.com/sysadminsmedia/homebox/backend/internal/sys/validate"
)

type aiItemReanalysisRequest struct {
	Instruction string `json:"instruction"`
}

func aiItemReanalysisRequestError(err error) error {
	switch {
	case ent.IsNotFound(err):
		return validate.NewRequestError(errors.New("item not found"), http.StatusNotFound)
	case errors.Is(err, services.ErrAIDisabled), errors.Is(err, services.ErrAIProvider):
		return validate.NewRequestError(err, http.StatusServiceUnavailable)
	case errors.Is(err, services.ErrAIInvalidRequest):
		return validate.NewRequestError(err, http.StatusUnprocessableEntity)
	case errors.Is(err, services.ErrAIUpstream):
		return validate.NewRequestError(errors.New("the AI provider could not analyze this item"), http.StatusBadGateway)
	default:
		return validate.NewRequestError(err, http.StatusInternalServerError)
	}
}

// HandleAIItemReanalysis godoc
//
//	@Summary	Preview Qwen reanalysis for an existing inventory item
//	@Tags		AI Capture
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string					true	"Item ID"
//	@Param		payload	body		aiItemReanalysisRequest	false	"Optional visual-analysis guidance"
//	@Success	200		{object}	services.AIItemReanalysisOut
//	@Failure	422		{object}	validate.ErrorResponse
//	@Failure	502		{object}	validate.ErrorResponse
//	@Router		/v1/ai/items/{id}/reanalyze [post]
//	@Security	Bearer
func (ctrl *V1Controller) HandleAIItemReanalysis() errchain.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		id, err := ctrl.routeID(r)
		if err != nil {
			return err
		}
		var body aiItemReanalysisRequest
		if err := server.Decode(r, &body); err != nil {
			return validate.NewRequestError(errors.New("invalid request body"), http.StatusBadRequest)
		}
		body.Instruction = strings.TrimSpace(body.Instruction)
		out, err := ctrl.svc.AIItems.Reanalyze(services.NewContext(r.Context()), id, body.Instruction)
		if err != nil {
			return aiItemReanalysisRequestError(err)
		}
		return server.JSON(w, http.StatusOK, out)
	}
}
