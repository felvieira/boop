package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chrisgreg/boop/server/internal/ids"
)

// SessionCookie is the name of the admin session cookie.
const SessionCookie = "boop_session"

// SessionTTL is how long an admin login lasts.
const SessionTTL = 30 * 24 * time.Hour

// Admin guards the web UI and admin endpoints with a single username/password
// from the environment. When Enabled is false everything is open (v1 default).
type Admin struct {
	username string
	password string // stored hashed
	enabled  bool

	mu       sync.Mutex
	sessions map[string]time.Time // token hash -> expiry
	now      func() time.Time
}

// NewAdmin returns an Admin; it is enabled only when both values are non-empty.
func NewAdmin(username, password string) *Admin {
	a := &Admin{sessions: map[string]time.Time{}, now: time.Now}
	if username != "" && password != "" {
		a.enabled = true
		a.username = username
		a.password = Hash(password)
	}
	return a
}

// Enabled reports whether admin authentication is configured.
func (a *Admin) Enabled() bool { return a != nil && a.enabled }

// Check verifies a username/password pair in constant time.
func (a *Admin) Check(username, password string) bool {
	if !a.Enabled() {
		return false
	}
	u := subtle.ConstantTimeCompare(hashBytes(username), hashBytes(a.username)) == 1
	p := subtle.ConstantTimeCompare([]byte(Hash(password)), []byte(a.password)) == 1
	return u && p
}

// Login creates a session and returns its raw token.
func (a *Admin) Login(username, password string) (string, bool) {
	if !a.Check(username, password) {
		return "", false
	}
	tok := NewSecret("boop_sess")
	a.mu.Lock()
	a.sessions[Hash(tok)] = a.now().Add(SessionTTL)
	a.mu.Unlock()
	return tok, true
}

// Logout revokes a session token.
func (a *Admin) Logout(token string) {
	a.mu.Lock()
	delete(a.sessions, Hash(token))
	a.mu.Unlock()
}

// Valid reports whether a raw session token is live.
func (a *Admin) Valid(token string) bool {
	if token == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.sessions[Hash(token)]
	if !ok {
		return false
	}
	if a.now().After(exp) {
		delete(a.sessions, Hash(token))
		return false
	}
	return true
}

// Authorized reports whether r carries a valid session cookie or HTTP Basic
// credentials. Always true when auth is not enabled.
func (a *Admin) Authorized(r *http.Request) bool {
	if !a.Enabled() {
		return true
	}
	if c, err := r.Cookie(SessionCookie); err == nil && a.Valid(c.Value) {
		return true
	}
	if u, p, ok := r.BasicAuth(); ok && a.Check(u, p) {
		return true
	}
	return false
}

// Actor returns the authenticated admin name for audit records.
func (a *Admin) Actor(r *http.Request) string {
	if a == nil || !a.Authorized(r) {
		return ""
	}
	if username, password, ok := r.BasicAuth(); ok && a.Check(username, password) {
		return username
	}
	if a.username != "" {
		return a.username
	}
	return "Admin"
}

// SetCookie writes the session cookie for token onto w.
func (a *Admin) SetCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r), MaxAge: int(SessionTTL.Seconds()),
	})
}

// ClearCookie removes the session cookie.
func (a *Admin) ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r), MaxAge: -1})
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func hashBytes(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

var _ = ids.Now
