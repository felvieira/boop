package support

import (
	"context"
	"database/sql"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/chrisgreg/boop/server/internal/ids"
)

var titleStopWords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "from": {}, "this": {}, "that": {}, "when": {},
	"para": {}, "com": {}, "uma": {}, "uns": {}, "por": {}, "que": {}, "das": {}, "dos": {},
	"não": {}, "mais": {}, "como": {}, "sobre": {}, "entre": {}, "app": {}, "site": {}, "sistema": {},
	"bug": {}, "issue": {}, "erro": {}, "error": {}, "problema": {}, "cliente": {}, "solicitação": {},
}

var (
	supportReferencePattern = regexp.MustCompile(`(?i)\b[A-Z][A-Z0-9]{1,12}-\d{1,8}\b|#\d{1,12}|\b(?:issue|pedido|order|ticket|erro|error)\s*[:#]?\s*[A-Z0-9][A-Z0-9_-]{2,31}\b`)
	supportURLPattern       = regexp.MustCompile(`https?://[^\s<>"']+`)
)

type RelatedCandidate struct {
	CardID      string `json:"card_id"`
	Product     string `json:"product"`
	Title       string `json:"title"`
	ReportedAt  string `json:"reported_at"`
	MatchCount  int    `json:"match_count"`
	MatchReason string `json:"match_reason"`
}

type CardLink struct {
	ID            string `json:"id"`
	CardID        string `json:"card_id"`
	RelatedCardID string `json:"related_card_id"`
	Product       string `json:"product"`
	Title         string `json:"title"`
	Relation      string `json:"relation"`
	Direction     string `json:"direction"`
	CreatedAt     string `json:"created_at"`
}

func referenceTokens(value string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, match := range supportReferencePattern.FindAllString(value, -1) {
		key := strings.ToUpper(strings.Join(strings.Fields(match), ""))
		if key != "" {
			tokens["ref:"+key] = struct{}{}
		}
	}
	for _, match := range supportURLPattern.FindAllString(value, -1) {
		match = strings.TrimRight(match, ".,;:!?)]}")
		parsed, err := url.Parse(match)
		if err != nil || parsed.Host == "" || parsed.User != nil {
			continue
		}
		key := strings.ToLower(parsed.Host) + strings.TrimRight(parsed.EscapedPath(), "/")
		if len(key) > 0 && len(key) <= 240 {
			tokens["url:"+key] = struct{}{}
		}
	}
	return tokens
}

func tokenOverlap(left, right map[string]struct{}) int {
	count := 0
	for token := range left {
		if _, found := right[token]; found {
			count++
		}
	}
	return count
}

func titleTokens(value string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, part := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) }) {
		if len([]rune(part)) < 3 {
			continue
		}
		if _, stop := titleStopWords[part]; !stop {
			result[part] = struct{}{}
		}
	}
	return result
}

func titleSimilarity(left, right map[string]struct{}) (int, float64) {
	common := tokenOverlap(left, right)
	union := len(left) + len(right) - common
	if union == 0 {
		return 0, 0
	}
	return common, float64(common) / float64(union)
}

