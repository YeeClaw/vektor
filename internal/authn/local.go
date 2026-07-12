package authn

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type Local struct {
	db *sql.DB
	SessionManager
}

func NewLocal(db *sql.DB) *Local {
	return &Local{db: db}
}

func (l *Local) RegisterPublicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/register", l.registerHandler)
	mux.HandleFunc("POST /auth/login", l.loginHandler)
}

func (l *Local) registerHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if input.Email == "" || input.Name == "" || input.Password == "" {
		http.Error(w, "email, name, and password are required", http.StatusBadRequest)
		return
	}
	if len(input.Password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	id := uuid.New().String()
	_, err = l.db.ExecContext(r.Context(),
		"INSERT INTO users (id, email, name, password_hash) VALUES (?, ?, ?, ?)",
		id, input.Email, input.Name, string(hash),
	)
	if err != nil {
		http.Error(w, "email already registered", http.StatusConflict)
		return
	}

	claims := &Claims{Sub: id, Email: input.Email, Name: input.Name}
	token, err := l.CreateSessionToken(claims, 7*24*time.Hour)
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(claims)
}

func (l *Local) loginHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if input.Email == "" || input.Password == "" {
		http.Error(w, "email and password are required", http.StatusBadRequest)
		return
	}

	var id, email, name string
	var passwordHash sql.NullString
	err := l.db.QueryRowContext(r.Context(),
		"SELECT id, email, name, password_hash FROM users WHERE email = ?",
		input.Email,
	).Scan(&id, &email, &name, &passwordHash)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if !passwordHash.Valid {
		http.Error(w, "this account uses OIDC login", http.StatusBadRequest)
		return
	}

	hash := []byte(passwordHash.String)
	pass := []byte(input.Password)
	if err := bcrypt.CompareHashAndPassword(hash, pass); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	claims := &Claims{Sub: id, Email: email, Name: name}
	token, err := l.CreateSessionToken(claims, 7*24*time.Hour)
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(claims)
}
