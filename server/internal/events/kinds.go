package events

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chrisgreg/boop/server/internal/ids"
)

// Business kinds an event is sorted into, so an owner running several apps
// can count sales, churn and failures without reading every notification.
// The kind is derived at query time from the stored event, so a rule change
// reclassifies history without a migration.
const (
	KindSale          = "sale"
	KindRenewal       = "renewal"
	KindTrial         = "trial"
	KindSignup        = "signup"
	KindLead          = "lead"
	KindPaymentFailed = "payment_failed"
	KindCancellation  = "cancellation"
	KindRefund        = "refund"
	KindError         = "error"
	KindOther         = "other"
)

// Kinds lists every kind in display order: money in, money at risk, health.
var Kinds = []string{KindSale, KindRenewal, KindTrial, KindSignup, KindLead, KindPaymentFailed, KindCancellation, KindRefund, KindError, KindOther}

// ValidKind reports whether k is a known kind.
func ValidKind(k string) bool { return slices.Contains(Kinds, k) }

// kindRule matches when every term of any one alternative appears in the
// lower-cased type, title and source (plus body and payload when wide).
// Rules are evaluated in order; the first match wins, so the specific
// (failure, refund, cancellation, renewal) precede the generic (sale).
type kindRule struct {
	kind string
	any  [][]string
	wide bool
	// business rules skip error/critical events so an exception that
	// mentions "subscription" is not counted as a sale or a cancellation.
	business bool
}

var kindRules = []kindRule{
	{kind: KindPaymentFailed, any: [][]string{
		{"falha", "pagamento"}, {"falha", "cobran"}, {"pagamento", "recusad"}, {"pagamento", "falhou"},
		{"payment", "fail"}, {"payment", "declin"}, {"charge", "fail"}, {"past_due"}, {"inadimpl"},
	}},
	{kind: KindRefund, business: true, any: [][]string{
		{"reembols"}, {"refund"}, {"estorn"}, {"chargeback"},
	}},
	{kind: KindCancellation, business: true, any: [][]string{
		{"cancel", "assinatura"}, {"cancel", "subscription"}, {"cancel", "plan"}, {"cancelamento"}, {"churn"},
		{"subscription", "deleted"}, {"subscription", "expired"}, {"assinatura", "expirad"}, {"assinatura", "encerrad"},
		{"não será renovada"}, {"nao sera renovada"}, {"unsubscribe"},
	}},
	{kind: KindRenewal, business: true, any: [][]string{{"renova"}, {"renew"}}},
	// A trial that converted is a paid sale, not a trial.
	{kind: KindSale, business: true, any: [][]string{{"trial", "convert"}, {"teste", "convert"}}},
	{kind: KindTrial, business: true, any: [][]string{
		{"trial"}, {"período de teste"}, {"periodo de teste"}, {"teste grátis"}, {"teste gratis"},
	}},
	// Store webhooks often report the trial only as a status in the body or data.
	{kind: KindTrial, business: true, wide: true, any: [][]string{{"trialing"}}},
	{kind: KindLead, business: true, any: [][]string{{"parceri"}, {"afiliad"}, {"affiliat"}, {"partner"}, {"novo lead"}, {"new lead"}}},
	{kind: KindSale, business: true, any: [][]string{
		{"nova assinatura"}, {"novo pedido"}, {"new subscription"}, {"assinatura"}, {"subscription"},
		{"pagamento"}, {"payment"}, {"pedido"}, {"order"}, {"purchase"}, {"compra"}, {"venda"}, {"sale"},
		{"checkout"}, {"invoice", "paid"}, {"fatura", "paga"}, {"pix"},
	}},
	{kind: KindSignup, business: true, any: [][]string{
		{"signup"}, {"sign up"}, {"sign_up"}, {"sign-up"}, {"cadastro"}, {"novo usuário"}, {"novo usuario"},
		{"new user"}, {"user.created"}, {"user_created"}, {"registration"},
	}},
}

// kindExpr returns a SQL expression yielding the kind of the events row
// aliased as alias. Terms are package constants, inlined as literals so the
// expression needs no bind arguments wherever it is embedded.
func kindExpr(alias string) string {
	a := alias + "."
	narrow := "lower(" + a + "type || ' ' || " + a + "title || ' ' || " + a + "source)"
	wide := "lower(" + a + "type || ' ' || " + a + "title || ' ' || " + a + "source || ' ' || " + a + "body || ' ' || " + a + "payload_json)"
	business := a + "level NOT IN ('error', 'critical')"
	var b strings.Builder
	b.WriteString("(CASE")
	for _, r := range kindRules {
		text := narrow
		if r.wide {
			text = wide
		}
		alts := make([]string, 0, len(r.any))
		for _, terms := range r.any {
			conds := make([]string, 0, len(terms))
			for _, t := range terms {
				conds = append(conds, "instr("+text+", '"+strings.ReplaceAll(t, "'", "''")+"') > 0")
			}
			alts = append(alts, "("+strings.Join(conds, " AND ")+")")
		}
		cond := "(" + strings.Join(alts, " OR ") + ")"
		if r.business {
			cond = business + " AND " + cond
		}
		fmt.Fprintf(&b, " WHEN %s THEN '%s'", cond, r.kind)
	}
	fmt.Fprintf(&b, " WHEN %slevel IN ('error', 'critical') THEN '%s' ELSE '%s' END)", a, KindError, KindOther)
	return b.String()
}

// KindCounts maps every kind to its event count; absent kinds are zero.
type KindCounts map[string]int

