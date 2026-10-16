// Package api exposes the Boop HTTP API and wires it to the domain packages.
package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chrisgreg/boop/server/internal/apns"
	"github.com/chrisgreg/boop/server/internal/auth"
	"github.com/chrisgreg/boop/server/internal/config"
	"github.com/chrisgreg/boop/server/internal/delivery"
	"github.com/chrisgreg/boop/server/internal/devices"
	"github.com/chrisgreg/boop/server/internal/events"
	"github.com/chrisgreg/boop/server/internal/events/levels"
	"github.com/chrisgreg/boop/server/internal/events/redact"
	"github.com/chrisgreg/boop/server/internal/mcp"
	"github.com/chrisgreg/boop/server/internal/openrouter"
	"github.com/chrisgreg/boop/server/internal/pairing"
	"github.com/chrisgreg/boop/server/internal/projects"
	"github.com/chrisgreg/boop/server/internal/settings"
	"github.com/chrisgreg/boop/server/internal/silences"
	"github.com/chrisgreg/boop/server/internal/support"
	"github.com/chrisgreg/boop/server/internal/webpush"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
)

// Version is the server version, overridden at build time via -ldflags.
var Version = "1.3.0"

// Server holds every dependency the handlers need.
type Server struct {
	Config     config.Config
	DB         *sql.DB
	Log        *slog.Logger
	Settings   *settings.Store
	Projects   *projects.Store
	Devices    *devices.Store
	Pairing    *pairing.Store
	Events     *events.Store
	Silences   *silences.Store
	Support    *support.Store
	OpenRouter *openrouter.Client
	Dispatcher *delivery.Dispatcher
	APNS       *apns.Client    // nil when not configured
	APNSError  string          // why APNS is nil
	WebPush    *webpush.Client // nil when not configured
	EventHub   *EventHub
	Admin      *auth.Admin // web UI / admin API login; open when not enabled
	StartedAt  time.Time
	Web        http.Handler
}

// EventHub broadcasts new events to SSE subscribers in real time.
type EventHub struct {
	mu          sync.RWMutex
	subscribers map[chan events.Event]struct{}
}

func NewEventHub() *EventHub {
	return &EventHub{
		subscribers: make(map[chan events.Event]struct{}),
	}
}

func (h *EventHub) Subscribe() chan events.Event {
	ch := make(chan events.Event, 32)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribers[ch] = struct{}{}
	return ch
}

func (h *EventHub) Unsubscribe(ch chan events.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subscribers, ch)
	close(ch)
}

func (h *EventHub) Publish(e events.Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- e:
		default:
		}
	}
}

