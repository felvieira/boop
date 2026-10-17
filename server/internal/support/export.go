package support

import (
	"context"
	"errors"
	"regexp"
)

const maxSupportExportCards = 5000

var exportSecretPattern = regexp.MustCompile(`(?i)\b(api[_ -]?key|access[_ -]?token|refresh[_ -]?token|password|passwd|secret|authorization)\s*[:=]\s*(?:bearer\s+)?[^\s,;]+`)

type ExportRow struct {
	CardID       string `json:"card_id"`
	Product      string `json:"product"`
	Title        string `json:"title"`
	Sender       string `json:"sender"`
	ReportedAt   string `json:"reported_at"`
	List         string `json:"list"`
	Status       string `json:"status"`
	Priority     string `json:"priority"`
	Category     string `json:"category"`
	Description  string `json:"description"`
	ReviewState  string `json:"review_state"`
	DueAt        string `json:"due_at"`
	ArchivedAt   string `json:"archived_at"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	AuditAction  string `json:"audit_action"`
	AuditField   string `json:"audit_field"`
	AuditOld     string `json:"audit_old_value"`
	AuditNew     string `json:"audit_new_value"`
	AuditActor   string `json:"audit_actor"`
	AuditCreated string `json:"audit_created_at"`
}

func (s *Store) ExportRows(ctx context.Context, filter CardFilter) ([]ExportRow, error) {
	where, args, err := cardFilterWhere(filter)
	if err != nil {
		return nil, err
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_cards c WHERE `+where, args...).Scan(&count); err != nil {
		return nil, err
	}
	if count > maxSupportExportCards {
		return nil, ErrExportLimit
	}
	query := `SELECT c.id,c.product,c.title,c.sender,c.reported_at,COALESCE(l.name,''),c.status,c.priority,c.category,c.description,c.review_state,c.due_at,c.archived_at,c.created_at,c.updated_at,
		COALESCE(a.action,''),COALESCE(a.field,''),COALESCE(a.old_value,''),COALESCE(a.new_value,''),COALESCE(a.actor,''),COALESCE(a.created_at,'')
		FROM support_cards c
		LEFT JOIN support_lists l ON l.id=c.list_id
		LEFT JOIN support_card_audit a ON a.card_id=c.id
		WHERE ` + where + ` ORDER BY c.created_at DESC,c.id,a.created_at,a.id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ExportRow, 0, count)
	for rows.Next() {
		var row ExportRow
		if err := rows.Scan(&row.CardID, &row.Product, &row.Title, &row.Sender, &row.ReportedAt, &row.List,
			&row.Status, &row.Priority, &row.Category, &row.Description, &row.ReviewState, &row.DueAt,
			&row.ArchivedAt, &row.CreatedAt, &row.UpdatedAt, &row.AuditAction, &row.AuditField,
			&row.AuditOld, &row.AuditNew, &row.AuditActor, &row.AuditCreated); err != nil {
			return nil, err
		}
		row.Description = exportSecretPattern.ReplaceAllString(row.Description, "$1=[redacted]")
		row.AuditOld = exportSecretPattern.ReplaceAllString(row.AuditOld, "$1=[redacted]")
		row.AuditNew = exportSecretPattern.ReplaceAllString(row.AuditNew, "$1=[redacted]")
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

var ErrExportLimit = errors.New("support export exceeds the card limit")
