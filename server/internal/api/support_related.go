package api

import (
	"net/http"
)

func (s *Server) supportRelatedCards(w http.ResponseWriter, r *http.Request) {
	links, suggestions, err := s.Support.RelatedCards(r.Context(), r.PathValue("id"))
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links, "suggestions": suggestions})
}

func (s *Server) createSupportCardLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TargetCardID string `json:"target_card_id"`
		Relation     string `json:"relation"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	cardID := r.PathValue("id")
	if err := s.Support.CreateCardLink(r.Context(), cardID, in.TargetCardID, in.Relation); err != nil {
		supportMutationError(w, s, err)
		return
	}
	links, suggestions, err := s.Support.RelatedCards(r.Context(), cardID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links, "suggestions": suggestions})
}

func (s *Server) deleteSupportCardLink(w http.ResponseWriter, r *http.Request) {
	if err := s.Support.DeleteCardLink(r.Context(), r.PathValue("id"), r.PathValue("link_id")); err != nil {
		supportMutationError(w, s, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
