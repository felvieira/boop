// Package webpush handles Web Push (VAPID) delivery for PWAs and modern browsers.
package webpush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	wp "github.com/SherClockHolmes/webpush-go"
	"github.com/chrisgreg/boop/server/internal/apns"
	"github.com/chrisgreg/boop/server/internal/config"
	"github.com/chrisgreg/boop/server/internal/settings"
)

// ErrUnregistered is returned when a Web Push endpoint reports a 404/410 (expired/unregistered subscription).
var ErrUnregistered = errors.New("webpush: subscription expired or unregistered")

// Client sends Web Push notifications via VAPID.
type Client struct {
	cfg config.WebPush
	log *slog.Logger
}

// InitOrLoadKeys ensures VAPID keys exist, either loaded from config, SQLite settings, or newly generated.
func InitOrLoadKeys(ctx context.Context, st *settings.Store, cfg config.WebPush, log *slog.Logger) (*Client, error) {
	pub := cfg.VAPIDPublicKey
	priv := cfg.VAPIDPrivateKey
	sub := cfg.Subject

	if sub == "" {
		sub = "mailto:admin@boop.local"
	}

	if pub == "" || priv == "" {
		dbPub, errPub := st.Get(ctx, settings.KeyVAPIDPublicKey, "")
		dbPriv, errPriv := st.Get(ctx, settings.KeyVAPIDPrivateKey, "")
		if errPub == nil && errPriv == nil && dbPub != "" && dbPriv != "" {
			pub = dbPub
			priv = dbPriv
		} else {
			var errGen error
			priv, pub, errGen = wp.GenerateVAPIDKeys()
			if errGen != nil {
				return nil, fmt.Errorf("generate vapid keys: %w", errGen)
			}
			_ = st.Set(ctx, settings.KeyVAPIDPublicKey, pub)
			_ = st.Set(ctx, settings.KeyVAPIDPrivateKey, priv)
			_ = st.Set(ctx, settings.KeyVAPIDSubject, sub)
			log.Info("webpush.keys_generated", "public_key", pub)
		}
	}

	return &Client{
		cfg: config.WebPush{
			VAPIDPublicKey:  pub,
			VAPIDPrivateKey: priv,
			Subject:         sub,
		},
		log: log,
	}, nil
}

// PublicKey returns the VAPID public key.
func (c *Client) PublicKey() string {
	if c == nil {
		return ""
	}
	return c.cfg.VAPIDPublicKey
}

// Payload is the JSON body delivered to the Service Worker push event.
type Payload struct {
	ID        string        `json:"id"`
	EventID   string        `json:"event_id"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	ProjectID string        `json:"project_id"`
	URL       string        `json:"url"`
	Actions   []apns.Action `json:"actions,omitempty"`
}

// Send delivers a notification to a Web Push subscription.
func (c *Client) Send(ctx context.Context, subscriptionJSON string, n apns.Notification) (string, error) {
	if c == nil || c.cfg.VAPIDPublicKey == "" || c.cfg.VAPIDPrivateKey == "" {
		return "", errors.New("webpush is not configured")
	}

	var sub wp.Subscription
	if err := json.Unmarshal([]byte(subscriptionJSON), &sub); err != nil {
		return "", fmt.Errorf("invalid subscription JSON: %w", err)
	}
	if sub.Endpoint == "" {
		return "", errors.New("empty subscription endpoint")
	}

	url := "/"
	if n.EventID != "" {
		url = "/events/" + n.EventID
	}
	p := Payload{
		ID:        n.EventID,
		EventID:   n.EventID,
		Title:     n.Title,
		Body:      n.Body,
		ProjectID: n.ProjectID,
		URL:       url,
		Actions:   n.Actions,
	}

	data, err := json.Marshal(p)
	if err != nil {
		return "", err
	}

	res, err := wp.SendNotificationWithContext(ctx, data, &sub, &wp.Options{
		Subscriber:      c.cfg.Subject,
		VAPIDPublicKey:  c.cfg.VAPIDPublicKey,
		VAPIDPrivateKey: c.cfg.VAPIDPrivateKey,
		TTL:             86400,
		Urgency:         wp.UrgencyHigh,
	})
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
		return "", ErrUnregistered
	}
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("webpush endpoint returned HTTP %d", res.StatusCode)
	}

	return "webpush-" + n.EventID, nil
}
