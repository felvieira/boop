package api

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chrisgreg/boop/server/internal/ids"
	"github.com/chrisgreg/boop/server/internal/support"
)

type supportMetricGroup struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type supportMetricsResponse struct {
	From             string               `json:"from"`
	To               string               `json:"to"`
	Notifications    int                  `json:"notifications_processed"`
	Matched          int                  `json:"matched"`
	Filtered         int                  `json:"filtered"`
	OpenFailures     int                  `json:"open_failures"`
	Retries          int                  `json:"retries"`
	CardsCreated     int                  `json:"cards_created"`
	ManualCards      int                  `json:"manual_cards_created"`
	Corrections      int                  `json:"human_corrections"`
	Runs             int                  `json:"ai_runs"`
	TokensPrompt     int                  `json:"prompt_tokens"`
	TokensCompletion int                  `json:"completion_tokens"`
	TokensTotal      int                  `json:"total_tokens"`
	CostUSD          *float64             `json:"cost_usd"`
	CostRuns         int                  `json:"cost_measured_runs"`
	AvgRunDurationMS int64                `json:"average_run_duration_ms"`
	ByProduct        []supportMetricGroup `json:"by_product"`
	ByCategory       []supportMetricGroup `json:"by_category"`
	FilteredByReason []supportMetricGroup `json:"filtered_by_reason"`
	Daily            supportDailyDigest   `json:"daily_digest"`
}

type supportDailyDigest struct {
	WindowStart string `json:"window_start"`
	NewCards    int    `json:"new_cards"`
	NewLeads    int    `json:"new_partnership_leads"`
	Urgent      int    `json:"urgent_active"`
	Blocked     int    `json:"blocked_active"`
	Overdue     int    `json:"overdue_active"`
}

func analyticsRange(r *http.Request) (string, string, bool) {
	today := time.Now().UTC()
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" {
		from = today.AddDate(0, 0, -29).Format("2006-01-02")
	}
	if to == "" {
		to = today.Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", from); err != nil {
		return "", "", false
	}
	if _, err := time.Parse("2006-01-02", to); err != nil || from > to {
		return "", "", false
	}
	return from, to, true
}

func scanMetricGroups(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}, limit int) ([]supportMetricGroup, error) {
	groups := make([]supportMetricGroup, 0)
	for rows.Next() {
		var group supportMetricGroup
		if err := rows.Scan(&group.Name, &group.Count); err != nil {
			return nil, err
		}
		groups = append(groups, group)
		if len(groups) >= limit {
			break
		}
	}
	return groups, rows.Err()
}

