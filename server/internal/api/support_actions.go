package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chrisgreg/boop/server/internal/settings"
	"github.com/chrisgreg/boop/server/internal/support"
)

func (s *Server) listSupportActions(w http.ResponseWriter, r *http.Request) {
	actions, err := s.Support.ListActions(r.Context(), r.PathValue("id"))
	if err != nil {
		supportActionError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": actions})
}

func (s *Server) supportActionHistory(w http.ResponseWriter, r *http.Request) {
	history, err := s.Support.ActionHistory(r.Context(), r.PathValue("id"))
	if err != nil {
		supportActionError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": history})
}

func (s *Server) createSupportAction(w http.ResponseWriter, r *http.Request) {
	var in support.ActionInput
	if !readJSON(w, r, &in) {
		return
	}
	action, err := s.Support.CreateAction(r.Context(), r.PathValue("id"), in, s.Admin.Actor(r))
	if err != nil {
		supportActionError(w, s, err)
		return
	}
	writeJSON(w, http.StatusCreated, action)
}

func (s *Server) updateSupportAction(w http.ResponseWriter, r *http.Request) {
	var in support.ActionInput
	if !readJSON(w, r, &in) {
		return
	}
	action, err := s.Support.UpdateAction(r.Context(), r.PathValue("id"), in, s.Admin.Actor(r))
	if err != nil {
		supportActionError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) cancelSupportAction(w http.ResponseWriter, r *http.Request) {
	if err := s.Support.CancelAction(r.Context(), r.PathValue("id"), s.Admin.Actor(r)); err != nil {
		supportActionError(w, s, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) approveSupportReply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm bool `json:"confirm"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !in.Confirm {
		writeError(w, http.StatusBadRequest, "confirmation_required", "set confirm=true after reviewing the reply draft")
		return
	}
	action, err := s.Support.ApproveReply(r.Context(), r.PathValue("id"), s.Admin.Actor(r))
	if err != nil {
		supportActionError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) executeSupportAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm bool `json:"confirm"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !in.Confirm {
		writeError(w, http.StatusBadRequest, "confirmation_required", "review the destination and content, then set confirm=true")
		return
	}
	id := r.PathValue("id")
	actor := s.Admin.Actor(r)
	action, err := s.Support.GetAction(r.Context(), id)
	if err != nil {
		supportActionError(w, s, err)
		return
	}
	if action.Kind == "technical_plan" {
		completed, err := s.Support.CompleteTechnicalPlan(r.Context(), id, actor)
		if err != nil {
			supportActionError(w, s, err)
			return
		}
		writeJSON(w, http.StatusOK, completed)
		return
	}
	if action.Kind != "github_issue" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_action_kind", "only GitHub issues and technical plans can be executed here")
		return
	}
	if action.State == "completed" {
		writeJSON(w, http.StatusOK, action)
		return
	}
	token, err := s.githubToken(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "github_not_configured", "add a valid GitHub token in Settings before creating an issue")
		return
	}
	started, err := s.Support.BeginGitHubExecution(r.Context(), id, actor)
	if err != nil {
		supportActionError(w, s, err)
		return
	}
	if started.State == "completed" {
		writeJSON(w, http.StatusOK, started)
		return
	}
	callCtx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	issue, reconciled, uncertain, callErr := newGitHubIssueClient().createOrFindIssue(callCtx, token, *started)
	cancel()
	if callErr != nil {
		if storeErr := s.Support.FailGitHubExecution(r.Context(), id, actor, callErr.Error(), uncertain); storeErr != nil {
			s.Log.Error("support.action_state_write_failed", "action_id", id, "error", storeErr)
		}
		code := "github_issue_failed"
		message := callErr.Error()
		if uncertain {
			code = "github_issue_reconciliation_required"
			message = "GitHub may have created the issue. Check the action status and reconcile before retrying."
		}
		writeError(w, http.StatusBadGateway, code, message)
		return
	}
	completed, err := s.Support.FinishGitHubExecution(r.Context(), id, actor, issue, reconciled)
	if err != nil {
		if storeErr := s.Support.MarkGitHubNeedsReconciliation(r.Context(), id, actor, "GitHub returned an issue, but Boop could not save the result."); storeErr != nil {
			s.Log.Error("support.action_state_write_failed", "action_id", id, "error", storeErr)
		}
		writeError(w, http.StatusServiceUnavailable, "github_issue_reconciliation_required", "GitHub returned an issue, but Boop could not save its link. Reconcile this action before retrying.")
		return
	}
	writeJSON(w, http.StatusOK, completed)
}

func (s *Server) reconcileSupportAction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	actor := s.Admin.Actor(r)
	action, err := s.Support.GetAction(r.Context(), id)
	if err != nil {
		supportActionError(w, s, err)
		return
	}
	if action.Kind != "github_issue" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_action_kind", "only GitHub issue actions can be reconciled")
		return
	}
	if action.State == "completed" {
		writeJSON(w, http.StatusOK, map[string]any{"found": true, "action": action})
		return
	}
	if action.State != "executing" && action.State != "needs_reconciliation" {
		writeError(w, http.StatusConflict, "invalid_action_state", "only an in-progress or uncertain GitHub action can be reconciled")
		return
	}
	token, err := s.githubToken(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "github_not_configured", "add a valid GitHub token in Settings before reconciling")
		return
	}
	callCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	marker := "<!-- boop-support-action:" + action.ID + " -->"
	issue, findErr := newGitHubIssueClient().findIssue(callCtx, token, action.Destination, marker)
	cancel()
	if findErr != nil {
		writeError(w, http.StatusBadGateway, "github_reconcile_failed", "could not check GitHub for the issue; try again later")
		return
	}
	if issue != nil && validGitHubIssueURL(action.Destination, issue.HTMLURL) {
		completed, err := s.Support.FinishGitHubExecution(r.Context(), id, actor, support.ExternalIssue{ID: strconv.FormatInt(issue.ID, 10), URL: issue.HTMLURL}, true)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"found": true, "action": completed})
		return
	}
	if action.State == "executing" {
		if err := s.Support.MarkGitHubNeedsReconciliation(r.Context(), id, actor, "No matching issue was found in the latest GitHub issue pages; confirm manually before retrying."); err != nil {
			supportActionError(w, s, err)
			return
		}
	}
	current, err := s.Support.GetAction(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"found": false, "action": current})
}

