<script lang="ts">
  import { tick } from 'svelte'
  import { api, type SupportCard, type SupportCardAuditEntry, type SupportCardDraft, type SupportCardLink, type SupportCategory, type SupportChecklistItem, type SupportLabel, type SupportList, type SupportPriority, type SupportRelatedCandidate, type SupportReviewState, type SupportTriageStatus } from '../lib/api'
  import Button from '../lib/ui/Button.svelte'
  import Card from '../lib/ui/Card.svelte'
  import Notice from '../lib/ui/Notice.svelte'
  import { link } from '../lib/router.svelte'
  import SupportActivity from './SupportActivity.svelte'
  import SupportInsights from './SupportInsights.svelte'
  import SupportRules from './SupportRules.svelte'
  import SupportActions from './SupportActions.svelte'

  const labelPalette: SupportLabel['color'][] = ['purple', 'blue', 'green', 'yellow', 'orange', 'red', 'gray']
  const categories: { value: SupportCategory; label: string }[] = [
    { value: 'bug', label: 'Bug' }, { value: 'correction', label: 'Correction' },
    { value: 'suggestion', label: 'Suggestion' }, { value: 'partnership', label: 'Partnership / affiliate' },
    { value: 'customer_request', label: 'Customer request' }, { value: 'internal_bug', label: 'Internal issue' },
    { value: 'other', label: 'Other' },
  ]
  let lists = $state<SupportList[]>([])
  let cards = $state<SupportCard[]>([])
  let triageStatus = $state<SupportTriageStatus | null>(null)
  let loading = $state(true)
  let error = $state('')
  let notice = $state('')
  let busyCard = $state('')
  let search = $state('')
  let productFilter = $state('')
  let categoryFilter = $state<SupportCategory | ''>('')
  let priorityFilter = $state<SupportPriority | ''>('')
  let reviewFilter = $state<SupportReviewState | ''>('')
  let fromDate = $state('')
  let toDate = $state('')
  let showArchived = $state(false)
  let activityView = $state(false)
  let rulesView = $state(false)
  let insightsView = $state(false)
  let dragCardId = $state('')
  let dragListId = $state('')
  let boardDialog: HTMLDialogElement
  let detailCard = $state<SupportCard | null>(null)
  let cardHistory = $state<SupportCardAuditEntry[]>([])
  let cardLinks = $state<SupportCardLink[]>([])
  let relatedCandidates = $state<SupportRelatedCandidate[]>([])
  let relationBusy = $state('')
  let detailLoading = $state(false)
  let creating = $state(false)
  let saving = $state(false)
  let draft = $state<SupportCardDraft>({ title: '', product: 'Boop', description: '', priority: 'normal', category: 'other', due_at: '', list_id: '', labels: [] })
  let dueLocal = $state('')
  let labelInput = $state('')
  let labelColor = $state<SupportLabel['color']>('blue')
  let commentDraft = $state('')
  let checklistDraft = $state('')
  let listNameDraft = $state('')
  let editingListId = $state('')
  let newListName = $state('')

  const visibleCards = $derived.by(() => {
    const query = search.trim().toLocaleLowerCase()
    return cards.filter((card) => {
      if (Boolean(card.archived_at) !== showArchived) return false
      if (productFilter && card.product !== productFilter) return false
      if (categoryFilter && card.category !== categoryFilter) return false
      if (priorityFilter && card.priority !== priorityFilter) return false
      if (reviewFilter && card.review_state !== reviewFilter) return false
      if (fromDate && card.reported_at.slice(0, 10) < fromDate) return false
      if (toDate && card.reported_at.slice(0, 10) > toDate) return false
      if (!query) return true
      return [card.title, card.product, card.sender, card.description, card.original_text, ...card.labels.map((label) => label.name)]
        .some((value) => value.toLocaleLowerCase().includes(query))
    })
  })
  const products = $derived([...new Set(cards.filter((card) => !card.archived_at).map((card) => card.product))].sort((a, b) => a.localeCompare(b)))
  const detailTitle = $derived(creating ? 'Create a card' : 'Card details')

  async function load(showLoader = false) {
    if (showLoader) loading = true
    try {
      error = ''
      const [board, status] = await Promise.all([api.supportBoard({
        archived: showArchived, q: search.trim(), product: productFilter, category: categoryFilter || undefined,
        priority: priorityFilter || undefined, review_state: reviewFilter || undefined, from: fromDate, to: toDate,
      }), api.supportTriageStatus()])
      lists = board.lists
      cards = board.cards
      triageStatus = status
    } catch (e: any) {
      error = e.message
    } finally {
      loading = false
    }
  }

  function inputFromCard(card: SupportCard): SupportCardDraft {
    return {
      title: card.title,
      product: card.product,
      description: card.description,
      priority: card.priority,
      category: card.category,
      due_at: card.due_at,
      list_id: card.list_id,
      labels: [...card.labels],
    }
  }

  function localDateTime(value: string) {
    if (!value) return ''
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return ''
    return new Date(date.getTime() - date.getTimezoneOffset() * 60_000).toISOString().slice(0, 16)
  }

  function utcDateTime(value: string) {
    return value ? new Date(value).toISOString() : ''
  }

  async function openCard(card: SupportCard) {
    detailCard = card
    cardHistory = []
    cardLinks = []
    relatedCandidates = []
    creating = false
    detailLoading = true
    draft = inputFromCard(card)
    dueLocal = localDateTime(card.due_at)
    commentDraft = ''
    checklistDraft = ''
    error = ''
    await tick()
    if (!boardDialog.open) boardDialog.showModal()
    try {
      const fullCard = await api.supportCard(card.id)
      detailCard = fullCard
      draft = inputFromCard(fullCard)
      dueLocal = localDateTime(fullCard.due_at)
      cards = cards.map((item) => item.id === fullCard.id ? fullCard : item)
      const [historyResult, relationResult] = await Promise.all([
        api.supportCardHistory(card.id), api.supportCardRelated(card.id),
      ])
      cardHistory = historyResult.history
      cardLinks = relationResult.links
      relatedCandidates = relationResult.suggestions
    } catch (e: any) {
      error = e.message
    } finally {
      detailLoading = false
    }
  }

  function refreshCardFromAction() {
    if (!detailCard) return
    const cardId = detailCard.id
    void api.supportCard(cardId).then((updated) => {
      if (detailCard?.id !== cardId) return
      detailCard = updated
      cards = cards.map((item) => item.id === cardId ? updated : item)
    }).catch((e: any) => {
      error = e.message
    })
  }

  async function openNewCard(listId: string) {
    detailCard = null
    cardHistory = []
    creating = true
    detailLoading = false
    saving = false
    draft = { title: '', product: 'Boop', description: '', priority: 'normal', category: 'other', due_at: '', list_id: listId, labels: [] }
    dueLocal = ''
    labelInput = ''
    commentDraft = ''
    checklistDraft = ''
    error = ''
    await tick()
    if (!boardDialog.open) boardDialog.showModal()
  }

  async function saveCard(event?: SubmitEvent) {
    event?.preventDefault()
    if (saving) return
    if (!draft.title.trim()) {
      error = 'Add a title before saving this card.'
      return
    }
    saving = true
    error = ''
    try {
      const payload = { ...draft, title: draft.title.trim(), product: draft.product.trim() || 'Boop', due_at: utcDateTime(dueLocal) }
      let updated: SupportCard
      if (creating) {
        updated = await api.createSupportCard(payload)
        cards = [...cards, updated]
        creating = false
      } else if (detailCard) {
        const priorListId = detailCard.list_id
        updated = await api.updateSupportCard(detailCard.id, payload)
        if (payload.list_id !== priorListId) updated = await api.moveSupportCard(detailCard.id, payload.list_id, 100_000)
        cards = cards.map((card) => card.id === updated.id ? updated : card)
      } else {
        return
      }
      detailCard = updated
      draft = inputFromCard(updated)
      dueLocal = localDateTime(updated.due_at)
      notice = 'Card saved.'
      await load()
    } catch (e: any) {
      error = e.message
    } finally {
      saving = false
    }
  }

  async function markReviewed(card: SupportCard) {
    try {
      const reviewed = await api.reviewSupportCard(card.id)
      cards = cards.map((item) => item.id === reviewed.id ? reviewed : item)
      detailCard = reviewed
      cardHistory = (await api.supportCardHistory(card.id)).history
      notice = 'Card marked as reviewed.'
    } catch (e: any) {
      error = e.message || 'Could not mark this card as reviewed.'
    }
  }

  async function openRelatedCard(cardId: string) {
    try {
      const card = cards.find((item) => item.id === cardId) ?? await api.supportCard(cardId)
      await openCard(card)
    } catch (e: any) {
      error = e.message || 'Could not open the related card.'
    }
  }

  async function createCardLink(candidate: SupportRelatedCandidate, relation: SupportCardLink['relation']) {
    if (!detailCard || relationBusy) return
    relationBusy = candidate.card_id
    error = ''
    try {
      const result = await api.createSupportCardLink(detailCard.id, candidate.card_id, relation)
      cardLinks = result.links
      relatedCandidates = result.suggestions
      notice = relation === 'duplicate' ? 'Card marked as a possible duplicate.' : 'Cards linked.'
    } catch (e: any) {
      error = e.message || 'Could not link these cards.'
    } finally {
      relationBusy = ''
    }
  }

  async function removeCardLink(link: SupportCardLink) {
    if (!detailCard || relationBusy) return
    relationBusy = link.id
    error = ''
    try {
      await api.deleteSupportCardLink(detailCard.id, link.id)
      const result = await api.supportCardRelated(detailCard.id)
      cardLinks = result.links
      relatedCandidates = result.suggestions
      notice = 'Card relationship removed.'
    } catch (e: any) {
      error = e.message || 'Could not remove this relationship.'
    } finally {
      relationBusy = ''
    }
  }

  async function moveCard(cardId: string, listId: string, position: number) {
    if (!cardId || busyCard === cardId) return
    busyCard = cardId
    error = ''
    try {
      const updated = await api.moveSupportCard(cardId, listId, position)
      cards = cards.map((card) => card.id === cardId ? updated : card)
      if (detailCard?.id === cardId) {
        detailCard = updated
        draft = { ...draft, list_id: updated.list_id }
      }
      await load()
    } catch (e: any) {
      error = e.message
      await load()
    } finally {
      busyCard = ''
    }
  }

  function startCardDrag(event: DragEvent, card: SupportCard) {
    if (showArchived || !event.dataTransfer) return
    dragCardId = card.id
    dragListId = ''
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', card.id)
  }

  function startListDrag(event: DragEvent, list: SupportList) {
    if (!event.dataTransfer) return
    dragListId = list.id
    dragCardId = ''
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', `list:${list.id}`)
  }

  function allowDrop(event: DragEvent) {
    event.preventDefault()
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
  }

  async function dropOnList(event: DragEvent, list: SupportList, index: number) {
    event.preventDefault()
    event.stopPropagation()
    if (dragListId) {
      const from = lists.findIndex((item) => item.id === dragListId)
      const to = from < index ? index - 1 : index
      await reorderList(dragListId, to)
    } else if (dragCardId) {
      const target = visibleCards.filter((card) => card.list_id === list.id && !card.archived_at)
      await moveCard(dragCardId, list.id, target.length)
    }
    dragCardId = ''
    dragListId = ''
  }

  async function dropOnCard(event: DragEvent, list: SupportList, targetIndex: number) {
    event.preventDefault()
    event.stopPropagation()
    if (!dragCardId) return
    const target = visibleCards.filter((card) => card.list_id === list.id && !card.archived_at)
    const targetCard = event.currentTarget as HTMLElement
    const after = event.clientY > targetCard.getBoundingClientRect().top + targetCard.offsetHeight / 2
    let position = targetIndex + (after ? 1 : 0)
    const dragged = cards.find((card) => card.id === dragCardId)
    if (dragged?.list_id === list.id) {
      const fromIndex = target.findIndex((card) => card.id === dragCardId)
      if (fromIndex >= 0 && fromIndex < position) position--
    }
    await moveCard(dragCardId, list.id, Math.max(0, position))
    dragCardId = ''
  }

  async function reorderList(listId: string, position: number) {
    try {
      const result = await api.updateSupportList(listId, { position })
      lists = result.lists
    } catch (e: any) {
      error = e.message
    }
  }

  async function createList(event: SubmitEvent) {
    event.preventDefault()
    if (!newListName.trim()) return
    try {
      await api.createSupportList(newListName.trim())
      newListName = ''
      await load()
    } catch (e: any) {
      error = e.message
    }
  }

  function beginRename(list: SupportList) {
    editingListId = list.id
    listNameDraft = list.name
  }

  async function saveListName(list: SupportList) {
    if (!listNameDraft.trim()) return
    try {
      const result = await api.updateSupportList(list.id, { name: listNameDraft.trim() })
      lists = result.lists
      editingListId = ''
    } catch (e: any) {
      error = e.message
    }
  }

  async function deleteList(list: SupportList) {
    if (list.is_default || cards.some((card) => card.list_id === list.id)) return
    try {
      await api.deleteSupportList(list.id)
      lists = lists.filter((item) => item.id !== list.id)
    } catch (e: any) {
      error = e.message
    }
  }

  function addLabel(event?: SubmitEvent) {
    event?.preventDefault()
    const name = labelInput.trim()
    if (!name || draft.labels.some((label) => label.name.toLocaleLowerCase() === name.toLocaleLowerCase())) return
    draft = { ...draft, labels: [...draft.labels, { name, color: labelColor }] }
    labelInput = ''
  }

  function removeLabel(name: string) {
    draft = { ...draft, labels: draft.labels.filter((label) => label.name !== name) }
  }

  async function addComment(event: SubmitEvent) {
    event.preventDefault()
    if (!detailCard || !commentDraft.trim()) return
    try {
      const comment = await api.addSupportComment(detailCard.id, commentDraft.trim())
      detailCard = { ...detailCard, comments: [...detailCard.comments, comment], comments_count: detailCard.comments_count + 1 }
      cards = cards.map((card) => card.id === detailCard?.id ? { ...card, comments_count: (card.comments_count ?? 0) + 1 } : card)
      commentDraft = ''
    } catch (e: any) {
      error = e.message
    }
  }

  async function deleteComment(commentId: string) {
    if (!detailCard) return
    try {
      await api.deleteSupportComment(detailCard.id, commentId)
      detailCard = { ...detailCard, comments: detailCard.comments.filter((comment) => comment.id !== commentId), comments_count: Math.max(0, detailCard.comments_count - 1) }
      cards = cards.map((card) => card.id === detailCard?.id ? { ...card, comments_count: Math.max(0, card.comments_count - 1) } : card)
    } catch (e: any) {
      error = e.message
    }
  }

  async function addChecklistItem(event: SubmitEvent) {
    event.preventDefault()
    if (!detailCard || !checklistDraft.trim()) return
    try {
      const item = await api.addSupportChecklistItem(detailCard.id, checklistDraft.trim())
      detailCard = { ...detailCard, checklist: [...detailCard.checklist, item], checklist_total: detailCard.checklist_total + 1 }
      cards = cards.map((card) => card.id === detailCard?.id ? { ...card, checklist_total: card.checklist_total + 1 } : card)
      checklistDraft = ''
    } catch (e: any) {
      error = e.message
    }
  }

  async function toggleChecklistItem(item: SupportChecklistItem, done: boolean) {
    if (!detailCard) return
    try {
      const updated = await api.updateSupportChecklistItem(detailCard.id, item.id, done)
      detailCard = updated
      cards = cards.map((card) => card.id === updated.id ? updated : card)
    } catch (e: any) {
      error = e.message
    }
  }

  async function deleteChecklistItem(itemId: string) {
    if (!detailCard) return
    try {
      await api.deleteSupportChecklistItem(detailCard.id, itemId)
      const item = detailCard.checklist.find((entry) => entry.id === itemId)
      detailCard = {
        ...detailCard,
        checklist: detailCard.checklist.filter((entry) => entry.id !== itemId),
        checklist_total: Math.max(0, detailCard.checklist_total - 1),
        checklist_done: Math.max(0, detailCard.checklist_done - (item?.done ? 1 : 0)),
      }
      cards = cards.map((card) => card.id === detailCard?.id ? { ...card, checklist_total: detailCard.checklist_total, checklist_done: detailCard.checklist_done } : card)
    } catch (e: any) {
      error = e.message
    }
  }

  async function uploadAttachments(event: Event) {
    if (!detailCard) return
    const input = event.currentTarget as HTMLInputElement
    const files = Array.from(input.files ?? [])
    if (!files.length) return
    busyCard = detailCard.id
    try {
      const updated = await api.addSupportAttachments(detailCard.id, files)
      detailCard = updated
      cards = cards.map((card) => card.id === updated.id ? updated : card)
    } catch (e: any) {
      error = e.message
    } finally {
      input.value = ''
      busyCard = ''
    }
  }

  async function deleteAttachment(attachmentId: string) {
    if (!detailCard) return
    try {
      await api.deleteSupportAttachment(detailCard.id, attachmentId)
      const updated = await api.supportCard(detailCard.id)
      detailCard = updated
      cards = cards.map((card) => card.id === updated.id ? updated : card)
    } catch (e: any) {
      error = e.message
    }
  }

  async function toggleArchive() {
    if (!detailCard) return
    try {
      const updated = detailCard.archived_at
        ? await api.restoreSupportCard(detailCard.id)
        : await api.archiveSupportCard(detailCard.id)
      detailCard = updated
      await load()
      if (showArchived !== Boolean(updated.archived_at)) boardDialog.close()
    } catch (e: any) {
      error = e.message
    }
  }

  function closeDialog() {
    detailCard = null
    cardHistory = []
    cardLinks = []
    relatedCandidates = []
    creating = false
    detailLoading = false
    labelInput = ''
    commentDraft = ''
    checklistDraft = ''
  }

  function summary(card: SupportCard) {
    return (card.description || (card.manual ? '' : card.original_text)).trim()
  }

  function formatDate(value: string) {
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '' : new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric' }).format(date)
  }

  function dueState(card: SupportCard) {
    if (!card.due_at) return ''
    const due = new Date(card.due_at).getTime()
    return due < Date.now() && card.status !== 'done' ? 'overdue' : 'due'
  }

  function attachmentURL(card: SupportCard, attachmentId: string) {
    return `/api/v1/support/cards/${encodeURIComponent(card.id)}/attachments/${encodeURIComponent(attachmentId)}`
  }

  $effect(() => {
    const filterSnapshot = [search, productFilter, categoryFilter, priorityFilter, reviewFilter, fromDate, toDate, showArchived].join('|')
    void filterSnapshot
    const timer = setTimeout(() => load(), 250)
    return () => clearTimeout(timer)
  })

  $effect(() => {
    const timer = setInterval(() => load(), 30_000)
    return () => clearInterval(timer)
  })
