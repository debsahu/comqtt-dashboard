package mqttauth

import (
	"fmt"

	"github.com/wind-c/comqtt/v2/config"
	"github.com/wind-c/comqtt/v2/plugin"
	mauth "github.com/wind-c/comqtt/v2/plugin/auth/mysql"
	pauth "github.com/wind-c/comqtt/v2/plugin/auth/postgresql"
	rauth "github.com/wind-c/comqtt/v2/plugin/auth/redis"
)

// FromComqttConfig builds a Config from comqtt's full config object by
// loading the backend-specific YAML at cfg.Auth.ConfPath. Returns
// (nil, nil) when there is no manageable backend for the dashboard - i.e.
// when broker auth is anonymous or HTTP-delegated.
//
// Both cmd binaries call this once at startup; the returned Config is
// passed to New() to construct the runtime backend.
func FromComqttConfig(cfg *config.Config) (*Config, error) {
	if cfg.Auth.Way == uint(0) { // AuthAnonymous - no users to manage.
		return nil, nil
	}
	mode := AuthMode(cfg.Auth.Way) // Way 1 = username, 2 = clientid; matches our enum.

	switch cfg.Auth.Datasource {
	case uint(0): // AuthDSFree: built-in auth.Ledger YAML at ConfPath.
		return &Config{
			Kind:     "file",
			Mode:     mode,
			ACLMode:  mode,
			HashType: HashNone, // ledger stores plaintext passwords by default
			File:     &FileConfig{Path: cfg.Auth.ConfPath},
		}, nil
	case uint(1): // AuthDSRedis
		var opts rauth.Options
		if err := plugin.LoadYaml(cfg.Auth.ConfPath, &opts); err != nil {
			return nil, fmt.Errorf("mqttauth: load redis auth conf %s: %w", cfg.Auth.ConfPath, err)
		}
		out := &Config{
			Kind:     "redis",
			Mode:     AuthMode(opts.AuthMode),
			ACLMode:  AuthMode(opts.AclMode),
			HashType: HashType(opts.PasswordHash),
			HashKey:  opts.HashKey,
			Redis: &RedisConfig{
				AuthKeyPrefix: opts.AuthKeyPrefix,
				ACLKeyPrefix:  opts.AclKeyPrefix,
			},
		}
		if opts.RedisOptions != nil {
			out.Redis.Addr = opts.RedisOptions.Addr
			out.Redis.Username = opts.RedisOptions.Username
			out.Redis.Password = opts.RedisOptions.Password
			out.Redis.DB = opts.RedisOptions.DB
		}
		return out, nil
	case uint(2): // AuthDSMysql
		var opts mauth.Options
		if err := plugin.LoadYaml(cfg.Auth.ConfPath, &opts); err != nil {
			return nil, fmt.Errorf("mqttauth: load mysql auth conf %s: %w", cfg.Auth.ConfPath, err)
		}
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true",
			opts.Dsn.LoginName, opts.Dsn.LoginPassword,
			opts.Dsn.Host, opts.Dsn.Port, opts.Dsn.Schema, opts.Dsn.Charset)
		return &Config{
			Kind:     "mysql",
			Mode:     AuthMode(opts.AuthMode),
			ACLMode:  AuthMode(opts.AclMode),
			HashType: HashType(opts.Auth.PasswordHash),
			HashKey:  opts.Auth.HashKey,
			SQL: &SQLConfig{
				Driver: "mysql",
				DSN:    dsn,
				Auth: AuthTable{
					Table:          opts.Auth.Table,
					UserColumn:     opts.Auth.UserColumn,
					PasswordColumn: opts.Auth.PasswordColumn,
					AllowColumn:    opts.Auth.AllowColumn,
				},
				ACL: ACLTable{
					Table:        opts.Acl.Table,
					UserColumn:   opts.Acl.UserColumn,
					TopicColumn:  opts.Acl.TopicColumn,
					AccessColumn: opts.Acl.AccessColumn,
				},
			},
		}, nil
	case uint(3): // AuthDSPostgresql
		var opts pauth.Options
		if err := plugin.LoadYaml(cfg.Auth.ConfPath, &opts); err != nil {
			return nil, fmt.Errorf("mqttauth: load postgresql auth conf %s: %w", cfg.Auth.ConfPath, err)
		}
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			opts.Dsn.LoginName, opts.Dsn.LoginPassword,
			opts.Dsn.Host, opts.Dsn.Port, opts.Dsn.Schema, opts.Dsn.SslMode)
		return &Config{
			Kind:     "postgres",
			Mode:     AuthMode(opts.AuthMode),
			ACLMode:  AuthMode(opts.AclMode),
			HashType: HashType(opts.Auth.PasswordHash),
			HashKey:  opts.Auth.HashKey,
			SQL: &SQLConfig{
				Driver: "postgres",
				DSN:    dsn,
				Auth: AuthTable{
					Table:          opts.Auth.Table,
					UserColumn:     opts.Auth.UserColumn,
					PasswordColumn: opts.Auth.PasswordColumn,
					AllowColumn:    opts.Auth.AllowColumn,
				},
				ACL: ACLTable{
					Table:        opts.Acl.Table,
					UserColumn:   opts.Acl.UserColumn,
					TopicColumn:  opts.Acl.TopicColumn,
					AccessColumn: opts.Acl.AccessColumn,
				},
			},
		}, nil
	case uint(4): // AuthDSHttp - delegates externally, no CRUD surface.
		return nil, nil
	}
	return nil, fmt.Errorf("mqttauth: unknown auth datasource %d", cfg.Auth.Datasource)
}
