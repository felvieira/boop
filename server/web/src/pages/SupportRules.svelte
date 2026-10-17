<script lang="ts">
  import { api, type SupportClassificationRule, type SupportPriority, type SupportCategory, type SupportRuleFiring, type SupportRuleSimulation, type SupportRuleSuggestion } from '../lib/api'
  import Button from '../lib/ui/Button.svelte'
  import Card from '../lib/ui/Card.svelte'
  import Notice from '../lib/ui/Notice.svelte'

  let { onChanged = () => {} }: { onChanged?: () => void } = $props()

  const categoryOptions: { value: SupportCategory; label: string }[] = [
    { value: 'bug', label: 'Bug' }, { value: 'correction', label: 'Correction' },
    { value: 'suggestion', label: 'Suggestion' }, { value: 'partnership', label: 'Partnership / affiliate' },
    { value: 'customer_request', label: 'Customer request' }, { value: 'internal_bug', label: 'Internal issue' },
    { value: 'other', label: 'Other' },
  ]
  const priorityOptions: { value: SupportPriority; label: string }[] = [
    { value: 'low', label: 'Low' }, { value: 'normal', label: 'Normal' },
    { value: 'high', label: 'High' }, { value: 'urgent', label: 'Urgent' },
  ]

  let rules = $state<SupportClassificationRule[]>([])
  let suggestions = $state<SupportRuleSuggestion[]>([])
  let firings = $state<SupportRuleFiring[]>([])
  let simulations = $state<Record<string, SupportRuleSimulation>>({})
  let loading = $state(true)
  let saving = $state(false)
  let error = $state('')
  let notice = $state('')
  let editingRuleID = $state('')
  let createDraft = $state({ name: '', product: '', field: 'category' as 'category' | 'priority', when_value: 'other', set_value: 'bug' })
  let editDraft = $state({ name: '', product: '', field: 'category' as 'category' | 'priority', when_value: 'other', set_value: 'bug' })
  let activateRuleID = $state('')

  const selectedOptions = $derived(createDraft.field === 'category' ? categoryOptions : priorityOptions)
  const editOptions = $derived(editDraft.field === 'category' ? categoryOptions : priorityOptions)

  async function load() {
    loading = true
    error = ''
    try {
      const [result, firingResult] = await Promise.all([api.supportClassificationRules(), api.supportRuleFirings()])
      rules = result.rules
      suggestions = result.suggestions
      firings = firingResult.firings
    } catch (e: any) {
      error = e.message || 'Could not load classification rules.'
    } finally {
      loading = false
    }
  }

  function changeCreateField(field: 'category' | 'priority') {
    createDraft = { ...createDraft, field, when_value: field === 'category' ? 'other' : 'normal', set_value: field === 'category' ? 'bug' : 'high' }
  }

  function changeEditField(field: 'category' | 'priority') {
    editDraft = { ...editDraft, field, when_value: field === 'category' ? 'other' : 'normal', set_value: field === 'category' ? 'bug' : 'high' }
  }

  async function createRule(source: 'manual' | 'learned' = 'manual', suggestion?: SupportRuleSuggestion) {
    saving = true
    error = ''
    try {
      const sourceDraft = suggestion
        ? { name: `Learned: ${suggestion.product || 'all projects'} ${suggestion.field}`, product: suggestion.product, field: suggestion.field, when_value: suggestion.when_value, set_value: suggestion.set_value }
        : createDraft
      await api.createSupportClassificationRule({ ...sourceDraft, field: sourceDraft.field as 'category' | 'priority', when_value: sourceDraft.when_value as SupportCategory | SupportPriority, set_value: sourceDraft.set_value as SupportCategory | SupportPriority, source })
      if (!suggestion) createDraft = { name: '', product: '', field: 'category', when_value: 'other', set_value: 'bug' }
      notice = 'Rule saved as a draft. Simulate it before activating.'
      await load()
      onChanged()
    } catch (e: any) {
      error = e.message || 'Could not save this rule.'
    } finally {
      saving = false
    }
  }

  function beginEdit(rule: SupportClassificationRule) {
    editingRuleID = rule.id
    editDraft = { name: rule.name, product: rule.product, field: rule.field, when_value: rule.when_value, set_value: rule.set_value }
  }

  async function saveEdit(rule: SupportClassificationRule) {
    saving = true
    error = ''
    try {
      await api.updateSupportClassificationRule(rule.id, {
        ...editDraft,
        field: editDraft.field,
        when_value: editDraft.when_value as SupportCategory | SupportPriority,
        set_value: editDraft.set_value as SupportCategory | SupportPriority,
        enabled: rule.enabled,
        dry_run: rule.dry_run,
        source: rule.source,
      })
      editingRuleID = ''
      delete simulations[rule.id]
      notice = 'Rule updated. Run a new simulation before activating it.'
      await load()
    } catch (e: any) {
      error = e.message || 'Could not update this rule.'
    } finally {
      saving = false
    }
  }

  async function simulate(rule: SupportClassificationRule) {
    error = ''
    try {
      simulations[rule.id] = await api.simulateSupportClassificationRule(rule.id)
    } catch (e: any) {
      error = e.message || 'Could not simulate this rule.'
    }
  }

  async function activate(rule: SupportClassificationRule) {
    if (!simulations[rule.id]) return
    saving = true
    error = ''
    try {
      await api.updateSupportClassificationRule(rule.id, { ...rule, enabled: true, dry_run: false })
      activateRuleID = ''
      notice = 'Rule activated. Matching future notifications will use it.'
      await load()
      onChanged()
    } catch (e: any) {
      error = e.message || 'Could not activate this rule.'
    } finally {
      saving = false
    }
  }

  async function pause(rule: SupportClassificationRule) {
    saving = true
    error = ''
    try {
      await api.updateSupportClassificationRule(rule.id, { ...rule, enabled: false, dry_run: true })
      notice = 'Rule paused.'
      await load()
    } catch (e: any) {
      error = e.message || 'Could not pause this rule.'
    } finally {
      saving = false
    }
  }

  async function remove(rule: SupportClassificationRule) {
    saving = true
    error = ''
    try {
      await api.deleteSupportClassificationRule(rule.id)
      notice = 'Rule removed.'
      await load()
    } catch (e: any) {
      error = e.message || 'Could not remove this rule.'
    } finally {
      saving = false
    }
  }

  function categoryName(value: string) {
    return categoryOptions.find((option) => option.value === value)?.label ?? value
  }

  $effect(() => { load() })
