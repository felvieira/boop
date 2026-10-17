<script lang="ts">
import { api, type Status, type Settings, type SettingsPatch, type Project, type Delivery, type Silence, type SilenceField, type OpenRouterModel, type SupportTriagePreview } from '../lib/api'
  import { relative, duration, retentionLabel, compact } from '../lib/format'
  import Card from '../lib/ui/Card.svelte'
  import Button from '../lib/ui/Button.svelte'
  import Select from '../lib/ui/Select.svelte'
  import Input from '../lib/ui/Input.svelte'
  import Switch from '../lib/ui/Switch.svelte'
  import SettingRow from '../lib/ui/SettingRow.svelte'
  import StatusDot from '../lib/ui/StatusDot.svelte'
  import Notice from '../lib/ui/Notice.svelte'
  import CodeBlock from '../lib/ui/CodeBlock.svelte'
  import Metric from '../lib/ui/Metric.svelte'
  import { panel, pop, reorder, soft } from '../lib/motion'
  import Skeleton from '../lib/ui/Skeleton.svelte'
  import { link } from '../lib/router.svelte'

  let status = $state<Status | null>(null)
  let settings = $state<Settings | null>(null)
  let projects = $state<Project[]>([])
  let error = $state('')
  let aiModels = $state<OpenRouterModel[]>([])
  let aiModelsLoading = $state(false)
  let aiModelError = $state('')
  let aiKeyDraft = $state('')
  let githubTokenDraft = $state('')
  let githubRepoMappings = $state<Record<string, string>>({})
  let githubProductDraft = $state('')
  let githubRepositoryDraft = $state('')
  let aiModelDraft = $state('')
  let aiModelSearch = $state('')
  let aiSaving = $state(false)
  let aiNotice = $state('')
  let githubSaving = $state(false)
  let githubRepoSaving = $state(false)
  let githubNotice = $state('')
  let triagePromptDraft = $state('')
  let triageSaving = $state(false)
  let triageTesting = $state(false)
  let triagePreview = $state<SupportTriagePreview | null>(null)
  let digestSaving = $state(false)
  let testing = $state(false)
  let testResult = $state<{ deliveries: Delivery[]; apns_configured: boolean } | null>(null)
  let newKey = $state('')
  let testProject = $state('')
  let silences = $state<Silence[]>([])
  let silencedEvents = $state(0)
  let silField = $state<SilenceField>('title')
  let silValue = $state('')
  let silProject = $state('')
  let silNote = $state('')

  async function loadSilences() {
    try {
      const r = await api.silences()
      silences = r.silences
      silencedEvents = r.silenced_events
    } catch (e: any) {
      error = e.message
    }
  }

  async function addSilence() {
    if (!silValue.trim()) return
    try {
      await api.createSilence({ field: silField, value: silValue.trim(), project_id: silProject || undefined, note: silNote.trim() || undefined })
      silValue = ''
      silNote = ''
      await loadSilences()
    } catch (e: any) {
      error = e.message
    }
  }

  async function removeSilence(s: Silence) {
    try {
      await api.deleteSilence(s.id)
      silences = silences.filter((x) => x.id !== s.id)
    } catch (e: any) {
      error = e.message
    }
  }

  async function load() {
    try {
      ;[status, settings] = await Promise.all([api.status(), api.settings()])
      aiModelDraft = settings.openrouter_model
      triagePromptDraft = settings.support_triage_instructions
      githubRepoMappings = { ...(settings.github_repositories ?? {}) }
      projects = (await api.projects()).projects
      await Promise.all([loadSilences(), loadOpenRouterModels()])
    } catch (e: any) {
      error = e.message
    }
  }
  $effect(() => {
    load()
  })

  async function setMCP(v: boolean) {
    try {
      settings = await api.updateSettings({ mcp_enabled: v })
    } catch (e: any) {
      error = e.message
    }
  }

  async function setRetention(v: string) {
    try {
      settings = await api.updateSettings({ retention_days: Number(v) })
    } catch (e: any) {
      error = e.message
    }
  }

  async function loadOpenRouterModels() {
    aiModelsLoading = true
    aiModelError = ''
    try {
      aiModels = (await api.openRouterModels()).models
    } catch (e: any) {
      aiModelError = e.message
    } finally {
      aiModelsLoading = false
    }
  }

  async function saveAISettings() {
    if (!settings || !aiModelDraft) return
    aiSaving = true
    aiNotice = ''
    error = ''
    const key = aiKeyDraft.trim()
    const patch: SettingsPatch = { openrouter_model: aiModelDraft }
    if (key) patch.openrouter_api_key = key
    try {
      settings = await api.updateSettings(patch)
      aiKeyDraft = ''
      aiNotice = key ? 'OpenRouter key validated and saved. The worker will use it on the next triage cycle.' : 'Model saved. The worker will use it on the next triage cycle.'
    } catch (e: any) {
      error = e.message
    } finally {
      aiSaving = false
    }
  }

  async function clearAIKey() {
    aiSaving = true
    aiNotice = ''
    error = ''
    try {
      settings = await api.updateSettings({ clear_openrouter_api_key: true })
      aiKeyDraft = ''
      aiNotice = settings.openrouter_api_key_set
        ? 'Saved key removed. Boop is still using the OpenRouter key from its environment.'
        : 'Saved key removed. Add an OpenRouter key to resume triage.'
    } catch (e: any) {
      error = e.message
    } finally {
      aiSaving = false
    }
  }

  async function saveGitHubToken() {
    const token = githubTokenDraft.trim()
    if (!token) return
    githubSaving = true
    githubNotice = ''
    error = ''
    try {
      settings = await api.updateSettings({ github_token: token })
      githubTokenDraft = ''
      githubNotice = 'GitHub token validated and saved encrypted. Issue drafts still require a separate approval on each card.'
    } catch (e: any) {
      error = e.message
    } finally {
      githubSaving = false
    }
  }

  async function clearGitHubToken() {
    githubSaving = true
    githubNotice = ''
    error = ''
    try {
      settings = await api.updateSettings({ clear_github_token: true })
      githubTokenDraft = ''
      githubNotice = 'Saved GitHub token removed. Existing drafts remain available for review.'
    } catch (e: any) {
      error = e.message
    } finally {
      githubSaving = false
    }
  }

  async function saveGitHubRepoMappings(next: Record<string, string>) {
    githubRepoSaving = true
    githubNotice = ''
    error = ''
    try {
      settings = await api.updateSettings({ github_repositories: next })
      githubRepoMappings = { ...settings.github_repositories }
      githubProductDraft = ''
      githubRepositoryDraft = ''
      githubNotice = 'Product-to-repository routing saved.'
    } catch (e: any) {
      error = e.message
    } finally {
      githubRepoSaving = false
    }
  }

  async function addGitHubRepoMapping() {
    const product = githubProductDraft.trim()
    const repository = githubRepositoryDraft.trim()
    if (!product || !repository) return
    const next = { ...githubRepoMappings }
    const existing = Object.keys(next).find((name) => name.toLowerCase() === product.toLowerCase())
    if (existing) delete next[existing]
    next[product] = repository
    await saveGitHubRepoMappings(next)
  }

  async function removeGitHubRepoMapping(product: string) {
    const next = { ...githubRepoMappings }
    delete next[product]
    await saveGitHubRepoMappings(next)
  }

  async function saveTriageInstructions() {
    triageSaving = true
    aiNotice = ''
    error = ''
    try {
      settings = await api.updateSettings({ support_triage_instructions: triagePromptDraft.trim() })
      triagePromptDraft = settings.support_triage_instructions
      aiNotice = 'Triage instructions saved. The worker will use them on the next run.'
    } catch (e: any) {
      error = e.message
    } finally {
      triageSaving = false
    }
  }

  async function restoreDefaultTriageInstructions() {
    triagePromptDraft = ''
    await saveTriageInstructions()
  }

  async function previewTriage() {
    triageTesting = true
    triagePreview = null
    error = ''
    try {
      triagePreview = await api.previewSupportTriage()
    } catch (e: any) {
      error = e.message
    } finally {
      triageTesting = false
    }
  }

  async function setDigestEnabled(enabled: boolean) {
    digestSaving = true
    error = ''
    try {
      settings = await api.updateSettings({ support_digest_enabled: enabled })
    } catch (e: any) {
      error = e.message
    } finally {
      digestSaving = false
    }
  }

  async function setDigestHour(hour: string) {
    digestSaving = true
    error = ''
    try {
      settings = await api.updateSettings({ support_digest_hour: Number(hour) })
    } catch (e: any) {
      error = e.message
    } finally {
      digestSaving = false
    }
  }

  async function addKey() {
    const k = newKey.trim()
    if (!k || !settings) return
    try {
      settings = await api.updateSettings({ redact_keys: [...settings.redact_keys, k] })
      newKey = ''
    } catch (e: any) {
      error = e.message
    }
  }

  async function removeKey(k: string) {
    if (!settings) return
    try {
      settings = await api.updateSettings({ redact_keys: settings.redact_keys.filter((x) => x !== k) })
    } catch (e: any) {
      error = e.message
    }
  }

  async function sendTest() {
    testing = true
    error = ''
    testResult = null
    try {
      testResult = await api.test(testProject || undefined)
      await load()
    } catch (e: any) {
      error = e.message
    } finally {
      testing = false
    }
  }

  const retentionOptions = [
    { value: '7', label: '7 days' },
    { value: '14', label: '14 days' },
    { value: '30', label: '30 days' },
    { value: '90', label: '90 days' },
    { value: '0', label: 'Unlimited' },
  ]
  const filteredAIModels = $derived(aiModels.filter((model) => {
    const query = aiModelSearch.trim().toLowerCase()
    return !query || `${model.name} ${model.id}`.toLowerCase().includes(query)
  }))
  const aiModelOptions = $derived.by(() => {
    const options = filteredAIModels.map((model) => ({
      value: model.id,
      label: `${model.name} · ${model.id}`,
    }))
    if (aiModelDraft && !options.some((option) => option.value === aiModelDraft)) {
      options.unshift({ value: aiModelDraft, label: `Current: ${aiModelDraft}` })
    }
    return options
  })
  const selectedAIModel = $derived(aiModels.find((model) => model.id === aiModelDraft) ?? null)
  function modelPrice(value: number) {
    return `$${value.toLocaleString('en-US', { maximumFractionDigits: 4 })}`
  }
  const lastPush = $derived(status?.last_push ?? null)
  const origin = typeof location !== 'undefined' ? location.origin : ''