func (s *Store) RelatedCards(ctx context.Context, cardID string) ([]CardLink, []RelatedCandidate, error) {
	var product, title, description string
	if err := s.db.QueryRowContext(ctx, `SELECT product,title,description FROM support_cards WHERE id=?`, cardID).Scan(&product, &title, &description); err == sql.ErrNoRows {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	links, err := s.cardLinks(ctx, cardID)
	if err != nil {
		return nil, nil, err
	}
	excluded := map[string]bool{cardID: true}
	for _, link := range links {
		excluded[link.RelatedCardID] = true
	}
	baseTokens := referenceTokens(title + "\n" + description)
	baseTitleTokens := titleTokens(title)
	if len(baseTokens) == 0 && len(baseTitleTokens) == 0 {
		return links, []RelatedCandidate{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,product,title,description,reported_at FROM support_cards
		WHERE id<>? AND lower(product)=lower(?) AND archived_at='' ORDER BY reported_at DESC,id LIMIT 500`, cardID, product)
	if err != nil {
		return nil, nil, err
	}
	candidates := make([]RelatedCandidate, 0)
	for rows.Next() {
		var id, candidateProduct, candidateTitle, candidateDescription, reportedAt string
		if err := rows.Scan(&id, &candidateProduct, &candidateTitle, &candidateDescription, &reportedAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if excluded[id] {
			continue
		}
		count := tokenOverlap(baseTokens, referenceTokens(candidateTitle+"\n"+candidateDescription))
		reason := "exact_reference"
		if count == 0 {
			count, similarity := titleSimilarity(baseTitleTokens, titleTokens(candidateTitle))
			if count < 2 || similarity < 0.35 {
				continue
			}
			reason = "similar_title"
		}
		candidates = append(candidates, RelatedCandidate{CardID: id, Product: candidateProduct, Title: candidateTitle, ReportedAt: reportedAt, MatchCount: count, MatchReason: reason})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].MatchReason != candidates[j].MatchReason {
			return candidates[i].MatchReason == "exact_reference"
		}
		if candidates[i].MatchCount != candidates[j].MatchCount {
			return candidates[i].MatchCount > candidates[j].MatchCount
		}
		return candidates[i].ReportedAt > candidates[j].ReportedAt
	})
	if len(candidates) > 15 {
		candidates = candidates[:15]
	}
	return links, candidates, nil
}

func (s *Store) cardLinks(ctx context.Context, cardID string) ([]CardLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT l.id,?,other.id,other.product,other.title,l.relation,
		CASE WHEN l.relation='related' THEN 'related' WHEN l.card_id=? THEN 'duplicate_of' ELSE 'duplicated_by' END,l.created_at
		FROM support_card_links l JOIN support_cards other ON other.id=CASE WHEN l.card_id=? THEN l.related_card_id ELSE l.card_id END
		WHERE l.card_id=? OR l.related_card_id=? ORDER BY l.created_at DESC,l.id`, cardID, cardID, cardID, cardID, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := make([]CardLink, 0)
	for rows.Next() {
		var link CardLink
		if err := rows.Scan(&link.ID, &link.CardID, &link.RelatedCardID, &link.Product, &link.Title, &link.Relation, &link.Direction, &link.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

func (s *Store) CreateCardLink(ctx context.Context, cardID, targetID, relation string) error {
	if cardID == "" || targetID == "" || cardID == targetID || (relation != "related" && relation != "duplicate") {
		return ErrInvalid
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM support_cards WHERE id IN (?,?)`, cardID, targetID).Scan(&count); err != nil {
		return err
	}
	if count != 2 {
		return ErrNotFound
	}
	now := ids.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	linkID := ids.New("lnk")
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO support_card_links(id,card_id,related_card_id,relation,created_at) VALUES(?,?,?,?,?)`, linkID, cardID, targetID, relation, now)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 1 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO support_card_link_audit(id,card_id,related_card_id,relation,action,created_at) VALUES(?,?,?,?,'linked',?)`, ids.New("lna"), cardID, targetID, relation, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteCardLink(ctx context.Context, cardID, linkID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var storedCardID, relatedCardID, relation string
	if err := tx.QueryRowContext(ctx, `SELECT card_id,related_card_id,relation FROM support_card_links WHERE id=? AND (card_id=? OR related_card_id=?)`, linkID, cardID, cardID).
		Scan(&storedCardID, &relatedCardID, &relation); err == sql.ErrNoRows {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM support_card_links WHERE id=?`, linkID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO support_card_link_audit(id,card_id,related_card_id,relation,action,created_at) VALUES(?,?,?,?,'unlinked',?)`, ids.New("lna"), storedCardID, relatedCardID, relation, ids.Now()); err != nil {
		return err
	}
	return tx.Commit()
}