// Handler builds the router.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)

	// Admin session.
	mux.HandleFunc("GET /api/v1/auth/me", s.authMe)
	mux.HandleFunc("POST /api/v1/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.authLogout)

	// Event ingestion: project API key.
	mux.Handle("POST /api/v1/events", s.projectAuth(s.createEvent))

	// Sentry SDK compatibility: point a Sentry DSN at Boop and existing Sentry
	// clients report here. The DSN public key is a Boop project API key; the
	// numeric project id in the path is ignored.
	mux.HandleFunc("POST /api/{project_id}/envelope/{$}", s.sentryEnvelope)

	// Reading events: device credential or the (unauthenticated) web UI.
	mux.Handle("GET /api/v1/events", s.readerAuth(s.listEvents))
	mux.Handle("GET /api/v1/events/{id}", s.readerAuth(s.getEvent))
	mux.Handle("GET /api/v1/events/{id}/deliveries", s.readerAuth(s.eventDeliveries))
	mux.Handle("GET /api/v1/events/stream", s.readerAuth(s.streamEvents))

	// Web Push (PWA).
	mux.HandleFunc("GET /api/v1/push/vapid-key", s.getVAPIDKey)
	mux.Handle("POST /api/v1/push/subscribe", s.readerAuth(s.subscribeWebPush))
	mux.Handle("POST /api/v1/push/unsubscribe", s.readerAuth(s.unsubscribeWebPush))
	// Devices: the paired phone manages itself; the web UI lists/removes.
	mux.Handle("POST /api/v1/devices", s.deviceAuth(s.registerDevice))
	mux.Handle("PATCH /api/v1/devices/{id}", s.deviceOrAdmin(s.updateDevice))
	mux.Handle("DELETE /api/v1/devices/{id}", s.deviceOrAdmin(s.deleteDevice))
	mux.Handle("GET /api/v1/devices", s.adminAuth(s.listDevices))

	// Pairing.
	mux.Handle("POST /api/v1/pairing", s.adminAuth(s.createPairing))
	mux.Handle("GET /api/v1/pairing", s.adminAuth(s.listPairing))
	mux.Handle("DELETE /api/v1/pairing/{id}", s.adminAuth(s.revokePairing))
	mux.HandleFunc("POST /api/v1/pairing/exchange", s.exchangePairing)

	// Admin.
	mux.Handle("GET /api/v1/projects", s.adminAuth(s.listProjects))
	mux.HandleFunc("GET /api/v1/projects/icons", s.projectIcons)
	mux.Handle("POST /api/v1/projects", s.adminAuth(s.createProject))
	mux.Handle("GET /api/v1/projects/{id}", s.adminAuth(s.getProject))
	mux.Handle("PATCH /api/v1/projects/{id}", s.adminAuth(s.updateProject))
	mux.Handle("DELETE /api/v1/projects/{id}", s.adminAuth(s.deleteProject))
	mux.Handle("POST /api/v1/projects/{id}/rotate-key", s.adminAuth(s.rotateProjectKey))
	mux.Handle("POST /api/v1/events/{id}/unsilence", s.adminAuth(s.unsilenceEvent))
	mux.Handle("GET /api/v1/silences/{id}", s.adminAuth(s.getSilence))
	mux.Handle("GET /api/v1/silences", s.adminAuth(s.listSilences))
	mux.Handle("POST /api/v1/silences", s.adminAuth(s.createSilence))
	mux.Handle("DELETE /api/v1/silences/{id}", s.adminAuth(s.deleteSilence))
	mux.Handle("GET /api/v1/status", s.adminAuth(s.status))
	mux.Handle("GET /api/v1/settings", s.adminAuth(s.getSettings))
	mux.Handle("PATCH /api/v1/settings", s.adminAuth(s.updateSettings))
	mux.Handle("GET /api/v1/openrouter/models", s.adminAuth(s.listOpenRouterModels))
	mux.Handle("POST /api/v1/test", s.adminAuth(s.testNotification))

	// AI-curated support cards and their private image attachments. These require
	// configured admin auth even if the rest of Boop is deployed open.
	mux.Handle("GET /api/v1/support/board", s.supportAuth(s.getSupportBoard))
	mux.Handle("GET /api/v1/support/lists", s.supportAuth(s.listSupportLists))
	mux.Handle("POST /api/v1/support/lists", s.supportAuth(s.createSupportList))
	mux.Handle("PATCH /api/v1/support/lists/{id}", s.supportAuth(s.updateSupportList))
	mux.Handle("DELETE /api/v1/support/lists/{id}", s.supportAuth(s.deleteSupportList))
	mux.Handle("GET /api/v1/support/cards", s.supportAuth(s.listSupportCards))
	mux.Handle("POST /api/v1/support/cards", s.supportAuth(s.createSupportCard))
	mux.Handle("GET /api/v1/support/cards/{id}", s.supportAuth(s.getSupportCard))
	mux.Handle("PATCH /api/v1/support/cards/{id}", s.supportAuth(s.updateSupportCard))
	mux.Handle("POST /api/v1/support/cards/{id}/move", s.supportAuth(s.moveSupportCard))
	mux.Handle("POST /api/v1/support/cards/{id}/archive", s.supportAuth(s.archiveSupportCard))
	mux.Handle("POST /api/v1/support/cards/{id}/restore", s.supportAuth(s.restoreSupportCard))
	mux.Handle("POST /api/v1/support/cards/{id}/comments", s.supportAuth(s.addSupportComment))
	mux.Handle("DELETE /api/v1/support/cards/{id}/comments/{comment_id}", s.supportAuth(s.deleteSupportComment))
	mux.Handle("POST /api/v1/support/cards/{id}/checklist", s.supportAuth(s.addSupportChecklistItem))
	mux.Handle("PATCH /api/v1/support/cards/{id}/checklist/{item_id}", s.supportAuth(s.updateSupportChecklistItem))
	mux.Handle("DELETE /api/v1/support/cards/{id}/checklist/{item_id}", s.supportAuth(s.deleteSupportChecklistItem))
	mux.Handle("POST /api/v1/support/cards/{id}/attachments", s.supportAuth(s.addSupportAttachments))
	mux.Handle("DELETE /api/v1/support/cards/{id}/attachments/{attachment_id}", s.supportAuth(s.deleteSupportAttachment))
	mux.Handle("GET /api/v1/support/triage/status", s.supportAuth(s.supportTriageStatus))
	mux.Handle("POST /api/v1/support/cards/upsert", s.supportIngestAuth(s.upsertSupportCard))
	mux.Handle("PATCH /api/v1/support/cards/{id}/status", s.supportAuth(s.setSupportCardStatus))
	mux.Handle("GET /api/v1/support/cards/{id}/attachments/{attachment_id}", s.supportAuth(s.getSupportAttachment))

	// MCP (read-only) for AI agents: Streamable HTTP at /mcp.
	mux.Handle("/mcp", s.mcpAuth(mcp.Handler(mcp.NewServer(mcp.Stores{Projects: s.Projects, Events: s.Events}, Version), s.Log)))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	if s.Web != nil {
		mux.Handle("/", s.Web)
	}
	return s.logging(mux)
}

// ---- middleware ----

type ctxKey int

const (
	ctxProject ctxKey = iota
	ctxDevice
)

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			s.Log.Debug("http.request", "method", r.Method, "path", r.URL.Path, "status", rw.status, "ms", time.Since(start).Milliseconds())
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// projectAuth requires a project API key.
func (s *Server) projectAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := auth.Bearer(r)
		if key == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a project API key is required: Authorization: Bearer boop_proj_...")
			return
		}
		p, err := s.Projects.Authenticate(r.Context(), key)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid project API key")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxProject, p)))
	})
}

// deviceAuth requires a device credential.
func (s *Server) deviceAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred := auth.Bearer(r)
		if cred == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "a device credential is required")
			return
		}
		d, err := s.Devices.Authenticate(r.Context(), cred)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device credential")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxDevice, d)))
	})
}

// readerAuth accepts either a valid device credential or no credential at all
// (the embedded web UI). Any other bearer token is rejected.
func (s *Server) readerAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred := auth.Bearer(r)
		if cred == "" {
			if !s.Admin.Authorized(r) {
				writeError(w, http.StatusUnauthorized, "login_required", "sign in to the Boop web UI")
				return
			}
			next(w, r)
			return
		}
		d, err := s.Devices.Authenticate(r.Context(), cred)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device credential")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxDevice, d)))
	})
}

// mcpAuth guards the MCP endpoint. It accepts the configured BOOP_MCP_TOKEN,
// a device credential, or an admin session/HTTP Basic login (or nothing when
// admin auth is off, like the rest of the read API). Project API keys are
// refused: they are write-only ingestion credentials.
func (s *Server) mcpAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if on, err := s.mcpEnabled(r.Context()); err != nil {
			s.fail(w, err)
			return
		} else if !on {
			writeError(w, http.StatusNotFound, "mcp_disabled", "the MCP endpoint is turned off in Settings")
			return
		}
		cred := auth.Bearer(r)
		switch {
		case cred == "":
			if !s.Admin.Authorized(r) {
				writeError(w, http.StatusUnauthorized, "unauthorized", "an MCP token (BOOP_MCP_TOKEN) or device credential is required: Authorization: Bearer ...")
				return
			}
		case s.Config.MCPToken != "" && auth.Equal(auth.Hash(cred), auth.Hash(s.Config.MCPToken)):
		case auth.HasPrefix(cred, auth.PrefixDevice):
			if _, err := s.Devices.Authenticate(r.Context(), cred); err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device credential")
				return
			}
		default:
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid MCP credential")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// adminAuth serves the web UI and admin API. When BOOP_ADMIN_USER/PASSWORD are
// set, a session cookie (from /auth/login) or HTTP Basic credentials are
// required; otherwise it is open and expected to sit behind the operator's own
// proxy. Project and device credentials are explicitly refused so that a
// leaked client secret grants no admin rights.
func (s *Server) adminAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cred := auth.Bearer(r); cred != "" {
			writeError(w, http.StatusForbidden, "forbidden", "project and device credentials cannot perform administrative actions")
			return
		}
		if !s.Admin.Authorized(r) {
			writeError(w, http.StatusUnauthorized, "login_required", "sign in to the Boop web UI")
			return
		}
		next(w, r)
	})
}

