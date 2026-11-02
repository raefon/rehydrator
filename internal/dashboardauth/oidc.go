// Package dashboardauth implements dashboard-only OpenID Connect sessions.
package dashboardauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const sessionCookie = "__Host-rehydrator-session"
const flowCookie = "__Host-rehydrator-flow"

type Config struct {
	Enabled       bool     `yaml:"enabled"`
	IssuerURL     string   `yaml:"issuer_url"`
	ClientID      string   `yaml:"client_id"`
	ClientSecret  string   `yaml:"client_secret"`
	RedirectURL   string   `yaml:"redirect_url"`
	AllowedGroups []string `yaml:"allowed_groups"`
}

func (c Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	issuer, e := url.Parse(c.IssuerURL)
	if e != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return fmt.Errorf("dashboard OIDC requires an HTTPS issuer URL")
	}
	redirect, e := url.Parse(c.RedirectURL)
	if e != nil || redirect.Scheme != "https" || redirect.Host == "" || redirect.User != nil || redirect.Path != "/auth/callback" || redirect.RawQuery != "" || redirect.Fragment != "" {
		return fmt.Errorf("dashboard OIDC requires an HTTPS redirect URL ending in /auth/callback")
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return fmt.Errorf("dashboard OIDC client ID and secret required")
	}
	return nil
}

type flow struct {
	state, nonce, verifier string
	expires                time.Time
}
type Auth struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	groups   []string
	origin   string
	mu       sync.Mutex
	flows    map[[32]byte]flow
	sessions map[[32]byte]time.Time
}

func New(ctx context.Context, c Config) (*Auth, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	discovery := oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(discovery, c.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("dashboard OIDC discovery failed")
	}
	endpoint := provider.Endpoint()
	for _, raw := range []string{endpoint.AuthURL, endpoint.TokenURL} {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fmt.Errorf("dashboard OIDC provider endpoints must use HTTPS")
		}
	}
	u, _ := url.Parse(c.RedirectURL)
	return &Auth{oauth: oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: c.RedirectURL, Endpoint: endpoint, Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}, verifier: provider.Verifier(&oidc.Config{ClientID: c.ClientID}), groups: c.AllowedGroups, origin: u.Scheme + "://" + u.Host, flows: map[[32]byte]flow{}, sessions: map[[32]byte]time.Time{}}, nil
}
func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func key(v string) [32]byte { return sha256.Sum256([]byte(v)) }
func cookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (a *Auth) prune() {
	now := time.Now()
	for k, f := range a.flows {
		if !now.Before(f.expires) {
			delete(a.flows, k)
		}
	}
	for k, t := range a.sessions {
		if !now.Before(t) {
			delete(a.sessions, k)
		}
	}
}
func (a *Auth) Valid(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prune()
	expiry, ok := a.sessions[key(c.Value)]
	return ok && time.Now().Before(expiry)
}
func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	binding, state, nonce, verifier := random(), random(), random(), oauth2.GenerateVerifier()
	a.mu.Lock()
	a.prune()
	if len(a.flows) >= 1024 {
		a.mu.Unlock()
		http.Error(w, "too many pending logins", 503)
		return
	}
	a.flows[key(binding)] = flow{state, nonce, verifier, time.Now().Add(5 * time.Minute)}
	a.mu.Unlock()
	cookie(w, flowCookie, binding, 300)
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}
func (a *Auth) Callback(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	c, err := r.Cookie(flowCookie)
	if err != nil {
		http.Error(w, "login state expired; sign in again", 400)
		return
	}
	a.mu.Lock()
	a.prune()
	f, ok := a.flows[key(c.Value)]
	delete(a.flows, key(c.Value))
	a.mu.Unlock()
	cookie(w, flowCookie, "", -1)
	if !ok || r.URL.Query().Get("state") != f.state || r.URL.Query().Get("code") == "" {
		http.Error(w, "invalid login state", 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	token, err := a.oauth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(f.verifier))
	if err != nil {
		http.Error(w, "identity provider exchange failed", 502)
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "identity token missing", 401)
		return
	}
	id, err := a.verifier.Verify(ctx, raw)
	if err != nil || id.Nonce != f.nonce || id.Subject == "" {
		http.Error(w, "invalid identity token", 401)
		return
	}
	var claims struct {
		Groups []string `json:"groups"`
	}
	if id.Claims(&claims) != nil {
		http.Error(w, "invalid identity claims", 401)
		return
	}
	allowed := len(a.groups) == 0
	for _, want := range a.groups {
		for _, got := range claims.Groups {
			if want == got {
				allowed = true
			}
		}
	}
	if !allowed {
		denyAccount(w)
		return
	}
	expires := time.Now().Add(time.Hour)
	if id.Expiry.Before(expires) {
		expires = id.Expiry
	}
	if !expires.After(time.Now()) {
		http.Error(w, "identity token expired", 401)
		return
	}
	sid := random()
	a.mu.Lock()
	a.prune()
	if len(a.sessions) >= 4096 {
		a.mu.Unlock()
		http.Error(w, "session capacity reached", 503)
		return
	}
	if old, e := r.Cookie(sessionCookie); e == nil {
		delete(a.sessions, key(old.Value))
	}
	a.sessions[key(sid)] = expires
	a.mu.Unlock()
	cookie(w, sessionCookie, sid, int(time.Until(expires).Seconds()))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if r.Header.Get("Origin") != a.origin {
		http.Error(w, "origin rejected", 403)
		return
	}
	if c, e := r.Cookie(sessionCookie); e == nil {
		a.mu.Lock()
		delete(a.sessions, key(c.Value))
		a.mu.Unlock()
	}
	cookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, "/auth/signed-out", http.StatusSeeOther)
}
func (a *Auth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		switch r.URL.Path {
		case "/auth/login":
			a.Login(w, r)
			return
		case "/auth/callback":
			a.Callback(w, r)
			return
		case "/auth/logout":
			a.Logout(w, r)
			return
		case "/auth/signed-out":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<p>Signed out of Rehydrator.</p><a href="/auth/login">Sign in</a>`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/auth/") {
			http.NotFound(w, r)
			return
		}
		if !a.Valid(r) {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}
