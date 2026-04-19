package main

import (
	"database/sql"
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
func readUntil(t *testing.T, c *websocket.Conn, want func(NexusMessage) bool) NexusMessage {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
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
