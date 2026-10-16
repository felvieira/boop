// Package support stores private board cards, notification sources, and attachments.
package support

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/chrisgreg/boop/server/internal/ids"
)

var (
	ErrNotFound = errors.New("support card not found")
	ErrInvalid  = errors.New("invalid support card")
	ErrInUse    = errors.New("support list is not empty")
)

var statuses = map[string]bool{"new": true, "in_progress": true, "blocked": true, "done": true}

type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Content     []byte `json:"-"`
	CreatedAt   string `json:"created_at"`
}

type Card struct {
	ID              string          `json:"id"`
	SourceAccount   string          `json:"source_account"`
	SourceThreadID  string          `json:"source_thread_id"`
	SourceMessageID string          `json:"source_message_id"`
	Product         string          `json:"product"`
	Title           string          `json:"title"`
	Sender          string          `json:"sender"`
	ReportedAt      string          `json:"reported_at"`
	OriginalText    string          `json:"original_text"`
	Status          string          `json:"status"`
	ListID          string          `json:"list_id"`
	Position        int             `json:"position"`
	Description     string          `json:"description"`
	DueAt           string          `json:"due_at"`
	Priority        string          `json:"priority"`
	Labels          []Label         `json:"labels"`
	ArchivedAt      string          `json:"archived_at"`
	Manual          bool            `json:"manual"`
	CommentsCount   int             `json:"comments_count"`
	ChecklistDone   int             `json:"checklist_done"`
	ChecklistTotal  int             `json:"checklist_total"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
	Attachments     []Attachment    `json:"attachments"`
	Checklist       []ChecklistItem `json:"checklist"`
	Comments        []Comment       `json:"comments"`
}

type Input struct {
	SourceAccount   string `json:"source_account"`
	SourceThreadID  string `json:"source_thread_id"`
	SourceMessageID string `json:"source_message_id"`
	Product         string `json:"product"`
	Title           string `json:"title"`
	Sender          string `json:"sender"`
	ReportedAt      string `json:"reported_at"`
	OriginalText    string `json:"original_text"`
	Description     string `json:"description"`
}

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func valid(in Input) bool {
	_, err := time.Parse(time.RFC3339, in.ReportedAt)
	return strings.TrimSpace(in.SourceAccount) != "" && len(in.SourceAccount) <= 320 &&
		strings.TrimSpace(in.SourceThreadID) != "" && len(in.SourceThreadID) <= 512 &&
		strings.TrimSpace(in.SourceMessageID) != "" && len(in.SourceMessageID) <= 512 &&
		strings.TrimSpace(in.Product) != "" && len(in.Product) <= 200 &&
		strings.TrimSpace(in.Title) != "" && len(in.Title) <= 500 && len(in.Sender) <= 500 &&
		strings.TrimSpace(in.OriginalText) != "" && len(in.OriginalText) <= 200000 && len(in.Description) <= 200000 && err == nil
}

// Upsert is idempotent on the Gmail account/conversation/message identity. New
// files replace old files only when an update contains attachments.
func (s *Store) Upsert(ctx context.Context, in Input, files []Attachment) (*Card, error) {
	if !valid(in) {
		return nil, ErrInvalid
	}
	in.SourceAccount = strings.ToLower(strings.TrimSpace(in.SourceAccount))
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var id, status, created string
	err = tx.QueryRowContext(ctx, `SELECT id,status,created_at FROM support_cards WHERE source_account=? AND source_thread_id=? AND source_message_id=?`, in.SourceAccount, in.SourceThreadID, in.SourceMessageID).Scan(&id, &status, &created)
	if err == sql.ErrNoRows {
		id, status, created = ids.New("sup"), "new", now
		var listID string
		if err = tx.QueryRowContext(ctx, `SELECT id FROM support_lists WHERE is_default=1 AND status=?`, status).Scan(&listID); err != nil {
			return nil, err
		}
		var position int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(position),0)-1000 FROM support_cards WHERE list_id=? AND archived_at=''`, listID).Scan(&position); err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO support_cards (id,source_account,source_thread_id,source_message_id,product,title,sender,reported_at,original_text,status,list_id,position,description,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, in.SourceAccount, in.SourceThreadID, in.SourceMessageID, in.Product, in.Title, in.Sender, in.ReportedAt, in.OriginalText, status, listID, position, in.Description, created, now)
	} else if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE support_cards SET product=?,title=?,sender=?,reported_at=?,original_text=?,description=CASE WHEN ?<>'' THEN ? ELSE description END,updated_at=? WHERE id=?`, in.Product, in.Title, in.Sender, in.ReportedAt, in.OriginalText, in.Description, in.Description, now, id)
	}
	if err != nil {
		return nil, err
	}
	if len(files) > 0 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM support_attachments WHERE card_id=?`, id); err != nil {
			return nil, err
		}
		for _, file := range files {
			_, err = tx.ExecContext(ctx, `INSERT INTO support_attachments(id,card_id,filename,content_type,content,created_at) VALUES(?,?,?,?,?,?)`, ids.New("att"), id, file.Filename, file.ContentType, file.Content, now)
			if err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Store) List(ctx context.Context) ([]Card, error) {
	return s.ListWithArchived(ctx, false)
}

