package mqttauth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"

	// Driver imports use blank-import side effects to register with
	// database/sql. The actual go-sql-driver/mysql and pgx/v5/stdlib
	// packages are already transitively in go.mod through comqtt itself.
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// sqlBackend implements Backend against the same auth/acl tables that
// plugin/auth/{mysql,postgresql} read. Table and column names are taken
// from cfg.SQL.Auth and cfg.SQL.ACL, mirroring upstream's configurable
// schema, so a dashboard write goes to the exact rows the broker queries
// on its next OnConnectAuthenticate / OnACLCheck.
//
// Concurrency: database/sql.DB is goroutine-safe; no extra mutex needed.
type sqlBackend struct {
	cfg     Config
	db      *sql.DB
	dialect dialect
}

// dialect captures the cross-database differences our queries need.
type dialect struct {
	driver string // "mysql" | "pgx"
	kind   string // "mysql" | "postgres" (matches Backend.Kind())

	// ph returns the n-th placeholder. MySQL uses "?" (n ignored), Postgres
	// uses "$N".
	ph func(n int) string

	// upsertUser builds the INSERT ... ON-CONFLICT-UPDATE statement that
	// inserts a user row or updates it on (user_col) conflict, setting
	// password and allow.
	upsertUser func(t AuthTable) string

	// insertReturningID builds the INSERT statement for ACL rules that
	// returns the new row's id. MySQL uses LastInsertId, Postgres uses
	// RETURNING; for uniform handling we emit RETURNING for Postgres and
	// rely on Result.LastInsertId for MySQL (driver-specific).
	insertRule func(t ACLTable) string // INSERT with placeholders only

	// supportsLastInsertID is true for MySQL; for Postgres we must use
	// QueryRow on an INSERT ... RETURNING id statement instead.
	supportsLastInsertID bool
}

// identRe rejects table/column names that could carry SQL injection. The
// names come from operator config at startup (not from end users), so this
// is defense in depth rather than the primary safety boundary.
var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateIdent(name, role string) error {
	if !identRe.MatchString(name) {
		return fmt.Errorf("mqttauth sql: invalid %s identifier %q", role, name)
	}
	return nil
}

func validateSQLConfig(s *SQLConfig) error {
	for _, id := range []struct{ name, role string }{
		{s.Auth.Table, "auth.table"},
		{s.Auth.UserColumn, "auth.user-column"},
		{s.Auth.PasswordColumn, "auth.password-column"},
		{s.Auth.AllowColumn, "auth.allow-column"},
		{s.ACL.Table, "acl.table"},
		{s.ACL.UserColumn, "acl.user-column"},
		{s.ACL.TopicColumn, "acl.topic-column"},
		{s.ACL.AccessColumn, "acl.access-column"},
	} {
		if err := validateIdent(id.name, id.role); err != nil {
			return err
		}
	}
	return nil
}

var mysqlDialect = dialect{
	driver:               "mysql",
	kind:                 "mysql",
	ph:                   func(int) string { return "?" },
	supportsLastInsertID: true,
	upsertUser: func(t AuthTable) string {
		return fmt.Sprintf(
			"INSERT INTO %s (%s, %s, %s) VALUES (?, ?, ?) "+
				"ON DUPLICATE KEY UPDATE %s = VALUES(%s), %s = VALUES(%s)",
			t.Table, t.UserColumn, t.PasswordColumn, t.AllowColumn,
			t.PasswordColumn, t.PasswordColumn, t.AllowColumn, t.AllowColumn,
		)
	},
	insertRule: func(t ACLTable) string {
		return fmt.Sprintf(
			"INSERT INTO %s (%s, %s, %s) VALUES (?, ?, ?)",
			t.Table, t.UserColumn, t.TopicColumn, t.AccessColumn,
		)
	},
}

