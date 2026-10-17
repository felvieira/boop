// Package delivery fans an event out to every pushable device, records the
// outcome in the deliveries table, and logs push.sent / push.failed.
package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/chrisgreg/boop/server/internal/apns"
	"github.com/chrisgreg/boop/server/internal/devices"
	"github.com/chrisgreg/boop/server/internal/events"
	"github.com/chrisgreg/boop/server/internal/events/levels"
	"github.com/chrisgreg/boop/server/internal/ids"
	"github.com/chrisgreg/boop/server/internal/projects"
	"github.com/chrisgreg/boop/server/internal/webpush"
)

// Sender is the subset of *apns.Client the dispatcher needs.
type Sender interface {
	Send(ctx context.Context, deviceToken string, n apns.Notification) (string, error)
}

// WebPushSender is the subset of *webpush.Client the dispatcher needs.
type WebPushSender interface {
	Send(ctx context.Context, subscriptionJSON string, n apns.Notification) (string, error)
}

// Statuses recorded in deliveries.status.
const (
	StatusSent    = "sent"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// Delivery is one attempt to push an event to a device.
type Delivery struct {
	ID          string `json:"id"`
	EventID     string `json:"event_id"`
	DeviceID    string `json:"device_id"`
	DeviceName  string `json:"device_name"`
	Status      string `json:"status"`
	APNSID      string `json:"apns_id,omitempty"`
	Error       string `json:"error,omitempty"`
	AttemptedAt string `json:"attempted_at"`
}

// Dispatcher sends notifications for events.
type Dispatcher struct {
	db        *sql.DB
	devices   *devices.Store
	sender    Sender        // nil when APNs is not configured
	webSender WebPushSender // nil when Web Push is not configured
	log       *slog.Logger
	timeout   time.Duration

	queue chan job
	wg    sync.WaitGroup
}

type job struct {
	event   events.Event
	project projects.Project
}

// New returns a Dispatcher. sender may be nil, in which case deliveries are
// recorded as skipped.
func New(db *sql.DB, d *devices.Store, sender Sender, log *slog.Logger) *Dispatcher {
	return &Dispatcher{db: db, devices: d, sender: sender, log: log, timeout: 10 * time.Second, queue: make(chan job, 1024)}
}

// SetWebPush sets the Web Push sender for PWA/browser notifications.
func (d *Dispatcher) SetWebPush(s WebPushSender) {
	d.webSender = s
}

// Start runs the background worker until ctx is cancelled.
func (d *Dispatcher) Start(ctx context.Context) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case j := <-d.queue:
				d.Deliver(context.Background(), j.event, j.project)
			}
		}
	}()
}

// Wait blocks until the worker has stopped.
func (d *Dispatcher) Wait() { d.wg.Wait() }

// Enqueue schedules delivery without blocking the caller. If the queue is
// full the event is delivered synchronously instead.
func (d *Dispatcher) Enqueue(e events.Event, p projects.Project) {
	select {
	case d.queue <- job{event: e, project: p}:
	default:
		d.Deliver(context.Background(), e, p)
	}
}

// Deliver pushes e to every registered device synchronously and returns the
// recorded deliveries.
func (d *Dispatcher) Deliver(ctx context.Context, e events.Event, p projects.Project) []Delivery {
	if !p.Notify || !levels.AtLeast(e.Level, p.MinLevel) {
		return nil
	}
	devs, err := d.devices.Pushable(ctx)
	if err != nil {
		d.log.Error("push.failed", "event_id", e.ID, "error", err.Error())
		return nil
	}
	n := apns.Notification{
		Title:     p.Name + " · " + e.Title,
		Body:      e.Body,
		EventID:   e.ID,
		ProjectID: p.ID,
		Prominent: e.Level == levels.Critical,
	}
	for _, a := range e.Actions {
		n.Actions = append(n.Actions, apns.Action{Label: a.Label, URL: a.URL})
	}
	if n.Body == "" {
		n.Body = e.Title
		n.Title = p.Name
	}
	var out []Delivery
	for _, dev := range devs {
		rec := Delivery{ID: ids.New("dlv"), EventID: e.ID, DeviceID: dev.ID, DeviceName: dev.Name, AttemptedAt: ids.Now()}
		isWeb := dev.Platform == "web" || dev.Platform == "pwa" || (dev.DeviceToken != nil && strings.HasPrefix(*dev.DeviceToken, "{"))
		if isWeb {
			if d.webSender == nil {
				rec.Status = StatusSkipped
				rec.Error = "Web Push is not configured"
			} else {
				sctx, cancel := context.WithTimeout(ctx, d.timeout)
				wpID, err := d.webSender.Send(sctx, *dev.DeviceToken, n)
				cancel()
				if err != nil {
					rec.Status = StatusFailed
					rec.Error = err.Error()
					if errors.Is(err, webpush.ErrUnregistered) {
						_ = d.devices.ClearToken(ctx, dev.ID)
					}
					d.log.Warn("push.failed", "event_id", e.ID, "device_id", dev.ID, "platform", "web", "error", rec.Error)
				} else {
					rec.Status = StatusSent
					rec.APNSID = wpID
					d.log.Info("push.sent", "event_id", e.ID, "device_id", dev.ID, "platform", "web")
				}
			}
		} else {
			if d.sender == nil {
				rec.Status = StatusSkipped
				rec.Error = "APNs is not configured"
			} else {
				sctx, cancel := context.WithTimeout(ctx, d.timeout)
				apnsID, err := d.sender.Send(sctx, *dev.DeviceToken, n)
				cancel()
				if err != nil {
					rec.Status = StatusFailed
					rec.Error = err.Error()
					var ae *apns.Error
					if errors.As(err, &ae) && ae.Unregistered() {
						_ = d.devices.ClearToken(ctx, dev.ID)
					}
					d.log.Warn("push.failed", "event_id", e.ID, "device_id", dev.ID, "error", rec.Error)
				} else {
					rec.Status = StatusSent
					rec.APNSID = apnsID
					d.log.Info("push.sent", "event_id", e.ID, "device_id", dev.ID, "apns_id", apnsID)
				}
			}
		}
		if _, err := d.db.ExecContext(ctx, `INSERT INTO deliveries (id, event_id, device_id, status, apns_id, error, attempted_at, created_at) VALUES (?,?,?,?,?,?,?,?)`,
			rec.ID, rec.EventID, rec.DeviceID, rec.Status, rec.APNSID, rec.Error, rec.AttemptedAt, rec.AttemptedAt); err != nil {
			d.log.Error("delivery.record_failed", "error", err.Error())
		}
		out = append(out, rec)
	}
	return out
}

