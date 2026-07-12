package authn

import "net/http"

type Authenticator interface {
	RegisterPublicRoutes(mux *http.ServeMux)
	Middleware(next http.Handler) http.Handler
}

var _ Authenticator = (*OIDC)(nil)
var _ Authenticator = (*Local)(nil)
