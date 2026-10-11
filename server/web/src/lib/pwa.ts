// PWA utilities: Service Worker, Web Push subscriptions, audio chimes, and live SSE streaming.

import { api, type Event } from './api'

/** Convert URL-safe base64 string to Uint8Array for PushManager applicationServerKey. */
function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const rawData = atob(base64)
  const outputArray = new Uint8Array(rawData.length)
  for (let i = 0; i < rawData.length; ++i) {
    outputArray[i] = rawData.charCodeAt(i)
  }
  return outputArray
}

let audioCtx: AudioContext | null = null

/** Plays a soft, gentle synthetic chime using Web Audio API (zero audio files needed). */
export function playBoopSound() {
  try {
    if (!audioCtx) {
      let ContextConstructor: typeof AudioContext | undefined = window.AudioContext
      if (!ContextConstructor && 'webkitAudioContext' in window) {
        const candidate = (window as Record<string, unknown>).webkitAudioContext
        if (typeof candidate === 'function') {
          ContextConstructor = candidate as unknown as typeof AudioContext
        }
      }
      if (!ContextConstructor) return
      audioCtx = new ContextConstructor()
    }

    const t = audioCtx.currentTime
    const osc = audioCtx.createOscillator()
    const gain = audioCtx.createGain()

    osc.type = 'sine'
    // Pitch ramp: start at C5 (523Hz) and shift up to E5 (659Hz)
    osc.frequency.setValueAtTime(523.25, t)
    osc.frequency.exponentialRampToValueAtTime(659.25, t + 0.08)

    // Gentle envelope: quick attack, smooth exponential decay
    gain.gain.setValueAtTime(0.001, t)
    gain.gain.exponentialRampToValueAtTime(0.25, t + 0.02)
    gain.gain.exponentialRampToValueAtTime(0.001, t + 0.28)

    osc.connect(gain)
    gain.connect(audioCtx.destination)

    osc.start(t)
    osc.stop(t + 0.3)
  } catch {
    /* AudioContext not allowed before user gesture or unsupported */
  }
}

export async function registerServiceWorker(): Promise<ServiceWorkerRegistration | null> {
  if (typeof window === 'undefined' || !('serviceWorker' in navigator)) return null
  try {
    return await navigator.serviceWorker.register('/sw.js', { scope: '/' })
  } catch (err) {
    console.warn('[PWA] Service worker registration failed:', err)
    return null
  }
}

export async function isSubscribedToPush(): Promise<boolean> {
  if (typeof window === 'undefined' || !('serviceWorker' in navigator) || !('PushManager' in window)) return false
  try {
    const reg = await navigator.serviceWorker.getRegistration()
    if (!reg) return false
    const sub = await reg.pushManager.getSubscription()
    return !!sub
  } catch {
    return false
  }
}

export async function subscribeToWebPush(): Promise<{ success: boolean; error?: string }> {
  if (typeof window === 'undefined' || !('serviceWorker' in navigator) || !('PushManager' in window)) {
    return { success: false, error: 'Push notifications are not supported in this browser' }
  }

  try {
    const permission = await Notification.requestPermission()
    if (permission !== 'granted') {
      return { success: false, error: 'Notification permission was denied' }
    }

    const reg = await registerServiceWorker()
    if (!reg) {
      return { success: false, error: 'Failed to register service worker' }
    }

    await navigator.serviceWorker.ready

    const vapidResp = await api.getVapidKey()
    if (!vapidResp.public_key) {
      return { success: false, error: 'Server does not have Web Push VAPID key configured' }
    }

    const decodedAppServerKey = urlBase64ToUint8Array(vapidResp.public_key)
    const appServerKey = new Uint8Array(decodedAppServerKey.byteLength)
    appServerKey.set(decodedAppServerKey)
    const subscription = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: appServerKey.buffer,
    })

    const ua = navigator.userAgent
    let deviceName = 'PWA Web Browser'
    if (ua.includes('iPhone') || ua.includes('iPad')) deviceName = 'Safari no iOS'
    else if (ua.includes('Android')) deviceName = 'Chrome no Android'
    else if (ua.includes('Chrome')) deviceName = 'Chrome'
    else if (ua.includes('Firefox')) deviceName = 'Firefox'
    else if (ua.includes('Safari')) deviceName = 'Safari'

    const result = await api.subscribePush({
      name: deviceName,
      platform: 'web',
      subscription: subscription.toJSON(),
    })

    if (result.credential) {
      try {
        localStorage.setItem('boop_pwa_cred', result.credential)
      } catch {}
    }

    return { success: true }
  } catch (err: unknown) {
    const message = err instanceof Error ? err.message : 'Subscription failed'
    console.error('[PWA] Push subscription error:', err)
    return { success: false, error: message }
  }
}

export async function unsubscribeFromWebPush(): Promise<void> {
  if (typeof window === 'undefined' || !('serviceWorker' in navigator) || !('PushManager' in window)) return
  try {
    const reg = await navigator.serviceWorker.getRegistration()
    if (reg) {
      const sub = await reg.pushManager.getSubscription()
      if (sub) {
        await api.unsubscribePush(sub.endpoint).catch(() => {})
        await sub.unsubscribe()
      }
    }
    localStorage.removeItem('boop_pwa_cred')
  } catch (err) {
    console.warn('[PWA] Unsubscribe error:', err)
  }
}

/** Connects to Server-Sent Events (SSE) live event stream for instant in-app delivery. */
export function connectLiveEventStream(onEvent: (event: Event) => void): () => void {
  if (typeof window === 'undefined' || typeof EventSource === 'undefined') {
    return () => {}
  }

  let es: EventSource | null = null
  let stopped = false
  let reconnectTimer: number | undefined

  function connect() {
    if (stopped) return
    try {
      es = new EventSource('/api/v1/events/stream')
      es.addEventListener('event', (msg) => {
        try {
          const parsed = JSON.parse(msg.data) as Event
          if (parsed && typeof parsed.id === 'string') {
            onEvent(parsed)
          }
        } catch {
          /* ignore malformed message */
        }
      })
      es.onerror = () => {
        if (es) {
          es.close()
          es = null
        }
        if (!stopped) {
          reconnectTimer = window.setTimeout(connect, 3000)
        }
      }
    } catch {
      if (!stopped) {
        reconnectTimer = window.setTimeout(connect, 5000)
      }
    }
  }

  connect()

  return () => {
    stopped = true
    window.clearTimeout(reconnectTimer)
    if (es) {
      es.close()
      es = null
    }
  }
}
