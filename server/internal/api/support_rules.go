package api

import (
	"errors"
	"net/http"

	"github.com/chrisgreg/boop/server/internal/support"
)

type supportRuleFiring struct {
	ID        string `json:"id"`
	RuleID    string `json:"rule_id"`
	RuleName  string `json:"rule_name"`
	EventID   string `json:"event_id"`
	CardID    string `json:"card_id"`
	Product   string `json:"product"`
	Field     string `json:"field"`
	Outcome   string `json:"outcome"`
	Before    string `json:"before_value"`
	After     string `json:"after_value"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) listSupportRuleFirings(w http.ResponseWriter, r *http.Request) {
	limit := boundedQueryInt(r, "limit", 50, 1, 100)
	rows, err := s.DB.QueryContext(r.Context(), `SELECT f.id,COALESCE(f.rule_id,''),COALESCE(r.name,''),f.event_id,COALESCE(f.card_id,''),f.product,f.field,f.outcome,f.before_value,f.after_value,f.reason,f.created_at
		FROM support_rule_firings f LEFT JOIN support_classification_rules r ON r.id=f.rule_id
		ORDER BY f.created_at DESC,f.id DESC LIMIT ?`, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	firings := make([]supportRuleFiring, 0, limit)
	for rows.Next() {
		var item supportRuleFiring
		if err := rows.Scan(&item.ID, &item.RuleID, &item.RuleName, &item.EventID, &item.CardID, &item.Product,
			&item.Field, &item.Outcome, &item.Before, &item.After, &item.Reason, &item.CreatedAt); err != nil {
			s.fail(w, err)
			return
		}
		firings = append(firings, item)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"firings": firings})
}

func (s *Server) listSupportClassificationRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.Support.ListClassificationRules(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	suggestions, err := s.Support.ClassificationRuleSuggestions(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules, "suggestions": suggestions})
}

func (s *Server) createSupportClassificationRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		support.RuleInput
		Source string `json:"source"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	rule, err := s.Support.CreateClassificationRule(r.Context(), in.RuleInput, in.Source)
	if errors.Is(err, support.ErrInvalid) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_support_rule", "rule name, field, and classification values must be valid and distinct")
		return
	}
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) updateSupportClassificationRule(w http.ResponseWriter, r *http.Request) {
	var in struct {
		support.RuleInput
		Enabled bool `json:"enabled"`
		DryRun  bool `json:"dry_run"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	rule, err := s.Support.UpdateClassificationRule(r.Context(), r.PathValue("id"), in.RuleInput, in.Enabled, in.DryRun)
	if errors.Is(err, support.ErrInvalid) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_support_rule", "rule name, field, and classification values must be valid and distinct")
		return
	}
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) deleteSupportClassificationRule(w http.ResponseWriter, r *http.Request) {
	if err := s.Support.DeleteClassificationRule(r.Context(), r.PathValue("id")); err != nil {
		supportMutationError(w, s, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) simulateSupportClassificationRule(w http.ResponseWriter, r *http.Request) {
	result, err := s.Support.SimulateClassificationRule(r.Context(), r.PathValue("id"))
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
