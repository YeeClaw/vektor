package authn

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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
	SessionSecret []byte // HMAC signing key. Derived from config
}

type Claims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// CreateSessionToken generates a token based on provided Claims and a given time-to-live
// as a time constant. Returns a base64 encoding string with the payload and signature or an
// error.
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

	payload := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, s.SessionSecret)

	mac.Write([]byte(payload))
	signature := mac.Sum(nil)

	token := payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	return token, nil
}

// ValidateSession takes a given token in <base64 payload>.<base64 signature>, validates
// the signature based on the SessionSecret and returns the entitled Claims.
func (s *SessionManager) ValidateSession(token string) (*Claims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid session format")
	}

	// Validate HMAC
	payload := parts[0]
	sentBase64Sig := parts[1]

	mac := hmac.New(sha256.New, s.SessionSecret)
	mac.Write([]byte(payload))

	expected := mac.Sum(nil)
	sentSig, err := base64.RawURLEncoding.DecodeString(sentBase64Sig)
	if err != nil {
		return nil, fmt.Errorf("signature isn't valid Base64 text")
	}

	if !hmac.Equal(expected, sentSig) {
		return nil, fmt.Errorf("invalid session signature")
	}

	// Process session data
	data, err := base64.RawURLEncoding.DecodeString(payload)
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
