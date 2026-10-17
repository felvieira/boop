package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/chrisgreg/boop/server/internal/ids"
)

type supportTriageEvent struct {
	EventID       string  `json:"event_id"`
	ProjectID     string  `json:"project_id"`
	Project       string  `json:"project"`
	Source        string  `json:"source"`
	Type          string  `json:"type"`
	Level         string  `json:"level"`
	EventTitle    string  `json:"event_title"`
	EventBody     string  `json:"event_body"`
	EventData     string  `json:"event_data"`
	OccurredAt    string  `json:"occurred_at"`
	State         string  `json:"state"`
	Category      string  `json:"category"`
	Priority      string  `json:"priority"`
	Title         string  `json:"title"`
	Digest        string  `json:"digest"`
	Reason        string  `json:"reason"`
	Confidence    float64 `json:"confidence"`
	Model         string  `json:"model"`
	PromptVersion string  `json:"prompt_version"`
	Attempts      int     `json:"attempts"`
	NextAttemptAt string  `json:"next_attempt_at"`
	LeaseUntil    string  `json:"lease_until"`
	LastError     string  `json:"last_error"`
	ProcessedAt   string  `json:"processed_at"`
	CardID        string  `json:"card_id"`
	CardListID    string  `json:"card_list_id"`
	CardStatus    string  `json:"card_status"`
}

type supportTriageAuditEntry struct {
	ID            string  `json:"id"`
	Action        string  `json:"action"`
	State         string  `json:"state"`
	Category      string  `json:"category"`
	Priority      string  `json:"priority"`
	Title         string  `json:"title"`
	Digest        string  `json:"digest"`
	Reason        string  `json:"reason"`
	Confidence    float64 `json:"confidence"`
	Model         string  `json:"model"`
	PromptVersion string  `json:"prompt_version"`
	Error         string  `json:"error"`
	CreatedAt     string  `json:"created_at"`
}

