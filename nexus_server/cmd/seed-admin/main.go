// seed-admin: one-shot CLI to provision (or update) a verified, optionally
// admin Phaze account directly in the SQLite DB. Use case: bootstrapping
// the first account on a fresh deploy when email/SMTP isn't wired yet,
// or when you've locked yourself out and need to bypass the email
// verification round-trip.
//
// Safety:
//   - The bcrypt cost matches the live server (DefaultCost) so the hash
//     is indistinguishable from a normal user's.
//   - If the user already exists, the password and verified flag are
//     UPDATEd; nothing else is touched. Re-running is idempotent.
//   - -admin only flips is_admin to 1 (never demotes). Pair with the
//     env-driven PHAZE_ADMIN_USERS list on the server for redundancy.
//
// Run on Fly:
//
//   fly ssh console -a skype7-reborn -C \
//     "/app/seed-admin -username jack -password 'CHANGE-ME-IMMEDIATELY' \
//                      -email jack@example.com -admin"
//
// The same binary runs locally for dev DB pokes:
//
//   go run ./cmd/seed-admin -db ./nexus.db -username dev -password test1234 \
//                           -email dev@example.com -admin
package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,32}$`)

func main() {
	var (
		dbPath   = flag.String("db", envOr("DB_PATH", "/data/nexus.db"), "SQLite DB path")
		username = flag.String("username", "", "Username to create or update (required)")
		password = flag.String("password", "", "Plaintext password (>= 8 chars). If empty, reads from PHAZE_SEED_PASSWORD env or stdin.")
		email    = flag.String("email", "", "Email address (required for new accounts)")
		makeAdmin = flag.Bool("admin", false, "Promote this user to admin (idempotent)")
	)
	flag.Parse()

	if *username == "" {
		fatal("-username required")
	}
	if !usernameRegex.MatchString(*username) {
		fatal("invalid username (3-32 chars, a-z A-Z 0-9 . _ -)")
	}

	pw := *password
	if pw == "" {
		pw = strings.TrimSpace(os.Getenv("PHAZE_SEED_PASSWORD"))
	}
	if pw == "" {
		fatal("password required: pass -password '...' or set PHAZE_SEED_PASSWORD")
	}
	if len(pw) < 8 {
		fatal("password must be at least 8 characters")
	}

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		fatal("open db %q: %v", *dbPath, err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		log.Printf("warn: PRAGMA busy_timeout: %v", err)
	}

	// Discover which columns actually exist, then add any that are missing.
	existing := map[string]bool{}
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		fatal("PRAGMA table_info: %v", err)
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt sql.NullString
		_ = rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk)
		existing[name] = true
	}
	rows.Close()

	type colDef struct{ name, ddl string }
	for _, c := range []colDef{
		{"email", `ALTER TABLE users ADD COLUMN email TEXT`},
		{"mood", `ALTER TABLE users ADD COLUMN mood TEXT`},
		{"display_name", `ALTER TABLE users ADD COLUMN display_name TEXT`},
		{"is_verified", `ALTER TABLE users ADD COLUMN is_verified INTEGER DEFAULT 0`},
		{"verification_code", `ALTER TABLE users ADD COLUMN verification_code TEXT`},
		{"phone_number", `ALTER TABLE users ADD COLUMN phone_number TEXT`},
		{"phone_verified", `ALTER TABLE users ADD COLUMN phone_verified INTEGER DEFAULT 0`},
		{"is_admin", `ALTER TABLE users ADD COLUMN is_admin INTEGER DEFAULT 0`},
		{"is_banned", `ALTER TABLE users ADD COLUMN is_banned INTEGER DEFAULT 0`},
		{"ban_reason", `ALTER TABLE users ADD COLUMN ban_reason TEXT`},
	} {
		if !existing[c.name] {
			if _, err := db.Exec(c.ddl); err != nil {
				log.Printf("warn: add column %s: %v", c.name, err)
			} else {
				log.Printf("migrated: added column %s", c.name)
			}
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		fatal("bcrypt: %v", err)
	}

	// Does the user already exist?
	var existingID int
	err = db.QueryRow(`SELECT id FROM users WHERE username = ?`, *username).Scan(&existingID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if *email == "" {
			fatal("-email required when creating a new account")
		}
		// `salt` column is NOT NULL in the live schema. bcrypt already
		// embeds a per-hash salt; the column is legacy, so we just store
		// an empty string to satisfy the constraint.
		res, err := db.Exec(
			`INSERT INTO users (username, email, mood, display_name, password_hash, salt, is_verified)
			 VALUES (?, ?, '', ?, ?, '', 1)`,
			*username, *email, *username, string(hash))
		if err != nil {
			fatal("insert: %v", err)
		}
		id, _ := res.LastInsertId()
		fmt.Printf("created user %q (id=%d) verified\n", *username, id)
	case err != nil:
		fatal("select user: %v", err)
	default:
		_, err := db.Exec(
			`UPDATE users SET password_hash = ?, salt = '', is_verified = 1 WHERE username = ?`,
			string(hash), *username)
		if err != nil {
			fatal("update: %v", err)
		}
		fmt.Printf("updated user %q (id=%d) — password reset, verified\n", *username, existingID)
	}

	if *makeAdmin {
		// is_admin column may not exist yet on older schemas. Try the
		// UPDATE; if the column is missing, advise the operator to deploy
		// the new server first.
		if _, err := db.Exec(`UPDATE users SET is_admin = 1 WHERE username = ?`, *username); err != nil {
			if strings.Contains(err.Error(), "no such column: is_admin") {
				fmt.Println("note: is_admin column missing — deploy the new nexus_server build first, then re-run with -admin")
			} else {
				fatal("promote admin: %v", err)
			}
		} else {
			fmt.Printf("promoted %q to admin\n", *username)
		}
	}

	// Drop any banned flag in case this is a recovery path. Best-effort.
	if _, err := db.Exec(`UPDATE users SET is_banned = 0, ban_reason = '' WHERE username = ?`, *username); err != nil {
		// Pre-ban-column schemas hit "no such column"; that's fine.
		if !strings.Contains(err.Error(), "no such column") {
			log.Printf("warn: clear ban flag: %v", err)
		}
	}

	// Kill any stale session tokens so the user always gets a fresh login
	// after the password reset. Otherwise a token issued before reset can
	// still authenticate against the rotated password — confusing.
	if _, err := db.Exec(`UPDATE session_tokens SET revoked = 1 WHERE username = ?`, *username); err != nil {
		log.Printf("warn: revoke sessions: %v", err)
	}

	fmt.Printf("done — login with username %q and the password you supplied\n", *username)
}

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "seed-admin: "+format+"\n", args...)
	os.Exit(1)
}
