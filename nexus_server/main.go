package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
)

// NexusMessage is the wire protocol for Private Skype
type NexusMessage struct {
	Type      string   `json:"type"`
	Sender    string   `json:"sender"`
	Recipient string   `json:"recipient"`
	Body      string   `json:"body"`
	Status    string   `json:"status"`
	Results   []string `json:"results"`
	SDP       string   `json:"sdp"`
	Candidate string   `json:"candidate"`
	Token     string   `json:"token"`
	Error     string   `json:"error"`
	ConvoID   string   `json:"convo_id,omitempty"`
	ConvoName string   `json:"convo_name,omitempty"`
	Members   []string `json:"members,omitempty"`
}

type Client struct {
	Conn     *websocket.Conn
	Username string
	Status   string
}

type NexusServer struct {
	DB      *sql.DB
	Clients map[string]*Client
	Mu      sync.RWMutex
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ---------- Database Setup ----------

func (s *NexusServer) initDB() {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			salt TEXT NOT NULL,
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
	}
	for _, q := range tables {
		if _, err := s.DB.Exec(q); err != nil {
			log.Fatalf("DB init error: %v", err)
		}
	}
}

// ---------- Account Management (bcrypt) ----------

func (s *NexusServer) registerUser(username, password string) error {
	if len(password) < 4 {
		return errShortPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec("INSERT INTO users (username, password_hash, salt) VALUES (?, ?, '')",
		username, string(hash))
	return err
}

func (s *NexusServer) authenticateUser(username, password string) bool {
	var hash string
	err := s.DB.QueryRow("SELECT password_hash FROM users WHERE username = ?", username).Scan(&hash)
	if err != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

var errShortPassword = &passErr{}

type passErr struct{}

func (*passErr) Error() string { return "password too short" }

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
	s.DB.Exec("INSERT INTO offline_messages (sender, recipient, body, msg_type) VALUES (?, ?, ?, ?)",
		sender, recipient, body, msgType)
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
		client.Conn.WriteJSON(NexusMessage{
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
			client.Conn.WriteJSON(NexusMessage{
				Type:   "presence",
				Sender: username,
				Status: status,
			})
		}
	}
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
			break
		}

		switch msg.Type {

		case "register":
			err := s.registerUser(msg.Sender, msg.Body)
			if err != nil {
				ws.WriteJSON(NexusMessage{Type: "register_result", Error: "Username already taken"})
			} else {
				log.Printf("New user registered: %s", msg.Sender)
				ws.WriteJSON(NexusMessage{Type: "register_result", Status: "ok"})
			}

		case "auth":
			if msg.Body == "" {
				ws.WriteJSON(NexusMessage{Type: "auth_result", Error: "Password required"})
				continue
			}
			if !s.authenticateUser(msg.Sender, msg.Body) {
				ws.WriteJSON(NexusMessage{Type: "auth_result", Error: "Invalid username or password"})
				continue
			}
			username = msg.Sender
			s.Mu.Lock()
			// Kick existing session if any
			if existing, ok := s.Clients[username]; ok {
				existing.Conn.WriteJSON(NexusMessage{Type: "kicked", Body: "Logged in from another location"})
				existing.Conn.Close()
			}
			s.Clients[username] = &Client{Conn: ws, Username: username, Status: "Online"}
			s.Mu.Unlock()
			log.Printf("User %s authenticated", username)

			ws.WriteJSON(NexusMessage{Type: "auth_result", Status: "ok", Sender: username})

			// Broadcast online presence to friends
			s.broadcastPresence(username, "Online")

			// Deliver any offline messages
			s.deliverOfflineMessages(username)

			// Send pending friend requests
			pending := s.getPendingRequests(username)
			if len(pending) > 0 {
				ws.WriteJSON(NexusMessage{Type: "pending_requests", Results: pending})
			}

			// Send conversations this user belongs to
			for _, cm := range s.userConversations(username) {
				cm.Type = "convo_info"
				ws.WriteJSON(cm)
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
				ws.WriteJSON(NexusMessage{Type: "friend_status", Sender: f, Status: status})
			}

		case "msg":
			if username == "" {
				continue
			}
			log.Printf("Message from %s to %s", msg.Sender, msg.Recipient)
			s.Mu.RLock()
			recipientClient, online := s.Clients[msg.Recipient]
			s.Mu.RUnlock()

			if online {
				recipientClient.Conn.WriteJSON(msg)
			} else {
				// Store for offline delivery
				s.storeOfflineMessage(msg.Sender, msg.Recipient, msg.Body, "msg")
				ws.WriteJSON(NexusMessage{
					Type:   "msg_status",
					Body:   "delivered_offline",
					Sender: msg.Recipient,
				})
			}

		case "typing":
			if username == "" {
				continue
			}
			s.Mu.RLock()
			if recipientClient, ok := s.Clients[msg.Recipient]; ok {
				recipientClient.Conn.WriteJSON(NexusMessage{
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
			ws.WriteJSON(NexusMessage{
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
				recipientClient.Conn.WriteJSON(NexusMessage{
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
				requesterClient.Conn.WriteJSON(NexusMessage{
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
				peer.Conn.WriteJSON(NexusMessage{Type: "friend_removed", Sender: username})
			}
			s.Mu.RUnlock()

		case "convo_create":
			if username == "" {
				continue
			}
			if err := s.createConversation(msg.ConvoID, msg.ConvoName, username, msg.Members); err != nil {
				ws.WriteJSON(NexusMessage{Type: "convo_error", Error: err.Error()})
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
					c.Conn.WriteJSON(notice)
				}
			}
			s.Mu.RUnlock()
			log.Printf("Conversation %s (%s) created by %s with %d members", msg.ConvoID, msg.ConvoName, username, len(members))

		case "convo_msg":
			if username == "" || msg.ConvoID == "" {
				continue
			}
			members := s.conversationMembers(msg.ConvoID)
			fanout := NexusMessage{
				Type:    "convo_msg",
				Sender:  username,
				Body:    msg.Body,
				ConvoID: msg.ConvoID,
			}
			s.Mu.RLock()
			for _, m := range members {
				if m == username {
					continue
				}
				if c, ok := s.Clients[m]; ok {
					c.Conn.WriteJSON(fanout)
				} else {
					s.DB.Exec(`INSERT INTO offline_messages (sender, recipient, body, msg_type, convo)
						VALUES (?, ?, ?, 'convo_msg', ?)`, username, m, msg.Body, msg.ConvoID)
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
					c.Conn.WriteJSON(NexusMessage{
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
				peer.Conn.WriteJSON(NexusMessage{
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
				recipientClient.Conn.WriteJSON(msg)
			} else {
				ws.WriteJSON(NexusMessage{
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
				recipientClient.Conn.WriteJSON(msg)
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
	w.Write([]byte(`{"status":"ok","server":"skype-nexus","version":"1.0.0"}`))
}

const rootHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>Skype 7 Reborn — Nexus Server</title>
<style>
body{font-family:"Segoe UI",Tahoma,sans-serif;background:linear-gradient(180deg,#B3E5FC,#f5f7fa 240px);color:#333;margin:0;padding:60px 24px;text-align:center}
h1{color:#005F8F;font-weight:300;font-size:48px;margin:0 0 8px}
p{color:#666;font-size:17px;max-width:560px;margin:0 auto 12px}
code{background:#fff;border:1px solid #d8dee5;padding:2px 6px;border-radius:3px;font-family:Consolas,monospace}
a{color:#005F8F}
.box{background:#fff;border:1px solid #d8dee5;border-radius:6px;padding:24px;max-width:560px;margin:32px auto;text-align:left}
.ok{color:#2e7d32;font-weight:600}
</style></head><body>
<h1>Skype 7 Reborn</h1>
<p>This is a <strong>Nexus relay server</strong>, not a website. There's nothing for you to click here.</p>
<div class="box">
<p><span class="ok">&#10003;</span> Server is online and accepting connections.</p>
<p>Download the desktop client at <a href="https://jakes1345.github.io/skype7-reborn/">jakes1345.github.io/skype7-reborn</a>.</p>
<p>Source code: <a href="https://github.com/jakes1345/skype7-reborn">github.com/jakes1345/skype7-reborn</a>.</p>
<p>WebSocket endpoint: <code>wss://{{HOST}}/cable</code> &nbsp;&middot;&nbsp; Health: <a href="/health">/health</a></p>
</div>
</body></html>`

func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	host := r.Host
	if host == "" {
		host = "skype7-reborn.fly.dev"
	}
	w.Write([]byte(strings.ReplaceAll(rootHTML, "{{HOST}}", host)))
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

	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/cable", server.handleConnections)
	http.HandleFunc("/health", healthHandler)

	bindAddr := os.Getenv("BIND_ADDR")
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	log.Printf("Skype Nexus Server v1.0.0 starting on %s:%s...", bindAddr, port)
	log.Printf("  WebSocket endpoint: ws://%s:%s/cable", bindAddr, port)
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
