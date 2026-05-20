package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
	_ "modernc.org/sqlite"
)

// ---------- CORS / Rate limiting ----------

var allowedOrigins = func() map[string]bool {
	raw := os.Getenv("Phaze_ALLOWED_ORIGINS")
	if raw == "" {
		return nil // nil = allow all (dev mode)
	}
	m := map[string]bool{}
	for _, o := range strings.Split(raw, ",") {
		m[strings.TrimSpace(o)] = true
	}
	return m
}()

func originAllowed(r *http.Request) bool {
	if allowedOrigins == nil {
		return true
	}
	return allowedOrigins[r.Header.Get("Origin")]
}

// pstnBridgeEnabled gates Twilio outbound PSTN. Default off so relays run
// WebRTC-only (Phaze-to-Phaze) with no carrier or Twilio call charges.
func pstnBridgeEnabled() bool {
	return strings.EqualFold(os.Getenv("PHAZE_ENABLE_PSTN"), "true")
}

type limiterEntry struct {
	lim  *rate.Limiter
	last time.Time
}

type ipLimiter struct {
	mu       sync.Mutex
	entries  map[string]*limiterEntry
	r        rate.Limit
	burst    int
	idleTTL  time.Duration
	maxSize  int
	lastGC   time.Time
	gcEvery  time.Duration
}

func newIPLimiter(r rate.Limit, burst int) *ipLimiter {
	return &ipLimiter{
		entries: map[string]*limiterEntry{},
		r:       r,
		burst:   burst,
		idleTTL: 10 * time.Minute,
		maxSize: 50000,
		gcEvery: 1 * time.Minute,
	}
}

// gcLocked evicts idle limiters. Caller must hold l.mu.
func (l *ipLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastGC) < l.gcEvery && len(l.entries) < l.maxSize {
		return
	}
	cutoff := now.Add(-l.idleTTL)
	for ip, e := range l.entries {
		if e.last.Before(cutoff) {
			delete(l.entries, ip)
		}
	}
	// If still over cap, drop oldest opportunistically.
	if len(l.entries) > l.maxSize {
		for ip := range l.entries {
			delete(l.entries, ip)
			if len(l.entries) <= l.maxSize*9/10 {
				break
			}
		}
	}
	l.lastGC = now
}

func (l *ipLimiter) allow(ip string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked(now)
	e, ok := l.entries[ip]
	if !ok {
		e = &limiterEntry{lim: rate.NewLimiter(l.r, l.burst)}
		l.entries[ip] = e
	}
	e.last = now
	return e.lim.Allow()
}

// trustedProxyHeader is set when the server runs behind a known reverse proxy
// (Fly.io edge, Cloudflare, nginx). Empty = ignore X-Forwarded-For entirely so
// attackers cannot spoof a source IP to bypass per-IP rate limits.
var trustedProxyHeader = strings.TrimSpace(os.Getenv("PHAZE_TRUST_PROXY_HEADER"))

