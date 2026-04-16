package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
	"golang.org/x/crypto/bcrypt"
	"net/url"
)

// NexusMessage is the wire protocol for TAZHER™
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
	Email       string   `json:"email,omitempty"`
	Mood        string   `json:"mood,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	ConvoID     string   `json:"convo_id,omitempty"`
	ConvoName string   `json:"convo_name,omitempty"`
	Members   []string `json:"members,omitempty"`
	TurnConfig  *TurnConfig `json:"turn_config,omitempty"`
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
}

type NexusServer struct {
	DB      *sql.DB
	Clients map[string]*Client
	Mu      sync.RWMutex
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ---------- Sovereign Media Configuration ----------

var (
	TurnSecret = os.Getenv("TAZHER_TURN_SECRET") // Shared secret with CoTURN
	TurnURL    = os.Getenv("TAZHER_TURN_URL")    // e.g. "turn:turn.tazher.com:3478"
)

func (s *NexusServer) generateMediaToken(username string) *TurnConfig {
	if TurnSecret == "" || TurnURL == "" {
		return nil
	}
	
	// CoTURN Dynamic Credential Algorithm (timestamp:username)
	timestamp := time.Now().Add(24 * time.Hour).Unix()
	user := fmt.Sprintf("%d:%s", timestamp, username)
	
	// Pass = HMAC-SHA1(secret, user)
	mac := hmac.New(sha1.New, []byte(TurnSecret))
	mac.Write([]byte(user))
	password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

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
	}
	for _, q := range tables {
		if _, err := s.DB.Exec(q); err != nil {
			log.Fatalf("DB init error: %v", err)
		}
	}
}

// ---------- Account Management (bcrypt) ----------

func (s *NexusServer) registerUser(username, email, mood, password string) (string, error) {
	if len(password) < 4 {
		return "", errShortPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	
	code := fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	
	_, err = s.DB.Exec("INSERT INTO users (username, email, mood, password_hash, salt, verification_code) VALUES (?, ?, ?, ?, '', ?)",
		username, email, mood, string(hash), code)
	return code, err
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
			return
		}

		switch msg.Type {
		case "pstn_call":
			number := msg.Body
			// SECURITY CHECK: Verify this number belongs to this sender
			var verified int
			err := s.DB.QueryRow("SELECT phone_verified FROM users WHERE username = ? AND phone_number = ?", username, number).Scan(&verified)
			if err != nil || verified == 0 {
				ws.WriteJSON(NexusMessage{Type: "pstn_status", Error: "Caller identity not verified. Please link your phone in Settings."})
				continue
			}

			log.Printf("[PSTN-SECURE] User %s initiating call to %s", msg.Sender, number)
			err = s.initiateTwilioCall(number)
			if err != nil {
				ws.WriteJSON(NexusMessage{Type: "pstn_status", Error: "Telephony error: " + err.Error()})
			} else {
				ws.WriteJSON(NexusMessage{Type: "pstn_status", Status: "Connecting via Sovereign Bridge..."})
			}

		case "register":
			code, err := s.registerUser(msg.Sender, msg.Email, msg.Mood, msg.Body)
			if err != nil {
				ws.WriteJSON(NexusMessage{Type: "register_result", Error: "Username already taken or database error"})
			} else {
				log.Printf("New user registered: %s (%s) - Code: %s", msg.Sender, msg.Email, code)
				go s.sendEmail(msg.Email, "Activate your Tazher Identity", 
					"<h1>Welcome to Tazher</h1><p>Your activation code is: <b>"+code+"</b></p><p>Enter this in the app to start using the mesh.</p>")
				ws.WriteJSON(NexusMessage{Type: "register_result", Status: "pending_verification"})
			}

		case "verify_email":
			if s.verifyUser(msg.Sender, msg.Body) {
				ws.WriteJSON(NexusMessage{Type: "verify_result", Status: "ok"})
			} else {
				ws.WriteJSON(NexusMessage{Type: "verify_result", Error: "Invalid verification code"})
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
				ws.WriteJSON(NexusMessage{Type: "phone_link_result", Error: "Update failed"})
			} else {
				log.Printf("[SMS] Sending verification to %s: %s", number, code)
				go s.sendSMS(number, "Your TAZHER verification code is: "+code)
				ws.WriteJSON(NexusMessage{Type: "phone_link_result", Status: "code_sent"})
			}

		case "verify_phone_link":
			var dbCode string
			err := s.DB.QueryRow("SELECT verification_code FROM users WHERE username = ?", username).Scan(&dbCode)
			if err == nil && dbCode == msg.Body {
				s.DB.Exec("UPDATE users SET phone_verified = 1, verification_code = NULL WHERE username = ?", username)
				ws.WriteJSON(NexusMessage{Type: "phone_link_result", Status: "verified"})
			} else {
				s.DB.Exec("UPDATE users SET verification_code = NULL WHERE username = ?", username)
				ws.WriteJSON(NexusMessage{Type: "phone_link_result", Error: "Invalid code. Security lockout: please request a new code."})
			}

		case "update_profile":
			// Update mood and display name
			_, err := s.DB.Exec("UPDATE users SET mood = ?, display_name = ? WHERE username = ?", 
				msg.Mood, msg.DisplayName, msg.Sender)
			if err != nil {
				ws.WriteJSON(NexusMessage{Type: "update_result", Error: "Update failed"})
			} else {
				log.Printf("Profile updated for %s: %s | %s", msg.Sender, msg.DisplayName, msg.Mood)
				ws.WriteJSON(NexusMessage{Type: "update_result", Status: "ok"})
				// Broadcast this change to all online friends
				s.broadcastProfileUpdate(msg.Sender, msg.DisplayName, msg.Mood)
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

			ws.WriteJSON(NexusMessage{
				Type:       "auth_result",
				Status:     "ok",
				Sender:     username,
				TurnConfig: s.generateMediaToken(username),
			})

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
			if msg.Recipient == "TazherBot" {
				s.handleBotMessage(ws, msg)
				continue
			}
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
<html><head><meta charset="utf-8"><title>Tazher — Nexus Server</title>
<style>
	body { background: #1a1a2e; color: #fff; font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; display: flex; flex-direction: column; align-items: center; justify-content: center; height: 100vh; margin: 0; }
	h1 { color: #00aff0; font-size: 3rem; margin-bottom: 0.5rem; }
	p { color: #888; margin-top: 0; }
	.card { background: rgba(255,255,255,0.05); padding: 2rem; border-radius: 12px; border: 1px solid rgba(0,175,240,0.3); text-align: center; max-width: 500px; }
	.btn { display: inline-block; background: #00aff0; color: #fff; text-decoration: none; padding: 12px 24px; border-radius: 6px; font-weight: bold; margin-top: 1rem; transition: background 0.2s; }
	.btn:hover { background: #008cc0; }
	code { background: #000; padding: 4px 8px; border-radius: 4px; color: #00ff00; }
</style></head><body>
	<h1>TAZHER</h1>
	<p><i>Don't stop til you've had enough.</i></p>
	<div class="card">
		<h3>Nexus Relay v1.0.0</h3>
		<p>This is the official TAZHER Hub. Your client uses this node to find friends and synchronize your sovereign mesh.</p>
		<code>Status: ONLINE</code>
		<br><br>
		<a href="https://github.com/jakes1345/skype7-reborn/releases" class="btn">Download Desktop Client</a>
	</div>
</body></html>`

