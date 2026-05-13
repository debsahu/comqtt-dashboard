package mqttauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/wind-c/comqtt/v2/mqtt/hooks/auth"
	"gopkg.in/yaml.v3"
)

// fileBackend implements Backend against an auth.Ledger YAML file (the same
// file comqtt's built-in mqtt/hooks/auth.Hook reads in LedgerMode).
//
// Scope:
//   - User CRUD operates on the Users map (1:1 subject -> UserRule).
//   - ACL CRUD operates on per-user UserRule.ACL filter maps.
//   - The top-level Auth and ACL slices (which support Client/Username/Remote
//     wildcard matchers) are preserved verbatim on write but not editable
//     through the dashboard. Operators wanting wildcard rules edit the YAML
//     directly. Documented in the dashboard help text.
//
// Concurrency:
//   - In-process writes are serialized by mu. The dashboard is single-process
//     today; file-backend deployments are expected to be single-mode (one
//     broker pod, one filesystem).
//   - Writes use os.CreateTemp + os.Rename in the same directory so the
//     broker never observes a partial file. Caller must ensure the directory
//     is writable.
type fileBackend struct {
	cfg Config
	mu  sync.Mutex
}

func newFileBackend(cfg Config) (Backend, error) {
	if cfg.File == nil || cfg.File.Path == "" {
		return nil, errors.New("mqttauth file: Config.File.Path required")
	}
	// Surface permission/IO errors early on construction so the dashboard
	// fails to start rather than 500-ing on the first write attempt.
	if _, err := os.Stat(cfg.File.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("mqttauth file: stat %s: %w", cfg.File.Path, err)
	}
	return &fileBackend{cfg: cfg}, nil
}

func (b *fileBackend) Kind() string       { return "file" }
func (b *fileBackend) Mode() AuthMode     { return b.cfg.Mode }
func (b *fileBackend) HashType() HashType { return b.cfg.HashType }

func (b *fileBackend) Close() error { return nil }

// load reads and parses the YAML ledger file. Missing file is treated as
// an empty ledger so first-write succeeds without a pre-seed.
func (b *fileBackend) load() (*auth.Ledger, error) {
	data, err := os.ReadFile(b.cfg.File.Path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &auth.Ledger{Users: auth.Users{}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", b.cfg.File.Path, err)
	}
	var l auth.Ledger
	if err := yaml.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse %s: %w", b.cfg.File.Path, err)
	}
	if l.Users == nil {
		l.Users = auth.Users{}
	}
	return &l, nil
}

// save marshals the ledger and writes it atomically. CreateTemp in the same
// directory ensures the rename is atomic on POSIX filesystems.
func (b *fileBackend) save(l *auth.Ledger) error {
	data, err := yaml.Marshal(l)
	if err != nil {
		return fmt.Errorf("marshal ledger: %w", err)
	}
	dir := filepath.Dir(b.cfg.File.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".ledger-*.yml.tmp")
	if err != nil {
		return fmt.Errorf("create tempfile: %w", err)
	}
	tmpName := f.Name()
	defer os.Remove(tmpName) // no-op once rename succeeds
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write tempfile: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close tempfile: %w", err)
	}
	if err := os.Rename(tmpName, b.cfg.File.Path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmpName, b.cfg.File.Path, err)
	}
	return nil
}

func (b *fileBackend) Users(_ context.Context) ([]User, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, err := b.load()
	if err != nil {
		return nil, err
	}
	out := make([]User, 0, len(l.Users))
	for subject, rule := range l.Users {
		out = append(out, User{Subject: subject, Allow: !rule.Disallow})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject < out[j].Subject })
	return out, nil
}

func (b *fileBackend) GetUser(_ context.Context, subject string) (*User, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, err := b.load()
	if err != nil {
		return nil, err
	}
	rule, ok := l.Users[subject]
	if !ok {
		return nil, ErrNotFound
	}
	return &User{Subject: subject, Allow: !rule.Disallow}, nil
}