func clientIP(r *http.Request) string {
	if trustedProxyHeader != "" {
		if v := strings.TrimSpace(r.Header.Get(trustedProxyHeader)); v != "" {
			// Take leftmost entry (original client). Edge proxies append on the right.
			if i := strings.Index(v, ","); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			if v != "" {
				return v
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

var globalLimiter = newIPLimiter(rate.Limit(10), 30) // 10 req/s, burst 30 per IP

func rateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !globalLimiter.allow(clientIP(r)) {
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

// NexusMessage is the wire protocol for Phaze™
type NexusMessage struct {
	Type        string      `json:"type"`
	Sender      string      `json:"sender"`
	Recipient   string      `json:"recipient"`
	Body        string      `json:"body"`
	Status      string      `json:"status"`
	Results     []string    `json:"results"`
	SDP         string      `json:"sdp"`
	Candidate   string      `json:"candidate"`
	Token       string      `json:"token"`
	Error       string      `json:"error"`
	Email       string      `json:"email,omitempty"`
	Mood        string      `json:"mood,omitempty"`
	DisplayName string      `json:"display_name,omitempty"`
	ConvoID     string      `json:"convo_id,omitempty"`
	ConvoName   string      `json:"convo_name,omitempty"`
	Members     []string    `json:"members,omitempty"`
	TurnConfig  *TurnConfig `json:"turn_config,omitempty"`
	TOTPCode    string      `json:"totp_code,omitempty"`
	TOTPURI     string      `json:"totp_uri,omitempty"`
	QRToken     string      `json:"qr_token,omitempty"`
	QRData      string      `json:"qr_data,omitempty"`
	DeviceInfo  string      `json:"device_info,omitempty"`

	// Envelopes[recipient] = ciphertext body encrypted to that member's key.
	// Used for group E2EE: the client fans out per-member ciphertext so the
	// server never sees plaintext. Only set on "convo_msg".
	Envelopes map[string]string `json:"envelopes,omitempty"`

	// PublicKey / KeyFingerprint forwarded so pairwise TOFU still works when
	// a client is about to send a group envelope to a member it hasn't keyed.
	PublicKey      []byte `json:"public_key,omitempty"`
	KeyFingerprint string `json:"key_fingerprint,omitempty"`

	// --- Servers + Channels (Discord-style "Spaces") ---
	ServerID    string             `json:"server_id,omitempty"`
	ChannelID   string             `json:"channel_id,omitempty"`
	ServerName  string             `json:"server_name,omitempty"`
	ChannelName string             `json:"channel_name,omitempty"`
	Topic       string             `json:"topic,omitempty"`
	Kind        string             `json:"kind,omitempty"`  // "text" | "voice"
	Role        string             `json:"role,omitempty"`  // member | admin | owner
	Visibility  string             `json:"visibility,omitempty"` // public | private
	InviteCode  string             `json:"invite_code,omitempty"`
	Servers     []ServerSummary    `json:"servers,omitempty"`
	Channels    []ChannelInfo      `json:"channels,omitempty"`
	Messages    []ChannelMsg       `json:"messages,omitempty"`
	HistoryFrom int64              `json:"history_from,omitempty"` // id cursor (return messages with id < this)
}

// ServerSummary is the slim view a client gets for the server-list pane.
type ServerSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Owner       string `json:"owner"`
	Visibility  string `json:"visibility"`
	Role        string `json:"role"`
	InviteCode  string `json:"invite_code,omitempty"`
}

// ChannelInfo is one channel inside a server.
type ChannelInfo struct {
	ID       string `json:"id"`
	ServerID string `json:"server_id"`
	Name     string `json:"name"`
	Topic    string `json:"topic,omitempty"`
	Kind     string `json:"kind"`
	Position int    `json:"position"`
}

// ChannelMsg is one row of channel chat history (plaintext server-side).
type ChannelMsg struct {
	ID        int64  `json:"id"`
	ChannelID string `json:"channel_id"`
	Sender    string `json:"sender"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type TurnConfig struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Client struct {
	Conn     *websocket.Conn
	Username string
	Status   string
	// writeMu serializes all writes to Conn. gorilla/websocket is explicit
	// that concurrent writes are undefined behavior, and this server fans
	// messages out to other clients' conns from unrelated read-loops.
	writeMu sync.Mutex
	// msgLimiter throttles inbound WS messages per-connection. The HTTP
	// rateLimit middleware only covers the upgrade handshake — once WS is
	// established, a single authed client could otherwise flood the read
	// loop unbounded.
	msgLimiter *rate.Limiter
}

// Send locks the per-connection write mutex and emits a JSON message.
// Every outbound WebSocket write goes through here — do not call
// WriteJSON on the underlying connection directly.
func (c *Client) Send(m NexusMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	conn := c.Conn
	return conn.WriteJSON(m)
}

type NexusServer struct {
	DB      *sql.DB
	Clients map[string]*Client
	Mu      sync.RWMutex
}

var upgrader = websocket.Upgrader{
	CheckOrigin: originAllowed,
}

// ---------- Sovereign Media Configuration ----------

var (
	TurnSecret    = os.Getenv("PHAZE_TURN_SECRET")
	TurnURL       = os.Getenv("PHAZE_TURN_URL")
	TurnShortTerm = os.Getenv("PHAZE_TURN_SHORT_TERM") == "true"
)

func (s *NexusServer) generateMediaToken(username string) *TurnConfig {
	if TurnSecret == "" || TurnURL == "" {
		log.Printf("[TURN] No TURN configured (TURN_SECRET or TURN_URL not set)")
		return nil
	}

	var expiresIn time.Duration
	if TurnShortTerm {
		expiresIn = 10 * time.Minute
	} else {
		expiresIn = 24 * time.Hour
	}

	timestamp := time.Now().Add(expiresIn).Unix()
	user := fmt.Sprintf("%d:%s", timestamp, username)

	mac := hmac.New(sha1.New, []byte(TurnSecret))
	mac.Write([]byte(user))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	log.Printf("[TURN] Generated token for %s (expires in %v)", username, expiresIn)

	return &TurnConfig{
		URL:      TurnURL,
		Username: user,
		Password: password,
	}
}

func (s *NexusServer) initDB() {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			email TEXT,
			mood TEXT,
			display_name TEXT,
			password_hash TEXT NOT NULL,
			salt TEXT NOT NULL,
			is_verified INTEGER DEFAULT 0,
			verification_code TEXT,
			phone_number TEXT,
			phone_verified INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS friends (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_a TEXT NOT NULL,
			user_b TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_a, user_b)
		)`,
		`CREATE TABLE IF NOT EXISTS offline_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sender TEXT NOT NULL,
			recipient TEXT NOT NULL,
			body TEXT NOT NULL,
			msg_type TEXT NOT NULL DEFAULT 'msg',
			convo TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS conversations (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			created_by TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS conversation_members (
			convo_id TEXT NOT NULL,
			username TEXT NOT NULL,
			PRIMARY KEY (convo_id, username)
		)`,
		`CREATE TABLE IF NOT EXISTS blocks (
			blocker TEXT NOT NULL,
			blocked TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (blocker, blocked)
		)`,
		`CREATE TABLE IF NOT EXISTS abuse_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			reporter TEXT NOT NULL,
			subject TEXT NOT NULL,
			reason TEXT NOT NULL,
			body TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS session_tokens (
			token TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			device_info TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			revoked INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS password_resets (
			token TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			used INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS qr_login_tokens (
			token TEXT PRIMARY KEY,
			username TEXT DEFAULT '',
			session_token TEXT DEFAULT '',
			device_info TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			approved INTEGER DEFAULT 0
		)`,
		// --- Servers ("Spaces") + Channels ---
		// Persistent communities. Channel-level chat history is server-side
		// plaintext (unlike 1:1 / convo E2EE) so search, moderation, and join
		// history work. Private servers + E2EE-channels are a future feature.
		`CREATE TABLE IF NOT EXISTS servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT DEFAULT '',
			icon TEXT DEFAULT '',
			owner TEXT NOT NULL,
			visibility TEXT NOT NULL DEFAULT 'private',
			invite_code TEXT UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS channels (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL,
			name TEXT NOT NULL,
			topic TEXT DEFAULT '',
			kind TEXT NOT NULL DEFAULT 'text',
			position INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS server_members (
			server_id TEXT NOT NULL,
			username TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'member',
			joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (server_id, username)
		)`,
		`CREATE TABLE IF NOT EXISTS channel_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel_id TEXT NOT NULL,
			sender TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_channels_server ON channels(server_id, position)`,
		`CREATE INDEX IF NOT EXISTS idx_channel_messages ON channel_messages(channel_id, id)`,
		`CREATE INDEX IF NOT EXISTS idx_server_members_user ON server_members(username)`,
		`CREATE INDEX IF NOT EXISTS idx_servers_invite ON servers(invite_code)`,
	}
	for _, q := range tables {
		if _, err := s.DB.Exec(q); err != nil {
			log.Fatalf("DB init error: %v", err)
		}
	}
	// Idempotent column migrations for existing deployments.
	migrations := []string{
		`ALTER TABLE users ADD COLUMN totp_secret TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN totp_enabled INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN is_admin INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN is_banned INTEGER DEFAULT 0`,
		`ALTER TABLE users ADD COLUMN ban_reason TEXT DEFAULT ''`,
		`ALTER TABLE users ADD COLUMN banned_at DATETIME`,
		`ALTER TABLE abuse_reports ADD COLUMN status TEXT DEFAULT 'pending'`,
		`ALTER TABLE abuse_reports ADD COLUMN resolved_by TEXT DEFAULT ''`,
		`ALTER TABLE abuse_reports ADD COLUMN resolved_at DATETIME`,
		`CREATE INDEX IF NOT EXISTS idx_abuse_reports_status ON abuse_reports(status)`,
		`CREATE INDEX IF NOT EXISTS idx_users_banned ON users(is_banned)`,
	}
	for _, q := range migrations {
		if _, err := s.DB.Exec(q); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			log.Printf("DB migration skipped (%v)", err)
		}
	}

	// Promote any usernames listed in PHAZE_ADMIN_USERS (comma-separated) to
	// admin on every boot. Lets you bootstrap the first admin without a DB
	// shell, and keeps admin status in sync if you rotate the env var.
	if raw := strings.TrimSpace(os.Getenv("PHAZE_ADMIN_USERS")); raw != "" {
		for _, u := range strings.Split(raw, ",") {
			u = strings.TrimSpace(u)
			if u == "" {
				continue
			}
			if _, err := s.DB.Exec(`UPDATE users SET is_admin = 1 WHERE username = ?`, u); err != nil {
				log.Printf("[admin] promote %s: %v", u, err)
			}
		}
	}
}

// ---------- Account Management (bcrypt) ----------

func (s *NexusServer) registerUser(username, email, mood, password string) (string, error) {
	if !validUsername(username) {
		return "", errBadUsername
	}
	if !validEmail(email) {
		return "", errBadEmail
	}
	if len(password) < 8 {
		return "", errShortPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	code, err := randDigits(6)
	if err != nil {
		return "", err
	}

	_, err = s.DB.Exec("INSERT INTO users (username, email, mood, password_hash, salt, verification_code) VALUES (?, ?, ?, ?, '', ?)",
		username, email, mood, string(hash), code)
	return code, err
}

// ---------- Auth MVP helpers (OTP, email, username, session, TOTP, reset) ----------

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

func validUsername(u string) bool { return usernameRegex.MatchString(u) }

func validEmail(e string) bool {
	if e == "" {
		return false
	}
	_, err := mail.ParseAddress(e)
	return err == nil
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randDigits(n int) (string, error) {
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		out[i] = byte('0' + v.Int64())
	}
	return string(out), nil
}

func (s *NexusServer) issueSessionToken(username, device string) (string, error) {
	tok, err := randHex(32)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(30 * 24 * time.Hour)
	_, err = s.DB.Exec(
		"INSERT INTO session_tokens (token, username, device_info, expires_at) VALUES (?, ?, ?, ?)",
		tok, username, device, expires,
	)
	return tok, err
}

func (s *NexusServer) sessionUsername(token string) string {
	if token == "" {
		return ""
	}
	var u string
	var expires time.Time
	var revoked int
	err := s.DB.QueryRow(
		"SELECT username, expires_at, revoked FROM session_tokens WHERE token = ?",
		token,
	).Scan(&u, &expires, &revoked)
	if err != nil || revoked != 0 || time.Now().After(expires) {
		return ""
	}
	return u
}

func (s *NexusServer) revokeSession(token string) {
	s.DB.Exec("UPDATE session_tokens SET revoked = 1 WHERE token = ?", token)
}

// deleteAccount performs a GDPR Article 17 ("right to erasure") cascade for a
// single user. Runs in a single transaction so partial failure leaves the
// account intact. Reports MADE BY the user are removed; reports ABOUT the
// user are retained (the subject column is just text, so the username string
// remains in the safety log — this is the legitimate-interests carve-out).
func (s *NexusServer) deleteAccount(username string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	statements := []struct {
		sql  string
		args []any
	}{
		{`DELETE FROM friends WHERE user_a = ? OR user_b = ?`, []any{username, username}},
		{`DELETE FROM offline_messages WHERE sender = ? OR recipient = ?`, []any{username, username}},
		{`DELETE FROM conversation_members WHERE username = ?`, []any{username}},
		{`DELETE FROM conversations WHERE created_by = ?`, []any{username}},
		{`DELETE FROM blocks WHERE blocker = ? OR blocked = ?`, []any{username, username}},
		{`DELETE FROM abuse_reports WHERE reporter = ?`, []any{username}},
		{`DELETE FROM session_tokens WHERE username = ?`, []any{username}},
		{`DELETE FROM password_resets WHERE username = ?`, []any{username}},
		{`DELETE FROM qr_login_tokens WHERE username = ?`, []any{username}},
		// Drop the user last — every other row referencing the username is
		// already gone, so a foreign-key constraint (if added later) would still
		// pass.
		{`DELETE FROM users WHERE username = ?`, []any{username}},
	}
	for _, q := range statements {
		if _, err := tx.Exec(q.sql, q.args...); err != nil {
			return fmt.Errorf("deleteAccount %q: %w", q.sql, err)
		}
	}
	return tx.Commit()
}

func (s *NexusServer) totpStatus(username string) (secret string, enabled bool) {
	var e int
	s.DB.QueryRow("SELECT totp_secret, totp_enabled FROM users WHERE username = ?", username).Scan(&secret, &e)
	return secret, e == 1
}

func (s *NexusServer) generateTOTPURI(username string) (uri, secret string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Phaze",
		AccountName: username,
	})
	if err != nil {
		return "", "", err
	}
	return key.URL(), key.Secret(), nil
}

func (s *NexusServer) enableTOTP(username, secret, code string) bool {
	if !totp.Validate(code, secret) {
		return false
	}
	_, err := s.DB.Exec("UPDATE users SET totp_secret = ?, totp_enabled = 1 WHERE username = ?", secret, username)
	return err == nil
}

func (s *NexusServer) disableTOTP(username string) {
	s.DB.Exec("UPDATE users SET totp_secret = '', totp_enabled = 0 WHERE username = ?", username)
}

func (s *NexusServer) verifyTOTP(username, code string) bool {
	secret, enabled := s.totpStatus(username)
	if !enabled || secret == "" {
		return true
	}
	return totp.Validate(code, secret)
}

func (s *NexusServer) createPasswordReset(email string) (string, string, error) {
	var username string
	err := s.DB.QueryRow("SELECT username FROM users WHERE email = ?", email).Scan(&username)
	if err != nil {
		return "", "", err
	}
	tok, err := randHex(24)
	if err != nil {
		return "", "", err
	}
	expires := time.Now().Add(1 * time.Hour)
	_, err = s.DB.Exec(
		"INSERT INTO password_resets (token, username, expires_at) VALUES (?, ?, ?)",
		tok, username, expires,
	)
	return tok, username, err
}

func (s *NexusServer) consumePasswordReset(token, newPassword string) error {
	if len(newPassword) < 8 {
		return errShortPassword
	}
	var username string
	var expires time.Time
	var used int
	err := s.DB.QueryRow(
		"SELECT username, expires_at, used FROM password_resets WHERE token = ?",
		token,
	).Scan(&username, &expires, &used)
	if err != nil {
		return err
	}
	if used != 0 || time.Now().After(expires) {
		return errResetInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE users SET password_hash = ? WHERE username = ?", string(hash), username); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec("UPDATE password_resets SET used = 1 WHERE token = ?", token); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *NexusServer) createQRLogin() (string, error) {
	tok, err := randHex(16)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(5 * time.Minute)
	_, err = s.DB.Exec(
		"INSERT INTO qr_login_tokens (token, expires_at) VALUES (?, ?)",
		tok, expires,
	)
	return tok, err
}

func (s *NexusServer) approveQRLogin(token, username, device string) error {
	sess, err := s.issueSessionToken(username, device)
	if err != nil {
		return err
	}
	res, err := s.DB.Exec(
		"UPDATE qr_login_tokens SET username = ?, session_token = ?, device_info = ?, approved = 1 WHERE token = ? AND approved = 0 AND expires_at > CURRENT_TIMESTAMP",
		username, sess, device, token,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errQRInvalid
	}
	return nil
}

func (s *NexusServer) checkQRLogin(token string) (string, string, bool) {
	var username, sess string
	var approved int
	var expires time.Time
	err := s.DB.QueryRow(
		"SELECT username, session_token, approved, expires_at FROM qr_login_tokens WHERE token = ?",
		token,
	).Scan(&username, &sess, &approved, &expires)
	if err != nil || time.Now().After(expires) {
		return "", "", false
	}
	return username, sess, approved == 1
}

func (s *NexusServer) verifyUser(username, code string) bool {
	var dbCode string
	err := s.DB.QueryRow("SELECT verification_code FROM users WHERE username = ?", username).Scan(&dbCode)
	if err != nil || dbCode != code {
		return false
	}
	_, err = s.DB.Exec("UPDATE users SET is_verified = 1, verification_code = NULL WHERE username = ?", username)
	return err == nil
}

func (s *NexusServer) sendEmail(to, subject, body string) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")

	if host == "" || user == "" || pass == "" {
		log.Printf("[MAIL-SIM] To: %s | Subject: %s | Body: %s", to, subject, body)
		return nil
	}

	auth := smtp.PlainAuth("", user, pass, host)
	msg := []byte("To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\r\n" +
		"\r\n" +
		body + "\r\n")

	addr := fmt.Sprintf("%s:%s", host, port)
	if port == "" {
		addr = host + ":587"
	}

	return smtp.SendMail(addr, auth, user, []string{to}, msg)
}

// sendEmailLogged wraps sendEmail for 'go'-launched calls so SMTP failures
// land in logs instead of vanishing silently. Use this any time email
// delivery is not on the caller's synchronous response path.
func (s *NexusServer) sendEmailLogged(to, subject, body string) {
	if err := s.sendEmail(to, subject, body); err != nil {
		log.Printf("[mail] send to %s subject %q failed: %v", to, subject, err)
	}
}

func (s *NexusServer) authenticateUser(username, password string) bool {
	var hash string
	var isVerified bool
	err := s.DB.QueryRow("SELECT password_hash, is_verified FROM users WHERE username = ?", username).Scan(&hash, &isVerified)
	if err != nil {
		return false
	}
	if !isVerified {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// userBanInfo returns (banned, reason). Reason is empty for non-banned users.
func (s *NexusServer) userBanInfo(username string) (bool, string) {
	var banned int
	var reason string
	err := s.DB.QueryRow(`SELECT is_banned, COALESCE(ban_reason, '') FROM users WHERE username = ?`, username).Scan(&banned, &reason)
	if err != nil {
		return false, ""
	}
	return banned == 1, reason
}

// userIsAdmin reports whether the user has the admin flag set.
func (s *NexusServer) userIsAdmin(username string) bool {
	var n int
	err := s.DB.QueryRow(`SELECT is_admin FROM users WHERE username = ?`, username).Scan(&n)
	if err != nil {
		return false
	}
	return n == 1
}

// ---------- Servers + Channels (Discord-style "Spaces") ----------

var validServerName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} _\-\.']{1,63}$`)
var validChannelName = regexp.MustCompile(`^[a-z0-9][a-z0-9\-_]{1,31}$`)

// userIsServerMember reports whether the user is in the given server.
func (s *NexusServer) userIsServerMember(server, user string) bool {
	var n int
	s.DB.QueryRow(`SELECT 1 FROM server_members WHERE server_id = ? AND username = ?`, server, user).Scan(&n)
	return n == 1
}

// userServerRole returns the user's role in the server, or "" if not a member.
func (s *NexusServer) userServerRole(server, user string) string {
	var r string
	if err := s.DB.QueryRow(`SELECT role FROM server_members WHERE server_id = ? AND username = ?`, server, user).Scan(&r); err != nil {
		return ""
	}
	return r
}

// listUserServers returns the server-list pane data for a user.
func (s *NexusServer) listUserServers(username string) ([]ServerSummary, error) {
	rows, err := s.DB.Query(
		`SELECT s.id, s.name, COALESCE(s.description,''), COALESCE(s.icon,''),
		         s.owner, s.visibility, m.role, COALESCE(s.invite_code,'')
		   FROM servers s JOIN server_members m ON s.id = m.server_id
		  WHERE m.username = ?
		  ORDER BY m.joined_at ASC`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerSummary{}
	for rows.Next() {
		var ss ServerSummary
		if err := rows.Scan(&ss.ID, &ss.Name, &ss.Description, &ss.Icon, &ss.Owner, &ss.Visibility, &ss.Role, &ss.InviteCode); err != nil {
			continue
		}
		out = append(out, ss)
	}
	return out, nil
}

// listServerChannels returns every channel in a server.
func (s *NexusServer) listServerChannels(serverID string) ([]ChannelInfo, error) {
	rows, err := s.DB.Query(
		`SELECT id, server_id, name, COALESCE(topic,''), kind, position
		   FROM channels WHERE server_id = ?
		  ORDER BY position ASC, name ASC`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelInfo{}
	for rows.Next() {
		var c ChannelInfo
		if err := rows.Scan(&c.ID, &c.ServerID, &c.Name, &c.Topic, &c.Kind, &c.Position); err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// channelHistory returns the most recent `limit` messages in a channel,
// optionally before a cursor id. Returned in chronological order (oldest first).
func (s *NexusServer) channelHistory(channelID string, beforeID int64, limit int) ([]ChannelMsg, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows *sql.Rows
	var err error
	if beforeID > 0 {
		rows, err = s.DB.Query(
			`SELECT id, channel_id, sender, body, CAST(created_at AS TEXT)
			   FROM channel_messages
			  WHERE channel_id = ? AND id < ?
			  ORDER BY id DESC LIMIT ?`, channelID, beforeID, limit)
	} else {
		rows, err = s.DB.Query(
			`SELECT id, channel_id, sender, body, CAST(created_at AS TEXT)
			   FROM channel_messages
			  WHERE channel_id = ?
			  ORDER BY id DESC LIMIT ?`, channelID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChannelMsg
	for rows.Next() {
		var m ChannelMsg
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.Sender, &m.Body, &m.CreatedAt); err != nil {
			continue
		}
		out = append(out, m)
	}
	// Reverse to chronological.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// broadcastChannelMsg fan-outs a new channel message to all currently
// connected members of that server. Plaintext on the wire; clients filter
// by the channel they have open.
func (s *NexusServer) broadcastChannelMsg(serverID string, payload NexusMessage) {
	rows, err := s.DB.Query(`SELECT username FROM server_members WHERE server_id = ?`, serverID)
	if err != nil {
		return
	}
	defer rows.Close()
	var recipients []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err == nil {
			recipients = append(recipients, u)
		}
	}
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	for _, u := range recipients {
		if c, ok := s.Clients[u]; ok {
			c.Send(payload)
		}
	}
}

var errShortPassword = &strErr{"password must be at least 8 characters"}
var errBadUsername = &strErr{"invalid username (3-32 chars, a-z A-Z 0-9 . _ -)"}
var errBadEmail = &strErr{"invalid email address"}
var errResetInvalid = &strErr{"password reset token invalid or expired"}
var errQRInvalid = &strErr{"qr login token invalid or expired"}

type strErr struct{ msg string }

func (e *strErr) Error() string { return e.msg }

// ---------- Friend Management ----------

func (s *NexusServer) getFriends(username string) []string {
	rows, err := s.DB.Query(`
		SELECT CASE WHEN user_a = ? THEN user_b ELSE user_a END as friend
		FROM friends
		WHERE (user_a = ? OR user_b = ?) AND status = 'accepted'`,
		username, username, username)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var friends []string
	for rows.Next() {
		var f string
		rows.Scan(&f)
		friends = append(friends, f)
	}
	return friends
}

func (s *NexusServer) sendFriendRequest(from, to string) error {
	// Check if already exists in either direction
	var count int
	s.DB.QueryRow("SELECT COUNT(*) FROM friends WHERE (user_a=? AND user_b=?) OR (user_a=? AND user_b=?)",
		from, to, to, from).Scan(&count)
	if count > 0 {
		return nil // Already exists
	}
	_, err := s.DB.Exec("INSERT INTO friends (user_a, user_b, status) VALUES (?, ?, 'pending')", from, to)
	return err
}

func (s *NexusServer) acceptFriendRequest(from, to string) error {
	_, err := s.DB.Exec("UPDATE friends SET status = 'accepted' WHERE user_a = ? AND user_b = ? AND status = 'pending'",
		from, to)
	return err
}

func (s *NexusServer) rejectFriendRequest(from, to string) error {
	_, err := s.DB.Exec("DELETE FROM friends WHERE user_a = ? AND user_b = ? AND status = 'pending'",
		from, to)
	return err
}

// areFriends reports whether two usernames have an accepted friendship
// record in either direction.
func (s *NexusServer) areFriends(a, b string) bool {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM friends
		WHERE status = 'accepted' AND ((user_a = ? AND user_b = ?) OR (user_a = ? AND user_b = ?))`,
		a, b, b, a).Scan(&n)
	return n > 0
}

func (s *NexusServer) removeFriend(a, b string) error {
	_, err := s.DB.Exec(`DELETE FROM friends
		WHERE (user_a = ? AND user_b = ?) OR (user_a = ? AND user_b = ?)`,
		a, b, b, a)
	return err
}

// ---------- Group Chat ----------

func (s *NexusServer) createConversation(id, name, creator string, members []string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO conversations (id, name, created_by) VALUES (?, ?, ?)", id, name, creator); err != nil {
		tx.Rollback()
		return err
	}
	seen := map[string]bool{creator: false}
	all := append([]string{creator}, members...)
	for _, m := range all {
		if seen[m] {
			continue
		}
		seen[m] = true
		if _, err := tx.Exec("INSERT OR IGNORE INTO conversation_members (convo_id, username) VALUES (?, ?)", id, m); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *NexusServer) conversationMembers(id string) []string {
	rows, err := s.DB.Query("SELECT username FROM conversation_members WHERE convo_id = ?", id)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		rows.Scan(&u)
		out = append(out, u)
	}
	return out
}

func (s *NexusServer) userConversations(username string) []NexusMessage {
	rows, err := s.DB.Query(`SELECT c.id, c.name
		FROM conversations c
		JOIN conversation_members m ON m.convo_id = c.id
		WHERE m.username = ?`, username)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []NexusMessage
	for rows.Next() {
		var m NexusMessage
		rows.Scan(&m.ConvoID, &m.ConvoName)
		m.Members = s.conversationMembers(m.ConvoID)
		out = append(out, m)
	}
	return out
}

func (s *NexusServer) leaveConversation(convoID, username string) error {
	_, err := s.DB.Exec("DELETE FROM conversation_members WHERE convo_id = ? AND username = ?", convoID, username)
	return err
}

func (s *NexusServer) getPendingRequests(username string) []string {
	rows, err := s.DB.Query("SELECT user_a FROM friends WHERE user_b = ? AND status = 'pending'", username)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var pending []string
	for rows.Next() {
		var f string
		rows.Scan(&f)
		pending = append(pending, f)
	}
	return pending
}

// ---------- Offline Messages ----------

func (s *NexusServer) storeOfflineMessage(sender, recipient, body, msgType string) {
	if _, err := s.DB.Exec("INSERT INTO offline_messages (sender, recipient, body, msg_type) VALUES (?, ?, ?, ?)",
		sender, recipient, body, msgType); err != nil {
		log.Printf("[offline] store %s->%s (%s) failed: %v", sender, recipient, msgType, err)
	}
}

func (s *NexusServer) deliverOfflineMessages(username string) {
	s.Mu.RLock()
	client, online := s.Clients[username]
	s.Mu.RUnlock()
	if !online {
		return
	}

	rows, err := s.DB.Query("SELECT id, sender, body, msg_type, created_at FROM offline_messages WHERE recipient = ? ORDER BY created_at ASC", username)
	if err != nil {
		return
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		var sender, body, msgType, createdAt string
		rows.Scan(&id, &sender, &body, &msgType, &createdAt)
		client.Send(NexusMessage{
			Type:   msgType,
			Sender: sender,
			Body:   body,
			Status: "offline:" + createdAt,
		})
		ids = append(ids, id)
	}

	// Delete delivered messages
	for _, id := range ids {
		s.DB.Exec("DELETE FROM offline_messages WHERE id = ?", id)
	}
	if len(ids) > 0 {
		log.Printf("Delivered %d offline messages to %s", len(ids), username)
	}
}

// ---------- Presence ----------

func (s *NexusServer) broadcastPresence(username, status string) {
	friends := s.getFriends(username)
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	for _, friend := range friends {
		if client, ok := s.Clients[friend]; ok {
			client.Send(NexusMessage{
				Type:   "presence",
				Sender: username,
				Status: status,
			})
		}
	}
}

// ---------- Trust & Safety: blocks + abuse reports ----------

// isBlocked reports whether `blocker` has blocked `blocked`. Either direction
// being blocked should suppress message delivery (checked at the call site).
func (s *NexusServer) isBlocked(blocker, blocked string) bool {
	var n int
	err := s.DB.QueryRow(
		"SELECT 1 FROM blocks WHERE blocker = ? AND blocked = ? LIMIT 1",
		blocker, blocked).Scan(&n)
	return err == nil
}

func (s *NexusServer) blockUser(blocker, blocked string) error {
	if blocker == "" || blocked == "" || blocker == blocked {
		return fmt.Errorf("invalid block")
	}
	_, err := s.DB.Exec(
		"INSERT OR IGNORE INTO blocks (blocker, blocked) VALUES (?, ?)",
		blocker, blocked)
	return err
}

func (s *NexusServer) unblockUser(blocker, blocked string) error {
	_, err := s.DB.Exec(
		"DELETE FROM blocks WHERE blocker = ? AND blocked = ?",
		blocker, blocked)
	return err
}

func (s *NexusServer) listBlocks(blocker string) []string {
	rows, err := s.DB.Query(
		"SELECT blocked FROM blocks WHERE blocker = ? ORDER BY created_at DESC",
		blocker)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if rows.Scan(&u) == nil {
			out = append(out, u)
		}
	}
	return out
}

func (s *NexusServer) recordAbuseReport(reporter, subject, reason, body string) error {
	if reporter == "" || subject == "" || reason == "" {
		return fmt.Errorf("missing fields")
	}
	_, err := s.DB.Exec(
		"INSERT INTO abuse_reports (reporter, subject, reason, body) VALUES (?, ?, ?, ?)",
		reporter, subject, reason, body)
	return err
}

// ---------- Search ----------

func (s *NexusServer) searchUsers(query, excludeUser string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}

	// Search registered users by username substring
	rows, err := s.DB.Query("SELECT username FROM users WHERE LOWER(username) LIKE ? AND username != ? LIMIT 20",
		"%"+query+"%", excludeUser)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		results = append(results, name)
	}
	return results
}

// ---------- WebSocket Handler ----------

func (s *NexusServer) handleConnections(w http.ResponseWriter, r *http.Request) {
	log.Printf("Incoming connection: %s %s", r.Method, r.URL.String())
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		metrics.wsConnectionsFail.Add(1)
		return
	}
	metrics.wsConnections.Add(1)
	defer ws.Close()

	// client owns the write mutex for this connection. Pre-auth writes use
	// it even before the client is registered in s.Clients.
	// msgLimiter: 20 msg/s sustained, burst 40. Accommodates typing
	// indicators + rapid sends without permitting a tight spam loop.
	client := &Client{
		Conn:       ws,
		msgLimiter: rate.NewLimiter(rate.Limit(20), 40),
	}

	var username string

	for {
		var msg NexusMessage
		err := ws.ReadJSON(&msg)
		if err != nil {
			log.Printf("Read error: %v", err)
			if username != "" {
				// Compare-and-swap delete: only remove the map entry if it
				// still points at *this* connection. A concurrent login from
				// the same user kicks the previous session via Conn.Close(),
				// which wakes *this* read-loop with an error — without the
				// guard below we'd delete the freshly-installed new session
				// and mark the user offline even though they're online on
				// another device.
				s.Mu.Lock()
				current, ok := s.Clients[username]
				weWereReplaced := ok && current != client
				if ok && current == client {
					delete(s.Clients, username)
				}
				s.Mu.Unlock()
				if !weWereReplaced {
					s.broadcastPresence(username, "Offline")
					log.Printf("User %s disconnected", username)
				} else {
					log.Printf("User %s old session closed (replaced by newer login)", username)
				}
			}
			return
		}

		// Per-connection rate limit. Drop silently on overflow: an authed
		// spammer shouldn't learn they've tripped the limiter.
		if !client.msgLimiter.Allow() {
			log.Printf("[ratelimit] dropping %q from %s", msg.Type, username)
			continue
		}

		metrics.wsMessagesIn.Add(1)

		switch msg.Type {
		case "pstn_call":
			metrics.pstnAttempts.Add(1)
			if !pstnBridgeEnabled() {
				metrics.pstnRejected.Add(1)
				client.Send(NexusMessage{
					Type:  "pstn_status",
					Error: "PSTN bridge is disabled on this relay. Use in-app voice/video (WebRTC) with Phaze contacts — no phone network or Twilio required.",
				})
				continue
			}
			number := msg.Body
			// SECURITY CHECK: Verify this number belongs to this sender
			var verified int
			err := s.DB.QueryRow("SELECT phone_verified FROM users WHERE username = ? AND phone_number = ?", username, number).Scan(&verified)
			if err != nil || verified == 0 {
				client.Send(NexusMessage{Type: "pstn_status", Error: "Caller identity not verified. Please link your phone in Settings."})
				continue
			}

			log.Printf("[PSTN-SECURE] User %s initiating call to %s", msg.Sender, number)
			err = s.initiateTwilioCall(number)
			if err != nil {
				client.Send(NexusMessage{Type: "pstn_status", Error: "Telephony error: " + err.Error()})
			} else {
				client.Send(NexusMessage{Type: "pstn_status", Status: "Connecting via Sovereign Bridge..."})
			}

		case "register":
			code, err := s.registerUser(msg.Sender, msg.Email, msg.Mood, msg.Body)
			if err != nil {
				client.Send(NexusMessage{Type: "register_result", Error: "Username already taken or database error"})
			} else {
				log.Printf("New user registered: %s (%s) - Code: %s", msg.Sender, msg.Email, code)
				go s.sendEmailLogged(msg.Email, "Activate your Phaze Identity",
					"<h1>Welcome to Phaze</h1><p>Your activation code is: <b>"+code+"</b></p><p>Enter this in the app to start using the mesh.</p>")
				client.Send(NexusMessage{Type: "register_result", Status: "pending_verification"})
			}

		case "verify_email":
			if s.verifyUser(msg.Sender, msg.Body) {
				client.Send(NexusMessage{Type: "verify_result", Status: "ok"})
			} else {
				client.Send(NexusMessage{Type: "verify_result", Error: "Invalid verification code"})
			}

		case "status_update":
			s.Mu.Lock()
			if client, ok := s.Clients[username]; ok {
				client.Status = msg.Body
				log.Printf("User %s changed status to %s", username, msg.Body)
			}
			s.Mu.Unlock()
			s.broadcastPresence(username, msg.Body)

		case "request_phone_link":
			number := msg.Body
			code := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
			_, err := s.DB.Exec("UPDATE users SET verification_code = ?, phone_number = ? WHERE username = ?", code, number, username)
			if err != nil {
				client.Send(NexusMessage{Type: "phone_link_result", Error: "Update failed"})
			} else {
				log.Printf("[SMS] Sending verification to %s: %s", number, code)
				go s.sendSMS(number, "Your Phaze verification code is: "+code)
				client.Send(NexusMessage{Type: "phone_link_result", Status: "code_sent"})
			}

		case "verify_phone_link":
			var dbCode string
			err := s.DB.QueryRow("SELECT verification_code FROM users WHERE username = ?", username).Scan(&dbCode)
			if err == nil && dbCode == msg.Body {
				s.DB.Exec("UPDATE users SET phone_verified = 1, verification_code = NULL WHERE username = ?", username)
				client.Send(NexusMessage{Type: "phone_link_result", Status: "verified"})
			} else {
				s.DB.Exec("UPDATE users SET verification_code = NULL WHERE username = ?", username)
				client.Send(NexusMessage{Type: "phone_link_result", Error: "Invalid code. Security lockout: please request a new code."})
			}

		case "update_profile":
			// Update mood and display name
			_, err := s.DB.Exec("UPDATE users SET mood = ?, display_name = ? WHERE username = ?",
				msg.Mood, msg.DisplayName, msg.Sender)
			if err != nil {
				client.Send(NexusMessage{Type: "update_result", Error: "Update failed"})
			} else {
				log.Printf("Profile updated for %s: %s | %s", msg.Sender, msg.DisplayName, msg.Mood)
				client.Send(NexusMessage{Type: "update_result", Status: "ok"})
				// Broadcast this change to all online friends
				s.broadcastProfileUpdate(msg.Sender, msg.DisplayName, msg.Mood)
			}

		case "auth":
			if msg.Body == "" {
				metrics.authFailure.Add(1)
				client.Send(NexusMessage{Type: "auth_result", Error: "Password required"})
				continue
			}
			if !s.authenticateUser(msg.Sender, msg.Body) {
				metrics.authFailure.Add(1)
				client.Send(NexusMessage{Type: "auth_result", Error: "Invalid username or password"})
				continue
			}
			if banned, reason := s.userBanInfo(msg.Sender); banned {
				metrics.authFailure.Add(1)
				body := "Account suspended"
				if reason != "" {
					body += ": " + reason
				}
				client.Send(NexusMessage{Type: "auth_result", Error: body, Status: "banned"})
				continue
			}
			if !s.verifyTOTP(msg.Sender, msg.TOTPCode) {
				metrics.authFailure.Add(1)
				client.Send(NexusMessage{Type: "auth_result", Error: "2FA code required or invalid", Status: "totp_required"})
				continue
			}
			metrics.authSuccess.Add(1)
			username = msg.Sender
			sessTok, _ := s.issueSessionToken(username, msg.DeviceInfo)
			s.Mu.Lock()
			if existing, ok := s.Clients[username]; ok {
				existing.Send(NexusMessage{Type: "kicked", Body: "Logged in from another location"})
				existing.Conn.Close()
			}
			client.Username = username
			client.Status = "Online"
			s.Clients[username] = client
			s.Mu.Unlock()
			log.Printf("User %s authenticated", username)

			client.Send(NexusMessage{
				Type:       "auth_result",
				Status:     "ok",
				Sender:     username,
				QRToken:    sessTok,
				TurnConfig: s.generateMediaToken(username),
			})

			// Broadcast online presence to friends
			s.broadcastPresence(username, "Online")

			// Deliver any offline messages
			s.deliverOfflineMessages(username)

			// Send pending friend requests
			pending := s.getPendingRequests(username)
			if len(pending) > 0 {
				client.Send(NexusMessage{Type: "pending_requests", Results: pending})
			}

			// Send conversations this user belongs to
			for _, cm := range s.userConversations(username) {
				cm.Type = "convo_info"
				client.Send(cm)
			}

			// Send friends list with online status
			friends := s.getFriends(username)
			for _, f := range friends {
				status := "Offline"
				s.Mu.RLock()
				if c, ok := s.Clients[f]; ok {
					status = c.Status
				}
				s.Mu.RUnlock()
				client.Send(NexusMessage{Type: "friend_status", Sender: f, Status: status})
			}

		case "session_auth":
			u := s.sessionUsername(msg.QRToken)
			if u == "" {
				client.Send(NexusMessage{Type: "auth_result", Error: "Session expired, please log in"})
				continue
			}
			if banned, reason := s.userBanInfo(u); banned {
				body := "Account suspended"
				if reason != "" {
					body += ": " + reason
				}
				s.revokeSession(msg.QRToken)
				client.Send(NexusMessage{Type: "auth_result", Error: body, Status: "banned"})
				continue
			}
			username = u
			s.Mu.Lock()
			if existing, ok := s.Clients[username]; ok {
				existing.Send(NexusMessage{Type: "kicked", Body: "Logged in from another location"})
				existing.Conn.Close()
			}
			client.Username = username
			client.Status = "Online"
			s.Clients[username] = client
			s.Mu.Unlock()
			log.Printf("User %s resumed via session token", username)
			client.Send(NexusMessage{
				Type:       "auth_result",
				Status:     "ok",
				Sender:     username,
				QRToken:    msg.QRToken,
				TurnConfig: s.generateMediaToken(username),
			})
			s.broadcastPresence(username, "Online")
			s.deliverOfflineMessages(username)

		case "revoke_session":
			if username == "" || msg.QRToken == "" {
				continue
			}
			s.revokeSession(msg.QRToken)
			client.Send(NexusMessage{Type: "session_revoked", Status: "ok"})

		case "delete_account":
			// GDPR Article 17 — right to erasure. Requires the user to be
			// authenticated AND to confirm their password in msg.Body so an
			// attacker with a stolen session token can't nuke the account.
			if username == "" {
				client.Send(NexusMessage{Type: "delete_account_result", Error: "Not authenticated"})
				continue
			}
			if msg.Body == "" || !s.authenticateUser(username, msg.Body) {
				client.Send(NexusMessage{Type: "delete_account_result", Error: "Password confirmation required"})
				continue
			}
			if err := s.deleteAccount(username); err != nil {
				log.Printf("[delete_account] %s: %v", username, err)
				client.Send(NexusMessage{Type: "delete_account_result", Error: "Internal error — try again"})
				continue
			}
			log.Printf("[delete_account] erased account %s", username)
			// Notify friends so their rosters update.
			s.broadcastPresence(username, "Offline")
			client.Send(NexusMessage{Type: "delete_account_result", Status: "ok"})
			// Drop the connection and the in-memory client entry.
			s.Mu.Lock()
			if cur, ok := s.Clients[username]; ok && cur == client {
				delete(s.Clients, username)
			}
			s.Mu.Unlock()
			ws.Close()
			return

		case "resend_verification":
			var email string
			err := s.DB.QueryRow("SELECT email FROM users WHERE username = ?", msg.Sender).Scan(&email)
			if err != nil || email == "" {
				client.Send(NexusMessage{Type: "register_result", Error: "User not found"})
				continue
			}
			code, err := randDigits(6)
			if err != nil {
				client.Send(NexusMessage{Type: "register_result", Error: "Internal error"})
				continue
			}
			if _, err := s.DB.Exec("UPDATE users SET verification_code = ? WHERE username = ?", code, msg.Sender); err != nil {
				client.Send(NexusMessage{Type: "register_result", Error: "Database error"})
				continue
			}
			go s.sendEmailLogged(email, "Your Phaze activation code",
				"<h1>New code</h1><p>Your activation code is: <b>"+code+"</b></p>")
			client.Send(NexusMessage{Type: "register_result", Status: "code_resent"})

		case "enable_totp":
			if username == "" {
				client.Send(NexusMessage{Type: "totp_result", Error: "Not authenticated"})
				continue
			}
			uri, secret, err := s.generateTOTPURI(username)
			if err != nil {
				client.Send(NexusMessage{Type: "totp_result", Error: "Could not generate secret"})
				continue
			}
			// Stash secret pending verification; set enabled=0 so auth still allows login until confirmed.
			s.DB.Exec("UPDATE users SET totp_secret = ?, totp_enabled = 0 WHERE username = ?", secret, username)
			client.Send(NexusMessage{Type: "totp_result", Status: "pending_confirm", TOTPURI: uri})

		case "confirm_totp":
			if username == "" {
				client.Send(NexusMessage{Type: "totp_result", Error: "Not authenticated"})
				continue
			}
			secret, _ := s.totpStatus(username)
			if secret == "" {
				client.Send(NexusMessage{Type: "totp_result", Error: "No pending TOTP enrollment"})
				continue
			}
			if !s.enableTOTP(username, secret, msg.TOTPCode) {
				client.Send(NexusMessage{Type: "totp_result", Error: "Invalid code"})
				continue
			}
			client.Send(NexusMessage{Type: "totp_result", Status: "enabled"})

		case "disable_totp":
			if username == "" {
				continue
			}
			if !s.authenticateUser(username, msg.Body) {
				client.Send(NexusMessage{Type: "totp_result", Error: "Password required"})
				continue
			}
			s.disableTOTP(username)
			client.Send(NexusMessage{Type: "totp_result", Status: "disabled"})

		case "forgot_password":
			// Accept email in msg.Email; always ack "sent" to avoid user enumeration.
			go func(addr string) {
				tok, user, err := s.createPasswordReset(addr)
				if err != nil {
					log.Printf("[reset] no user for %s", addr)
					return
				}
				link := "https://phazechat.world/reset?token=" + tok
				s.sendEmailLogged(addr, "Reset your Phaze password",
					"<h1>Reset password</h1><p>Hello "+user+",</p><p>Click to reset (valid 1 hour): <a href=\""+link+"\">"+link+"</a></p>")
			}(msg.Email)
			client.Send(NexusMessage{Type: "forgot_password_result", Status: "sent"})

		case "reset_password":
			if err := s.consumePasswordReset(msg.QRToken, msg.Body); err != nil {
				client.Send(NexusMessage{Type: "reset_password_result", Error: err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "reset_password_result", Status: "ok"})

		case "change_password":
			// msg.Body = "oldpass:newpass" — split on first colon only
			if username == "" {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Not authenticated"})
				continue
			}
			idx := strings.Index(msg.Body, ":")
			if idx < 1 || idx == len(msg.Body)-1 {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Malformed request"})
				continue
			}
			oldPw, newPw := msg.Body[:idx], msg.Body[idx+1:]
			if !s.authenticateUser(username, oldPw) {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Current password incorrect"})
				continue
			}
			if len(newPw) < 8 {
				client.Send(NexusMessage{Type: "change_password_result", Error: "New password must be at least 8 characters"})
				continue
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
			if err != nil {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Internal error"})
				continue
			}
			if _, err := s.DB.Exec("UPDATE users SET password_hash = ? WHERE username = ?", string(hash), username); err != nil {
				client.Send(NexusMessage{Type: "change_password_result", Error: "Database error"})
				continue
			}
			log.Printf("[security] %s changed password", username)
			client.Send(NexusMessage{Type: "change_password_result", Status: "ok"})

		case "qr_login_create":
			tok, err := s.createQRLogin()
			if err != nil {
				client.Send(NexusMessage{Type: "qr_login_result", Error: "Could not create QR token"})
				continue
			}
			client.Send(NexusMessage{
				Type:    "qr_login_result",
				Status:  "pending",
				QRToken: tok,
				QRData:  "phaze://login?token=" + tok,
			})

		case "qr_login_approve":
			if username == "" {
				client.Send(NexusMessage{Type: "qr_login_result", Error: "Not authenticated"})
				continue
			}
			if err := s.approveQRLogin(msg.QRToken, username, msg.DeviceInfo); err != nil {
				client.Send(NexusMessage{Type: "qr_login_result", Error: err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "qr_login_result", Status: "approved"})

		case "qr_login_check":
			u, sess, approved := s.checkQRLogin(msg.QRToken)
			if !approved {
				client.Send(NexusMessage{Type: "qr_login_result", Status: "pending", QRToken: msg.QRToken})
				continue
			}
			// Promote this socket onto the approved session.
			username = u
			s.Mu.Lock()
			if existing, ok := s.Clients[username]; ok {
				existing.Send(NexusMessage{Type: "kicked", Body: "Logged in from another location"})
				existing.Conn.Close()
			}
			client.Username = username
			client.Status = "Online"
			s.Clients[username] = client
			s.Mu.Unlock()
			log.Printf("User %s logged in via QR", username)
			client.Send(NexusMessage{
				Type:       "auth_result",
				Status:     "ok",
				Sender:     username,
				QRToken:    sess,
				TurnConfig: s.generateMediaToken(username),
			})
			s.broadcastPresence(username, "Online")
			s.deliverOfflineMessages(username)

		case "msg":
			if username == "" {
				continue
			}
			// Authoritative sender = authenticated session, not client claim
			msg.Sender = username
			if msg.Recipient == "PhazeBot" {
				s.handleBotMessage(client, msg)
				continue
			}
			// Trust & safety: drop if either party has blocked the other.
			// Sender sees a benign delivered_offline status — no oracle leak.
			if s.isBlocked(msg.Recipient, msg.Sender) || s.isBlocked(msg.Sender, msg.Recipient) {
				log.Printf("[block] dropped %s -> %s", msg.Sender, msg.Recipient)
				client.Send(NexusMessage{
					Type: "msg_status", Body: "delivered_offline", Sender: msg.Recipient,
				})
				continue
			}
			log.Printf("Message from %s to %s", msg.Sender, msg.Recipient)
			s.Mu.RLock()
			recipientClient, online := s.Clients[msg.Recipient]
			s.Mu.RUnlock()

			if online {
				recipientClient.Send(msg)
			} else {
				s.storeOfflineMessage(msg.Sender, msg.Recipient, msg.Body, "msg")
				client.Send(NexusMessage{
					Type:   "msg_status",
					Body:   "delivered_offline",
					Sender: msg.Recipient,
				})
			}

		case "block":
			if username == "" {
				continue
			}
			if err := s.blockUser(username, msg.Recipient); err != nil {
				client.Send(NexusMessage{Type: "block_result", Error: err.Error()})
			} else {
				log.Printf("[block] %s blocked %s", username, msg.Recipient)
				client.Send(NexusMessage{Type: "block_result", Status: "blocked", Recipient: msg.Recipient})
			}

		case "unblock":
			if username == "" {
				continue
			}
			log.Printf("[block] %s unblocking %s", username, msg.Recipient)
			if err := s.unblockUser(username, msg.Recipient); err != nil {
				client.Send(NexusMessage{Type: "block_result", Error: err.Error()})
			} else {
				client.Send(NexusMessage{Type: "block_result", Status: "unblocked", Recipient: msg.Recipient})
			}

		case "list_blocks":
			if username == "" {
				continue
			}
			client.Send(NexusMessage{Type: "blocks", Results: s.listBlocks(username)})

		case "report_abuse":
			if username == "" {
				continue
			}
			// msg.Recipient = subject (offending user), msg.Status = reason tag, msg.Body = freeform
			if err := s.recordAbuseReport(username, msg.Recipient, msg.Status, msg.Body); err != nil {
				client.Send(NexusMessage{Type: "report_result", Error: err.Error()})
			} else {
				log.Printf("[abuse] report from %s about %s reason=%s", username, msg.Recipient, msg.Status)
				client.Send(NexusMessage{Type: "report_result", Status: "received"})
			}

		case "typing":
			if username == "" {
				continue
			}
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(NexusMessage{
					Type:   "typing",
					Sender: username,
				})
			}
			s.Mu.RUnlock()

		case "presence":
			if username == "" {
				continue
			}
			// Directed key handoff (native_client replies to key_request with a
			// presence carrying public_key + recipient = requester).
			if msg.Recipient != "" && len(msg.PublicKey) == 32 && s.areFriends(username, msg.Recipient) {
				msg.Sender = username
				s.Mu.RLock()
				if peer, ok := s.Clients[msg.Recipient]; ok {
					if err := peer.Send(msg); err != nil {
						log.Printf("[presence] key forward to %s: %v", msg.Recipient, err)
					}
				}
				s.Mu.RUnlock()
			}
			log.Printf("User %s is now %s", username, msg.Status)
			s.Mu.Lock()
			if client, ok := s.Clients[username]; ok {
				client.Status = msg.Status
			}
			s.Mu.Unlock()
			s.broadcastPresence(username, msg.Status)

		case "search":
			if username == "" {
				continue
			}
			log.Printf("User %s searching for: %s", username, msg.Body)
			results := s.searchUsers(msg.Body, username)
			client.Send(NexusMessage{
				Type:    "search_results",
				Results: results,
			})

		case "friend_request":
			if username == "" {
				continue
			}
			err := s.sendFriendRequest(username, msg.Recipient)
			if err != nil {
				log.Printf("Friend request error: %v", err)
				continue
			}
			log.Printf("Friend request: %s -> %s", username, msg.Recipient)
			// Notify recipient if online
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(NexusMessage{
					Type:   "friend_request",
					Sender: username,
				})
			}
			s.Mu.RUnlock()

		case "friend_accept":
			if username == "" {
				continue
			}
			err := s.acceptFriendRequest(msg.Sender, username)
			if err != nil {
				log.Printf("Friend accept error: %v", err)
				continue
			}
			log.Printf("Friend accepted: %s accepted %s", username, msg.Sender)
			// Notify the requester
			s.Mu.RLock()
			if requesterClient, ok := s.Clients[msg.Sender]; ok {
				requesterClient.Send(NexusMessage{
					Type:   "friend_accepted",
					Sender: username,
				})
			}
			s.Mu.RUnlock()

		case "friend_reject":
			if username == "" {
				continue
			}
			_ = s.rejectFriendRequest(msg.Sender, username)
			log.Printf("Friend reject: %s rejected %s", username, msg.Sender)

		case "friend_remove":
			if username == "" {
				continue
			}
			_ = s.removeFriend(username, msg.Recipient)
			log.Printf("Friend removed: %s <-> %s", username, msg.Recipient)
			s.Mu.RLock()
			if peer, ok := s.Clients[msg.Recipient]; ok {
				peer.Send(NexusMessage{Type: "friend_removed", Sender: username})
			}
			s.Mu.RUnlock()

		case "convo_create":
			if username == "" {
				continue
			}
			// Only accept members who are already accepted friends of the
			// creator. Without this, any authed user can spam strangers into
			// unsolicited group chats. Self is always allowed.
			friendSet := map[string]bool{username: true}
			for _, f := range s.getFriends(username) {
				friendSet[f] = true
			}
			eligible := msg.Members[:0:0]
			for _, m := range msg.Members {
				if friendSet[m] {
					eligible = append(eligible, m)
				}
			}
			if len(eligible) == 0 {
				client.Send(NexusMessage{Type: "convo_error", Error: "No eligible members — add friends first"})
				continue
			}
			if err := s.createConversation(msg.ConvoID, msg.ConvoName, username, eligible); err != nil {
				client.Send(NexusMessage{Type: "convo_error", Error: err.Error()})
				continue
			}
			members := s.conversationMembers(msg.ConvoID)
			notice := NexusMessage{
				Type:      "convo_created",
				ConvoID:   msg.ConvoID,
				ConvoName: msg.ConvoName,
				Members:   members,
				Sender:    username,
			}
			s.Mu.RLock()
			for _, m := range members {
				if c, ok := s.Clients[m]; ok {
					c.Send(notice)
				}
			}
			s.Mu.RUnlock()
			log.Printf("Conversation %s (%s) created by %s with %d members", msg.ConvoID, msg.ConvoName, username, len(members))

		case "convo_msg":
			if username == "" || msg.ConvoID == "" {
				continue
			}
			metrics.convoMessages.Add(1)
			members := s.conversationMembers(msg.ConvoID)
			s.Mu.RLock()
			for _, m := range members {
				if m == username {
					continue
				}
				// Prefer per-member envelope so the server never sees plaintext.
				// Fall back to msg.Body for older clients still using the legacy
				// plaintext fan-out path.
				body := msg.Body
				if msg.Envelopes != nil {
					if env, ok := msg.Envelopes[m]; ok {
						body = env
					}
				}
				fanout := NexusMessage{
					Type:    "convo_msg",
					Sender:  username,
					Body:    body,
					ConvoID: msg.ConvoID,
				}
				if c, ok := s.Clients[m]; ok {
					c.Send(fanout)
				} else {
					s.DB.Exec(`INSERT INTO offline_messages (sender, recipient, body, msg_type, convo)
						VALUES (?, ?, ?, 'convo_msg', ?)`, username, m, body, msg.ConvoID)
				}
			}
			s.Mu.RUnlock()

		case "convo_leave":
			if username == "" {
				continue
			}
			_ = s.leaveConversation(msg.ConvoID, username)
			members := s.conversationMembers(msg.ConvoID)
			s.Mu.RLock()
			for _, m := range members {
				if c, ok := s.Clients[m]; ok {
					c.Send(NexusMessage{
						Type: "convo_left", Sender: username, ConvoID: msg.ConvoID,
					})
				}
			}
			s.Mu.RUnlock()

		case "read_receipt":
			if username == "" {
				continue
			}
			// Only friends can send each other read receipts. Without this
			// gate, any authed user could spam fake "I read your message"
			// notifications to strangers — low-impact but a free side-channel.
			// Group read receipts aren't modeled on the wire (no ConvoID
			// field in receipts) so we skip them for now.
			if !s.areFriends(username, msg.Recipient) {
				continue
			}
			s.Mu.RLock()
			if peer, ok := s.Clients[msg.Recipient]; ok {
				peer.Send(NexusMessage{
					Type: "read_receipt", Sender: username, Body: msg.Body,
				})
			}
			s.Mu.RUnlock()

		// Pairwise public-key handoff for NaCl box E2EE. Desktop clients send
		// this when they need a peer's key; the recipient answers with a
		// "presence" message carrying public_key (see native_client).
		case "key_request":
			if username == "" || msg.Recipient == "" {
				continue
			}
			if !s.areFriends(username, msg.Recipient) {
				continue
			}
			metrics.keyRequests.Add(1)
			msg.Sender = username
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			}
			s.Mu.RUnlock()

		case "call_offer", "call_answer", "ice_candidate":
			if username == "" {
				continue
			}
			log.Printf("Signal [%s] from %s to %s", msg.Type, msg.Sender, msg.Recipient)
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			} else {
				client.Send(NexusMessage{
					Type:  "call_error",
					Body:  "User is offline",
					Error: msg.Recipient + " is not available",
				})
			}
			s.Mu.RUnlock()

		case "call_reject", "call_end":
			if username == "" {
				continue
			}
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Send(msg)
			}
			s.Mu.RUnlock()

		// ---------- Servers + Channels ----------

		case "server_create":
			if username == "" {
				continue
			}
			name := strings.TrimSpace(msg.ServerName)
			if !validServerName.MatchString(name) {
				client.Send(NexusMessage{Type: "server_result", Error: "Server name: 2-64 chars, letters/digits/space/-_.'"})
				continue
			}
			visibility := strings.ToLower(strings.TrimSpace(msg.Visibility))
			if visibility != "public" && visibility != "private" {
				visibility = "private"
			}
			id, err := randHex(16)
			if err != nil {
				client.Send(NexusMessage{Type: "server_result", Error: "rand failure"})
				continue
			}
			invite, err := randHex(8)
			if err != nil {
				client.Send(NexusMessage{Type: "server_result", Error: "rand failure"})
				continue
			}
			tx, err := s.DB.Begin()
			if err != nil {
				client.Send(NexusMessage{Type: "server_result", Error: "db error"})
				continue
			}
			func() {
				defer tx.Rollback()
				if _, err := tx.Exec(
					`INSERT INTO servers (id, name, description, owner, visibility, invite_code) VALUES (?,?,?,?,?,?)`,
					id, name, strings.TrimSpace(msg.Topic), username, visibility, invite); err != nil {
					client.Send(NexusMessage{Type: "server_result", Error: "create server: " + err.Error()})
					return
				}
				if _, err := tx.Exec(
					`INSERT INTO server_members (server_id, username, role) VALUES (?, ?, 'owner')`,
					id, username); err != nil {
					client.Send(NexusMessage{Type: "server_result", Error: "add owner: " + err.Error()})
					return
				}
				// Bootstrap channels every server has from day one.
				for i, ch := range []string{"general", "random"} {
					cid, err := randHex(12)
					if err != nil {
						client.Send(NexusMessage{Type: "server_result", Error: "rand failure"})
						return
					}
					if _, err := tx.Exec(
						`INSERT INTO channels (id, server_id, name, kind, position) VALUES (?,?,?, 'text', ?)`,
						cid, id, ch, i); err != nil {
						client.Send(NexusMessage{Type: "server_result", Error: "channel: " + err.Error()})
						return
					}
				}
				if err := tx.Commit(); err != nil {
					client.Send(NexusMessage{Type: "server_result", Error: "commit: " + err.Error()})
					return
				}
				channels, _ := s.listServerChannels(id)
				client.Send(NexusMessage{
					Type:       "server_result",
					Status:     "ok",
					ServerID:   id,
					ServerName: name,
					InviteCode: invite,
					Role:       "owner",
					Visibility: visibility,
					Channels:   channels,
				})
				log.Printf("[server] %s created server %q (%s)", username, name, id)
			}()

		case "server_list":
			if username == "" {
				continue
			}
			servers, err := s.listUserServers(username)
			if err != nil {
				client.Send(NexusMessage{Type: "server_list_result", Error: "db: " + err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "server_list_result", Status: "ok", Servers: servers})

		case "server_join":
			if username == "" {
				continue
			}
			code := strings.TrimSpace(msg.InviteCode)
			if code == "" {
				client.Send(NexusMessage{Type: "server_join_result", Error: "invite_code required"})
				continue
			}
			var serverID, serverName string
			err := s.DB.QueryRow(
				`SELECT id, name FROM servers WHERE invite_code = ?`, code).Scan(&serverID, &serverName)
			if err != nil {
				client.Send(NexusMessage{Type: "server_join_result", Error: "invite invalid"})
				continue
			}
			if _, err := s.DB.Exec(
				`INSERT OR IGNORE INTO server_members (server_id, username, role) VALUES (?, ?, 'member')`,
				serverID, username); err != nil {
				client.Send(NexusMessage{Type: "server_join_result", Error: "db: " + err.Error()})
				continue
			}
			channels, _ := s.listServerChannels(serverID)
			client.Send(NexusMessage{
				Type:       "server_join_result",
				Status:     "ok",
				ServerID:   serverID,
				ServerName: serverName,
				Channels:   channels,
			})
			log.Printf("[server] %s joined %s (%s)", username, serverName, serverID)

		case "server_leave":
			if username == "" || msg.ServerID == "" {
				continue
			}
			// Owners must transfer ownership before leaving; for v1, owner
			// can't leave their own server.
			role := s.userServerRole(msg.ServerID, username)
			if role == "owner" {
				client.Send(NexusMessage{Type: "server_leave_result", Error: "owners can't leave; delete the server or transfer ownership first"})
				continue
			}
			if _, err := s.DB.Exec(
				`DELETE FROM server_members WHERE server_id = ? AND username = ?`,
				msg.ServerID, username); err != nil {
				client.Send(NexusMessage{Type: "server_leave_result", Error: "db: " + err.Error()})
				continue
			}
			client.Send(NexusMessage{Type: "server_leave_result", Status: "ok", ServerID: msg.ServerID})

		case "server_info":
			if username == "" || msg.ServerID == "" {
				continue
			}
			if !s.userIsServerMember(msg.ServerID, username) {
				client.Send(NexusMessage{Type: "server_info_result", Error: "not a member"})
				continue
			}
			channels, _ := s.listServerChannels(msg.ServerID)
			// Members list.
			memberRows, _ := s.DB.Query(`SELECT username FROM server_members WHERE server_id = ?`, msg.ServerID)
			var members []string
			if memberRows != nil {
				for memberRows.Next() {
					var u string
					if memberRows.Scan(&u) == nil {
						members = append(members, u)
					}
				}
				memberRows.Close()
			}
			client.Send(NexusMessage{
				Type:     "server_info_result",
				Status:   "ok",
				ServerID: msg.ServerID,
				Channels: channels,
				Members:  members,
			})

		case "channel_create":
			if username == "" || msg.ServerID == "" {
				continue
			}
			role := s.userServerRole(msg.ServerID, username)
			if role != "owner" && role != "admin" {
				client.Send(NexusMessage{Type: "channel_result", Error: "admin only"})
				continue
			}
			name := strings.ToLower(strings.TrimSpace(msg.ChannelName))
			if !validChannelName.MatchString(name) {
				client.Send(NexusMessage{Type: "channel_result", Error: "channel name: lowercase a-z 0-9 - _ , 2-32 chars"})
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(msg.Kind))
			if kind != "text" && kind != "voice" {
				kind = "text"
			}
			cid, err := randHex(12)
			if err != nil {
				client.Send(NexusMessage{Type: "channel_result", Error: "rand failure"})
				continue
			}
			var maxPos int
			s.DB.QueryRow(`SELECT COALESCE(MAX(position), 0) FROM channels WHERE server_id = ?`, msg.ServerID).Scan(&maxPos)
			if _, err := s.DB.Exec(
				`INSERT INTO channels (id, server_id, name, topic, kind, position) VALUES (?,?,?,?,?,?)`,
				cid, msg.ServerID, name, strings.TrimSpace(msg.Topic), kind, maxPos+1); err != nil {
				client.Send(NexusMessage{Type: "channel_result", Error: "db: " + err.Error()})
				continue
			}
			channels, _ := s.listServerChannels(msg.ServerID)
			// Push update to everyone in the server.
			s.broadcastChannelMsg(msg.ServerID, NexusMessage{
				Type:     "server_channels_updated",
				ServerID: msg.ServerID,
				Channels: channels,
			})
			client.Send(NexusMessage{Type: "channel_result", Status: "ok", ServerID: msg.ServerID, ChannelID: cid})

		case "channel_msg":
			if username == "" || msg.ChannelID == "" {
				continue
			}
			// Resolve server, check membership.
			var serverID string
			if err := s.DB.QueryRow(`SELECT server_id FROM channels WHERE id = ?`, msg.ChannelID).Scan(&serverID); err != nil {
				client.Send(NexusMessage{Type: "channel_msg_result", Error: "no such channel"})
				continue
			}
			if !s.userIsServerMember(serverID, username) {
				client.Send(NexusMessage{Type: "channel_msg_result", Error: "not a member"})
				continue
			}
			body := strings.TrimSpace(msg.Body)
			if body == "" || len(body) > 8000 {
				client.Send(NexusMessage{Type: "channel_msg_result", Error: "body 1-8000 chars"})
				continue
			}
			res, err := s.DB.Exec(
				`INSERT INTO channel_messages (channel_id, sender, body) VALUES (?,?,?)`,
				msg.ChannelID, username, body)
			if err != nil {
				client.Send(NexusMessage{Type: "channel_msg_result", Error: "db: " + err.Error()})
				continue
			}
			id, _ := res.LastInsertId()
			out := NexusMessage{
				Type:      "channel_msg_in",
				ServerID:  serverID,
				ChannelID: msg.ChannelID,
				Sender:    username,
				Body:      body,
				Messages: []ChannelMsg{{
					ID: id, ChannelID: msg.ChannelID, Sender: username, Body: body,
					CreatedAt: time.Now().UTC().Format(time.RFC3339),
				}},
			}
			s.broadcastChannelMsg(serverID, out)

		case "channel_history":
			if username == "" || msg.ChannelID == "" {
				continue
			}
			var serverID string
			if err := s.DB.QueryRow(`SELECT server_id FROM channels WHERE id = ?`, msg.ChannelID).Scan(&serverID); err != nil {
				client.Send(NexusMessage{Type: "channel_history_result", Error: "no such channel"})
				continue
			}
			if !s.userIsServerMember(serverID, username) {
				client.Send(NexusMessage{Type: "channel_history_result", Error: "not a member"})
				continue
			}
			history, err := s.channelHistory(msg.ChannelID, msg.HistoryFrom, 50)
			if err != nil {
				client.Send(NexusMessage{Type: "channel_history_result", Error: "db: " + err.Error()})
				continue
			}
			client.Send(NexusMessage{
				Type:      "channel_history_result",
				Status:    "ok",
				ServerID:  serverID,
				ChannelID: msg.ChannelID,
				Messages:  history,
			})

		default:
			log.Printf("Unknown message type: %s from %s", msg.Type, username)
		}
	}
}

// ---------- Health Check ----------

func (s *NexusServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	dbOK := s.DB.PingContext(ctx) == nil

	v := os.Getenv("Phaze_LATEST_VERSION")
	if v == "" {
		v = "1.0.0-Phaze"
	}

	turnOK := TurnSecret != "" && TurnURL != ""

	s.Mu.RLock()
	clients := len(s.Clients)
	s.Mu.RUnlock()

	status := "ok"
	code := http.StatusOK
	if !dbOK {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":            status,
		"server":            "phaze-nexus",
		"version":           v,
		"database_ok":       dbOK,
		"turn_configured":   turnOK,
		"connected_clients": clients,
	})
}

// ---------- Metrics (Prometheus text format) ----------

var metricsStart = time.Now()

type nexusMetrics struct {
	wsConnections     atomic.Uint64
	wsConnectionsFail atomic.Uint64
	wsMessagesIn      atomic.Uint64
	authSuccess       atomic.Uint64
	authFailure       atomic.Uint64
	keyRequests       atomic.Uint64
	convoMessages     atomic.Uint64
	pstnAttempts      atomic.Uint64
	pstnRejected      atomic.Uint64
}

var metrics = &nexusMetrics{}

func (s *NexusServer) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if tok := strings.TrimSpace(os.Getenv("PHAZE_METRICS_TOKEN")); tok != "" {
		got := r.Header.Get("Authorization")
		if got != "Bearer "+tok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	s.Mu.RLock()
	activeClients := len(s.Clients)
	s.Mu.RUnlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	var pending int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM offline_messages`).Scan(&pending)

	var users int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP nexus_uptime_seconds Seconds since server start\n")
	fmt.Fprintf(w, "# TYPE nexus_uptime_seconds counter\n")
	fmt.Fprintf(w, "nexus_uptime_seconds %.0f\n", time.Since(metricsStart).Seconds())
	fmt.Fprintf(w, "# HELP nexus_active_clients Connected authenticated WebSocket clients\n")
	fmt.Fprintf(w, "# TYPE nexus_active_clients gauge\n")
	fmt.Fprintf(w, "nexus_active_clients %d\n", activeClients)
	fmt.Fprintf(w, "# HELP nexus_registered_users Total user accounts\n")
	fmt.Fprintf(w, "# TYPE nexus_registered_users gauge\n")
	fmt.Fprintf(w, "nexus_registered_users %d\n", users)
	fmt.Fprintf(w, "# HELP nexus_offline_messages_pending Queued messages awaiting recipient login\n")
	fmt.Fprintf(w, "# TYPE nexus_offline_messages_pending gauge\n")
	fmt.Fprintf(w, "nexus_offline_messages_pending %d\n", pending)
	fmt.Fprintf(w, "# HELP nexus_ws_connections_total WebSocket upgrade attempts\n")
	fmt.Fprintf(w, "# TYPE nexus_ws_connections_total counter\n")
	fmt.Fprintf(w, "nexus_ws_connections_total %d\n", metrics.wsConnections.Load())
	fmt.Fprintf(w, "# HELP nexus_ws_connections_failed_total WebSocket upgrades that failed\n")
	fmt.Fprintf(w, "# TYPE nexus_ws_connections_failed_total counter\n")
	fmt.Fprintf(w, "nexus_ws_connections_failed_total %d\n", metrics.wsConnectionsFail.Load())
	fmt.Fprintf(w, "# HELP nexus_ws_messages_in_total Inbound WebSocket messages\n")
	fmt.Fprintf(w, "# TYPE nexus_ws_messages_in_total counter\n")
	fmt.Fprintf(w, "nexus_ws_messages_in_total %d\n", metrics.wsMessagesIn.Load())
	fmt.Fprintf(w, "# HELP nexus_auth_total Auth attempts by result\n")
	fmt.Fprintf(w, "# TYPE nexus_auth_total counter\n")
	fmt.Fprintf(w, "nexus_auth_total{result=\"ok\"} %d\n", metrics.authSuccess.Load())
	fmt.Fprintf(w, "nexus_auth_total{result=\"fail\"} %d\n", metrics.authFailure.Load())
	fmt.Fprintf(w, "# HELP nexus_key_requests_total Pairwise key_request relays\n")
	fmt.Fprintf(w, "# TYPE nexus_key_requests_total counter\n")
	fmt.Fprintf(w, "nexus_key_requests_total %d\n", metrics.keyRequests.Load())
	fmt.Fprintf(w, "# HELP nexus_convo_messages_total Group envelope messages relayed\n")
	fmt.Fprintf(w, "# TYPE nexus_convo_messages_total counter\n")
	fmt.Fprintf(w, "nexus_convo_messages_total %d\n", metrics.convoMessages.Load())
	fmt.Fprintf(w, "# HELP nexus_pstn_total PSTN attempts and rejections\n")
	fmt.Fprintf(w, "# TYPE nexus_pstn_total counter\n")
	fmt.Fprintf(w, "nexus_pstn_total{result=\"attempt\"} %d\n", metrics.pstnAttempts.Load())
	fmt.Fprintf(w, "nexus_pstn_total{result=\"rejected_disabled\"} %d\n", metrics.pstnRejected.Load())
	fmt.Fprintf(w, "# HELP go_memstats_alloc_bytes Currently allocated heap bytes\n")
	fmt.Fprintf(w, "# TYPE go_memstats_alloc_bytes gauge\n")
	fmt.Fprintf(w, "go_memstats_alloc_bytes %d\n", memStats.Alloc)
	fmt.Fprintf(w, "# HELP go_goroutines Currently running goroutines\n")
	fmt.Fprintf(w, "# TYPE go_goroutines gauge\n")
	fmt.Fprintf(w, "go_goroutines %d\n", runtime.NumGoroutine())
}

const rootHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<title>Phaze — Free calls to friends and family</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
	:root { --skype-blue: #00AFF0; --skype-dark: #0078D4; }
	body { margin: 0; font-family: 'Segoe UI', Tahoma, sans-serif; background: #fff; color: #333; }
	.hero { background: linear-gradient(135deg, var(--skype-blue), var(--skype-dark)); color: #fff; text-align: center; padding: 100px 20px; }
	.hero h1 { font-size: 3rem; font-weight: 300; margin: 0 0 20px; }
	.hero h1 strong { font-weight: 700; }
	.hero p { font-size: 1.25rem; opacity: 0.9; max-width: 600px; margin: 0 auto 40px; }
	.btn { display: inline-block; background: #fff; color: var(--skype-blue); text-decoration: none; padding: 16px 40px; border-radius: 4px; font-weight: 700; font-size: 1.1rem; box-shadow: 0 4px 15px rgba(0,0,0,0.1); transition: 0.2s; }
	.btn:hover { transform: translateY(-2px); box-shadow: 0 6px 20px rgba(0,0,0,0.2); }
	.navbar { background: #fff; border-bottom: 1px solid #eee; padding: 15px 40px; display: flex; align-items: center; justify-content: space-between; }
	.nav-brand { font-weight: 700; font-size: 1.5rem; color: var(--skype-blue); display: flex; align-items: center; gap: 10px; }
	.nav-links { display: flex; gap: 30px; list-style: none; margin: 0; padding: 0; }
	.nav-links a { text-decoration: none; color: #333; font-size: 0.9rem; font-weight: 600; }
	.nav-links a:hover { color: var(--skype-blue); }
	.container { max-width: 1140px; margin: 0 auto; }
</style></head><body>
	<nav class="navbar">
		<div class="nav-brand"><svg width="32" height="32" viewBox="0 0 32 32"><circle cx="16" cy="16" r="15" fill="#00AFF0"/><text x="16" y="22" text-anchor="middle" fill="white" font-size="16" font-weight="700" font-family="sans-serif">P</text></svg> Phaze</div>
		<ul class="nav-links">
			<li><a href="/features">Features</a></li>
			<li><a href="/download">Download</a></li>
			<li><a href="/rates">Rates</a></li>
		</ul>
	</nav>
	<div class="stats">
		<div class="stat-item">
			<span class="stat-val" id="node-count">...</span>
			<span class="stat-label">Online Now</span>
		</div>
		<div class="stat-item">
			<span class="stat-val" id="member-count">...</span>
			<span class="stat-label">Sovereign Members</span>
		</div>
		<div class="stat-item">
			<span class="stat-val">E2EE</span>
			<span class="stat-label">Encrypted</span>
		</div>
	</div>
	<section class="hero">
		<div class="container">
			<h1>Stay in touch with the people who <strong>matter most</strong></h1>
			<p>Phaze keeps the world talking. Free video calls, voice calls and instant messaging on any device.</p>
			<a href="/download" class="btn">Download Phaze</a>
		</div>
	</section>
</body></html>`

func (s *NexusServer) landingHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Try to serve the high-fidelity template first
	_, err := os.Stat("templates/landing.html")
	if err == nil {
		http.ServeFile(w, r, "templates/landing.html")
		return
	}
	// Fallback to beautiful inline HTML
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(rootHTML))
}

func (s *NexusServer) downloadHandler(w http.ResponseWriter, r *http.Request) {
	// Custom handling for binaries to ensure correct MIME types
	if strings.HasSuffix(r.URL.Path, ".apk") {
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	}
	http.ServeFile(w, r, "templates/download.html")
}

func (s *NexusServer) fileDownloadHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/downloads/")

	// Force octet-stream + attachment for every binary so mobile browsers
	// (Samsung Internet, Chrome Android) save to Downloads instead of
	// handing off to the package installer or a file viewer.
	if strings.HasSuffix(path, ".apk") {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"Phaze.apk\"")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
	} else if strings.HasSuffix(path, ".exe") {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"Phaze.exe\"")
		w.Header().Set("Cache-Control", "no-store")
	} else if strings.HasSuffix(path, ".linux") {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"Phaze.linux\"")
		w.Header().Set("Cache-Control", "no-store")
	}

	http.ServeFile(w, r, "public/downloads/"+path)
}

func (s *NexusServer) featuresHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/features.html")
}

func (s *NexusServer) ratesHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/rates.html")
}

func (s *NexusServer) aboutHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/about.html")
}

func (s *NexusServer) supportHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/support.html")
}

func (s *NexusServer) privacyHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/privacy.html")
}

func (s *NexusServer) termsHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/terms.html")
}

func (s *NexusServer) legalHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "templates/legal.html")
}

func (s *NexusServer) resetHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		token = r.FormValue("token")
		pw := r.FormValue("password")
		if err := s.consumePasswordReset(token, pw); err != nil {
			fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>Reset failed</title><body style="font-family:system-ui;max-width:520px;margin:80px auto;padding:20px"><h1>Reset failed</h1><p>%s</p><p><a href="/">Back to Phaze</a></p>`, err.Error())
			return
		}
		fmt.Fprint(w, `<!doctype html><meta charset=utf-8><title>Password reset</title><body style="font-family:system-ui;max-width:520px;margin:80px auto;padding:20px"><h1>Password updated</h1><p>You can now log in with your new password.</p><p><a href="/">Back to Phaze</a></p>`)
		return
	}
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset=utf-8><title>Reset Phaze password</title><style>body{font-family:system-ui;max-width:520px;margin:80px auto;padding:20px}input{width:100%%;padding:10px;margin:8px 0;font-size:1rem}button{background:#00AFF0;color:#fff;border:0;padding:12px 24px;border-radius:6px;font-size:1rem;cursor:pointer}</style></head><body><h1>Reset your Phaze password</h1><form method="POST"><input type="hidden" name="token" value="%s"><label>New password (min. 8 chars)<input type="password" name="password" minlength="8" required></label><button type="submit">Set new password</button></form></body></html>`, token)
}

// ---------- Admin moderation API ----------

// adminFromRequest authenticates an admin caller. Expects Authorization:
// Bearer <session_token>. Returns "" + status code on failure (already
// written by the helper). The session_token is the standard one issued
// by /auth — there is no separate "admin token".
func (s *NexusServer) adminFromRequest(w http.ResponseWriter, r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		http.Error(w, "Authorization required", http.StatusUnauthorized)
		return ""
	}
	tok := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	u := s.sessionUsername(tok)
	if u == "" {
		http.Error(w, "invalid or expired session", http.StatusUnauthorized)
		return ""
	}
	if !s.userIsAdmin(u) {
		http.Error(w, "admin access required", http.StatusForbidden)
		return ""
	}
	return u
}

// AdminReport is one row of the abuse_reports table for the listing endpoint.
type AdminReport struct {
	ID         int64  `json:"id"`
	Reporter   string `json:"reporter"`
	Subject    string `json:"subject"`
	Reason     string `json:"reason"`
	Body       string `json:"body"`
	Status     string `json:"status"`
	ResolvedBy string `json:"resolved_by,omitempty"`
	ResolvedAt string `json:"resolved_at,omitempty"`
	CreatedAt  string `json:"created_at"`
}

func (s *NexusServer) adminReportsHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	rows, err := s.DB.Query(
		`SELECT id, reporter, subject, reason, COALESCE(body, ''), COALESCE(status, 'pending'),
		         COALESCE(resolved_by, ''), COALESCE(CAST(resolved_at AS TEXT), ''), CAST(created_at AS TEXT)
		   FROM abuse_reports
		  WHERE COALESCE(status, 'pending') = ?
		  ORDER BY id DESC LIMIT 500`, status)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []AdminReport{}
	for rows.Next() {
		var rep AdminReport
		if err := rows.Scan(&rep.ID, &rep.Reporter, &rep.Subject, &rep.Reason, &rep.Body,
			&rep.Status, &rep.ResolvedBy, &rep.ResolvedAt, &rep.CreatedAt); err != nil {
			continue
		}
		out = append(out, rep)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func (s *NexusServer) adminResolveReportHandler(w http.ResponseWriter, r *http.Request) {
	admin := s.adminFromRequest(w, r)
	if admin == "" {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Path: /api/v1/admin/reports/{id}/resolve
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/reports/"), "/")
	if len(parts) < 2 || parts[1] != "resolve" {
		http.Error(w, "expected /api/v1/admin/reports/{id}/resolve", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "bad report id", http.StatusBadRequest)
		return
	}
	res, err := s.DB.Exec(
		`UPDATE abuse_reports SET status = 'resolved', resolved_by = ?, resolved_at = CURRENT_TIMESTAMP WHERE id = ?`,
		admin, id)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		http.Error(w, "report not found", http.StatusNotFound)
		return
	}
	log.Printf("[admin] %s resolved report %d", admin, id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": id})
}

func (s *NexusServer) adminBanHandler(w http.ResponseWriter, r *http.Request) {
	admin := s.adminFromRequest(w, r)
	if admin == "" {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Path: /api/v1/admin/users/{username}/(ban|unban)
	tail := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/")
	parts := strings.Split(tail, "/")
	if len(parts) < 2 {
		http.Error(w, "expected /api/v1/admin/users/{username}/(ban|unban)", http.StatusBadRequest)
		return
	}
	target := parts[0]
	action := parts[1]
	if target == "" || !validUsername(target) {
		http.Error(w, "bad username", http.StatusBadRequest)
		return
	}
	if target == admin {
		http.Error(w, "cannot ban yourself", http.StatusBadRequest)
		return
	}

	var reason string
	if r.ContentLength > 0 && r.ContentLength < 4096 {
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
		reason = strings.TrimSpace(body.Reason)
	}

	switch action {
	case "ban":
		res, err := s.DB.Exec(
			`UPDATE users SET is_banned = 1, ban_reason = ?, banned_at = CURRENT_TIMESTAMP WHERE username = ?`,
			reason, target)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		// Revoke all live sessions so the user is logged out everywhere.
		s.DB.Exec(`UPDATE session_tokens SET revoked = 1 WHERE username = ?`, target)
		// Kick connected session, if any.
		s.Mu.Lock()
		if c, ok := s.Clients[target]; ok {
			body := "Account suspended"
			if reason != "" {
				body += ": " + reason
			}
			c.Send(NexusMessage{Type: "kicked", Body: body})
			c.Conn.Close()
			delete(s.Clients, target)
		}
		s.Mu.Unlock()
		log.Printf("[admin] %s banned %s (reason=%q)", admin, target, reason)
	case "unban":
		res, err := s.DB.Exec(
			`UPDATE users SET is_banned = 0, ban_reason = '', banned_at = NULL WHERE username = ?`, target)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		log.Printf("[admin] %s unbanned %s", admin, target)
	default:
		http.Error(w, "expected /ban or /unban", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": target, "action": action})
}

func (s *NexusServer) adminBannedUsersHandler(w http.ResponseWriter, r *http.Request) {
	if s.adminFromRequest(w, r) == "" {
		return
	}
	rows, err := s.DB.Query(
		`SELECT username, COALESCE(ban_reason, ''), COALESCE(CAST(banned_at AS TEXT), '')
		   FROM users WHERE is_banned = 1 ORDER BY banned_at DESC LIMIT 500`)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type bannedUser struct {
		Username string `json:"username"`
		Reason   string `json:"reason"`
		BannedAt string `json:"banned_at"`
	}
	out := []bannedUser{}
	for rows.Next() {
		var u bannedUser
		if err := rows.Scan(&u.Username, &u.Reason, &u.BannedAt); err != nil {
			continue
		}
		out = append(out, u)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ---------- Update manifest (GitHub Releases) ----------

// PlatformAsset is one downloadable artifact for the auto-update flow.
// SHA256 is the canonical integrity check; the client MUST verify before
// running the new binary. Empty SHA256 means checksums.txt was unavailable
// at refresh time — clients should refuse to auto-install in that case.
type PlatformAsset struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Name   string `json:"name"`
}

type UpdateManifest struct {
	Version      string                   `json:"version"`
	ReleaseURL   string                   `json:"release_url"`
	ReleaseNotes string                   `json:"release_notes,omitempty"`
	PublishedAt  string                   `json:"published_at,omitempty"`
	Platforms    map[string]PlatformAsset `json:"platforms"`
	RefreshedAt  int64                    `json:"refreshed_at"`
	Source       string                   `json:"source"` // "github" or "env"
}

type updateCache struct {
	mu       sync.RWMutex
	manifest UpdateManifest
	expires  time.Time
}

var updates = &updateCache{}

const updateTTL = 5 * time.Minute

// ghRelease is the subset of the GitHub Releases JSON we care about.
type ghRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// classifyAsset returns the platform key ("windows" / "linux" / "android")
// for an asset name, or "" if it doesn't match any of our targets. Match is
// intentionally broad so we accept either "Phaze.exe"/"Phaze.apk"/"Phaze.linux"
// (current naming) or the goreleaser pattern "Phaze_<os>_<arch>.<ext>".
func classifyAsset(name string) string {
	low := strings.ToLower(name)
	switch {
	case strings.HasSuffix(low, ".exe"),
		strings.Contains(low, "windows"):
		return "windows"
	case strings.HasSuffix(low, ".apk"):
		return "android"
	case strings.HasSuffix(low, ".linux"),
		strings.Contains(low, "linux"):
		return "linux"
	}
	return ""
}

// fetchChecksums downloads checksums.txt (goreleaser format: "<sha256>  <name>")
// and returns a map of filename -> sha256 hex. Returns nil on error so the
// caller can decide whether to publish an unverified manifest.
func fetchChecksums(url string, client *http.Client) map[string]string {
	if url == "" {
		return nil
	}
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Accept", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sum, name := fields[0], fields[1]
		if len(sum) != 64 {
			continue
		}
		// Strip a leading "*" goreleaser sometimes prefixes for binary mode.
		name = strings.TrimPrefix(name, "*")
		out[name] = strings.ToLower(sum)
	}
	return out
}

// refreshUpdateManifest fetches the latest release from GitHub and rebuilds
// the cached manifest. Falls back to env-var-only manifest when the API
// roundtrip fails (so the endpoint never disappears, just becomes thin).
func refreshUpdateManifest(repo string) UpdateManifest {
	envVersion := strings.TrimSpace(os.Getenv("Phaze_LATEST_VERSION"))
	envURL := strings.TrimSpace(os.Getenv("Phaze_UPDATE_URL"))
	if envURL == "" {
		envURL = "https://github.com/" + repo + "/releases/latest"
	}

	manifest := UpdateManifest{
		Version:     envVersion,
		ReleaseURL:  envURL,
		Platforms:   map[string]PlatformAsset{},
		RefreshedAt: time.Now().Unix(),
		Source:      "env",
	}

	if repo == "" {
		return manifest
	}

	client := &http.Client{Timeout: 8 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[update] github fetch: %v", err)
		return manifest
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[update] github status %d", resp.StatusCode)
		return manifest
	}

	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		log.Printf("[update] github decode: %v", err)
		return manifest
	}

	// Find checksums.txt asset (goreleaser default name) before classifying
	// per-platform so we can attach SHA-256 to each.
	var checksumsURL string
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, "checksums.txt") {
			checksumsURL = a.BrowserDownloadURL
			break
		}
	}
	sums := fetchChecksums(checksumsURL, client)

	platforms := map[string]PlatformAsset{}
	for _, a := range rel.Assets {
		plat := classifyAsset(a.Name)
		if plat == "" {
			continue
		}
		if _, taken := platforms[plat]; taken {
			continue // first match wins per platform
		}
		platforms[plat] = PlatformAsset{
			URL:    a.BrowserDownloadURL,
			SHA256: sums[a.Name],
			Size:   a.Size,
			Name:   a.Name,
		}
	}

	version := strings.TrimPrefix(rel.TagName, "v")
	if version == "" {
		version = envVersion
	}

	return UpdateManifest{
		Version:      version,
		ReleaseURL:   rel.HTMLURL,
		ReleaseNotes: rel.Body,
		PublishedAt:  rel.PublishedAt,
		Platforms:    platforms,
		RefreshedAt:  time.Now().Unix(),
		Source:       "github",
	}
}

func (s *NexusServer) versionHandler(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(os.Getenv("PHAZE_RELEASE_REPO"))
	if repo == "" {
		repo = "jakes1345/skype7-reborn"
	}

	updates.mu.RLock()
	fresh := time.Now().Before(updates.expires) && updates.manifest.RefreshedAt > 0
	manifest := updates.manifest
	updates.mu.RUnlock()

	if !fresh {
		manifest = refreshUpdateManifest(repo)
		updates.mu.Lock()
		updates.manifest = manifest
		updates.expires = time.Now().Add(updateTTL)
		updates.mu.Unlock()
	}

	// Backwards-compat shim: older clients only look at `version` and `url`.
	// New clients use `platforms` + `release_notes`.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	out := struct {
		UpdateManifest
		// Legacy fields:
		URL string `json:"url"`
	}{UpdateManifest: manifest, URL: manifest.ReleaseURL}
	json.NewEncoder(w).Encode(out)
}

func (s *NexusServer) statsHandler(w http.ResponseWriter, r *http.Request) {
	s.Mu.RLock()
	active := len(s.Clients)
	s.Mu.RUnlock()

	var total int
	err := s.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&total)
	if err != nil {
		total = 0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_nodes":  active,
		"total_members": total,
		"timestamp":     time.Now().Unix(),
		"status":        "online",
	})
}

// ---------- Main ----------

// resolveWorkingDir sets the process working directory so relative paths
// templates/, public/, and the default SQLite file resolve correctly when
// the binary is launched from outside nexus_server/ (for example ../bin/phaze-nexus).
func resolveWorkingDir() {
	if d := strings.TrimSpace(os.Getenv("PHAZE_ASSET_ROOT")); d != "" {
		abs, err := filepath.Abs(d)
		if err != nil {
			log.Printf("[nexus] PHAZE_ASSET_ROOT: %v", err)
			return
		}
		if err := os.Chdir(abs); err != nil {
			log.Printf("[nexus] PHAZE_ASSET_ROOT chdir %q: %v", abs, err)
		} else {
			log.Printf("[nexus] working directory: %s (PHAZE_ASSET_ROOT)", abs)
		}
		return
	}
	if _, err := os.Stat("templates/landing.html"); err == nil {
		if wd, err := os.Getwd(); err == nil {
			log.Printf("[nexus] working directory: %s", wd)
		}
		return
	}
	exe, err := os.Executable()
	if err != nil {
		log.Printf("[nexus] could not resolve executable: %v", err)
		return
	}
	exeDir := filepath.Clean(filepath.Dir(exe))
	candidates := []string{
		exeDir,
		filepath.Join(exeDir, "..", "nexus_server"),
		filepath.Join(exeDir, "..", "..", "nexus_server"),
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "templates", "landing.html")); err != nil {
			continue
		}
		if err := os.Chdir(abs); err != nil {
			log.Printf("[nexus] chdir %q: %v", abs, err)
			continue
		}
		log.Printf("[nexus] working directory: %s (auto-detected)", abs)
		return
	}
	if wd, err := os.Getwd(); err == nil {
		log.Printf("[nexus] working directory: %s (templates/ not found; some pages use built-in HTML fallback)", wd)
	}
}

func main() {
	resolveWorkingDir()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "nexus.db"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	// Enable WAL mode for better concurrent access
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	server := &NexusServer{
		DB:      db,
		Clients: make(map[string]*Client),
	}
	server.initDB()

	http.HandleFunc("/ws", rateLimit(server.handleConnections))
	http.HandleFunc("/api/v1/version", rateLimit(server.versionHandler))
	http.HandleFunc("/api/v1/profile/", rateLimit(server.profileHandler))
	http.HandleFunc("/api/v1/avatars/", rateLimit(server.avatarHandler))
	http.HandleFunc("/twiml/outbound", rateLimit(server.twimlHandler))

	fs := http.FileServer(http.Dir("public"))
	http.Handle("/public/", http.StripPrefix("/public/", fs))
	http.HandleFunc("/downloads/", server.fileDownloadHandler)

	// Web client (React/Vite SPA). The Docker build stage compiles
	// web/dist into /app/web; in dev a symlink (or PHAZE_WEB_DIR env)
	// points elsewhere. SPA fallback to index.html so client-side routes
	// keep working on hard refresh; static assets get long-lived caching
	// via Vite's content-hashed filenames.
	webDir := strings.TrimSpace(os.Getenv("PHAZE_WEB_DIR"))
	if webDir == "" {
		webDir = "web"
	}
	if _, err := os.Stat(filepath.Join(webDir, "index.html")); err == nil {
		webFS := http.FileServer(http.Dir(webDir))
		http.HandleFunc("/web/", func(w http.ResponseWriter, r *http.Request) {
			rel := strings.TrimPrefix(r.URL.Path, "/web/")
			candidate := filepath.Join(webDir, filepath.FromSlash(rel))
			if rel != "" {
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					if strings.HasPrefix(rel, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					}
					http.StripPrefix("/web/", webFS).ServeHTTP(w, r)
					return
				}
			}
			// SPA fallback — anything unknown serves index.html.
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, filepath.Join(webDir, "index.html"))
		})
		// Bare /web → redirect to /web/ so relative asset URLs resolve correctly.
		http.HandleFunc("/web", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/web/", http.StatusFound)
		})
		log.Printf("[web] serving SPA from %s at /web/", webDir)
	} else {
		log.Printf("[web] no SPA at %s — /web/ will 404 until web client is built", webDir)
	}

	http.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		fmt.Fprint(w, "User-agent: *\nAllow: /\nDisallow: /api/\nDisallow: /ws\nDisallow: /reset\nSitemap: https://phazechat.world/sitemap.xml\n")
	})
	http.HandleFunc("/sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		const base = "https://phazechat.world"
		paths := []string{"/", "/download", "/features", "/rates", "/about", "/support", "/privacy", "/terms", "/legal"}
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`)
		fmt.Fprint(w, `<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
		for _, p := range paths {
			fmt.Fprintf(w, `<url><loc>%s%s</loc></url>`, base, p)
		}
		fmt.Fprint(w, `</urlset>`)
	})

	http.HandleFunc("/", server.landingHandler)
	http.HandleFunc("/download", server.downloadHandler)
	http.HandleFunc("/features", server.featuresHandler)
	http.HandleFunc("/rates", server.ratesHandler)
	http.HandleFunc("/about", server.aboutHandler)
	http.HandleFunc("/support", server.supportHandler)
	http.HandleFunc("/privacy", server.privacyHandler)
	http.HandleFunc("/terms", server.termsHandler)
	http.HandleFunc("/legal", server.legalHandler)
	http.HandleFunc("/reset", server.resetHandler)
	http.HandleFunc("/version", server.versionHandler)
	http.HandleFunc("/health", server.healthHandler)
	http.HandleFunc("/metrics", server.metricsHandler)
	http.HandleFunc("/api/v1/admin/reports", server.adminReportsHandler)
	http.HandleFunc("/api/v1/admin/reports/", server.adminResolveReportHandler) // /{id}/resolve
	http.HandleFunc("/api/v1/admin/users/", server.adminBanHandler)             // /{username}/(ban|unban)
	http.HandleFunc("/api/v1/admin/banned", server.adminBannedUsersHandler)

	http.HandleFunc("/api/v1/stats", rateLimit(server.statsHandler))

	bindAddr := os.Getenv("BIND_ADDR")
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	log.Printf("Phaze Nexus Server v1.0.0 starting on %s:%s...", bindAddr, port)
	log.Printf("  WebSocket endpoint: ws://%s:%s/ws", bindAddr, port)
	log.Printf("  Health check: http://%s:%s/health", bindAddr, port)

	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			server.Mu.RLock()
			count := len(server.Clients)
			server.Mu.RUnlock()
			log.Printf("Connected clients: %d", count)
		}
	}()

	err = http.ListenAndServe(bindAddr+":"+port, nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func (s *NexusServer) sendSMS(to, body string) error {
	sid := os.Getenv("TWILIO_SID")
	token := os.Getenv("TWILIO_TOKEN")
	from := os.Getenv("TWILIO_FROM")

	if sid == "" || token == "" || from == "" {
		log.Printf("[SMS-SIM] To: %s | Body: %s", to, body)
		return nil
	}

	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", sid)
	v := url.Values{}
	v.Set("To", to)
	v.Set("From", from)
	v.Set("Body", body)

	req, _ := http.NewRequest("POST", apiURL, strings.NewReader(v.Encode()))
	req.SetBasicAuth(sid, token)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (s *NexusServer) broadcastProfileUpdate(username, displayName, mood string) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	msg := NexusMessage{
		Type:        "profile_update",
		Sender:      username,
		DisplayName: displayName,
		Mood:        mood,
	}
	for _, client := range s.Clients {
		if client.Username != username {
			client.Send(msg)
		}
	}
}

func (s *NexusServer) profileHandler(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/v1/profile/")
	if username == "" {
		http.Error(w, "Username required", 400)
		return
	}
	var displayName, mood string
	err := s.DB.QueryRow("SELECT display_name, mood FROM users WHERE username = ?", username).Scan(&displayName, &mood)
	if err != nil {
		http.Error(w, "User not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"username":     username,
		"display_name": displayName,
		"mood":         mood,
	})
}

func (s *NexusServer) avatarHandler(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/v1/avatars/")
	if username == "" {
		http.Error(w, "Username required", 400)
		return
	}
	// Securely serve avatar file
	path := "avatars/" + username + ".png"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.ServeFile(w, r, "assets/default_avatar.png")
		return
	}
	http.ServeFile(w, r, path)
}

