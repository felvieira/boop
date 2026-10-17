package support

import (
	"context"
	"strings"
	"time"
)

type CardFilter struct {
	IncludeArchived bool
	Query           string
	Product         string
	Category        string
	Priority        string
	ListID          string
	ReviewState     string
	ReportedFrom    string
	ReportedTo      string
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func validDateFilter(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func cardFilterWhere(filter CardFilter) (string, []any, error) {
	if filter.Query != "" && len(filter.Query) > 200 {
		filter.Query = filter.Query[:200]
	}
	if len(filter.Product) > 200 || len(filter.ListID) > 100 || !validDateFilter(filter.ReportedFrom) || !validDateFilter(filter.ReportedTo) {
		return "", nil, ErrInvalid
	}
	if filter.Category != "" && !categories[filter.Category] {
		return "", nil, ErrInvalid
	}
	if filter.Priority != "" && !priorities[filter.Priority] {
		return "", nil, ErrInvalid
	}
	if filter.ReviewState != "" && filter.ReviewState != "needs_review" && filter.ReviewState != "reviewed" {
		return "", nil, ErrInvalid
	}
	conditions := []string{"(?=1 OR c.archived_at='')"}
	args := []any{filter.IncludeArchived}
	if filter.Query != "" {
		conditions = append(conditions, `lower(c.title || ' ' || c.product || ' ' || c.description || ' ' || c.original_text || ' ' || c.sender) LIKE ? ESCAPE '\'`)
		args = append(args, "%"+strings.ToLower(escapeLike(filter.Query))+"%")
	}
	if filter.Product != "" {
		conditions = append(conditions, "lower(c.product)=lower(?)")
		args = append(args, filter.Product)
	}
	if filter.Category != "" {
		conditions = append(conditions, "c.category=?")
		args = append(args, filter.Category)
	}
	if filter.Priority != "" {
		conditions = append(conditions, "c.priority=?")
		args = append(args, filter.Priority)
	}
	if filter.ListID != "" {
		conditions = append(conditions, "c.list_id=?")
		args = append(args, filter.ListID)
	}
	if filter.ReviewState != "" {
		conditions = append(conditions, "c.review_state=?")
		args = append(args, filter.ReviewState)
	}
	if filter.ReportedFrom != "" {
		conditions = append(conditions, "substr(c.reported_at,1,10)>=?")
		args = append(args, filter.ReportedFrom)
	}
	if filter.ReportedTo != "" {
		conditions = append(conditions, "substr(c.reported_at,1,10)<=?")
		args = append(args, filter.ReportedTo)
	}
	return strings.Join(conditions, " AND "), args, nil
}

func (s *Store) ListFiltered(ctx context.Context, filter CardFilter) ([]Card, error) {
	where, args, err := cardFilterWhere(filter)
	if err != nil {
		return nil, err
	}
	query := cardSelect + ` WHERE ` + where + ` ORDER BY c.list_id,c.position,c.created_at DESC,c.id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	cards := make([]Card, 0)
	for rows.Next() {
		card, scanErr := scanCard(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		cards = append(cards, card)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
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
