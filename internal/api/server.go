package api

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"forge.coltco.net/austin/vektor/internal/auth"
)

type Server struct {
	db   *sql.DB
	auth *auth.Auth
	mux  *http.ServeMux
}

func NewServer(db *sql.DB, a *auth.Auth) *Server {
	s := &Server{
		db:   db,
		auth: a,
		mux:  http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Auth routes (unauthenticated)
	s.mux.HandleFunc("GET /auth/login", s.auth.LoginHandler())
	s.mux.HandleFunc("GET /auth/callback", s.auth.CallbackHandler(s.handleAuthCallback))

	// API routes (authenticated)
	api := http.NewServeMux()
	api.HandleFunc("GET /api/projects", s.handleListProjects)
	api.HandleFunc("POST /api/projects", s.handleCreateProject)
	api.HandleFunc("GET /api/projects/{projectKey}/issues", s.handleListIssues)
	api.HandleFunc("POST /api/projects/{projectKey}/issues", s.handleCreateIssue)
	api.HandleFunc("PATCH /api/issues/{id}", s.handleUpdateIssue)
	api.HandleFunc("GET /api/me", s.handleMe)

	s.mux.Handle("/api/", s.auth.Middleware(api))
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request, claims *auth.Claims) {
	// Upsert user in DB
	_, err := s.db.ExecContext(r.Context(),
		`INSERT INTO users (id, email, name) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET email=excluded.email, name=excluded.name`,
		claims.Sub, claims.Email, claims.Name,
	)
	if err != nil {
		log.Printf("error upserting user: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	token, err := auth.CreateSessionToken(claims, 7*24*time.Hour)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "vektor_session",
		Value:    token,
		Path:     "/",
		MaxAge:   7 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, "/", http.StatusFound)
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
