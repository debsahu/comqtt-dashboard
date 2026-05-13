package mqttauth

import "context"

// fileBackend is the auth.Ledger YAML implementation. Implementation lives
// in chunk 2 of v0.3.0; this stub exists so the factory and surrounding
// dashboard code can be developed in parallel.
type fileBackend struct {
	cfg Config
}

func newFileBackend(cfg Config) (Backend, error) {
	return &fileBackend{cfg: cfg}, nil
}

func (b *fileBackend) Kind() string       { return "file" }
func (b *fileBackend) Mode() AuthMode     { return b.cfg.Mode }
func (b *fileBackend) HashType() HashType { return b.cfg.HashType }

func (b *fileBackend) Users(context.Context) ([]User, error) { return nil, ErrUnsupported }
func (b *fileBackend) GetUser(context.Context, string) (*User, error) {
	return nil, ErrUnsupported
}
func (b *fileBackend) PutUser(context.Context, User, string) error { return ErrUnsupported }
func (b *fileBackend) DeleteUser(context.Context, string) error    { return ErrUnsupported }

func (b *fileBackend) Rules(context.Context, string) ([]ACLRule, error) { return nil, ErrUnsupported }
func (b *fileBackend) PutRule(context.Context, ACLRule) (string, error) { return "", ErrUnsupported }
func (b *fileBackend) DeleteRule(context.Context, string) error         { return ErrUnsupported }

func (b *fileBackend) Close() error { return nil }
