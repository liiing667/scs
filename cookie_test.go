package scs

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateCookiePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cookie  SessionCookie
		wantErr error
	}{
		// Invalid policies which must fail before a cookie is issued.
		{
			name:    "empty name",
			cookie:  SessionCookie{Name: ""},
			wantErr: ErrInvalidCookieName,
		},
		{
			name:    "name with whitespace",
			cookie:  SessionCookie{Name: "bad name"},
			wantErr: ErrInvalidCookieName,
		},
		{
			name:    "name with disallowed punctuation",
			cookie:  SessionCookie{Name: "bad;name"},
			wantErr: ErrInvalidCookieName,
		},
		{
			name:    "name with control character",
			cookie:  SessionCookie{Name: "bad\nname"},
			wantErr: ErrInvalidCookieName,
		},
		{
			name:    "SameSite None without Secure",
			cookie:  SessionCookie{Name: "session", SameSite: http.SameSiteNoneMode, Secure: false},
			wantErr: ErrInvalidCookieAttr,
		},
		{
			name:    "Partitioned without Secure",
			cookie:  SessionCookie{Name: "session", Partitioned: true, Secure: false},
			wantErr: ErrInvalidCookieAttr,
		},
		{
			name:    "Host prefix without Secure",
			cookie:  SessionCookie{Name: "__Host-session", Path: "/", Secure: false},
			wantErr: ErrInvalidCookieAttr,
		},
		{
			name:    "Host prefix with non-root Path",
			cookie:  SessionCookie{Name: "__Host-session", Path: "/app", Secure: true},
			wantErr: ErrInvalidCookieAttr,
		},
		{
			name:    "Host prefix with Domain",
			cookie:  SessionCookie{Name: "__Host-session", Path: "/", Domain: "example.com", Secure: true},
			wantErr: ErrInvalidCookieAttr,
		},
		{
			name:    "Secure prefix without Secure",
			cookie:  SessionCookie{Name: "__Secure-session", Secure: false},
			wantErr: ErrInvalidCookieAttr,
		},

		// Legal policies, including legacy attribute combinations.
		{
			name:   "default policy",
			cookie: SessionCookie{Name: "session", Path: "/", SameSite: http.SameSiteLaxMode, HttpOnly: true},
		},
		{
			name:   "no SameSite attribute",
			cookie: SessionCookie{Name: "session", SameSite: 0},
		},
		{
			name:   "SameSite None with Secure",
			cookie: SessionCookie{Name: "session", SameSite: http.SameSiteNoneMode, Secure: true},
		},
		{
			name:   "Partitioned with Secure",
			cookie: SessionCookie{Name: "session", Partitioned: true, Secure: true},
		},
		{
			name:   "Host prefix fully compliant",
			cookie: SessionCookie{Name: "__Host-session", Path: "/", Secure: true, SameSite: http.SameSiteNoneMode},
		},
		{
			name:   "Secure prefix with Secure",
			cookie: SessionCookie{Name: "__Secure-session", Secure: true, SameSite: http.SameSiteLaxMode},
		},
		{
			name:   "custom token name",
			cookie: SessionCookie{Name: "session_token", Path: "/", SameSite: http.SameSiteStrictMode},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sm := New()
			sm.Cookie = tt.cookie

			err := sm.validateCookiePolicy()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("want error matching %v; got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("want no error; got %v", err)
			}
		})
	}
}

func TestAddSessionCookieRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	sessionManager := New()
	sessionManager.Cookie = SessionCookie{Name: "bad name"}

	rec := httptest.NewRecorder()
	sessionManager.AddSessionCookie(nil, rec, "token", time.Now().Add(time.Hour))

	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("want no Set-Cookie headers for an invalid policy; got %v", got)
	}

	sessionManager.Cookie = SessionCookie{Name: "session", Path: "/"}
	sessionManager.Cookie.Persist = true
	rec = httptest.NewRecorder()
	sessionManager.AddSessionCookie(nil, rec, "token", time.Now().Add(time.Hour))

	if got := rec.Header().Get("Set-Cookie"); !strings.Contains(got, "session=token") {
		t.Errorf("want a session cookie for a valid policy; got %q", got)
	}
}