func (s *Server) supportMetrics(w http.ResponseWriter, r *http.Request) {
	from, to, valid := analyticsRange(r)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid_date_range", "from and to must be valid dates with from no later than to")
		return
	}
	metrics := supportMetricsResponse{From: from, To: to, ByProduct: []supportMetricGroup{}, ByCategory: []supportMetricGroup{}, FilteredByReason: []supportMetricGroup{}}
	if err := s.DB.QueryRowContext(r.Context(), `SELECT
		COALESCE(SUM(CASE WHEN state='matched' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN state='ignored' THEN 1 ELSE 0 END),0)
		FROM notification_triage WHERE processed_at<>'' AND substr(processed_at,1,10)>=? AND substr(processed_at,1,10)<=?`, from, to).
		Scan(&metrics.Matched, &metrics.Filtered); err != nil {
		s.fail(w, err)
		return
	}
	metrics.Notifications = metrics.Matched + metrics.Filtered
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM notification_triage
		WHERE state='pending' AND last_error<>'' AND substr(updated_at,1,10)>=? AND substr(updated_at,1,10)<=?`, from, to).Scan(&metrics.OpenFailures); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM support_triage_audit
		WHERE action IN ('retry','reprocess','failure') AND substr(created_at,1,10)>=? AND substr(created_at,1,10)<=?`, from, to).Scan(&metrics.Retries); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.DB.QueryRowContext(r.Context(), `SELECT
		COALESCE(SUM(CASE WHEN source_account<>'manual@boop.local' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN source_account='manual@boop.local' THEN 1 ELSE 0 END),0)
		FROM support_cards WHERE substr(created_at,1,10)>=? AND substr(created_at,1,10)<=?`, from, to).
		Scan(&metrics.CardsCreated, &metrics.ManualCards); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM support_classification_feedback
		WHERE substr(created_at,1,10)>=? AND substr(created_at,1,10)<=?`, from, to).Scan(&metrics.Corrections); err != nil {
		s.fail(w, err)
		return
	}
	var cost sql.NullFloat64
	var avgDuration sql.NullFloat64
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*),COALESCE(SUM(prompt_tokens),0),COALESCE(SUM(completion_tokens),0),
		COALESCE(SUM(total_tokens),0),SUM(cost_usd),COUNT(cost_usd),COALESCE(AVG(duration_ms),0)
		FROM support_triage_runs WHERE substr(finished_at,1,10)>=? AND substr(finished_at,1,10)<=?`, from, to).
		Scan(&metrics.Runs, &metrics.TokensPrompt, &metrics.TokensCompletion, &metrics.TokensTotal, &cost, &metrics.CostRuns, &avgDuration); err != nil {
		s.fail(w, err)
		return
	}
	if avgDuration.Valid {
		metrics.AvgRunDurationMS = int64(avgDuration.Float64)
	}
	if cost.Valid {
		metrics.CostUSD = &cost.Float64
	}
	products, err := s.DB.QueryContext(r.Context(), `SELECT product,COUNT(*) FROM support_cards
		WHERE substr(created_at,1,10)>=? AND substr(created_at,1,10)<=? GROUP BY product ORDER BY COUNT(*) DESC,product LIMIT 20`, from, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	metrics.ByProduct, err = scanMetricGroups(products, 20)
	products.Close()
	if err != nil {
		s.fail(w, err)
		return
	}
	categories, err := s.DB.QueryContext(r.Context(), `SELECT category,COUNT(*) FROM support_cards
		WHERE substr(created_at,1,10)>=? AND substr(created_at,1,10)<=? GROUP BY category ORDER BY COUNT(*) DESC,category`, from, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	metrics.ByCategory, err = scanMetricGroups(categories, 20)
	categories.Close()
	if err != nil {
		s.fail(w, err)
		return
	}
	reasons, err := s.DB.QueryContext(r.Context(), `SELECT reason,COUNT(*) FROM notification_triage
		WHERE state='ignored' AND substr(processed_at,1,10)>=? AND substr(processed_at,1,10)<=?
		GROUP BY reason ORDER BY COUNT(*) DESC LIMIT 10`, from, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	metrics.FilteredByReason, err = scanMetricGroups(reasons, 10)
	reasons.Close()
	if err != nil {
		s.fail(w, err)
		return
	}
	windowStart := ids.Format(time.Now().UTC().Add(-24 * time.Hour))
	metrics.Daily.WindowStart = windowStart
	now := ids.Now()
	if err := s.DB.QueryRowContext(r.Context(), `SELECT
		COALESCE(SUM(CASE WHEN created_at>=? AND archived_at='' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN created_at>=? AND archived_at='' AND category='partnership' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN archived_at='' AND priority='urgent' AND status<>'done' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN archived_at='' AND status='blocked' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN archived_at='' AND status<>'done' AND due_at<>'' AND due_at<? THEN 1 ELSE 0 END),0)
		FROM support_cards`, windowStart, windowStart, now).
		Scan(&metrics.Daily.NewCards, &metrics.Daily.NewLeads, &metrics.Daily.Urgent, &metrics.Daily.Blocked, &metrics.Daily.Overdue); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func supportFilterFromRequest(r *http.Request) support.CardFilter {
	query := r.URL.Query()
	return support.CardFilter{
		IncludeArchived: query.Get("archived") == "true",
		Query:           query.Get("q"), Product: query.Get("product"), Category: query.Get("category"),
		Priority: query.Get("priority"), ListID: query.Get("list_id"), ReviewState: query.Get("review_state"),
		ReportedFrom: query.Get("from"), ReportedTo: query.Get("to"),
	}
}

func (s *Server) exportSupport(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Support.ExportRows(r.Context(), supportFilterFromRequest(r))
	if errors.Is(err, support.ErrInvalid) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_support_filter", "check the export filters and try again")
		return
	}
	if errors.Is(err, support.ErrExportLimit) {
		writeError(w, http.StatusRequestEntityTooLarge, "support_export_too_large", fmt.Sprintf("exports are limited to %d cards; narrow the filters", 5000))
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	stamp := time.Now().UTC().Format("20060102")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))) {
	case "json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="boop-support-`+stamp+`.json"`)
		if err := json.NewEncoder(w).Encode(map[string]any{"exported_at": ids.Now(), "count": len(rows), "records": rows}); err != nil {
			s.Log.Error("support.export_write_failed", "format", "json", "error", err)
		}
	case "csv", "":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="boop-support-`+stamp+`.csv"`)
		writer := csv.NewWriter(w)
		header := []string{"card_id", "product", "title", "sender", "reported_at", "list", "status", "priority", "category", "description", "review_state", "due_at", "archived_at", "created_at", "updated_at", "audit_action", "audit_field", "audit_old_value", "audit_new_value", "audit_actor", "audit_created_at"}
		if err := writer.Write(header); err != nil {
			s.Log.Error("support.export_write_failed", "format", "csv", "error", err)
			return
		}
		for _, row := range rows {
			record := []string{row.CardID, row.Product, row.Title, row.Sender, row.ReportedAt, row.List, row.Status, row.Priority, row.Category, row.Description, row.ReviewState, row.DueAt, row.ArchivedAt, row.CreatedAt, row.UpdatedAt, row.AuditAction, row.AuditField, row.AuditOld, row.AuditNew, row.AuditActor, row.AuditCreated}
			for i, value := range record {
				record[i] = csvSafeCell(value)
			}
			if err := writer.Write(record); err != nil {
				s.Log.Error("support.export_write_failed", "format", "csv", "error", err)
				return
			}
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			s.Log.Error("support.export_write_failed", "format", "csv", "error", err)
		}
	default:
		writeError(w, http.StatusBadRequest, "invalid_export_format", "format must be csv or json")
	}
}

func csvSafeCell(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
