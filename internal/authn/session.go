package authn

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const userContextKey contextKey = "user"

var (
	ErrSessionFormat     = errors.New("invalid session format")
	ErrSignatureEncoding = errors.New("invalid session signature encoding")
	ErrSessionSignature  = errors.New("invalid session signature")
	ErrPayloadEncoding   = errors.New("invalid session payload encoding")
	ErrSessionData       = errors.New("invalid session data")
	ErrSessionExpired    = errors.New("session expired")
)

type Claims struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (c Claims) LogValue() slog.Value {
	return slog.GroupValue(slog.String("sub", c.Sub))
}

type SessionManager struct {
	SessionSecret []byte // HMAC signing key. Derived from config
	log           *slog.Logger
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
		return nil, ErrSessionFormat
	}

	// Validate HMAC
	payload := parts[0]
	sentBase64Sig := parts[1]

	mac := hmac.New(sha256.New, s.SessionSecret)
	mac.Write([]byte(payload))

	expected := mac.Sum(nil)
	sentSig, err := base64.RawURLEncoding.DecodeString(sentBase64Sig)
	if err != nil {
		return nil, ErrSignatureEncoding
	}

	if !hmac.Equal(expected, sentSig) {
		return nil, ErrSessionSignature
	}

	// Process session data
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrPayloadEncoding
	}

	var session struct {
		Claims Claims `json:"claims"`
		Exp    int64  `json:"exp"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, ErrSessionData
	}

	if time.Now().Unix() > session.Exp {
		return nil, ErrSessionExpired
	}

	return &session.Claims, nil
}

func (s *SessionManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("vektor_session")
		if err != nil {
			s.log.DebugContext(r.Context(), "invalid cookie",
				slog.String("error", err.Error()),
				slog.String("path", r.URL.Path),
				slog.String("remote", r.RemoteAddr),
			)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		claims, err := s.ValidateSession(cookie.Value)
		if err != nil {
			// Expired is routine. A forged signature is a bug or an attack. A payload
			// that fails to decode *after* a valid HMAC means something holding the
			// signing key emitted garbase; this one is probably the biggest deal
			level := slog.LevelInfo
			switch {
			case errors.Is(err, ErrSessionSignature):
				level = slog.LevelWarn
			case errors.Is(err, ErrPayloadEncoding), errors.Is(err, ErrSessionData):
				level = slog.LevelError
			}
			s.log.LogAttrs(r.Context(), level, "session rejected",
				slog.String("error", err.Error()),
				slog.String("path", r.URL.Path),
				slog.String("remote", r.RemoteAddr),
			)
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
