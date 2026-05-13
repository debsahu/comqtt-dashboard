package mqttauth

// Config is the disjoint-union of per-backend configurations. Exactly one of
// File/Redis/SQL is consulted, picked by Kind. Mode, ACLMode, HashType, and
// HashKey apply across all backends.
//
// Kind is the canonical lowercase backend name: "file" | "redis" | "mysql" |
// "postgres". Pass-through from the broker's --auth-ds flag.
type Config struct {
	Kind string

	// Mode is the lookup key used by user records.
	// ACLMode is the lookup key used by ACL records. Comqtt allows these to
	// differ (e.g. auth-by-username, ACL-by-clientid) so we keep them
	// independent.
	Mode    AuthMode
	ACLMode AuthMode

	// HashType is the algorithm used for password storage. HashKey is the
	// shared secret consumed by HMAC-* variants and ignored otherwise.
	HashType HashType
	HashKey  string

	// Per-backend specifics. The factory consults the field matching Kind
	// and returns an error if it is nil.
	File  *FileConfig
	Redis *RedisConfig
	SQL   *SQLConfig
}

// FileConfig points at a YAML ledger file matching the shape consumed by
// comqtt's built-in mqtt/hooks/auth.Hook running in LedgerMode.
type FileConfig struct {
	// Path is the YAML file. Must be writable by the dashboard process; the
	// file backend writes via atomic rename so a partial flush never makes
	// the broker see half a config.
	Path string
}

// RedisConfig matches the lookup shape of comqtt's plugin/auth/redis: a
// HASH at AuthKeyPrefix (default "comqtt:auth") and per-subject HASH at
// ACLKeyPrefix:<subject>.
type RedisConfig struct {
	Addr     string
	Username string
	Password string
	DB       int

	// AuthKeyPrefix defaults to "comqtt:auth" when empty (matching
	// plugin/auth/redis.defaultAuthkeyPrefix).
	AuthKeyPrefix string
	// ACLKeyPrefix defaults to "comqtt:acl" when empty.
	ACLKeyPrefix string
}

// SQLConfig is shared between MySQL and Postgres backends; Driver selects.
// Driver is one of "mysql" | "postgres". DSN is the database/sql connection
// string the chosen driver expects.
type SQLConfig struct {
	Driver string
	DSN    string

	Auth AuthTable
	ACL  ACLTable
}

// AuthTable mirrors plugin/auth/{mysql,postgresql}.AuthTable so the
// dashboard reads and writes the same physical table the broker reads.
type AuthTable struct {
	Table          string
	UserColumn     string
	PasswordColumn string
	AllowColumn    string
}

// ACLTable mirrors plugin/auth/{mysql,postgresql}.AclTable.
type ACLTable struct {
	Table        string
	UserColumn   string
	TopicColumn  string
	AccessColumn string
}
