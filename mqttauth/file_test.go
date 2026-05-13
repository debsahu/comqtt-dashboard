package mqttauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wind-c/comqtt/v2/mqtt/hooks/auth"
	"gopkg.in/yaml.v3"
)

func newFileBackendT(t *testing.T) (Backend, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.yml")
	b, err := New(Config{
		Kind:     "file",
		Mode:     ModeUsername,
		HashType: HashBcrypt,
		File:     &FileConfig{Path: path},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, path
}

func TestFileBackendKindAndMode(t *testing.T) {
	b, _ := newFileBackendT(t)
	if b.Kind() != "file" {
		t.Errorf("Kind()=%q want file", b.Kind())
	}
	if b.Mode() != ModeUsername {
		t.Errorf("Mode()=%v want username", b.Mode())
	}
	if b.HashType() != HashBcrypt {
		t.Errorf("HashType()=%v want bcrypt", b.HashType())
	}
}

func TestFileBackendEmptyWhenFileMissing(t *testing.T) {
	b, _ := newFileBackendT(t)
	users, err := b.Users(context.Background())
	if err != nil {
		t.Fatalf("Users on missing file should be empty, got err: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users, got %d", len(users))
	}
}

func TestFileBackendUserCRUD(t *testing.T) {
	b, path := newFileBackendT(t)
	ctx := context.Background()

	if err := b.PutUser(ctx, User{Subject: "alice", Allow: true}, "hunter2"); err != nil {
		t.Fatalf("PutUser create: %v", err)
	}
	if err := b.PutUser(ctx, User{Subject: "bob", Allow: false}, "passw0rd"); err != nil {
		t.Fatalf("PutUser create bob: %v", err)
	}

	users, err := b.Users(ctx)
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) != 2 || users[0].Subject != "alice" || users[1].Subject != "bob" {
		t.Fatalf("Users=%+v want sorted [alice, bob]", users)
	}
	if users[0].Allow != true || users[1].Allow != false {
		t.Errorf("Allow flags wrong: %+v", users)
	}

	// Update without password keeps the existing one.
	if err := b.PutUser(ctx, User{Subject: "alice", Allow: false}, ""); err != nil {
		t.Fatalf("PutUser update: %v", err)
	}
	u, err := b.GetUser(ctx, "alice")
	if err != nil || u.Allow != false {
		t.Fatalf("GetUser alice = %+v err=%v; want Allow=false", u, err)
	}
	// Verify password is still alice's original (round-trip via ledger).
	l := readLedger(t, path)
	stored := string(l.Users["alice"].Password)
	if !verifyPassword(HashBcrypt, "", stored, "hunter2") {
		t.Errorf("password update with empty plaintext should preserve original; got hash that does not verify against original")
	}

	// PutUser create without password is rejected.
	if err := b.PutUser(ctx, User{Subject: "charlie", Allow: true}, ""); err == nil {
		t.Errorf("PutUser create without password should error")
	}

	if err := b.DeleteUser(ctx, "bob"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := b.GetUser(ctx, "bob"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUser after delete = %v want ErrNotFound", err)
	}
	if err := b.DeleteUser(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteUser missing = %v want ErrNotFound", err)
	}
}

func TestFileBackendACLCRUD(t *testing.T) {
	b, _ := newFileBackendT(t)
	ctx := context.Background()

	// PutRule for non-existent user fails.
	if _, err := b.PutRule(ctx, ACLRule{Subject: "ghost", Topic: "x", Access: AccessRead}); !errors.Is(err, ErrNotFound) {
		t.Errorf("PutRule on missing user = %v want ErrNotFound", err)
	}

	// Create user first, then rules.
	if err := b.PutUser(ctx, User{Subject: "alice", Allow: true}, "p"); err != nil {
		t.Fatal(err)
	}
	id1, err := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "sensors/+/temp", Access: AccessRead})
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	id2, err := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "commands/#", Access: AccessWrite})
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	if id1 == id2 {
		t.Errorf("expected distinct IDs, got both %q", id1)
	}

	rules, err := b.Rules(ctx, "alice")
	if err != nil {
		t.Fatalf("Rules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("Rules len=%d want 2: %+v", len(rules), rules)
	}
	// Sorted by Topic
	if rules[0].Topic != "commands/#" || rules[1].Topic != "sensors/+/temp" {
		t.Errorf("Rules order: %+v", rules)
	}

	// Empty subject = all rules
	all, err := b.Rules(ctx, "")
	if err != nil {
		t.Fatalf("Rules(\"\"): %v", err)
	}
	if len(all) != 2 {
		t.Errorf("Rules(all) len=%d want 2", len(all))
	}

	if err := b.DeleteRule(ctx, id1); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	if err := b.DeleteRule(ctx, id1); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteRule again = %v want ErrNotFound", err)
	}

	rules, _ = b.Rules(ctx, "alice")
	if len(rules) != 1 || rules[0].ID != id2 {
		t.Errorf("After delete, rules=%+v want only id2=%s", rules, id2)
	}
}

