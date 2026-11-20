package scs

import (
	"errors"
	"fmt"
	"net/http"
)

// Cookie policy errors. These are returned (possibly wrapped) before any
// session cookie is issued, so that a configuration which the browser would
// reject or refuse to send back fails fast instead of silently breaking the
// session.
var (
	ErrInvalidCookieName = errors.New("scs: invalid cookie name")
	ErrInvalidCookieAttr = errors.New("scs: invalid cookie attribute")
)

// isCookieNameRune reports whether r is a valid RFC 6265 token character. It
// mirrors the token validation used by net/http when serializing cookies.
func isCookieNameRune(r rune) bool {
	return r < 0x80 && cookieTokenTable[byte(r)]
}

var cookieTokenTable = [128]bool{
	'!': true, '#': true, '$': true, '%': true, '&': true, '\'': true,
	'*': true, '+': true, '-': true, '.': true, '^': true, '_': true,
	'`': true, '|': true, '~': true,

	'0': true, '1': true, '2': true, '3': true, '4': true,
	'5': true, '6': true, '7': true, '8': true, '9': true,

	'a': true, 'b': true, 'c': true, 'd': true, 'e': true, 'f': true,
	'g': true, 'h': true, 'i': true, 'j': true, 'k': true, 'l': true,
	'm': true, 'n': true, 'o': true, 'p': true, 'q': true, 'r': true,
	's': true, 't': true, 'u': true, 'v': true, 'w': true, 'x': true,
	'y': true, 'z': true,

	'A': true, 'B': true, 'C': true, 'D': true, 'E': true, 'F': true,
	'G': true, 'H': true, 'I': true, 'J': true, 'K': true, 'L': true,
	'M': true, 'N': true, 'O': true, 'P': true, 'Q': true, 'R': true,
	'S': true, 'T': true, 'U': true, 'V': true, 'W': true, 'X': true,
	'Y': true, 'Z': true,
}

// validateCookiePolicy verifies that the configured cookie name and
// SameSite, Secure and Partitioned attributes form a policy that a browser
// will actually store and return on subsequent requests.
//
// The enforced rules are:
//
//   - Name must be a non-empty RFC 6265 cookie name (no whitespace, commas,
//     colons, semicolons, backslashes, equals signs or control characters).
//     An invalid name makes net/http emit an empty Set-Cookie header, so the
//     session token would never reach the browser.
//   - SameSite=None requires Secure, otherwise browsers reject the cookie.
//   - Partitioned requires Secure.
//   - The "__Host-" name prefix requires Secure, Path="/" and an empty Domain.
//   - The "__Secure-" name prefix requires Secure.
func (s *SessionManager) validateCookiePolicy() error {
	name := s.Cookie.Name

	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidCookieName)
	}
	for _, r := range name {
		if !isCookieNameRune(r) {
			return fmt.Errorf("%w: %q contains disallowed characters", ErrInvalidCookieName, name)
		}
	}

	if s.Cookie.SameSite == http.SameSiteNoneMode && !s.Cookie.Secure {
		return fmt.Errorf("%w: SameSite=None requires the Secure attribute", ErrInvalidCookieAttr)
	}

	if s.Cookie.Partitioned && !s.Cookie.Secure {
		return fmt.Errorf("%w: Partitioned requires the Secure attribute", ErrInvalidCookieAttr)
	}

	if len(name) >= len("__Host-") && name[:len("__Host-")] == "__Host-" {
		switch {
		case !s.Cookie.Secure:
			return fmt.Errorf("%w: the __Host- name prefix requires the Secure attribute", ErrInvalidCookieAttr)
		case s.Cookie.Path != "/":
			return fmt.Errorf("%w: the __Host- name prefix requires Path to be \"/\"", ErrInvalidCookieAttr)
		case s.Cookie.Domain != "":
			return fmt.Errorf("%w: the __Host- name prefix requires an empty Domain", ErrInvalidCookieAttr)
		}
	}

	if len(name) >= len("__Secure-") && name[:len("__Secure-")] == "__Secure-" {
		if !s.Cookie.Secure {
			return fmt.Errorf("%w: the __Secure- name prefix requires the Secure attribute", ErrInvalidCookieAttr)
		}
	}

	return nil
}
