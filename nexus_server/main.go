package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
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

type ipLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
	r        rate.Limit
	burst    int
}

func newIPLimiter(r rate.Limit, burst int) *ipLimiter {
	return &ipLimiter{limiters: map[string]*rate.Limiter{}, r: r, burst: burst}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	lim, ok := l.limiters[ip]
	if !ok {
		lim = rate.NewLimiter(l.r, l.burst)
		l.limiters[ip] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if i := strings.Index(xf, ","); i >= 0 {
			return strings.TrimSpace(xf[:i])
		}
		return strings.TrimSpace(xf)
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
	}
	for _, q := range migrations {
		if _, err := s.DB.Exec(q); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			log.Printf("DB migration skipped (%v)", err)
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
		return
	}
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
				s.Mu.Lock()
				delete(s.Clients, username)
				s.Mu.Unlock()
				s.broadcastPresence(username, "Offline")
				log.Printf("User %s disconnected", username)
			}
			return
		}

		// Per-connection rate limit. Drop silently on overflow: an authed
		// spammer shouldn't learn they've tripped the limiter.
		if !client.msgLimiter.Allow() {
			log.Printf("[ratelimit] dropping %q from %s", msg.Type, username)
			continue
		}

		switch msg.Type {
		case "pstn_call":
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
				client.Send(NexusMessage{Type: "auth_result", Error: "Password required"})
				continue
			}
			if !s.authenticateUser(msg.Sender, msg.Body) {
				client.Send(NexusMessage{Type: "auth_result", Error: "Invalid username or password"})
				continue
			}
			if !s.verifyTOTP(msg.Sender, msg.TOTPCode) {
				client.Send(NexusMessage{Type: "auth_result", Error: "2FA code required or invalid", Status: "totp_required"})
				continue
			}
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
			s.Mu.RLock()
			if peer, ok := s.Clients[msg.Recipient]; ok {
				peer.Send(NexusMessage{
					Type: "read_receipt", Sender: username, Body: msg.Body,
				})
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

		default:
			log.Printf("Unknown message type: %s from %s", msg.Type, username)
		}
	}
}

// ---------- Health Check ----------

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","server":"phaze-nexus","version":"1.0.0"}`))
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

func (s *NexusServer) versionHandler(w http.ResponseWriter, r *http.Request) {
	v := os.Getenv("Phaze_LATEST_VERSION")
	if v == "" {
		v = "1.0.0-Phaze"
	}
	u := os.Getenv("Phaze_UPDATE_URL")
	if u == "" {
		u = "https://phazechat.world/releases"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"version": v,
		"url":     u,
	})
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

func main() {
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
	http.HandleFunc("/health", healthHandler)

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
		Body:      "I am the Phaze Mesh Assistant. Try these commands: /mesh, /version, /pstn",
	}

	cmd := strings.ToLower(strings.TrimSpace(msg.Body))
	switch {
	case cmd == "/mesh":
		s.Mu.RLock()
		count := len(s.Clients)
		s.Mu.RUnlock()
		reply.Body = fmt.Sprintf("The Phaze Mesh currently has %d active sovereign peers.", count)
	case cmd == "/version":
		reply.Body = "Nexus Server v1.0.0-Phaze | Build: Enterprise-Mesh"
	case cmd == "/pstn":
		reply.Body = "PSTN Bridge is ACTIVE. Link your phone in Settings to use Caller ID."
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