func newKindCounts() KindCounts {
	c := make(KindCounts, len(Kinds))
	for _, k := range Kinds {
		c[k] = 0
	}
	return c
}

// Revenue sums the declared `data.amount` of events, in hundredths, by
// currency (`data.currency`, three letters) and kind:
// {"BRL": {"sale": 14990, "refund": 2990}}. Events without a positive amount
// and a valid currency are counted but add nothing.
type Revenue map[string]map[string]int64

func (r Revenue) add(currency, kind string, cents int64) {
	if currency == "" || cents <= 0 {
		return
	}
	if r[currency] == nil {
		r[currency] = map[string]int64{}
	}
	r[currency][kind] += cents
}

// ProjectStats is one project's share of a Stats window.
type ProjectStats struct {
	ProjectID   string     `json:"project_id"`
	ProjectName string     `json:"project_name"`
	ProjectSlug string     `json:"project_slug"`
	ProjectIcon string     `json:"project_icon"`
	Total       int        `json:"total"`
	Kinds       KindCounts `json:"kinds"`
	Revenue     Revenue    `json:"revenue_cents"`
}

// Window holds the counts of one period.
type Window struct {
	Since   string     `json:"since,omitempty"`
	Until   string     `json:"until,omitempty"`
	Total   int        `json:"total"`
	Kinds   KindCounts `json:"kinds"`
	Revenue Revenue    `json:"revenue_cents"`
}

// Money columns: the declared amount in hundredths and the currency code,
// both NULL/0 when the sender did not declare them in the event data.
const (
	currencyExpr = `CASE WHEN length(json_extract(e.payload_json, '$.currency')) = 3 THEN upper(json_extract(e.payload_json, '$.currency')) ELSE '' END`
	centsExpr    = `CAST(round(CAST(json_extract(e.payload_json, '$.amount') AS REAL) * 100) AS INTEGER)`
)

// Stats counts events by kind and project. Previous covers the equally long
// window right before Since, so clients can show a trend; it is nil when the
// window is open-ended.
type Stats struct {
	Window
	Previous *Window        `json:"previous,omitempty"`
	Projects []ProjectStats `json:"projects"`
}

// Stats counts the events matching f (Kind, cursor, limit and grouping are
// ignored: every occurrence counts) by kind and by project.
func (s *Store) Stats(ctx context.Context, f Filter, now time.Time) (Stats, error) {
	var err error
	if f.Since, err = normaliseTime(f.Since); err != nil {
		return Stats{}, err
	}
	if f.Until, err = normaliseTime(f.Until); err != nil {
		return Stats{}, err
	}
	f.Kind, f.Before, f.Grouped = "", "", false

	cur, projects, err := s.countKinds(ctx, f, true)
	if err != nil {
		return Stats{}, err
	}
	out := Stats{Window: cur, Projects: projects}
	if f.Since == "" {
		return out, nil
	}
	since, _ := ids.Parse(f.Since)
	until := now.UTC()
	if f.Until != "" {
		until, _ = ids.Parse(f.Until)
	}
	if !until.After(since) {
		return out, nil
	}
	pf := f
	pf.Since, pf.Until = ids.Format(since.Add(-until.Sub(since))), f.Since
	prev, _, err := s.countKinds(ctx, pf, false)
	if err != nil {
		return Stats{}, err
	}
	out.Previous = &prev
	return out, nil
}

func (s *Store) countKinds(ctx context.Context, f Filter, perProject bool) (Window, []ProjectStats, error) {
	where, args := filterClause(f, "e")
	where = append([]string{"1=1"}, where...)
	rows, err := s.db.QueryContext(ctx, `SELECT e.project_id, p.name, p.slug, p.icon, `+kindExpr("e")+` AS kind, `+currencyExpr+` AS cur, COUNT(*), `+
		`COALESCE(SUM(CASE WHEN `+centsExpr+` > 0 THEN `+centsExpr+` ELSE 0 END), 0)`+
		fromClause+` WHERE `+strings.Join(where, " AND ")+` GROUP BY e.project_id, kind, cur`, args...)
	if err != nil {
		return Window{}, nil, err
	}
	defer rows.Close()
	w := Window{Since: f.Since, Until: f.Until, Kinds: newKindCounts(), Revenue: Revenue{}}
	byProject := map[string]*ProjectStats{}
	var order []string
	for rows.Next() {
		var ps ProjectStats
		var kind, currency string
		var n int
		var cents int64
		if err := rows.Scan(&ps.ProjectID, &ps.ProjectName, &ps.ProjectSlug, &ps.ProjectIcon, &kind, &currency, &n, &cents); err != nil {
			return Window{}, nil, err
		}
		w.Total += n
		w.Kinds[kind] += n
		w.Revenue.add(currency, kind, cents)
		if !perProject {
			continue
		}
		p, ok := byProject[ps.ProjectID]
		if !ok {
			ps.Kinds = newKindCounts()
			ps.Revenue = Revenue{}
			p = &ps
			byProject[ps.ProjectID] = p
			order = append(order, ps.ProjectID)
		}
		p.Total += n
		p.Kinds[kind] += n
		p.Revenue.add(currency, kind, cents)
	}
	if err := rows.Err(); err != nil {
		return Window{}, nil, err
	}
	projects := make([]ProjectStats, 0, len(order))
	for _, id := range order {
		projects = append(projects, *byProject[id])
	}
	slices.SortStableFunc(projects, func(a, b ProjectStats) int {
		if a.Total != b.Total {
			return b.Total - a.Total
		}
		return strings.Compare(a.ProjectName, b.ProjectName)
	})
	return w, projects, nil
}
