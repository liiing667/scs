package scs

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/mockstore"
)

func TestSessionDataFromContext(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("the code did not panic")
		}
	}()

	s := New()
	s.getSessionDataFromContext(context.Background())
}

func TestSessionManager_Load(T *testing.T) {
	T.Parallel()

	T.Run("happy path", func(t *testing.T) {
		s := New()
		s.IdleTimeout = time.Hour * 24

		ctx := context.Background()
		expected := "example"
		exampleDeadline := time.Now().Add(time.Hour)

		encodedValue, err := s.Codec.Encode(exampleDeadline, map[string]interface{}{
			"things": "stuff",
		})
		if err != nil {
			t.Errorf("unexpected error encoding value: %v", err)
		}

		if err := s.Store.Commit(expected, encodedValue, exampleDeadline); err != nil {
			t.Errorf("error committing to session store: %v", err)
		}

		newCtx, err := s.Load(ctx, expected)
		if err != nil {
			t.Errorf("error loading from session manager: %v", err)
		}
		if newCtx == nil {
			t.Error("returned context is unexpectedly nil")
		}

		sd, ok := newCtx.Value(s.ContextKey).(*sessionData)
		if !ok {
			t.Error("sessionData not present in returned context")
		}
		if sd == nil {
			t.Error("sessionData present in returned context unexpectedly nil")
			return
		}

		actual := sd.token

		if expected != actual {
			t.Errorf("expected %s to equal %s", expected, actual)
		}
	})

	T.Run("with preexisting session data", func(t *testing.T) {
		s := New()

		obligatorySessionData := &sessionData{}
		ctx := context.WithValue(context.Background(), s.ContextKey, obligatorySessionData)
		expected := "example"

		newCtx, err := s.Load(ctx, expected)
		if err != nil {
			t.Errorf("error loading from session manager: %v", err)
		}
		if newCtx == nil {
			t.Error("returned context is unexpectedly nil")
		}
	})

	T.Run("with empty token", func(t *testing.T) {
		s := New()

		ctx := context.Background()
		expected := ""
		exampleDeadline := time.Now().Add(time.Hour)

		encodedValue, err := s.Codec.Encode(exampleDeadline, map[string]interface{}{
			"things": "stuff",
		})
		if err != nil {
			t.Errorf("unexpected error encoding value: %v", err)
		}

		if err := s.Store.Commit(expected, encodedValue, exampleDeadline); err != nil {
			t.Errorf("error committing to session store: %v", err)
		}

		newCtx, err := s.Load(ctx, "")
		if err != nil {
			t.Errorf("error loading from session manager: %v", err)
		}
		if newCtx == nil {
			t.Error("returned context is unexpectedly nil")
		}

		sd, ok := newCtx.Value(s.ContextKey).(*sessionData)
		if !ok {
			t.Error("sessionData not present in returned context")
		}
		if sd == nil {
			t.Error("sessionData present in returned context unexpectedly nil")
			return
		}

		actual := sd.token

		if expected != actual {
			t.Errorf("expected %s to equal %s", expected, actual)
		}
	})

	T.Run("with error finding token in store", func(t *testing.T) {
		s := New()
		store := &mockstore.MockStore{}

		ctx := context.Background()
		expected := "example"

		store.ExpectFind(expected, []byte{}, true, errors.New("arbitrary"))
		s.Store = store

		newCtx, err := s.Load(ctx, expected)
		if err == nil {
			t.Errorf("no error loading from session manager: %v", err)
		}
		if newCtx != nil {
			t.Error("returned context is unexpectedly not nil")
		}
	})

	T.Run("with unfound token in store", func(t *testing.T) {
		s := New()

		ctx := context.Background()
		exampleToken := "example"
		expected := ""

		newCtx, err := s.Load(ctx, exampleToken)
		if err != nil {
			t.Errorf("error loading from session manager: %v", err)
		}
		if newCtx == nil {
			t.Error("returned context is unexpectedly nil")
		}

		sd, ok := newCtx.Value(s.ContextKey).(*sessionData)
		if !ok {
			t.Error("sessionData not present in returned context")
		}
		if sd == nil {
			t.Error("sessionData present in returned context unexpectedly nil")
			return
		}

		actual := sd.token

		if expected != actual {
			t.Errorf("expected %s to equal %s", expected, actual)
		}
	})

	T.Run("with error decoding found token", func(t *testing.T) {
		s := New()

		ctx := context.Background()
		expected := "example"
		exampleDeadline := time.Now().Add(time.Hour)

		if err := s.Store.Commit(expected, []byte(""), exampleDeadline); err != nil {
			t.Errorf("error committing to session store: %v", err)
		}

		newCtx, err := s.Load(ctx, expected)
		if err == nil {
			t.Errorf("no error loading from session manager: %v", err)
		}
		if newCtx != nil {
			t.Error("returned context is unexpectedly nil")
		}
	})

	T.Run("with token hashing", func(t *testing.T) {
		s := New()
		s.HashTokenInStore = true
		s.IdleTimeout = time.Hour * 24

		expectedToken := "example"
		expectedExpiry := time.Now().Add(time.Hour)

		initialCtx := context.WithValue(context.Background(), s.ContextKey, &sessionData{
			deadline: expectedExpiry,
			expiry:   expectedExpiry,
			token:    expectedToken,
			values: map[string]interface{}{
				"blah": "blah",
			},
			mu: sync.Mutex{},
		})

		actualToken, actualExpiry, err := s.Commit(initialCtx)
		if expectedToken != actualToken {
			t.Errorf("expected token to equal %q, but received %q", expectedToken, actualToken)
		}
		if expectedExpiry != actualExpiry {
			t.Errorf("expected expiry to equal %v, but received %v", expectedExpiry, actualExpiry)
		}
		if err != nil {
			t.Errorf("unexpected error returned: %v", err)
		}

		retrievedCtx, err := s.Load(context.Background(), expectedToken)
		if err != nil {
			t.Errorf("unexpected error returned: %v", err)
		}
		retrievedSessionData, ok := retrievedCtx.Value(s.ContextKey).(*sessionData)
		if !ok {
			t.Errorf("unexpected data in retrieved context")
		} else if retrievedSessionData.token != expectedToken {
			t.Errorf("expected token in context's session data data to equal %v, but received %v", expectedToken, retrievedSessionData.token)
		}

		if err := s.Destroy(retrievedCtx); err != nil {
			t.Errorf("unexpected error returned: %v", err)
		}
	})
}

