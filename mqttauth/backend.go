package mqttauth

import "context"

// Backend is the unified interface the dashboard's Auth and ACL pages call.
// Each plugin/auth/* in comqtt has a corresponding implementation under this
// package's sub-trees.
//
// All Context-taking methods are expected to honor cancellation and obey any
// deadline set by callers (typically a 3s default applied by the handler
// layer to keep dashboard responsiveness predictable).
type Backend interface {
	// Kind returns a short stable name for the backend ("file", "redis",
	// "mysql", "postgres"). Used in admin UI badges and structured logging.
	Kind() string

	// Mode returns how user records are keyed (username or clientid). The UI
	// labels Subject columns accordingly.
	Mode() AuthMode

	// HashType returns the password hash algorithm this backend writes. The UI
	// labels password fields with this so operators know what they are
	// configuring.
	HashType() HashType

	// Users returns all user records. Returns an empty slice (not nil) when
	// no users exist.
	Users(ctx context.Context) ([]User, error)

	// GetUser returns the user with the given subject, or ErrNotFound.
	GetUser(ctx context.Context, subject string) (*User, error)

	// PutUser upserts a user record. plaintextPassword is hashed per
	// HashType() before write. Pass empty plaintextPassword to leave the
	// stored password unchanged on update; ErrNotFound on update of a missing
	// subject; the implementation distinguishes create vs update by existence
	// of the record.
	PutUser(ctx context.Context, u User, plaintextPassword string) error

	// DeleteUser removes the user with the given subject. Returns ErrNotFound
	// when no record matched.
	DeleteUser(ctx context.Context, subject string) error

	// Rules returns ACL rules for the given subject, or all rules when
	// subject is empty. Returns an empty slice when no rules match.
	Rules(ctx context.Context, subject string) ([]ACLRule, error)

	// PutRule inserts or updates an ACL rule. If r.ID is empty, the
	// implementation creates a new record and returns its assigned id.
	// Otherwise the existing record with that id is replaced; ErrNotFound if
	// the id does not exist.
	PutRule(ctx context.Context, r ACLRule) (string, error)

	// DeleteRule removes the ACL rule with the given id. ErrNotFound if no
	// record matched.
	DeleteRule(ctx context.Context, id string) error

	// Close releases any underlying connections. Idempotent.
	Close() error
}
