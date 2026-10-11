// Package support stores private, email-originated support cards.
package support

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/chrisgreg/boop/server/internal/ids"
)

var (
	ErrNotFound = errors.New("support card not found")
	ErrInvalid  = errors.New("invalid support card")
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
	ID              string       `json:"id"`
	SourceAccount   string       `json:"source_account"`
	SourceThreadID  string       `json:"source_thread_id"`
	SourceMessageID string       `json:"source_message_id"`
	Product         string       `json:"product"`
	Title           string       `json:"title"`
	Sender          string       `json:"sender"`
	ReportedAt      string       `json:"reported_at"`
	OriginalText    string       `json:"original_text"`
	Status          string       `json:"status"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
	Attachments     []Attachment `json:"attachments"`
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
		strings.TrimSpace(in.OriginalText) != "" && len(in.OriginalText) <= 200000 && err == nil
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
		_, err = tx.ExecContext(ctx, `INSERT INTO support_cards (id,source_account,source_thread_id,source_message_id,product,title,sender,reported_at,original_text,status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, id, in.SourceAccount, in.SourceThreadID, in.SourceMessageID, in.Product, in.Title, in.Sender, in.ReportedAt, in.OriginalText, status, created, now)
	} else if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE support_cards SET product=?,title=?,sender=?,reported_at=?,original_text=?,updated_at=? WHERE id=?`, in.Product, in.Title, in.Sender, in.ReportedAt, in.OriginalText, now, id)
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
	rows, err := s.db.QueryContext(ctx, `SELECT id,source_account,source_thread_id,source_message_id,product,title,sender,reported_at,original_text,status,created_at,updated_at FROM support_cards ORDER BY CASE status WHEN 'new' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'blocked' THEN 3 ELSE 4 END, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cards := []Card{}
	for rows.Next() {
		var card Card
		if err = rows.Scan(&card.ID, &card.SourceAccount, &card.SourceThreadID, &card.SourceMessageID, &card.Product, &card.Title, &card.Sender, &card.ReportedAt, &card.OriginalText, &card.Status, &card.CreatedAt, &card.UpdatedAt); err != nil {
			return nil, err
		}
		card.Attachments = []Attachment{}
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
	card := &Card{}
	err := s.db.QueryRowContext(ctx, `SELECT id,source_account,source_thread_id,source_message_id,product,title,sender,reported_at,original_text,status,created_at,updated_at FROM support_cards WHERE id=?`, id).Scan(&card.ID, &card.SourceAccount, &card.SourceThreadID, &card.SourceMessageID, &card.Product, &card.Title, &card.Sender, &card.ReportedAt, &card.OriginalText, &card.Status, &card.CreatedAt, &card.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	card.Attachments, err = s.attachments(ctx, id)
	return card, err
}

func (s *Store) SetStatus(ctx context.Context, id, status string) (*Card, error) {
	if !statuses[status] {
		return nil, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `UPDATE support_cards SET status=?,updated_at=? WHERE id=?`, status, ids.Now(), id)
	if err != nil {
		return nil, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrNotFound
	}
	return s.Get(ctx, id)
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