var postgresDialect = dialect{
	driver:               "pgx",
	kind:                 "postgres",
	ph:                   func(n int) string { return fmt.Sprintf("$%d", n) },
	supportsLastInsertID: false,
	upsertUser: func(t AuthTable) string {
		return fmt.Sprintf(
			"INSERT INTO %s (%s, %s, %s) VALUES ($1, $2, $3) "+
				"ON CONFLICT (%s) DO UPDATE SET %s = EXCLUDED.%s, %s = EXCLUDED.%s",
			t.Table, t.UserColumn, t.PasswordColumn, t.AllowColumn,
			t.UserColumn, t.PasswordColumn, t.PasswordColumn, t.AllowColumn, t.AllowColumn,
		)
	},
	insertRule: func(t ACLTable) string {
		return fmt.Sprintf(
			"INSERT INTO %s (%s, %s, %s) VALUES ($1, $2, $3) RETURNING id",
			t.Table, t.UserColumn, t.TopicColumn, t.AccessColumn,
		)
	},
}

func newSQLBackend(cfg Config) (Backend, error) {
	if cfg.SQL == nil {
		return nil, errors.New("mqttauth sql: Config.SQL required")
	}
	if err := validateSQLConfig(cfg.SQL); err != nil {
		return nil, err
	}
	var d dialect
	switch cfg.SQL.Driver {
	case "mysql":
		d = mysqlDialect
	case "postgres":
		d = postgresDialect
	default:
		return nil, fmt.Errorf("mqttauth sql: unknown Driver %q (want mysql or postgres)", cfg.SQL.Driver)
	}
	db, err := sql.Open(d.driver, cfg.SQL.DSN)
	if err != nil {
		return nil, fmt.Errorf("mqttauth sql: open %s: %w", d.driver, err)
	}
	return &sqlBackend{cfg: cfg, db: db, dialect: d}, nil
}

func (b *sqlBackend) Kind() string       { return b.dialect.kind }
func (b *sqlBackend) Mode() AuthMode     { return b.cfg.Mode }
func (b *sqlBackend) HashType() HashType { return b.cfg.HashType }
func (b *sqlBackend) Close() error       { return b.db.Close() }

func (b *sqlBackend) auth() AuthTable { return b.cfg.SQL.Auth }
func (b *sqlBackend) acl() ACLTable   { return b.cfg.SQL.ACL }

func (b *sqlBackend) Users(ctx context.Context) ([]User, error) {
	q := fmt.Sprintf("SELECT %s, %s FROM %s ORDER BY %s",
		b.auth().UserColumn, b.auth().AllowColumn, b.auth().Table, b.auth().UserColumn)
	rows, err := b.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("sql Users: %w", err)
	}
	defer rows.Close()
	out := make([]User, 0)
	for rows.Next() {
		var (
			subject string
			allow   int
		)
		if err := rows.Scan(&subject, &allow); err != nil {
			return nil, err
		}
		out = append(out, User{Subject: subject, Allow: allow != 0})
	}
	return out, rows.Err()
}

func (b *sqlBackend) GetUser(ctx context.Context, subject string) (*User, error) {
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s",
		b.auth().AllowColumn, b.auth().Table, b.auth().UserColumn, b.dialect.ph(1))
	var allow int
	err := b.db.QueryRowContext(ctx, q, subject).Scan(&allow)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sql GetUser: %w", err)
	}
	return &User{Subject: subject, Allow: allow != 0}, nil
}

