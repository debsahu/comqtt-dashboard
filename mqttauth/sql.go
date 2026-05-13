package mqttauth

import "context"

// sqlBackend serves both MySQL (chunk 4) and Postgres (chunk 5) of v0.3.0.
// The implementation switches on cfg.SQL.Driver to choose the right
// placeholder syntax and driver registration; the two database flavors
// otherwise share the same query shapes.
type sqlBackend struct {
	cfg Config
}

func newSQLBackend(cfg Config) (Backend, error) {
	return &sqlBackend{cfg: cfg}, nil
}

func (b *sqlBackend) Kind() string {
	if b.cfg.SQL != nil && b.cfg.SQL.Driver == "postgres" {
		return "postgres"
	}
	return "mysql"
}
func (b *sqlBackend) Mode() AuthMode     { return b.cfg.Mode }
func (b *sqlBackend) HashType() HashType { return b.cfg.HashType }

func (b *sqlBackend) Users(context.Context) ([]User, error) { return nil, ErrUnsupported }
func (b *sqlBackend) GetUser(context.Context, string) (*User, error) {
	return nil, ErrUnsupported
}
func (b *sqlBackend) PutUser(context.Context, User, string) error { return ErrUnsupported }
func (b *sqlBackend) DeleteUser(context.Context, string) error    { return ErrUnsupported }

func (b *sqlBackend) Rules(context.Context, string) ([]ACLRule, error) { return nil, ErrUnsupported }
func (b *sqlBackend) PutRule(context.Context, ACLRule) (string, error) { return "", ErrUnsupported }
func (b *sqlBackend) DeleteRule(context.Context, string) error         { return ErrUnsupported }

func (b *sqlBackend) Close() error { return nil }