// supportAuth requires the existing admin login to be configured and authenticated.
// Keeping support data closed when admin auth is disabled prevents a deployment
// that relies only on its reverse proxy from accidentally publishing customer mail.
func (s *Server) supportAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Add("Vary", "Cookie")
		w.Header().Add("Vary", "Authorization")
		if s.Support == nil {
			writeError(w, http.StatusServiceUnavailable, "support_unavailable", "support storage is unavailable")
			return
		}
		if s.Admin == nil || !s.Admin.Enabled() {
			writeError(w, http.StatusServiceUnavailable, "support_auth_required", "configure BOOP_ADMIN_USER and BOOP_ADMIN_PASSWORD to enable the private support board")
			return
		}
		s.adminAuth(next).ServeHTTP(w, r)
	})
}

func (s *Server) supportIngestAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Add("Vary", "Authorization")
		if s.Support == nil {
			writeError(w, http.StatusServiceUnavailable, "support_unavailable", "support storage is unavailable")
			return
		}
		if credential := auth.Bearer(r); credential != "" {
			if s.Config.SupportIngestToken != "" && auth.Equal(auth.Hash(credential), auth.Hash(s.Config.SupportIngestToken)) {
				next(w, r)
				return
			}
			writeError(w, http.StatusUnauthorized, "invalid_support_token", "a valid support-ingest token is required")
			return
		}
		if s.Admin == nil || !s.Admin.Enabled() {
			writeError(w, http.StatusServiceUnavailable, "support_auth_required", "configure BOOP_SUPPORT_INGEST_TOKEN or the Boop admin login to ingest support cards")
			return
		}
		s.adminAuth(next).ServeHTTP(w, r)
	})
}

// ---- admin session ----

func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"auth_required": s.Admin.Enabled(), "authenticated": s.Admin.Authorized(r)})
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Admin.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"auth_required": false, "authenticated": true})
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	tok, ok := s.Admin.Login(in.Username, in.Password)
	if !ok {
		// Slow down guessing a little; there is one account and no lockout.
		time.Sleep(400 * time.Millisecond)
		s.Log.Warn("auth.login_failed", "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "bad_credentials", "wrong username or password")
		return
	}
	s.Admin.SetCookie(w, r, tok)
	s.Log.Info("auth.login", "remote", r.RemoteAddr)
	writeJSON(w, http.StatusOK, map[string]any{"auth_required": true, "authenticated": true})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		s.Admin.Logout(c.Value)
	}
	s.Admin.ClearCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// deviceOrAdmin allows a device to act on itself, or the web UI on anything.
func (s *Server) deviceOrAdmin(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cred := auth.Bearer(r)
		if cred == "" {
			if !s.Admin.Authorized(r) {
				writeError(w, http.StatusUnauthorized, "login_required", "sign in to the Boop web UI")
				return
			}
			next(w, r)
			return
		}
		d, err := s.Devices.Authenticate(r.Context(), cred)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid device credential")
			return
		}
		if d.ID != r.PathValue("id") {
			writeError(w, http.StatusForbidden, "forbidden", "a device may only modify itself")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxDevice, d)))
	})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: code, Message: msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body must be 1 MB or smaller")
		return false
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a JSON object")
		return false
	}
	if err := json.Unmarshal(b, v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

// fail maps domain errors to HTTP responses.
func (s *Server) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projects.ErrNotFound), errors.Is(err, events.ErrNotFound), errors.Is(err, devices.ErrNotFound), errors.Is(err, pairing.ErrNotFound), errors.Is(err, silences.ErrNotFound), errors.Is(err, support.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, projects.ErrInvalid), errors.Is(err, events.ErrInvalid), errors.Is(err, devices.ErrInvalid), errors.Is(err, silences.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
	case errors.Is(err, pairing.ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, "invalid_pairing_token", err.Error())
	default:
		s.Log.Error("http.error", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
	}
}

func (s *Server) redactor(ctx context.Context) *redact.Redactor {
	extra, err := s.Settings.GetList(ctx, settings.KeyRedactKeys)
	if err != nil {
		s.Log.Error("settings.read_failed", "error", err.Error())
	}
	return redact.New(extra...)
}

// ---- health & status ----

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "database": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type statusResponse struct {
	Version        string             `json:"version"`
	Server         string             `json:"server"`
	Database       string             `json:"database"`
	DatabasePath   string             `json:"database_path"`
	BaseURL        string             `json:"base_url"`
	UptimeSeconds  int64              `json:"uptime_seconds"`
	APNS           apnsStatus         `json:"apns"`
	Devices        int                `json:"devices"`
	PushableDevice int                `json:"pushable_devices"`
	Projects       int                `json:"projects"`
	Events         int                `json:"events"`
	LastPush       *delivery.Delivery `json:"last_push"`
	RetentionDays  int                `json:"retention_days"`
	SetupCompleted bool               `json:"setup_completed"`
	AdminAuth      bool               `json:"admin_auth"`
	WebPush        webpushStatus      `json:"webpush"`
}

