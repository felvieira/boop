<script lang="ts">
  import { api, type Device, type PairingToken } from '../lib/api'
  import { relative } from '../lib/format'
  import Card from '../lib/ui/Card.svelte'
  import Button from '../lib/ui/Button.svelte'
  import QR from '../lib/ui/QR.svelte'
  import StatusDot from '../lib/ui/StatusDot.svelte'
  import Notice from '../lib/ui/Notice.svelte'
  import Empty from '../lib/ui/Empty.svelte'
  import CodeBlock from '../lib/ui/CodeBlock.svelte'
  import { panel, soft, pop, reorder } from '../lib/motion'
  import Skeleton from '../lib/ui/Skeleton.svelte'
  import { isSubscribedToPush, subscribeToWebPush, unsubscribeFromWebPush } from '../lib/pwa'

  let { embedded = false, onpaired }: { embedded?: boolean; onpaired?: () => void } = $props()
  let pushSubscribed = $state(false)

  let devices = $state<Device[]>([])
  let loaded = $state(false)
  let pairing = $state<PairingToken | null>(null)
  let error = $state('')
  let now = $state(new Date())
  let showPayload = $state(false)

  async function load() {
    try {
      devices = (await api.devices()).devices
      now = new Date()
    } catch (e: any) {
      error = e.message
    } finally {
      loaded = true
    }
  }

  async function togglePush() {
    error = ''
    try {
      if (pushSubscribed) {
        await unsubscribeFromWebPush()
        pushSubscribed = false
      } else {
        const res = await subscribeToWebPush()
        if (res.success) {
          pushSubscribed = true
          await load()
        } else if (res.error) {
          error = res.error
        }
      }
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : 'Falha na inscrição push'
    }
  }
  async function startPairing() {
    error = ''
    try {
      pairing = await api.createPairing()
    } catch (e: any) {
      error = e.message
    }
  }

  async function cancelPairing() {
    if (!pairing) return
    try {
      await api.revokePairing(pairing.id)
    } catch {
      /* already used or expired */
    }
    pairing = null
  }

  async function remove(d: Device) {
    try {
      await api.deleteDevice(d.id)
      await load()
    } catch (e: any) {
      error = e.message
    }
  }

  $effect(() => {
    load()
    isSubscribedToPush().then((v) => (pushSubscribed = v))
    const t = setInterval(async () => {
      const before = devices.length
      await load()
      if (pairing && devices.length > before) {
        pairing = null
        onpaired?.()
      }
      if (pairing && new Date(pairing.expires_at) < new Date()) pairing = null
    }, 3000)
    return () => clearInterval(t)
  })

  const secondsLeft = $derived(pairing ? Math.max(0, Math.round((new Date(pairing.expires_at).getTime() - now.getTime()) / 1000)) : 0)
  const qrJSON = $derived(pairing?.qr ? JSON.stringify(pairing.qr) : '')
</script>

