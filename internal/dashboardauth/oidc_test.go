package dashboardauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

func TestOIDCFlow(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
		denied int
	}{
		{name: "valid"},
		{name: "wrong nonce", denied: 401, mutate: func(c map[string]any) { c["nonce"] = "wrong" }},
		{name: "wrong audience", denied: 401, mutate: func(c map[string]any) { c["aud"] = "other" }},
		{name: "wrong issuer", denied: 401, mutate: func(c map[string]any) { c["iss"] = "https://other" }},
		{name: "expired", denied: 401, mutate: func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{name: "group denied", denied: 403, mutate: func(c map[string]any) { c["groups"] = []string{"other"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var nonce, expectedVerifier string
			exchanges := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				exchanges++
				_ = r.ParseForm()
				if r.FormValue("code_verifier") != expectedVerifier {
					t.Error("PKCE verifier mismatch")
				}
				claims := map[string]any{"iss": "https://issuer.example", "aud": "client", "sub": "user-1", "exp": time.Now().Add(30 * time.Minute).Unix(), "iat": time.Now().Unix(), "nonce": nonce, "groups": []string{"media-admin"}}
				if tc.mutate != nil {
					tc.mutate(claims)
				}
				payload, _ := json.Marshal(claims)
				signed, e := signer.Sign(payload)
				if e != nil {
					t.Error(e)
					return
				}
				raw, e := signed.CompactSerialize()
				if e != nil {
					t.Error(e)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "unused", "token_type": "Bearer", "id_token": raw})
			}))
			defer server.Close()
			a := &Auth{oauth: oauth2.Config{ClientID: "client", ClientSecret: "secret", RedirectURL: "https://gateway.example/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://issuer.example/authorize", TokenURL: server.URL, AuthStyle: oauth2.AuthStyleInHeader}}, verifier: oidc.NewVerifier("https://issuer.example", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&private.PublicKey}}, &oidc.Config{ClientID: "client"}), groups: []string{"media-admin"}, origin: "https://gateway.example", flows: map[[32]byte]flow{}, sessions: map[[32]byte]time.Time{}}
			w := httptest.NewRecorder()
			a.Login(w, httptest.NewRequest("GET", "https://gateway.example/auth/login", nil))
			if w.Code != 302 {
				t.Fatal(w.Code)
			}
			location, _ := url.Parse(w.Header().Get("Location"))
			q := location.Query()
			nonce = q.Get("nonce")
			binding := w.Result().Cookies()[0]
			f := a.flows[key(binding.Value)]
			expectedVerifier = f.verifier
			if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || nonce == "" {
				t.Fatal("missing PKCE/nonce")
			}
			callback := "https://gateway.example/auth/callback?code=code&state=" + q.Get("state")
			r := httptest.NewRequest("GET", callback, nil)
			r.AddCookie(binding)
			w = httptest.NewRecorder()
			a.Callback(w, r)
			if tc.denied != 0 {
				if w.Code != tc.denied {
					t.Fatal(w.Code, w.Body.String())
				}
				if len(a.sessions) != 0 {
					t.Fatal("invalid login created session")
				}
				return
			}
			if w.Code != 303 {
				t.Fatal(w.Code, w.Body.String())
			}
			var session *http.Cookie
			for _, c := range w.Result().Cookies() {
				if c.Name == sessionCookie {
					session = c
				}
			}
			if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode {
				t.Fatal("unsafe session cookie")
			}
			r = httptest.NewRequest("GET", "https://gateway.example/", nil)
			r.AddCookie(session)
			if !a.Valid(r) {
				t.Fatal("session not valid")
			}
			replay := httptest.NewRequest("GET", callback, nil)
			replay.AddCookie(binding)
			w = httptest.NewRecorder()
			a.Callback(w, replay)
			if w.Code != 400 || exchanges != 1 {
				t.Fatal("replayed callback accepted")
			}
			r = httptest.NewRequest("POST", "https://gateway.example/auth/logout", nil)
			r.AddCookie(session)
			w = httptest.NewRecorder()
			a.Logout(w, r)
			if w.Code != 403 || !a.Valid(r) {
				t.Fatal("cross origin logout")
			}
			r.Header.Set("Origin", "https://gateway.example")
			w = httptest.NewRecorder()
			a.Logout(w, r)
			if w.Code != 303 || a.Valid(r) {
				t.Fatal("logout failed")
			}
		})
	}
}
func TestStateAndSessionExpiry(t *testing.T) {
	a := &Auth{flows: map[[32]byte]flow{key("binding"): {state: "expected", expires: time.Now().Add(time.Minute)}}, sessions: map[[32]byte]time.Time{key("old"): time.Now().Add(-time.Second)}}
	r := httptest.NewRequest("GET", "https://gateway/auth/callback?state=wrong&code=x", nil)
	r.AddCookie(&http.Cookie{Name: flowCookie, Value: "binding"})
	w := httptest.NewRecorder()
	a.Callback(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("GET", "https://gateway/", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "old"})
	if a.Valid(r) {
		t.Fatal("expired session accepted")
	}
	r = httptest.NewRequest("GET", "https://gateway/", nil)
	r.Header.Set("X-authentik-username", "admin")
	r.SetBasicAuth("arr", "secret")
	w = httptest.NewRecorder()
	a.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unauthenticated request reached UI") })).ServeHTTP(w, r)
	if w.Code != 302 {
		t.Fatal(w.Code)
	}
}
func TestConfigValidation(t *testing.T) {
	valid := Config{Enabled: true, IssuerURL: "https://auth.example/application/o/rehydrator/", ClientID: "client", ClientSecret: "secret", RedirectURL: "https://gateway.example/auth/callback"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, redirect := range []string{"http://gateway.example/auth/callback", "https://gateway.example/other", "https://user:pass@gateway.example/auth/callback"} {
		c := valid
		c.RedirectURL = redirect
		if c.Validate() == nil {
			t.Fatal("accepted", redirect)
		}
	}
	c := valid
	c.IssuerURL = "http://issuer.example"
	if c.Validate() == nil {
		t.Fatal("HTTP issuer accepted")
	}
	_, err := New(context.Background(), c)
	if err == nil {
		t.Fatal("invalid config accepted")
	}
}
