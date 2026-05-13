package mqttauth

// AuthMode selects what the lookup key is for auth and ACL records. It
// mirrors comqtt's auth.Access constants where it is overloaded as a mode
// indicator (0=anon, 1=username, 2=clientid).
type AuthMode uint8

const (
	ModeAnonymous AuthMode = iota
	ModeUsername
	ModeClientID
)

// String returns a stable lowercase mode name for templating and logs.
func (m AuthMode) String() string {
	switch m {
	case ModeAnonymous:
		return "anonymous"
	case ModeUsername:
		return "username"
	case ModeClientID:
		return "clientid"
	default:
		return "unknown"
	}
}

// HashType identifies which hashing algorithm a backend uses for stored
// passwords. Values match comqtt's plugin/auth.HashType so a dashboard
// configured with the same hash as the broker writes hashes the broker can
// verify.
type HashType uint8

const (
	HashNone HashType = iota
	HashBcrypt
	HashMD5
	HashSHA1
	HashSHA256
	HashSHA512
	HashHmacSHA1
	HashHmacSHA256
	HashHmacSHA512
)

// String returns the human-readable name used in admin UI dropdowns.
func (h HashType) String() string {
	switch h {
	case HashNone:
		return "none"
	case HashBcrypt:
		return "bcrypt"
	case HashMD5:
		return "md5"
	case HashSHA1:
		return "sha1"
	case HashSHA256:
		return "sha256"
	case HashSHA512:
		return "sha512"
	case HashHmacSHA1:
		return "hmac-sha1"
	case HashHmacSHA256:
		return "hmac-sha256"
	case HashHmacSHA512:
		return "hmac-sha512"
	default:
		return "unknown"
	}
}

// Access matches comqtt's auth.Access constants. The dashboard exposes the
// four values directly in ACL editing forms; backends store them as the same
// byte values.
type Access uint8

const (
	AccessDeny      Access = 0
	AccessRead      Access = 1 // subscribe only
	AccessWrite     Access = 2 // publish only
	AccessReadWrite Access = 3
)

// String returns the human-readable name used in ACL UI dropdowns.
func (a Access) String() string {
	switch a {
	case AccessDeny:
		return "deny"
	case AccessRead:
		return "subscribe"
	case AccessWrite:
		return "publish"
	case AccessReadWrite:
		return "pubsub"
	default:
		return "unknown"
	}
}

// User is the wire shape returned by Backend.Users / Backend.GetUser. Passwords
// are never read back through this struct; PutUser takes the plaintext
// separately.
type User struct {
	// Subject is the lookup key for this record: a username when the backend
	// is configured AuthMode=Username, or a clientID when AuthMode=ClientID.
	Subject string `json:"subject"`
	// Allow is whether the user may connect. False denies authentication
	// without removing the record (useful for temporary lockouts).
	Allow bool `json:"allow"`
}

// ACLRule is the wire shape for one row in the ACL table. ID is backend-
// assigned and opaque to callers (a row id for SQL backends, the topic filter
// for redis, or the slice index for the file backend).
type ACLRule struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Topic   string `json:"topic"`
	Access  Access `json:"access"`
}
