package support

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/chrisgreg/boop/server/internal/ids"
)

type BoardList struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Position  int    `json:"position"`
	Status    string `json:"status"`
	IsDefault bool   `json:"is_default"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type ChecklistItem struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Done      bool   `json:"done"`
	Position  int    `json:"position"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Comment struct {
	ID        string `json:"id"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
}

type CardInput struct {
	Title       string  `json:"title"`
	Product     string  `json:"product"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"`
	DueAt       string  `json:"due_at"`
	ListID      string  `json:"list_id"`
	Labels      []Label `json:"labels"`
}

type CardEdit = CardInput

var priorities = map[string]bool{"low": true, "normal": true, "high": true, "urgent": true}
var labelColors = map[string]bool{"purple": true, "blue": true, "green": true, "yellow": true, "orange": true, "red": true, "gray": true}

func validLabels(labels []Label) bool {
	if len(labels) > 20 {
		return false
	}
	seen := map[string]bool{}
	for i := range labels {
		labels[i].Name = strings.TrimSpace(labels[i].Name)
		labels[i].Color = strings.TrimSpace(labels[i].Color)
		key := strings.ToLower(labels[i].Name)
		if key == "" || len(labels[i].Name) > 48 || seen[key] || !labelColors[labels[i].Color] {
			return false
		}
		seen[key] = true
	}
	return true
}

func normalizeCardInput(in CardInput) (CardInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Product = strings.TrimSpace(in.Product)
	in.Priority = strings.TrimSpace(in.Priority)
	in.Description = strings.TrimSpace(in.Description)
	in.DueAt = strings.TrimSpace(in.DueAt)
	if in.Priority == "" {
		in.Priority = "normal"
	}
	if in.Product == "" {
		in.Product = "Boop"
	}
	if in.Labels == nil {
		in.Labels = []Label{}
	}
	if in.Title == "" || len(in.Title) > 500 || len(in.Product) > 200 || len(in.Description) > 200000 || !priorities[in.Priority] || !validLabels(in.Labels) {
		return in, ErrInvalid
	}
	if in.DueAt != "" {
		if _, err := ids.Parse(in.DueAt); err != nil {
			if _, err = time.Parse(time.RFC3339, in.DueAt); err != nil {
				return in, ErrInvalid
			}
		}
	}
	return in, nil
}

func (s *Store) Lists(ctx context.Context) ([]BoardList, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,position,status,is_default,created_at,updated_at FROM support_lists ORDER BY position,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []BoardList{}
	for rows.Next() {
		var list BoardList
		if err := rows.Scan(&list.ID, &list.Name, &list.Position, &list.Status, &list.IsDefault, &list.CreatedAt, &list.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, list)
	}
	return result, rows.Err()
}

func (s *Store) CreateList(ctx context.Context, name string) (*BoardList, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return nil, ErrInvalid
	}
	var duplicate int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM support_lists WHERE lower(name)=lower(?))`, name).Scan(&duplicate); err != nil {
		return nil, err
	}
	if duplicate != 0 {
		return nil, ErrInvalid
	}
	now, id := ids.Now(), ids.New("lst")
	var position int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1000)+1000 FROM support_lists`).Scan(&position); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO support_lists(id,name,position,status,is_default,created_at,updated_at) VALUES(?,?,?,'new',0,?,?)`, id, name, position, now, now); err != nil {
		return nil, err
	}
	return &BoardList{ID: id, Name: name, Position: position, Status: "new", CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) RenameList(ctx context.Context, id, name string) (*BoardList, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return nil, ErrInvalid
	}
	var duplicate int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM support_lists WHERE lower(name)=lower(?) AND id<>?)`, name, id).Scan(&duplicate); err != nil {
		return nil, err
	}
	if duplicate != 0 {
		return nil, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `UPDATE support_lists SET name=?,updated_at=? WHERE id=?`, name, ids.Now(), id)
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
	var list BoardList
	err = s.db.QueryRowContext(ctx, `SELECT id,name,position,status,is_default,created_at,updated_at FROM support_lists WHERE id=?`, id).Scan(&list.ID, &list.Name, &list.Position, &list.Status, &list.IsDefault, &list.CreatedAt, &list.UpdatedAt)
	return &list, err
}

