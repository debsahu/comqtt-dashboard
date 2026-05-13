// Package mqttauth manages comqtt broker authentication and authorization
// state from the dashboard. Each Backend implementation reads and writes the
// same on-disk/on-wire shape comqtt's corresponding plugin/auth/* runtime
// hooks already consume, so a change made through the dashboard is visible
// to the running broker on its next lookup without any synchronization layer
// or process restart.
//
// Backends supported in v0.3.0:
//
//   - file:     auth.Ledger YAML file (built-in comqtt hook)
//   - redis:    plugin/auth/redis HSET/HGETALL key shape
//   - mysql:    plugin/auth/mysql configurable auth/acl tables
//   - postgres: plugin/auth/postgresql configurable auth/acl tables
//
// The dashboard's Auth and ACL pages consume Backend through a single
// interface. The active backend is selected by the cmd-binary based on
// cfg.Auth.Datasource and constructed via factory.New().
//
// This package does not manage the dashboard's own operator credentials
// (admin/viewer roles) - those live in dashboard/auth and are unrelated to
// MQTT-broker auth.
package mqttauth

import "errors"

// ErrUnsupported is returned by Backend methods when the operation is not
// supported by the active backend (e.g. user CRUD against an http-delegated
// auth backend, or wildcard-subject ACL queries against a key-value store
// that only indexes by exact subject).
var ErrUnsupported = errors.New("mqttauth: operation not supported by this backend")

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("mqttauth: not found")

// ErrConflict is returned when a write would create a duplicate of a unique
// record (e.g. a second user with the same username).
var ErrConflict = errors.New("mqttauth: conflict")
