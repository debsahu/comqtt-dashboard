package mqttauth

import (
	"crypto/subtle"

	pa "github.com/wind-c/comqtt/v2/plugin/auth"
)

// hashPassword produces the stored representation of plaintext using the
// backend's configured HashType. The returned string is the exact byte shape
// the corresponding comqtt plugin reads on its OnConnectAuthenticate path,
// so a record written by the dashboard authenticates against the running
// broker without any reload step.
//
// key is consumed only by HMAC variants (HashHmac*); other hash types ignore
// it. plain may be empty - in that case the caller should treat the resulting
// hash as "do not change", not "set to empty".
func hashPassword(ht HashType, key, plain string) string {
	switch ht {
	case HashNone:
		return plain
	case HashBcrypt:
		return pa.Bcrypt(plain)
	case HashMD5:
		return pa.Md5(plain)
	case HashSHA1:
		return pa.Sha1(plain)
	case HashSHA256:
		return pa.Sha256(plain)
	case HashSHA512:
		return pa.Sha512(plain)
	case HashHmacSHA1:
		return pa.HmacSha1(plain, key)
	case HashHmacSHA256:
		return pa.HmacSha256(plain, key)
	case HashHmacSHA512:
		return pa.HmacSha512(plain, key)
	default:
		return plain
	}
}

// toUpstreamHash maps our HashType to comqtt's pa.HashType. Both enums use
// the same integer values; the cast is a thin documentation barrier.
func toUpstreamHash(h HashType) pa.HashType { return pa.HashType(h) }

// verifyPassword checks plain against the stored hashed value using the
// backend's HashType, with constant-time comparison for non-bcrypt variants.
// Not used by the dashboard at runtime (comqtt's broker performs auth
// itself); included for tests that want to verify a written record would
// successfully authenticate against the broker's CompareHash path.
func verifyPassword(ht HashType, key, hashed, plain string) bool {
	if ht == HashBcrypt {
		// Bcrypt has its own constant-time comparison internally.
		return pa.CompareHash(hashed, plain, key, toUpstreamHash(ht))
	}
	expected := hashPassword(ht, key, plain)
	return subtle.ConstantTimeCompare([]byte(hashed), []byte(expected)) == 1
}