type webpushStatus struct {
	Configured bool   `json:"configured"`
	PublicKey  string `json:"public_key,omitempty"`
}
type apnsStatus struct {
	Configured  bool     `json:"configured"`
	Error       string   `json:"error,omitempty"`
	Missing     []string `json:"missing,omitempty"`
	TeamID      string   `json:"team_id,omitempty"`
	KeyID       string   `json:"key_id,omitempty"`
	BundleID    string   `json:"bundle_id,omitempty"`
	Environment string   `json:"environment"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	resp := statusResponse{Version: Version, Server: "ok", Database: "ok", DatabasePath: s.Config.DatabasePath, BaseURL: s.baseURL(r), UptimeSeconds: int64(time.Since(s.StartedAt).Seconds()), AdminAuth: s.Admin.Enabled()}
	if err := s.DB.PingContext(ctx); err != nil {
		resp.Database = err.Error()
	}
	resp.APNS = apnsStatus{Configured: s.APNS != nil, Error: s.APNSError, Missing: s.Config.APNS.Missing(),
		TeamID: s.Config.APNS.TeamID, KeyID: s.Config.APNS.KeyID, BundleID: s.Config.APNS.BundleID, Environment: s.Config.APNS.Environment}
	resp.WebPush = webpushStatus{
		Configured: s.WebPush != nil && s.WebPush.PublicKey() != "",
		PublicKey: func() string {
			if s.WebPush != nil {
				return s.WebPush.PublicKey()
			}
			return ""
		}(),
	}
	devs, err := s.Devices.List(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	resp.Devices = len(devs)
	for _, d := range devs {
		if d.HasToken {
			resp.PushableDevice++
		}
	}
	if resp.Projects, err = s.Projects.Count(ctx); err != nil {
		s.fail(w, err)
		return
	}
	if resp.Events, err = s.Events.Count(ctx); err != nil {
		s.fail(w, err)
		return
	}
	if resp.LastPush, err = s.Dispatcher.Last(ctx); err != nil {
		s.fail(w, err)
		return
	}
	if resp.RetentionDays, err = s.Settings.GetInt(ctx, settings.KeyRetentionDays, s.Config.RetentionDays); err != nil {
		s.fail(w, err)
		return
	}
	if resp.SetupCompleted, err = s.Settings.GetBool(ctx, settings.KeySetupCompleted); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// baseURL is the configured public URL, or the request's origin as a fallback.
func (s *Server) baseURL(r *http.Request) string {
	if s.Config.BaseURL != "" {
		return s.Config.BaseURL
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

// ---- settings ----

type settingsResponse struct {
	RetentionDays          int      `json:"retention_days"`
	RedactKeys             []string `json:"redact_keys"`
	DefaultRedact          []string `json:"default_redact_keys"`
	SetupCompleted         bool     `json:"setup_completed"`
	MCPEnabled             bool     `json:"mcp_enabled"`
	OpenRouterAPIKeySet    bool     `json:"openrouter_api_key_set"`
	OpenRouterAPIKeySource string   `json:"openrouter_api_key_source"`
	OpenRouterAPIKeyError  bool     `json:"openrouter_api_key_error"`
	OpenRouterModel        string   `json:"openrouter_model"`
	// MCPTokenSet reports whether BOOP_MCP_TOKEN is configured (never the token itself).
	MCPTokenSet bool `json:"mcp_token_set"`
}

type settingsInput struct {
	RetentionDays         *int      `json:"retention_days"`
	RedactKeys            *[]string `json:"redact_keys"`
	SetupCompleted        *bool     `json:"setup_completed"`
	MCPEnabled            *bool     `json:"mcp_enabled"`
	OpenRouterAPIKey      *string   `json:"openrouter_api_key"`
	ClearOpenRouterAPIKey bool      `json:"clear_openrouter_api_key"`
	OpenRouterModel       *string   `json:"openrouter_model"`
}

var openRouterModelID = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._:-]+$`)

// mcpEnabled reads the MCP switch; it defaults to on.
func (s *Server) mcpEnabled(ctx context.Context) (bool, error) {
	v, err := s.Settings.Get(ctx, settings.KeyMCPEnabled, "true")
	return v != "false" && v != "0", err
}

