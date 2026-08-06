package authn

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// discardLogger is for tests that need a working logger but do not assert on its
// output. A SessionManager with a nil log panics the moment Middleware runs, so
// every constructed authenticator needs one of these.
func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// logCapture collects JSON log records so tests can assert on them. Not safe for
// concurrent use, which is fine: every test here drives one request at a time.
type logCapture struct {
	buf bytes.Buffer
}

// newLogCapture returns a logger writing JSON into the capture. The level is
// LevelDebug so the DebugContext line on the missing-cookie path is visible;
// production defaults would silently drop it.
func newLogCapture() (*slog.Logger, *logCapture) {
	c := &logCapture{}
	h := slog.NewJSONHandler(&c.buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), c
}

// records decodes every line written so far. Decoding into a map rather than a
// struct keeps the assertions honest: a test can only read a key the handler
// actually emitted.
func (c *logCapture) records(t *testing.T) []map[string]any {
	t.Helper()

	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(c.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not valid JSON: %v (line: %q)", err, line)
		}
		out = append(out, rec)
	}
	return out
}

// only returns the single record written, failing if there was not exactly one.
func (c *logCapture) only(t *testing.T) map[string]any {
	t.Helper()

	recs := c.records(t)
	if len(recs) != 1 {
		t.Fatalf("got %d log records, want exactly 1:\n%s", len(recs), c.buf.String())
	}
	return recs[0]
}

// newCapturingManager is newTestManager plus a logger whose output the test can
// read back.
func newCapturingManager(t *testing.T, secret string) (*SessionManager, *logCapture) {
	t.Helper()

	logger, capture := newLogCapture()
	return &SessionManager{SessionSecret: []byte(secret), log: logger}, capture
}

// serveThroughMiddleware runs one request carrying cookie (nil means none) and
// returns the recorder. The wrapped handler is a spyHandler so callers can still
// assert the request was blocked.
func serveThroughMiddleware(sm *SessionManager, cookie *http.Cookie) (*httptest.ResponseRecorder, *spyHandler) {
	spy := &spyHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()

	sm.Middleware(spy).ServeHTTP(rec, req)
	return rec, spy
}

// TestMiddlewareLogsRejectionReason is the counterpart to
// TestMiddlewareRejectsUniformly: the client cannot tell these cases apart, but
// the operator must be able to. It pins both the reason and the severity, since
// the whole point of the switch in Middleware is that a forged signature is not
// the same event as an expired session.
func TestMiddlewareLogsRejectionReason(t *testing.T) {
	sm := newTestManager(t, testSecret)
	other := newTestManager(t, altSecret)

	valid := mustCreateToken(t, sm, &Claims{Sub: "u1"}, time.Hour)
	payload := payloadOf(t, valid)
	expired := mustCreateToken(t, sm, &Claims{Sub: "u1"}, -time.Minute)
	foreign := mustCreateToken(t, other, &Claims{Sub: "u1"}, time.Hour)

	tests := []struct {
		name      string
		cookie    *http.Cookie // nil means: send no cookie at all
		wantMsg   string
		wantLevel string
		wantError error // nil means: do not assert on the error attr
	}{
		// No cookie at all is ordinary unauthenticated traffic, not an event --
		// hence DEBUG, so it does not drown the interesting lines at INFO.
		{"no cookie", nil, "invalid cookie", "DEBUG", nil},
		{"wrong cookie name", &http.Cookie{Name: "some_other_cookie", Value: valid}, "invalid cookie", "DEBUG", nil},

		// Structural failures: junk from an unauthenticated client. Routine.
		{"malformed token", sessionCookie("nodotshere"), "session rejected", "INFO", ErrSessionFormat},
		{"signature is not base64", sessionCookie(payload + ".!!!"), "session rejected", "INFO", ErrSignatureEncoding},
		{"expired token", sessionCookie(expired), "session rejected", "INFO", ErrSessionExpired},

		// A signature that does not verify is a forgery attempt or a key mismatch.
		{"wrong signature", sessionCookie(payload + "." + base64.RawURLEncoding.EncodeToString([]byte("wrong signature bytes"))),
			"session rejected", "WARN", ErrSessionSignature},
		{"token signed with another key", sessionCookie(foreign), "session rejected", "WARN", ErrSessionSignature},

		// These two are only reachable *after* a valid HMAC, so something holding
		// the signing key emitted an unusable payload. Loudest case in the path.
		{"signed payload is not base64", sessionCookie(mintRaw(testSecret, "!!! not base64 !!!")),
			"session rejected", "ERROR", ErrPayloadEncoding},
		{"signed payload is not JSON", sessionCookie(mintRaw(testSecret, base64.RawURLEncoding.EncodeToString([]byte("not json")))),
			"session rejected", "ERROR", ErrSessionData},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, capture := newCapturingManager(t, testSecret)

			rec, spy := serveThroughMiddleware(sm, tt.cookie)

			// The response half of the promise still has to hold.
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if spy.called {
				t.Error("wrapped handler ran, want the request blocked")
			}

			got := capture.only(t)
			if got["msg"] != tt.wantMsg {
				t.Errorf("msg = %v, want %q", got["msg"], tt.wantMsg)
			}
			if got["level"] != tt.wantLevel {
				t.Errorf("level = %v, want %q", got["level"], tt.wantLevel)
			}
			if tt.wantError != nil && got["error"] != tt.wantError.Error() {
				t.Errorf("error = %v, want %q", got["error"], tt.wantError)
			}
			// Every rejection needs enough context to be actionable.
			if got["path"] != "/api/me" {
				t.Errorf("path = %v, want %q", got["path"], "/api/me")
			}
			if got["remote"] == nil || got["remote"] == "" {
				t.Errorf("remote = %v, want a non-empty address", got["remote"])
			}
		})
	}
}

