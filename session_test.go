package scs

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/mockstore"
)

type testServer struct {
	*httptest.Server
}

func newTestServer(t *testing.T, h http.Handler) *testServer {
	ts := httptest.NewTLSServer(h)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	ts.Client().Jar = jar

	ts.Client().CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &testServer{ts}
}

func (ts *testServer) execute(t *testing.T, urlPath string) (http.Header, string) {
	rs, err := ts.Client().Get(ts.URL + urlPath)
	if err != nil {
		t.Fatal(err)
	}

	defer rs.Body.Close()
	body, err := ioutil.ReadAll(rs.Body)
	if err != nil {
		t.Fatal(err)
	}

	return rs.Header, string(body)
}

func extractTokenFromCookie(c string) string {
	parts := strings.Split(c, ";")
	return strings.SplitN(parts[0], "=", 2)[1]
}

func TestEnable(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	mux := http.NewServeMux()
	mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := sessionManager.Get(r.Context(), "foo").(string)
		w.Write([]byte(s))
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	header, _ := ts.execute(t, "/put")
	token1 := extractTokenFromCookie(header.Get("Set-Cookie"))

	header, body := ts.execute(t, "/get")
	if body != "bar" {
		t.Errorf("want %q; got %q", "bar", body)
	}
	if header.Get("Set-Cookie") != "" {
		t.Errorf("want %q; got %q", "", header.Get("Set-Cookie"))
	}

	header, _ = ts.execute(t, "/put")
	token2 := extractTokenFromCookie(header.Get("Set-Cookie"))
	if token1 != token2 {
		t.Error("want tokens to be the same")
	}
}

func TestLifetime(t *testing.T) {
	t.Parallel()

	sessionManager := New()
	sessionManager.Lifetime = 500 * time.Millisecond

	mux := http.NewServeMux()
	mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := sessionManager.Get(r.Context(), "foo")
		if v == nil {
			http.Error(w, "foo does not exist in session", 500)
			return
		}
		w.Write([]byte(v.(string)))
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	ts.execute(t, "/put")

	_, body := ts.execute(t, "/get")
	if body != "bar" {
		t.Errorf("want %q; got %q", "bar", body)
	}
	time.Sleep(time.Second)

	_, body = ts.execute(t, "/get")
	if body != "foo does not exist in session\n" {
		t.Errorf("want %q; got %q", "foo does not exist in session\n", body)
	}
}

func TestIdleTimeout(t *testing.T) {
	t.Parallel()

	sessionManager := New()
	sessionManager.IdleTimeout = 200 * time.Millisecond
	sessionManager.Lifetime = time.Second

	mux := http.NewServeMux()
	mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := sessionManager.Get(r.Context(), "foo")
		if v == nil {
			http.Error(w, "foo does not exist in session", 500)
			return
		}
		w.Write([]byte(v.(string)))
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	ts.execute(t, "/put")

	time.Sleep(100 * time.Millisecond)
	ts.execute(t, "/get")

	time.Sleep(150 * time.Millisecond)
	_, body := ts.execute(t, "/get")
	if body != "bar" {
		t.Errorf("want %q; got %q", "bar", body)
	}

	time.Sleep(200 * time.Millisecond)
	_, body = ts.execute(t, "/get")
	if body != "foo does not exist in session\n" {
		t.Errorf("want %q; got %q", "foo does not exist in session\n", body)
	}
}

