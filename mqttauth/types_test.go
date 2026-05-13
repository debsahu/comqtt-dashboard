package mqttauth

import "testing"

func TestAuthModeString(t *testing.T) {
	cases := map[AuthMode]string{
		ModeAnonymous: "anonymous",
		ModeUsername:  "username",
		ModeClientID:  "clientid",
		AuthMode(99):  "unknown",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("AuthMode(%d).String()=%q want %q", in, got, want)
		}
	}
}

func TestHashTypeString(t *testing.T) {
	cases := map[HashType]string{
		HashNone:       "none",
		HashBcrypt:     "bcrypt",
		HashMD5:        "md5",
		HashSHA1:       "sha1",
		HashSHA256:     "sha256",
		HashSHA512:     "sha512",
		HashHmacSHA1:   "hmac-sha1",
		HashHmacSHA256: "hmac-sha256",
		HashHmacSHA512: "hmac-sha512",
		HashType(99):   "unknown",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("HashType(%d).String()=%q want %q", in, got, want)
		}
	}
}

func TestAccessString(t *testing.T) {
	cases := map[Access]string{
		AccessDeny:      "deny",
		AccessRead:      "subscribe",
		AccessWrite:     "publish",
		AccessReadWrite: "pubsub",
		Access(99):      "unknown",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("Access(%d).String()=%q want %q", in, got, want)
		}
	}
}

// TestAccessIntegerStability is a guard against accidental enum reordering
// that would silently break stored ACL records (the integer values are
// persisted in redis values and SQL columns).
func TestAccessIntegerStability(t *testing.T) {
	expect := map[Access]uint8{
		AccessDeny:      0,
		AccessRead:      1,
		AccessWrite:     2,
		AccessReadWrite: 3,
	}
	for a, want := range expect {
		if uint8(a) != want {
			t.Errorf("Access %s = %d want %d (renumbering would invalidate stored ACL records)", a, uint8(a), want)
		}
	}
}

// TestAuthModeIntegerStability guards the persisted on-the-wire enum mapping
// with comqtt's auth-mode config (0=anon, 1=username, 2=clientid).
func TestAuthModeIntegerStability(t *testing.T) {
	expect := map[AuthMode]uint8{
		ModeAnonymous: 0,
		ModeUsername:  1,
		ModeClientID:  2,
	}
	for m, want := range expect {
		if uint8(m) != want {
			t.Errorf("AuthMode %s = %d want %d", m, uint8(m), want)
		}
	}
}

// TestHashTypeIntegerStability guards the persisted enum mapping with
// comqtt's plugin/auth.HashType (which we re-export via the toUpstreamHash
// cast). Any drift in the upstream enum would break our hashed records'
// verifiability against the broker.
func TestHashTypeIntegerStability(t *testing.T) {
	expect := map[HashType]uint8{
		HashNone: 0, HashBcrypt: 1, HashMD5: 2, HashSHA1: 3,
		HashSHA256: 4, HashSHA512: 5, HashHmacSHA1: 6, HashHmacSHA256: 7, HashHmacSHA512: 8,
	}
	for h, want := range expect {
		if uint8(h) != want {
			t.Errorf("HashType %s = %d want %d", h, uint8(h), want)
		}
	}
}
