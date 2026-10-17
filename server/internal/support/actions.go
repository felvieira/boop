package support

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/chrisgreg/boop/server/internal/ids"
)

var (
	ErrActionState = errors.New("support action is not in a state that allows this operation")
	ErrActionKind  = errors.New("support action kind does not allow this operation")
	githubRepoName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,38}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
	actionEmail    = regexp.MustCompile(`^[^<>@\s]+@[^<>@\s]+\.[^<>@\s]+$`)
)

type Action struct {
	ID          string `json:"id"`
	CardID      string `json:"card_id"`
	Kind        string `json:"kind"`
	State       string `json:"state"`
	Destination string `json:"destination"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	ExternalID  string `json:"external_id"`
	ExternalURL string `json:"external_url"`
	Error       string `json:"error"`
	ApprovedBy  string `json:"approved_by"`
	ApprovedAt  string `json:"approved_at"`
	CompletedAt string `json:"completed_at"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type ActionInput struct {
	Kind        string `json:"kind"`
	Destination string `json:"destination"`
	Title       string `json:"title"`
	Body        string `json:"body"`
}

type ActionAuditEntry struct {
	ID            string `json:"id"`
	Action        string `json:"action"`
	Actor         string `json:"actor"`
	Summary       string `json:"summary"`
	PayloadSHA256 string `json:"payload_sha256"`
	ExternalID    string `json:"external_id"`
	Result        string `json:"result"`
	Error         string `json:"error"`
	CreatedAt     string `json:"created_at"`
}

type ExternalIssue struct {
	ID  string
	URL string
}

func normalizeActionInput(in ActionInput) (ActionInput, error) {
	in.Kind = strings.TrimSpace(strings.ToLower(in.Kind))
	in.Destination = strings.TrimSpace(in.Destination)
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	if in.Kind != "github_issue" && in.Kind != "customer_reply" && in.Kind != "technical_plan" {
		return ActionInput{}, ErrInvalid
	}
	if in.Title == "" || len([]rune(in.Title)) > 256 || in.Body == "" || len([]rune(in.Body)) > 60000 {
		return ActionInput{}, ErrInvalid
	}
	if in.Kind == "github_issue" {
		if !ValidGitHubRepository(in.Destination) {
			return ActionInput{}, ErrInvalid
		}
	} else if in.Kind == "customer_reply" {
		if len(in.Destination) > 320 || (in.Destination != "" && !actionEmail.MatchString(in.Destination)) {
			return ActionInput{}, ErrInvalid
		}
	} else if in.Destination != "" {
		return ActionInput{}, ErrInvalid
	}
	return in, nil
}

func ValidGitHubRepository(repo string) bool {
	parts := strings.SplitN(strings.TrimSpace(repo), "/", 2)
	return len(parts) == 2 && githubRepoName.MatchString(strings.TrimSpace(repo)) &&
		parts[0] != "." && parts[0] != ".." && parts[1] != "." && parts[1] != ".."
}

func scanAction(row rowScanner) (Action, error) {
	var action Action
	err := row.Scan(&action.ID, &action.CardID, &action.Kind, &action.State, &action.Destination, &action.Title, &action.Body,
		&action.ExternalID, &action.ExternalURL, &action.Error, &action.ApprovedBy, &action.ApprovedAt, &action.CompletedAt,
		&action.CreatedAt, &action.UpdatedAt)
	return action, err
}

const actionSelect = `SELECT id,card_id,kind,state,destination,title,body,external_id,external_url,error,approved_by,approved_at,completed_at,created_at,updated_at FROM support_actions`

func (s *Store) ListActions(ctx context.Context, cardID string) ([]Action, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM support_cards WHERE id=?)`, cardID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, actionSelect+` WHERE card_id=? ORDER BY created_at DESC,id DESC`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	actions := make([]Action, 0)
	for rows.Next() {
		action, scanErr := scanAction(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		actions = append(actions, action)
	}
	return actions, rows.Err()
}

func (s *Store) GetAction(ctx context.Context, id string) (*Action, error) {
	action, err := scanAction(s.db.QueryRowContext(ctx, actionSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &action, nil
}

func (s *Store) CreateAction(ctx context.Context, cardID string, in ActionInput, actor string) (*Action, error) {
	in, err := normalizeActionInput(in)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	now := ids.Now()
	id := ids.New("sac")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM support_cards WHERE id=?)`, cardID).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, ErrNotFound
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO support_actions(id,card_id,kind,state,destination,title,body,idempotency_key,created_at,updated_at)
		VALUES(?,?,?,'draft',?,?,?,?,?,?)`, id, cardID, in.Kind, in.Destination, in.Title, in.Body, id, now, now)
	if err != nil {
		return nil, err
	}
	if err = insertActionAudit(ctx, tx, id, "created", actor, "Draft prepared for review.", actionPayloadHash(in), "", "draft", ""); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAction(ctx, id)
}

func (s *Store) UpdateAction(ctx context.Context, id string, in ActionInput, actor string) (*Action, error) {
	in, err := normalizeActionInput(in)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM support_actions WHERE id=?`, id).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if state != "draft" {
		return nil, ErrActionState
	}
	result, err := tx.ExecContext(ctx, `UPDATE support_actions SET kind=?,destination=?,title=?,body=?,updated_at=? WHERE id=? AND state='draft'`,
		in.Kind, in.Destination, in.Title, in.Body, now, id)
	if err != nil {
		return nil, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return nil, ErrActionState
	}
	if err = insertActionAudit(ctx, tx, id, "edited", actor, "Draft content updated.", actionPayloadHash(in), "", "draft", ""); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAction(ctx, id)
}