func (s *NexusServer) landingHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "templates/landing.html")
}

func (s *NexusServer) versionHandler(w http.ResponseWriter, r *http.Request) {
	v := os.Getenv("TAZHER_LATEST_VERSION")
	if v == "" {
		v = "1.0.0-TAZHER"
	}
	u := os.Getenv("TAZHER_UPDATE_URL")
	if u == "" {
		u = "https://github.com/jakes1345/skype7-reborn/releases"
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"version": v,
		"url":     u,
	})
}

func (s *NexusServer) statsHandler(w http.ResponseWriter, r *http.Request) {
	s.Mu.RLock()
	count := len(s.Clients)
	s.Mu.RUnlock()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"active_nodes": count,
		"timestamp":    time.Now().Unix(),
		"status":       "online",
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

	http.HandleFunc("/ws", server.handleConnections)
	http.HandleFunc("/api/v1/version", server.versionHandler)
	http.HandleFunc("/api/v1/profile/", server.profileHandler)
	http.HandleFunc("/api/v1/avatars/", server.avatarHandler)
	http.HandleFunc("/twiml/outbound", server.twimlHandler)
	
	fs := http.FileServer(http.Dir("public"))
	http.Handle("/public/", http.StripPrefix("/public/", fs))
	
	http.HandleFunc("/api/v1/stats", server.statsHandler)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	http.HandleFunc("/", server.landingHandler)
	http.HandleFunc("/version", server.versionHandler)

	bindAddr := os.Getenv("BIND_ADDR")
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	log.Printf("Tazher Nexus Server v1.0.0 starting on %s:%s...", bindAddr, port)
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
			client.Conn.WriteJSON(msg)
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

func (s *NexusServer) handleBotMessage(ws *websocket.Conn, msg NexusMessage) {
	reply := NexusMessage{
		Type:      "msg",
		Sender:    "TazherBot",
		Recipient: msg.Sender,
		Body:      "I am the Tazher Mesh Assistant. Try these commands: /mesh, /version, /pstn",
	}

	cmd := strings.ToLower(strings.TrimSpace(msg.Body))
	switch {
	case cmd == "/mesh":
		s.Mu.RLock()
		count := len(s.Clients)
		s.Mu.RUnlock()
		reply.Body = fmt.Sprintf("The TAZHER Mesh currently has %d active sovereign peers.", count)
	case cmd == "/version":
		reply.Body = "Nexus Server v1.0.0-Tazher | Build: Enterprise-Mesh"
	case cmd == "/pstn":
		reply.Body = "PSTN Bridge is ACTIVE. Link your phone in Settings to use Caller ID."
	}
	ws.WriteJSON(reply)
}

func (s *NexusServer) twimlHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/xml")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response><Dial><Conference>TAZHER_MESH_BRIDGE</Conference></Dial></Response>`))
}

func (s *NexusServer) initiateTwilioCall(to string) error {
	sid := os.Getenv("TWILIO_SID")
	token := os.Getenv("TWILIO_TOKEN")
	from := os.Getenv("TWILIO_FROM")
	appURL := os.Getenv("TAZHER_APP_URL")

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