</script>

<div class="stack support-page">
  <div class="toolbar">
    <div class="heading">
      <h1>{rulesView ? 'Classification rules' : activityView ? 'AI triage activity' : insightsView ? 'Support insights' : showArchived ? 'Archived cards' : 'Support board'}</h1>
      <p class="muted">Customer reports, bugs, suggestions, and partner leads from every Boop project.</p>
    </div>
    <div class="toolbar-actions">
      <Button variant={!activityView && !rulesView && !insightsView ? 'primary' : 'secondary'} size="sm" onclick={() => { activityView = false; rulesView = false; insightsView = false }}>
        Board
      </Button>
      <Button variant={activityView ? 'primary' : 'secondary'} size="sm" onclick={() => { activityView = true; rulesView = false; insightsView = false }}>
        AI activity
      </Button>
      <Button variant={rulesView ? 'primary' : 'secondary'} size="sm" onclick={() => { rulesView = true; activityView = false; insightsView = false }}>
        Rules
      </Button>
      <Button variant={insightsView ? 'primary' : 'secondary'} size="sm" onclick={() => { insightsView = true; activityView = false; rulesView = false }}>
        Insights
      </Button>
      {#if !activityView && !rulesView && !insightsView}
        <Button variant="secondary" size="sm" onclick={() => (showArchived = !showArchived)}>
          {showArchived ? 'Back to board' : 'Archive'}
        </Button>
        {#if !showArchived}<Button size="sm" onclick={() => openNewCard(lists[0]?.id ?? '')}>＋ New card</Button>{/if}
      {/if}
      <Button variant="secondary" size="sm" onclick={() => load(true)} disabled={loading}>Refresh</Button>
    </div>
  </div>

  {#if triageStatus}
    <div class="triage-line" aria-live="polite">
      <span class="ai-mark">AI</span>
      <span>Checks for new notifications hourly</span>
      <span class="dot">·</span>
      <span>{triageStatus.pending + triageStatus.processing} pending</span>
      <span class="dot">·</span>
      <span>{triageStatus.failed} failed</span>
      <span class="dot">·</span>
      <span>{triageStatus.matched} cards created</span>
      <span class="dot">·</span>
      <span>{triageStatus.ignored} routine notices filtered</span>
      {#if triageStatus.last_finished_at}<span class="dot">·</span><span>Last run {formatDate(triageStatus.last_finished_at)}</span>{/if}
    </div>
    {#if triageStatus.configuration_error}
      <Notice tone="bad">The saved OpenRouter key could not be read. Replace it or remove it in <a href="/settings" onclick={link}>Settings</a> to resume queued triage.</Notice>
    {:else if !triageStatus.configured}
      <Notice tone="warn">AI triage is paused because no OpenRouter key is configured. Add a key and choose a model in <a href="/settings" onclick={link}>Settings</a>; queued notifications will be reviewed automatically.</Notice>
    {/if}
  {/if}
  {#if notice}<p class="success-note" role="status">{notice}</p>{/if}
  {#if error}<Notice tone="bad"><span role="alert">{error.includes('support_auth_required') ? 'The private support board is disabled. Configure BOOP_ADMIN_USER and BOOP_ADMIN_PASSWORD, then sign in.' : error}</span></Notice>{/if}

  {#if activityView}
    <SupportActivity onChanged={() => load()} />
  {:else if rulesView}
    <SupportRules onChanged={() => load()} />
  {:else if insightsView}
    <SupportInsights />
  {:else}
  <div class="board-tools">
    <label class="search">
      <span aria-hidden="true">⌕</span>
      <input bind:value={search} type="search" placeholder="Search cards" aria-label="Search cards" />
    </label>
    {#if !showArchived}
      <select bind:value={productFilter} aria-label="Filter by project">
        <option value="">All projects</option>
        {#each products as product (product)}<option value={product}>{product}</option>{/each}
      </select>
      <select bind:value={categoryFilter} aria-label="Filter by category">
        <option value="">All categories</option>
        {#each categories as category (category.value)}<option value={category.value}>{category.label}</option>{/each}
      </select>
      <select bind:value={priorityFilter} aria-label="Filter by priority">
        <option value="">All priorities</option>
        <option value="low">Low</option><option value="normal">Normal</option><option value="high">High</option><option value="urgent">Urgent</option>
      </select>
      <select bind:value={reviewFilter} aria-label="Filter by review state">
        <option value="">All review states</option>
        <option value="needs_review">Needs review</option>
        <option value="reviewed">Reviewed</option>
      </select>
      <label class="date-filter"><span>From</span><input bind:value={fromDate} type="date" aria-label="Filter reported from date" /></label>
      <label class="date-filter"><span>To</span><input bind:value={toDate} type="date" aria-label="Filter reported to date" /></label>
    {/if}
    <span class="result-count">{visibleCards.length} {visibleCards.length === 1 ? 'card' : 'cards'}</span>
  </div>

  {#if loading && lists.length === 0}
    <Card><div class="muted">Loading board…</div></Card>
  {:else if showArchived}
    <div class="archive-board" aria-label="Archived support cards">
      {#if visibleCards.length === 0}
        <Card><p class="muted">No archived cards match this search.</p></Card>
      {:else}
        {#each visibleCards as card (card.id)}
          <button type="button" class="ticket archive-ticket" onclick={() => openCard(card)}>
            {#each card.labels as label (label.name)}<span class={`label ${label.color}`}>{label.name}</span>{/each}
            <strong>{card.title}</strong>
            <span class="product">{card.product}</span>
          </button>
        {/each}
      {/if}
    </div>
  {:else}
    <div class="board" aria-label="Support Kanban board">
      {#each lists as list, listIndex (list.id)}
        {@const laneCards = visibleCards.filter((card) => card.list_id === list.id && !card.archived_at)}
        <section class="lane" aria-label={`${list.name}, ${laneCards.length} cards`} ondragover={allowDrop} ondrop={(event) => dropOnList(event, list, listIndex)}>
          <header class="lane-head" role="group" aria-label={`Reorder ${list.name} list`} draggable="true" ondragstart={(event) => startListDrag(event, list)} ondragover={allowDrop} ondrop={(event) => dropOnList(event, list, listIndex)}>
            {#if editingListId === list.id}
              <form class="rename-form" onsubmit={(event) => { event.preventDefault(); saveListName(list) }}>
                <input bind:value={listNameDraft} aria-label="List name" maxlength="80" />
                <button type="submit" aria-label="Save list name">Save</button>
              </form>
            {:else}
              <h2>{list.name}<span class="count">{laneCards.length}</span></h2>
              <div class="list-actions">
                <button type="button" class="icon-button" aria-label={`Move ${list.name} left`} title="Move list left" disabled={listIndex === 0} onclick={() => reorderList(list.id, listIndex - 1)}>←</button>
                <button type="button" class="icon-button" aria-label={`Move ${list.name} right`} title="Move list right" disabled={listIndex === lists.length - 1} onclick={() => reorderList(list.id, listIndex + 1)}>→</button>
                <button type="button" class="icon-button" aria-label={`Rename ${list.name}`} title="Rename list" onclick={() => beginRename(list)}>···</button>
                {#if !list.is_default}
                  <button type="button" class="icon-button remove-list" aria-label={`Delete ${list.name}`} title="Delete empty list" disabled={cards.some((card) => card.list_id === list.id)} onclick={() => deleteList(list)}>×</button>
                {/if}
              </div>
            {/if}
          </header>
          <div class="lane-cards" class:drop-target={!!dragCardId && laneCards.length === 0} role="list" aria-label={`${list.name} cards`} ondragover={allowDrop} ondrop={(event) => dropOnList(event, list, listIndex)}>
            {#each laneCards as card, cardIndex (card.id)}
              <button
                type="button"
                class="ticket"
                class:dragging={dragCardId === card.id}
                draggable={!showArchived && !search.trim() && !productFilter && !categoryFilter && !priorityFilter && !reviewFilter && !fromDate && !toDate}
                ondragstart={(event) => startCardDrag(event, card)}
                ondragend={() => { dragCardId = ''; dragListId = '' }}
                ondragover={allowDrop}
                ondrop={(event) => dropOnCard(event, list, cardIndex)}
                onclick={() => openCard(card)}
                aria-label={`${card.title}, ${card.product}, open card details`}
              >
                <div class="ticket-top">
                  {#if card.priority !== 'normal'}<span class={`priority ${card.priority}`}>{card.priority}</span>{/if}
                  <span class="category-tag">{card.category.replace('_', ' ')}</span>
                  <span class="product">{card.product}</span>
                  <time datetime={card.reported_at}>{formatDate(card.reported_at)}</time>
                </div>
                {#if card.labels.length}<div class="labels">{#each card.labels as label (label.name)}<span class={`label ${label.color}`}>{label.name}</span>{/each}</div>{/if}
                <strong class="ticket-title">{card.title}</strong>
                {#if summary(card)}<span class="ticket-summary">{summary(card)}</span>{/if}
                <div class="ticket-bottom">
                  <span class="ticket-source">{card.manual ? 'Manual card' : card.sender || card.source_account}</span>
                  {#if card.review_state === 'needs_review'}<span class="review-tag">Needs review</span>{/if}
                  <span class="ticket-metrics">
                    {#if card.due_at}<span class:overdue={dueState(card) === 'overdue'} title="Due date">◷ {formatDate(card.due_at)}</span>{/if}
                    {#if card.checklist_total}<span title="Checklist progress">☑ {card.checklist_done}/{card.checklist_total}</span>{/if}
                    {#if card.comments_count}<span title="Comments">▤ {card.comments_count}</span>{/if}
                    {#if card.attachments.length}<span title="Attachments">⌁ {card.attachments.length}</span>{/if}
                  </span>
                </div>
              </button>
            {/each}
            {#if laneCards.length === 0}<p class="empty-lane">Drop a card here or add one below</p>{/if}
          </div>
          <button type="button" class="add-card" onclick={() => openNewCard(list.id)}>＋ Add a card</button>
        </section>
      {/each}
      <form class="add-list" onsubmit={createList}>
        <label for="new-list">Add another list</label>
        <input id="new-list" bind:value={newListName} maxlength="80" placeholder="List name" />
        <Button variant="secondary" size="sm" disabled={!newListName.trim()}>＋ Add list</Button>
      </form>
    </div>
  {/if}
  {/if}
</div>

<dialog bind:this={boardDialog} class="card-dialog" onclose={closeDialog}>
  <div class="detail-form">
    <header class="dialog-head">
      <div><span class="dialog-eyebrow">{detailCard?.product ?? 'Support board'}</span><h2>{detailTitle}</h2></div>
      <button type="button" class="dialog-close" aria-label="Close card details" onclick={() => boardDialog.close()}>×</button>
    </header>
    {#if detailLoading}
      <div class="dialog-loading">Loading card details…</div>
    {:else}
      <div class="dialog-content">
        <main class="dialog-main">
          <label class="field"><span>Title</span><input bind:value={draft.title} required maxlength="500" placeholder="What needs to get done?" /></label>
          <label class="field"><span>Description</span><textarea bind:value={draft.description} rows="5" maxlength="200000" placeholder="Add context, acceptance criteria, or notes…"></textarea></label>

          {#if !creating && detailCard}
            <section class="detail-section">
              <div class="section-title"><h3>Checklist</h3><span>{detailCard.checklist_done}/{detailCard.checklist_total}</span></div>
              {#if detailCard.checklist_total}<div class="progress"><span style={`width:${Math.round(detailCard.checklist_done / detailCard.checklist_total * 100)}%`}></span></div>{/if}
              <ul class="checklist">
                {#each detailCard.checklist as item (item.id)}
                  <li><input type="checkbox" checked={item.done} aria-label={`Mark ${item.text} ${item.done ? 'incomplete' : 'complete'}`} onchange={(event) => toggleChecklistItem(item, event.currentTarget.checked)} /><span class:completed={item.done}>{item.text}</span><button type="button" class="inline-remove" aria-label={`Remove ${item.text}`} onclick={() => deleteChecklistItem(item.id)}>×</button></li>
                {/each}
              </ul>
              <form class="inline-form" onsubmit={addChecklistItem}><input bind:value={checklistDraft} maxlength="500" placeholder="Add checklist item" aria-label="New checklist item" /><Button variant="secondary" size="sm" disabled={!checklistDraft.trim()}>Add</Button></form>
            </section>

            <section class="detail-section">
              <div class="section-title"><h3>Activity</h3><span>{detailCard.comments_count} comments</span></div>
              <ul class="comments">
                {#each detailCard.comments as comment (comment.id)}
                  <li><div class="comment-meta"><strong>{comment.author}</strong><time datetime={comment.created_at}>{new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(comment.created_at))}</time><button type="button" class="inline-remove" aria-label="Delete comment" onclick={() => deleteComment(comment.id)}>×</button></div><p>{comment.body}</p></li>
                {/each}
              </ul>
              <form class="comment-form" onsubmit={addComment}><textarea bind:value={commentDraft} rows="3" maxlength="10000" placeholder="Write a comment…" aria-label="Write a comment"></textarea><Button size="sm" disabled={!commentDraft.trim()}>Add comment</Button></form>
            </section>

            <section class="detail-section">
              <div class="section-title"><h3>Change history</h3><span>{cardHistory.length}</span></div>
              {#if cardHistory.length}
                <ul class="card-history">
                  {#each cardHistory as entry (entry.id)}
                    <li><strong>{entry.actor} · {entry.action}</strong>{#if entry.field}<span>{entry.field}: {entry.old_value || '—'} → {entry.new_value || '—'}</span>{/if}<time datetime={entry.created_at}>{new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(entry.created_at))}</time></li>
                  {/each}
                </ul>
              {:else}
                <p class="muted caption">No changes recorded yet.</p>
              {/if}
            </section>

            <section class="detail-section">
              <div class="section-title"><h3>Related cards</h3><span>{cardLinks.length}</span></div>
              {#if cardLinks.length}
                <ul class="related-list">
                  {#each cardLinks as item (item.id)}
                    <li>
                      <button type="button" class="related-title" onclick={() => openRelatedCard(item.related_card_id)}>{item.direction === 'duplicate_of' ? 'Duplicate of' : item.direction === 'duplicated_by' ? 'Duplicated by' : 'Related'} · {item.title}</button>
                      <span>{item.product}</span>
                      <button type="button" class="inline-remove" aria-label={`Remove relation to ${item.title}`} onclick={() => removeCardLink(item)} disabled={relationBusy === item.id}>×</button>
                    </li>
                  {/each}
                </ul>
              {/if}
              {#if relatedCandidates.length}
                <p class="muted caption">Suggestions use exact issue references or URLs first, then similar titles. Review before linking; nothing is merged automatically.</p>
                <ul class="related-list">
                  {#each relatedCandidates as candidate (candidate.card_id)}
                    <li class="candidate-row">
                      <button type="button" class="related-title" onclick={() => openRelatedCard(candidate.card_id)}>{candidate.title}</button>
                      <span>{candidate.product} · {candidate.match_reason === 'exact_reference' ? `${candidate.match_count} shared references` : `${candidate.match_count} shared title terms`}</span>
                      <div class="related-actions">
                        <Button variant="secondary" size="sm" onclick={() => createCardLink(candidate, 'related')} disabled={!!relationBusy}>Link</Button>
                        <Button variant="secondary" size="sm" onclick={() => createCardLink(candidate, 'duplicate')} disabled={!!relationBusy}>Duplicate</Button>
                      </div>
                    </li>
                  {/each}
                </ul>
              {:else if !cardLinks.length}
                <p class="muted caption">No exact-reference matches found. No cards are linked automatically.</p>
              {/if}
            </section>

            <SupportActions card={detailCard} onChanged={refreshCardFromAction} />

            {#if !detailCard.manual}
              <details class="source-details">
                <summary>Original notification</summary>
                <p class="source-meta">{detailCard.sender || detailCard.source_account} · {detailCard.product} · {new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(detailCard.reported_at))}</p>
                <pre>{detailCard.original_text}</pre>
                <small>Conversation {detailCard.source_thread_id} · Message {detailCard.source_message_id}</small>
              </details>
            {/if}
          {/if}
        </main>

        <aside class="dialog-side">
          <label class="field"><span>List</span><select bind:value={draft.list_id}>{#each lists as list (list.id)}<option value={list.id}>{list.name}</option>{/each}</select></label>
          <label class="field"><span>Project / product</span><input bind:value={draft.product} maxlength="200" placeholder="Boop" /></label>
          <label class="field"><span>Priority</span><select bind:value={draft.priority}><option value="normal">Normal</option><option value="low">Low</option><option value="high">High</option><option value="urgent">Urgent</option></select></label>
          <label class="field"><span>Category</span><select bind:value={draft.category}>{#each categories as category (category.value)}<option value={category.value}>{category.label}</option>{/each}</select></label>
          {#if !creating && detailCard && !detailCard.manual}
            <section class="ai-classification">
              <div class="section-title"><h3>AI classification</h3><span class:needs-review={detailCard.review_state === 'needs_review'}>{detailCard.review_state === 'needs_review' ? 'Needs review' : 'Reviewed'}</span></div>
              <p>Suggested: {detailCard.ai_category || '—'} · {detailCard.ai_priority || '—'} · {Math.round(detailCard.ai_confidence * 100)}% confidence</p>
              {#if detailCard.ai_reason}<small>{detailCard.ai_reason}</small>{/if}
            </section>
          {/if}
          <label class="field"><span>Due date</span><input type="datetime-local" bind:value={dueLocal} /></label>

          <section class="detail-section label-section">
            <div class="section-title"><h3>Labels</h3></div>
            <div class="labels">{#each draft.labels as label (label.name)}<button type="button" class={`label ${label.color}`} onclick={() => removeLabel(label.name)} title="Remove label">{label.name} ×</button>{/each}</div>
            <form class="label-form" onsubmit={addLabel}>
              <input bind:value={labelInput} maxlength="48" placeholder="Label name" aria-label="New label" />
              <select bind:value={labelColor} aria-label="Label color">{#each labelPalette as color (color)}<option value={color}>{color}</option>{/each}</select>
              <Button variant="secondary" size="sm" disabled={!labelInput.trim()}>Add</Button>
            </form>
          </section>

          {#if !creating && detailCard}
            <section class="detail-section">
              <div class="section-title"><h3>Attachments</h3><span>{detailCard.attachments.length}</span></div>
              <div class="attachments">
                {#each detailCard.attachments as attachment (attachment.id)}
                  <div class="attachment">
                    <a href={attachmentURL(detailCard, attachment.id)} target="_blank" rel="noreferrer"><img src={attachmentURL(detailCard, attachment.id)} alt={attachment.filename} loading="lazy" /><span>{attachment.filename}</span></a>
                    <button type="button" class="inline-remove" aria-label={`Remove ${attachment.filename}`} onclick={() => deleteAttachment(attachment.id)}>×</button>
                  </div>
                {/each}
              </div>
              <label class="upload-button">＋ Attach images<input type="file" accept="image/png,image/jpeg,image/gif" multiple onchange={uploadAttachments} /></label>
              <small class="muted">PNG, JPG, or GIF · 7 MB total</small>
            </section>
          {/if}

          {#if !creating && detailCard}
            <div class="source-stamp"><span>{detailCard.manual ? 'Created manually' : 'From Boop notification'}</span><time datetime={detailCard.reported_at}>{formatDate(detailCard.reported_at)}</time></div>
          {/if}
        </aside>
      </div>
      {#if error}<p class="dialog-error" role="alert">{error}</p>{/if}
      <footer class="dialog-footer">
        {#if !creating}<Button variant="danger" size="sm" type="button" onclick={toggleArchive}>{detailCard?.archived_at ? 'Restore card' : 'Archive card'}</Button>{/if}
        {#if !creating && detailCard?.review_state === 'needs_review'}<Button variant="secondary" size="sm" type="button" onclick={() => markReviewed(detailCard!)}>Mark reviewed</Button>{/if}
        <span class="spacer"></span>
        <Button variant="secondary" size="sm" type="button" onclick={() => boardDialog.close()}>Close</Button>
        <Button size="sm" type="button" onclick={() => saveCard()} disabled={saving || detailLoading}>{saving ? 'Saving…' : 'Save card'}</Button>
      </footer>
    {/if}
  </div>
</dialog>

<style>
  .support-page { min-width: 0; gap: 16px; }
  h1 { font: var(--up-type-wordmark); color: var(--up-ink); }
  h2 { font: var(--up-type-setting); color: var(--up-ink); }
  h3 { font: var(--up-type-ui); color: var(--up-ink); }
  .toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
  .heading p { margin-top: 5px; }
  .toolbar-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .triage-line { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; color: var(--up-text-muted); font: var(--up-type-caption); }
  .ai-mark { color: var(--up-accent-hover); background: var(--up-accent-tint); border-radius: 4px; padding: 3px 6px; font-weight: 700; }
  .dot { color: var(--up-text-faint); }
  .success-note { color: #287a59; font: var(--up-type-caption); }
  .board-tools { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .search { display: flex; align-items: center; gap: 7px; width: min(320px, 100%); height: 34px; padding: 0 10px; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); color: var(--up-text-muted); background: var(--up-bg); }
  .search input { width: 100%; border: 0; outline: 0; color: var(--up-ink); font: var(--up-type-meta); background: transparent; }
  .board-tools select, .field input, .field select, .label-form input, .label-form select, .inline-form input { min-height: 34px; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); padding: 6px 9px; color: var(--up-ink); background: var(--up-bg); font: var(--up-type-meta); }
  .date-filter { display: inline-flex; align-items: center; gap: 5px; color: var(--up-text-muted); font: var(--up-type-caption); }
  .date-filter input { min-height: 34px; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); padding: 5px 7px; color: var(--up-ink); background: var(--up-bg); font: var(--up-type-caption); }
  .result-count { margin-left: auto; color: var(--up-text-muted); font: var(--up-type-caption); }
  .board { display: flex; align-items: flex-start; gap: 12px; overflow-x: auto; padding: 2px 2px 10px; min-height: min(68vh, 760px); }
  .lane { display: flex; flex: 0 0 286px; flex-direction: column; max-height: calc(100vh - 345px); min-height: 260px; padding: 8px; border: 1px solid #eceef4; border-radius: 10px; background: #f4f5f9; }
  .lane-head { display: flex; align-items: center; justify-content: space-between; min-height: 34px; padding: 0 2px 7px; cursor: grab; }
  .lane-head:active { cursor: grabbing; }
  .lane-head h2 { display: flex; min-width: 0; align-items: center; gap: 8px; overflow-wrap: anywhere; }
  .count { display: inline-flex; align-items: center; justify-content: center; min-width: 20px; height: 20px; padding: 0 5px; border-radius: 5px; background: #e8eaf1; color: var(--up-text-secondary); font: var(--up-type-caption); }
  .list-actions { display: flex; align-items: center; }
  .icon-button, .dialog-close, .inline-remove { display: inline-flex; align-items: center; justify-content: center; border: 0; background: transparent; color: var(--up-text-muted); cursor: pointer; }
  .icon-button { width: 25px; height: 25px; border-radius: 5px; font: var(--up-type-ui); }
  .icon-button:hover:not(:disabled), .dialog-close:hover, .inline-remove:hover { background: #e8eaf1; color: var(--up-ink); }
  .icon-button:disabled { opacity: .35; cursor: default; }
  .remove-list:hover:not(:disabled) { color: #a63b42; }
  .rename-form { display: flex; width: 100%; gap: 5px; }
  .rename-form input { width: 100%; min-width: 0; height: 30px; border: 1px solid var(--up-border-control); border-radius: 5px; padding: 4px 7px; font: var(--up-type-ui); }
  .rename-form button { border: 0; border-radius: 5px; padding: 0 7px; background: var(--up-accent); color: white; cursor: pointer; font: var(--up-type-caption); }
  .lane-cards { display: flex; min-height: 80px; flex: 1; flex-direction: column; gap: 8px; overflow-y: auto; padding: 1px 2px 5px; border-radius: 6px; }
  .lane-cards.drop-target { background: rgba(124, 131, 232, .09); }
  .ticket { display: flex; width: 100%; flex-direction: column; gap: 8px; padding: 10px; border: 1px solid #e7e8ef; border-radius: 7px; background: var(--up-bg); color: var(--up-ink); box-shadow: 0 1px 2px rgba(23,24,31,.04); text-align: left; cursor: grab; }
  .ticket:hover { border-color: #d6d9e8; background: white; box-shadow: 0 2px 6px rgba(23,24,31,.08); }
  .ticket:focus-visible, .icon-button:focus-visible, .dialog-close:focus-visible { outline: 2px solid var(--up-accent); outline-offset: 2px; }
  .ticket.dragging { opacity: .45; }
  .ticket-top, .ticket-bottom { display: flex; align-items: center; justify-content: space-between; gap: 7px; min-width: 0; }
  .ticket-top time { flex: none; color: var(--up-text-muted); font: var(--up-type-caption); }
  .product { overflow: hidden; color: var(--up-accent-hover); font: var(--up-type-caption); font-weight: 700; text-overflow: ellipsis; white-space: nowrap; }
  .priority { flex: none; padding: 2px 5px; border-radius: 4px; font: var(--up-type-caption); text-transform: capitalize; }
  .priority.low { color: #53775e; background: #e7f2e8; }
  .priority.high { color: #9a6221; background: #fff0d7; }
  .priority.urgent { color: #a13742; background: #fde5e6; }
  .category-tag, .review-tag { display: inline-flex; width: fit-content; align-items: center; border-radius: 4px; padding: 2px 5px; background: #eef0f7; color: var(--up-text-secondary); font: var(--up-type-caption); text-transform: capitalize; }
  .review-tag { background: #fff0d7; color: #8b5d16; }
  .ai-classification { display: flex; flex-direction: column; gap: 7px; padding: 10px; border: 1px solid var(--up-border-hairline); border-radius: 7px; background: white; }
  .ai-classification p, .ai-classification small { color: var(--up-text-secondary); font: var(--up-type-caption); }
  .ai-classification small { white-space: pre-wrap; overflow-wrap: anywhere; }
  .ai-classification .needs-review { color: #8b5d16; }
  .ticket-title { display: -webkit-box; overflow: hidden; color: var(--up-ink); font: var(--up-type-ui); line-clamp: 3; -webkit-box-orient: vertical; -webkit-line-clamp: 3; }
  .ticket-summary { display: -webkit-box; overflow: hidden; color: var(--up-text-secondary); font: var(--up-type-caption); white-space: pre-wrap; overflow-wrap: anywhere; line-clamp: 3; -webkit-box-orient: vertical; -webkit-line-clamp: 3; }
  .labels { display: flex; flex-wrap: wrap; gap: 5px; }
  .label { display: inline-flex; align-items: center; width: fit-content; border: 0; border-radius: 4px; padding: 3px 6px; font: var(--up-type-caption); line-height: 1.2; }
  .label.purple { color: #5f4aa5; background: #eee9fa; }
  .label.blue { color: #316589; background: #e3f0f7; }
  .label.green { color: #367454; background: #e3f3e8; }
  .label.yellow { color: #85650e; background: #fbf2cf; }
  .label.orange { color: #925521; background: #fcebdc; }
  .label.red { color: #973c47; background: #f9e5e7; }
  .label.gray { color: #596071; background: #e9ebef; }
  button.label { cursor: pointer; }
  .ticket-bottom { margin-top: 2px; color: var(--up-text-muted); font: var(--up-type-caption); }
  .ticket-source { overflow: hidden; min-width: 0; text-overflow: ellipsis; white-space: nowrap; }
  .ticket-metrics { display: flex; flex: none; align-items: center; gap: 7px; }
  .ticket-metrics .overdue { color: #a13742; }
  .empty-lane { padding: 18px 6px; color: var(--up-text-muted); font: var(--up-type-caption); text-align: center; }
  .add-card { display: flex; flex: none; align-items: center; gap: 5px; margin: 5px 1px 0; padding: 8px 5px; border: 0; border-radius: 5px; background: transparent; color: var(--up-text-secondary); font: var(--up-type-ui); text-align: left; cursor: pointer; }
  .add-card:hover { background: #e9eaf2; color: var(--up-ink); }
  .add-list { display: flex; flex: 0 0 238px; flex-direction: column; gap: 8px; padding: 13px; border: 1px dashed var(--up-border-control); border-radius: 10px; background: #fafafd; }
  .add-list label { font: var(--up-type-ui); color: var(--up-text-secondary); }
  .add-list input { min-height: 34px; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); padding: 6px 9px; font: var(--up-type-meta); }
  .archive-board { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 10px; align-items: start; }
  .archive-ticket { cursor: pointer; }

  .card-dialog { width: min(940px, calc(100vw - 28px)); max-width: 940px; max-height: min(90vh, 900px); overflow: hidden; padding: 0; border: 1px solid var(--up-border-control); border-radius: 12px; color: var(--up-ink); background: var(--up-bg); box-shadow: 0 18px 60px rgba(23,24,31,.22); }
  .card-dialog::backdrop { background: rgba(18,19,28,.48); }
  .detail-form { display: flex; max-height: min(90vh, 900px); flex-direction: column; }
  .dialog-head { display: flex; flex: none; align-items: flex-start; justify-content: space-between; gap: 16px; padding: 18px 22px; border-bottom: 1px solid var(--up-border-hairline); }
  .dialog-head h2 { margin-top: 2px; font: var(--up-type-wordmark); }
  .dialog-eyebrow { color: var(--up-accent-hover); font: var(--up-type-caption); }
  .dialog-close { width: 30px; height: 30px; border-radius: 6px; font-size: 23px; }
  .dialog-content { display: grid; min-height: 0; grid-template-columns: minmax(0, 1.5fr) minmax(240px, .85fr); overflow: auto; }
  .dialog-main, .dialog-side { display: flex; min-width: 0; flex-direction: column; gap: 18px; padding: 20px 22px; }
  .dialog-side { border-left: 1px solid var(--up-border-hairline); background: #fafafd; }
  .field { display: flex; flex-direction: column; gap: 6px; color: var(--up-text-secondary); font: var(--up-type-caption); }
  .field input, .field select { width: 100%; min-height: 36px; color: var(--up-ink); font: var(--up-type-meta); }
  .field textarea, .comment-form textarea { width: 100%; resize: vertical; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); padding: 9px 10px; color: var(--up-ink); background: var(--up-bg); font: var(--up-type-meta); }
  .detail-section { display: flex; flex-direction: column; gap: 10px; }
  .section-title { display: flex; align-items: center; justify-content: space-between; color: var(--up-text-muted); font: var(--up-type-caption); }
  .section-title h3 { font: var(--up-type-ui); }
  .progress { height: 5px; overflow: hidden; border-radius: 4px; background: #e9eaf1; }
  .progress span { display: block; height: 100%; background: var(--up-alt-mint); }
  .checklist, .comments { display: flex; flex-direction: column; gap: 7px; margin: 0; padding: 0; list-style: none; }
  .checklist li { display: flex; align-items: center; gap: 9px; min-width: 0; font: var(--up-type-meta); }
  .checklist input { flex: none; accent-color: var(--up-accent); }
  .checklist li span { flex: 1; overflow-wrap: anywhere; }
  .completed { color: var(--up-text-muted); text-decoration: line-through; }
  .inline-remove { flex: none; width: 23px; height: 23px; border-radius: 4px; font-size: 17px; }
  .inline-form, .label-form { display: flex; align-items: center; gap: 6px; }
  .inline-form input, .label-form input { flex: 1; min-width: 0; }
  .comments li { padding: 9px; border: 1px solid var(--up-border-hairline); border-radius: 7px; background: white; }
  .comment-meta { display: flex; align-items: center; gap: 8px; color: var(--up-text-muted); font: var(--up-type-caption); }
  .comment-meta strong { color: var(--up-text-secondary); }
  .comment-meta time { margin-left: auto; }
  .comments li p { margin-top: 6px; color: var(--up-ink); font: var(--up-type-meta); white-space: pre-wrap; overflow-wrap: anywhere; }
  .card-history { display: flex; max-height: 190px; flex-direction: column; gap: 8px; overflow: auto; margin: 0; padding: 0; list-style: none; }
  .card-history li { display: flex; flex-direction: column; gap: 3px; padding: 7px 8px; border-left: 2px solid var(--up-accent); background: white; }
  .card-history strong, .card-history span, .card-history time { color: var(--up-text-secondary); font: var(--up-type-caption); }
  .related-list { display: flex; flex-direction: column; gap: 7px; margin: 0; padding: 0; list-style: none; }
  .related-list li { display: flex; align-items: center; gap: 8px; min-width: 0; padding: 8px; border: 1px solid var(--up-border-hairline); border-radius: 6px; background: white; }
  .related-title { flex: 1; min-width: 0; border: 0; padding: 0; overflow: hidden; background: transparent; color: var(--up-ink); font: var(--up-type-meta); text-align: left; text-overflow: ellipsis; white-space: nowrap; cursor: pointer; }
  .related-list li > span { color: var(--up-text-muted); font: var(--up-type-caption); white-space: nowrap; }
  .related-list .candidate-row { align-items: flex-start; flex-direction: column; }
  .related-list .candidate-row .related-title { width: 100%; }
  .related-actions { display: flex; gap: 6px; }
  .comment-form { display: flex; align-items: flex-end; flex-direction: column; gap: 7px; }
  .label-section .labels:empty { display: none; }
  .label-form select { width: 85px; text-transform: capitalize; }
  .source-details { padding-top: 12px; border-top: 1px solid var(--up-border-hairline); }
  .source-details summary { color: var(--up-text-secondary); font: var(--up-type-ui); cursor: pointer; }
  .source-meta { margin-top: 10px; color: var(--up-text-muted); font: var(--up-type-caption); overflow-wrap: anywhere; }
  .source-details pre { max-height: 260px; overflow: auto; margin: 9px 0; padding: 10px; border-radius: 6px; background: #f4f5f9; color: var(--up-ink); font: var(--up-type-caption); white-space: pre-wrap; overflow-wrap: anywhere; }
  .source-details small, .dialog-side small { color: var(--up-text-muted); font: var(--up-type-caption); overflow-wrap: anywhere; }
  .attachments { display: flex; flex-direction: column; gap: 7px; }
  .attachment { display: flex; align-items: center; justify-content: space-between; gap: 6px; padding: 5px; border: 1px solid var(--up-border-hairline); border-radius: 6px; background: white; }
  .attachment a { display: flex; min-width: 0; align-items: center; gap: 8px; color: var(--up-text-secondary); font: var(--up-type-caption); }
  .attachment a span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .attachment img { width: 40px; height: 40px; flex: none; border-radius: 4px; object-fit: cover; }
  .upload-button { display: inline-flex; width: fit-content; align-items: center; gap: 6px; padding: 7px 9px; border: 1px solid var(--up-border-control); border-radius: 6px; background: white; color: var(--up-text-secondary); font: var(--up-type-caption); cursor: pointer; }
  .upload-button input { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0,0,0,0); white-space: nowrap; }
  .source-stamp { display: flex; justify-content: space-between; gap: 8px; padding-top: 8px; border-top: 1px solid var(--up-border-hairline); color: var(--up-text-muted); font: var(--up-type-caption); }
  .dialog-loading { padding: 28px; color: var(--up-text-muted); }
  .dialog-error { flex: none; padding: 0 22px 10px; color: #a13742; font: var(--up-type-caption); }
  .dialog-footer { display: flex; flex: none; align-items: center; gap: 8px; padding: 12px 22px; border-top: 1px solid var(--up-border-hairline); }
  .spacer { flex: 1; }

  @media (max-width: 760px) {
    .toolbar { align-items: flex-start; flex-direction: column; }
    .toolbar-actions { width: 100%; }
    .board { min-height: 58vh; }
    .lane { max-height: 64vh; }
    .card-dialog { width: 100vw; max-width: none; max-height: 100dvh; height: 100dvh; border: 0; border-radius: 0; }
    .detail-form { max-height: 100dvh; height: 100dvh; }
    .dialog-content { display: flex; flex-direction: column; }
    .dialog-side { border-top: 1px solid var(--up-border-hairline); border-left: 0; }
    .dialog-main, .dialog-side { padding: 16px; }
    .dialog-head, .dialog-footer { padding-right: 16px; padding-left: 16px; }
  }
</style>
