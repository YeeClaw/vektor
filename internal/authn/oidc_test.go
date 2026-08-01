package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestOIDC returns an OIDC authenticator suitable for route tests.
//
// NewOIDC performs live provider discovery against the issuer URL, so it cannot
// be used here without standing up a fake provider. RegisterPublicRoutes reads
// none of the fields NewOIDC populates, so the zero value covers everything this
// file asserts. Testing callbackHandler end to end will need the fake provider;
// that is deliberately out of scope for issue #40.
func newTestOIDC(t *testing.T) *OIDC {
	t.Helper()
	return &OIDC{SessionManager: SessionManager{SessionSecret: []byte(testSecret)}}
}

func TestOIDCRegisterPublicRoutes(t *testing.T) {
	mux := newTestMux(t, newTestOIDC(t))

	tests := []struct {
		name        string
		method      string
		path        string
		wantPattern string
	}{
		{"login", http.MethodGet, "/auth/login", "GET /auth/login"},
		{"callback", http.MethodGet, "/auth/callback", "GET /auth/callback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireRoute(t, mux, tt.method, tt.path, tt.wantPattern)
		})
	}
}

// TestOIDCPublicRoutesAreMethodScoped pins that the OIDC routes answer only on
// GET, since both are browser redirect targets.
func TestOIDCPublicRoutesAreMethodScoped(t *testing.T) {
	mux := newTestMux(t, newTestOIDC(t))

	for _, path := range []string{"/auth/login", "/auth/callback"} {
		t.Run(path, func(t *testing.T) {
			for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
				requireStatus(t, mux, method, path, http.StatusMethodNotAllowed)
			}
		})
	}
}

// TestOIDCRegistersNoOtherRoutes guards the blast radius: the OIDC authenticator
// must not claim the local-auth registration path it has no handler for.
func TestOIDCRegistersNoOtherRoutes(t *testing.T) {
	mux := newTestMux(t, newTestOIDC(t))

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/auth/register"},
		{http.MethodGet, "/"},
		{http.MethodGet, "/api/me"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			requireStatus(t, mux, tt.method, tt.path, http.StatusNotFound)
		})
	}
}

// TestOIDCLoginSetsStateCookie exercises the one OIDC handler that needs no
// provider: loginHandler mints a state nonce, stores it in a cookie, and
// redirects. The redirect target depends on oauth2 config the zero value lacks,
// so only the cookie and the status are asserted.
func TestOIDCLoginSetsStateCookie(t *testing.T) {
	a := newTestOIDC(t)

	first := loginStateCookie(t, a)
	if first.Value == "" {
		t.Fatal("state cookie value is empty, want a random nonce")
	}
	if !first.HttpOnly {
		t.Error("state cookie is not HttpOnly, want it inaccessible to scripts")
	}
	if first.MaxAge <= 0 {
		t.Errorf("state cookie MaxAge = %d, want a positive expiry", first.MaxAge)
	}

	// The nonce is the CSRF defence for the callback, so it must differ per
	// request. A fixed value would let an attacker replay a known state.
	second := loginStateCookie(t, a)
	if first.Value == second.Value {
		t.Errorf("two logins produced the same state nonce %q, want distinct values", first.Value)
	}
}

// loginStateCookie drives loginHandler once and returns the vektor_state cookie.
func loginStateCookie(t *testing.T, a *OIDC) *http.Cookie {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()

	a.loginHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == "vektor_state" {
			return c
		}
	}
	t.Fatalf("no %q cookie set; got %v", "vektor_state", rec.Result().Cookies())
	return nil
}
