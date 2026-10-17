<script lang="ts">
  import { api, type SupportAction, type SupportActionAuditEntry, type SupportActionInput, type SupportActionKind, type SupportCard } from '../lib/api'
  import Button from '../lib/ui/Button.svelte'

  let { card, onChanged }: { card: SupportCard; onChanged: () => void } = $props()

  let actions = $state<SupportAction[]>([])
  let history = $state<SupportActionAuditEntry[]>([])
  let selectedId = $state('')
  let draft = $state<SupportActionInput | null>(null)
  let githubConfigured = $state(false)
  let githubRepositories = $state<Record<string, string>>({})
  let loading = $state(true)
  let busy = $state(false)
  let confirmExecution = $state(false)
  let error = $state('')
  let notice = $state('')

  const selected = $derived(actions.find((action) => action.id === selectedId) ?? null)
  const editable = $derived(!selected || selected.state === 'draft')

  $effect(() => {
    const id = card.id
    void load(id)
  })

  async function load(cardId: string, preferId = '') {
    loading = true
    error = ''
    try {
      const [result, settings] = await Promise.all([api.supportActions(cardId), api.settings()])
      actions = result.actions
      githubConfigured = settings.github_token_set && !settings.github_token_error
      githubRepositories = settings.github_repositories ?? {}
      if (preferId) selectedId = preferId
      else if (!selectedId || !actions.some((action) => action.id === selectedId)) selectedId = actions[0]?.id ?? ''
      const current = actions.find((action) => action.id === selectedId)
      draft = current ? inputFromAction(current) : null
      confirmExecution = false
      history = current ? (await api.supportActionHistory(current.id)).history : []
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  function inputFromAction(action: SupportAction): SupportActionInput {
    return { kind: action.kind, destination: action.destination, title: action.title, body: action.body }
  }

  function newAction(kind: SupportActionKind) {
    const titleBase = card.title.trim().slice(0, 220)
    const fallback = card.description.trim() || card.title.trim()
    let title = ''
    let body = ''
    if (kind === 'github_issue') {
      title = `[${card.category}] ${titleBase}`.slice(0, 256)
      body = `## Summary\n${fallback}\n\n## Project\n${card.product}\n\n## Priority\n${card.priority}\n\n## Review checklist\n- [ ] Reproduce the report\n- [ ] Identify the cause\n- [ ] Implement and verify a fix\n\nReview this draft for private data before creating the issue.`
    } else if (kind === 'customer_reply') {
      title = `Reply: ${titleBase}`.slice(0, 256)
      body = `Olá! Obrigado por entrar em contato sobre “${card.title}”.\n\nRecebemos seu relato e vamos analisá-lo. Se precisarmos de mais informações, entraremos em contato.\n\nEquipe Boop`
      draft = { kind, destination: extractEmail(card.sender), title, body }
      selectedId = ''
      history = []
      confirmExecution = false
      error = ''
      notice = 'Draft only. Nothing has been sent or created yet.'
      return
    } else {
      title = `Plano técnico: ${titleBase}`.slice(0, 256)
      body = `Reproduzir o problema com base no relato e registrar as condições.\nIdentificar a causa no projeto ${card.product}.\nImplementar a correção e revisar os efeitos colaterais.\nValidar o comportamento corrigido e atualizar este cartão com o resultado.`
    }
    selectedId = ''
    const mappedRepository = kind === 'github_issue'
      ? Object.entries(githubRepositories).find(([product]) => product.trim().toLowerCase() === card.product.trim().toLowerCase())?.[1] ?? ''
      : ''
    draft = { kind, destination: mappedRepository, title, body }
    history = []
    confirmExecution = false
    error = ''
    notice = 'Draft only. Nothing has been sent or created yet.'
  }

  async function selectAction(action: SupportAction) {
    selectedId = action.id
    draft = inputFromAction(action)
    confirmExecution = false
    error = ''
    notice = ''
    try {
      history = (await api.supportActionHistory(action.id)).history
    } catch (e: any) {
      error = e.message
    }
  }

  async function saveDraft() {
    if (!draft || busy) return
    busy = true
    error = ''
    notice = ''
    try {
      const saved = selectedId
        ? await api.updateSupportAction(selectedId, draft)
        : await api.createSupportAction(card.id, draft)
      await load(card.id, saved.id)
      notice = 'Draft saved for review.'
    } catch (e: any) {
      error = e.message
    } finally {
      busy = false
    }
  }

  async function approveReply() {
    if (!selected || busy) return
    busy = true
    error = ''
    notice = ''
    try {
      await api.approveSupportReply(selected.id)
      await load(card.id, selected.id)
      await copyReply()
    } catch (e: any) {
      error = e.message
    } finally {
      busy = false
    }
  }

  async function copyReply() {
    if (!selected || selected.kind !== 'customer_reply' || selected.state !== 'approved') return
    try {
      await navigator.clipboard.writeText(selected.body)
      notice = 'Approved reply copied. Boop did not send it.'
    } catch {
      notice = 'Reply approved. Clipboard access failed; select and copy the text below.'
    }
  }

  async function execute() {
    if (!selected || !confirmExecution || busy) return
    busy = true
    error = ''
    notice = ''
    try {
      const completed = await api.executeSupportAction(selected.id)
      await load(card.id, completed.id)
      if (completed.kind === 'github_issue') notice = 'GitHub issue created and recorded.'
      else {
        notice = 'Approved plan added to the card checklist.'
        onChanged()
      }
      confirmExecution = false
    } catch (e: any) {
      error = e.message
      await load(card.id, selected.id)
    } finally {
      busy = false
    }
  }

  async function reconcile() {
    if (!selected || busy) return
    busy = true
    error = ''
    notice = ''
    try {
      const result = await api.reconcileSupportAction(selected.id)
      await load(card.id, selected.id)
      notice = result.found ? 'Existing GitHub issue found and linked.' : 'No matching issue appeared in the latest GitHub results. Check the repository before authorizing a retry.'
    } catch (e: any) {
      error = e.message
    } finally {
      busy = false
    }
  }

  async function authorizeRetry() {
    if (!selected || !confirmExecution || busy) return
    busy = true
    error = ''
    notice = ''
    try {
      await api.authorizeSupportActionRetry(selected.id)
      await load(card.id, selected.id)
      notice = 'Retry authorized. The next issue attempt checks GitHub for the idempotency marker first.'
      confirmExecution = false
    } catch (e: any) {
      error = e.message
    } finally {
      busy = false
    }
  }

  async function cancelDraft() {
    if (!selected || busy) return
    busy = true
    error = ''
    try {
      await api.cancelSupportAction(selected.id)
      await load(card.id)
      notice = 'Draft cancelled and retained in the action history.'
    } catch (e: any) {
      error = e.message
    } finally {
      busy = false
    }
  }

  function formatDate(value: string) {
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('pt-BR', { dateStyle: 'short', timeStyle: 'short' }).format(date)
  }

  function kindLabel(kind: SupportActionKind) {
    return kind === 'github_issue' ? 'GitHub issue' : kind === 'customer_reply' ? 'Customer reply' : 'Technical plan'
  }

  function extractEmail(value: string) {
    return value.match(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/i)?.[0] ?? ''
  }

  function mailtoHref(action: SupportAction) {
    return `mailto:${action.destination}?subject=${encodeURIComponent(action.title)}&body=${encodeURIComponent(action.body)}`
  }
</script>

<section class="actions-section" aria-labelledby="support-actions-heading">
  <div class="section-head">
    <div><h3 id="support-actions-heading">Prepared actions</h3><p>Review the destination and content before approving an action.</p></div>
    <div class="new-actions">
      <Button variant="secondary" size="sm" onclick={() => newAction('github_issue')}>＋ Issue</Button>
      <Button variant="secondary" size="sm" onclick={() => newAction('customer_reply')}>＋ Reply</Button>
      <Button variant="secondary" size="sm" onclick={() => newAction('technical_plan')}>＋ Plan</Button>
    </div>
  </div>

  {#if !githubConfigured}<p class="hint">Configure a GitHub token in Settings to create issues. Reply and plan drafts work without it.</p>{/if}
  {#if error}<p class="message error" role="alert">{error}</p>{/if}
  {#if notice}<p class="message" aria-live="polite">{notice}</p>{/if}
  {#if loading}<p class="muted">Loading prepared actions…</p>{/if}

  {#if actions.length}
    <div class="action-list" aria-label="Saved actions">
      {#each actions as action (action.id)}
        <button type="button" class="action-chip" class:selected={selectedId === action.id} onclick={() => selectAction(action)}>
          <span>{kindLabel(action.kind)}</span><strong>{action.title}</strong><small>{action.state.replaceAll('_', ' ')}</small>
        </button>
      {/each}
    </div>
  {/if}

  {#if draft}
    <div class="draft-editor">
      <div class="draft-title"><span class="kind">{selected ? kindLabel(selected.kind) : kindLabel(draft.kind)}</span>{#if selected}<span class={`state ${selected.state}`}>{selected.state.replaceAll('_', ' ')}</span>{/if}</div>
      {#if draft.kind === 'github_issue'}
        <label><span>GitHub repository</span><input bind:value={draft.destination} disabled={!editable} placeholder="owner/repository" autocomplete="off" spellcheck="false" /></label>
        <p class="hint">{Object.keys(githubRepositories).length ? 'Destination is prefilled from the product mapping in Settings; review it before approval.' : 'Add a product-to-repository mapping in Settings or enter the destination here.'}</p>
      {:else if draft.kind === 'customer_reply'}
        <label><span>Recipient email · optional</span><input type="email" bind:value={draft.destination} disabled={!editable} maxlength="320" placeholder="person@example.com" autocomplete="email" /></label>
      {/if}
      <label><span>Title</span><input bind:value={draft.title} disabled={!editable} maxlength="256" /></label>
      <label><span>Content</span><textarea bind:value={draft.body} disabled={!editable} rows="7" maxlength="60000"></textarea></label>

      {#if selected?.kind === 'github_issue' && selected.state === 'draft'}
        <label class="approval-check"><input type="checkbox" bind:checked={confirmExecution} /><span>I reviewed <strong>{selected.destination || 'the repository'}</strong>, the title, and the full issue body. I approve creating this issue.</span></label>
        {#if !githubConfigured}<p class="hint">Save a GitHub token in Settings before creating this issue.</p>{/if}
      {:else if selected?.kind === 'technical_plan' && selected.state === 'draft'}
        <label class="approval-check"><input type="checkbox" bind:checked={confirmExecution} /><span>I reviewed this plan and approve adding its steps to the card checklist.</span></label>
      {:else if selected?.state === 'needs_reconciliation'}
        <p class="hint">The request may have reached GitHub. Reconcile before retrying to avoid duplicate issues.</p>
        <label class="approval-check"><input type="checkbox" bind:checked={confirmExecution} /><span>I checked the configured repository and confirmed this issue does not exist. Allow a safe retry.</span></label>
      {:else if selected?.kind === 'github_issue' && selected.state === 'failed'}
        <label class="approval-check"><input type="checkbox" bind:checked={confirmExecution} /><span>I reviewed the failed attempt and approve retrying this GitHub issue.</span></label>
      {:else if selected?.state === 'approved' && selected.kind === 'customer_reply'}
        <p class="hint">This reply is approved for manual use. Boop never sends it.</p>
        <Button variant="secondary" size="sm" onclick={copyReply}>Copy approved reply</Button>
        {#if selected.destination}<a class="external-link" href={mailtoHref(selected)}>Open email draft for {selected.destination} ↗</a>{/if}
      {/if}

      {#if selected?.state === 'failed'}<p class="message error">{selected.error || 'The action failed. Review the details before retrying.'}</p>{/if}
      {#if selected?.state === 'executing'}<p class="hint">Execution status is still being reconciled. Check GitHub before authorizing another attempt.</p>{/if}
      {#if selected?.external_url}<a class="external-link" href={selected.external_url} target="_blank" rel="noreferrer">Open issue #{selected.external_id} on GitHub ↗</a>{/if}

      <div class="action-controls">
        {#if editable}<Button variant="secondary" size="sm" onclick={saveDraft} disabled={busy || !draft.title.trim() || !draft.body.trim() || (draft.kind === 'github_issue' && !draft.destination.trim())}>{busy ? 'Saving…' : 'Save draft'}</Button>{/if}
        {#if selected?.kind === 'github_issue' && (selected.state === 'draft' || selected.state === 'failed')}
          <Button size="sm" onclick={execute} disabled={busy || !confirmExecution || !githubConfigured}>{busy ? 'Working…' : selected.state === 'failed' ? 'Approve & retry issue' : 'Approve & create issue'}</Button>
        {:else if selected?.kind === 'technical_plan' && selected.state === 'draft'}
          <Button size="sm" onclick={execute} disabled={busy || !confirmExecution}>{busy ? 'Working…' : 'Approve & add checklist'}</Button>
        {:else if selected?.kind === 'customer_reply' && selected.state === 'draft'}
          <Button size="sm" onclick={approveReply} disabled={busy}>{busy ? 'Approving…' : 'Approve & copy reply'}</Button>
        {/if}
        {#if selected?.kind === 'github_issue' && (selected.state === 'executing' || selected.state === 'needs_reconciliation')}
          <Button variant="secondary" size="sm" onclick={reconcile} disabled={busy}>{busy ? 'Checking…' : 'Check GitHub result'}</Button>
        {/if}
        {#if selected?.state === 'needs_reconciliation'}
          <Button variant="danger" size="sm" onclick={authorizeRetry} disabled={busy || !confirmExecution}>Authorize retry</Button>
        {/if}
        {#if selected?.state === 'draft'}<Button variant="danger" size="sm" onclick={cancelDraft} disabled={busy}>Cancel draft</Button>{/if}
      </div>
    </div>
  {/if}

  {#if history.length}
    <details class="action-history">
      <summary>Action history · {history.length}</summary>
      <ul>
        {#each history as entry (entry.id)}
          <li><strong>{entry.action.replaceAll('_', ' ')}</strong><span>{entry.summary}</span><small>{entry.actor} · {formatDate(entry.created_at)}</small>{#if entry.external_id}<small>External id: {entry.external_id}</small>{/if}</li>
        {/each}
      </ul>
    </details>
  {/if}
</section>

<style>
  .actions-section { display: grid; gap: 12px; border-top: 1px solid var(--up-border, #e5e7eb); padding-top: 16px; }
  .section-head, .new-actions, .action-controls, .draft-title { display: flex; align-items: center; gap: 8px; }
  .section-head { justify-content: space-between; align-items: flex-start; flex-wrap: wrap; }
  h3 { margin: 0; font: var(--up-type-ui); color: var(--up-ink); }
  .section-head p, .hint, .muted { margin: 4px 0 0; color: var(--up-text-muted, #6b7280); font: var(--up-type-caption); }
  .action-list { display: flex; gap: 8px; overflow-x: auto; padding-bottom: 3px; }
  .action-chip { display: grid; gap: 3px; min-width: 150px; max-width: 240px; text-align: left; padding: 9px 10px; border: 1px solid var(--up-border, #e5e7eb); border-radius: 10px; color: var(--up-ink); background: var(--up-surface, #fff); cursor: pointer; }
  .action-chip.selected { border-color: var(--up-accent, #635bff); box-shadow: 0 0 0 1px var(--up-accent, #635bff); }
  .action-chip span, .action-chip small, .kind, .state { color: var(--up-text-muted, #6b7280); font: var(--up-type-caption); }
  .action-chip strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: var(--up-type-ui); }
  .draft-editor { display: grid; gap: 10px; padding: 12px; border: 1px solid var(--up-border, #e5e7eb); border-radius: 12px; background: var(--up-surface-subtle, #f9fafb); }
  .draft-editor label:not(.approval-check) { display: grid; gap: 5px; color: var(--up-text-muted, #6b7280); font: var(--up-type-caption); }
  .draft-editor input:not([type=checkbox]), .draft-editor textarea { width: 100%; padding: 9px 10px; border: 1px solid var(--up-border, #d1d5db); border-radius: 8px; background: var(--up-surface, #fff); color: var(--up-ink); font: var(--up-type-body); }
  .draft-editor textarea { resize: vertical; min-height: 120px; white-space: pre-wrap; }
  .draft-editor input:disabled, .draft-editor textarea:disabled { opacity: .78; }
  .approval-check { display: flex; align-items: flex-start; gap: 8px; color: var(--up-ink); font: var(--up-type-caption); }
  .approval-check input { margin-top: 2px; }
  .draft-title { justify-content: space-between; }
  .state { padding: 3px 7px; border-radius: 999px; background: #eef2ff; color: #4338ca; text-transform: capitalize; }
  .state.failed, .state.needs_reconciliation { color: #9a3412; background: #ffedd5; }
  .state.completed { color: #166534; background: #dcfce7; }
  .message { margin: 0; color: var(--up-text-muted, #6b7280); font: var(--up-type-caption); }
  .message.error { color: #b91c1c; }
  .external-link { color: var(--up-accent, #4f46e5); font: var(--up-type-caption); }
  .action-controls { flex-wrap: wrap; }
  .action-history summary { cursor: pointer; color: var(--up-text-muted, #6b7280); font: var(--up-type-caption); }
  .action-history ul { display: grid; gap: 8px; padding: 0; margin: 10px 0 0; list-style: none; }
  .action-history li { display: grid; gap: 3px; padding: 8px 10px; border-left: 2px solid var(--up-border, #d1d5db); }
  .action-history li strong { color: var(--up-ink); font: var(--up-type-ui); text-transform: capitalize; }
  .action-history li span, .action-history li small { color: var(--up-text-muted, #6b7280); font: var(--up-type-caption); }
  @media (max-width: 600px) { .section-head { display: grid; } .new-actions { flex-wrap: wrap; } .draft-editor { padding: 9px; } }
</style>