func TestSessionManager_Commit(T *testing.T) {
	T.Parallel()

	T.Run("happy path", func(t *testing.T) {
		s := New()
		s.IdleTimeout = time.Hour * 24

		expectedToken := "example"
		expectedExpiry := time.Now().Add(time.Hour)

		ctx := context.WithValue(context.Background(), s.ContextKey, &sessionData{
			deadline: expectedExpiry,
			expiry:   expectedExpiry,
			token:    expectedToken,
			values: map[string]interface{}{
				"blah": "blah",
			},
			mu: sync.Mutex{},
		})

		actualToken, actualExpiry, err := s.Commit(ctx)
		if expectedToken != actualToken {
			t.Errorf("expected token to equal %q, but received %q", expectedToken, actualToken)
		}
		if expectedExpiry != actualExpiry {
			t.Errorf("expected expiry to equal %v, but received %v", expectedExpiry, actualExpiry)
		}
		if err != nil {
			t.Errorf("unexpected error returned: %v", err)
		}
	})

	T.Run("with empty token", func(t *testing.T) {
		s := New()
		s.IdleTimeout = time.Hour * 24

		expectedToken := "XO6_D4NBpGP3D_BtekxTEO6o2ZvOzYnArauSQbgg"
		expectedExpiry := time.Now().Add(time.Hour)

		ctx := context.WithValue(context.Background(), s.ContextKey, &sessionData{
			deadline: expectedExpiry,
			expiry:   expectedExpiry,
			token:    expectedToken,
			values: map[string]interface{}{
				"blah": "blah",
			},
			mu: sync.Mutex{},
		})

		actualToken, actualExpiry, err := s.Commit(ctx)
		if expectedToken != actualToken {
			t.Errorf("expected token to equal %q, but received %q", expectedToken, actualToken)
		}
		if expectedExpiry != actualExpiry {
			t.Errorf("expected expiry to equal %v, but received %v", expectedExpiry, actualExpiry)
		}
		if err != nil {
			t.Errorf("unexpected error returned: %v", err)
		}
	})

	T.Run("with expired deadline", func(t *testing.T) {
		s := New()
		s.IdleTimeout = time.Millisecond

		expectedToken := "example"
		expectedExpiry := time.Now().Add(time.Hour * -100)

		ctx := context.WithValue(context.Background(), s.ContextKey, &sessionData{
			deadline: time.Now().Add(time.Hour * 24),
			token:    expectedToken,
			values: map[string]interface{}{
				"blah": "blah",
			},
			mu: sync.Mutex{},
		})

		actualToken, actualExpiry, err := s.Commit(ctx)
		if expectedToken != actualToken {
			t.Errorf("expected token to equal %q, but received %q", expectedToken, actualToken)
		}
		if expectedExpiry == actualExpiry {
			t.Errorf("expected expiry not to equal %v", actualExpiry)
		}
		if err != nil {
			t.Errorf("unexpected error returned: %v", err)
		}
	})

	T.Run("with error committing to store", func(t *testing.T) {
		s := New()
		s.IdleTimeout = time.Hour * 24

		store := &mockstore.MockStore{}
		expectedErr := errors.New("arbitrary")
		deadline := time.Now().Add(time.Hour)

		sd := &sessionData{
			deadline: deadline,
			expiry:   deadline,
			token:    "example",
			values: map[string]interface{}{
				"blah": "blah",
			},
			mu: sync.Mutex{},
		}
		expectedBytes, err := s.Codec.Encode(sd.deadline, sd.values)
		if err != nil {
			t.Errorf("unexpected encode error: %v", err)
		}

		ctx := context.WithValue(context.Background(), s.ContextKey, sd)

		store.ExpectCommit(sd.token, expectedBytes, sd.deadline, expectedErr)
		s.Store = store

		actualToken, _, err := s.Commit(ctx)
		if actualToken != "" {
			t.Error("expected empty token")
		}
		if err == nil {
			t.Error("expected error not returned")
		}
	})

	T.Run("with token hashing", func(t *testing.T) {
		s := New()
		s.HashTokenInStore = true
		s.IdleTimeout = time.Hour * 24

		expectedToken := "example"
		expectedExpiry := time.Now().Add(time.Hour)

		ctx := context.WithValue(context.Background(), s.ContextKey, &sessionData{
			deadline: expectedExpiry,
			expiry:   expectedExpiry,
			token:    expectedToken,
			values: map[string]interface{}{
				"blah": "blah",
			},
			mu: sync.Mutex{},
		})

		actualToken, actualExpiry, err := s.Commit(ctx)
		if expectedToken != actualToken {
			t.Errorf("expected token to equal %q, but received %q", expectedToken, actualToken)
		}
		if expectedExpiry != actualExpiry {
			t.Errorf("expected expiry to equal %v, but received %v", expectedExpiry, actualExpiry)
		}
		if err != nil {
			t.Errorf("unexpected error returned: %v", err)
		}
	})
}