</script>

<section class="rules-page stack" aria-label="Support classification rules">
  <Card>
    <div class="intro">
      <div><h2>Rules from your corrections</h2><p class="muted">Repeated corrections become suggestions. They start disabled and in simulation mode; nothing changes until you review and activate a rule.</p></div>
      <Button variant="secondary" size="sm" onclick={load} disabled={loading}>Refresh</Button>
    </div>
  </Card>

  {#if notice}<p class="success-note" role="status">{notice}</p>{/if}
  {#if error}<Notice tone="bad"><span role="alert">{error}</span></Notice>{/if}

  {#if suggestions.length}
    <section class="stack">
      <div class="section-head"><h2>Suggested rules</h2><span>{suggestions.length}</span></div>
      {#each suggestions as suggestion (`${suggestion.product}:${suggestion.field}:${suggestion.when_value}:${suggestion.set_value}`)}
        <Card>
          <div class="rule-row">
            <div class="rule-copy">
              <strong>{suggestion.product || 'All projects'} · {suggestion.field}</strong>
              <span>{categoryName(suggestion.when_value)} → {categoryName(suggestion.set_value)}</span>
              <small>Based on {suggestion.sample_count} separate cards · last corrected {new Intl.DateTimeFormat('en', { dateStyle: 'medium' }).format(new Date(suggestion.last_seen_at))}</small>
            </div>
            <Button size="sm" onclick={() => createRule('learned', suggestion)} disabled={saving}>Create draft</Button>
          </div>
        </Card>
      {/each}
    </section>
  {/if}

  <Card>
    <form class="rule-form" onsubmit={(event) => { event.preventDefault(); createRule() }}>
      <div><h2>New deterministic rule</h2><p class="muted">Rules match the AI suggestion and can change category or priority for a project, or globally.</p></div>
      <label><span>Name</span><input bind:value={createDraft.name} maxlength="120" required placeholder="Example: raise partner leads" /></label>
      <label><span>Project scope</span><input bind:value={createDraft.product} maxlength="200" placeholder="Blank applies to every project" /></label>
      <label><span>Field</span><select value={createDraft.field} onchange={(event) => changeCreateField(event.currentTarget.value as 'category' | 'priority')}><option value="category">Category</option><option value="priority">Priority</option></select></label>
      <label><span>When the suggestion is</span><select bind:value={createDraft.when_value}>{#each selectedOptions as option (option.value)}<option value={option.value}>{option.label}</option>{/each}</select></label>
      <label><span>Set it to</span><select bind:value={createDraft.set_value}>{#each selectedOptions as option (option.value)}<option value={option.value}>{option.label}</option>{/each}</select></label>
      <Button disabled={saving || !createDraft.name.trim()}>Save as draft</Button>
    </form>
  </Card>

  <section class="stack">
    <div class="section-head"><h2>Saved rules</h2><span>{rules.length}</span></div>
    {#if loading}
      <Card><p class="muted">Loading rules…</p></Card>
    {:else if !rules.length}
      <Card><p class="muted">No classification rules yet. Review cards on the board; repeated corrections will appear as suggestions here.</p></Card>
    {:else}
      {#each rules as rule (rule.id)}
        <Card>
          {#if editingRuleID === rule.id}
            <form class="rule-form edit-form" onsubmit={(event) => { event.preventDefault(); saveEdit(rule) }}>
              <label><span>Name</span><input bind:value={editDraft.name} maxlength="120" required /></label>
              <label><span>Project scope</span><input bind:value={editDraft.product} maxlength="200" placeholder="Blank applies globally" /></label>
              <label><span>Field</span><select value={editDraft.field} onchange={(event) => changeEditField(event.currentTarget.value as 'category' | 'priority')}><option value="category">Category</option><option value="priority">Priority</option></select></label>
              <label><span>When</span><select bind:value={editDraft.when_value}>{#each editOptions as option (option.value)}<option value={option.value}>{option.label}</option>{/each}</select></label>
              <label><span>Set to</span><select bind:value={editDraft.set_value}>{#each editOptions as option (option.value)}<option value={option.value}>{option.label}</option>{/each}</select></label>
              <div class="actions"><Button disabled={saving}>Save changes</Button><Button type="button" variant="secondary" onclick={() => (editingRuleID = '')}>Cancel</Button></div>
            </form>
          {:else}
            <div class="rule-row">
              <div class="rule-copy">
                <strong>{rule.name}</strong>
                <span>{rule.product || 'All projects'} · {rule.field}: {categoryName(rule.when_value)} → {categoryName(rule.set_value)}</span>
                <small>{rule.enabled ? (rule.dry_run ? 'Enabled in simulation only' : 'Active for new classifications') : 'Draft / paused'} · {rule.source === 'learned' ? 'Suggested from corrections' : 'Created manually'}</small>
                {#if simulations[rule.id]}<small>Simulation matched {simulations[rule.id].match_count} historical cards; sample IDs: {simulations[rule.id].sample_cards.join(', ') || 'none'}</small>{/if}
              </div>
              <div class="actions">
                <Button variant="secondary" size="sm" onclick={() => beginEdit(rule)}>Edit</Button>
                <Button variant="secondary" size="sm" onclick={() => simulate(rule)} disabled={saving}>Simulate</Button>
                {#if rule.enabled && !rule.dry_run}
                  <Button variant="secondary" size="sm" onclick={() => pause(rule)} disabled={saving}>Pause</Button>
                {:else if activateRuleID === rule.id}
                  <Button size="sm" onclick={() => activate(rule)} disabled={saving || !simulations[rule.id]}>Approve and activate</Button>
                  <Button variant="secondary" size="sm" onclick={() => (activateRuleID = '')}>Cancel</Button>
                {:else}
                  <Button size="sm" onclick={() => { activateRuleID = rule.id; simulate(rule) }} disabled={saving}>Activate…</Button>
                {/if}
                <Button variant="danger" size="sm" onclick={() => remove(rule)} disabled={saving}>Delete</Button>
              </div>
            </div>
          {/if}
        </Card>
      {/each}
    {/if}
  </section>

  <section class="stack">
    <div class="section-head"><h2>Recent rule activity</h2><span>{firings.length}</span></div>
    {#if !firings.length}
      <Card><p class="muted">No rules have matched notifications yet.</p></Card>
    {:else}
      {#each firings as firing (firing.id)}
        <Card>
          <div class="firing-row">
            <div><strong>{firing.rule_name || 'Removed rule'}</strong><span>{firing.product} · {firing.field}: {firing.before_value} → {firing.after_value}</span><small>{firing.outcome === 'applied' ? 'Applied' : 'Simulation only'} · {new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(firing.created_at))}</small></div>
            <span class="firing-id">{firing.card_id || firing.event_id}</span>
          </div>
          {#if firing.reason}<p class="muted caption firing-reason">{firing.reason}</p>{/if}
        </Card>
      {/each}
    {/if}
  </section>
</section>

<style>
  .rules-page { min-width: 0; }
  h2 { color: var(--up-ink); font: var(--up-type-setting); }
  .intro, .rule-row, .section-head { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
  .intro p, .rule-form p { max-width: 740px; margin-top: 6px; font: var(--up-type-meta); }
  .section-head { justify-content: flex-start; color: var(--up-text-secondary); }
  .section-head span { border-radius: 5px; padding: 2px 7px; background: #eef0f7; font: var(--up-type-caption); }
  .rule-form { display: grid; grid-template-columns: repeat(3, minmax(150px, 1fr)); gap: 12px; align-items: end; }
  .rule-form > div:first-child { grid-column: 1 / -1; }
  .rule-form label { display: flex; min-width: 0; flex-direction: column; gap: 5px; color: var(--up-text-secondary); font: var(--up-type-caption); }
  .rule-form input, .rule-form select { min-height: 36px; width: 100%; color: var(--up-ink); font: var(--up-type-meta); }
  .rule-copy { display: flex; min-width: 0; flex-direction: column; gap: 5px; }
  .rule-copy strong { color: var(--up-ink); font: var(--up-type-ui); }
  .rule-copy span, .rule-copy small { color: var(--up-text-secondary); font: var(--up-type-caption); overflow-wrap: anywhere; }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px; }
  .firing-row { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; }
  .firing-row > div { display: flex; min-width: 0; flex-direction: column; gap: 5px; }
  .firing-row strong, .firing-row span, .firing-row small { color: var(--up-text-secondary); font: var(--up-type-caption); overflow-wrap: anywhere; }
  .firing-id { max-width: 260px; text-align: right; }
  .firing-reason { margin-top: 8px; }
  .edit-form .actions { grid-column: 1 / -1; justify-content: flex-start; }
  @media (max-width: 760px) {
    .rule-form { grid-template-columns: 1fr; }
    .rule-form > div:first-child, .edit-form .actions { grid-column: auto; }
    .rule-row { align-items: flex-start; flex-direction: column; }
    .firing-row { flex-direction: column; }
    .firing-id { text-align: left; }
    .actions { justify-content: flex-start; }
  }
</style>