// TestMiddlewareNeverLogsCookieValue is the regression guard that matters most
// here. A session token is a bearer credential: anything that writes one into a
// log file hands out live sessions to whoever can read that file.
func TestMiddlewareNeverLogsCookieValue(t *testing.T) {
	signer := newTestManager(t, testSecret)
	valid := mustCreateToken(t, signer, &Claims{Sub: "u1", Email: "user@example.com"}, time.Hour)
	expired := mustCreateToken(t, signer, &Claims{Sub: "u1"}, -time.Minute)

	tests := []struct {
		name  string
		token string
	}{
		{"valid token", valid},
		{"expired token", expired},
		{"forged signature", payloadOf(t, valid) + ".!!!"},
		{"signed garbage", mintRaw(testSecret, "!!! not base64 !!!")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, capture := newCapturingManager(t, testSecret)

			serveThroughMiddleware(sm, sessionCookie(tt.token))

			if strings.Contains(capture.buf.String(), tt.token) {
				t.Errorf("session token leaked into logs:\n%s", capture.buf.String())
			}
			// The payload half alone is enough to be worth withholding: it carries
			// the claims, and pairing it with a signature is the attacker's job.
			if payload, _, found := strings.Cut(tt.token, "."); found && payload != "" {
				if strings.Contains(capture.buf.String(), payload) {
					t.Errorf("token payload leaked into logs:\n%s", capture.buf.String())
				}
			}
		})
	}
}

// TestClaimsLogValueOmitsPII pins Claims.LogValue. Without it, logging a Claims
// anywhere writes the user's email and name into every sink the logs reach; with
// it, the redaction is structural rather than a rule everyone has to remember.
func TestClaimsLogValueOmitsPII(t *testing.T) {
	logger, capture := newLogCapture()
	claims := Claims{Sub: "u1", Email: "user@example.com", Name: "Austin"}

	logger.Info("upserting user failed", "user", claims)

	got := capture.only(t)
	user, ok := got["user"].(map[string]any)
	if !ok {
		t.Fatalf("user attr = %#v, want a group object", got["user"])
	}
	if user["sub"] != claims.Sub {
		t.Errorf("user.sub = %v, want %q", user["sub"], claims.Sub)
	}
	if len(user) != 1 {
		t.Errorf("user group = %#v, want only the sub key", user)
	}
	for _, pii := range []string{claims.Email, claims.Name} {
		if strings.Contains(capture.buf.String(), pii) {
			t.Errorf("PII %q leaked into logs:\n%s", pii, capture.buf.String())
		}
	}
}
