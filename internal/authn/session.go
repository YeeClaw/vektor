package authn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const userContextKey contextKey = "user"

type SessionManager struct {
	// Empty today--will have more information as OIDC and Local auth are fleshed out
}

type Claims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (s *SessionManager) CreateSessionToken(claims *Claims, ttl time.Duration) (string, error) {
	session := struct {
		Claims Claims `json:"claims"`
		Exp    int64  `json:"exp"`
	}{
		Claims: *claims,
		Exp:    time.Now().Add(ttl).Unix(),
	}

	data, err := json.Marshal(session)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (s *SessionManager) ValidateSession(token string) (*Claims, error) {
	parts := strings.SplitN(token, ".", 2)
	// This assumes JWT but will change when I implement HMAC
	if len(parts) != 1 {
		return nil, fmt.Errorf("invalid session format")
	}

	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("invalid session encoding")
	}

	var session struct {
		Claims Claims `json:"claims"`
		Exp    int64  `json:"exp"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("invalid session data")
	}

	if time.Now().Unix() > session.Exp {
		return nil, fmt.Errorf("session expired")
	}

	return &session.Claims, nil
}

func (s *SessionManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("vektor_session")
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		claims, err := s.ValidateSession(cookie.Value)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func UserFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(userContextKey).(*Claims)
	return claims
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