func (s *Store) MoveList(ctx context.Context, id string, position int) error {
	if position < 0 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM support_lists ORDER BY position,id`)
	if err != nil {
		return err
	}
	var listIDs []string
	found := false
	for rows.Next() {
		var listID string
		if err := rows.Scan(&listID); err != nil {
			rows.Close()
			return err
		}
		if listID == id {
			found = true
		} else {
			listIDs = append(listIDs, listID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	if position > len(listIDs) {
		position = len(listIDs)
	}
	listIDs = append(listIDs, "")
	copy(listIDs[position+1:], listIDs[position:])
	listIDs[position] = id
	now := ids.Now()
	for i, listID := range listIDs {
		if _, err := tx.ExecContext(ctx, `UPDATE support_lists SET position=?,updated_at=? WHERE id=?`, i*1000, now, listID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteList(ctx context.Context, id string) error {
	var isDefault int
	if err := s.db.QueryRowContext(ctx, `SELECT is_default FROM support_lists WHERE id=?`, id).Scan(&isDefault); err == sql.ErrNoRows {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if isDefault == 1 {
		return ErrInvalid
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_cards WHERE list_id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return ErrInUse
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM support_lists WHERE id=?`, id)
	return err
}

func (s *Store) CreateCard(ctx context.Context, input CardInput) (*Card, error) {
	in, err := normalizeCardInput(input)
	if err != nil {
		return nil, err
	}
	if in.ListID == "" {
		if err := s.db.QueryRowContext(ctx, `SELECT id FROM support_lists WHERE is_default=1 AND status='new'`).Scan(&in.ListID); err != nil {
			return nil, err
		}
	}
	var status string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM support_lists WHERE id=?`, in.ListID).Scan(&status); err == sql.ErrNoRows {
		return nil, ErrInvalid
	} else if err != nil {
		return nil, err
	}
	labels, _ := json.Marshal(in.Labels)
	now, id := ids.Now(), ids.New("sup")
	var position int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(position),-1000)+1000 FROM support_cards WHERE list_id=? AND archived_at=''`, in.ListID).Scan(&position); err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO support_cards(id,source_account,source_thread_id,source_message_id,product,title,sender,reported_at,original_text,status,list_id,position,description,due_at,priority,labels_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,'',?,?,?, ?,?,?,?,?,?,?,?)`, id, "manual@boop.local", id, id, in.Product, in.Title, now, "Created manually in the Boop board.", status, in.ListID, position, in.Description, in.DueAt, in.Priority, string(labels), now, now)
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Store) UpdateCard(ctx context.Context, id string, input CardEdit) (*Card, error) {
	in, err := normalizeCardInput(input)
	if err != nil {
		return nil, err
	}
	labels, _ := json.Marshal(in.Labels)
	res, err := s.db.ExecContext(ctx, `UPDATE support_cards SET title=?,product=?,description=?,due_at=?,priority=?,labels_json=?,updated_at=? WHERE id=?`,
		in.Title, in.Product, in.Description, in.DueAt, in.Priority, string(labels), ids.Now(), id)
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

func (s *Store) MoveCard(ctx context.Context, id, listID string, position int) (*Card, error) {
	if position < 0 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var sourceList, status string
	err = tx.QueryRowContext(ctx, `SELECT list_id FROM support_cards WHERE id=? AND archived_at=''`, id).Scan(&sourceList)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT status FROM support_lists WHERE id=?`, listID).Scan(&status); err == sql.ErrNoRows {
		return nil, ErrInvalid
	} else if err != nil {
		return nil, err
	}
	idsInTarget, err := cardIDs(ctx, tx, listID, id)
	if err != nil {
		return nil, err
	}
	if position > len(idsInTarget) {
		position = len(idsInTarget)
	}
	idsInTarget = append(idsInTarget, "")
	copy(idsInTarget[position+1:], idsInTarget[position:])
	idsInTarget[position] = id
	now := ids.Now()
	if _, err = tx.ExecContext(ctx, `UPDATE support_cards SET list_id=?,status=?,updated_at=? WHERE id=?`, listID, status, now, id); err != nil {
		return nil, err
	}
	for i, cardID := range idsInTarget {
		if _, err = tx.ExecContext(ctx, `UPDATE support_cards SET position=? WHERE id=?`, i*1000, cardID); err != nil {
			return nil, err
		}
	}
	if sourceList != listID {
		idsInSource, readErr := cardIDs(ctx, tx, sourceList, "")
		if readErr != nil {
			return nil, readErr
		}
		for i, cardID := range idsInSource {
			if _, err = tx.ExecContext(ctx, `UPDATE support_cards SET position=? WHERE id=?`, i*1000, cardID); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func cardIDs(ctx context.Context, tx *sql.Tx, listID, except string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM support_cards WHERE list_id=? AND archived_at='' AND id<>? ORDER BY position,created_at DESC,id`, listID, except)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func (s *Store) SetArchived(ctx context.Context, id string, archived bool) (*Card, error) {
	var query string
	var args []any
	if archived {
		query, args = `UPDATE support_cards SET archived_at=?,updated_at=? WHERE id=? AND archived_at=''`, []any{ids.Now(), ids.Now(), id}
	} else {
		query, args = `UPDATE support_cards SET archived_at='',position=(SELECT COALESCE(MAX(position),-1000)+1000 FROM support_cards c WHERE c.list_id=support_cards.list_id AND c.archived_at=''),updated_at=? WHERE id=? AND archived_at<>''`, []any{ids.Now(), id}
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		if _, err := s.Get(ctx, id); err != nil {
			return nil, err
		}
	}
	return s.Get(ctx, id)
}

func (s *Store) AddComment(ctx context.Context, cardID, body string) (*Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 10000 {
		return nil, ErrInvalid
	}
	if _, err := s.Get(ctx, cardID); err != nil {
		return nil, err
	}
	comment := &Comment{ID: ids.New("cmt"), Body: body, Author: "Admin", CreatedAt: ids.Now()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO support_comments(id,card_id,body,author,created_at) VALUES(?,?,?,?,?)`, comment.ID, cardID, comment.Body, comment.Author, comment.CreatedAt)
	return comment, err
}

func (s *Store) DeleteComment(ctx context.Context, cardID, commentID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM support_comments WHERE card_id=? AND id=?`, cardID, commentID)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) AddChecklistItem(ctx context.Context, cardID, text string) (*ChecklistItem, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 500 {
		return nil, ErrInvalid
	}
	if _, err := s.Get(ctx, cardID); err != nil {
		return nil, err
	}
	var position int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_checklist_items WHERE card_id=?`, cardID).Scan(&position); err != nil {
		return nil, err
	}
	item := &ChecklistItem{ID: ids.New("chk"), Text: text, Position: position, CreatedAt: ids.Now(), UpdatedAt: ids.Now()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO support_checklist_items(id,card_id,text,done,position,created_at,updated_at) VALUES(?,?,?,0,?,?,?)`, item.ID, cardID, item.Text, item.Position, item.CreatedAt, item.UpdatedAt)
	return item, err
}

func (s *Store) UpdateChecklistItem(ctx context.Context, cardID, itemID string, done bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE support_checklist_items SET done=?,updated_at=? WHERE card_id=? AND id=?`, done, ids.Now(), cardID, itemID)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteChecklistItem(ctx context.Context, cardID, itemID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM support_checklist_items WHERE card_id=? AND id=?`, cardID, itemID)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) checklist(ctx context.Context, cardID string) ([]ChecklistItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,text,done,position,created_at,updated_at FROM support_checklist_items WHERE card_id=? ORDER BY position,created_at,id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ChecklistItem{}
	for rows.Next() {
		var item ChecklistItem
		if err := rows.Scan(&item.ID, &item.Text, &item.Done, &item.Position, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) comments(ctx context.Context, cardID string) ([]Comment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,body,author,created_at FROM support_comments WHERE card_id=? ORDER BY created_at,id`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Comment{}
	for rows.Next() {
		var item Comment
		if err := rows.Scan(&item.ID, &item.Body, &item.Author, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) AddAttachments(ctx context.Context, cardID string, files []Attachment) error {
	if _, err := s.Get(ctx, cardID); err != nil {
		return err
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, file := range files {
		if !strings.HasPrefix(file.ContentType, "image/") || len(file.Content) == 0 {
			return ErrInvalid
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO support_attachments(id,card_id,filename,content_type,content,created_at) VALUES(?,?,?,?,?,?)`, ids.New("att"), cardID, file.Filename, file.ContentType, file.Content, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteAttachment(ctx context.Context, cardID, attachmentID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM support_attachments WHERE card_id=? AND id=?`, cardID, attachmentID)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}
