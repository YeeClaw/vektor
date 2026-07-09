package auth

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Auth struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
	db       *sql.DB
}

func New(
	ctx context.Context,
	issuer,
	clientID,
	clientSecret,
	redirectURL string,
	db *sql.DB,
) (*Auth, error) {

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering OIDC provider: %w", err)
	}

	oauth := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	return &Auth{
		provider: provider,
		verifier: verifier,
		oauth:    oauth,
		db:       db,
	}, nil
}

func (a *Auth) LoginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state, err := randomState()
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "vektor_state",
			Value:    state,
			Path:     "/",
			MaxAge:   300,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		http.Redirect(w, r, a.oauth.AuthCodeURL(state), http.StatusFound)
	}
}

func (a *Auth) CallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stateCookie, err := r.Cookie("vektor_state")
		if err != nil || r.URL.Query().Get("state") != stateCookie.Value {
			http.Error(w, "invalid state", http.StatusBadRequest)
			return
		}

		token, err := a.oauth.Exchange(r.Context(), r.URL.Query().Get("code"))
		if err != nil {
			http.Error(w, "token exchange failed", http.StatusUnauthorized)
			return
		}

		rawIDToken, ok := token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "missing id_token", http.StatusUnauthorized)
			return
		}

		idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
		if err != nil {
			http.Error(w, "invalid id_token", http.StatusUnauthorized)
			return
		}

		var claims Claims
		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "invalid claims", http.StatusUnauthorized)
			return
		}

		// Upsert user in DB
		_, err = a.db.ExecContext(r.Context(),
			`INSERT INTO users (id, email, name) VALUES (?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET email=excluded.email, name=excluded.name`,
			claims.Sub, claims.Email, claims.Name,
		)
		if err != nil {
			log.Printf("error upserting user: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		cookieToken, err := CreateSessionToken(&claims, 7*24*time.Hour)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "vektor_session",
			Value:    cookieToken,
			Path:     "/",
			MaxAge:   7 * 24 * 60 * 60,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})

		http.Redirect(w, r, "/", http.StatusFound)
	}

}