func TestDestroy(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	mux := http.NewServeMux()
	mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/destroy", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := sessionManager.Destroy(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}))
	mux.HandleFunc("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := sessionManager.Get(r.Context(), "foo")
		if v == nil {
			http.Error(w, "foo does not exist in session", 500)
			return
		}
		w.Write([]byte(v.(string)))
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	ts.execute(t, "/put")
	header, _ := ts.execute(t, "/destroy")
	cookie := header.Get("Set-Cookie")

	if strings.HasPrefix(cookie, fmt.Sprintf("%s=;", sessionManager.Cookie.Name)) == false {
		t.Fatalf("got %q: expected prefix %q", cookie, fmt.Sprintf("%s=;", sessionManager.Cookie.Name))
	}
	if strings.Contains(cookie, "Expires=Thu, 01 Jan 1970 00:00:01 GMT") == false {
		t.Fatalf("got %q: expected to contain %q", cookie, "Expires=Thu, 01 Jan 1970 00:00:01 GMT")
	}
	if strings.Contains(cookie, "Max-Age=0") == false {
		t.Fatalf("got %q: expected to contain %q", cookie, "Max-Age=0")
	}

	_, body := ts.execute(t, "/get")
	if body != "foo does not exist in session\n" {
		t.Errorf("want %q; got %q", "foo does not exist in session\n", body)
	}
}

func TestRenewToken(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	mux := http.NewServeMux()
	mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/renew", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := sessionManager.RenewToken(r.Context())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}))
	mux.HandleFunc("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := sessionManager.Get(r.Context(), "foo")
		if v == nil {
			http.Error(w, "foo does not exist in session", 500)
			return
		}
		w.Write([]byte(v.(string)))
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	header, _ := ts.execute(t, "/put")
	cookie := header.Get("Set-Cookie")
	originalToken := extractTokenFromCookie(cookie)

	header, _ = ts.execute(t, "/renew")
	cookie = header.Get("Set-Cookie")
	newToken := extractTokenFromCookie(cookie)

	if newToken == originalToken {
		t.Fatal("token has not changed")
	}

	_, body := ts.execute(t, "/get")
	if body != "bar" {
		t.Errorf("want %q; got %q", "bar", body)
	}
}

func TestRememberMe(t *testing.T) {
	t.Parallel()

	sessionManager := New()
	sessionManager.Cookie.Persist = false

	mux := http.NewServeMux()
	mux.HandleFunc("/put-normal", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/put-rememberMe-true", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.RememberMe(r.Context(), true)
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/put-rememberMe-false", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.RememberMe(r.Context(), false)
		sessionManager.Put(r.Context(), "foo", "bar")
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	header, _ := ts.execute(t, "/put-normal")
	header.Get("Set-Cookie")

	if strings.Contains(header.Get("Set-Cookie"), "Max-Age=") || strings.Contains(header.Get("Set-Cookie"), "Expires=") {
		t.Errorf("want no Max-Age or Expires attributes; got %q", header.Get("Set-Cookie"))
	}

	header, _ = ts.execute(t, "/put-rememberMe-true")
	header.Get("Set-Cookie")

	if !strings.Contains(header.Get("Set-Cookie"), "Max-Age=") || !strings.Contains(header.Get("Set-Cookie"), "Expires=") {
		t.Errorf("want Max-Age and Expires attributes; got %q", header.Get("Set-Cookie"))
	}

	header, _ = ts.execute(t, "/put-rememberMe-false")
	header.Get("Set-Cookie")

	if strings.Contains(header.Get("Set-Cookie"), "Max-Age=") || strings.Contains(header.Get("Set-Cookie"), "Expires=") {
		t.Errorf("want no Max-Age or Expires attributes; got %q", header.Get("Set-Cookie"))
	}
}

func TestIterate(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	mux := http.NewServeMux()
	mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.Put(r.Context(), "foo", r.URL.Query().Get("foo"))
	}))

	for i := 0; i < 3; i++ {
		ts := newTestServer(t, sessionManager.LoadAndSave(mux))
		defer ts.Close()

		ts.execute(t, "/put?foo="+strconv.Itoa(i))
	}

	results := []string{}

	err := sessionManager.Iterate(context.Background(), func(ctx context.Context) error {
		i := sessionManager.GetString(ctx, "foo")
		results = append(results, i)
		return nil
	})

	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(results)

	if !reflect.DeepEqual(results, []string{"0", "1", "2"}) {
		t.Fatalf("unexpected value: got %v", results)
	}

	err = sessionManager.Iterate(context.Background(), func(ctx context.Context) error {
		return errors.New("expected error")
	})
	if err.Error() != "expected error" {
		t.Fatal("didn't get expected error")
	}
}

func TestSetToken(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	mux := http.NewServeMux()
	mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.SetToken(r.Context(), "my-custom-token")
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := sessionManager.Get(r.Context(), "foo")
		if v == nil {
			http.Error(w, "foo does not exist in session", 500)
			return
		}
		w.Write([]byte(v.(string)))
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	header, _ := ts.execute(t, "/put")
	cookie := header.Get("Set-Cookie")
	token := extractTokenFromCookie(cookie)

	if token != "my-custom-token" {
		t.Errorf("want %q; got %q", "my-custom-token", token)
	}

	_, body := ts.execute(t, "/get")
	if body != "bar" {
		t.Errorf("want %q; got %q", "bar", body)
	}
}