func (s *Store) CancelAction(ctx context.Context, id, actor string) error {
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE support_actions SET state='cancelled',updated_at=? WHERE id=? AND state='draft'`, now, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM support_actions WHERE id=?)`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		return ErrActionState
	}
	if err = insertActionAudit(ctx, tx, id, "cancelled", actor, "Draft cancelled before execution.", "", "", "cancelled", ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ApproveReply(ctx context.Context, id, actor string) (*Action, error) {
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var action Action
	if err := tx.QueryRowContext(ctx, `SELECT kind,state,destination,title,body FROM support_actions WHERE id=?`, id).Scan(&action.Kind, &action.State, &action.Destination, &action.Title, &action.Body); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if action.Kind != "customer_reply" {
		return nil, ErrActionKind
	}
	if action.State != "draft" {
		return nil, ErrActionState
	}
	if _, err = tx.ExecContext(ctx, `UPDATE support_actions SET state='approved',approved_by=?,approved_at=?,updated_at=? WHERE id=? AND state='draft'`, actor, now, now, id); err != nil {
		return nil, err
	}
	if err = insertActionAudit(ctx, tx, id, "approved", actor, "Reply approved for manual copy; Boop did not send it.", actionPayloadHash(ActionInput{Kind: action.Kind, Destination: action.Destination, Title: action.Title, Body: action.Body}), "", "approved", ""); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAction(ctx, id)
}

func (s *Store) BeginGitHubExecution(ctx context.Context, id, actor string) (*Action, error) {
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	action, err := scanAction(tx.QueryRowContext(ctx, actionSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if action.Kind != "github_issue" {
		return nil, ErrActionKind
	}
	if action.State == "completed" {
		return &action, nil
	}
	if action.State != "draft" && action.State != "failed" {
		return nil, ErrActionState
	}
	if _, err := tx.ExecContext(ctx, `UPDATE support_actions SET state='executing',approved_by=?,approved_at=?,error='',updated_at=? WHERE id=? AND state=?`, actor, now, now, id, action.State); err != nil {
		return nil, err
	}
	approvalSummary := "GitHub issue creation approved by the administrator."
	if action.State == "failed" {
		approvalSummary = "GitHub issue retry approved after reviewing the previous failure."
	}
	if err = insertActionAudit(ctx, tx, id, "approved", actor, approvalSummary, actionPayloadHash(ActionInput{Kind: action.Kind, Destination: action.Destination, Title: action.Title, Body: action.Body}), "", "approved", ""); err != nil {
		return nil, err
	}
	if err = insertActionAudit(ctx, tx, id, "execution_started", actor, "GitHub issue creation started.", actionPayloadHash(ActionInput{Kind: action.Kind, Destination: action.Destination, Title: action.Title, Body: action.Body}), "", "executing", ""); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAction(ctx, id)
}

func (s *Store) FinishGitHubExecution(ctx context.Context, id, actor string, issue ExternalIssue, reconciled bool) (*Action, error) {
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE support_actions SET state='completed',external_id=?,external_url=?,error='',completed_at=?,updated_at=? WHERE id=? AND state IN ('executing','needs_reconciliation')`, issue.ID, issue.URL, now, now, id)
	if err != nil {
		return nil, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return nil, ErrActionState
	}
	auditAction, summary, outcome := "executed", "GitHub issue created.", "created"
	if reconciled {
		auditAction, summary, outcome = "reconciled", "Existing GitHub issue found by the Boop idempotency marker.", "reconciled"
	}
	if err = insertActionAudit(ctx, tx, id, auditAction, actor, summary, "", issue.ID, outcome, ""); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAction(ctx, id)
}

func (s *Store) FailGitHubExecution(ctx context.Context, id, actor, message string, uncertain bool) error {
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	state, actionName := "failed", "execution_failed"
	if uncertain {
		state, actionName = "needs_reconciliation", "needs_reconciliation"
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE support_actions SET state=?,error=?,updated_at=? WHERE id=? AND state='executing'`, state, message, now, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrActionState
	}
	if err = insertActionAudit(ctx, tx, id, actionName, actor, message, "", "", state, message); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MarkGitHubNeedsReconciliation(ctx context.Context, id, actor, message string) error {
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE support_actions SET state='needs_reconciliation',error=?,updated_at=? WHERE id=? AND state='executing'`, message, now, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrActionState
	}
	if err = insertActionAudit(ctx, tx, id, "needs_reconciliation", actor, message, "", "", "needs_reconciliation", message); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AuthorizeGitHubRetry(ctx context.Context, id, actor string) error {
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE support_actions SET state='failed',error='Administrator confirmed no matching issue exists.',updated_at=? WHERE id=? AND state='needs_reconciliation'`, now, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM support_actions WHERE id=?)`, id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		return ErrActionState
	}
	if err = insertActionAudit(ctx, tx, id, "retry_authorized", actor, "Administrator confirmed that the issue is absent from GitHub.", "", "", "retry_authorized", ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CompleteTechnicalPlan(ctx context.Context, id, actor string) (*Action, error) {
	if strings.TrimSpace(actor) == "" {
		actor = "Admin"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	action, err := scanAction(tx.QueryRowContext(ctx, actionSelect+` WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if action.Kind != "technical_plan" {
		return nil, ErrActionKind
	}
	if action.State != "draft" {
		return nil, ErrActionState
	}
	items := planChecklistItems(action.Body)
	if len(items) == 0 {
		return nil, ErrInvalid
	}
	var position int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),0) FROM support_checklist_items WHERE card_id=?`, action.CardID).Scan(&position); err != nil {
		return nil, err
	}
	now := ids.Now()
	if _, err = tx.ExecContext(ctx, `UPDATE support_actions SET state='executing',approved_by=?,approved_at=?,updated_at=? WHERE id=? AND state='draft'`, actor, now, now, id); err != nil {
		return nil, err
	}
	if err = insertActionAudit(ctx, tx, id, "approved", actor, "Technical plan approved and will become checklist items.", actionPayloadHash(ActionInput{Kind: action.Kind, Title: action.Title, Body: action.Body}), "", "approved", ""); err != nil {
		return nil, err
	}
	for _, item := range items {
		position += 1000
		if _, err = tx.ExecContext(ctx, `INSERT INTO support_checklist_items(id,card_id,text,done,position,created_at,updated_at) VALUES(?,?,?,0,?,?,?)`, ids.New("chk"), action.CardID, item, position, now, now); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE support_actions SET state='completed',completed_at=?,updated_at=? WHERE id=? AND state='executing'`, now, now, id); err != nil {
		return nil, err
	}
	if err = insertActionAudit(ctx, tx, id, "executed", actor, fmt.Sprintf("Added %d approved plan steps to the card checklist.", len(items)), "", "", "checklist_updated", ""); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetAction(ctx, id)
}

