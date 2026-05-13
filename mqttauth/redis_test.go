package mqttauth

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/wind-c/comqtt/v2/mqtt/hooks/auth"
)

func newRedisBackendT(t *testing.T, ht HashType) (Backend, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	b, err := New(Config{
		Kind:     "redis",
		Mode:     ModeUsername,
		HashType: ht,
		Redis:    &RedisConfig{Addr: mr.Addr()},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, mr
}

func TestRedisBackendKindAndDefaults(t *testing.T) {
	b, _ := newRedisBackendT(t, HashBcrypt)
	if b.Kind() != "redis" {
		t.Errorf("Kind()=%q want redis", b.Kind())
	}
}

func TestRedisBackendUserCRUD(t *testing.T) {
	b, mr := newRedisBackendT(t, HashBcrypt)
	ctx := context.Background()

	if err := b.PutUser(ctx, User{Subject: "alice", Allow: true}, "hunter2"); err != nil {
		t.Fatalf("PutUser: %v", err)
	}
	if err := b.PutUser(ctx, User{Subject: "bob", Allow: false}, "p"); err != nil {
		t.Fatal(err)
	}

	// Confirm wire shape matches what the broker plugin reads: HASH
	// "comqtt:auth" field=<subject> value=JSON.
	got := mr.HGet("comqtt:auth", "alice")
	if got == "" {
		t.Fatalf("miniredis HGet: empty (record missing)")
	}
	var ar auth.AuthRule
	if err := json.Unmarshal([]byte(got), &ar); err != nil {
		t.Fatalf("decode broker-shape JSON: %v body=%s", err, got)
	}
	if !ar.Allow {
		t.Errorf("Allow=false in stored record, want true")
	}
	if !verifyPassword(HashBcrypt, "", string(ar.Password), "hunter2") {
		t.Errorf("stored password does not verify against original")
	}

	users, err := b.Users(ctx)
	if err != nil {
		t.Fatalf("Users: %v", err)
	}
	if len(users) != 2 || users[0].Subject != "alice" || users[1].Subject != "bob" {
		t.Fatalf("Users=%+v want sorted [alice, bob]", users)
	}

	// Update without password preserves it.
	if err := b.PutUser(ctx, User{Subject: "alice", Allow: false}, ""); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Fresh variable to avoid carrying stale fields from the first decode:
	// auth.AuthRule uses `omitempty` so a zero Allow is omitted from JSON,
	// and Unmarshal won't reset fields the new payload doesn't mention.
	got = mr.HGet("comqtt:auth", "alice")
	var ar2 auth.AuthRule
	_ = json.Unmarshal([]byte(got), &ar2)
	if ar2.Allow {
		t.Errorf("Allow should be false after update")
	}
	if !verifyPassword(HashBcrypt, "", string(ar2.Password), "hunter2") {
		t.Errorf("password should be preserved on update with empty plaintext")
	}

	// PutUser create requires a password.
	if err := b.PutUser(ctx, User{Subject: "charlie", Allow: true}, ""); err == nil {
		t.Errorf("expected error creating user without password")
	}

	// Delete + delete-missing semantics.
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

func TestRedisBackendACLCRUD(t *testing.T) {
	b, mr := newRedisBackendT(t, HashNone)
	ctx := context.Background()

	// PutRule does NOT require a pre-existing user (matches broker
	// behavior; the broker just won't authenticate the user, but ACL data
	// is independent in redis).
	id1, err := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "sensors/+/temp", Access: AccessRead})
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	if _, err := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "commands/#", Access: AccessWrite}); err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	if _, err := b.PutRule(ctx, ACLRule{Subject: "bob", Topic: "#", Access: AccessReadWrite}); err != nil {
		t.Fatalf("PutRule: %v", err)
	}

	// Wire shape: HASH "comqtt:acl:<subject>" field=topic value=access int.
	v := mr.HGet("comqtt:acl:alice", "sensors/+/temp")
	if v != strconv.Itoa(int(AccessRead)) {
		t.Errorf("stored ACL access=%q want %d", v, AccessRead)
	}

	// Per-subject Rules.
	r, err := b.Rules(ctx, "alice")
	if err != nil {
		t.Fatalf("Rules alice: %v", err)
	}
	if len(r) != 2 {
		t.Fatalf("Rules alice len=%d want 2: %+v", len(r), r)
	}

	// All-subject Rules via SCAN.
	all, err := b.Rules(ctx, "")
	if err != nil {
		t.Fatalf("Rules all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("Rules(all) len=%d want 3: %+v", len(all), all)
	}

	// Delete one rule, leave the others.
	if err := b.DeleteRule(ctx, id1); err != nil {
		t.Fatalf("DeleteRule: %v", err)
	}
	if err := b.DeleteRule(ctx, id1); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteRule second time = %v want ErrNotFound", err)
	}

	remaining, _ := b.Rules(ctx, "alice")
	if len(remaining) != 1 || remaining[0].Topic != "commands/#" {
		t.Errorf("remaining after delete = %+v want only commands/#", remaining)
	}
}

func TestRedisBackendDeleteUserClearsACL(t *testing.T) {
	b, mr := newRedisBackendT(t, HashNone)
	ctx := context.Background()

	if err := b.PutUser(ctx, User{Subject: "alice", Allow: true}, "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "x", Access: AccessRead}); err != nil {
		t.Fatal(err)
	}
	if v := mr.HGet("comqtt:acl:alice", "x"); v == "" {
		t.Fatalf("setup: ACL hash missing")
	}

	if err := b.DeleteUser(ctx, "alice"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if mr.Exists("comqtt:acl:alice") {
		t.Errorf("ACL hash should be removed when user is deleted")
	}
}

func TestRedisBackendCustomPrefixes(t *testing.T) {
	mr := miniredis.RunT(t)
	b, err := New(Config{
		Kind:     "redis",
		Mode:     ModeUsername,
		HashType: HashNone,
		Redis: &RedisConfig{
			Addr:          mr.Addr(),
			AuthKeyPrefix: "my-auth",
			ACLKeyPrefix:  "my-acl",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })

	if err := b.PutUser(context.Background(), User{Subject: "u", Allow: true}, "p"); err != nil {
		t.Fatal(err)
	}
	if !mr.Exists("my-auth") {
		t.Errorf("custom AuthKeyPrefix not honored")
	}
	if _, err := b.PutRule(context.Background(), ACLRule{Subject: "u", Topic: "t", Access: AccessRead}); err != nil {
		t.Fatal(err)
	}
	if !mr.Exists("my-acl:u") {
		t.Errorf("custom ACLKeyPrefix not honored")
	}
}