func TestMultipleWrites(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	mux := http.NewServeMux()
	mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(""))
		s := sessionManager.PopString(r.Context(), "foo")
		_, _, err := sessionManager.Commit(r.Context())
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(s))
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	_, _ = ts.execute(t, "/put")

	_, body := ts.execute(t, "/get")
	if body != "bar" {
		t.Errorf("want %q; got %q", "bar", body)
	}

	_, body = ts.execute(t, "/get")
	if body != "" {
		t.Errorf("want %q; got %q", "", body)
	}
}

func TestValidToken(t *testing.T) {
	t.Parallel()

	validTokens := []string{
		"abc",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_",
		"my-custom-token",
		"token.with.dots",
		"v1:abc/DEF+123=",
	}
	for _, token := range validTokens {
		if !validToken(token) {
			t.Errorf("validToken(%q): want true; got false", token)
		}
	}

	invalidTokens := []string{
		"",
		"bad token",
		`"quoted"`,
		"semi;colon",
		"ctl\x01value",
		"tab\tvalue",
	}
	for _, token := range invalidTokens {
		if validToken(token) {
			t.Errorf("validToken(%q): want false; got true", token)
		}
	}
}

func TestExtractSessionToken(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	tests := []struct {
		name      string
		cookies   []string
		wantToken string
		wantErr   error
	}{
		{name: "no session cookie", cookies: []string{"other=1"}, wantToken: "", wantErr: nil},
		{name: "no cookies at all", cookies: nil, wantToken: "", wantErr: nil},
		{name: "single session cookie", cookies: []string{"session=abc"}, wantToken: "abc", wantErr: nil},
		{name: "duplicate distinct session cookies in one header", cookies: []string{"session=abc; session=def"}, wantToken: "", wantErr: ErrMultipleSessionCookies},
		{name: "duplicate identical session cookies in one header", cookies: []string{"session=abc; session=abc"}, wantToken: "", wantErr: ErrMultipleSessionCookies},
		{name: "duplicate session cookies across headers", cookies: []string{"session=abc", "session=def"}, wantToken: "", wantErr: ErrMultipleSessionCookies},
		{name: "empty session cookie value", cookies: []string{"session="}, wantToken: "", wantErr: ErrInvalidSessionToken},
		{name: "whitespace in token", cookies: []string{"session=bad value"}, wantToken: "", wantErr: ErrInvalidSessionToken},
		{name: "trailing whitespace in token", cookies: []string{"session=abc "}, wantToken: "", wantErr: ErrInvalidSessionToken},
		{name: "quoted token", cookies: []string{`session="quoted"`}, wantToken: "", wantErr: ErrInvalidSessionToken},
		{name: "control character in token", cookies: []string{"session=ctl\x01value"}, wantToken: "", wantErr: ErrInvalidSessionToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for _, cookie := range tt.cookies {
				req.Header.Add("Cookie", cookie)
			}

			token, err := sessionManager.extractSessionToken(req)
			if token != tt.wantToken {
				t.Errorf("want token %q; got %q", tt.wantToken, token)
			}
			if err != tt.wantErr {
				t.Errorf("want err %v; got %v", tt.wantErr, err)
			}
		})
	}
}

func TestLoadAndSaveRejectsDuplicateSessionCookies(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		sessionManager.Put(r.Context(), "foo", "bar")
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Cookie", "session=abc; session=def")

	sessionManager.LoadAndSave(handler).ServeHTTP(rr, req)

	if handlerCalled {
		t.Error("next handler should not be called for a request with duplicate session cookies")
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("want status %d; got %d", http.StatusInternalServerError, rr.Code)
	}
	if setCookie := rr.Header().Values("Set-Cookie"); len(setCookie) != 0 {
		t.Errorf("want no Set-Cookie headers on the error path; got %v", setCookie)
	}
}