type rowScanner interface{ Scan(...any) error }

const cardSelect = `SELECT c.id,c.source_account,c.source_thread_id,c.source_message_id,c.product,c.title,c.sender,c.reported_at,c.original_text,c.status,
	c.list_id,c.position,c.description,c.due_at,c.priority,c.labels_json,c.archived_at,c.created_at,c.updated_at,
	(SELECT COUNT(*) FROM support_comments sc WHERE sc.card_id=c.id),
	(SELECT COALESCE(SUM(done),0) FROM support_checklist_items si WHERE si.card_id=c.id),
	(SELECT COUNT(*) FROM support_checklist_items si WHERE si.card_id=c.id)
	FROM support_cards c`

func scanCard(row rowScanner) (Card, error) {
	var card Card
	var labelsJSON string
	err := row.Scan(&card.ID, &card.SourceAccount, &card.SourceThreadID, &card.SourceMessageID,
		&card.Product, &card.Title, &card.Sender, &card.ReportedAt, &card.OriginalText, &card.Status,
		&card.ListID, &card.Position, &card.Description, &card.DueAt, &card.Priority, &labelsJSON, &card.ArchivedAt,
		&card.CreatedAt, &card.UpdatedAt, &card.CommentsCount, &card.ChecklistDone, &card.ChecklistTotal)
	if err != nil {
		return Card{}, err
	}
	card.Manual = card.SourceAccount == "manual@boop.local"
	card.Labels = decodeLabels(labelsJSON)
	card.Attachments = []Attachment{}
	card.Checklist = []ChecklistItem{}
	card.Comments = []Comment{}
	return card, nil
}

func decodeLabels(raw string) []Label {
	var labels []Label
	if json.Unmarshal([]byte(raw), &labels) != nil || labels == nil {
		return []Label{}
	}
	return labels
}

func (s *Store) ListWithArchived(ctx context.Context, includeArchived bool) ([]Card, error) {
	archivedFlag := 0
	if includeArchived {
		archivedFlag = 1
	}
	rows, err := s.db.QueryContext(ctx, cardSelect+` WHERE (?=1 OR c.archived_at='') ORDER BY c.list_id, c.position, c.created_at DESC, c.id`, archivedFlag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []Card{}
	for rows.Next() {
		card, scanErr := scanCard(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		cards = append(cards, card)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for i := range cards {
		cards[i].Attachments, err = s.attachments(ctx, cards[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return cards, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Card, error) {
	card, err := scanCard(s.db.QueryRowContext(ctx, cardSelect+` WHERE c.id=?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	card.Attachments, err = s.attachments(ctx, id)
	if err != nil {
		return nil, err
	}
	card.Checklist, err = s.checklist(ctx, id)
	if err != nil {
		return nil, err
	}
	card.Comments, err = s.comments(ctx, id)
	return &card, err
}

func (s *Store) SetStatus(ctx context.Context, id, status string) (*Card, error) {
	if !statuses[status] {
		return nil, ErrInvalid
	}
	var listID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM support_lists WHERE is_default=1 AND status=?`, status).Scan(&listID); err != nil {
		return nil, err
	}
	var position int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_cards WHERE list_id=? AND archived_at=''`, listID).Scan(&position); err != nil {
		return nil, err
	}
	return s.MoveCard(ctx, id, listID, position)
}

func (s *Store) Attachment(ctx context.Context, cardID, attachmentID string) (*Attachment, error) {
	var attachment Attachment
	err := s.db.QueryRowContext(ctx, `SELECT id,filename,content_type,content,created_at FROM support_attachments WHERE card_id=? AND id=?`, cardID, attachmentID).Scan(&attachment.ID, &attachment.Filename, &attachment.ContentType, &attachment.Content, &attachment.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &attachment, nil
}

func (s *Store) attachments(ctx context.Context, cardID string) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,filename,content_type,created_at FROM support_attachments WHERE card_id=? ORDER BY created_at,id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attachments := []Attachment{}
	for rows.Next() {
		var attachment Attachment
		if err = rows.Scan(&attachment.ID, &attachment.Filename, &attachment.ContentType, &attachment.CreatedAt); err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, rows.Err()
}
