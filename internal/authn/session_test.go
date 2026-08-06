package authn

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Two distinct keys: altSecret exists so rotation can be tested, i.e. that a
// token signed under an old key is rejected once the key changes.
const (
	testSecret = "test-secret"
	altSecret  = "a-different-secret"
)

func newTestManager(t *testing.T, secret string) *SessionManager {
	t.Helper()
	// Middleware logs on every rejection path, so a nil logger panics. Tests that
	// assert on log output use newCapturingManager instead.
	return &SessionManager{SessionSecret: []byte(secret), log: discardLogger()}
}

// mustCreateToken mints a valid token or fails the test. Used for setup, where a
// signing failure means the test itself is broken, not the code under test.
func mustCreateToken(t *testing.T, sm *SessionManager, claims *Claims, ttl time.Duration) string {
	t.Helper()
	token, err := sm.CreateSessionToken(claims, ttl)
	if err != nil {
		t.Fatalf("CreateSessionToken(%+v, %v): unexpected error: %v", claims, ttl, err)
	}
	return token
}

// mintRaw signs an arbitrary payload string with secret, producing a token whose
// signature is valid but whose payload need not be.
//
// CreateSessionToken can only ever emit base64-encoded JSON, so it cannot reach
// ValidateSession's "invalid session encoding" and "invalid session data"
// branches. Forging a signature is the only way in, and that requires the key --
// which is exactly why these tests live in package authn rather than authn_test.
// It also documents a real property: in production those two branches are
// unreachable without having already broken the HMAC.
func mintRaw(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// payloadOf returns the payload half of a token.
func payloadOf(t *testing.T, token string) string {
	t.Helper()
	payload, _, found := strings.Cut(token, ".")
	if !found {
		t.Fatalf("token %q has no %q separator", token, ".")
	}
	return payload
}

func TestCreateSessionTokenFormat(t *testing.T) {
	sm := newTestManager(t, testSecret)
	claims := &Claims{Sub: "u1", Email: "a@b.c", Name: "Austin"}

	before := time.Now()
	token := mustCreateToken(t, sm, claims, time.Hour)

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("token has %d dot-separated parts, want 2: %q", len(parts), token)
	}

	// Both halves must be raw (unpadded) base64url. Issue #2 shipped a bug where
	// signing used RawURLEncoding and validation used padded URLEncoding, so the
	// encoding is pinned on both halves deliberately.
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("payload is not raw base64url: %v", err)
	}
	if _, err := base64.RawURLEncoding.DecodeString(parts[1]); err != nil {
		t.Fatalf("signature is not raw base64url: %v", err)
	}
	if strings.Contains(token, "=") {
		t.Errorf("token contains padding, want raw (unpadded) encoding: %q", token)
	}

	var session struct {
		Claims Claims `json:"claims"`
		Exp    int64  `json:"exp"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatalf("payload is not valid session JSON: %v", err)
	}
	if session.Claims != *claims {
		t.Errorf("payload claims = %+v, want %+v", session.Claims, *claims)
	}

	// Exp is a whole-second Unix timestamp, so allow a one-second window rather
	// than demanding an exact match.
	wantExp := before.Add(time.Hour).Unix()
	if session.Exp < wantExp || session.Exp > wantExp+1 {
		t.Errorf("payload exp = %d, want within [%d, %d]", session.Exp, wantExp, wantExp+1)
	}
}

// TestSessionRoundTrip is the compatibility test: whatever CreateSessionToken
// emits, ValidateSession must accept and decode back to the same claims. Both
// bugs in issue #2 were failures of the two functions to agree with each other,
// which is exactly what this catches.
func TestSessionRoundTrip(t *testing.T) {
	sm := newTestManager(t, testSecret)

	tests := []struct {
		name   string
		claims *Claims
	}{
		{"populated claims", &Claims{Sub: "u1", Email: "a@b.c", Name: "Austin"}},
		// The zero value: a session carrying no claims is still structurally
		// valid and must survive the round trip.
		{"empty claims", &Claims{}},
		// JSON escaping and base64 are where non-ASCII and reserved characters
		// break, and the payload passes through both.
		{"unicode and reserved characters", &Claims{
			Sub:   "u|2",
			Email: "austin+vektor@example.com",
			Name:  `Ævar "Ørn" 日本 <>&`,
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := mustCreateToken(t, sm, tt.claims, time.Hour)

			got, err := sm.ValidateSession(token)
			if err != nil {
				t.Fatalf("ValidateSession: unexpected error: %v", err)
			}
			if *got != *tt.claims {
				t.Errorf("claims = %+v, want %+v", *got, *tt.claims)
			}
		})
	}
}

func TestValidateSessionRejectsMalformedToken(t *testing.T) {
	sm := newTestManager(t, testSecret)
	valid := mustCreateToken(t, sm, &Claims{Sub: "u1"}, time.Hour)
	payload := payloadOf(t, valid)

	// want pins which branch rejects each token, not merely that one did. Three
	// distinct sentinels appear here, and which one fires is not always obvious
	// from the input -- see the per-case notes.
	tests := []struct {
		name  string
		token string
		want  error
	}{
		{"empty token", "", ErrSessionFormat},
		{"no separator", "nodotshere", ErrSessionFormat},
		{"payload only, no separator", payload, ErrSessionFormat},
		// These two reach the HMAC comparison rather than failing earlier: an
		// empty signature half is valid (if empty) base64, so validation gets as
		// far as hmac.Equal and fails there against a 32-byte digest.
		{"separator but empty signature", payload + ".", ErrSessionSignature},
		{"empty payload and signature", ".", ErrSessionSignature},
		{"signature is not base64", payload + ".!!!not-base64!!!", ErrSignatureEncoding},
		// SplitN(token, ".", 2) keeps every extra dot inside the signature half,
		// so a JWT-shaped three-part token fails to base64-decode. Pinning this
		// documents that Vektor tokens are two-part, not JWTs.
		{"extra separator", valid + ".extra", ErrSignatureEncoding},
		{"leading separator", "." + valid, ErrSignatureEncoding},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := sm.ValidateSession(tt.token)
			if !errors.Is(err, tt.want) {
				t.Errorf("ValidateSession(%q) error = %v, want %v", tt.token, err, tt.want)
			}
			if claims != nil {
				t.Errorf("ValidateSession(%q) returned claims %+v, want nil", tt.token, claims)
			}
		})
	}
}

// TestValidateSessionRejectsSignedGarbage covers the two branches reachable only
// with the signing key: a correctly signed payload that is not base64, and one
// that is base64 but not valid session JSON.
func TestValidateSessionRejectsSignedGarbage(t *testing.T) {
	sm := newTestManager(t, testSecret)

	// Every case here must fail *after* the signature check. Asserting the
	// sentinel is what proves that: a regression that rejected these at the HMAC
	// comparison instead would still produce an error, and the old err != nil
	// assertion would not have noticed.
	tests := []struct {
		name    string
		payload string
		want    error
	}{
		{"payload is not base64", "!!! not base64 !!!", ErrPayloadEncoding},
		{"payload is base64 but not JSON", base64.RawURLEncoding.EncodeToString([]byte("plain text, not json")), ErrSessionData},
		{"payload is base64 JSON but not an object", base64.RawURLEncoding.EncodeToString([]byte(`["an","array"]`)), ErrSessionData},
		// An empty payload signs and decodes cleanly; it dies at json.Unmarshal
		// on empty input, so this is a data failure rather than an encoding one.
		{"payload is empty", "", ErrSessionData},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := mintRaw(testSecret, tt.payload)

			claims, err := sm.ValidateSession(token)
			if !errors.Is(err, tt.want) {
				t.Errorf("ValidateSession(%q) error = %v, want %v", token, err, tt.want)
			}
			if claims != nil {
				t.Errorf("ValidateSession(%q) returned claims %+v, want nil", token, claims)
			}
		})
	}
}

// TestValidateSessionRejectsTamperedPayload is the test that actually pins the
// signature check. Delete the hmac.Equal call in ValidateSession and every
// round-trip test still passes -- only this one fails.
func TestValidateSessionRejectsTamperedPayload(t *testing.T) {
	sm := newTestManager(t, testSecret)
	token := mustCreateToken(t, sm, &Claims{Sub: "u1", Email: "user@example.com"}, time.Hour)

	payload, signature, _ := strings.Cut(token, ".")
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decoding payload: %v", err)
	}

	var session map[string]any
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatalf("unmarshaling payload: %v", err)
	}

	// Privilege escalation attempt: rewrite the identity, keep the old signature.
	claims, ok := session["claims"].(map[string]any)
	if !ok {
		t.Fatalf("payload %s has no claims object", data)
	}
	claims["sub"] = "admin"
	claims["email"] = "admin@example.com"

	tampered, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("remarshaling payload: %v", err)
	}
	forged := base64.RawURLEncoding.EncodeToString(tampered) + "." + signature

	got, err := sm.ValidateSession(forged)
	// ErrSessionSignature specifically: the tampered payload is still well-formed
	// base64 JSON, so a build that dropped the HMAC check would reject it later
	// (or not at all) and a bare err != nil assertion could not tell the
	// difference.
	if !errors.Is(err, ErrSessionSignature) {
		t.Fatalf("ValidateSession(tampered) error = %v, want %v", err, ErrSessionSignature)
	}
	if got != nil {
		t.Errorf("ValidateSession returned claims %+v for a tampered token, want nil", got)
	}
}

// TestValidateSessionRejectsForeignKey covers key rotation: a token minted under
// one secret must not validate under another.
func TestValidateSessionRejectsForeignKey(t *testing.T) {
	signer := newTestManager(t, testSecret)
	validator := newTestManager(t, altSecret)

	token := mustCreateToken(t, signer, &Claims{Sub: "u1"}, time.Hour)

	// Sanity check: the token is valid under the key that signed it, so a failure
	// below is genuinely about the key and not a malformed token.
	if _, err := signer.ValidateSession(token); err != nil {
		t.Fatalf("token invalid under its own signing key: %v", err)
	}

	got, err := validator.ValidateSession(token)
	if !errors.Is(err, ErrSessionSignature) {
		t.Fatalf("ValidateSession(foreign key) error = %v, want %v", err, ErrSessionSignature)
	}
	if got != nil {
		t.Errorf("ValidateSession returned claims %+v for a foreign-key token, want nil", got)
	}
}

func TestValidateSessionRejectsExpired(t *testing.T) {
	sm := newTestManager(t, testSecret)

	// A negative TTL, not zero: Exp is truncated to whole seconds and compared
	// with >, so a zero TTL stays valid for the remainder of the current second.
	token := mustCreateToken(t, sm, &Claims{Sub: "u1"}, -time.Minute)

	got, err := sm.ValidateSession(token)
	// Expiry is the last check in ValidateSession, so this sentinel also proves
	// the token passed every structural and signature check ahead of it.
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("ValidateSession(expired) error = %v, want %v", err, ErrSessionExpired)
	}
	if got != nil {
		t.Errorf("ValidateSession returned claims %+v for an expired token, want nil", got)
	}
}

func TestValidateSessionAcceptsUnexpired(t *testing.T) {
	sm := newTestManager(t, testSecret)
	token := mustCreateToken(t, sm, &Claims{Sub: "u1"}, 30*time.Second)

	if _, err := sm.ValidateSession(token); err != nil {
		t.Errorf("ValidateSession rejected an unexpired token: %v", err)
	}
}

// sessionCookie builds the cookie Middleware looks for.
func sessionCookie(value string) *http.Cookie {
	return &http.Cookie{Name: "vektor_session", Value: value}
}

// spyHandler records whether it ran and what claims it saw, so tests can assert
// on things that happen inside the wrapped handler.
type spyHandler struct {
	called bool
	claims *Claims
}

func (s *spyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.called = true
	s.claims = UserFromContext(r.Context())
	w.WriteHeader(http.StatusOK)
}

// TestMiddlewareRejectsUniformly checks both halves of the promise: every failure
// mode is a 401, and every failure mode produces the *same* body. A response that
// leaked which check failed would tell an attacker whether a token was expired,
// forged, or merely malformed.
func TestMiddlewareRejectsUniformly(t *testing.T) {
	sm := newTestManager(t, testSecret)
	other := newTestManager(t, altSecret)

	expired := mustCreateToken(t, sm, &Claims{Sub: "u1"}, -time.Minute)
	foreign := mustCreateToken(t, other, &Claims{Sub: "u1"}, time.Hour)
	valid := mustCreateToken(t, sm, &Claims{Sub: "u1"}, time.Hour)

	tests := []struct {
		name   string
		cookie *http.Cookie // nil means: send no cookie at all
	}{
		{"no cookie", nil},
		{"wrong cookie name", &http.Cookie{Name: "some_other_cookie", Value: valid}},
		{"empty cookie value", sessionCookie("")},
		{"malformed token", sessionCookie("nodotshere")},
		{"signature is not base64", sessionCookie(payloadOf(t, valid) + ".!!!")},
		{"wrong signature", sessionCookie(payloadOf(t, valid) + "." + base64.RawURLEncoding.EncodeToString([]byte("wrong signature bytes")))},
		{"expired token", sessionCookie(expired)},
		{"token signed with another key", sessionCookie(foreign)},
	}

	bodies := make([]string, len(tests))
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyHandler{}
			req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()

			sm.Middleware(spy).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			if spy.called {
				t.Error("wrapped handler ran, want the request blocked")
			}
			bodies[i] = rec.Body.String()
		})
	}

	for i, body := range bodies {
		if body != bodies[0] {
			t.Errorf("case %q body = %q, want %q (identical to every other failure)",
				tests[i].name, body, bodies[0])
		}
	}
}

func TestMiddlewarePassesClaimsToHandler(t *testing.T) {
	sm := newTestManager(t, testSecret)
	want := &Claims{Sub: "u1", Email: "a@b.c", Name: "Austin"}
	token := mustCreateToken(t, sm, want, time.Hour)

	spy := &spyHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(sessionCookie(token))
	rec := httptest.NewRecorder()

	sm.Middleware(spy).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %q)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !spy.called {
		t.Fatal("wrapped handler did not run, want it reached with a valid session")
	}
	if spy.claims == nil {
		t.Fatal("UserFromContext returned nil inside the handler, want claims")
	}
	if *spy.claims != *want {
		t.Errorf("claims in context = %+v, want %+v", *spy.claims, *want)
	}
}

// TestMiddlewareDoesNotLeakClaimsAcrossRequests guards the context plumbing: the
// claims must ride on the per-request context, not on shared state.
func TestMiddlewareDoesNotLeakClaimsAcrossRequests(t *testing.T) {
	sm := newTestManager(t, testSecret)
	alice := &Claims{Sub: "alice", Email: "alice@example.com", Name: "Alice"}
	bob := &Claims{Sub: "bob", Email: "bob@example.com", Name: "Bob"}

	for _, want := range []*Claims{alice, bob, alice} {
		spy := &spyHandler{}
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.AddCookie(sessionCookie(mustCreateToken(t, sm, want, time.Hour)))

		sm.Middleware(spy).ServeHTTP(httptest.NewRecorder(), req)

		if spy.claims == nil || *spy.claims != *want {
			t.Errorf("claims in context = %+v, want %+v", spy.claims, want)
		}
	}
}
