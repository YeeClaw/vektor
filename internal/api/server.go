package api

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"forge.coltco.net/austin/vektor/internal/authn"
)

type Server struct {
	db    *sql.DB
	authn authn.Authenticator
	mux   *http.ServeMux
}

func NewServer(db *sql.DB, a authn.Authenticator) *Server {
	s := &Server{
		db:    db,
		authn:  a,
		mux:   http.NewServeMux(),
	}

	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Unauthenticated routes (such as ogin and what not)
	s.authn.RegisterPublicRoutes(s.mux)

	// API routes (authenticated)
	api := http.NewServeMux()
	api.HandleFunc("GET /api/projects", s.handleListProjects)
	api.HandleFunc("POST /api/projects", s.handleCreateProject)
	api.HandleFunc("GET /api/projects/{projectKey}/issues", s.handleListIssues)
	api.HandleFunc("POST /api/projects/{projectKey}/issues", s.handleCreateIssue)
	api.HandleFunc("PATCH /api/issues/{id}", s.handleUpdateIssue)
	api.HandleFunc("GET /api/me", s.handleMe)

	s.mux.Handle("/api/", s.authn.Middleware(api))
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := authn.UserFromContext(r.Context())
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
