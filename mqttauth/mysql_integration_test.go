//go:build integration_sql

package mqttauth

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// To run:
//
//	docker run -d --rm --name mqttauth-mysql -p 3306:3306 \
//	  -e MYSQL_ROOT_PASSWORD=root -e MYSQL_DATABASE=comqtt mysql:8
//	# (wait ~15s for mysql to initialize, then)
//	MQTTAUTH_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/comqtt?parseTime=true' \
//	  go test -race -tags integration_sql ./mqttauth/...
//
// The schema mirrors plugin/auth/mysql/testdata/init.sql exactly so a
// dashboard-created user is the same row shape comqtt's runtime hook reads.

const mysqlSchema = `
CREATE TABLE IF NOT EXISTS auth (
    id INT AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(255) NOT NULL UNIQUE,
    password VARCHAR(255) NOT NULL,
    allow SMALLINT DEFAULT 1 NOT NULL,
    created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated TIMESTAMP NULL
);
CREATE TABLE IF NOT EXISTS acl (
    id INT AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(255) NOT NULL,
    topic VARCHAR(255) NOT NULL,
    access SMALLINT DEFAULT 3 NOT NULL,
    created TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated TIMESTAMP NULL
);
`

func newMySQLBackendT(t *testing.T) Backend {
	t.Helper()
	dsn := os.Getenv("MQTTAUTH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set MQTTAUTH_TEST_MYSQL_DSN=user:pw@tcp(host:3306)/db to enable; see file header")
	}
	if err := initSQLSchema(t, "mysql", dsn, mysqlSchema); err != nil {
		t.Fatalf("initSQLSchema: %v", err)
	}
	b, err := New(Config{
		Kind:     "mysql",
		Mode:     ModeUsername,
		HashType: HashBcrypt,
		SQL: &SQLConfig{
			Driver: "mysql",
			DSN:    dsn,
			Auth:   AuthTable{Table: "auth", UserColumn: "username", PasswordColumn: "password", AllowColumn: "allow"},
			ACL:    ACLTable{Table: "acl", UserColumn: "username", TopicColumn: "topic", AccessColumn: "access"},
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		_ = b.Close()
	})
	return b
}

// initSQLSchema creates tables if missing and truncates them so each test
// starts with a clean slate, regardless of what previous runs left behind.
// Splits the multi-statement schema on ';' (the schema text is internal so
// no risk of semicolons inside string literals).
func initSQLSchema(t *testing.T, driver, dsn, schema string) error {
	t.Helper()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	// Wait briefly for the DB to be reachable.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	for _, stmt := range splitSQL(schema) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, tbl := range []string{"auth", "acl"} {
		if _, err := db.ExecContext(ctx, "DELETE FROM "+tbl); err != nil {
			return err
		}
	}
	return nil
}

func splitSQL(s string) []string {
	out := []string{}
	cur := ""
	for _, ch := range s {
		if ch == ';' {
			if t := trim(cur); t != "" {
				out = append(out, t)
			}
			cur = ""
			continue
		}
		cur += string(ch)
	}
	if t := trim(cur); t != "" {
		out = append(out, t)
	}
	return out
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}
	return s[start:end]
}

func TestMySQL_KindAndMetadata(t *testing.T) {
	b := newMySQLBackendT(t)
	if b.Kind() != "mysql" {
		t.Errorf("Kind=%q want mysql", b.Kind())
	}
	if b.Mode() != ModeUsername {
		t.Errorf("Mode=%v want username", b.Mode())
	}
	if b.HashType() != HashBcrypt {
		t.Errorf("HashType=%v want bcrypt", b.HashType())
	}
}

func TestMySQL_UserCRUD(t *testing.T) {
	b := newMySQLBackendT(t)
	ctx := context.Background()

	if err := b.PutUser(ctx, User{Subject: "alice", Allow: true}, "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := b.PutUser(ctx, User{Subject: "bob", Allow: false}, "p"); err != nil {
		t.Fatal(err)
	}

	// Verify the stored password is what comqtt's CompareHash would accept.
	dsn := os.Getenv("MQTTAUTH_TEST_MYSQL_DSN")
	db, _ := sql.Open("mysql", dsn)
	defer db.Close()
	var stored string
	if err := db.QueryRow("SELECT password FROM auth WHERE username = ?", "alice").Scan(&stored); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if !verifyPassword(HashBcrypt, "", stored, "hunter2") {
		t.Errorf("stored password does not verify against original")
	}

	users, err := b.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Subject != "alice" || users[1].Subject != "bob" {
		t.Fatalf("Users=%+v want sorted [alice, bob]", users)
	}
	if users[0].Allow != true || users[1].Allow != false {
		t.Errorf("Allow flags: %+v", users)
	}

	// Update with empty password preserves the stored hash.
	if err := b.PutUser(ctx, User{Subject: "alice", Allow: false}, ""); err != nil {
		t.Fatal(err)
	}
	var stored2 string
	_ = db.QueryRow("SELECT password FROM auth WHERE username = ?", "alice").Scan(&stored2)
	if stored != stored2 {
		t.Errorf("password not preserved on update with empty plaintext")
	}

	// PutUser create without password is rejected.
	if err := b.PutUser(ctx, User{Subject: "charlie", Allow: true}, ""); err == nil {
		t.Errorf("PutUser create without password should error")
	}

	// Delete + delete-missing.
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

func TestMySQL_ACLCRUD(t *testing.T) {
	b := newMySQLBackendT(t)
	ctx := context.Background()

	id1, err := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "sensors/+/temp", Access: AccessRead})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := b.PutRule(ctx, ACLRule{Subject: "alice", Topic: "cmd/#", Access: AccessWrite})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PutRule(ctx, ACLRule{Subject: "bob", Topic: "#", Access: AccessReadWrite}); err != nil {
		t.Fatal(err)
	}

	all, err := b.Rules(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("Rules(all) len=%d want 3: %+v", len(all), all)
	}
	alice, _ := b.Rules(ctx, "alice")
	if len(alice) != 2 {
		t.Fatalf("Rules(alice) len=%d want 2", len(alice))
	}

	// Update existing rule.
	if _, err := b.PutRule(ctx, ACLRule{ID: id1, Subject: "alice", Topic: "sensors/+/temp", Access: AccessReadWrite}); err != nil {
		t.Fatal(err)
	}
	updated, _ := b.Rules(ctx, "alice")
	for _, r := range updated {
		if r.ID == id1 && r.Access != AccessReadWrite {
			t.Errorf("update did not change access: %+v", r)
		}
	}

	// Delete, twice.
	if err := b.DeleteRule(ctx, id2); err != nil {
		t.Fatal(err)
	}
	if err := b.DeleteRule(ctx, id2); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteRule twice = %v want ErrNotFound", err)
	}
}

func TestMySQL_DeleteUserCascadesACL(t *testing.T) {
	b := newMySQLBackendT(t)
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
		t.Errorf("after DeleteUser, ACL rules should be cascade-deleted, got %+v", rules)
	}
}