func TestLoadAndSaveRejectsDuplicateSessionCookieHeaders(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	// Two separate Cookie headers, as might be forwarded by a proxy.
	req.Header.Add("Cookie", "session=abc")
	req.Header.Add("Cookie", "session=def")

	sessionManager.LoadAndSave(handler).ServeHTTP(rr, req)

	if handlerCalled {
		t.Error("next handler should not be called when session cookies arrive in separate headers")
	}
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("want status %d; got %d", http.StatusInternalServerError, rr.Code)
	}
	if setCookie := rr.Header().Values("Set-Cookie"); len(setCookie) != 0 {
		t.Errorf("want no Set-Cookie headers on the error path; got %v", setCookie)
	}
}

func TestLoadAndSaveRejectsMalformedSessionToken(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		sessionManager.Put(r.Context(), "foo", "bar")
		w.WriteHeader(http.StatusOK)
	})

	cases := map[string]string{
		"empty value":         "session=",
		"whitespace in value": "session=bad value",
		"control in value":    "session=ctl\x01value",
		"quoted value":        `session="quoted"`,
	}

	for name, cookie := range cases {
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("Cookie", cookie)

			sessionManager.LoadAndSave(handler).ServeHTTP(rr, req)

			if handlerCalled {
				t.Error("next handler should not be called for a request with an invalid session token")
			}
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("want status %d; got %d", http.StatusInternalServerError, rr.Code)
			}
			if setCookie := rr.Header().Values("Set-Cookie"); len(setCookie) != 0 {
				t.Errorf("want no Set-Cookie headers on the error path; got %v", setCookie)
			}
		})
	}
}

func TestLoadAndSaveInvalidTokenUsesErrorFunc(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	var gotErr error
	sessionManager.ErrorFunc = func(w http.ResponseWriter, r *http.Request, err error) {
		gotErr = err
		w.WriteHeader(http.StatusBadRequest)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Cookie", "session=abc; session=def")

	sessionManager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})).ServeHTTP(rr, req)

	if gotErr != ErrMultipleSessionCookies {
		t.Fatalf("want ErrMultipleSessionCookies; got %v", gotErr)
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("want custom ErrorFunc status %d; got %d", http.StatusBadRequest, rr.Code)
	}
	if setCookie := rr.Header().Values("Set-Cookie"); len(setCookie) != 0 {
		t.Errorf("want no Set-Cookie headers on the error path; got %v", setCookie)
	}
}

func TestLoadAndSaveInvalidRequestsDoNotTouchStore(t *testing.T) {
	t.Parallel()

	sessionManager := New()
	// The mock store panics if any of its methods are called without a
	// matching expectation, proving that a failed request never reaches the
	// store and cannot create or overwrite a session.
	sessionManager.Store = &mockstore.MockStore{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called")
	})

	cases := map[string][]string{
		"duplicate session cookies": {"session=abc; session=def"},
		"empty token":               {"session="},
		"malformed token":           {"session=bad value"},
	}

	for name, cookies := range cases {
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/", nil)
			for _, cookie := range cookies {
				req.Header.Add("Cookie", cookie)
			}

			sessionManager.LoadAndSave(handler).ServeHTTP(rr, req)

			if rr.Code != http.StatusInternalServerError {
				t.Errorf("want status %d; got %d", http.StatusInternalServerError, rr.Code)
			}
			if setCookie := rr.Header().Values("Set-Cookie"); len(setCookie) != 0 {
				t.Errorf("want no Set-Cookie headers on the error path; got %v", setCookie)
			}
		})
	}
}

func TestLoadAndSaveAcceptsCustomStoreTokens(t *testing.T) {
	t.Parallel()

	sessionManager := New()

	mux := http.NewServeMux()
	mux.HandleFunc("/put", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionManager.SetToken(r.Context(), "my-custom-token")
		sessionManager.Put(r.Context(), "foo", "bar")
	}))
	mux.HandleFunc("/get", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := sessionManager.Get(r.Context(), "foo")
		if v == nil {
			http.Error(w, "foo does not exist in session", 500)
			return
		}
		w.Write([]byte(v.(string)))
	}))

	ts := newTestServer(t, sessionManager.LoadAndSave(mux))
	defer ts.Close()

	header, _ := ts.execute(t, "/put")
	if token := extractTokenFromCookie(header.Get("Set-Cookie")); token != "my-custom-token" {
		t.Fatalf("want token %q; got %q", "my-custom-token", token)
	}

	_, body := ts.execute(t, "/get")
	if body != "bar" {
		t.Errorf("want %q; got %q", "bar", body)
	}
}
