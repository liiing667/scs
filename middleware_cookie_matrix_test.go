package scs

import (
	"net/http"
	"strings"
	"testing"
)

// TestLoadAndSaveCookiePolicyMatrix pins the cookie policy behavior of the
// LoadAndSave middleware across the rejection, pass-through and response
// header constraint matrix.
func TestLoadAndSaveCookiePolicyMatrix(t *testing.T) {
	t.Parallel()

	invalidPolicies := []struct {
		name   string
		cookie SessionCookie
	}{
		{"invalid name", SessionCookie{Name: "bad name"}},
		{"empty name", SessionCookie{Name: ""}},
		{"SameSite None without Secure", SessionCookie{Name: "session", SameSite: http.SameSiteNoneMode, Secure: false}},
		{"Partitioned without Secure", SessionCookie{Name: "session", Partitioned: true, Secure: false}},
		{"Host prefix without Secure", SessionCookie{Name: "__Host-session", Path: "/", Secure: false}},
		{"Host prefix wrong Path", SessionCookie{Name: "__Host-session", Path: "/app", Secure: true}},
		{"Host prefix with Domain", SessionCookie{Name: "__Host-session", Path: "/", Domain: "example.com", Secure: true}},
		{"Secure prefix without Secure", SessionCookie{Name: "__Secure-session", Secure: false}},
	}

	for _, tc := range invalidPolicies {
		tc := tc
		t.Run("reject/"+tc.name, func(t *testing.T) {
			t.Parallel()

			sessionManager := New()
			sessionManager.Cookie = tc.cookie

			handlerCalled := false
			mux := http.NewServeMux()
			mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handlerCalled = true
				sessionManager.Put(r.Context(), "foo", "bar")
			}))

			ts := newTestServer(t, sessionManager.LoadAndSave(mux))
			defer ts.Close()

			header, body := ts.execute(t, "/put")

			if handlerCalled {
				t.Error("want the wrapped handler not to be called for an invalid policy")
			}
			if !strings.HasPrefix(body, http.StatusText(http.StatusInternalServerError)) {
				t.Errorf("want a 500 error body; got %q", body)
			}
			if got := header.Values("Set-Cookie"); len(got) != 0 {
				t.Errorf("want no Set-Cookie headers for an invalid policy; got %v", got)
			}
		})
	}

	validPolicies := []struct {
		name   string
		cookie SessionCookie
		attrs  []string
	}{
		{
			name:   "default legacy policy",
			cookie: SessionCookie{Name: "session", Path: "/", SameSite: http.SameSiteLaxMode, HttpOnly: true},
			attrs:  []string{"session=", "Path=/", "HttpOnly", "SameSite=Lax"},
		},
		{
			name:   "no SameSite attribute",
			cookie: SessionCookie{Name: "session", SameSite: 0},
			attrs:  []string{"session="},
		},
		{
			name:   "SameSite None with Secure",
			cookie: SessionCookie{Name: "session", SameSite: http.SameSiteNoneMode, Secure: true},
			attrs:  []string{"SameSite=None", "Secure"},
		},
		{
			name:   "Partitioned with Secure",
			cookie: SessionCookie{Name: "session", Partitioned: true, Secure: true},
			attrs:  []string{"Secure", "Partitioned"},
		},
		{
			name:   "Host prefix",
			cookie: SessionCookie{Name: "__Host-session", Path: "/", Secure: true, SameSite: http.SameSiteNoneMode},
			attrs:  []string{"__Host-session=", "Path=/", "Secure", "SameSite=None"},
		},
		{
			name:   "Secure prefix",
			cookie: SessionCookie{Name: "__Secure-session", Secure: true, SameSite: http.SameSiteLaxMode},
			attrs:  []string{"__Secure-session=", "Secure", "SameSite=Lax"},
		},
	}

	for _, tc := range validPolicies {
		tc := tc
		t.Run("pass/"+tc.name, func(t *testing.T) {
			t.Parallel()

			sessionManager := New()
			sessionManager.Cookie = tc.cookie

			mux := http.NewServeMux()
			mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sessionManager.Put(r.Context(), "foo", "bar")
			}))
			mux.HandleFunc("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(sessionManager.GetString(r.Context(), "foo")))
			}))

			ts := newTestServer(t, sessionManager.LoadAndSave(mux))
			defer ts.Close()

			header, _ := ts.execute(t, "/put")
			setCookie := header.Get("Set-Cookie")
			if setCookie == "" {
				t.Fatal("want a Set-Cookie header for a modified session; got none")
			}
			for _, attr := range tc.attrs {
				if !strings.Contains(setCookie, attr) {
					t.Errorf("want Set-Cookie %q to contain %q", setCookie, attr)
				}
			}
			if tc.name == "no SameSite attribute" && strings.Contains(setCookie, "SameSite") {
				t.Errorf("want no SameSite attribute; got %q", setCookie)
			}
			if got := header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
				t.Errorf("want Cache-Control to contain no-store; got %q", got)
			}

			// The cookie jar round-trip proves the browser can store and send
			// the identity back under the configured policy.
			_, body := ts.execute(t, "/get")
			if body != "bar" {
				t.Errorf("want the session token to round-trip via cookie; got body %q", body)
			}
		})
	}
}

// TestLoadAndSaveInvalidPolicyBlocksRequest ensures an invalid policy is
// rejected before the handler runs, even on a Destroyed session, so no
// unusable deletion cookie is ever emitted.
func TestLoadAndSaveInvalidPolicyBlocksRequest(t *testing.T) {
	t.Parallel()

	sessionManager := New()
	sessionManager.Cookie = SessionCookie{Name: "bad name"}

	mux := http.NewServeMux()
	mux.HandleFunc("/destroy", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := sessionManager.Destroy(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	header, body := ts.execute(t, "/destroy")
	if !strings.HasPrefix(body, http.StatusText(http.StatusInternalServerError)) {
		t.Errorf("want a 500 error body; got %q", body)
	}
	if got := header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("want no Set-Cookie headers for an invalid policy; got %v", got)
	}
}
