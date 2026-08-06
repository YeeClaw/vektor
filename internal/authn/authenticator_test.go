package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// requireRoute asserts that (method, path) resolves to exactly the pattern want.
//
// This uses mux.Handler for lookup rather than mux.ServeHTTP because lookup does
// not invoke the handler -- these authenticators are built with a nil *sql.DB, so
// actually running a handler would panic. It is also the stronger assertion:
// ServeHTTP would only tell us "not a 404", while Handler names the pattern that
// matched.
//
// This is the shape that catches issue #1. The pattern "GET auth/login", missing
// its leading slash, parses as host "auth" + path "/login", so a request to
// /auth/login resolves to the empty pattern and this fails immediately.
func requireRoute(t *testing.T, mux *http.ServeMux, method, path, want string) {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	_, pattern := mux.Handler(req)
	if pattern != want {
		if pattern == "" {
			t.Errorf("%s %s matched no route, want pattern %q", method, path, want)
			return
		}
		t.Errorf("%s %s matched pattern %q, want %q", method, path, pattern, want)
	}
}

// requireStatus serves a request and asserts on the status code. Only safe for
// requests expected NOT to reach a handler (404 and 405 are produced by the mux
// itself), which is all this file needs it for.
func requireStatus(t *testing.T, mux *http.ServeMux, method, path string, want int) {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Errorf("%s %s status = %d, want %d", method, path, rec.Code, want)
	}
}

// newTestMux registers a's public routes on a fresh mux. Each test gets its own
// mux: ServeMux panics on conflicting patterns, so Local and OIDC (which both
// claim /auth/login) must never share one.
func newTestMux(t *testing.T, a Authenticator) *http.ServeMux {
	t.Helper()

	mux := http.NewServeMux()
	a.RegisterPublicRoutes(mux)
	return mux
}

// TestPublicRoutesAreRootRelative is the blanket guard against the issue #1 bug
// class across every Authenticator implementation: whatever routes an
// implementation registers, none of them may be scoped to a host. Adding a new
// Authenticator to this list gets it checked for free.
func TestPublicRoutesAreRootRelative(t *testing.T) {
	tests := []struct {
		name string
		auth Authenticator
		// paths every implementation must expose, with the method it answers on
		routes map[string]string // path -> method
	}{
		{
			name:   "Local",
			auth:   NewLocal([]byte(testSecret), nil, discardLogger()),
			routes: map[string]string{"/auth/register": http.MethodPost, "/auth/login": http.MethodPost},
		},
		{
			// The zero value is enough: RegisterPublicRoutes reads no OIDC state,
			// and NewOIDC would require live provider discovery over the network.
			name:   "OIDC",
			auth:   &OIDC{},
			routes: map[string]string{"/auth/login": http.MethodGet, "/auth/callback": http.MethodGet},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTestMux(t, tt.auth)

			for path, method := range tt.routes {
				req := httptest.NewRequest(method, path, nil)
				if _, pattern := mux.Handler(req); pattern == "" {
					t.Errorf("%s %s matched no route; is the pattern missing its leading %q?",
						method, path, "/")
				}
			}
		})
	}
}
