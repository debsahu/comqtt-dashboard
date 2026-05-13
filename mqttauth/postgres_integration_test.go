//go:build integration_sql

package mqttauth

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// To run:
//
//	docker run -d --rm --name mqttauth-postgres -p 5432:5432 \
//	  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=comqtt postgres:16
//	# wait ~5s for ready
//	MQTTAUTH_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:5432/comqtt?sslmode=disable' \
//	  go test -race -tags integration_sql -run TestPostgres ./mqttauth/...
//
// Schema mirrors plugin/auth/postgresql/testdata/init/init.sql exactly so
// dashboard-written rows are the same shape comqtt's runtime hook queries.

const postgresSchema = `
CREATE TABLE IF NOT EXISTS auth (
    id serial PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password TEXT NOT NULL,
    allow smallint DEFAULT 1 NOT NULL,
    created timestamp with time zone DEFAULT NOW(),
    updated timestamp
);
CREATE TABLE IF NOT EXISTS acl (
    id serial PRIMARY KEY,
    username TEXT NOT NULL,
    topic TEXT NOT NULL,
    access smallint DEFAULT 3 NOT NULL,
    created timestamp with time zone DEFAULT NOW(),
    updated timestamp
);
`

func newPostgresBackendT(t *testing.T) Backend {
	t.Helper()
	dsn := os.Getenv("MQTTAUTH_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set MQTTAUTH_TEST_POSTGRES_DSN=postgres://... to enable; see file header")
	}
	if err := initSQLSchema(t, "pgx", dsn, postgresSchema); err != nil {
		t.Fatalf("initSQLSchema: %v", err)
	}
	b, err := New(Config{
		Kind:     "postgres",
		Mode:     ModeUsername,
		HashType: HashBcrypt,
		SQL: &SQLConfig{
			Driver: "postgres",
			DSN:    dsn,
			Auth:   AuthTable{Table: "auth", UserColumn: "username", PasswordColumn: "password", AllowColumn: "allow"},
			ACL:    ACLTable{Table: "acl", UserColumn: "username", TopicColumn: "topic", AccessColumn: "access"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestPostgres_KindAndMetadata(t *testing.T) {
	b := newPostgresBackendT(t)
	if b.Kind() != "postgres" {
		t.Errorf("Kind=%q want postgres", b.Kind())
	}
}

func TestPostgres_UserCRUD(t *testing.T) {
	b := newPostgresBackendT(t)
	ctx := context.Background()

	if err := b.PutUser(ctx, User{Subject: "alice", Allow: true}, "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := b.PutUser(ctx, User{Subject: "bob", Allow: false}, "p"); err != nil {
		t.Fatal(err)
	}

	dsn := os.Getenv("MQTTAUTH_TEST_POSTGRES_DSN")
	db, _ := sql.Open("pgx", dsn)
	defer db.Close()
	var stored string
	if err := db.QueryRow("SELECT password FROM auth WHERE username = $1", "alice").Scan(&stored); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if !verifyPassword(HashBcrypt, "", stored, "hunter2") {
		t.Errorf("stored password does not verify against original")
	}

	users, _ := b.Users(ctx)
	if len(users) != 2 || users[0].Subject != "alice" || users[1].Subject != "bob" {
		t.Fatalf("Users=%+v", users)
	}
	if users[0].Allow != true || users[1].Allow != false {
		t.Errorf("Allow flags: %+v", users)
	}

	if err := b.PutUser(ctx, User{Subject: "alice", Allow: false}, ""); err != nil {
		t.Fatal(err)
	}
	var stored2 string
	_ = db.QueryRow("SELECT password FROM auth WHERE username = $1", "alice").Scan(&stored2)
	if stored != stored2 {
		t.Errorf("password not preserved on update with empty plaintext")
	}

	if err := b.PutUser(ctx, User{Subject: "charlie", Allow: true}, ""); err == nil {
		t.Errorf("create without password should error")
	}

	if err := b.DeleteUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.GetUser(ctx, "bob"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetUser after delete = %v want ErrNotFound", err)
	}
	if err := b.DeleteUser(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteUser missing = %v want ErrNotFound", err)
	}
}

func TestPostgres_ACLCRUD(t *testing.T) {
	b := newPostgresBackendT(t)
	ctx := context.Background()

	id1, err := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "sensors/+/temp", Access: AccessRead})
	if err != nil {
		t.Fatalf("PutRule: %v", err)
	}
	id2, _ := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "cmd/#", Access: AccessWrite})
	_, _ = b.PutRule(ctx, ACLRule{Subject: "bob", Topic: "#", Access: AccessReadWrite})

	all, err := b.Rules(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("Rules(all) len=%d want 3", len(all))
	}

	// Update via PutRule with ID set.
	if _, err := b.PutRule(ctx, ACLRule{ID: id1, Subject: "alice", Topic: "sensors/+/temp", Access: AccessReadWrite}); err != nil {
		t.Fatal(err)
	}
	updated, _ := b.Rules(ctx, "alice")
	for _, r := range updated {
		if r.ID == id1 && r.Access != AccessReadWrite {
			t.Errorf("update did not stick: %+v", r)
		}
	}

	if err := b.DeleteRule(ctx, id2); err != nil {
		t.Fatal(err)
	}
	if err := b.DeleteRule(ctx, id2); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteRule second = %v want ErrNotFound", err)
	}
}

func TestPostgres_DeleteUserCascadesACL(t *testing.T) {
	b := newPostgresBackendT(t)
	ctx := context.Background()

	if err := b.PutUser(ctx, User{Subject: "alice", Allow: true}, "p"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "x", Access: AccessRead}); err != nil {
		t.Fatal(err)
	}
	if err := b.DeleteUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	rules, _ := b.Rules(ctx, "alice")
	if len(rules) != 0 {
		t.Errorf("ACL rules not cascade-deleted: %+v", rules)
	}
}