</script>

<div class="stack">
  {#if error}<div transition:panel><Notice tone="bad">{error}</Notice></div>{/if}

  {#if !status && !error}
    <div class="metrics">
      {#each [0, 1, 2, 3] as i (i)}
        <Card><Skeleton lines={2} height={12} widths={['50%', '35%']} /></Card>
      {/each}
    </div>
    <Card><Skeleton lines={1} height={13} width="20%" /><div class="status" style="margin-top: 16px">{#each [0, 1, 2, 3, 4, 5] as i (i)}<Skeleton lines={2} height={11} widths={['40%', '70%']} />{/each}</div></Card>
    <Card><Skeleton lines={3} height={12} widths={['35%', '80%', '60%']} /></Card>
  {/if}

  {#if status}
    <div class="metrics" in:soft>
      <Card><Metric label="Events" value={compact(status.events)} /></Card>
      <Card><Metric label="Projects" value={String(status.projects)} /></Card>
      <Card><Metric label="Devices" value={String(status.devices)} delta={status.pushable_devices < status.devices ? `${status.pushable_devices} with push` : undefined} tone="neutral" /></Card>
      <Card><Metric label="Uptime" value={duration(status.uptime_seconds)} /></Card>
    </div>

    <Card title="Status">
      <div class="status">
        <div><span class="k">Server</span><StatusDot tone="ok">Healthy</StatusDot></div>
        <div><span class="k">Database</span>{#if status.database === 'ok'}<StatusDot tone="ok">Healthy</StatusDot>{:else}<StatusDot tone="bad">{status.database}</StatusDot>{/if}</div>
        <div>
          <span class="k">APNs</span>
          {#if status.apns.configured}
            <StatusDot tone="ok">Configured · {status.apns.environment}</StatusDot>
          {:else}
            <StatusDot tone="warn">Not configured</StatusDot>
          {/if}
        </div>
        <div>
          <span class="k">Last push</span>
          {#if lastPush}
            <StatusDot tone={lastPush.status === 'sent' ? 'ok' : lastPush.status === 'failed' ? 'bad' : 'muted'}>
              {lastPush.status === 'sent' ? 'Successful' : lastPush.status === 'failed' ? 'Failed' : 'Skipped'} {relative(lastPush.attempted_at)}
            </StatusDot>
          {:else}
            <span class="muted">None yet</span>
          {/if}
        </div>
        <div><span class="k">Version</span><span>{status.version}</span></div>
        <div><span class="k">Database path</span><span class="mono">{status.database_path}</span></div>
        <div><span class="k">Base URL</span><span class="mono">{status.base_url}</span></div>
        <div><span class="k">Retention</span><span>{retentionLabel(status.retention_days)}</span></div>
        <div>
          <span class="k">Admin login</span>
          {#if status.admin_auth}<StatusDot tone="ok">Enabled</StatusDot>{:else}<StatusDot tone="warn">Off · set BOOP_ADMIN_USER and BOOP_ADMIN_PASSWORD</StatusDot>{/if}
        </div>
      </div>
      {#if !status.apns.configured}
        <div style="margin-top: 16px">
          <Notice tone="warn">
            Pushes are stored but not sent. {status.apns.error ? status.apns.error + '.' : ''} Set the APNS_* environment variables and restart Boop.
          </Notice>
        </div>
      {:else if lastPush?.status === 'failed' && lastPush.error}
        <div style="margin-top: 16px"><Notice tone="bad">Last push failed: {lastPush.error}</Notice></div>
      {/if}
    </Card>

    <Card title="Apple Push Notifications">
      <div class="status">
        <div><span class="k">Team id</span><span class="mono">{status.apns.team_id || '—'}</span></div>
        <div><span class="k">Key id</span><span class="mono">{status.apns.key_id || '—'}</span></div>
        <div><span class="k">Bundle id</span><span class="mono">{status.apns.bundle_id || '—'}</span></div>
        <div><span class="k">Environment</span><span>{status.apns.environment}</span></div>
      </div>
      {#if !status.apns.configured}
        <p class="secondary lead" style="margin-top: 16px">Add these to your container environment. The private key should be mounted as a file, not pasted into an environment variable.</p>
        <CodeBlock code={`APNS_TEAM_ID=YOUR_TEAM_ID\nAPNS_KEY_ID=YOUR_KEY_ID\nAPNS_BUNDLE_ID=com.example.Boop\nAPNS_PRIVATE_KEY_PATH=/run/secrets/apns.p8\nAPNS_ENVIRONMENT=production`} />
      {/if}
    </Card>

    <Card title="Test Boop">
      <p class="secondary lead">Creates a test event and pushes it to every paired phone.</p>
      <div class="row" style="margin-top: 12px; flex-wrap: wrap">
        <Select bind:value={testProject} options={[{ value: '', label: projects.length ? `Project: ${projects[projects.length - 1]?.name}` : 'No project yet' }, ...projects.map((p) => ({ value: p.id, label: p.name }))]} style="width: 220px" aria-label="Project" />
        <Button onclick={sendTest} disabled={testing || projects.length === 0}>{testing ? 'Sending' : 'Send test notification'}</Button>
      </div>
      {#if testResult}
        <div style="margin-top: 16px" transition:panel>
          {#if testResult.deliveries.length === 0}
            <Notice tone="info">Event created. No paired phones with push registered, so nothing was sent.</Notice>
          {:else if !testResult.apns_configured}
            <Notice tone="warn">Event created, but APNs is not configured so {testResult.deliveries.length} delivery{testResult.deliveries.length === 1 ? ' was' : 'ies were'} skipped.</Notice>
          {:else if testResult.deliveries.every((d) => d.status === 'sent')}
            <Notice tone="good">Sent to {testResult.deliveries.length} device{testResult.deliveries.length === 1 ? '' : 's'}.</Notice>
          {:else}
            <Notice tone="bad">
              {#each testResult.deliveries.filter((d) => d.status !== 'sent') as d (d.id)}
                <div>{d.device_name}: {d.error}</div>
              {/each}
            </Notice>
          {/if}
        </div>
      {/if}
    </Card>
  {/if}

  {#if settings}
    <Card title="AI support triage">
      <p class="secondary lead">One OpenRouter key and model are used to review notifications from every Boop project. Saving a key validates authentication only; it does not run a model completion.</p>
      <div class="ai-fields">
        <label class="ai-field">
          <span class="k">OpenRouter API key</span>
          <Input type="password" bind:value={aiKeyDraft} placeholder={settings.openrouter_api_key_source === 'settings' ? 'Saved securely · leave blank to keep it' : settings.openrouter_api_key_source === 'environment' ? 'Set in deployment environment · leave blank to keep it' : 'Paste a rotated OpenRouter key'} autocomplete="new-password" autocapitalize="none" spellcheck="false" aria-label="OpenRouter API key" />
          <span class="muted caption">
            {#if settings.openrouter_api_key_error}
              The saved key could not be decrypted. Enter a replacement key or remove the saved key to recover.
            {:else if settings.openrouter_api_key_set}
              Key configured via {settings.openrouter_api_key_source === 'environment' ? 'deployment environment' : 'Settings'}; Boop never sends it back to the browser.
            {:else}
              The key is encrypted on the server and is never stored in browser storage.
            {/if}
          </span>
        </label>
        <div class="ai-field">
          <label class="k" for="ai-model-search">Find a model</label>
          <Input id="ai-model-search" bind:value={aiModelSearch} placeholder="Search model or provider" aria-label="Search OpenRouter models" />
          {#if aiModels.length > 0}
            <Select bind:value={aiModelDraft} options={aiModelOptions} style="width: 100%" aria-label="OpenRouter model" />
          {:else}
            <Input bind:value={aiModelDraft} placeholder="provider/model-id" aria-label="OpenRouter model id" mono />
          {/if}
          {#if selectedAIModel && selectedAIModel.input_price_per_million !== null && selectedAIModel.output_price_per_million !== null}
            <span class="muted caption">Per 1M tokens: {modelPrice(selectedAIModel.input_price_per_million)} input · {modelPrice(selectedAIModel.output_price_per_million)} output</span>
          {:else if selectedAIModel}
            <span class="muted caption">Published prices are unavailable for this model.</span>
          {:else}
            <span class="muted caption">Current model: {aiModelDraft || 'not selected'}</span>
          {/if}
          {#if aiModelsLoading}<span class="muted caption">Loading model catalog…</span>{/if}
          {#if aiModelError}<span class="model-error">Could not load OpenRouter models. You can enter a provider/model id above.</span>{/if}
        </div>
      </div>
      <p class="muted caption ai-cost">Saving a key starts reviewing all queued and future notifications. OpenRouter may charge for model usage at the selected model’s published token prices.</p>
      {#if aiNotice}<div class="ai-notice" aria-live="polite"><Notice tone="good">{aiNotice}</Notice></div>{/if}
      <div class="row ai-actions">
        <Button onclick={saveAISettings} disabled={aiSaving || !aiModelDraft}>{aiSaving ? 'Saving…' : 'Save AI settings'}</Button>
        {#if settings.openrouter_api_key_source === 'settings'}
          <Button variant="danger" onclick={clearAIKey} disabled={aiSaving}>Remove saved key</Button>
        {/if}
      </div>
      <div class="triage-config">
        <label class="ai-field" for="triage-instructions">
          <span class="k">Global triage instructions</span>
          <textarea id="triage-instructions" class="triage-prompt" bind:value={triagePromptDraft} maxlength="4000" rows="5" placeholder="Optional guidance for how Boop should recognize useful customer contacts and classify them."></textarea>
          <span class="muted caption">These instructions guide all projects. The built-in privacy and output rules remain in force. Leave blank to use the default.</span>
        </label>
        <div class="row ai-actions">
          <Button variant="secondary" onclick={saveTriageInstructions} disabled={triageSaving}>{triageSaving ? 'Saving…' : 'Save instructions'}</Button>
          <Button variant="secondary" onclick={restoreDefaultTriageInstructions} disabled={triageSaving || !triagePromptDraft.trim()}>Restore default</Button>
          <Button variant="secondary" onclick={previewTriage} disabled={triageTesting || !settings.openrouter_api_key_set}>{triageTesting ? 'Previewing…' : 'Preview with synthetic data'}</Button>
        </div>
        {#if triagePreview}
          <div class="triage-preview" aria-live="polite">
            <div class="row"><strong>{triagePreview.include ? 'Would create a card' : 'Would filter this notification'}</strong><span class="muted caption">{triagePreview.model} · {triagePreview.category} · {triagePreview.priority} · {Math.round(triagePreview.confidence * 100)}% confidence · prompt {triagePreview.prompt_version}</span></div>
            <strong>{triagePreview.title}</strong>
            <p>{triagePreview.summary}</p>
            <span class="muted caption">{triagePreview.reason}</span>
            <span class="muted caption">Preview usage: {triagePreview.usage.total_tokens} tokens{triagePreview.usage.cost === null ? ' · provider did not report cost' : ` · $${triagePreview.usage.cost.toFixed(6)}`}. Synthetic data only; no card was created.</span>
          </div>
        {/if}
      </div>
    </Card>

    <Card title="GitHub issue approvals">
      <p class="secondary lead">Boop can create a GitHub issue from a support card only after an administrator reviews and approves its destination, title, and body.</p>
      <label class="ai-field">
        <span class="k">GitHub personal access token</span>
        <Input type="password" bind:value={githubTokenDraft} placeholder={settings.github_token_set ? 'Saved securely · leave blank to keep it' : 'Paste a token with Issues read/write access'} autocomplete="new-password" autocapitalize="none" spellcheck="false" aria-label="GitHub personal access token" />
        <span class="muted caption">
          {#if settings.github_token_error}
            The saved token could not be decrypted. Enter a replacement token or remove the saved token to recover.
          {:else if settings.github_token_set}
            Token validated against GitHub, encrypted on this server, and never returned to the browser. Select the repository on each issue draft.
          {:else}
            Give the token Issues read/write access only to the repositories you plan to use. Boop validates it before saving.
          {/if}
        </span>
      </label>
      {#if githubNotice}<div class="ai-notice" aria-live="polite"><Notice tone="good">{githubNotice}</Notice></div>{/if}
      <div class="row ai-actions">
        <Button onclick={saveGitHubToken} disabled={githubSaving || !githubTokenDraft.trim()}>{githubSaving ? 'Saving…' : 'Save GitHub token'}</Button>
        {#if settings.github_token_set}<Button variant="danger" onclick={clearGitHubToken} disabled={githubSaving}>Remove saved token</Button>{/if}
      </div>
      <div class="repo-routing">
        <div><strong>Product routing</strong><p class="muted caption">New issue drafts use this destination when the product name matches. The card still requires approval.</p></div>
        {#if Object.keys(githubRepoMappings).length}
          <div class="repo-mappings">
            {#each Object.entries(githubRepoMappings).sort(([a], [b]) => a.localeCompare(b)) as [product, repository] (product)}
              <div class="repo-mapping"><span>{product}</span><code>{repository}</code><Button variant="danger" size="sm" onclick={() => removeGitHubRepoMapping(product)} disabled={githubRepoSaving}>Remove</Button></div>
            {/each}
          </div>
        {/if}
        <div class="repo-form">
          <label class="ai-field"><span class="k">Boop product</span><Input bind:value={githubProductDraft} maxlength={200} placeholder="VisaLab" aria-label="Boop product name" /></label>
          <label class="ai-field"><span class="k">GitHub repository</span><Input bind:value={githubRepositoryDraft} placeholder="felvieira/visalab" aria-label="GitHub repository mapping" /></label>
          <Button variant="secondary" onclick={addGitHubRepoMapping} disabled={githubRepoSaving || !githubProductDraft.trim() || !githubRepositoryDraft.trim()}>{githubRepoSaving ? 'Saving…' : 'Save mapping'}</Button>
        </div>
      </div>
    </Card>

    <Card title="Daily support digest">
      <p class="secondary lead">A short daily push summarizes new or updated support cards and highlights urgent, blocked, overdue, and partnership items. It is calculated from the board without an AI call.</p>
      <SettingRow label="Send digest" hint="The digest is sent to paired devices with push enabled. Partial failures retry without repeating deliveries that already succeeded. Time zone: America/Sao_Paulo.">
        <Switch checked={settings.support_digest_enabled} disabled={digestSaving} onchange={setDigestEnabled} label="Daily support digest" />
      </SettingRow>
      <SettingRow label="Delivery time" hint="Choose the hour when the daily digest may be sent.">
        <Select value={String(settings.support_digest_hour)} options={Array.from({ length: 24 }, (_, hour) => ({ value: String(hour), label: `${String(hour).padStart(2, '0')}:00` }))} onchange={(e) => setDigestHour((e.currentTarget as HTMLSelectElement).value)} style="width: 120px" />
      </SettingRow>
    </Card>

    <Card title="Retention">
      <SettingRow label="Keep events for" hint="Older events are deleted automatically once an hour. Unlimited keeps everything. If BOOP_RETENTION_DAYS is set in the environment it overrides this on restart.">
        <Select value={String(settings.retention_days)} options={retentionOptions.some((o) => o.value === String(settings!.retention_days)) ? retentionOptions : [...retentionOptions, { value: String(settings.retention_days), label: `${settings.retention_days} days` }]} onchange={(e) => setRetention((e.currentTarget as HTMLSelectElement).value)} style="width: 150px" />
      </SettingRow>
    </Card>

    <Card title="MCP">
      <p class="secondary lead">Lets the AI assistant you already use read your events over the Model Context Protocol (read-only: list, search and inspect events and projects). Endpoint: <span class="mono">{status?.base_url ?? origin}/mcp</span>.</p>
      <SettingRow label="MCP endpoint" hint={settings.mcp_token_set ? 'Connect with the BOOP_MCP_TOKEN bearer token, a device credential, or your admin login.' : 'No BOOP_MCP_TOKEN is set: device credentials and the admin login work; set the env var to give an agent its own token.'}>
        <Switch checked={settings.mcp_enabled} onchange={setMCP} label="MCP endpoint" />
      </SettingRow>
    </Card>

    <Card title="Silences">
      <p class="secondary lead">Events matching a rule are still stored and shown, but never pushed to a phone. Add rules from an event's page or here. Fingerprint and source match exactly; title ignores case.</p>
      <p class="lead" style="margin-top: 8px"><a href="/?silenced=true" onclick={link}>{silencedEvents} silenced event{silencedEvents === 1 ? '' : 's'}</a> · open one to unsilence it or push it now.</p>
      {#if silences.length === 0}
        <p class="muted" style="margin-top: 12px">No silences.</p>
      {:else}
        <div class="sils" style="margin-top: 12px">
          {#each silences as s (s.id)}
            <div class="sil" animate:reorder out:soft>
              <div class="sil-main">
                <span class="pill custom">{s.field}</span>
                <span class="mono sv">{s.value}</span>
                <span class="muted caption">· {s.project_name || 'every project'}{s.note ? ` · ${s.note}` : ''} · {relative(s.created_at)}</span>
              </div>
              <Button variant="danger" size="sm" onclick={() => removeSilence(s)}>Remove</Button>
            </div>
          {/each}
        </div>
      {/if}
      <form
        class="sil-form"
        onsubmit={(e) => {
          e.preventDefault()
          addSilence()
        }}
      >
        <Select bind:value={silField} options={[{ value: 'title', label: 'Title' }, { value: 'fingerprint', label: 'Fingerprint' }, { value: 'source', label: 'Source' }]} style="width: 130px" aria-label="Field" />
        <Input bind:value={silValue} placeholder="Value to match" aria-label="Value" mono />
        <Select bind:value={silProject} options={[{ value: '', label: 'Every project' }, ...projects.map((p) => ({ value: p.id, label: p.name }))]} style="width: 160px" aria-label="Project" />
        <Input bind:value={silNote} placeholder="Note (optional)" aria-label="Note" style="width: 180px" />
        <Button variant="secondary" type="submit" disabled={!silValue.trim()}>Add</Button>
      </form>
    </Card>

    <Card title="Redaction">
      <p class="secondary lead">Values under these keys are replaced with [REDACTED] anywhere in event data before it is stored. Matching ignores case and treats - and _ the same.</p>
      <div class="keys" style="margin-top: 12px">
        {#each settings.default_redact_keys as k (k)}
          <span class="pill muted">{k}</span>
        {/each}
        {#each settings.redact_keys as k (k)}
          <span class="pill custom" in:pop out:soft animate:reorder>{k}<button type="button" aria-label="Remove {k}" onclick={() => removeKey(k)}>×</button></span>
        {/each}
      </div>
      <form
        class="row"
        style="margin-top: 16px"
        onsubmit={(e) => {
          e.preventDefault()
          addKey()
        }}
      >
        <Input bind:value={newKey} placeholder="Add a key, e.g. ssn" aria-label="Redaction key" style="width: 220px" mono />
        <Button variant="secondary" type="submit" disabled={!newKey.trim()}>Add</Button>
      </form>
    </Card>
  {/if}
</div>

<style>
  .metrics { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--up-space-4); }
  .status { display: grid; grid-template-columns: 1fr 1fr; gap: 12px 24px; }
  .status > div { display: flex; flex-direction: column; gap: 3px; font: var(--up-type-meta); min-width: 0; }
  .status span:last-child { overflow: hidden; text-overflow: ellipsis; }
  .k { font: var(--up-type-caption); color: var(--up-text-muted); }
  .lead { font: var(--up-type-meta); line-height: 1.6; }
  .keys { display: flex; flex-wrap: wrap; gap: 8px; }
  .ai-fields { display: grid; grid-template-columns: 1fr 1fr; gap: var(--up-space-4); margin-top: 16px; }
  .ai-field { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  .ai-field > :global(input), .ai-field > :global(span.wrap) { width: 100%; }
  .ai-cost { margin-top: 12px; }
  .ai-actions { margin-top: 14px; flex-wrap: wrap; }
  .ai-notice { margin-top: 12px; }
  .repo-routing { display: grid; gap: 12px; margin-top: 20px; padding-top: 18px; border-top: 1px solid var(--up-border-hairline); }
  .repo-routing strong { font: var(--up-type-ui); color: var(--up-text); }
  .repo-mappings { display: grid; gap: 6px; }
  .repo-mapping { display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: var(--up-radius-control); background: var(--up-bg-hover); font: var(--up-type-meta); }
  .repo-mapping span { flex: 1; }
  .repo-mapping code { color: var(--up-text-muted); }
  .repo-form { display: flex; gap: 10px; align-items: end; flex-wrap: wrap; }
  .repo-form .ai-field { flex: 1; min-width: 180px; }
  .triage-config { display: flex; flex-direction: column; gap: 8px; margin-top: 20px; padding-top: 18px; border-top: 1px solid var(--up-border-hairline); }
  .triage-prompt { width: 100%; resize: vertical; border: 1px solid var(--up-border-control); border-radius: var(--up-radius-control); background: var(--up-bg); color: var(--up-text); padding: 10px 12px; font: var(--up-type-meta); line-height: 1.5; }
  .triage-preview { display: flex; flex-direction: column; gap: 8px; margin-top: 8px; padding: 14px; border-radius: var(--up-radius-card); background: var(--up-bg-hover); font: var(--up-type-meta); }
  .triage-preview p { margin: 0; }
  .model-error { color: var(--up-outage-strong); font: var(--up-type-caption); }
  .sils { display: flex; flex-direction: column; gap: 6px; }
  .sil { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 10px; border-radius: var(--up-radius-control); transition: background 120ms ease-out; }
  .sil:hover { background: var(--up-bg-hover); }
  .sil-main { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; font: var(--up-type-meta); }
  .sv { word-break: break-all; }
  .sil-form { display: flex; gap: 8px; margin-top: 16px; flex-wrap: wrap; align-items: center; }
  .sil-form :global(input:nth-of-type(1)) { flex: 1; min-width: 160px; }
  .pill { display: inline-flex; align-items: center; gap: 6px; font: var(--up-type-code); padding: 4px 10px; border-radius: var(--up-radius-pill); background: var(--up-bg-hover); box-shadow: var(--up-ring-inset); }
  .pill.custom { background: var(--up-accent-tint); color: var(--up-accent-hover); }
  .pill button { background: none; border: none; cursor: pointer; color: inherit; font-size: 14px; line-height: 1; padding: 0; }
  @media (max-width: 600px) {
    .metrics { grid-template-columns: 1fr 1fr; }
    .status { grid-template-columns: 1fr; }
    .ai-fields { grid-template-columns: 1fr; }
  }
</style>
