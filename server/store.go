package server

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
	email      TEXT PRIMARY KEY,
	is_admin   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tokens (
	hash         BLOB PRIMARY KEY,
	email        TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER
);
CREATE TABLE IF NOT EXISTS domains (
	name       TEXT PRIMARY KEY,
	email      TEXT NOT NULL REFERENCES users(email) ON DELETE CASCADE,
	created_at INTEGER NOT NULL
);`

var (
	errLastAdmin      = errors.New("cannot remove the last admin")
	errDomainReserved = errors.New("domain is already reserved")
)

// Store holds the allowlist and the CLI tokens issued to it.
type Store struct {
	db *sql.DB
}

type User struct {
	Email    string
	IsAdmin  bool
	LastUsed time.Time // zero if the user never connected
}

func OpenStore(path string) (*Store, error) {
	// SQLite reports a missing directory as "out of memory (14)".
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// ponytail: one connection serializes all queries; fine for an allowlist,
	// raise it (with WAL) if token lookups ever queue up.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening database %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (st *Store) Close() error {
	return st.db.Close()
}

func (st *Store) HasAdmin() (bool, error) {
	var ok bool
	err := st.db.QueryRow(`SELECT EXISTS (SELECT 1 FROM users WHERE is_admin = 1)`).Scan(&ok)
	return ok, err
}

// User returns the allowlisted user with email, or ok=false if there is none.
func (st *Store) User(email string) (user User, ok bool, err error) {
	user.Email = email
	err = st.db.QueryRow(`SELECT is_admin FROM users WHERE email = ?`, email).Scan(&user.IsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, false, nil
	}
	return user, err == nil, err
}

func (st *Store) Users() ([]User, error) {
	rows, err := st.db.Query(`
		SELECT u.email, u.is_admin, COALESCE(MAX(t.last_used_at), 0)
		FROM users u LEFT JOIN tokens t ON t.email = u.email
		GROUP BY u.email ORDER BY u.email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var lastUsed int64
		if err := rows.Scan(&u.Email, &u.IsAdmin, &lastUsed); err != nil {
			return nil, err
		}
		if lastUsed != 0 {
			u.LastUsed = time.Unix(lastUsed, 0)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// AddUser allowlists email. Adding an existing user only ever raises them to admin.
func (st *Store) AddUser(email string, admin bool) error {
	_, err := st.db.Exec(`
		INSERT INTO users (email, is_admin, created_at) VALUES (?, ?, ?)
		ON CONFLICT (email) DO UPDATE SET is_admin = MAX(is_admin, excluded.is_admin)`,
		email, admin, time.Now().Unix())
	return err
}

// RemoveUser deletes email and, by cascade, its tokens.
func (st *Store) RemoveUser(email string) error {
	res, err := st.db.Exec(`
		DELETE FROM users WHERE email = ?
		AND (is_admin = 0 OR (SELECT COUNT(*) FROM users WHERE is_admin = 1) > 1)`, email)
	return lastAdminCheck(res, err)
}

func (st *Store) SetAdmin(email string, admin bool) error {
	res, err := st.db.Exec(`
		UPDATE users SET is_admin = ? WHERE email = ?
		AND (? OR is_admin = 0 OR (SELECT COUNT(*) FROM users WHERE is_admin = 1) > 1)`,
		admin, email, admin)
	return lastAdminCheck(res, err)
}

// lastAdminCheck turns "no row changed" into errLastAdmin. An unknown email also
// changes nothing; the admin panel only offers emails it listed, so the message fits.
func lastAdminCheck(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return errLastAdmin
	}
	return nil
}

// CreateToken issues a new CLI token for email. Only its hash is stored: the
// token is random, so a plain hash needs no salt or slow KDF.
func (st *Store) CreateToken(email string) (string, error) {
	token := rand.Text()
	hash := sha256.Sum256([]byte(token))
	_, err := st.db.Exec(`INSERT INTO tokens (hash, email, created_at) VALUES (?, ?, ?)`,
		hash[:], email, time.Now().Unix())
	return token, err
}

// TokenEmail returns the email a token belongs to and records its use. A token
// of a removed user is gone with the user, so ok=false covers both.
func (st *Store) TokenEmail(token string) (email string, ok bool, err error) {
	hash := sha256.Sum256([]byte(token))
	err = st.db.QueryRow(`UPDATE tokens SET last_used_at = ? WHERE hash = ? RETURNING email`,
		time.Now().Unix(), hash[:]).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return email, err == nil, err
}

// Domain is a subdomain reserved for one user.
type Domain struct {
	Name  string
	Email string
}

// DomainOwner returns who name is reserved for, or ok=false if it is free.
func (st *Store) DomainOwner(name string) (email string, ok bool, err error) {
	err = st.db.QueryRow(`SELECT email FROM domains WHERE name = ?`, name).Scan(&email)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return email, err == nil, err
}

func (st *Store) Domains() ([]Domain, error) {
	rows, err := st.db.Query(`SELECT name, email FROM domains ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []Domain
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.Name, &d.Email); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	return domains, rows.Err()
}

// ReserveDomain reserves name for email. A name reserved already, even for the
// same user, gives errDomainReserved: reassigning means releasing it first.
func (st *Store) ReserveDomain(name, email string) error {
	res, err := st.db.Exec(`INSERT INTO domains (name, email, created_at) VALUES (?, ?, ?)
		ON CONFLICT (name) DO NOTHING`, name, email, time.Now().Unix())
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return errDomainReserved
	}
	return nil
}

func (st *Store) ReleaseDomain(name string) error {
	_, err := st.db.Exec(`DELETE FROM domains WHERE name = ?`, name)
	return err
}
