package support

import (
	"context"
	"time"
)

type DigestSummary struct {
	NewCards   int  `json:"new_cards"`
	Updated    int  `json:"updated_cards"`
	NewLeads   int  `json:"new_partnership_leads"`
	Urgent     int  `json:"urgent_active"`
	Blocked    int  `json:"blocked_active"`
	Overdue    int  `json:"overdue_active"`
	HasChanges bool `json:"has_changes"`
}

func (s *Store) DigestSince(ctx context.Context, since time.Time, now time.Time) (DigestSummary, error) {
	var summary DigestSummary
	sinceText := since.UTC().Format(time.RFC3339Nano)
	nowText := now.UTC().Format(time.RFC3339Nano)
	err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN created_at>=? AND archived_at='' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN created_at<? AND updated_at>=? AND archived_at='' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN created_at>=? AND archived_at='' AND category='partnership' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN (created_at>=? OR updated_at>=?) AND archived_at='' AND priority='urgent' AND status<>'done' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN (created_at>=? OR updated_at>=?) AND archived_at='' AND status='blocked' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN (created_at>=? OR updated_at>=?) AND archived_at='' AND status<>'done' AND due_at<>'' AND due_at<? THEN 1 ELSE 0 END),0)
		FROM support_cards`, sinceText, sinceText, sinceText, sinceText, sinceText, sinceText, sinceText, sinceText, sinceText, sinceText, nowText).
		Scan(&summary.NewCards, &summary.Updated, &summary.NewLeads, &summary.Urgent, &summary.Blocked, &summary.Overdue)
	summary.HasChanges = summary.NewCards+summary.Updated > 0
	return summary, err
}