func (s *Server) authorizeSupportActionRetry(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ConfirmNotCreated bool `json:"confirm_not_created"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !in.ConfirmNotCreated {
		writeError(w, http.StatusBadRequest, "confirmation_required", "confirm that the issue is absent from GitHub before authorizing a retry")
		return
	}
	if err := s.Support.AuthorizeGitHubRetry(r.Context(), r.PathValue("id"), s.Admin.Actor(r)); err != nil {
		supportActionError(w, s, err)
		return
	}
	action, err := s.Support.GetAction(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, action)
}

func (s *Server) githubToken(ctx context.Context) (string, error) {
	if s.Settings == nil {
		return "", errors.New("settings unavailable")
	}
	token, err := s.Settings.GetSecret(ctx, settings.KeyGitHubToken)
	if err != nil || token == "" {
		return "", errors.New("GitHub token unavailable")
	}
	return token, nil
}

func supportActionError(w http.ResponseWriter, s *Server, err error) {
	switch {
	case errors.Is(err, support.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "support_action_not_found", "the support card or action was not found")
	case errors.Is(err, support.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid_support_action", "check the action type, destination, title, and content")
	case errors.Is(err, support.ErrActionKind):
		writeError(w, http.StatusUnprocessableEntity, "invalid_action_kind", "this action does not support the requested operation")
	case errors.Is(err, support.ErrActionState):
		writeError(w, http.StatusConflict, "invalid_action_state", "the action changed or is not in a state that allows this operation")
	default:
		if strings.Contains(err.Error(), "GitHub") {
			writeError(w, http.StatusBadGateway, "github_unavailable", "GitHub could not complete the request; check the action history before retrying")
			return
		}
		s.fail(w, err)
	}
}
