<script lang="ts">
  import { api, setLoginHandler, type Event } from './lib/api'
  import { router, link } from './lib/router.svelte'
  import Tabs from './lib/ui/Tabs.svelte'
  import StatusDot from './lib/ui/StatusDot.svelte'
  import LevelBadge from './lib/ui/LevelBadge.svelte'
  import Button from './lib/ui/Button.svelte'
  import Inbox from './pages/Inbox.svelte'
  import EventDetail from './pages/EventDetail.svelte'
  import Group from './pages/Group.svelte'
  import Projects from './pages/Projects.svelte'
  import Devices from './pages/Devices.svelte'
  import Settings from './pages/Settings.svelte'
  import Setup from './pages/Setup.svelte'
  import Login from './pages/Login.svelte'
  import SupportBoard from './pages/SupportBoard.svelte'
  import { pageIn } from './lib/motion'
  import { playBoopSound, connectLiveEventStream, subscribeToWebPush, isSubscribedToPush, registerServiceWorker } from './lib/pwa'
  let setupCompleted = $state<boolean | null>(null)
  let apnsConfigured = $state<boolean | null>(null)
  let unreachable = $state(false)
  let authRequired = $state(false)
  let needsLogin = $state(false)
  let toastEvent = $state<Event | null>(null)
  let pushSubscribed = $state(false)
  let toastTimer: number | undefined
  setLoginHandler(() => (needsLogin = true))
  async function boot() {
    try {
      const me = await api.me()
      authRequired = me.auth_required
      if (me.auth_required && !me.authenticated) {
        needsLogin = true
        unreachable = false
        return
      }
      needsLogin = false
      const s = await api.status()
      setupCompleted = s.setup_completed
      apnsConfigured = s.apns.configured
      unreachable = false
      if (!s.setup_completed && router.route.name !== 'setup') router.navigate('/setup', true)
    } catch {
      unreachable = true
    }
  }
  $effect(() => {
    boot()
    registerServiceWorker()
    isSubscribedToPush().then((sub) => (pushSubscribed = sub))
    const disconnectStream = connectLiveEventStream((e) => {
      playBoopSound()
      toastEvent = e
      window.clearTimeout(toastTimer)
      toastTimer = window.setTimeout(() => {
        toastEvent = null
      }, 7000)
      window.dispatchEvent(new CustomEvent('boop:new_event', { detail: e }))
    })
    const t = setInterval(boot, 60_000)
    return () => {
      clearInterval(t)
      window.clearTimeout(toastTimer)
      disconnectStream()
    }
  })

  async function enablePush() {
    const res = await subscribeToWebPush()
    if (res.success) {
      pushSubscribed = true
    } else if (res.error) {
      alert(res.error)
    }
  }
  async function signOut() {
    await api.logout().catch(() => {})
    needsLogin = true
  }

  const route = $derived(router.route)
  const tabs = [
    { label: 'Inbox', href: '/' },
    { label: 'Projects', href: '/projects' },
    { label: 'Devices', href: '/devices' },
    { label: 'Settings', href: '/settings' },
    { label: 'Support', href: '/support' },
  ]
  const activeTab = $derived(route.name === 'event' || route.name === 'group' ? '/' : router.path)
</script>