func (s *Server) readSettings(ctx context.Context) (settingsResponse, error) {
	var out settingsResponse
	var err error
	if out.RetentionDays, err = s.Settings.GetInt(ctx, settings.KeyRetentionDays, s.Config.RetentionDays); err != nil {
		return out, err
	}
	if out.RedactKeys, err = s.Settings.GetList(ctx, settings.KeyRedactKeys); err != nil {
		return out, err
	}
	if out.RedactKeys == nil {
		out.RedactKeys = []string{}
	}
	out.DefaultRedact = redact.DefaultKeys
	if out.SetupCompleted, err = s.Settings.GetBool(ctx, settings.KeySetupCompleted); err != nil {
		return out, err
	}
	out.MCPTokenSet = s.Config.MCPToken != ""
	if out.MCPEnabled, err = s.mcpEnabled(ctx); err != nil {
		return out, err
	}
	provider, err := s.Settings.OpenRouterConfig(ctx, s.Config.OpenRouterAPIKey, s.Config.OpenRouterModel)
	if err != nil {
		hasSecret, hasErr := s.Settings.HasSecret(ctx, settings.KeyOpenRouterAPIKey)
		if hasErr != nil {
			return out, hasErr
		}
		out.OpenRouterAPIKeyError = hasSecret
	}
	out.OpenRouterAPIKeySet = provider.APIKey != "" || out.OpenRouterAPIKeyError
	out.OpenRouterAPIKeySource = provider.KeySource
	out.OpenRouterModel = provider.Model
	return out, nil
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	out, err := s.readSettings(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsInput
	if !readJSON(w, r, &in) {
		return
	}
	ctx := r.Context()
	if in.ClearOpenRouterAPIKey && in.OpenRouterAPIKey != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "provide a new OpenRouter key or clear the saved key, not both")
		return
	}
	var openRouterAPIKey string
	if in.OpenRouterAPIKey != nil {
		openRouterAPIKey = strings.TrimSpace(*in.OpenRouterAPIKey)
		if len(openRouterAPIKey) < 10 || len(openRouterAPIKey) > 512 {
			writeError(w, http.StatusUnprocessableEntity, "invalid", "openrouter_api_key must contain between 10 and 512 characters")
			return
		}
		checkCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		err := s.openRouterClient().ValidateAPIKey(checkCtx, openRouterAPIKey)
		cancel()
		if errors.Is(err, openrouter.ErrInvalidAPIKey) {
			writeError(w, http.StatusUnprocessableEntity, "invalid_openrouter_key", "OpenRouter rejected this API key")
			return
		}
		if err != nil {
			writeError(w, http.StatusBadGateway, "openrouter_unavailable", "could not validate the OpenRouter key; try again")
			return
		}
	}
	var openRouterModel string
	if in.OpenRouterModel != nil {
		openRouterModel = strings.TrimSpace(*in.OpenRouterModel)
		if len(openRouterModel) > 200 || !openRouterModelID.MatchString(openRouterModel) {
			writeError(w, http.StatusUnprocessableEntity, "invalid", "openrouter_model must be a valid provider/model id")
			return
		}
	}
	if in.RetentionDays != nil {
		if *in.RetentionDays < 0 || *in.RetentionDays > 3650 {
			writeError(w, http.StatusUnprocessableEntity, "invalid", "retention_days must be between 0 (unlimited) and 3650")
			return
		}
		if err := s.Settings.Set(ctx, settings.KeyRetentionDays, strconv.Itoa(*in.RetentionDays)); err != nil {
			s.fail(w, err)
			return
		}
	}
	if in.RedactKeys != nil {
		var clean []string
		for _, k := range *in.RedactKeys {
			if k = strings.TrimSpace(k); k != "" {
				if strings.Contains(k, ",") {
					writeError(w, http.StatusUnprocessableEntity, "invalid", "redact_keys entries may not contain commas")
					return
				}
				clean = append(clean, k)
			}
		}
		if err := s.Settings.Set(ctx, settings.KeyRedactKeys, strings.Join(clean, ",")); err != nil {
			s.fail(w, err)
			return
		}
	}
	if in.SetupCompleted != nil {
		if err := s.Settings.Set(ctx, settings.KeySetupCompleted, strconv.FormatBool(*in.SetupCompleted)); err != nil {
			s.fail(w, err)
			return
		}
	}
	if in.MCPEnabled != nil {
		if err := s.Settings.Set(ctx, settings.KeyMCPEnabled, strconv.FormatBool(*in.MCPEnabled)); err != nil {
			s.fail(w, err)
			return
		}
		s.Log.Info("settings.mcp", "enabled", *in.MCPEnabled)
	}
	if in.ClearOpenRouterAPIKey {
		if err := s.Settings.DeleteSecret(ctx, settings.KeyOpenRouterAPIKey); err != nil {
			s.fail(w, err)
			return
		}
	}
	if in.OpenRouterAPIKey != nil {
		if err := s.Settings.SetSecret(ctx, settings.KeyOpenRouterAPIKey, openRouterAPIKey); err != nil {
			s.fail(w, err)
			return
		}
	}
	if in.OpenRouterModel != nil {
		if err := s.Settings.Set(ctx, settings.KeyOpenRouterModel, openRouterModel); err != nil {
			s.fail(w, err)
			return
		}
	}
	out, err := s.readSettings(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) openRouterClient() *openrouter.Client {
	if s.OpenRouter != nil {
		return s.OpenRouter
	}
	return openrouter.NewClient()
}

func (s *Server) listOpenRouterModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.openRouterClient().ListModels(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "openrouter_unavailable", "could not load the OpenRouter model catalog")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

