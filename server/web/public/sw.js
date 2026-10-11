// Service Worker for Boop PWA

self.addEventListener('install', (event) => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(clients.claim());
});

self.addEventListener('push', (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch (err) {
    data = { title: event.data ? event.data.text() : 'Boop' };
  }

  const title = data.title || 'Boop';
  const options = {
    body: data.body || '',
    icon: '/icon-192.png',
    badge: '/icon-192.png',
    data: {
      url: data.url || '/',
      id: data.id || data.event_id || ''
    },
    tag: data.id || ('boop-' + Date.now()),
    renotify: true
  };

  if (Array.isArray(data.actions) && data.actions.length > 0) {
    options.actions = data.actions.slice(0, 3).map((a) => ({
      action: a.url || a.action || 'open',
      title: a.label || a.title || 'Open'
    }));
  }

  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();

  let targetUrl = (event.notification.data && event.notification.data.url) || '/';
  if (event.action && (event.action.startsWith('http://') || event.action.startsWith('https://'))) {
    targetUrl = event.action;
  }

  event.waitUntil(
    clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windowClients) => {
      for (const client of windowClients) {
        if (client.url.includes(self.location.origin) && 'focus' in client) {
          if ('navigate' in client && targetUrl !== '/') {
            client.navigate(targetUrl);
          }
          return client.focus();
        }
      }
      if (clients.openWindow) {
        return clients.openWindow(targetUrl);
      }
    })
  );
});