// BroadcastOnce sends a digest at most once per device and local calendar day.
// Successful device sends are persisted so a partial failure can retry without
// duplicating notifications on devices that already received the digest.
func (d *Dispatcher) BroadcastOnce(ctx context.Context, localDate, title, body string) (int, error) {
	devs, err := d.devices.Pushable(ctx)
	if err != nil {
		return 0, err
	}
	n := apns.Notification{Title: title, Body: body}
	sent := 0
	var sendErrors []error
	for _, dev := range devs {
		var alreadySent int
		if err := d.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM support_digest_deliveries WHERE local_date=? AND device_id=?)`, localDate, dev.ID).Scan(&alreadySent); err != nil {
			sendErrors = append(sendErrors, fmt.Errorf("check digest delivery for device %s: %w", dev.ID, err))
			continue
		}
		if alreadySent != 0 || dev.DeviceToken == nil || strings.TrimSpace(*dev.DeviceToken) == "" {
			continue
		}
		isWeb := dev.Platform == "web" || dev.Platform == "pwa" || strings.HasPrefix(*dev.DeviceToken, "{")
		var pushID string
		var sendErr error
		if isWeb {
			if d.webSender == nil {
				continue
			}
			sctx, cancel := context.WithTimeout(ctx, d.timeout)
			pushID, sendErr = d.webSender.Send(sctx, *dev.DeviceToken, n)
			cancel()
		} else {
			if d.sender == nil {
				continue
			}
			sctx, cancel := context.WithTimeout(ctx, d.timeout)
			pushID, sendErr = d.sender.Send(sctx, *dev.DeviceToken, n)
			cancel()
		}
		if sendErr != nil {
			if isWeb && errors.Is(sendErr, webpush.ErrUnregistered) {
				_ = d.devices.ClearToken(ctx, dev.ID)
			} else if !isWeb {
				var ae *apns.Error
				if errors.As(sendErr, &ae) && ae.Unregistered() {
					_ = d.devices.ClearToken(ctx, dev.ID)
				}
			}
			d.log.Warn("support.digest.push_failed", "device_id", dev.ID, "platform", dev.Platform, "error", sendErr.Error())
			sendErrors = append(sendErrors, fmt.Errorf("send digest to device %s: %w", dev.ID, sendErr))
			continue
		}
		if _, err := d.db.ExecContext(ctx, `INSERT OR IGNORE INTO support_digest_deliveries (local_date,device_id,sent_at) VALUES (?,?,?)`, localDate, dev.ID, ids.Now()); err != nil {
			sendErrors = append(sendErrors, fmt.Errorf("record digest delivery for device %s: %w", dev.ID, err))
			continue
		}
		sent++
		d.log.Info("support.digest.push_sent", "device_id", dev.ID, "platform", dev.Platform, "push_id", pushID)
	}
	return sent, errors.Join(sendErrors...)
}

// ForEvent lists deliveries recorded for an event.
func (d *Dispatcher) ForEvent(ctx context.Context, eventID string) ([]Delivery, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT dl.id, dl.event_id, dl.device_id, COALESCE(dv.name, ''), dl.status, dl.apns_id, dl.error, dl.attempted_at
		FROM deliveries dl LEFT JOIN devices dv ON dv.id = dl.device_id WHERE dl.event_id = ? ORDER BY dl.attempted_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var r Delivery
		if err := rows.Scan(&r.ID, &r.EventID, &r.DeviceID, &r.DeviceName, &r.Status, &r.APNSID, &r.Error, &r.AttemptedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Last returns the most recent delivery attempt, if any.
func (d *Dispatcher) Last(ctx context.Context) (*Delivery, error) {
	var r Delivery
	err := d.db.QueryRowContext(ctx, `SELECT dl.id, dl.event_id, dl.device_id, COALESCE(dv.name, ''), dl.status, dl.apns_id, dl.error, dl.attempted_at
		FROM deliveries dl LEFT JOIN devices dv ON dv.id = dl.device_id ORDER BY dl.attempted_at DESC LIMIT 1`).
		Scan(&r.ID, &r.EventID, &r.DeviceID, &r.DeviceName, &r.Status, &r.APNSID, &r.Error, &r.AttemptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}
