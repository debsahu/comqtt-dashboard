package mqttauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	rv8 "github.com/redis/go-redis/v9"
	"github.com/wind-c/comqtt/v2/mqtt/hooks/auth"
)

// Default key prefixes mirror comqtt's plugin/auth/redis.defaultAuth/Acl
// KeyPrefix. Operators who customize these in the broker config must pass
// the same values via RedisConfig so the dashboard reads the same keys.
const (
	defaultRedisAuthPrefix = "comqtt:auth"
	defaultRedisACLPrefix  = "comqtt:acl"
)

// redisBackend implements Backend against the same key shape that
// plugin/auth/redis reads:
//
//	HASH  <AuthKeyPrefix>            field=<subject>  value=JSON{"allow":bool,"password":"<hashed>"}
//	HASH  <ACLKeyPrefix>:<subject>   field=<topic>    value=<access integer as string>
//
// Writes go straight to redis; the running broker picks them up on its next
// lookup with no reload step.
type redisBackend struct {
	cfg       Config
	db        *rv8.Client
	authKey   string
	aclPrefix string
}

func newRedisBackend(cfg Config) (Backend, error) {
	if cfg.Redis == nil {
		return nil, errors.New("mqttauth redis: Config.Redis required")
	}
	r := cfg.Redis
	if r.Addr == "" {
		return nil, errors.New("mqttauth redis: Config.Redis.Addr required")
	}
	b := &redisBackend{
		cfg: cfg,
		db: rv8.NewClient(&rv8.Options{
			Addr:     r.Addr,
			Username: r.Username,
			Password: r.Password,
			DB:       r.DB,
		}),
		authKey:   defaultIfEmpty(r.AuthKeyPrefix, defaultRedisAuthPrefix),
		aclPrefix: defaultIfEmpty(r.ACLKeyPrefix, defaultRedisACLPrefix),
	}
	return b, nil
}

func defaultIfEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func (b *redisBackend) Kind() string       { return "redis" }
func (b *redisBackend) Mode() AuthMode     { return b.cfg.Mode }
func (b *redisBackend) HashType() HashType { return b.cfg.HashType }
func (b *redisBackend) Close() error       { return b.db.Close() }

func (b *redisBackend) aclKey(subject string) string {
	return b.aclPrefix + ":" + subject
}