// getStoredPassword returns the current hashed password for subject. Used
// internally to implement "empty plaintext = preserve" on PutUser updates.
func (b *sqlBackend) getStoredPassword(ctx context.Context, subject string) (string, error) {
	q := fmt.Sprintf("SELECT %s FROM %s WHERE %s = %s",
		b.auth().PasswordColumn, b.auth().Table, b.auth().UserColumn, b.dialect.ph(1))
	var pw string
	err := b.db.QueryRowContext(ctx, q, subject).Scan(&pw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return pw, nil
}

func (b *sqlBackend) PutUser(ctx context.Context, u User, plaintextPassword string) error {
	if u.Subject == "" {
		return errors.New("PutUser: Subject required")
	}
	existing, err := b.getStoredPassword(ctx, u.Subject)
	exists := err == nil
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if !exists && plaintextPassword == "" {
		return errors.New("PutUser: plaintextPassword required for new user")
	}
	stored := existing
	if plaintextPassword != "" {
		stored = hashPassword(b.cfg.HashType, b.cfg.HashKey, plaintextPassword)
	}
	allow := 0
	if u.Allow {
		allow = 1
	}
	_, err = b.db.ExecContext(ctx, b.dialect.upsertUser(b.auth()), u.Subject, stored, allow)
	if err != nil {
		return fmt.Errorf("sql PutUser: %w", err)
	}
	return nil
}

func (b *sqlBackend) DeleteUser(ctx context.Context, subject string) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE %s = %s",
		b.auth().Table, b.auth().UserColumn, b.dialect.ph(1))
	res, err := b.db.ExecContext(ctx, q, subject)
	if err != nil {
		return fmt.Errorf("sql DeleteUser: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	// Cascade ACL rules belonging to this subject. No FK in upstream schema.
	cascade := fmt.Sprintf("DELETE FROM %s WHERE %s = %s",
		b.acl().Table, b.acl().UserColumn, b.dialect.ph(1))
	_, _ = b.db.ExecContext(ctx, cascade, subject)
	return nil
}

func (b *sqlBackend) Rules(ctx context.Context, subject string) ([]ACLRule, error) {
	var (
		q    string
		args []any
	)
	if subject == "" {
		q = fmt.Sprintf("SELECT id, %s, %s, %s FROM %s",
			b.acl().UserColumn, b.acl().TopicColumn, b.acl().AccessColumn, b.acl().Table)
	} else {
		q = fmt.Sprintf("SELECT id, %s, %s, %s FROM %s WHERE %s = %s",
			b.acl().UserColumn, b.acl().TopicColumn, b.acl().AccessColumn, b.acl().Table,
			b.acl().UserColumn, b.dialect.ph(1))
		args = []any{subject}
	}
	rows, err := b.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sql Rules: %w", err)
	}
	defer rows.Close()
	out := make([]ACLRule, 0)
	for rows.Next() {
		var (
			id     int64
			subj   string
			topic  string
			access int
		)
		if err := rows.Scan(&id, &subj, &topic, &access); err != nil {
			return nil, err
		}
		out = append(out, ACLRule{
			ID:      strconv.FormatInt(id, 10),
			Subject: subj,
			Topic:   topic,
			Access:  Access(access),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		return out[i].Topic < out[j].Topic
	})
	return out, nil
}

func (b *sqlBackend) PutRule(ctx context.Context, r ACLRule) (string, error) {
	if r.Subject == "" || r.Topic == "" {
		return "", errors.New("PutRule: Subject and Topic required")
	}
	if r.ID != "" {
		// UPDATE existing row.
		id, err := strconv.ParseInt(r.ID, 10, 64)
		if err != nil {
			return "", fmt.Errorf("PutRule: invalid id: %w", err)
		}
		q := fmt.Sprintf("UPDATE %s SET %s = %s, %s = %s, %s = %s WHERE id = %s",
			b.acl().Table,
			b.acl().UserColumn, b.dialect.ph(1),
			b.acl().TopicColumn, b.dialect.ph(2),
			b.acl().AccessColumn, b.dialect.ph(3),
			b.dialect.ph(4))
		res, err := b.db.ExecContext(ctx, q, r.Subject, r.Topic, int(r.Access), id)
		if err != nil {
			return "", fmt.Errorf("sql UpdateRule: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return "", ErrNotFound
		}
		return r.ID, nil
	}
	// INSERT new row.
	q := b.dialect.insertRule(b.acl())
	if b.dialect.supportsLastInsertID {
		res, err := b.db.ExecContext(ctx, q, r.Subject, r.Topic, int(r.Access))
		if err != nil {
			return "", fmt.Errorf("sql InsertRule: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(id, 10), nil
	}
	// Postgres path: RETURNING id.
	var id int64
	if err := b.db.QueryRowContext(ctx, q, r.Subject, r.Topic, int(r.Access)).Scan(&id); err != nil {
		return "", fmt.Errorf("sql InsertRule: %w", err)
	}
	return strconv.FormatInt(id, 10), nil
}

func (b *sqlBackend) DeleteRule(ctx context.Context, id string) error {
	parsed, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return fmt.Errorf("DeleteRule: invalid id: %w", err)
	}
	q := fmt.Sprintf("DELETE FROM %s WHERE id = %s", b.acl().Table, b.dialect.ph(1))
	res, err := b.db.ExecContext(ctx, q, parsed)
	if err != nil {
		return fmt.Errorf("sql DeleteRule: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