// ---- events ----

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	p := r.Context().Value(ctxProject).(projects.Project)
	var in events.Input
	if !readJSON(w, r, &in) {
		return
	}
	e, err := s.Events.Create(r.Context(), p.ID, in, s.redactor(r.Context()))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("event.created", "event_id", e.ID, "project_id", p.ID, "event_level", e.Level)
	if silenced := s.applySilence(r.Context(), &e); !silenced {
		s.Dispatcher.Enqueue(e, p)
	}
	if s.EventHub != nil {
		s.EventHub.Publish(e)
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": e.ID, "created_at": e.CreatedAt})
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := events.Filter{ProjectID: q.Get("project"), Level: q.Get("level"), Source: q.Get("source"), Fingerprint: q.Get("fingerprint"), Before: q.Get("before"),
		Since: q.Get("since"), Until: q.Get("until"), Grouped: q.Get("grouped") == "true" || q.Get("grouped") == "1"}
	if f.Level != "" && !levels.Valid(f.Level) {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "level must be one of "+strings.Join(levels.All, ", "))
		return
	}
	if v := q.Get("silenced"); v != "" {
		b := v == "true" || v == "1"
		f.Silenced = &b
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 {
			writeError(w, http.StatusUnprocessableEntity, "invalid", "limit must be a positive integer")
			return
		}
		f.Limit = n
	}
	page, err := s.Events.List(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) getEvent(w http.ResponseWriter, r *http.Request) {
	e, err := s.Events.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) eventDeliveries(w http.ResponseWriter, r *http.Request) {
	if _, err := s.Events.Get(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	d, err := s.Dispatcher.ForEvent(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": d})
}

// applySilence marks e as silenced when a rule matches and reports whether it did.
func (s *Server) applySilence(ctx context.Context, e *events.Event) bool {
	rule, err := s.Silences.Match(ctx, e.ProjectID, e.Fingerprint, e.Title, e.Source)
	if err != nil {
		s.Log.Error("silence.match_failed", "error", err.Error())
		return false
	}
	if rule == nil {
		return false
	}
	if err := s.Events.SetSilence(ctx, e.ID, rule.ID); err != nil {
		s.Log.Error("silence.record_failed", "error", err.Error())
	}
	e.SilenceID, e.Silenced = rule.ID, true
	s.Log.Info("event.silenced", "event_id", e.ID, "silence_id", rule.ID)
	return true
}

// ---- silences ----

// unsilenceEvent clears the flag and pushes the event now.
func (s *Server) unsilenceEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, err := s.Events.Get(ctx, r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if !e.Silenced {
		writeError(w, http.StatusUnprocessableEntity, "not_silenced", "this event was not silenced")
		return
	}
	p, err := s.Projects.Get(ctx, e.ProjectID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.Events.ClearSilence(ctx, e.ID); err != nil {
		s.fail(w, err)
		return
	}
	e.SilenceID, e.Silenced = "", false
	dl := s.Dispatcher.Deliver(ctx, e, p)
	if dl == nil {
		dl = []delivery.Delivery{}
	}
	s.Log.Info("event.unsilenced", "event_id", e.ID)
	writeJSON(w, http.StatusOK, map[string]any{"event": e, "deliveries": dl})
}

func (s *Server) getSilence(w http.ResponseWriter, r *http.Request) {
	sil, err := s.Silences.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sil)
}

func (s *Server) listSilences(w http.ResponseWriter, r *http.Request) {
	list, err := s.Silences.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	n, err := s.Events.CountSilenced(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"silences": list, "fields": silences.Fields, "silenced_events": n})
}

func (s *Server) createSilence(w http.ResponseWriter, r *http.Request) {
	var in silences.Input
	if !readJSON(w, r, &in) {
		return
	}
	sil, err := s.Silences.Create(r.Context(), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("silence.created", "silence_id", sil.ID, "field", sil.Field)
	writeJSON(w, http.StatusCreated, sil)
}

func (s *Server) deleteSilence(w http.ResponseWriter, r *http.Request) {
	if err := s.Silences.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("silence.deleted", "silence_id", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// ---- projects ----

func (s *Server) projectIcons(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"shapes": projects.IconShapes, "colors": projects.IconColors})
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Projects.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": ps})
}

type projectCreated struct {
	projects.Project
	APIKey string `json:"api_key"`
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in projects.Input
	if !readJSON(w, r, &in) {
		return
	}
	p, key, err := s.Projects.Create(r.Context(), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("project.created", "project_id", p.ID, "slug", p.Slug)
	writeJSON(w, http.StatusCreated, projectCreated{Project: p, APIKey: key})
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.Projects.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	var in projects.Input
	if !readJSON(w, r, &in) {
		return
	}
	p, err := s.Projects.Update(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	if err := s.Projects.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("project.deleted", "project_id", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) rotateProjectKey(w http.ResponseWriter, r *http.Request) {
	key, err := s.Projects.RotateKey(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	p, err := s.Projects.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("project.key_rotated", "project_id", p.ID)
	writeJSON(w, http.StatusOK, projectCreated{Project: p, APIKey: key})
}

// ---- devices ----

func (s *Server) registerDevice(w http.ResponseWriter, r *http.Request) {
	d := r.Context().Value(ctxDevice).(devices.Device)
	var in devices.Registration
	if !readJSON(w, r, &in) {
		return
	}
	d, err := s.Devices.Register(r.Context(), d.ID, in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("device.registered", "device_id", d.ID, "push_registered", d.HasToken)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request) {
	var in devices.Registration
	if !readJSON(w, r, &in) {
		return
	}
	d, err := s.Devices.Register(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	if err := s.Devices.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("device.removed", "device_id", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	ds, err := s.Devices.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": ds})
}

// ---- pairing ----

type pairingCreated struct {
	pairing.Token
	QR pairing.QRPayload `json:"qr"`
}

func (s *Server) createPairing(w http.ResponseWriter, r *http.Request) {
	t, err := s.Pairing.Create(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("pairing.created", "pairing_id", t.ID)
	writeJSON(w, http.StatusCreated, pairingCreated{Token: t, QR: pairing.QRPayload{Version: 1, Server: s.baseURL(r), Token: t.Raw}})
}

func (s *Server) listPairing(w http.ResponseWriter, r *http.Request) {
	ts, err := s.Pairing.Pending(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pairing_tokens": ts})
}

func (s *Server) revokePairing(w http.ResponseWriter, r *http.Request) {
	if err := s.Pairing.Revoke(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) exchangePairing(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	res, err := s.Pairing.Exchange(r.Context(), in.Token, in.Name, in.Platform)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("pairing.completed", "device_id", res.Device.ID)
	writeJSON(w, http.StatusCreated, res)
}

// ---- test notification ----

func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		ProjectID string `json:"project_id"`
	}
	if r.ContentLength != 0 {
		if !readJSON(w, r, &in) {
			return
		}
	}
	var p projects.Project
	var err error
	if in.ProjectID != "" {
		p, err = s.Projects.Get(ctx, in.ProjectID)
	} else {
		p, err = s.Projects.First(ctx)
		if errors.Is(err, projects.ErrNotFound) {
			writeError(w, http.StatusUnprocessableEntity, "no_project", "create a project before sending a test notification")
			return
		}
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	e, err := s.Events.Create(ctx, p.ID, events.Input{
		Title: "Test boop", Body: "If you can read this on your phone, Boop is working.", Level: levels.Success, Source: "boop",
		Data: json.RawMessage(`{"test":true}`),
	}, s.redactor(ctx))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("event.created", "event_id", e.ID, "project_id", p.ID, "event_level", e.Level, "test", true)
	var dl []delivery.Delivery
	if !s.applySilence(ctx, &e) {
		dl = s.Dispatcher.Deliver(ctx, e, p)
	}
	if dl == nil {
		dl = []delivery.Delivery{}
	}
	if s.EventHub != nil {
		s.EventHub.Publish(e)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"event": e, "deliveries": dl, "apns_configured": s.APNS != nil})
}

// ---- live stream (SSE) ----

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if s.EventHub == nil {
		s.EventHub = NewEventHub()
	}
	ch := s.EventHub.Subscribe()
	defer s.EventHub.Unsubscribe(ch)

	_, _ = w.Write([]byte("event: ping\ndata: {}\n\n"))
	flusher.Flush()

	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(e)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: event\ndata: %s\n\n", b)
			flusher.Flush()
		}
	}
}

// ---- web push ----

func (s *Server) getVAPIDKey(w http.ResponseWriter, r *http.Request) {
	if s.WebPush == nil || s.WebPush.PublicKey() == "" {
		writeError(w, http.StatusNotFound, "not_configured", "web push is not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"public_key": s.WebPush.PublicKey(),
	})
}

func (s *Server) subscribeWebPush(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string          `json:"name"`
		Platform     string          `json:"platform"`
		Subscription json.RawMessage `json:"subscription"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Subscription) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "subscription is required")
		return
	}

	subStr := string(in.Subscription)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "PWA Browser"
	}
	platform := strings.TrimSpace(in.Platform)
	if platform == "" {
		platform = "web"
	}

	if dev, ok := r.Context().Value(ctxDevice).(devices.Device); ok {
		reg := devices.Registration{
			Name:        &name,
			Platform:    &platform,
			DeviceToken: &subStr,
		}
		updated, err := s.Devices.Register(r.Context(), dev.ID, reg)
		if err != nil {
			s.fail(w, err)
			return
		}
		s.Log.Info("webpush.updated", "device_id", updated.ID)
		writeJSON(w, http.StatusOK, map[string]any{"device": updated})
		return
	}

	dev, cred, err := s.Devices.Create(r.Context(), name, platform)
	if err != nil {
		s.fail(w, err)
		return
	}
	reg := devices.Registration{
		DeviceToken: &subStr,
	}
	updated, err := s.Devices.Register(r.Context(), dev.ID, reg)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("webpush.subscribed", "device_id", updated.ID, "name", updated.Name)
	writeJSON(w, http.StatusCreated, map[string]any{
		"device":     updated,
		"credential": cred,
	})
}

func (s *Server) unsubscribeWebPush(w http.ResponseWriter, r *http.Request) {
	if dev, ok := r.Context().Value(ctxDevice).(devices.Device); ok {
		_ = s.Devices.ClearToken(r.Context(), dev.ID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if readJSON(w, r, &in) && in.Endpoint != "" {
		_ = s.Devices.DeleteByToken(r.Context(), in.Endpoint)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- email support cards ----

type supportCardInput struct {
	SourceAccount   string `json:"source_account"`
	SourceThreadID  string `json:"source_thread_id"`
	SourceMessageID string `json:"source_message_id"`
	Product         string `json:"product"`
	Title           string `json:"title"`
	Sender          string `json:"sender"`
	ReportedAt      string `json:"reported_at"`
	OriginalText    string `json:"original_text"`
}

func (s *Server) listSupportCards(w http.ResponseWriter, r *http.Request) {
	cards, err := s.Support.ListWithArchived(r.Context(), r.URL.Query().Get("archived") == "true")
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cards": cards})
}

func (s *Server) getSupportBoard(w http.ResponseWriter, r *http.Request) {
	lists, err := s.Support.Lists(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	cards, err := s.Support.ListWithArchived(r.Context(), r.URL.Query().Get("archived") == "true")
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": lists, "cards": cards})
}

func supportMutationError(w http.ResponseWriter, s *Server, err error) {
	switch {
	case errors.Is(err, support.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid_support_input", "check the card or list details and try again")
	case errors.Is(err, support.ErrInUse):
		writeError(w, http.StatusConflict, "support_list_not_empty", "move the cards out of this list before deleting it")
	case errors.Is(err, support.ErrNotFound):
		writeError(w, http.StatusNotFound, "support_item_not_found", "the card, list, or item no longer exists")
	default:
		s.fail(w, err)
	}
}

func (s *Server) listSupportLists(w http.ResponseWriter, r *http.Request) {
	lists, err := s.Support.Lists(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": lists})
}

func (s *Server) createSupportList(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	list, err := s.Support.CreateList(r.Context(), in.Name)
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusCreated, list)
}

func (s *Server) updateSupportList(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     *string `json:"name"`
		Position *int    `json:"position"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if in.Name != nil {
		if _, err := s.Support.RenameList(r.Context(), id, *in.Name); err != nil {
			supportMutationError(w, s, err)
			return
		}
	}
	if in.Position != nil {
		if err := s.Support.MoveList(r.Context(), id, *in.Position); err != nil {
			supportMutationError(w, s, err)
			return
		}
	}
	lists, err := s.Support.Lists(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lists": lists})
}

func (s *Server) deleteSupportList(w http.ResponseWriter, r *http.Request) {
	if err := s.Support.DeleteList(r.Context(), r.PathValue("id")); err != nil {
		supportMutationError(w, s, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createSupportCard(w http.ResponseWriter, r *http.Request) {
	var in support.CardInput
	if !readJSON(w, r, &in) {
		return
	}
	card, err := s.Support.CreateCard(r.Context(), in)
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusCreated, card)
}

func (s *Server) getSupportCard(w http.ResponseWriter, r *http.Request) {
	card, err := s.Support.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) updateSupportCard(w http.ResponseWriter, r *http.Request) {
	var in support.CardEdit
	if !readJSON(w, r, &in) {
		return
	}
	card, err := s.Support.UpdateCard(r.Context(), r.PathValue("id"), in)
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) moveSupportCard(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ListID   string `json:"list_id"`
		Position int    `json:"position"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	card, err := s.Support.MoveCard(r.Context(), r.PathValue("id"), in.ListID, in.Position)
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) archiveSupportCard(w http.ResponseWriter, r *http.Request) {
	card, err := s.Support.SetArchived(r.Context(), r.PathValue("id"), true)
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) restoreSupportCard(w http.ResponseWriter, r *http.Request) {
	card, err := s.Support.SetArchived(r.Context(), r.PathValue("id"), false)
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) addSupportComment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	comment, err := s.Support.AddComment(r.Context(), r.PathValue("id"), in.Body)
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusCreated, comment)
}

func (s *Server) deleteSupportComment(w http.ResponseWriter, r *http.Request) {
	if err := s.Support.DeleteComment(r.Context(), r.PathValue("id"), r.PathValue("comment_id")); err != nil {
		supportMutationError(w, s, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addSupportChecklistItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	item, err := s.Support.AddChecklistItem(r.Context(), r.PathValue("id"), in.Text)
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) updateSupportChecklistItem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Done bool `json:"done"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Support.UpdateChecklistItem(r.Context(), r.PathValue("id"), r.PathValue("item_id"), in.Done); err != nil {
		supportMutationError(w, s, err)
		return
	}
	card, err := s.Support.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) deleteSupportChecklistItem(w http.ResponseWriter, r *http.Request) {
	if err := s.Support.DeleteChecklistItem(r.Context(), r.PathValue("id"), r.PathValue("item_id")); err != nil {
		supportMutationError(w, s, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addSupportAttachments(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body must be 8 MB or smaller")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_multipart", "expected image attachments")
		}
		return
	}
	files, ok := parseSupportImageAttachments(w, r)
	if !ok {
		return
	}
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_attachment", "choose at least one image")
		return
	}
	if err := s.Support.AddAttachments(r.Context(), r.PathValue("id"), files); err != nil {
		supportMutationError(w, s, err)
		return
	}
	card, err := s.Support.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		supportMutationError(w, s, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) deleteSupportAttachment(w http.ResponseWriter, r *http.Request) {
	if err := s.Support.DeleteAttachment(r.Context(), r.PathValue("id"), r.PathValue("attachment_id")); err != nil {
		supportMutationError(w, s, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) supportTriageStatus(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.QueryContext(r.Context(), `SELECT state, COUNT(*) FROM notification_triage GROUP BY state`)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	counts := struct {
		Pending     int    `json:"pending"`
		Processing  int    `json:"processing"`
		Matched     int    `json:"matched"`
		Ignored     int    `json:"ignored"`
		Configured  bool   `json:"configured"`
		ConfigError bool   `json:"configuration_error"`
		Model       string `json:"model"`
	}{}
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			s.fail(w, err)
			return
		}
		switch state {
		case "pending":
			counts.Pending = count
		case "processing":
			counts.Processing = count
		case "matched":
			counts.Matched = count
		case "ignored":
			counts.Ignored = count
		}
	}
	if err := rows.Err(); err != nil {
		s.fail(w, err)
		return
	}
	provider, err := s.Settings.OpenRouterConfig(r.Context(), s.Config.OpenRouterAPIKey, s.Config.OpenRouterModel)
	if err != nil {
		hasSecret, hasErr := s.Settings.HasSecret(r.Context(), settings.KeyOpenRouterAPIKey)
		if hasErr != nil {
			s.fail(w, hasErr)
			return
		}
		counts.ConfigError = hasSecret
	}
	counts.Configured = provider.APIKey != ""
	counts.Model = provider.Model
	writeJSON(w, http.StatusOK, counts)
}

// upsertSupportCard accepts multipart/form-data with a JSON metadata field and
// optional image files. The identity tuple makes Gmail retries idempotent.
func (s *Server) upsertSupportCard(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body must be 8 MB or smaller")
		} else {
			writeError(w, http.StatusBadRequest, "invalid_multipart", "expected multipart form with metadata and image attachments")
		}
		return
	}
	var in supportCardInput
	if err := json.Unmarshal([]byte(r.FormValue("metadata")), &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "metadata must be a JSON object")
		return
	}
	files, ok := parseSupportImageAttachments(w, r)
	if !ok {
		return
	}
	card, err := s.Support.Upsert(r.Context(), support.Input{
		SourceAccount: in.SourceAccount, SourceThreadID: in.SourceThreadID, SourceMessageID: in.SourceMessageID,
		Product: in.Product, Title: in.Title, Sender: in.Sender, ReportedAt: in.ReportedAt, OriginalText: in.OriginalText,
	}, files)
	if errors.Is(err, support.ErrInvalid) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_support_card", "required email identity, product, title, original text, or timestamp is invalid")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func parseSupportImageAttachments(w http.ResponseWriter, r *http.Request) ([]support.Attachment, bool) {
	parts := r.MultipartForm.File["attachments"]
	files := make([]support.Attachment, 0, len(parts))
	var total int64
	for _, part := range parts {
		if total+part.Size > 7<<20 {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "image attachments must total 7 MB or less")
			return nil, false
		}
		total += part.Size
		f, err := part.Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_attachment", "could not open attachment")
			return nil, false
		}
		data, readErr := io.ReadAll(io.LimitReader(f, (7<<20)+1))
		_ = f.Close()
		if readErr != nil || len(data) > 7<<20 {
			writeError(w, http.StatusBadRequest, "invalid_attachment", "could not read attachment or file exceeds the limit")
			return nil, false
		}
		_, format, decodeErr := image.DecodeConfig(bytes.NewReader(data))
		contentTypes := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif"}
		contentType := contentTypes[format]
		if decodeErr != nil || contentType == "" {
			writeError(w, http.StatusUnprocessableEntity, "invalid_attachment", "only valid PNG, JPEG, and GIF images are accepted")
			return nil, false
		}
		filename := filepath.Base(strings.ReplaceAll(part.Filename, string(rune(92)), "/"))
		if filename == "." || filename == "" || len(filename) > 180 {
			filename = "attachment"
		}
		files = append(files, support.Attachment{Filename: filename, ContentType: contentType, Content: data})
	}
	return files, true
}

func (s *Server) setSupportCardStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	card, err := s.Support.SetStatus(r.Context(), r.PathValue("id"), in.Status)
	if errors.Is(err, support.ErrInvalid) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_status", "status must be new, in_progress, blocked, or done")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

func (s *Server) getSupportAttachment(w http.ResponseWriter, r *http.Request) {
	cardID := r.PathValue("id")
	if _, err := s.Support.Get(r.Context(), cardID); err != nil {
		s.fail(w, err)
		return
	}
	attachment, err := s.Support.Attachment(r.Context(), cardID, r.PathValue("attachment_id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", attachment.ContentType)
	w.Header().Set("Content-Disposition", `inline; filename="image"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(attachment.Content)
}