func (b *redisBackend) Users(ctx context.Context) ([]User, error) {
	all, err := b.db.HGetAll(ctx, b.authKey).Result()
	if err != nil {
		return nil, fmt.Errorf("redis HGETALL %s: %w", b.authKey, err)
	}
	out := make([]User, 0, len(all))
	for subject, raw := range all {
		var ar auth.AuthRule
		if err := json.Unmarshal([]byte(raw), &ar); err != nil {
			continue // skip malformed records rather than fail the whole list
		}
		out = append(out, User{Subject: subject, Allow: ar.Allow})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out, nil
}

func (b *redisBackend) GetUser(ctx context.Context, subject string) (*User, error) {
	raw, err := b.db.HGet(ctx, b.authKey, subject).Result()
	if errors.Is(err, rv8.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis HGET %s %s: %w", b.authKey, subject, err)
	}
	var ar auth.AuthRule
	if err := json.Unmarshal([]byte(raw), &ar); err != nil {
		return nil, fmt.Errorf("decode AuthRule: %w", err)
	}
	return &User{Subject: subject, Allow: ar.Allow}, nil
}

func (b *redisBackend) PutUser(ctx context.Context, u User, plaintextPassword string) error {
	if u.Subject == "" {
		return errors.New("PutUser: Subject required")
	}
	existingRaw, getErr := b.db.HGet(ctx, b.authKey, u.Subject).Result()
	exists := getErr == nil
	if !exists && !errors.Is(getErr, rv8.Nil) && getErr != nil {
		return fmt.Errorf("redis HGET %s: %w", u.Subject, getErr)
	}
	if !exists && plaintextPassword == "" {
		return errors.New("PutUser: plaintextPassword required for new user")
	}
	var rule auth.AuthRule
	if exists {
		if err := json.Unmarshal([]byte(existingRaw), &rule); err != nil {
			return fmt.Errorf("decode existing AuthRule: %w", err)
		}
	}
	rule.Allow = u.Allow
	if plaintextPassword != "" {
		rule.Password = auth.RString(hashPassword(b.cfg.HashType, b.cfg.HashKey, plaintextPassword))
	}
	encoded, err := json.Marshal(rule)
	if err != nil {
		return fmt.Errorf("encode AuthRule: %w", err)
	}
	if _, err := b.db.HSet(ctx, b.authKey, u.Subject, string(encoded)).Result(); err != nil {
		return fmt.Errorf("redis HSET %s %s: %w", b.authKey, u.Subject, err)
	}
	return nil
}

func (b *redisBackend) DeleteUser(ctx context.Context, subject string) error {
	deleted, err := b.db.HDel(ctx, b.authKey, subject).Result()
	if err != nil {
		return fmt.Errorf("redis HDEL %s %s: %w", b.authKey, subject, err)
	}
	if deleted == 0 {
		return ErrNotFound
	}
	// Best-effort: also drop the user's ACL hash so a deleted user does not
	// leave orphan rules visible to future Rules("") scans. The broker would
	// ignore them anyway (no auth match), but the dashboard listing is
	// cleaner.
	_ = b.db.Del(ctx, b.aclKey(subject)).Err()
	return nil
}

func (b *redisBackend) Rules(ctx context.Context, subject string) ([]ACLRule, error) {
	out := make([]ACLRule, 0)
	collect := func(s string) error {
		filters, err := b.db.HGetAll(ctx, b.aclKey(s)).Result()
		if err != nil {
			return fmt.Errorf("redis HGETALL %s: %w", b.aclKey(s), err)
		}
		for filter, accessStr := range filters {
			access, err := strconv.Atoi(accessStr)
			if err != nil {
				continue
			}
			out = append(out, ACLRule{
				ID:      encodeACLID(s, filter),
				Subject: s,
				Topic:   filter,
				Access:  Access(access),
			})
		}
		return nil
	}
	if subject != "" {
		if err := collect(subject); err != nil {
			return nil, err
		}
	} else {
		// SCAN keys matching the ACL prefix to enumerate every subject.
		// SCAN is preferred over KEYS (non-blocking, paginated).
		pattern := b.aclPrefix + ":*"
		iter := b.db.Scan(ctx, 0, pattern, 200).Iterator()
		for iter.Next(ctx) {
			key := iter.Val()
			// Subject is the suffix after the prefix + ":".
			s := key[len(b.aclPrefix)+1:]
			if err := collect(s); err != nil {
				return nil, err
			}
		}
		if err := iter.Err(); err != nil {
			return nil, fmt.Errorf("redis SCAN %s: %w", pattern, err)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Topic < out[j].Topic
	})
	return out, nil
}

func (b *redisBackend) PutRule(ctx context.Context, r ACLRule) (string, error) {
	if r.Subject == "" || r.Topic == "" {
		return "", errors.New("PutRule: Subject and Topic required")
	}
	if _, err := b.db.HSet(ctx, b.aclKey(r.Subject), r.Topic, strconv.Itoa(int(r.Access))).Result(); err != nil {
		return "", fmt.Errorf("redis HSET %s %s: %w", b.aclKey(r.Subject), r.Topic, err)
	}
	return encodeACLID(r.Subject, r.Topic), nil
}

func (b *redisBackend) DeleteRule(ctx context.Context, id string) error {
	subject, topic, err := decodeACLID(id)
	if err != nil {
		return fmt.Errorf("DeleteRule: invalid id: %w", err)
	}
	deleted, err := b.db.HDel(ctx, b.aclKey(subject), topic).Result()
	if err != nil {
		return fmt.Errorf("redis HDEL %s %s: %w", b.aclKey(subject), topic, err)
	}
	if deleted == 0 {
		return ErrNotFound
	}
	return nil
}