func (s *Store) ActionHistory(ctx context.Context, id string) ([]ActionAuditEntry, error) {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM support_actions WHERE id=?)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,action,actor,summary,payload_sha256,external_id,result,error,created_at FROM support_action_audit WHERE action_id=? ORDER BY created_at DESC,id DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]ActionAuditEntry, 0)
	for rows.Next() {
		var entry ActionAuditEntry
		if err := rows.Scan(&entry.ID, &entry.Action, &entry.Actor, &entry.Summary, &entry.PayloadSHA256, &entry.ExternalID, &entry.Result, &entry.Error, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func insertActionAudit(ctx context.Context, tx *sql.Tx, actionID, action, actor, summary, payloadHash, externalID, result, message string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO support_action_audit(id,action_id,action,actor,summary,payload_sha256,external_id,result,error,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		ids.New("saa"), actionID, action, actor, summary, payloadHash, externalID, result, message, ids.Now())
	return err
}

func actionPayloadHash(in ActionInput) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{in.Kind, in.Destination, in.Title, in.Body}, "\x00")))
	return fmt.Sprintf("%x", sum[:])
}

func planChecklistItems(body string) []string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	items := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*• ")
		if len(line) > 2 && line[0] >= '0' && line[0] <= '9' && (line[1] == '.' || line[1] == ')') {
			line = strings.TrimSpace(line[2:])
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len([]rune(line)) > 500 {
			line = string([]rune(line)[:500])
		}
		items = append(items, line)
		if len(items) == 50 {
			break
		}
	}
	return items
}
