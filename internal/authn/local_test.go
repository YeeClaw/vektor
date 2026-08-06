package authn

import (
	"net/http"
	"testing"
)

func TestLocalRegisterPublicRoutes(t *testing.T) {
	// A nil *sql.DB is fine: route registration never touches it, and these tests
	// only look routes up rather than serving them.
	mux := newTestMux(t, NewLocal([]byte(testSecret), nil, discardLogger()))

	tests := []struct {
		name        string
		method      string
		path        string
		wantPattern string
	}{
		{"register", http.MethodPost, "/auth/register", "POST /auth/register"},
		{"login", http.MethodPost, "/auth/login", "POST /auth/login"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireRoute(t, mux, tt.method, tt.path, tt.wantPattern)
		})
	}
}

// TestLocalPublicRoutesAreMethodScoped pins that the local routes answer only on
// POST. A 405 means the path exists but the method is wrong; a 404 would mean the
// path is not registered at all, so the distinction is what makes this test say
// something the previous one does not.
func TestLocalPublicRoutesAreMethodScoped(t *testing.T) {
	mux := newTestMux(t, NewLocal([]byte(testSecret), nil, discardLogger()))

	for _, path := range []string{"/auth/register", "/auth/login"} {
		t.Run(path, func(t *testing.T) {
			for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
				requireStatus(t, mux, method, path, http.StatusMethodNotAllowed)
			}
		})
	}
}

// TestLocalRegistersNoOtherRoutes guards the blast radius: the local
// authenticator must not claim paths outside /auth, and must not register the
// OIDC callback it has no handler for.
func TestLocalRegistersNoOtherRoutes(t *testing.T) {
	mux := newTestMux(t, NewLocal([]byte(testSecret), nil, discardLogger()))

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/auth/callback"},
		{http.MethodGet, "/"},
		{http.MethodGet, "/api/me"},
		{http.MethodPost, "/auth/logout"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			requireStatus(t, mux, tt.method, tt.path, http.StatusNotFound)
		})
	}
}