<div class="page">
  <header>
    <a class="brand" href="/" onclick={link}>
      <span class="mark"></span>
      <span class="wordmark">Boop</span>
    </a>
    <div class="right">
      {#if unreachable}
        <StatusDot tone="bad">Server unreachable</StatusDot>
      {:else if needsLogin}
        <span></span>
      {:else}
        {#if pushSubscribed}
          <span class="pwa-tag" title="Recebendo notificações push no navegador">PWA Push ativo</span>
        {:else}
          <button type="button" class="pwa-btn" onclick={enablePush} title="Ativar notificações push neste dispositivo">Ativar Push</button>
        {/if}
        {#if apnsConfigured === false}
          <a href="/settings" onclick={link} class="plain"><StatusDot tone="warn">APNs not configured</StatusDot></a>
        {:else if apnsConfigured}
          <StatusDot tone="ok">APNs ready</StatusDot>
        {/if}
      {/if}
      {#if authRequired && !needsLogin}
        <button type="button" class="signout" onclick={signOut}>Sign out</button>
      {/if}
    </div>
  </header>

  {#if toastEvent}
    <aside class="toast-bar" in:pageIn>
      <button type="button" class="toast-inner" onclick={() => { const id = toastEvent?.id; toastEvent = null; if (id) router.navigate('/events/' + id) }}>
        <LevelBadge level={toastEvent.level} />
        <div class="toast-info">
          <span class="toast-proj">{toastEvent.project_name}</span>
          <span class="toast-title">{toastEvent.title}</span>
          {#if toastEvent.body}
            <span class="toast-body">— {toastEvent.body}</span>
          {/if}
        </div>
        <Button size="sm">Ver</Button>
      </button>
      <button type="button" class="toast-dismiss" onclick={() => (toastEvent = null)}>✕</button>
    </aside>
  {/if}

  {#if needsLogin}
    <Login onsuccess={boot} />
  {:else if route.name === 'setup'}
    <Setup onfinished={() => (setupCompleted = true)} />
  {:else}
    <div class="tabs"><Tabs items={tabs} active={activeTab} /></div>
    {#key router.path}
      <div in:pageIn>
        {#if route.name === 'inbox'}
          <Inbox />
        {:else if route.name === 'event'}
          <EventDetail id={route.params.id} />
        {:else if route.name === 'group'}
          <Group project={route.params.project} fingerprint={route.params.fingerprint} />
        {:else if route.name === 'projects'}
          <Projects />
        {:else if route.name === 'devices'}
          <Devices />
        {:else if route.name === 'settings'}
          <Settings />
        {:else if route.name === 'support'}
          <SupportBoard />
        {:else}
          <p class="muted">Nothing here. <a href="/" onclick={link}>Back to the inbox</a>.</p>
        {/if}
      </div>
    {/key}
  {/if}

  <footer class="caption faint">Boop · self-hosted · <a href="/settings" onclick={link}>status</a></footer>
</div>

<style>
  header { display: flex; align-items: center; justify-content: space-between; padding: 22px 0; }
  .brand { display: inline-flex; align-items: center; gap: 10px; color: var(--up-ink); }
  .brand:hover { color: var(--up-ink); }
  .mark { width: 22px; height: 22px; border-radius: var(--up-radius-pill); background: var(--up-accent); position: relative; }
  .mark::after { content: ''; position: absolute; inset: 7px; border-radius: 50%; background: var(--up-bg); }
  .wordmark { font: var(--up-type-wordmark); letter-spacing: 0.02em; }
  .right { display: flex; align-items: center; gap: 14px; }
  .plain { color: inherit; }
  .pwa-btn {
    background: var(--up-accent);
    color: #fff;
    border: none;
    border-radius: var(--up-radius-pill, 999px);
    font-size: 12px;
    font-weight: 600;
    padding: 4px 10px;
    cursor: pointer;
    transition: opacity 0.15s;
  }
  .pwa-btn:hover { opacity: 0.88; }
  .pwa-tag {
    font-size: 11px;
    font-weight: 500;
    color: var(--up-accent);
    background: rgba(124, 131, 232, 0.12);
    padding: 3px 8px;
    border-radius: var(--up-radius-pill, 999px);
  }
  .toast-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    background: var(--up-surface);
    border: 1px solid var(--up-border-hairline);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.08);
    border-radius: var(--up-radius-md, 8px);
    padding: 10px 14px;
    margin-bottom: var(--up-space-4);
  }
  .toast-inner {
    display: flex;
    align-items: center;
    gap: 12px;
    flex: 1;
    cursor: pointer;
    min-width: 0;
    background: none;
    border: none;
    padding: 0;
    text-align: left;
    font: inherit;
  }
  .toast-info {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    font-size: 13px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .toast-proj { font-weight: 600; color: var(--up-ink); }
  .toast-title { color: var(--up-ink); }
  .toast-body { color: var(--up-text-muted); text-overflow: ellipsis; overflow: hidden; }
  .toast-dismiss {
    background: none;
    border: none;
    color: var(--up-text-muted);
    font-size: 14px;
    cursor: pointer;
    padding: 4px;
  }
  .toast-dismiss:hover { color: var(--up-ink); }
  .signout { background: none; border: none; cursor: pointer; padding: 0; font: var(--up-type-ui); color: var(--up-text-muted); }
  .signout:hover { color: var(--up-ink); }
  .tabs { margin-bottom: var(--up-space-5); }
  footer { margin-top: var(--up-space-6); padding-top: var(--up-space-4); border-top: 1px solid var(--up-border-hairline); }
</style>