func TestSessionManager_CommitMaxBytes(T *testing.T) {
	T.Parallel()

	deadline := time.Now().Add(time.Hour).UTC()

	s := New()

	encode := func(values map[string]interface{}) []byte {
		T.Helper()
		b, err := s.Codec.Encode(deadline, values)
		if err != nil {
			T.Fatalf("unexpected encode error: %v", err)
		}
		return b
	}

	normalValues := map[string]interface{}{"name": "alice"}

	// Fix the limit relative to a normal-sized session, with enough headroom
	// for the padding key used to construct exact-size payloads below. The
	// headroom keeps the padding length clear of gob's length-prefix
	// boundaries so that any exact size in the range is attainable.
	limit := len(encode(normalValues)) + 1024
	s.MaxBytes = limit

	// exactValues returns session values whose encoded size is exactly size
	// bytes, by adjusting the length of a padding value.
	exactValues := func(size int) map[string]interface{} {
		T.Helper()
		padLen := size - len(encode(map[string]interface{}{"pad": []byte{}}))
		for i := 0; ; i++ {
			if i == 10 {
				T.Fatalf("could not construct values with encoded size %d", size)
			}
			values := map[string]interface{}{"pad": make([]byte, padLen)}
			diff := size - len(encode(values))
			if diff == 0 {
				return values
			}
			padLen += diff
		}
	}

	T.Run("normal commit below limit", func(t *testing.T) {
		ctx, err := s.Load(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected load error: %v", err)
		}

		s.Put(ctx, "name", "alice")

		token, _, err := s.Commit(ctx)
		if err != nil {
			t.Fatalf("unexpected commit error: %v", err)
		}
		if token == "" {
			t.Error("expected non-empty token")
		}
		if _, found, err := s.Store.Find(token); err != nil || !found {
			t.Errorf("expected session data in store, got found=%v, err=%v", found, err)
		}
		if s.Status(ctx) != Committed {
			t.Errorf("expected status %v, got %v", Committed, s.Status(ctx))
		}
	})

	T.Run("boundary commit at limit", func(t *testing.T) {
		ctx, err := s.Load(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected load error: %v", err)
		}

		for key, val := range exactValues(limit) {
			s.Put(ctx, key, val)
		}

		token, _, err := s.Commit(ctx)
		if err != nil {
			t.Fatalf("unexpected commit error: %v", err)
		}
		if token == "" {
			t.Error("expected non-empty token")
		}
		if _, found, err := s.Store.Find(token); err != nil || !found {
			t.Errorf("expected session data in store, got found=%v, err=%v", found, err)
		}
	})

	T.Run("rejection above limit preserves identity", func(t *testing.T) {
		original := encode(normalValues)
		if err := s.Store.Commit("token123", original, deadline); err != nil {
			t.Fatalf("unexpected store error: %v", err)
		}

		ctx, err := s.Load(context.Background(), "token123")
		if err != nil {
			t.Fatalf("unexpected load error: %v", err)
		}

		for key, val := range exactValues(limit + 1) {
			s.Put(ctx, key, val)
		}

		token, _, err := s.Commit(ctx)
		if !errors.Is(err, ErrMaxBytesExceeded) {
			t.Fatalf("expected ErrMaxBytesExceeded, got %v", err)
		}
		if token != "" {
			t.Errorf("expected empty token, got %q", token)
		}
		if s.Token(ctx) != "token123" {
			t.Errorf("expected session token to remain %q, got %q", "token123", s.Token(ctx))
		}
		if s.Status(ctx) != Modified {
			t.Errorf("expected status %v, got %v", Modified, s.Status(ctx))
		}

		b, found, err := s.Store.Find("token123")
		if err != nil || !found {
			t.Fatalf("expected persisted session data, got found=%v, err=%v", found, err)
		}
		if !bytes.Equal(b, original) {
			t.Error("expected persisted session data to be unchanged")
		}
	})
}

