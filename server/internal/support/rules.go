package support

import (
	"context"
	"database/sql"
	"strings"

	"github.com/chrisgreg/boop/server/internal/ids"
)

type ClassificationRule struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Product   string `json:"product"`
	Field     string `json:"field"`
	WhenValue string `json:"when_value"`
	SetValue  string `json:"set_value"`
	Enabled   bool   `json:"enabled"`
	DryRun    bool   `json:"dry_run"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type RuleInput struct {
	Name      string `json:"name"`
	Product   string `json:"product"`
	Field     string `json:"field"`
	WhenValue string `json:"when_value"`
	SetValue  string `json:"set_value"`
}

type RuleSuggestion struct {
	Product     string `json:"product"`
	Field       string `json:"field"`
	WhenValue   string `json:"when_value"`
	SetValue    string `json:"set_value"`
	SampleCount int    `json:"sample_count"`
	LastSeenAt  string `json:"last_seen_at"`
}

type RuleSimulation struct {
	RuleID      string   `json:"rule_id"`
	MatchCount  int      `json:"match_count"`
	SampleCards []string `json:"sample_cards"`
}

func validRuleValue(field, value string) bool {
	switch field {
	case "category":
		return categories[value]
	case "priority":
		return priorities[value]
	default:
		return false
	}
}

func normalizeRuleInput(in RuleInput) (RuleInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Product = strings.TrimSpace(in.Product)
	in.Field = strings.TrimSpace(in.Field)
	in.WhenValue = strings.ToLower(strings.TrimSpace(in.WhenValue))
	in.SetValue = strings.ToLower(strings.TrimSpace(in.SetValue))
	if len(in.Name) == 0 || len(in.Name) > 120 || len(in.Product) > 200 ||
		!validRuleValue(in.Field, in.WhenValue) || !validRuleValue(in.Field, in.SetValue) || in.WhenValue == in.SetValue {
		return in, ErrInvalid
	}
	return in, nil
}

func scanRule(row rowScanner) (ClassificationRule, error) {
	var rule ClassificationRule
	var enabled, dryRun int
	err := row.Scan(&rule.ID, &rule.Name, &rule.Product, &rule.Field, &rule.WhenValue, &rule.SetValue,
		&enabled, &dryRun, &rule.Source, &rule.CreatedAt, &rule.UpdatedAt)
	rule.Enabled = enabled != 0
	rule.DryRun = dryRun != 0
	return rule, err
}

const ruleSelect = `SELECT id,name,product,field,when_value,set_value,enabled,dry_run,source,created_at,updated_at FROM support_classification_rules`

func (s *Store) ListClassificationRules(ctx context.Context) ([]ClassificationRule, error) {
	rows, err := s.db.QueryContext(ctx, ruleSelect+` ORDER BY enabled DESC,dry_run DESC,lower(product),name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := make([]ClassificationRule, 0)
	for rows.Next() {
		rule, scanErr := scanRule(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Store) ClassificationRuleSuggestions(ctx context.Context) ([]RuleSuggestion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT f.product,f.field,f.predicted_value,f.corrected_value,COUNT(DISTINCT f.card_id),MAX(f.created_at)
		FROM support_classification_feedback f
		WHERE NOT EXISTS (
			SELECT 1 FROM support_classification_rules r
			WHERE lower(r.product)=lower(f.product) AND r.field=f.field
			AND lower(r.when_value)=lower(f.predicted_value) AND lower(r.set_value)=lower(f.corrected_value)
		)
		GROUP BY f.product,f.field,f.predicted_value,f.corrected_value
		HAVING COUNT(DISTINCT f.card_id)>=3
		ORDER BY COUNT(DISTINCT f.card_id) DESC,MAX(f.created_at) DESC LIMIT 30`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	suggestions := make([]RuleSuggestion, 0)
	for rows.Next() {
		var item RuleSuggestion
		if err := rows.Scan(&item.Product, &item.Field, &item.WhenValue, &item.SetValue, &item.SampleCount, &item.LastSeenAt); err != nil {
			return nil, err
		}
		suggestions = append(suggestions, item)
	}
	return suggestions, rows.Err()
}

func (s *Store) CreateClassificationRule(ctx context.Context, input RuleInput, source string) (*ClassificationRule, error) {
	in, err := normalizeRuleInput(input)
	if err != nil {
		return nil, err
	}
	if source != "learned" {
		source = "manual"
	}
	now, id := ids.Now(), ids.New("rul")
	_, err = s.db.ExecContext(ctx, `INSERT INTO support_classification_rules(id,name,product,field,when_value,set_value,enabled,dry_run,source,created_at,updated_at)
		VALUES(?,?,?,?,?,?,0,1,?,?,?)`, id, in.Name, in.Product, in.Field, in.WhenValue, in.SetValue, source, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return nil, ErrInvalid
		}
		return nil, err
	}
	rule, err := scanRule(s.db.QueryRowContext(ctx, ruleSelect+` WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *Store) UpdateClassificationRule(ctx context.Context, id string, input RuleInput, enabled, dryRun bool) (*ClassificationRule, error) {
	in, err := normalizeRuleInput(input)
	if err != nil {
		return nil, err
	}
	now := ids.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE support_classification_rules
		SET name=?,product=?,field=?,when_value=?,set_value=?,enabled=?,dry_run=?,updated_at=? WHERE id=?`,
		in.Name, in.Product, in.Field, in.WhenValue, in.SetValue, enabled, dryRun, now, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return nil, ErrInvalid
		}
		return nil, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrNotFound
	}
	rule, err := scanRule(s.db.QueryRowContext(ctx, ruleSelect+` WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

func (s *Store) DeleteClassificationRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM support_classification_rules WHERE id=?`, id)
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

func (s *Store) SimulateClassificationRule(ctx context.Context, id string) (*RuleSimulation, error) {
	var rule ClassificationRule
	var err error
	rule, err = scanRule(s.db.QueryRowContext(ctx, ruleSelect+` WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	column := "ai_category"
	if rule.Field == "priority" {
		column = "ai_priority"
	}
	result := &RuleSimulation{RuleID: id, SampleCards: make([]string, 0, 10)}
	condition := ` FROM support_cards WHERE source_account LIKE 'boop:%' AND ` + column + `=? AND (?='' OR lower(product)=lower(?))`
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+condition, rule.WhenValue, rule.Product, rule.Product).Scan(&result.MatchCount); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id`+condition+` ORDER BY created_at DESC LIMIT 10`, rule.WhenValue, rule.Product, rule.Product)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cardID string
		if err := rows.Scan(&cardID); err != nil {
			return nil, err
		}
		result.SampleCards = append(result.SampleCards, cardID)
	}
	return result, rows.Err()
}

// ApplyClassificationRules runs only approved rules and records simulations
// without changing the proposed classification. At most one rule applies to
// each field for one event, with product-specific rules taking precedence.
func (s *Store) ApplyClassificationRules(ctx context.Context, eventID, product, category, priority string) (string, string, error) {
	rows, err := s.db.QueryContext(ctx, ruleSelect+` WHERE enabled=1 AND (product='' OR lower(product)=lower(?)) ORDER BY CASE WHEN product='' THEN 1 ELSE 0 END,id`, product)
	if err != nil {
		return category, priority, err
	}
	rules := make([]ClassificationRule, 0)
	for rows.Next() {
		rule, scanErr := scanRule(rows)
		if scanErr != nil {
			rows.Close()
			return category, priority, scanErr
		}
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return category, priority, err
	}
	if err := rows.Close(); err != nil {
		return category, priority, err
	}
	type firing struct {
		rule   ClassificationRule
		before string
		after  string
	}
	firings := make([]firing, 0)
	used := map[string]bool{}
	for _, rule := range rules {
		if used[rule.Field] {
			continue
		}
		current := category
		if rule.Field == "priority" {
			current = priority
		}
		if !strings.EqualFold(current, rule.WhenValue) {
			continue
		}
		used[rule.Field] = true
		firings = append(firings, firing{rule: rule, before: current, after: rule.SetValue})
		if !rule.DryRun {
			if rule.Field == "category" {
				category = rule.SetValue
			} else {
				priority = rule.SetValue
			}
		}
	}
	if len(firings) == 0 {
		return category, priority, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return category, priority, err
	}
	defer tx.Rollback()
	now := ids.Now()
	for _, item := range firings {
		outcome := "applied"
		if item.rule.DryRun {
			outcome = "simulated"
		}
		reason := "Regra " + item.rule.Name
		if _, err = tx.ExecContext(ctx, `INSERT INTO support_rule_firings(id,rule_id,event_id,product,field,outcome,before_value,after_value,reason,created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, ids.New("rfi"), item.rule.ID, eventID, product, item.rule.Field, outcome, item.before, item.after, reason, now); err != nil {
			return category, priority, err
		}
	}
	if err = tx.Commit(); err != nil {
		return category, priority, err
	}
	return category, priority, nil
}

func (s *Store) LinkRuleFirings(ctx context.Context, eventID, cardID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE support_rule_firings SET card_id=? WHERE event_id=? AND card_id IS NULL`, cardID, eventID)
	return err
}