func TestFileBackendPreservesWildcardRules(t *testing.T) {
	// Seed a file with top-level Auth + ACL rules that have wildcard
	// matchers (which the dashboard does not edit). Confirm dashboard CRUD
	// on Users leaves those rules intact.
	b, path := newFileBackendT(t)
	ctx := context.Background()

	seed := &auth.Ledger{
		Users: auth.Users{
			"alice": {Username: "alice", Password: auth.RString("seeded")},
		},
		Auth: auth.AuthRules{
			{Username: "service-*", Remote: "10.0.0.0/8", Allow: true},
		},
		ACL: auth.ACLRules{
			{Username: "*", Filters: auth.Filters{"$SYS/#": auth.Deny}},
		},
	}
	data, _ := yaml.Marshal(seed)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := b.PutUser(ctx, User{Subject: "bob", Allow: true}, "x"); err != nil {
		t.Fatal(err)
	}

	final := readLedger(t, path)
	if len(final.Auth) != 1 || final.Auth[0].Username != "service-*" {
		t.Errorf("top-level Auth rules dropped: %+v", final.Auth)
	}
	if len(final.ACL) != 1 || final.ACL[0].Username != "*" {
		t.Errorf("top-level ACL rules dropped: %+v", final.ACL)
	}
	if _, ok := final.Users["alice"]; !ok {
		t.Errorf("seeded user alice dropped")
	}
	if _, ok := final.Users["bob"]; !ok {
		t.Errorf("new user bob not added")
	}
}

func TestFileBackendAtomicWriteLeavesNoTempfile(t *testing.T) {
	b, path := newFileBackendT(t)
	ctx := context.Background()

	if err := b.PutUser(ctx, User{Subject: "alice", Allow: true}, "p"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".ledger-") {
			t.Errorf("tempfile leaked: %s", e.Name())
		}
	}
}

func TestEncodeDecodeACLIDRoundtrip(t *testing.T) {
	cases := []struct{ subject, topic string }{
		{"alice", "sensors/+/temp"},
		{"client_with_slash/in_id", "a/b/c"},
		{"", "#"}, // empty subject (decoded form)
		{"x", ""}, // empty topic
		{"unicode/ñ", "tópic/#"},
	}
	for _, c := range cases {
		id := encodeACLID(c.subject, c.topic)
		s, tp, err := decodeACLID(id)
		if err != nil {
			t.Errorf("decode(%q): %v", id, err)
			continue
		}
		if s != c.subject || tp != c.topic {
			t.Errorf("roundtrip subject/topic: got %q/%q want %q/%q", s, tp, c.subject, c.topic)
		}
	}
}

func readLedger(t *testing.T, path string) *auth.Ledger {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var l auth.Ledger
	if err := yaml.Unmarshal(data, &l); err != nil {
		t.Fatal(err)
	}
	return &l
}
