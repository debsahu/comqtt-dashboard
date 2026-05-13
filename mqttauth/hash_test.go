package mqttauth

import (
	"testing"

	pa "github.com/wind-c/comqtt/v2/plugin/auth"
)

// TestHashRoundtripVerifiesAgainstBroker proves that for every supported
// HashType, hashPassword produces a string that comqtt's own
// plugin/auth.CompareHash (the function broker plugins use to verify on
// OnConnectAuthenticate) accepts as a match. This is the core safety
// invariant of the whole mqttauth package: if it breaks, dashboard-created
// users cannot log in to the broker.
func TestHashRoundtripVerifiesAgainstBroker(t *testing.T) {
	const plain = "hunter2"
	const hmacKey = "shared-hmac-secret"

	cases := []struct {
		name string
		ht   HashType
		key  string
	}{
		{"none", HashNone, ""},
		{"bcrypt", HashBcrypt, ""},
		{"md5", HashMD5, ""},
		{"sha1", HashSHA1, ""},
		{"sha256", HashSHA256, ""},
		{"sha512", HashSHA512, ""},
		{"hmac-sha1", HashHmacSHA1, hmacKey},
		{"hmac-sha256", HashHmacSHA256, hmacKey},
		{"hmac-sha512", HashHmacSHA512, hmacKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := hashPassword(tc.ht, tc.key, plain)
			if h == "" {
				t.Fatalf("hashPassword returned empty string")
			}
			if !pa.CompareHash(h, plain, tc.key, pa.HashType(tc.ht)) {
				t.Fatalf("broker CompareHash rejected our hash: hashed=%q plain=%q key=%q ht=%v", h, plain, tc.key, tc.ht)
			}
		})
	}
}

func TestVerifyPasswordRejectsWrongPlaintext(t *testing.T) {
	for _, ht := range []HashType{HashNone, HashBcrypt, HashMD5, HashSHA256, HashHmacSHA256} {
		h := hashPassword(ht, "k", "right")
		if verifyPassword(ht, "k", h, "wrong") {
			t.Errorf("HashType=%v: wrong password verified as correct", ht)
		}
		if !verifyPassword(ht, "k", h, "right") {
			t.Errorf("HashType=%v: right password rejected", ht)
		}
	}
}
