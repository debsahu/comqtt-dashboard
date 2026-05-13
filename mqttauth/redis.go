package mqttauth

import "context"

// redisBackend is the plugin/auth/redis HSET implementation. Lands in chunk
// 3 of v0.3.0.
type redisBackend struct {
	cfg Config
}

func newRedisBackend(cfg Config) (Backend, error) {
	return &redisBackend{cfg: cfg}, nil
}

func (b *redisBackend) Kind() string       { return "redis" }
func (b *redisBackend) Mode() AuthMode     { return b.cfg.Mode }
func (b *redisBackend) HashType() HashType { return b.cfg.HashType }

func (b *redisBackend) Users(context.Context) ([]User, error) { return nil, ErrUnsupported }
func (b *redisBackend) GetUser(context.Context, string) (*User, error) {
	return nil, ErrUnsupported
}
func (b *redisBackend) PutUser(context.Context, User, string) error { return ErrUnsupported }
func (b *redisBackend) DeleteUser(context.Context, string) error    { return ErrUnsupported }

func (b *redisBackend) Rules(context.Context, string) ([]ACLRule, error) {
	return nil, ErrUnsupported
}
func (b *redisBackend) PutRule(context.Context, ACLRule) (string, error) {
	return "", ErrUnsupported
}
func (b *redisBackend) DeleteRule(context.Context, string) error { return ErrUnsupported }

func (b *redisBackend) Close() error { return nil }