func (s *Server) supportTriageEvents(w http.ResponseWriter, r *http.Request) {
	limit := boundedQueryInt(r, "limit", 40, 1, 100)
	offset := boundedQueryInt(r, "offset", 0, 0, 1000000)
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 160 {
		query = query[:160]
	}

	conditions := []string{"1=1"}
	args := make([]any, 0, 4)
	switch state {
	case "pending", "processing", "matched", "ignored":
		conditions = append(conditions, "t.state=?")
		args = append(args, state)
	case "failed":
		conditions = append(conditions, "t.state='pending' AND t.last_error<>''")
	case "":
	default:
		writeError(w, http.StatusBadRequest, "invalid_triage_filter", "state must be pending, processing, matched, ignored, or failed")
		return
	}
	if query != "" {
		conditions = append(conditions, `lower(e.title || ' ' || e.body || ' ' || p.name || ' ' || e.source || ' ' || t.title || ' ' || t.digest || ' ' || t.reason) LIKE ?`)
		args = append(args, "%"+strings.ToLower(query)+"%")
	}
	where := strings.Join(conditions, " AND ")

	var total int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*)
		FROM notification_triage t JOIN events e ON e.id=t.event_id JOIN projects p ON p.id=e.project_id
		WHERE `+where, args...).Scan(&total); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT e.id,e.project_id,p.name,e.source,e.type,e.level,e.title,e.body,e.payload_json,e.occurred_at,
		t.state,t.category,t.priority,t.title,t.digest,t.reason,t.confidence,t.model,t.prompt_version,t.attempts,t.next_attempt_at,t.lease_until,t.last_error,t.processed_at,
		COALESCE(c.id,''),COALESCE(c.list_id,''),COALESCE(c.status,'')
		FROM notification_triage t JOIN events e ON e.id=t.event_id JOIN projects p ON p.id=e.project_id
		LEFT JOIN support_cards c ON c.source_message_id=e.id AND c.source_account='boop:' || lower(p.slug)
		WHERE `+where+` ORDER BY CASE WHEN t.state='processing' THEN 0 WHEN t.state='pending' AND t.last_error<>'' THEN 1 WHEN t.state='pending' THEN 2 ELSE 3 END,
		COALESCE(NULLIF(t.processed_at,''),t.updated_at) DESC,e.created_at DESC LIMIT ? OFFSET ?`, append(append([]any{}, args...), limit, offset)...)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	items := make([]supportTriageEvent, 0, limit)
	for rows.Next() {
		var item supportTriageEvent
		if err := rows.Scan(&item.EventID, &item.ProjectID, &item.Project, &item.Source, &item.Type, &item.Level,
			&item.EventTitle, &item.EventBody, &item.EventData, &item.OccurredAt, &item.State, &item.Category, &item.Priority,
			&item.Title, &item.Digest, &item.Reason, &item.Confidence, &item.Model, &item.PromptVersion, &item.Attempts, &item.NextAttemptAt,
			&item.LeaseUntil, &item.LastError, &item.ProcessedAt, &item.CardID, &item.CardListID, &item.CardStatus); err != nil {
			s.fail(w, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) supportTriageHistory(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("id")
	var exists int
	if err := s.DB.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM notification_triage WHERE event_id=?)`, eventID).Scan(&exists); err != nil {
		s.fail(w, err)
		return
	}
	if exists == 0 {
		writeError(w, http.StatusNotFound, "triage_event_not_found", "the notification has no triage record")
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id,action,state,category,priority,title,digest,reason,confidence,model,prompt_version,error,created_at
		FROM support_triage_audit WHERE event_id=? ORDER BY created_at DESC,id DESC LIMIT 100`, eventID)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	entries := make([]supportTriageAuditEntry, 0)
	for rows.Next() {
		var entry supportTriageAuditEntry
		if err := rows.Scan(&entry.ID, &entry.Action, &entry.State, &entry.Category, &entry.Priority, &entry.Title, &entry.Digest,
			&entry.Reason, &entry.Confidence, &entry.Model, &entry.PromptVersion, &entry.Error, &entry.CreatedAt); err != nil {
			s.fail(w, err)
			return
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": entries})
}

func (s *Server) retrySupportTriageEvent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reprocess bool `json:"reprocess"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	eventID := r.PathValue("id")
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer tx.Rollback()
	var state, lastError string
	if err := tx.QueryRowContext(r.Context(), `SELECT state,last_error FROM notification_triage WHERE event_id=?`, eventID).Scan(&state, &lastError); err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "triage_event_not_found", "the notification has no triage record")
		return
	} else if err != nil {
		s.fail(w, err)
		return
	}
	if state == "processing" {
		writeError(w, http.StatusConflict, "triage_event_processing", "wait for the current classification to finish")
		return
	}
	if state == "matched" || state == "ignored" {
		if !in.Reprocess {
			writeError(w, http.StatusConflict, "explicit_reprocess_required", "set reprocess=true to classify a completed notification again")
			return
		}
	} else if state == "pending" && lastError == "" {
		writeError(w, http.StatusConflict, "triage_event_already_queued", "this notification is already waiting for classification")
		return
	} else if state != "pending" {
		writeError(w, http.StatusConflict, "invalid_triage_state", "this notification cannot be reprocessed in its current state")
		return
	}
	now := ids.Now()
	action := "retry"
	if state == "matched" || state == "ignored" {
		action = "reprocess"
	}
	res, err := tx.ExecContext(r.Context(), `UPDATE notification_triage
		SET state='pending',attempts=CASE WHEN ?='reprocess' THEN 0 ELSE attempts END,next_attempt_at=?,lease_until='',last_error='',
		category=CASE WHEN ?='reprocess' THEN 'other' ELSE category END,
		priority=CASE WHEN ?='reprocess' THEN 'normal' ELSE priority END,
		title=CASE WHEN ?='reprocess' THEN '' ELSE title END,
		digest=CASE WHEN ?='reprocess' THEN '' ELSE digest END,
		reason=CASE WHEN ?='reprocess' THEN '' ELSE reason END,
		confidence=CASE WHEN ?='reprocess' THEN 0 ELSE confidence END,
		model=CASE WHEN ?='reprocess' THEN '' ELSE model END,
		prompt_version=CASE WHEN ?='reprocess' THEN '' ELSE prompt_version END,
		processed_at='',updated_at=? WHERE event_id=? AND state=?`,
		action, now, action, action, action, action, action, action, action, action, now, eventID, state)
	if err != nil {
		s.fail(w, err)
		return
	}
	if changed, _ := res.RowsAffected(); changed != 1 {
		writeError(w, http.StatusConflict, "triage_event_changed", "the notification state changed; refresh and try again")
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO support_triage_audit(id,event_id,action,state,reason,created_at)
		VALUES(?,?,?, 'pending', ?, ?)`, ids.New("tra"), eventID, action,
		"Ação manual do administrador; reclassificação explícita.", now)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event_id": eventID, "state": "pending", "action": action, "next_attempt_at": now})
}

func boundedQueryInt(r *http.Request, key string, fallback, minValue, maxValue int) int {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	if parsed < minValue {
		return minValue
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}
