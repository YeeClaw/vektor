package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"forge.coltco.net/austin/vektor/internal/authn"
)

type Server struct {
	db      *sql.DB
	authn   authn.Authenticator
	mux     *http.ServeMux
	handler http.Handler
	log     *slog.Logger
}

func NewServer(db *sql.DB, a authn.Authenticator, logger *slog.Logger) *Server {
	s := &Server{
		db:    db,
		authn: a,
		mux:   http.NewServeMux(),
		log:   logger.With("component", "api"),
	}

	s.routes()
	s.handler = s.withRequestLogging(s.mux)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
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

func (s *Server) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		s.log.LogAttrs(r.Context(), slog.LevelInfo, "request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real writer, so Flush/Hijack
// still work through this wrapper.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
