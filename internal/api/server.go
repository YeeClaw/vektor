package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"forge.coltco.net/austin/vektor/internal/auth"
)

type Server struct {
	db    *sql.DB
	auth  *auth.Auth
	local *auth.Local
	mux   *http.ServeMux
}

func NewServer(db *sql.DB, a *auth.Auth, l *auth.Local) *Server {
	s := &Server{
		db:    db,
		auth:  a,
		local: l,
		mux:   http.NewServeMux(),
	}

	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Auth routes (unauthenticated)
	if s.local != nil {
		s.mux.HandleFunc("POST /auth/register", s.local.RegisterHandler())
		s.mux.HandleFunc("POST /auth/login", s.local.LoginHandler())
	} else {
		s.mux.HandleFunc("GET /auth/login", s.auth.LoginHandler())
		s.mux.HandleFunc("GET /auth/callback", s.auth.CallbackHandler())
	}

	// API routes (authenticated)
	api := http.NewServeMux()
	api.HandleFunc("GET /api/projects", s.handleListProjects)
	api.HandleFunc("POST /api/projects", s.handleCreateProject)
	api.HandleFunc("GET /api/projects/{projectKey}/issues", s.handleListIssues)
	api.HandleFunc("POST /api/projects/{projectKey}/issues", s.handleCreateIssue)
	api.HandleFunc("PATCH /api/issues/{id}", s.handleUpdateIssue)
	api.HandleFunc("GET /api/me", s.handleMe)

	s.mux.Handle("/api/", auth.Middleware(api))
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, claims)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