<div class="stack">
  {#if error}<div transition:panel><Notice tone="bad">{error}</Notice></div>{/if}

  <Card title="PWA / Web Push">
    {#snippet action()}
      {#if pushSubscribed}
        <Button variant="secondary" size="sm" onclick={togglePush}>Desativar Push</Button>
      {:else}
        <Button size="sm" onclick={togglePush}>Ativar Push neste Navegador</Button>
      {/if}
    {/snippet}
    <p class="secondary lead" in:soft>
      Instale o Boop como PWA (no iOS: Safari &gt; Compartilhar &gt; Adicionar à Tela de Início; no Android/Chrome: Instalar App) para receber notificações nativas em segundo plano mesmo com o app fechado.
    </p>
    <div style="margin-top: 10px">
      {#if pushSubscribed}
        <StatusDot tone="ok">Notificações push ativas neste navegador</StatusDot>
      {:else}
        <StatusDot tone="warn">Notificações push desativadas neste navegador</StatusDot>
      {/if}
    </div>
  </Card>

  <Card title="Pair iPhone">
    {#snippet action()}
      {#if pairing}
        <Button variant="secondary" size="sm" onclick={cancelPairing}>Cancel</Button>
      {:else}
        <Button size="sm" onclick={startPairing}>Pair iPhone</Button>
      {/if}
    {/snippet}
    {#if pairing}
      <div class="pair" in:pop>
        <QR data={qrJSON} />
        <div class="pair-text">
          <div class="setting">Scan with the Boop app</div>
          <p class="secondary">Open Boop on your iPhone, tap Pair server, and point the camera at this code.</p>
          <p class="muted caption">Single use · expires in {Math.floor(secondsLeft / 60)}m {secondsLeft % 60}s</p>
          <p class="muted caption">Server address in the code: <span class="mono">{pairing.qr?.server}</span>. If that is not reachable from your phone, set BOOP_BASE_URL.</p>
          <button type="button" class="linkish" onclick={() => (showPayload = !showPayload)}>{showPayload ? 'Hide payload' : 'Show payload'}</button>
        </div>
      </div>
      {#if showPayload}
        <div style="margin-top: 16px" transition:panel><CodeBlock code={qrJSON} wrap /></div>
      {/if}
    {:else}
      <p class="secondary lead" in:soft>Generates a one-time QR code that the Boop iOS app scans to connect to this server. Nothing to type.</p>
    {/if}
  </Card>

  {#if !embedded}
    <Card title="Paired devices" flush>
      {#if !loaded}
        <Skeleton rows={2} />
      {:else if devices.length === 0}
        <Empty title="No devices paired">Pair an iPhone above to start receiving pushes.</Empty>
      {:else}
        <div class="thead caption faint">
          <div>Name</div><div>Push</div><div class="r">Last seen</div><div></div>
        </div>
        {#each devices as d (d.id)}
          <div class="dev" animate:reorder in:soft out:soft>
            <div class="dn">
              <div class="setting">{d.name}</div>
              <div class="caption faint">{d.platform}{d.app_bundle_id ? ` · ${d.app_bundle_id}` : ''} · paired {relative(d.created_at, now)}</div>
            </div>
            <div>
              {#if d.push_registered}
                <StatusDot tone="ok">Registered</StatusDot>
              {:else}
                <StatusDot tone="warn">No token</StatusDot>
              {/if}
            </div>
            <div class="r muted">{d.last_seen_at ? relative(d.last_seen_at, now) : 'Never'}</div>
            <div class="r"><Button variant="danger" size="sm" onclick={() => remove(d)}>Remove</Button></div>
          </div>
        {/each}
      {/if}
    </Card>
  {/if}
</div>

<style>
  .lead { font: var(--up-type-meta); line-height: 1.6; }
  .pair { display: flex; gap: var(--up-space-5); align-items: flex-start; }
  .pair-text { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  .pair-text p { font: var(--up-type-meta); line-height: 1.6; }
  .linkish { background: none; border: none; cursor: pointer; font: var(--up-type-ui); color: var(--up-accent); padding: 0; text-align: left; }
  .linkish:hover { color: var(--up-accent-hover); }
  .thead, .dev { display: grid; grid-template-columns: 1fr 120px 100px 90px; gap: var(--up-space-4); align-items: center; padding: 0 var(--up-space-4); }
  .thead { padding-bottom: 10px; border-bottom: 1px solid var(--up-border-hairline); }
  .dev { padding: 12px var(--up-space-4); border-radius: var(--up-radius-control); transition: background 120ms ease-out; }
  .dev:hover { background: var(--up-bg-hover); }
  .r { text-align: right; justify-self: end; }
  @media (max-width: 600px) {
    .pair { flex-direction: column; }
    .thead { display: none; }
    .dev { grid-template-columns: 1fr auto; }
    .dev > div:nth-child(3) { display: none; }
  }
</style>
