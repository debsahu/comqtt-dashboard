// Package comqttauthadapter adapts comqtt's broker config into the
// external comqttauth library's Config. Mirrors the shape of the local
// mqttauth/configloader.go but produces comqttauth.Config for use by
// the v0.4.0 regex feature. The two adapters coexist (one per package)
// because comqttauth and mqttauth are different Go modules.
package comqttauthadapter

import (
	"fmt"

	"github.com/debsahu/comqttauth"
	"github.com/wind-c/comqtt/v2/config"
	"github.com/wind-c/comqtt/v2/plugin"
	mauth "github.com/wind-c/comqtt/v2/plugin/auth/mysql"
	pauth "github.com/wind-c/comqtt/v2/plugin/auth/postgresql"
	rauth "github.com/wind-c/comqtt/v2/plugin/auth/redis"
)

// FromComqttConfig builds a *comqttauth.Config from comqtt's full config
// object by loading the backend-specific YAML at cfg.Auth.ConfPath.
// Returns (nil, nil) when there is no manageable backend (anonymous or
// HTTP-delegated auth); the caller should treat that as "regex feature
// not applicable here" and skip wiring.
func FromComqttConfig(cfg *config.Config) (*comqttauth.Config, error) {
	if cfg.Auth.Way == uint(0) { // AuthAnonymous - no users to manage.
		return nil, nil
	}
	mode := comqttauth.AuthMode(cfg.Auth.Way)

	switch cfg.Auth.Datasource {
	case uint(0): // AuthDSFree: built-in auth.Ledger YAML at ConfPath.
		return &comqttauth.Config{
			Kind:     "file",
			Mode:     mode,
			ACLMode:  mode,
			HashType: comqttauth.HashNone,
			File:     &comqttauth.FileConfig{Path: cfg.Auth.ConfPath},
		}, nil
	case uint(1): // AuthDSRedis
		var opts rauth.Options
		if err := plugin.LoadYaml(cfg.Auth.ConfPath, &opts); err != nil {
			return nil, fmt.Errorf("comqttauthadapter: load redis auth conf %s: %w", cfg.Auth.ConfPath, err)
		}
		out := &comqttauth.Config{
			Kind:     "redis",
			Mode:     comqttauth.AuthMode(opts.AuthMode),
			ACLMode:  comqttauth.AuthMode(opts.AclMode),
			HashType: comqttauth.HashType(opts.PasswordHash),
			HashKey:  opts.HashKey,
			Redis: &comqttauth.RedisConfig{
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
			return nil, fmt.Errorf("comqttauthadapter: load mysql auth conf %s: %w", cfg.Auth.ConfPath, err)
		}
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true",
			opts.Dsn.LoginName, opts.Dsn.LoginPassword,
			opts.Dsn.Host, opts.Dsn.Port, opts.Dsn.Schema, opts.Dsn.Charset)
		return &comqttauth.Config{
			Kind:     "mysql",
			Mode:     comqttauth.AuthMode(opts.AuthMode),
			ACLMode:  comqttauth.AuthMode(opts.AclMode),
			HashType: comqttauth.HashType(opts.Auth.PasswordHash),
			HashKey:  opts.Auth.HashKey,
			SQL: &comqttauth.SQLConfig{
				Driver: "mysql",
				DSN:    dsn,
				Auth: comqttauth.AuthTable{
					Table:          opts.Auth.Table,
					UserColumn:     opts.Auth.UserColumn,
					PasswordColumn: opts.Auth.PasswordColumn,
					AllowColumn:    opts.Auth.AllowColumn,
				},
				ACL: comqttauth.ACLTable{
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
			return nil, fmt.Errorf("comqttauthadapter: load postgresql auth conf %s: %w", cfg.Auth.ConfPath, err)
		}
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			opts.Dsn.LoginName, opts.Dsn.LoginPassword,
			opts.Dsn.Host, opts.Dsn.Port, opts.Dsn.Schema, opts.Dsn.SslMode)
		return &comqttauth.Config{
			Kind:     "postgres",
			Mode:     comqttauth.AuthMode(opts.AuthMode),
			ACLMode:  comqttauth.AuthMode(opts.AclMode),
			HashType: comqttauth.HashType(opts.Auth.PasswordHash),
			HashKey:  opts.Auth.HashKey,
			SQL: &comqttauth.SQLConfig{
				Driver: "postgres",
				DSN:    dsn,
				Auth: comqttauth.AuthTable{
					Table:          opts.Auth.Table,
					UserColumn:     opts.Auth.UserColumn,
					PasswordColumn: opts.Auth.PasswordColumn,
					AllowColumn:    opts.Auth.AllowColumn,
				},
				ACL: comqttauth.ACLTable{
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
	return nil, fmt.Errorf("comqttauthadapter: unknown auth datasource %d", cfg.Auth.Datasource)
}
