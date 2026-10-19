package triage

import (
	"encoding/json"
	"math/big"
	"regexp"
	"strings"
)

const (
	commercePaidSubscription = "paid_subscription"
	commerceTrialStart       = "trial_start"
)

var (
	camelCaseBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	decimalAmount     = regexp.MustCompile(`^[0-9]+(?:[.,][0-9]+)?$`)
	currencyCode      = regexp.MustCompile(`(?i)^[a-z]{3}$`)
	trialStartPhrase  = regexp.MustCompile(`\btrial\s+(?:(?:has(?:\s+been)?|had|is|was|foi|esta)\s+)?(?:started|began|begun|activated|ativado|ativada|initiated|iniciado|iniciada|iniciou|comecou)\b`)
	trialStartMarker  = regexp.MustCompile(`(?:^|[^a-z0-9_])trial[._\s-]+(?:start|started)(?:$|[^a-z0-9_])`)
	trialNounPhrase   = regexp.MustCompile(`\binicio\s+(?:do|de)\s+trial\b`)
	trialNegative     = regexp.MustCompile(`\b(?:failed|failure|error|errored|unsuccessful|scheduled|pending|requested|queued|queue|processing|awaiting|cancelled|canceled|declined|rejected|not|falha|falhou|erro|agendad[oa]s?|pendente(?:s)?|solicitad[oa]s?|cancelad[oa]s?|negad[oa]s?|recusad[oa]s?|aguardando|nao)\b`)
)

type commerceDecision struct {
	Candidate bool
	Eligible  bool
	Kind      string
	Amount    string
	Currency  string
	Reason    string
}

type commerceEvidence struct {
	subscription        bool
	order               bool
	trialStart          bool
	amountField         bool
	confirmed           bool
	unconfirmed         bool
	paid                bool
	unpaid              bool
	amount              string
	currency            string
	invalidAmount       bool
	conflictingAmounts  bool
	invalidCurrency     bool
	conflictingCurrency bool
	ambiguousCollection bool
}

// applyCommercePolicy is the final source-of-truth gate after model output.
// It deliberately uses only the stored event, never the model's verdict, to
// decide whether a subscription order or trial becomes a card.
func applyCommercePolicy(item event, result verdict) (verdict, commerceDecision) {
	decision := inspectCommerceEvent(item)
	if !decision.Candidate {
		return result, decision
	}
	if !decision.Eligible {
		result.Include = false
		result.Category = "other"
		result.Priority = "normal"
		result.Reason = decision.Reason
		return result, decision
	}

	result.ModelCategory = result.Category
	result.ModelPriority = result.Priority
	result.Include = true
	result.Category = "other"
	result.Priority = "normal"
	result.CommerceKind = decision.Kind
	if decision.Kind == commerceTrialStart {
		result.Title = "Trial iniciado · " + commerceProjectName(item)
		result.Summary = "Trial iniciado no projeto " + commerceProjectName(item) + ". Este evento não representa venda paga."
		result.Reason = "Evento de início de trial identificado nos campos de origem; registrado separadamente de vendas pagas."
		return result, decision
	}

	money := decision.Currency + " " + decision.Amount
	result.Title = "Assinatura paga · " + money
	result.Summary = "Pedido de assinatura confirmado e pago. Valor e moeda declarados no evento: " + money + "."
	result.Reason = "Evento contém confirmação explícita, pagamento concluído e valor positivo com moeda declarada."
	return result, decision
}

