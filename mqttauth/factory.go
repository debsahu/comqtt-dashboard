package mqttauth

import "fmt"

// New constructs the Backend for cfg.Kind. Returns an error if the per-Kind
// sub-config is missing or if Kind is unknown.
//
// Chunks 2-5 of v0.3.0 fill in the per-backend constructors. Until then,
// each constructor returns a stub that responds to interface calls with
// ErrUnsupported so the surrounding dashboard handler scaffold can be wired
// independently.
func New(cfg Config) (Backend, error) {
	switch cfg.Kind {
	case "file":
		if cfg.File == nil {
			return nil, fmt.Errorf("mqttauth: Kind=file requires Config.File")
		}
		return newFileBackend(cfg)
	case "redis":
		if cfg.Redis == nil {
			return nil, fmt.Errorf("mqttauth: Kind=redis requires Config.Redis")
		}
		return newRedisBackend(cfg)
	case "mysql":
		if cfg.SQL == nil || cfg.SQL.Driver != "mysql" {
			return nil, fmt.Errorf("mqttauth: Kind=mysql requires Config.SQL with Driver=mysql")
		}
		return newSQLBackend(cfg)
	case "postgres":
		if cfg.SQL == nil || cfg.SQL.Driver != "postgres" {
			return nil, fmt.Errorf("mqttauth: Kind=postgres requires Config.SQL with Driver=postgres")
		}
		return newSQLBackend(cfg)
	default:
		return nil, fmt.Errorf("mqttauth: unknown Kind %q (want file, redis, mysql, or postgres)", cfg.Kind)
	}
}