func (b *fileBackend) PutUser(_ context.Context, u User, plaintextPassword string) error {
	if u.Subject == "" {
		return errors.New("PutUser: Subject required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	l, err := b.load()
	if err != nil {
		return err
	}
	existing, exists := l.Users[u.Subject]
	if !exists && plaintextPassword == "" {
		return errors.New("PutUser: plaintextPassword required for new user")
	}
	rule := existing
	rule.Username = auth.RString(u.Subject)
	rule.Disallow = !u.Allow
	if plaintextPassword != "" {
		rule.Password = auth.RString(hashPassword(b.cfg.HashType, b.cfg.HashKey, plaintextPassword))
	}
	l.Users[u.Subject] = rule
	return b.save(l)
}

func (b *fileBackend) DeleteUser(_ context.Context, subject string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, err := b.load()
	if err != nil {
		return err
	}
	if _, ok := l.Users[subject]; !ok {
		return ErrNotFound
	}
	delete(l.Users, subject)
	return b.save(l)
}

func (b *fileBackend) Rules(_ context.Context, subject string) ([]ACLRule, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	l, err := b.load()
	if err != nil {
		return nil, err
	}
	out := make([]ACLRule, 0)
	collect := func(s string, rule auth.UserRule) {
		for filter, access := range rule.ACL {
			out = append(out, ACLRule{
				ID:      encodeACLID(s, string(filter)),
				Subject: s,
				Topic:   string(filter),
				Access:  Access(access),
			})
		}
	}
	if subject == "" {
		for s, rule := range l.Users {
			collect(s, rule)
		}
	} else if rule, ok := l.Users[subject]; ok {
		collect(subject, rule)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Topic < out[j].Topic
	})
	return out, nil
}

func (b *fileBackend) PutRule(_ context.Context, r ACLRule) (string, error) {
	if r.Subject == "" || r.Topic == "" {
		return "", errors.New("PutRule: Subject and Topic required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	l, err := b.load()
	if err != nil {
		return "", err
	}
	user, ok := l.Users[r.Subject]
	if !ok {
		return "", ErrNotFound // user must exist before ACL can be added
	}
	if user.ACL == nil {
		user.ACL = auth.Filters{}
	}
	user.ACL[auth.RString(r.Topic)] = auth.Access(r.Access)
	l.Users[r.Subject] = user
	if err := b.save(l); err != nil {
		return "", err
	}
	return encodeACLID(r.Subject, r.Topic), nil
}

func (b *fileBackend) DeleteRule(_ context.Context, id string) error {
	subject, topic, err := decodeACLID(id)
	if err != nil {
		return fmt.Errorf("DeleteRule: invalid id: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	l, err := b.load()
	if err != nil {
		return err
	}
	user, ok := l.Users[subject]
	if !ok || user.ACL == nil {
		return ErrNotFound
	}
	if _, ok := user.ACL[auth.RString(topic)]; !ok {
		return ErrNotFound
	}
	delete(user.ACL, auth.RString(topic))
	l.Users[subject] = user
	return b.save(l)
}

// encodeACLID combines subject and topic into one URL-safe id. Topic may
// contain '/' so a simple concatenation is ambiguous; base64url with a
// non-printable separator keeps the boundary unambiguous even after
// percent-decoding by HTTP path parsers.
const aclIDSep byte = 0x1F // ASCII unit separator

func encodeACLID(subject, topic string) string {
	buf := bytes.Buffer{}
	buf.WriteString(subject)
	buf.WriteByte(aclIDSep)
	buf.WriteString(topic)
	return base64.RawURLEncoding.EncodeToString(buf.Bytes())
}

func decodeACLID(id string) (subject, topic string, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", "", err
	}
	i := bytes.IndexByte(raw, aclIDSep)
	if i < 0 {
		return "", "", errors.New("missing separator")
	}
	return string(raw[:i]), string(raw[i+1:]), nil
}
