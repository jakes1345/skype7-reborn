package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

// newTestServer spins up a NexusServer backed by a temp SQLite DB and an
// httptest.Server exposing /ws. Returns the server and the ws:// base URL.
func newTestServer(t *testing.T) (*NexusServer, *httptest.Server, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "smoke.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Mirror main.go: WAL + busy_timeout. Without these the test goroutine
	// (reading via srv.DB) can deadlock with the server's write goroutine.
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")
	t.Cleanup(func() { db.Close() })

	srv := &NexusServer{DB: db, Clients: map[string]*Client{}}
	srv.initDB()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", srv.handleConnections)
	mux.HandleFunc("/api/v1/admin/reports", srv.adminReportsHandler)
	mux.HandleFunc("/api/v1/admin/reports/", srv.adminResolveReportHandler)
	mux.HandleFunc("/api/v1/admin/users/", srv.adminBanHandler)
	mux.HandleFunc("/api/v1/admin/banned", srv.adminBannedUsersHandler)
	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)

	u, _ := url.Parse(hs.URL)
	wsBase := "ws://" + u.Host
	return srv, hs, wsBase
}

// dial opens a WS connection to /ws.
func dial(t *testing.T, wsBase string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(wsBase+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	return c
}

// readUntil reads messages until one matches the predicate or the deadline trips.
// Each successful read resets the read deadline so bursty server messages cannot
// consume the entire window before the awaited frame arrives.
func readUntil(t *testing.T, c *websocket.Conn, want func(NexusMessage) bool) NexusMessage {
	t.Helper()
	const perRead = 5 * time.Second
	for {
		c.SetReadDeadline(time.Now().Add(perRead))
		var m NexusMessage
		if err := c.ReadJSON(&m); err != nil {
			t.Fatalf("read: %v", err)
		}
		if want(m) {
			return m
		}
	}
}

// registerAndVerify creates a user via the public API and flips is_verified
// directly so we don't have to round-trip the email code.
func registerAndVerify(t *testing.T, srv *NexusServer, username, password string) {
	t.Helper()
	if _, err := srv.registerUser(username, username+"@example.com", "smoke", password); err != nil {
		t.Fatalf("register %s: %v", username, err)
	}
	if _, err := srv.DB.Exec("UPDATE users SET is_verified = 1 WHERE username = ?", username); err != nil {
		t.Fatalf("verify %s: %v", username, err)
	}
}

func auth(t *testing.T, c *websocket.Conn, username, password string) {
	t.Helper()
	if err := c.WriteJSON(NexusMessage{Type: "auth", Sender: username, Body: password}); err != nil {
		t.Fatalf("send auth: %v", err)
	}
	res := readUntil(t, c, func(m NexusMessage) bool { return m.Type == "auth_result" })
	if res.Status != "ok" {
		t.Fatalf("auth %s failed: %q", username, res.Error)
	}
}

// TestSmoke_RegisterAuthMessageSignaling locks in Phase 1-4: bcrypt auth,
// authenticated-sender enforcement, message relay (proxy for E2EE body),
// and SDP signaling round-trip.
func TestSmoke_RegisterAuthMessageSignaling(t *testing.T) {
	srv, _, wsBase := newTestServer(t)

	registerAndVerify(t, srv, "alice", "password123")
	registerAndVerify(t, srv, "bob", "password123")

	alice := dial(t, wsBase)
	bob := dial(t, wsBase)

	auth(t, alice, "alice", "password123")
	auth(t, bob, "bob", "password123")

	// Drain the post-auth burst (friends/convos/pending) until both are quiet.
	// We rely on read deadlines in subsequent reads to bound the wait.
	time.Sleep(100 * time.Millisecond)

	// --- E2EE message relay ---
	// The body here stands in for the NaCl-sealed payload the real client
	// produces. The relay must forward it byte-for-byte and stamp the
	// authenticated sender, not trust the client's claim.
	cipherBody := "SEALED::deadbeefcafebabe"
	if err := alice.WriteJSON(NexusMessage{
		Type:      "msg",
		Sender:    "mallory", // intentionally wrong — server must overwrite
		Recipient: "bob",
		Body:      cipherBody,
	}); err != nil {
		t.Fatalf("alice send msg: %v", err)
	}

	got := readUntil(t, bob, func(m NexusMessage) bool { return m.Type == "msg" })
	if got.Sender != "alice" {
		t.Fatalf("sender forgery not blocked: got %q want %q", got.Sender, "alice")
	}
	if got.Body != cipherBody {
		t.Fatalf("body mutated in transit: got %q want %q", got.Body, cipherBody)
	}

	// --- Signaling round-trip (call_offer → call_answer → ice_candidate) ---
	if err := alice.WriteJSON(NexusMessage{
		Type: "call_offer", Sender: "alice", Recipient: "bob",
		SDP: "v=0\r\no=alice 0 0 IN IP4 127.0.0.1\r\n",
	}); err != nil {
		t.Fatalf("alice send call_offer: %v", err)
	}
	offer := readUntil(t, bob, func(m NexusMessage) bool { return m.Type == "call_offer" })
	if !strings.HasPrefix(offer.SDP, "v=0") {
		t.Fatalf("call_offer SDP corrupted: %q", offer.SDP)
	}

	if err := bob.WriteJSON(NexusMessage{
		Type: "call_answer", Sender: "bob", Recipient: "alice",
		SDP: "v=0\r\no=bob 0 0 IN IP4 127.0.0.1\r\n",
	}); err != nil {
		t.Fatalf("bob send call_answer: %v", err)
	}
	answer := readUntil(t, alice, func(m NexusMessage) bool { return m.Type == "call_answer" })
	if !strings.HasPrefix(answer.SDP, "v=0") {
		t.Fatalf("call_answer SDP corrupted: %q", answer.SDP)
	}

	if err := alice.WriteJSON(NexusMessage{
		Type: "ice_candidate", Sender: "alice", Recipient: "bob",
		Candidate: "candidate:1 1 UDP 2130706431 127.0.0.1 54321 typ host",
	}); err != nil {
		t.Fatalf("alice send ice_candidate: %v", err)
	}
	cand := readUntil(t, bob, func(m NexusMessage) bool { return m.Type == "ice_candidate" })
	if !strings.Contains(cand.Candidate, "127.0.0.1") {
		t.Fatalf("ice_candidate corrupted: %q", cand.Candidate)
	}
}

// TestSmoke_BlockSuppressesDelivery confirms that once alice blocks bob,
// bob's messages to alice are dropped (alice receives nothing, no offline
// queueing) while the sender still sees a non-leaky delivered_offline ack.
// Unblock restores delivery.
func TestSmoke_BlockSuppressesDelivery(t *testing.T) {
	srv, _, wsBase := newTestServer(t)

	registerAndVerify(t, srv, "alice", "password123")
	registerAndVerify(t, srv, "bob", "password123")

	alice := dial(t, wsBase)
	bob := dial(t, wsBase)
	auth(t, alice, "alice", "password123")
	auth(t, bob, "bob", "password123")
	time.Sleep(100 * time.Millisecond)

	// alice blocks bob.
	if err := alice.WriteJSON(NexusMessage{Type: "block", Recipient: "bob"}); err != nil {
		t.Fatalf("send block: %v", err)
	}
	res := readUntil(t, alice, func(m NexusMessage) bool { return m.Type == "block_result" })
	if res.Status != "blocked" {
		t.Fatalf("block failed: status=%q err=%q", res.Status, res.Error)
	}

	// bob → alice should be dropped. bob still gets a delivered_offline ack
	// (we deliberately do not signal "you were blocked").
	if err := bob.WriteJSON(NexusMessage{Type: "msg", Recipient: "alice", Body: "hi"}); err != nil {
		t.Fatalf("bob send: %v", err)
	}
	ack := readUntil(t, bob, func(m NexusMessage) bool { return m.Type == "msg_status" })
	if ack.Body != "delivered_offline" {
		t.Fatalf("expected delivered_offline ack, got %q", ack.Body)
	}

	// alice must NOT have received the msg. Short deadline = clean miss.
	// Note: once a gorilla/websocket read errors (including deadline), the
	// connection's read side is poisoned. We reconnect alice below.
	alice.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var leaked NexusMessage
	if err := alice.ReadJSON(&leaked); err == nil && leaked.Type == "msg" {
		t.Fatalf("blocked message leaked to alice: %+v", leaked)
	}
	alice.Close()

	// And nothing should have been queued — block path skips storeOfflineMessage.
	var queued int
	if err := srv.DB.QueryRow(
		"SELECT COUNT(*) FROM offline_messages WHERE recipient = ? AND sender = ?",
		"alice", "bob").Scan(&queued); err != nil {
		t.Fatalf("count offline_messages: %v", err)
	}
	if queued != 0 {
		t.Fatalf("blocked msg leaked into offline queue: %d rows", queued)
	}

	// Reconnect alice on a fresh socket, then unblock and verify delivery.
	alice = dial(t, wsBase)
	auth(t, alice, "alice", "password123")
	if err := alice.WriteJSON(NexusMessage{Type: "unblock", Recipient: "bob"}); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	res = readUntil(t, alice, func(m NexusMessage) bool { return m.Type == "block_result" })
	if res.Status != "unblocked" {
		t.Fatalf("unblock failed: %q", res.Error)
	}
	if err := bob.WriteJSON(NexusMessage{Type: "msg", Recipient: "alice", Body: "after-unblock"}); err != nil {
		t.Fatalf("bob send 2: %v", err)
	}
	got := readUntil(t, alice, func(m NexusMessage) bool {
		return m.Type == "msg" && m.Body == "after-unblock"
	})
	if got.Sender != "bob" {
		t.Fatalf("wrong sender after unblock: %q", got.Sender)
	}
}

// TestSmoke_AbuseReportPersisted confirms a report lands in the abuse_reports
// table with the correct fields.
func TestSmoke_AbuseReportPersisted(t *testing.T) {
	srv, _, wsBase := newTestServer(t)

	registerAndVerify(t, srv, "alice", "password123")
	registerAndVerify(t, srv, "bob", "password123")

	alice := dial(t, wsBase)
	auth(t, alice, "alice", "password123")
	time.Sleep(50 * time.Millisecond)

	if err := alice.WriteJSON(NexusMessage{
		Type: "report_abuse", Recipient: "bob", Status: "spam", Body: "sent me 50 invites",
	}); err != nil {
		t.Fatalf("send report: %v", err)
	}
	res := readUntil(t, alice, func(m NexusMessage) bool { return m.Type == "report_result" })
	if res.Status != "received" {
		t.Fatalf("report rejected: %q", res.Error)
	}

	var reporter, subject, reason, body string
	err := srv.DB.QueryRow(
		"SELECT reporter, subject, reason, body FROM abuse_reports WHERE reporter = ? AND subject = ?",
		"alice", "bob").Scan(&reporter, &subject, &reason, &body)
	if err != nil {
		t.Fatalf("report not persisted: %v", err)
	}
	if reason != "spam" || body != "sent me 50 invites" {
		t.Fatalf("report fields wrong: reason=%q body=%q", reason, body)
	}
}

// TestSmoke_OfflineDelivery confirms that messages sent to an offline user
// are queued and replayed on next auth.
func TestSmoke_OfflineDelivery(t *testing.T) {
	srv, _, wsBase := newTestServer(t)

	registerAndVerify(t, srv, "alice", "password123")
	registerAndVerify(t, srv, "bob", "password123")

	alice := dial(t, wsBase)
	auth(t, alice, "alice", "password123")

	// bob is offline; alice's message should queue.
	if err := alice.WriteJSON(NexusMessage{
		Type: "msg", Recipient: "bob", Body: "queued-while-offline",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	ack := readUntil(t, alice, func(m NexusMessage) bool { return m.Type == "msg_status" })
	if ack.Body != "delivered_offline" {
		t.Fatalf("expected delivered_offline, got %q (err=%q)", ack.Body, ack.Error)
	}

	// bob comes online and should receive the queued message.
	bob := dial(t, wsBase)
	auth(t, bob, "bob", "password123")

	got := readUntil(t, bob, func(m NexusMessage) bool {
		return m.Type == "msg" && m.Body == "queued-while-offline"
	})
	if got.Sender != "alice" {
		t.Fatalf("queued msg sender wrong: %q", got.Sender)
	}
}

// TestSmoke_KeyRequestRelay confirms friends can exchange key_request through
// the relay (required for NaCl box handoff between desktop and web).
func TestSmoke_KeyRequestRelay(t *testing.T) {
	srv, _, wsBase := newTestServer(t)

	registerAndVerify(t, srv, "alice", "password123")
	registerAndVerify(t, srv, "bob", "password123")
	// Mutual accepted friendship (minimal DB seed).
	if _, err := srv.DB.Exec(
		`INSERT INTO friends (user_a, user_b, status) VALUES ('alice', 'bob', 'accepted')`,
	); err != nil {
		t.Fatalf("seed friends: %v", err)
	}

	alice := dial(t, wsBase)
	bob := dial(t, wsBase)
	auth(t, alice, "alice", "password123")
	auth(t, bob, "bob", "password123")
	time.Sleep(100 * time.Millisecond)

	if err := bob.WriteJSON(NexusMessage{
		Type: "key_request", Recipient: "alice",
	}); err != nil {
		t.Fatalf("bob key_request: %v", err)
	}
	got := readUntil(t, alice, func(m NexusMessage) bool {
		return m.Type == "key_request" && m.Sender == "bob"
	})
	if got.Recipient != "alice" {
		t.Fatalf("key_request recipient wrong: %+v", got)
	}
}

// TestSmoke_PresencePublicKeyForward confirms a directed presence with a
// 32-byte public_key reaches the recipient friend (NaCl key handoff).
func TestSmoke_PresencePublicKeyForward(t *testing.T) {
	srv, _, wsBase := newTestServer(t)

	registerAndVerify(t, srv, "alice", "password123")
	registerAndVerify(t, srv, "bob", "password123")
	if _, err := srv.DB.Exec(
		`INSERT INTO friends (user_a, user_b, status) VALUES ('alice', 'bob', 'accepted')`,
	); err != nil {
		t.Fatalf("seed friends: %v", err)
	}

	alice := dial(t, wsBase)
	bob := dial(t, wsBase)
	auth(t, alice, "alice", "password123")
	auth(t, bob, "bob", "password123")
	time.Sleep(100 * time.Millisecond)

	pk := make([]byte, 32)
	for i := range pk {
		pk[i] = byte(i + 1)
	}
	if err := alice.WriteJSON(NexusMessage{
		Type: "presence", Recipient: "bob", Status: "Online",
		PublicKey: pk, KeyFingerprint: "deadbeefcafebabe",
	}); err != nil {
		t.Fatalf("alice presence: %v", err)
	}
	got := readUntil(t, bob, func(m NexusMessage) bool {
		return m.Type == "presence" && m.Sender == "alice" && len(m.PublicKey) == 32
	})
	if got.KeyFingerprint != "deadbeefcafebabe" {
		t.Fatalf("fingerprint lost: %+v", got)
	}
}

// TestSmoke_DeleteAccount confirms the GDPR erasure path nukes the user and
// cascades to friends + offline_messages + sessions. Reports BY the user are
// removed; reports ABOUT them are retained.
func TestSmoke_DeleteAccount(t *testing.T) {
	srv, _, wsBase := newTestServer(t)

	registerAndVerify(t, srv, "alice", "password123")
	registerAndVerify(t, srv, "bob", "password123")
	if _, err := srv.DB.Exec(
		`INSERT INTO friends (user_a, user_b, status) VALUES ('alice', 'bob', 'accepted')`,
	); err != nil {
		t.Fatalf("seed friends: %v", err)
	}
	if _, err := srv.DB.Exec(
		`INSERT INTO offline_messages (sender, recipient, body) VALUES ('alice', 'bob', 'hello')`,
	); err != nil {
		t.Fatalf("seed offline_messages: %v", err)
	}
	if _, err := srv.DB.Exec(
		`INSERT INTO abuse_reports (reporter, subject, reason) VALUES ('alice', 'carol', 'spam'), ('carol', 'alice', 'rude')`,
	); err != nil {
		t.Fatalf("seed abuse_reports: %v", err)
	}

	alice := dial(t, wsBase)
	auth(t, alice, "alice", "password123")

	// Wrong password is rejected without deleting the account.
	if err := alice.WriteJSON(NexusMessage{Type: "delete_account", Body: "wrong"}); err != nil {
		t.Fatalf("send delete (wrong): %v", err)
	}
	res := readUntil(t, alice, func(m NexusMessage) bool { return m.Type == "delete_account_result" })
	if res.Status == "ok" {
		t.Fatalf("wrong-password delete should not succeed: %+v", res)
	}

	// Confirm alice still exists.
	var n int
	srv.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'alice'`).Scan(&n)
	if n != 1 {
		t.Fatalf("alice should still exist after wrong password: count=%d", n)
	}

	// Correct password succeeds.
	if err := alice.WriteJSON(NexusMessage{Type: "delete_account", Body: "password123"}); err != nil {
		t.Fatalf("send delete (correct): %v", err)
	}
	res = readUntil(t, alice, func(m NexusMessage) bool { return m.Type == "delete_account_result" })
	if res.Status != "ok" {
		t.Fatalf("delete failed: %+v", res)
	}

	// users row gone.
	srv.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE username = 'alice'`).Scan(&n)
	if n != 0 {
		t.Fatalf("alice not erased: count=%d", n)
	}
	// friendship gone.
	srv.DB.QueryRow(`SELECT COUNT(*) FROM friends WHERE user_a = 'alice' OR user_b = 'alice'`).Scan(&n)
	if n != 0 {
		t.Fatalf("friend rows not erased: count=%d", n)
	}
	// offline messages gone.
	srv.DB.QueryRow(`SELECT COUNT(*) FROM offline_messages WHERE sender = 'alice' OR recipient = 'alice'`).Scan(&n)
	if n != 0 {
		t.Fatalf("offline_messages not erased: count=%d", n)
	}
	// reports BY alice gone.
	srv.DB.QueryRow(`SELECT COUNT(*) FROM abuse_reports WHERE reporter = 'alice'`).Scan(&n)
	if n != 0 {
		t.Fatalf("reports BY alice not erased: count=%d", n)
	}
	// reports ABOUT alice retained.
	srv.DB.QueryRow(`SELECT COUNT(*) FROM abuse_reports WHERE subject = 'alice'`).Scan(&n)
	if n != 1 {
		t.Fatalf("reports ABOUT alice should be retained: count=%d", n)
	}
}

// TestSmoke_AdminBanFlow verifies the ban path: admin promotes via env,
// REST endpoint flips is_banned, kicks the online client, and subsequent
// auth attempts are rejected with "Account suspended".
func TestSmoke_AdminBanFlow(t *testing.T) {
	srv, hs, wsBase := newTestServer(t)

	registerAndVerify(t, srv, "admin", "password123")
	registerAndVerify(t, srv, "spammer", "password123")
	if _, err := srv.DB.Exec(`UPDATE users SET is_admin = 1 WHERE username = 'admin'`); err != nil {
		t.Fatalf("promote admin: %v", err)
	}

	// 1) Spammer logs in successfully (control: unbanned baseline).
	c := dial(t, wsBase)
	auth(t, c, "spammer", "password123")
	c.Close()

	// 2) Admin logs in to mint a session token, which doubles as the bearer
	//    for the admin REST endpoints.
	adminC := dial(t, wsBase)
	if err := adminC.WriteJSON(NexusMessage{Type: "auth", Sender: "admin", Body: "password123"}); err != nil {
		t.Fatalf("admin auth send: %v", err)
	}
	res := readUntil(t, adminC, func(m NexusMessage) bool { return m.Type == "auth_result" })
	if res.Status != "ok" || res.QRToken == "" {
		t.Fatalf("admin auth: %+v", res)
	}
	adminToken := res.QRToken
	adminC.Close()

	// 3) POST /api/v1/admin/users/spammer/ban
	req, _ := http.NewRequest("POST", hs.URL+"/api/v1/admin/users/spammer/ban",
		strings.NewReader(`{"reason":"abuse: chain-spamming"}`))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ban POST: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ban POST status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 4) Spammer tries to re-auth — must be refused.
	c2 := dial(t, wsBase)
	if err := c2.WriteJSON(NexusMessage{Type: "auth", Sender: "spammer", Body: "password123"}); err != nil {
		t.Fatalf("spammer reauth: %v", err)
	}
	rej := readUntil(t, c2, func(m NexusMessage) bool { return m.Type == "auth_result" })
	if rej.Status != "banned" {
		t.Fatalf("ban not enforced: %+v", rej)
	}
	if !strings.Contains(rej.Error, "abuse: chain-spamming") {
		t.Fatalf("ban reason missing: %q", rej.Error)
	}
	c2.Close()

	// 5) Unauthorised caller — no admin token — gets 401.
	req, _ = http.NewRequest("GET", hs.URL+"/api/v1/admin/reports", nil)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-auth admin call status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 6) Non-admin authenticated caller — 403.
	regC := dial(t, wsBase)
	registerAndVerify(t, srv, "regular", "password123")
	if err := regC.WriteJSON(NexusMessage{Type: "auth", Sender: "regular", Body: "password123"}); err != nil {
		t.Fatalf("regular auth: %v", err)
	}
	regRes := readUntil(t, regC, func(m NexusMessage) bool { return m.Type == "auth_result" })
	regToken := regRes.QRToken
	regC.Close()

	req, _ = http.NewRequest("GET", hs.URL+"/api/v1/admin/reports", nil)
	req.Header.Set("Authorization", "Bearer "+regToken)
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin call status %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHealth_JSON(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.healthHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status: %v", body["status"])
	}
	if body["database_ok"] != true {
		t.Fatalf("database_ok: %v", body["database_ok"])
	}
}