func (s *NexusServer) handleBotMessage(client *Client, msg NexusMessage) {
	reply := NexusMessage{
		Type:      "msg",
		Sender:    "PhazeBot",
		Recipient: msg.Sender,
		Body:      "I am the Phaze Mesh Assistant. Try: /mesh, /version, /pstn (PSTN status), /webrtc",
	}

	cmd := strings.ToLower(strings.TrimSpace(msg.Body))
	switch {
	case cmd == "/mesh":
		s.Mu.RLock()
		count := len(s.Clients)
		s.Mu.RUnlock()
		reply.Body = fmt.Sprintf("The Phaze Mesh currently has %d active sovereign peers.", count)
	case cmd == "/webrtc":
		reply.Body = "Phaze voice/video uses WebRTC (Pion on desktop, browser APIs on web). Signaling goes over Nexus; media is peer-to-peer when possible, with TURN from your relay when NAT blocks direct paths."
	case cmd == "/version":
		reply.Body = "Nexus Server v1.0.0-Phaze | Build: Enterprise-Mesh"
	case cmd == "/pstn":
		if pstnBridgeEnabled() {
			reply.Body = "PSTN bridge is ON for this relay (Twilio). Link your phone in Settings for verified outbound. Otherwise use WebRTC calls in chat."
		} else {
			reply.Body = "PSTN bridge is OFF. Voice/video is WebRTC between Phaze users only — no carrier charges."
		}
	}
	client.Send(reply)
}

func (s *NexusServer) twimlHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/xml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response><Dial><Conference>Phaze_MESH_BRIDGE</Conference></Dial></Response>`))
}

func (s *NexusServer) initiateTwilioCall(to string) error {
	sid := os.Getenv("TWILIO_SID")
	token := os.Getenv("TWILIO_TOKEN")
	from := os.Getenv("TWILIO_FROM")
	appURL := os.Getenv("Phaze_APP_URL")

	if sid == "" || token == "" || from == "" || appURL == "" {
		log.Printf("[PSTN-SIM] Initiating call to %s via TwiML Hub", to)
		return nil
	}

	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Calls.json", sid)
	v := url.Values{}
	v.Set("To", to)
	v.Set("From", from)
	v.Set("Url", appURL+"/twiml/outbound")

	req, _ := http.NewRequest("POST", apiURL, strings.NewReader(v.Encode()))
	req.SetBasicAuth(sid, token)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