func TestPut(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	ctx := s.addSessionDataToContext(context.Background(), sd)

	s.Put(ctx, "foo", "bar")

	if sd.values["foo"] != "bar" {
		t.Errorf("got %q: expected %q", sd.values["foo"], "bar")
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}
}

func TestGet(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = "bar"
	ctx := s.addSessionDataToContext(context.Background(), sd)

	str, ok := s.Get(ctx, "foo").(string)
	if !ok {
		t.Errorf("could not convert %T to string", s.Get(ctx, "foo"))
	}

	if str != "bar" {
		t.Errorf("got %q: expected %q", str, "bar")
	}
}

func TestPop(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = "bar"
	ctx := s.addSessionDataToContext(context.Background(), sd)

	str, ok := s.Pop(ctx, "foo").(string)
	if !ok {
		t.Errorf("could not convert %T to string", s.Get(ctx, "foo"))
	}

	if str != "bar" {
		t.Errorf("got %q: expected %q", str, "bar")
	}

	_, ok = sd.values["foo"]
	if ok {
		t.Errorf("got %v: expected %v", ok, false)
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = "bar"
	ctx := s.addSessionDataToContext(context.Background(), sd)

	s.Remove(ctx, "foo")

	if sd.values["foo"] != nil {
		t.Errorf("got %v: expected %v", sd.values["foo"], nil)
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}
}

func TestClear(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = "bar"
	sd.values["baz"] = "boz"
	ctx := s.addSessionDataToContext(context.Background(), sd)

	if err := s.Clear(ctx); err != nil {
		t.Errorf("unexpected error encountered clearing session: %v", err)
	}

	if sd.values["foo"] != nil {
		t.Errorf("got %v: expected %v", sd.values["foo"], nil)
	}

	if sd.values["baz"] != nil {
		t.Errorf("got %v: expected %v", sd.values["baz"], nil)
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}
}

func TestExists(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = "bar"
	ctx := s.addSessionDataToContext(context.Background(), sd)

	if !s.Exists(ctx, "foo") {
		t.Errorf("got %v: expected %v", s.Exists(ctx, "foo"), true)
	}

	if s.Exists(ctx, "baz") {
		t.Errorf("got %v: expected %v", s.Exists(ctx, "baz"), false)
	}
}

func TestKeys(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = "bar"
	sd.values["woo"] = "waa"
	ctx := s.addSessionDataToContext(context.Background(), sd)

	keys := s.Keys(ctx)
	if !reflect.DeepEqual(keys, []string{"foo", "woo"}) {
		t.Errorf("got %v: expected %v", keys, []string{"foo", "woo"})
	}
}

func TestGetString(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = "bar"
	ctx := s.addSessionDataToContext(context.Background(), sd)

	str := s.GetString(ctx, "foo")
	if str != "bar" {
		t.Errorf("got %q: expected %q", str, "bar")
	}

	str = s.GetString(ctx, "baz")
	if str != "" {
		t.Errorf("got %q: expected %q", str, "")
	}
}

func TestGetBool(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = true
	ctx := s.addSessionDataToContext(context.Background(), sd)

	b := s.GetBool(ctx, "foo")
	if b != true {
		t.Errorf("got %v: expected %v", b, true)
	}

	b = s.GetBool(ctx, "baz")
	if b != false {
		t.Errorf("got %v: expected %v", b, false)
	}
}

func TestGetInt(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = 123
	ctx := s.addSessionDataToContext(context.Background(), sd)

	i := s.GetInt(ctx, "foo")
	if i != 123 {
		t.Errorf("got %v: expected %d", i, 123)
	}

	i = s.GetInt(ctx, "baz")
	if i != 0 {
		t.Errorf("got %v: expected %d", i, 0)
	}
}

func TestGetFloat(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = 123.456
	ctx := s.addSessionDataToContext(context.Background(), sd)

	f := s.GetFloat(ctx, "foo")
	if f != 123.456 {
		t.Errorf("got %v: expected %f", f, 123.456)
	}

	f = s.GetFloat(ctx, "baz")
	if f != 0 {
		t.Errorf("got %v: expected %f", f, 0.00)
	}
}

func TestGetBytes(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = []byte("bar")
	ctx := s.addSessionDataToContext(context.Background(), sd)

	b := s.GetBytes(ctx, "foo")
	if !bytes.Equal(b, []byte("bar")) {
		t.Errorf("got %v: expected %v", b, []byte("bar"))
	}

	b = s.GetBytes(ctx, "baz")
	if b != nil {
		t.Errorf("got %v: expected %v", b, nil)
	}
}

func TestGetTime(t *testing.T) {
	t.Parallel()

	now := time.Now()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = now
	ctx := s.addSessionDataToContext(context.Background(), sd)

	tm := s.GetTime(ctx, "foo")
	if tm != now {
		t.Errorf("got %v: expected %v", tm, now)
	}

	tm = s.GetTime(ctx, "baz")
	if !tm.IsZero() {
		t.Errorf("got %v: expected %v", tm, time.Time{})
	}
}

func TestPopString(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = "bar"
	ctx := s.addSessionDataToContext(context.Background(), sd)

	str := s.PopString(ctx, "foo")
	if str != "bar" {
		t.Errorf("got %q: expected %q", str, "bar")
	}

	_, ok := sd.values["foo"]
	if ok {
		t.Errorf("got %v: expected %v", ok, false)
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}

	str = s.PopString(ctx, "bar")
	if str != "" {
		t.Errorf("got %q: expected %q", str, "")
	}
}

func TestPopBool(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = true
	ctx := s.addSessionDataToContext(context.Background(), sd)

	b := s.PopBool(ctx, "foo")
	if b != true {
		t.Errorf("got %v: expected %v", b, true)
	}

	_, ok := sd.values["foo"]
	if ok {
		t.Errorf("got %v: expected %v", ok, false)
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}

	b = s.PopBool(ctx, "bar")
	if b != false {
		t.Errorf("got %v: expected %v", b, false)
	}
}

func TestPopInt(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = 123
	ctx := s.addSessionDataToContext(context.Background(), sd)

	i := s.PopInt(ctx, "foo")
	if i != 123 {
		t.Errorf("got %d: expected %d", i, 123)
	}

	_, ok := sd.values["foo"]
	if ok {
		t.Errorf("got %v: expected %v", ok, false)
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}

	i = s.PopInt(ctx, "bar")
	if i != 0 {
		t.Errorf("got %d: expected %d", i, 0)
	}
}

func TestPopFloat(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = 123.456
	ctx := s.addSessionDataToContext(context.Background(), sd)

	f := s.PopFloat(ctx, "foo")
	if f != 123.456 {
		t.Errorf("got %f: expected %f", f, 123.456)
	}

	_, ok := sd.values["foo"]
	if ok {
		t.Errorf("got %v: expected %v", ok, false)
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}

	f = s.PopFloat(ctx, "bar")
	if f != 0.0 {
		t.Errorf("got %f: expected %f", f, 0.0)
	}
}

func TestPopBytes(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = []byte("bar")
	ctx := s.addSessionDataToContext(context.Background(), sd)

	b := s.PopBytes(ctx, "foo")
	if !bytes.Equal(b, []byte("bar")) {
		t.Errorf("got %v: expected %v", b, []byte("bar"))
	}
	_, ok := sd.values["foo"]
	if ok {
		t.Errorf("got %v: expected %v", ok, false)
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}

	b = s.PopBytes(ctx, "bar")
	if b != nil {
		t.Errorf("got %v: expected %v", b, nil)
	}
}

func TestPopTime(t *testing.T) {
	t.Parallel()

	now := time.Now()
	s := New()
	sd := newSessionData(time.Hour, 0)
	sd.values["foo"] = now
	ctx := s.addSessionDataToContext(context.Background(), sd)

	tm := s.PopTime(ctx, "foo")
	if tm != now {
		t.Errorf("got %v: expected %v", tm, now)
	}

	_, ok := sd.values["foo"]
	if ok {
		t.Errorf("got %v: expected %v", ok, false)
	}

	if sd.status != Modified {
		t.Errorf("got %v: expected %v", sd.status, "modified")
	}

	tm = s.PopTime(ctx, "baz")
	if !tm.IsZero() {
		t.Errorf("got %v: expected %v", tm, time.Time{})
	}

}

func TestStatus(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	ctx := s.addSessionDataToContext(context.Background(), sd)

	status := s.Status(ctx)
	if status != Unmodified {
		t.Errorf("got %d: expected %d", status, Unmodified)
	}

	s.Put(ctx, "foo", "bar")
	status = s.Status(ctx)
	if status != Modified {
		t.Errorf("got %d: expected %d", status, Modified)
	}

	if err := s.Destroy(ctx); err != nil {
		t.Errorf("unexpected error destroying session data: %v", err)
	}

	status = s.Status(ctx)
	if status != Destroyed {
		t.Errorf("got %d: expected %d", status, Destroyed)
	}
}

func TestDeadlineAndExpiry(t *testing.T) {
	t.Parallel()

	s := New()
	sd := newSessionData(time.Hour, 0)
	ctx := s.addSessionDataToContext(context.Background(), sd)

	expiry := s.Expiry(ctx)
	deadline := s.Deadline(ctx)

	if !expiry.Equal(deadline) {
		t.Errorf("got %v: expected %v", expiry, deadline)
	}

	sd = newSessionData(time.Hour, time.Minute)
	ctx = s.addSessionDataToContext(context.Background(), sd)

	expiry = s.Expiry(ctx)
	deadline = s.Deadline(ctx)

	if !expiry.Before(deadline) {
		t.Errorf("got %v: expected before %v", expiry, deadline)
	}
}

func TestSetDeadlineUpdatesExpiry(t *testing.T) {
	t.Parallel()

	s := New()
	s.Lifetime = 24 * time.Hour
	ctx := s.addSessionDataToContext(context.Background(), newSessionData(s.Lifetime, 0))
	s.Put(ctx, "userID", "42")

	deadline := time.Now().Add(15 * time.Minute).UTC()
	s.SetDeadline(ctx, deadline)

	if got := s.Expiry(ctx); !got.Equal(deadline) {
		t.Fatalf("Expiry() = %v; want %v", got, deadline)
	}

	token, expiry, err := s.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit() error: %v", err)
	}
	if !expiry.Equal(deadline) {
		t.Fatalf("Commit() expiry = %v; want %v", expiry, deadline)
	}

	loaded, err := s.Load(context.Background(), token)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got := s.GetString(loaded, "userID"); got != "42" {
		t.Fatalf("GetString() = %q; want %q", got, "42")
	}
}

func TestLoadRejectsExpiredDeadline(t *testing.T) {
	t.Parallel()

	s := New()
	s.Lifetime = 24 * time.Hour
	ctx := s.addSessionDataToContext(context.Background(), newSessionData(s.Lifetime, 0))
	s.Put(ctx, "userID", "42")
	s.SetDeadline(ctx, time.Now().Add(-time.Hour))

	token, _, err := s.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit() error: %v", err)
	}

	loaded, err := s.Load(context.Background(), token)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if got := s.GetString(loaded, "userID"); got != "" {
		t.Fatalf("expired session was restored with userID %q", got)
	}
}

func TestSetDeadlineHonoursIdleTimeout(t *testing.T) {
	t.Parallel()

	s := New()
	s.IdleTimeout = time.Minute
	ctx := s.addSessionDataToContext(context.Background(), newSessionData(time.Hour, s.IdleTimeout))

	deadline := time.Now().Add(time.Hour).UTC()
	s.SetDeadline(ctx, deadline)

	if got := s.Expiry(ctx); !got.Before(deadline) {
		t.Fatalf("Expiry() = %v; want before %v", got, deadline)
	}
}
