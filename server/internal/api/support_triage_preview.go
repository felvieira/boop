package api

import (
	"errors"
	"net/http"

	"github.com/chrisgreg/boop/server/internal/triage"
)

func (s *Server) previewSupportTriage(w http.ResponseWriter, r *http.Request) {
	worker := triage.New(s.DB, s.Support, s.Settings, s.Config.OpenRouterAPIKey, s.Config.OpenRouterModel, s.Log)
	result, err := worker.Preview(r.Context())
	if errors.Is(err, triage.ErrProviderNotConfigured) {
		writeError(w, http.StatusConflict, "triage_not_configured", "configure an OpenRouter key before previewing the triage")
		return
	}
	if err != nil {
		s.Log.Error("support.triage.preview_failed", "error", err)
		writeError(w, http.StatusBadGateway, "triage_preview_failed", "the model preview failed; check the model and try again")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