func inspectCommerceEvent(item event) commerceDecision {
	evidence := commerceEvidence{}
	metadataText := item.Type + " " + item.Title
	metadata := normalizeCommerceText(metadataText)
	evidence.subscription = hasAny(metadata, "subscription", "assinatura")
	evidence.order = hasAny(metadata, "order", "pedido", "purchase", "compra", "checkout")
	evidence.trialStart = isTrialStartSignal(item.Type) || isTrialStartedTitle(item.Title) || isTrialStartSignal(item.Body)

	var payload any
	decoder := json.NewDecoder(strings.NewReader(item.Data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err == nil {
		inspectCommercePayload(payload, nil, &evidence)
	}
	if evidence.trialStart {
		return commerceDecision{
			Candidate: true,
			Eligible:  true,
			Kind:      commerceTrialStart,
		}
	}

	// An amount field makes an otherwise clearly identified subscription event
	// a commercial candidate even if its title omits the word "order". A
	// payment status by itself does not: it could describe a customer support
	// report rather than a new order.
	candidate := evidence.subscription && (evidence.order || evidence.amountField)
	if !candidate {
		return commerceDecision{}
	}

	var missing []string
	if evidence.confirmed && evidence.unconfirmed {
		missing = append(missing, "estados de confirmação conflitantes")
	} else if !evidence.confirmed {
		missing = append(missing, "confirmação explícita do pedido")
	}
	if evidence.paid && evidence.unpaid {
		missing = append(missing, "estados de pagamento conflitantes")
	} else if !evidence.paid {
		missing = append(missing, "pagamento concluído")
	}
	if evidence.ambiguousCollection {
		missing = append(missing, "dados de pedido em coleção ambígua")
	}
	amountValid := evidence.amount != "" && !evidence.invalidAmount && !evidence.conflictingAmounts
	if !amountValid {
		missing = append(missing, "valor positivo válido")
	}
	currencyValid := evidence.currency != "" && !evidence.invalidCurrency && !evidence.conflictingCurrency
	if !currencyValid {
		missing = append(missing, "código de moeda de três letras")
	}
	if len(missing) != 0 {
		return commerceDecision{
			Candidate: true,
			Reason:    "Pedido de assinatura filtrado: faltam dados de origem válidos para " + strings.Join(missing, ", ") + ".",
		}
	}
	return commerceDecision{
		Candidate: true,
		Eligible:  true,
		Kind:      commercePaidSubscription,
		Amount:    evidence.amount,
		Currency:  evidence.currency,
	}
}

func inspectCommercePayload(value any, path []string, evidence *commerceEvidence) {
	switch current := value.(type) {
	case map[string]any:
		for rawKey, child := range current {
			key := normalizeCommerceKey(rawKey)
			childPath := append(path, key)

			if key == "subscription" || strings.HasPrefix(key, "subscription_") {
				evidence.subscription = true
			}
			if key == "order" || key == "orderid" || strings.HasPrefix(key, "order_") || key == "current_order" || key == "pedido" || key == "purchase" || key == "checkout" || key == "invoice" {
				evidence.order = true
			}
			if isEventScope(path) && (key == "trial_started" || key == "trial_start") {
				evidence.trialStart = evidence.trialStart || indicatesTrialStart(child)
			}

			if isEventScope(path) && isEventMarkerKey(key) {
				if marker, ok := scalarText(child); ok {
					normalized := normalizeCommerceText(marker)
					evidence.subscription = evidence.subscription || hasAny(normalized, "subscription", "assinatura")
					evidence.order = evidence.order || hasAny(normalized, "order", "pedido", "purchase", "compra", "checkout")
					evidence.trialStart = evidence.trialStart || isTrialStartSignal(marker)
				}
			}

			switch key {
			case "confirmed", "is_confirmed", "order_confirmed":
				positive, negative := explicitStatus(child, func(value any) bool {
					return isTrue(value) || confirmationStatus(value)
				})
				evidence.confirmed = evidence.confirmed || positive
				evidence.unconfirmed = evidence.unconfirmed || negative
			case "confirmation_status":
				positive, negative := explicitStatus(child, confirmationStatus)
				evidence.confirmed = evidence.confirmed || positive
				evidence.unconfirmed = evidence.unconfirmed || negative
			case "order_status":
				positive, negative := explicitStatus(child, orderConfirmationStatus)
				evidence.confirmed = evidence.confirmed || positive
				evidence.unconfirmed = evidence.unconfirmed || negative
			case "payment_status":
				positive, negative := explicitStatus(child, paymentCompletedStatus)
				evidence.paid = evidence.paid || positive
				evidence.unpaid = evidence.unpaid || negative
			case "paid", "is_paid", "payment_completed", "payment_succeeded", "payment_captured", "captured":
				positive, negative := explicitStatus(child, func(value any) bool {
					return isTrue(value) || paymentCompletedStatus(value)
				})
				evidence.paid = evidence.paid || positive
				evidence.unpaid = evidence.unpaid || negative
			case "amount", "amount_total", "total_amount", "order_amount", "order_total", "paid_amount", "subscription_amount":
				evidence.amountField = true
				addCommerceAmount(child, evidence)
			case "currency", "currency_code":
				addCommerceCurrency(child, evidence)
			case "status":
				parent := ""
				if len(path) != 0 {
					parent = path[len(path)-1]
				}
				if parent == "payment" {
					positive, negative := explicitStatus(child, paymentCompletedStatus)
					evidence.paid = evidence.paid || positive
					evidence.unpaid = evidence.unpaid || negative
				}
				if parent == "order" || parent == "current_order" || parent == "order_details" || parent == "confirmation" {
					positive, negative := explicitStatus(child, orderConfirmationStatus)
					evidence.confirmed = evidence.confirmed || positive
					evidence.unconfirmed = evidence.unconfirmed || negative
				}
			}
			if isCommerceContextKey(key) {
				inspectCommercePayload(child, childPath, evidence)
			}
		}
	case []any:
		if len(current) != 0 && len(path) != 0 && isCommerceContextKey(path[len(path)-1]) {
			// A notification represents one order. Never combine confirmation,
			// payment, amount, or currency evidence across array entries.
			evidence.ambiguousCollection = true
		}
	}
}

func addCommerceAmount(value any, evidence *commerceEvidence) {
	raw, ok := scalarText(value)
	if !ok {
		evidence.invalidAmount = true
		return
	}
	raw = strings.TrimSpace(raw)
	if !decimalAmount.MatchString(raw) {
		evidence.invalidAmount = true
		return
	}
	normalized := strings.Replace(raw, ",", ".", 1)
	amount, ok := new(big.Rat).SetString(normalized)
	if !ok || amount.Sign() <= 0 {
		evidence.invalidAmount = true
		return
	}
	if evidence.amount != "" && evidence.amount != raw {
		evidence.conflictingAmounts = true
		return
	}
	evidence.amount = raw
}

func addCommerceCurrency(value any, evidence *commerceEvidence) {
	raw, ok := scalarText(value)
	if !ok {
		evidence.invalidCurrency = true
		return
	}
	currency := strings.ToUpper(strings.TrimSpace(raw))
	if !currencyCode.MatchString(currency) {
		evidence.invalidCurrency = true
		return
	}
	if evidence.currency != "" && evidence.currency != currency {
		evidence.conflictingCurrency = true
		return
	}
	evidence.currency = currency
}

func confirmationStatus(value any) bool {
	text, ok := scalarText(value)
	if !ok {
		return false
	}
	switch normalizeCommerceText(text) {
	case "confirmed", "approved", "accepted":
		return true
	default:
		return false
	}
}

func orderConfirmationStatus(value any) bool {
	text, ok := scalarText(value)
	if !ok {
		return false
	}
	switch normalizeCommerceText(text) {
	case "confirmed", "approved", "accepted", "complete", "completed":
		return true
	default:
		return false
	}
}

func explicitStatus(value any, isPositive func(any) bool) (positive, negative bool) {
	if isPositive(value) {
		return true, false
	}
	if hasNonEmptyScalar(value) {
		return false, true
	}
	return false, false
}

func paymentCompletedStatus(value any) bool {
	text, ok := scalarText(value)
	if !ok {
		return false
	}
	switch normalizeCommerceText(text) {
	case "paid", "succeeded", "success", "completed", "captured", "payment_succeeded", "payment_completed":
		return true
	default:
		return false
	}
}

func isTrue(value any) bool {
	if boolean, ok := value.(bool); ok {
		return boolean
	}
	text, ok := scalarText(value)
	if !ok {
		return false
	}
	switch normalizeCommerceText(text) {
	case "true", "yes", "1":
		return true
	default:
		return false
	}
}

func hasNonEmptyScalar(value any) bool {
	text, ok := scalarText(value)
	return ok && strings.TrimSpace(text) != "" && strings.TrimSpace(text) != "null"
}

func indicatesTrialStart(value any) bool {
	if boolean, ok := value.(bool); ok {
		return boolean
	}
	text, ok := scalarText(value)
	if !ok {
		return false
	}
	switch normalizeCommerceText(text) {
	case "true", "yes", "1", "start", "started", "began", "activated", "iniciado", "iniciada":
		return true
	default:
		return isTrialStartSignal(text)
	}
}

func scalarText(value any) (string, bool) {
	switch current := value.(type) {
	case string:
		return current, true
	case json.Number:
		return current.String(), true
	case bool:
		if current {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

func isEventMarkerKey(key string) bool {
	switch key {
	case "event", "event_type", "event_name", "notification_type", "action", "type":
		return true
	default:
		return false
	}
}

func isEventScope(path []string) bool {
	for _, key := range path {
		if isCommerceContextKey(key) && key != "data" && key != "payload" && key != "event" {
			return false
		}
	}
	return true
}

func isCommerceContextKey(key string) bool {
	switch key {
	case "data", "payload", "event", "order", "current_order", "order_details", "payment", "confirmation", "invoice", "checkout", "purchase":
		return true
	default:
		return false
	}
}

func isTrialStartSignal(value string) bool {
	normalized := normalizeCommerceText(value)
	words := strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(normalized)
	if trialNegative.MatchString(words) {
		return false
	}
	if trialStartMarker.MatchString(normalized) {
		return true
	}
	return trialStartPhrase.MatchString(words) || trialNounPhrase.MatchString(words)
}

func isTrialStartedTitle(value string) bool {
	return isTrialStartSignal(value)
}

func normalizeCommerceKey(value string) string {
	value = camelCaseBoundary.ReplaceAllString(value, `${1}_${2}`)
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("-", "_", " ", "_").Replace(value)
	return value
}

func normalizeCommerceText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u", "ç", "c",
	).Replace(value)
}

func hasAny(value string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func commerceProjectName(item event) string {
	name := strings.TrimSpace(item.ProjectName)
	if name == "" {
		name = strings.TrimSpace(item.ProjectSlug)
	}
	if name == "" {
		name = "Boop"
	}
	return truncateRunes(name, 160)
}

func supportIdentity(item event) (account, threadID, messageID string) {
	source := strings.TrimSpace(item.Source)
	if source == "" {
		source = "boop"
	}
	kind := strings.TrimSpace(item.Type)
	if kind == "" {
		kind = "notification"
	}
	slug := strings.ToLower(strings.TrimSpace(item.ProjectSlug))
	if slug == "" {
		slug = strings.ToLower(strings.TrimSpace(item.ProjectID))
	}
	return truncateBytes("boop:"+slug, 320), truncateBytes(source+":"+kind, 500), truncateBytes(item.ID, 512)
}
