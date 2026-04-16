package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/zalando/go-keyring"
	_ "modernc.org/sqlite"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"tazher-native/internal/chat"
	"tazher-native/internal/p2p"
	"tazher-native/internal/ui"
	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
)

func humanSize(n int) string {
	const k = 1024
	if n < k {
		return fmt.Sprintf("%d B", n)
	}
	if n < k*k {
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	}
	if n < k*k*k {
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(k*k*k))
}

const (
	Version        = "1.0.0-Tazher"
	keyringService = "tazher-native"
)

// NexusMessage matches the Nexus server protocol
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
}

// TazherApp holds all application state
type TazherApp struct {
	App        fyne.App
	MainWindow  fyne.Window

	// Windows
	ChatWindows map[string]fyne.Window
	CallWindows map[string]fyne.Window

	// Database
	DB *sql.DB

	// Settings
	CompactMode   bool
	ServerAddress string
	SoundEnabled  bool

	// Network
	Username string
	Conn     *websocket.Conn
	ConnMu   sync.Mutex
	authChan chan bool

	// UI State
	ChatLogs         map[string]*fyne.Container
	ChatTypingLabels map[string]*widget.Label
	SearchResult     *widget.List
	ContactList      *widget.List
	Discovered       []string
	Friends          []ui.FriendInfo
	PendingInbound   []string
	AvatarPath       string
	TypingTimers     map[string]*time.Timer

	// Notifications
	UnreadCounts map[string]int

	Calls *chat.CallManager

	Slicer *ui.AeroSlicer

	P2PNode      *p2p.TazherNode
	Sidebar      fyne.CanvasObject
	HomeView     fyne.CanvasObject
	ContentStack *fyne.Container
}

// Friends are stored as ui.FriendInfo to share with the sidebar


func NewTazherApp() *TazherApp {
	a := app.New()

	home, _ := os.UserHomeDir()
	dbDir := filepath.Join(home, ".private_tazher")
	os.MkdirAll(dbDir, 0755)
	dbPath := filepath.Join(dbDir, "main.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	// Create Tazher 7 Compatible Schema
	db.Exec(`CREATE TABLE IF NOT EXISTS Conversations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		identity TEXT UNIQUE,
		displayname TEXT,
		last_message_id INTEGER,
		creation_timestamp INTEGER,
		type INTEGER
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS Messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		convo_id INTEGER,
		chatname TEXT,
		author TEXT,
		from_dispname TEXT,
		body_xml TEXT,
		timestamp INTEGER,
		type INTEGER,
		guid BLOB
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS Contacts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tazhername TEXT UNIQUE,
		fullname TEXT,
		displayname TEXT,
		avatar_image BLOB,
		avatar_path TEXT,
		status TEXT DEFAULT 'Offline',
		mood_text TEXT
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS Transfers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type INTEGER,
		partner_handle TEXT,
		partner_dispname TEXT,
		status INTEGER,
		filename TEXT,
		filepath TEXT,
		filesize INTEGER,
		bytestransferred INTEGER,
		convo_id INTEGER
	)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS Profile (
		key TEXT PRIMARY KEY,
		value TEXT
	)`)

	s := &TazherApp{
		App:              a,
		ChatWindows:      make(map[string]fyne.Window),
		CallWindows:      make(map[string]fyne.Window),
		DB:               db,
		CompactMode:      false,
		SoundEnabled:     true,
		ChatLogs:         make(map[string]*fyne.Container),
		ChatTypingLabels: make(map[string]*widget.Label),
		TypingTimers:     make(map[string]*time.Timer),
		UnreadCounts:     make(map[string]int),
		Calls:            chat.NewCallManager(),
	}

	// Initialize Slicer with the master spritesheet
	slicer, err := ui.NewAeroSlicer("assets/ui_master_spritesheet.png")
	if err == nil {
		s.Slicer = slicer
	} else {
		log.Printf("Failed to load spritesheet: %v", err)
	}

	s.Calls.OnFile = func(peerName string, fileName string, totalSize int, data []byte) {
		home, _ := os.UserHomeDir()
		downloadPath := filepath.Join(home, "Downloads", fileName)
		if err := os.WriteFile(downloadPath, data, 0644); err != nil {
			log.Printf("save file: %v", err)
			return
		}

		// Persist the transfer record (Tazher-7-compatible schema)
		s.DB.Exec(`INSERT INTO Transfers (type, partner_handle, partner_dispname, status, filename, filepath, filesize, bytestransferred)
			VALUES (2, ?, ?, 8, ?, ?, ?, ?)`,
			peerName, peerName, fileName, downloadPath, totalSize, len(data))

		// Also add to message history so it survives restart
		ts := time.Now().Unix()
		label := "[File Received: " + fileName + "]"
		s.DB.Exec(`INSERT INTO Messages (chatname, author, body_xml, timestamp, type)
			VALUES (?, ?, ?, ?, 68)`, peerName, peerName, label, ts)

		s.App.SendNotification(fyne.NewNotification(
			"File received from "+peerName,
			fileName+" ("+humanSize(totalSize)+") saved to Downloads",
		))
		if logContainer, ok := s.ChatLogs[peerName]; ok {
			logContainer.Add(ui.NewMessageBubble(peerName, label, false))
			logContainer.Refresh()
		}
	}

	// Initialize P2P DHT Node
	p2pCtx := context.Background()
	p2pNode, err := p2p.NewTazherNode(p2pCtx, 0) // Random port
	if err == nil {
		s.P2PNode = p2pNode
		// Use standard libp2p bootstrap nodes for now
		go s.P2PNode.Bootstrap(p2p.DefaultBootstrapNodes)
		
		// Setup handler to process incoming P2P signaling (NexusMessages)
		s.P2PNode.SetupSignalingHandler(func(raw interface{}) {
			// Convert back to NexusMessage
			data, _ := json.Marshal(raw)
			var msg NexusMessage
			json.Unmarshal(data, &msg)
			
			// Process as if it came from the server
			s.HandleIncomingMessage(msg)
		})
	} else {
		log.Printf("Failed to start P2P node: %v", err)
	}

	return s
}

// ---------- Network ----------

func (s *TazherApp) ConnectToServer(password string) error {
	s.ConnMu.Lock()
	defer s.ConnMu.Unlock()

	if s.Conn != nil {
		s.Conn.Close()
	}

	// Single Mesh discovery: Dial Global and Local in parallel
	targets := []string{
		"ws://localhost:8080/cable",
		"wss://tazher7-reborn.fly.dev/cable",
	}
	
	// If user manually set a different server, prioritize it
	if s.ServerAddress != "" && !strings.Contains(s.ServerAddress, "localhost") && !strings.Contains(s.ServerAddress, "fly.dev") {
		targets = append([]string{s.ServerAddress}, targets...)
	}

	var c *websocket.Conn
	var err error
	for _, addr := range targets {
		log.Printf("[Mesh] Attempting connection to %s...", addr)
		c, _, err = websocket.DefaultDialer.Dial(addr, nil)
		if err == nil {
			s.ServerAddress = addr
			log.Printf("[Mesh] Connected via %s", addr)
			break
		}
	}

	if err != nil {
		return fmt.Errorf("could not reach any Tazher Nexus: %w", err)
	}
	s.Conn = c

	// Setup auth channel for handshake
	s.authChan = make(chan bool, 1)

	auth := NexusMessage{Type: "auth", Sender: s.Username, Body: password}
	s.Conn.WriteJSON(auth)

	go s.ReadLoop()

	// Wait for auth_result (timeout 5s)
	// We unlock briefly so ReadLoop can process the result if it comes fast
	s.ConnMu.Unlock()
	defer s.ConnMu.Lock()

	select {
	case success := <-s.authChan:
		if !success {
			return fmt.Errorf("invalid username or password")
		}
		s.PlaySound("Login.wav")
		
		// Announce on DHT
		if s.P2PNode != nil {
			go s.P2PNode.Announce(s.Username)
		}
		
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("authentication timeout")
	}
}

func (s *TazherApp) SendMessage(msg NexusMessage) {
	s.ConnMu.Lock()
	conn := s.Conn
	s.ConnMu.Unlock()

	sent := false
	if conn != nil {
		if err := conn.WriteJSON(msg); err == nil {
			sent = true
		}
	}

	if !sent && s.P2PNode != nil && msg.Recipient != "" {
		log.Printf("[P2P] Nexus down or send failed; attempting direct signaling to %s", msg.Recipient)
		go func() {
			if err := s.P2PNode.SendSignaling(msg.Recipient, msg); err != nil {
				log.Printf("[P2P] Failed to send to %s: %v", msg.Recipient, err)
			} else {
				log.Printf("[P2P] Successfully delivered message to %s", msg.Recipient)
			}
		}()
	}
}

func (s *TazherApp) ReadLoop() {
	for {
		var msg NexusMessage
		err := s.Conn.ReadJSON(&msg)
		if err != nil {
			log.Println("Connection lost:", err)
			// Attempt reconnect
			go func() {
				for i := 0; i < 10; i++ {
					time.Sleep(time.Duration(2<<uint(i)) * time.Second)
					if i > 5 {
						time.Sleep(30 * time.Second)
					}
					log.Printf("Reconnect attempt %d...", i+1)
					pass, _ := keyring.Get(keyringService, s.Username)
					if err := s.ConnectToServer(pass); err == nil {
						log.Println("Reconnected!")
						return
					}
				}
				log.Println("Failed to reconnect after 10 attempts")
			}()
			return
		}

		s.HandleIncomingMessage(msg)
	}
}

func (s *TazherApp) HandleIncomingMessage(msg NexusMessage) {
	switch msg.Type {
	case "auth_result":
		if msg.Error != "" || msg.Status == "failed" {
			log.Println("Auth failed:", msg.Error)
			select {
			case s.authChan <- false:
			default:
			}
		} else {
			log.Println("Authenticated as", msg.Sender)
			select {
			case s.authChan <- true:
			default:
			}
		}

	case "msg":
		s.PlaySound("MessageReceived.wav")
		ts := time.Now().Unix()
		s.DB.Exec("INSERT INTO Messages (chatname, author, body_xml, timestamp, type) VALUES (?, ?, ?, ?, 61)",
			msg.Sender, msg.Sender, msg.Body, ts)

		if logContainer, ok := s.ChatLogs[msg.Sender]; ok {
			logContainer.Add(ui.NewMessageBubble(msg.Sender, msg.Body, false))
			logContainer.Refresh()
		} else {
			s.UnreadCounts[msg.Sender]++
			s.App.SendNotification(fyne.NewNotification(
				"Message from "+msg.Sender,
				msg.Body,
			))
		}

	case "typing":
		if lbl, ok := s.ChatTypingLabels[msg.Sender]; ok {
			lbl.SetText(msg.Sender + " is typing...")
			lbl.Show()
			if timer, tok := s.TypingTimers[msg.Sender]; tok {
				timer.Stop()
			}
			s.TypingTimers[msg.Sender] = time.AfterFunc(3*time.Second, func() {
				lbl.Hide()
			})
		}

	case "search_results":
		s.Discovered = msg.Results
		if s.SearchResult != nil {
			s.SearchResult.Refresh()
		}

	case "presence", "friend_status":
		s.updateFriendStatus(msg.Sender, msg.Status)

	case "friend_request":
		s.PlaySound("MessageReceived.wav")
		s.PendingInbound = append(s.PendingInbound, msg.Sender)
		s.App.SendNotification(fyne.NewNotification(
			"Friend Request",
			msg.Sender+" wants to add you as a contact",
		))
		if s.MainWindow != nil {
			s.showFriendRequestDialog(msg.Sender)
		}

	case "friend_accepted":
		s.PlaySound("MessageReceived.wav")
		s.DB.Exec("INSERT OR IGNORE INTO Contacts (tazhername, status) VALUES (?, 'Online')", msg.Sender)
		s.loadFriends()
		if s.ContactList != nil {
			s.ContactList.Refresh()
		}
		s.App.SendNotification(fyne.NewNotification(
			"Friend Added",
			msg.Sender+" accepted your friend request",
		))

	case "pending_requests":
		s.PendingInbound = msg.Results
		for _, requester := range msg.Results {
			s.showFriendRequestDialog(requester)
		}

	case "call_offer":
		s.PlaySound("CallIncoming.wav")
		s.showIncomingCallDialog(msg.Sender, msg.SDP)

	case "call_answer":
		log.Printf("Call answered by %s", msg.Sender)
		s.Calls.HandleAnswer(msg.Sender, msg.SDP)

	case "call_reject", "call_end":
		if win, ok := s.CallWindows[msg.Sender]; ok {
			win.Close()
			delete(s.CallWindows, msg.Sender)
		}
		s.Calls.EndCall(msg.Sender)

	case "call_error":
		s.App.SendNotification(fyne.NewNotification("Call Failed", msg.Error))

	case "ice_candidate":
		log.Printf("ICE candidate from %s", msg.Sender)
		s.Calls.AddICECandidate(msg.Sender, msg.Candidate)

	case "msg_status":
		if msg.Body == "delivered_offline" {
			log.Printf("Message to %s stored for offline delivery", msg.Sender)
		}

	case "kicked":
		s.App.SendNotification(fyne.NewNotification("Tazher", msg.Body))
		log.Println("Kicked:", msg.Body)

	case "friend_removed":
		s.DB.Exec("DELETE FROM Contacts WHERE tazhername = ?", msg.Sender)
		s.loadFriends()
		if s.ContactList != nil {
			s.ContactList.Refresh()
		}

	case "convo_info", "convo_created":
		s.DB.Exec(`INSERT OR REPLACE INTO Conversations (identity, displayname, type)
			VALUES (?, ?, 2)`, msg.ConvoID, msg.ConvoName)
		if msg.Type == "convo_created" {
			s.App.SendNotification(fyne.NewNotification(
				"New Group Chat",
				msg.Sender+" added you to "+msg.ConvoName,
			))
		}

	case "convo_msg":
		s.PlaySound("MessageReceived.wav")
		ts := time.Now().Unix()
		s.DB.Exec(`INSERT INTO Messages (chatname, author, body_xml, timestamp, type)
			VALUES (?, ?, ?, ?, 61)`, msg.ConvoID, msg.Sender, msg.Body, ts)
		if logContainer, ok := s.ChatLogs[msg.ConvoID]; ok {
			logContainer.Add(ui.NewMessageBubble(msg.Sender, msg.Body, false))
			logContainer.Refresh()
		} else {
			s.UnreadCounts[msg.ConvoID]++
			s.App.SendNotification(fyne.NewNotification(
				msg.Sender+" in group",
				msg.Body,
			))
		}

	case "convo_left":
		if logContainer, ok := s.ChatLogs[msg.ConvoID]; ok {
			logContainer.Add(ui.NewMessageBubble("system", msg.Sender+" left the conversation", false))
			logContainer.Refresh()
		}

	case "read_receipt":
		log.Printf("%s read our message %s", msg.Sender, msg.Body)

	case "register_result":
		if msg.Error != "" {
			log.Println("Registration failed:", msg.Error)
		} else {
			log.Println("Registration successful")
		}
	}
}

// ---------- Friend Management ----------

func (s *TazherApp) updateFriendStatus(username, status string) {
	s.DB.Exec("UPDATE Contacts SET status = ? WHERE tazhername = ?", status, username)
	for i, f := range s.Friends {
		if f.Username == username {
			s.Friends[i].Status = status
			break
		}
	}
	if s.ContactList != nil {
		s.ContactList.Refresh()
	}
}

func (s *TazherApp) loadFriends() {
	s.Friends = nil
	rows, err := s.DB.Query("SELECT tazhername, COALESCE(status, 'offline'), COALESCE(avatar_path, '') FROM Contacts ORDER BY status DESC, tazhername ASC")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var f ui.FriendInfo
		rows.Scan(&f.Username, &f.Status, &f.Avatar)
		s.Friends = append(s.Friends, f)
	}
}

func (s *TazherApp) showFriendRequestDialog(from string) {
	if s.MainWindow == nil {
		return
	}
	dialog.ShowConfirm("Friend Request",
		from+" wants to add you as a contact.\nAccept?",
		func(accept bool) {
			if accept {
				s.SendMessage(NexusMessage{Type: "friend_accept", Sender: from})
				s.DB.Exec("INSERT OR IGNORE INTO Contacts (tazhername, status) VALUES (?, 'Online')", from)
				s.loadFriends()
				if s.ContactList != nil {
					s.ContactList.Refresh()
				}
			} else {
				s.SendMessage(NexusMessage{Type: "friend_reject", Sender: from})
			}
			// Drop from pending list
			for i, p := range s.PendingInbound {
				if p == from {
					s.PendingInbound = append(s.PendingInbound[:i], s.PendingInbound[i+1:]...)
					break
				}
			}
		}, s.MainWindow)
}

func (s *TazherApp) removeContact(name string) {
	dialog.ShowConfirm("Remove Contact",
		"Remove "+name+" from your contacts?",
		func(ok bool) {
			if !ok {
				return
			}
			s.SendMessage(NexusMessage{
				Type:      "friend_remove",
				Sender:    s.Username,
				Recipient: name,
			})
			s.DB.Exec("DELETE FROM Contacts WHERE tazhername = ?", name)
			s.loadFriends()
			if s.ContactList != nil {
				s.ContactList.Refresh()
			}
		}, s.MainWindow)
}

// ---------- Sound ----------

func (s *TazherApp) PlaySound(name string) {
	if !s.SoundEnabled {
		return
	}
	go func() {
		soundPath := filepath.Join("assets", "sounds", name)
		f, err := os.Open(soundPath)
		if err != nil {
			return // Sound file missing, skip silently
		}
		defer f.Close()

		// Check file is not empty
		info, err := f.Stat()
		if err != nil || info.Size() == 0 {
			return // Empty placeholder, skip
		}

		streamer, format, err := wav.Decode(f)
		if err != nil {
			return
		}

		speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
		done := make(chan struct{})
		speaker.Play(beep.Seq(streamer, beep.Callback(func() {
			streamer.Close()
			close(done)
		})))
		<-done
	}()
}

// ---------- Calling ----------

// StartCall is the *caller* path: build PC, send offer, open call window.
func (s *TazherApp) StartCall(name string) {
	if _, exists := s.CallWindows[name]; exists {
		return
	}

	_, offerSDP, err := s.Calls.CreateOffer(name, func(c *webrtc.ICECandidate) {
		candidateBytes, _ := json.Marshal(c.ToJSON())
		s.SendMessage(NexusMessage{
			Type:      "ice_candidate",
			Sender:    s.Username,
			Recipient: name,
			Candidate: string(candidateBytes),
		})
	})
	if err != nil {
		log.Printf("CreateOffer failed: %v", err)
		return
	}

	s.SendMessage(NexusMessage{
		Type:      "call_offer",
		Sender:    s.Username,
		Recipient: name,
		SDP:       offerSDP,
	})

	s.PlaySound("CallOutgoing.wav")
	s.openCallWindow(name, "Calling...")
}

// AnswerCall is the *callee* path: PC already built via HandleOffer; just open window.
func (s *TazherApp) AnswerCall(name string) {
	if _, exists := s.CallWindows[name]; exists {
		return
	}
	s.openCallWindow(name, "Connecting...")
}

func (s *TazherApp) openCallWindow(name, initialStatus string) {
	callWin := s.App.NewWindow("Call: " + name)
	callWin.Resize(fyne.NewSize(300, 450))
	callWin.SetFixedSize(true)
	callWin.SetOnClosed(func() {
		delete(s.CallWindows, name)
		s.SendMessage(NexusMessage{
			Type:      "call_end",
			Sender:    s.Username,
			Recipient: name,
		})
		s.Calls.EndCall(name)
	})
	s.CallWindows[name] = callWin

	avatar := ui.NewAvatarWithStatus(128, "Online", s.getFriendAvatar(name))
	statusLabel := widget.NewLabel(initialStatus)
	callTimer := widget.NewLabel("00:00")
	callTimer.Hide()

	var callStart time.Time
	var timerTicker *time.Ticker

	hangupBtn := widget.NewButton("End Call", func() {
		if timerTicker != nil {
			timerTicker.Stop()
		}
		s.SendMessage(NexusMessage{
			Type:      "call_end",
			Sender:    s.Username,
			Recipient: name,
		})
		s.Calls.EndCall(name)
		callWin.Close()
	})
	hangupBtn.Importance = widget.DangerImportance

	muted := false
	var muteBtn *widget.Button
	muteBtn = widget.NewButton("Mute", func() {
		muted = !muted
		s.Calls.SetMuted(name, muted)
		if muted {
			muteBtn.SetText("Unmute")
		} else {
			muteBtn.SetText("Mute")
		}
	})

	content := container.NewCenter(
		container.NewVBox(
			container.NewCenter(avatar),
			container.NewCenter(widget.NewLabelWithStyle(name, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})),
			container.NewCenter(statusLabel),
			container.NewCenter(callTimer),
			layout.NewSpacer(),
			container.NewCenter(container.NewHBox(muteBtn, hangupBtn)),
		),
	)

	callWin.SetContent(container.NewPadded(content))
	callWin.Show()

	// Start a simple loop waiting for peer connection to establish (p2p polling)
	go func() {
		for i := 0; i < 30; i++ {
			time.Sleep(1 * time.Second)
			s.Calls.Mu.Lock()
			pc, ok := s.Calls.Connections[name]
			s.Calls.Mu.Unlock()
			if ok && pc.ConnectionState() == webrtc.PeerConnectionStateConnected {
				statusLabel.SetText("Connected P2P")
				callTimer.Show()
				callStart = time.Now()
				timerTicker = time.NewTicker(1 * time.Second)
				for range timerTicker.C {
					elapsed := time.Since(callStart)
					mins := int(elapsed.Minutes())
					secs := int(elapsed.Seconds()) % 60
					callTimer.SetText(fmt.Sprintf("%02d:%02d", mins, secs))
				}
				return
			}
		}
	}()
}

func (s *TazherApp) showIncomingCallDialog(from, sdp string) {
	if s.MainWindow == nil {
		return
	}
	
	s.PlaySound("CallIncoming.wav")
	
	win := s.App.NewWindow("Incoming Call")
	win.Resize(fyne.NewSize(350, 500))
	win.SetFixedSize(true)

	overlay := ui.NewCallOverlay(from, s.getFriendAvatar(from), true)
	overlay.OnAnswer = func() {
		_, answerSDP, _ := s.Calls.HandleOffer(from, sdp, func(c *webrtc.ICECandidate) {
			candidateBytes, _ := json.Marshal(c.ToJSON())
			s.SendMessage(NexusMessage{
				Type:      "ice_candidate",
				Sender:    s.Username,
				Recipient: from,
				Candidate: string(candidateBytes),
			})
		})
		s.SendMessage(NexusMessage{
			Type:      "call_answer",
			Sender:    s.Username,
			Recipient: from,
			SDP:       answerSDP,
		})
		s.AnswerCall(from)
		win.Close()
	}
	overlay.OnReject = func() {
		s.SendMessage(NexusMessage{
			Type:      "call_reject",
			Sender:    s.Username,
			Recipient: from,
		})
		win.Close()
	}

	win.SetContent(overlay.Render())
	win.Show()
}

// ---------- Chat Window ----------

func (s *TazherApp) OpenChatWindow(name string) {
	// Standardize title/identity
	var isGroup bool
	var convoName string
	s.DB.QueryRow("SELECT displayname FROM Conversations WHERE identity = ? AND type = 2", name).Scan(&convoName)
	if convoName != "" {
		isGroup = true
	}

	title := name
	if isGroup {
		title = convoName
	}

	// 1. Prepare chat logic
	historyContainer := container.NewVBox()
	s.ChatLogs[name] = historyContainer

	// Load history
	rows, err := s.DB.Query("SELECT author, body_xml FROM Messages WHERE chatname = ? ORDER BY timestamp ASC", name)
	if err == nil {
		for rows.Next() {
			var author, body string
			rows.Scan(&author, &body)
			isMe := author == s.Username
			historyContainer.Add(ui.NewMessageBubble(author, body, isMe))
		}
		rows.Close()
	}

	scroll := container.NewVScroll(historyContainer)

	// Status indicator
	serverStatus := "TAZHER Unified Mesh"
	if strings.Contains(s.ServerAddress, "localhost") {
		serverStatus = "TAZHER: Local Node"
	}
	statusLabel := widget.NewLabelWithStyle(serverStatus, fyne.TextAlignCenter, fyne.TextStyle{Italic: true})

	// Typing indicator
	typingLabel := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	s.ChatTypingLabels[name] = typingLabel
	typingLabel.Hide()

	chatProps := ui.ChatViewProps{
		Name:    title,
		Status:  "Active now", // TODO: Get real status
		IsGroup: isGroup,
		OnCall:  func() { s.StartCall(name) },
		OnSend: func(text string) {
			if strings.TrimSpace(text) == "" {
				return
			}
			body := text
			if strings.HasPrefix(text, "/me ") {
				body = "* " + s.Username + " " + strings.TrimPrefix(text, "/me ")
			}
			s.SendMessage(NexusMessage{Type: "msg", Sender: s.Username, Recipient: name, Body: body})
			historyContainer.Add(ui.NewMessageBubble(s.Username, body, true))
			scroll.ScrollToBottom()
		},
		OnSendFile: func() {
			fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
				if err == nil && reader != nil {
					data, _ := io.ReadAll(reader)
					fileName := reader.URI().Name()
					s.Calls.SendFile(name, fileName, data)
				}
			}, s.MainWindow)
			fd.Show()
		},
	}

	chatView := ui.NewChatView(chatProps)
	// Inject the real scroll into the ChatView placeholder
	chatView.Objects[1].(*fyne.Container).Objects[0] = container.NewBorder(nil, container.NewVBox(statusLabel, typingLabel), nil, nil, scroll)

	s.ContentStack.Objects = []fyne.CanvasObject{chatView}
	s.ContentStack.Refresh()
}


func (s *TazherApp) CreateHomeView() fyne.CanvasObject {
	var lastMood string
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'mood'").Scan(&lastMood)
	return ui.NewTazherHome(s.Username, lastMood, s.Slicer, func(val string) {
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('mood', ?)", val)
		s.SendMessage(NexusMessage{
			Type:   "presence",
			Sender: s.Username,
			Status: "Online",
			Body:   val,
		})
	})
}

func (s *TazherApp) showNewGroupDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Group name")

	// Pre-checked friends to include
	s.loadFriends()
	checks := make([]*widget.Check, len(s.Friends))
	items := []fyne.CanvasObject{widget.NewLabelWithStyle("Select members:", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})}
	for i, f := range s.Friends {
		c := widget.NewCheck(f.Username, nil)
		checks[i] = c
		items = append(items, c)
	}

	form := container.NewVBox(
		widget.NewLabel("Group name:"),
		nameEntry,
		widget.NewSeparator(),
	)
	for _, o := range items {
		form.Add(o)
	}

	d := dialog.NewCustomConfirm("New Group Chat", "Create", "Cancel",
		container.NewVScroll(form),
		func(ok bool) {
			if !ok || strings.TrimSpace(nameEntry.Text) == "" {
				return
			}
			var members []string
			for i, c := range checks {
				if c.Checked {
					members = append(members, s.Friends[i].Username)
				}
			}
			if len(members) == 0 {
				dialog.ShowInformation("Tazher", "Select at least one contact", s.MainWindow)
				return
			}
			convoID := fmt.Sprintf("convo_%d_%s", time.Now().UnixNano(), s.Username)
			s.SendMessage(NexusMessage{
				Type:      "convo_create",
				Sender:    s.Username,
				ConvoID:   convoID,
				ConvoName: nameEntry.Text,
				Members:   members,
			})
		}, s.MainWindow)
	d.Resize(fyne.NewSize(420, 500))
	d.Show()
}

func (s *TazherApp) showAddContactDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Enter Tazher name...")

	dialog.ShowForm("Add Contact", "Send Request", "Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Tazher Name", nameEntry),
		},
		func(ok bool) {
			if ok && nameEntry.Text != "" {
				recipient := nameEntry.Text
				
				// Attempt Nexus first
				s.SendMessage(NexusMessage{
					Type:      "friend_request",
					Sender:    s.Username,
					Recipient: recipient,
				})
				
				// Optional: Inform user that we are also searching P2P
				if s.P2PNode != nil {
					go func() {
						log.Printf("[P2P] Searching for %s on DHT...", recipient)
						pi, err := s.P2PNode.FindUser(recipient)
						if err == nil {
							log.Printf("[P2P] Found %s at %s", recipient, pi.Addrs)
							// If we found them, we could immediately trigger a direct P2P handshake
							// for discovery. For now, SendMessage with P2P fallback handles the delivery.
						}
					}()
				}
				
				dialog.ShowInformation("Tazher™", "Friend request sent to "+recipient, s.MainWindow)
			}
		}, s.MainWindow)
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (s *TazherApp) ShowProfileWindow() {
	win := s.App.NewWindow("My Profile")
	win.Resize(fyne.NewSize(350, 450))

	// Get current profile
	var displayName, mood string
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'display_name'").Scan(&displayName)
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'mood'").Scan(&mood)

	nameEntry := widget.NewEntry()
	nameEntry.SetText(displayName)
	nameEntry.SetPlaceHolder("Display Name")

	moodEntry := widget.NewMultiLineEntry()
	moodEntry.SetText(mood)
	moodEntry.SetPlaceHolder("What's on your mind?")

	saveBtn := widget.NewButton("Save Profile", func() {
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('display_name', ?)", nameEntry.Text)
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('mood', ?)", moodEntry.Text)
		
		s.SendMessage(NexusMessage{
			Type:        "update_profile",
			Sender:      s.Username,
			DisplayName: nameEntry.Text,
			Mood:        moodEntry.Text,
		})
		
		s.Sidebar.Refresh() // Refresh sidebar to show new mood
		win.Close()
	})
	saveBtn.Importance = widget.HighImportance

	win.SetContent(container.NewPadded(
		container.NewVBox(
			widget.NewLabelWithStyle("Personal Information", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			widget.NewLabel("Full Name:"),
			nameEntry,
			widget.NewLabel("Mood Message:"),
			moodEntry,
			layout.NewSpacer(),
			saveBtn,
		),
	))
	win.Show()
}

func (s *TazherApp) getFriendAvatar(name string) string {
	var avatar string
	s.DB.QueryRow("SELECT avatar FROM Contacts WHERE tazhername = ?", name).Scan(&avatar)
	return avatar
}

func (s *TazherApp) getFriendStatus(name string) string {
	var status string
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'status'").Scan(&status)
	if status == "" {
		return "Offline"
	}
	return status
}

func (s *TazherApp) ShowMainWindow() {
	s.MainWindow = s.App.NewWindow("Tazher™ - " + s.Username)
	s.MainWindow.Resize(fyne.NewSize(1000, 700))

	s.loadFriends() // Ensure we have the list
	recent := s.getRecentChats()
	var recentNames []string
	for _, r := range recent {
		recentNames = append(recentNames, r.Name)
	}
	// Add friends who haven't messaged yet to ensure list isn't empty
	for _, f := range s.Friends {
		found := false
		for _, r := range recentNames {
			if r == f.Username {
				found = true
				break
			}
		}
		if !found {
			recentNames = append(recentNames, f.Username)
		}
	}

	sidebarProps := ui.SidebarProps{
		Username:    s.Username,
		Status:      "Online", 
		AvatarPath:  s.AvatarPath,
		Slicer:      s.Slicer,
		OnChatOpen:  s.OpenChatWindow,
		OnAddFriend: s.showAddContactDialog,
		OnNewGroup:  s.showNewGroupDialog,
		RecentChats: s.Friends,
		OnProfile:   s.ShowProfileWindow, // Added callback
	}

	s.Sidebar = ui.NewTazherSidebar(sidebarProps)
	s.HomeView = s.CreateHomeView()
	s.ContentStack = container.NewStack(s.HomeView)

	// --- Toolbar (Top Bar) ---
	toolbar := container.NewHBox(
		widget.NewButtonWithIcon("", theme.HomeIcon(), func() {
			s.ContentStack.Objects = []fyne.CanvasObject{s.HomeView}
			s.ContentStack.Refresh()
		}),
		layout.NewSpacer(),
		widget.NewLabel("Tazher Credit: $0.00"),
		widget.NewButton("Dial pad", func() {}),
	)

	// --- Setup Main Menu ---
	s.setupMenu(s.MainWindow)

	// Layout: [Sidebar] | [Toolbar / Content]
	split := container.NewHSplit(
		s.Sidebar,
		container.NewBorder(toolbar, nil, nil, nil, s.ContentStack),
	)
	split.Offset = 0.3

	s.MainWindow.SetContent(split)
	s.MainWindow.Show()
}

type RecentChat struct {
	Name    string
	LastMsg string
}

func (s *TazherApp) getRecentChats() []RecentChat {
	rows, err := s.DB.Query(`
		SELECT chatname, body_xml FROM Messages
		WHERE id IN (SELECT MAX(id) FROM Messages GROUP BY chatname)
		ORDER BY id DESC LIMIT 20`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var chats []RecentChat
	for rows.Next() {
		var c RecentChat
		rows.Scan(&c.Name, &c.LastMsg)
		if len(c.LastMsg) > 40 {
			c.LastMsg = c.LastMsg[:40] + "..."
		}
		chats = append(chats, c)
	}
	return chats
}

func (s *TazherApp) setupMenu(win fyne.Window) {
	tazherMenu := fyne.NewMenu("Tazher",
		fyne.NewMenuItem("Online Status", nil),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Privacy...", func() {}),
		fyne.NewMenuItem("Sign Out", func() { s.ShowLoginWindow() }),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Close", func() { win.Close() }),
	)

	contactsMenu := fyne.NewMenu("Contacts",
		fyne.NewMenuItem("Add Contact...", func() {}),
		fyne.NewMenuItem("Create New Group...", func() {}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show Outlook Contacts", func() {}),
	)

	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("Contacts", func() {}),
		fyne.NewMenuItem("Recent", func() {}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Compact View", func() {}),
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("Check for Updates", func() {}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("About Tazher™", func() {}),
	)

	debugMenu := fyne.NewMenu("Debug",
		fyne.NewMenuItem("Open Window...", func() {}),
		fyne.NewMenuItem("Contact List", func() {}),
		fyne.NewMenuItem("Getting Started Wizard", func() {}),
		fyne.NewMenuItem("Call Feedback", func() {}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Premium Video", func() {}),
		fyne.NewMenuItem("Premium Screen Sharing", func() {}),
	)

	mainMenu := fyne.NewMainMenu(tazherMenu, contactsMenu, viewMenu, helpMenu, debugMenu)
	win.SetMainMenu(mainMenu)
}



// ---------- Options Window ----------

func (s *TazherApp) ShowOptionsWindow() {
	win := s.App.NewWindow("Tazher™ - Options")
	win.Resize(fyne.NewSize(700, 500))

	categories := []string{"General", "Privacy", "Notifications", "Audio & Video", "Advanced"}
	catList := widget.NewList(
		func() int { return len(categories) },
		func() fyne.CanvasObject { return widget.NewLabel("Category") },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			o.(*widget.Label).SetText(categories[i])
		},
	)

	contentArea := container.NewStack(widget.NewLabel("Select a category on the left"))

	catList.OnSelected = func(id widget.ListItemID) {
		cat := categories[id]
		switch cat {
		case "General":
			compactCheck := widget.NewCheck("Launch in compact mode", func(val bool) {
				s.CompactMode = val
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('compact_mode', ?)", boolStr(val))
			})
			compactCheck.SetChecked(s.CompactMode)

			soundCheck := widget.NewCheck("Enable sounds", func(val bool) {
				s.SoundEnabled = val
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('notify_sounds', ?)", boolStr(val))
			})
			soundCheck.SetChecked(s.SoundEnabled)

			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					compactCheck,
					soundCheck,
				),
			}
		case "Audio & Video":
			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					widget.NewLabelWithStyle("Audio Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					widget.NewLabel("Audio calls use your system default devices."),
					widget.NewCheck("Enable sounds", func(val bool) {
						s.SoundEnabled = val
					}),
				),
			}
		case "Privacy":
			callPolicy := widget.NewRadioGroup([]string{
				"Allow calls from anyone",
				"Allow calls from people in my Contacts only",
			}, func(val string) {
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('privacy_calls', ?)", val)
			})
			var cur string
			s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'privacy_calls'").Scan(&cur)
			if cur != "" {
				callPolicy.SetSelected(cur)
			}

			webStatus := widget.NewCheck("Allow my status to be shown on the web", func(v bool) {
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('privacy_web_status', ?)", boolStr(v))
			})
			var webCur string
			s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'privacy_web_status'").Scan(&webCur)
			webStatus.SetChecked(webCur == "1")

			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					widget.NewLabelWithStyle("Privacy Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					callPolicy,
					webStatus,
				),
			}
		case "Notifications":
			desk := widget.NewCheck("Show desktop notifications for messages", func(v bool) {
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('notify_desktop', ?)", boolStr(v))
			})
			var dcur string
			s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'notify_desktop'").Scan(&dcur)
			desk.SetChecked(dcur != "0")

			sounds := widget.NewCheck("Play sounds for incoming messages", func(v bool) {
				s.SoundEnabled = v
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('notify_sounds', ?)", boolStr(v))
			})
			sounds.SetChecked(s.SoundEnabled)

			callNotify := widget.NewCheck("Show notification for incoming calls", func(v bool) {
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('notify_calls', ?)", boolStr(v))
			})
			var ccur string
			s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'notify_calls'").Scan(&ccur)
			callNotify.SetChecked(ccur != "0")

			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					widget.NewLabelWithStyle("Notification Settings", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					desk, sounds, callNotify,
				),
			}
		case "Advanced":
			addrEntry := widget.NewEntry()
			addrEntry.SetText(s.ServerAddress)

			saveBtn := widget.NewButton("Apply", func() {
				s.ServerAddress = addrEntry.Text
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('server', ?)", s.ServerAddress)
				log.Println("Server updated:", s.ServerAddress)
			})

			contentArea.Objects = []fyne.CanvasObject{
				container.NewVBox(
					widget.NewLabelWithStyle("Connection", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
					container.NewBorder(nil, nil, widget.NewLabel("Server:"), saveBtn, addrEntry),
					widget.NewSeparator(),
					widget.NewLabel("Private Nexus Protocol v1.0"),
					widget.NewLabel("All messages are relayed through your Nexus server."),
					widget.NewLabel("For end-to-end encryption, both clients must support it."),
				),
			}
		}
		contentArea.Refresh()
	}

	split := container.NewHSplit(catList, container.NewPadded(contentArea))
	split.Offset = 0.3

	win.SetContent(split)
	win.Show()
}

// ---------- Login & Registration ----------

func (s *TazherApp) ShowLoginWindow() {
	win := s.App.NewWindow("Tazher™ - Sign In")
	win.Resize(fyne.NewSize(400, 600))
	win.SetFixedSize(true)

	logo := canvas.NewImageFromFile("assets/tazher_logo.png")
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(200, 100))

	usernameEntry := widget.NewEntry()
	usernameEntry.SetPlaceHolder("Tazher Name")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Password")

	serverEntry := widget.NewEntry()
	serverEntry.SetText("wss://tazher7-reborn.fly.dev/cable")

	// Load saved credentials
	var savedUser, savedServer string
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'username'").Scan(&savedUser)
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'server'").Scan(&savedServer)
	if savedUser != "" {
		usernameEntry.SetText(savedUser)
	}
	if savedServer != "" {
		serverEntry.SetText(savedServer)
	}

	// Auto-login if we have everything
	if savedUser != "" && savedServer != "" {
		pass, err := keyring.Get(keyringService, savedUser)
		if err == nil && pass != "" {
			passwordEntry.SetText(pass)
			// Trigger login in a moment
			go func() {
				time.Sleep(500 * time.Millisecond)
				s.Username = savedUser
				s.ServerAddress = savedServer
				if s.ConnectToServer(pass) == nil {
					s.ShowMainWindow()
					s.CheckForUpdates()
					win.Close()
				}
			}()
		}
	}

	statusLabel := widget.NewLabel("")
	statusLabel.Hide()

	loginBtn := widget.NewButton("Sign In", func() {
		if usernameEntry.Text == "" || passwordEntry.Text == "" {
			statusLabel.SetText("Please enter username and password")
			statusLabel.Show()
			return
		}

		s.Username = usernameEntry.Text
		pass := passwordEntry.Text

		statusLabel.SetText("Connecting to Tazher...")
		statusLabel.Show()

		err := s.ConnectToServer(pass)
		if err != nil {
			statusLabel.SetText("Error: " + err.Error())
			return
		}

		// Persist successful credentials
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('username', ?)", s.Username)
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('server', ?)", s.ServerAddress)
		keyring.Set(keyringService, s.Username, pass)

		s.PlaySound("Login.wav")
		s.ShowMainWindow()
		s.CheckForUpdates()
		win.Close()
	})
	loginBtn.Importance = widget.HighImportance

	createBtn := widget.NewButton("Create Account", func() {
		s.showRegistrationWindow(serverEntry.Text)
	})

	p2pBtn := widget.NewButton("Sign In P2P Only", func() {
		if usernameEntry.Text == "" {
			statusLabel.SetText("Please enter a Tazher name")
			statusLabel.Show()
			return
		}
		s.Username = usernameEntry.Text
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('username', ?)", s.Username)
		
		// Start P2P identity
		if s.P2PNode != nil {
			go s.P2PNode.Announce(s.Username)
		}
		
		s.PlaySound("Login.wav")
		s.ShowMainWindow()
		win.Close()
	})
	p2pBtn.Importance = widget.MediumImportance

	win.SetContent(container.NewCenter(
		container.NewVBox(
			container.NewCenter(logo),
			widget.NewLabelWithStyle("Tazher: Private & Safe", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			widget.NewLabelWithStyle("Don't stop til you've had enough", fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
			widget.NewLabel("Sign in to your account"),
			container.NewPadded(usernameEntry),
			container.NewPadded(passwordEntry),
			statusLabel,
			container.NewPadded(loginBtn),
			container.NewPadded(p2pBtn),
			container.NewCenter(createBtn),
			layout.NewSpacer(),
			widget.NewLabelWithStyle("Version "+Version, fyne.TextAlignCenter, fyne.TextStyle{Italic: true}),
		),
	))
	win.Show()
}

func (s *TazherApp) showRegistrationWindow(serverAddr string) {
	win := s.App.NewWindow("Create Account")
	win.Resize(fyne.NewSize(400, 400))
	win.SetFixedSize(true)

	usernameEntry := widget.NewEntry()
	usernameEntry.SetPlaceHolder("Choose a Tazher name")

	emailEntry := widget.NewEntry()
	emailEntry.SetPlaceHolder("Email address")

	moodEntry := widget.NewEntry()
	moodEntry.SetPlaceHolder("Your mood (optional)")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Choose a password")

	confirmEntry := widget.NewPasswordEntry()
	confirmEntry.SetPlaceHolder("Confirm password")

	statusLabel := widget.NewLabel("")
	statusLabel.Hide()

	registerBtn := widget.NewButton("Create Account", func() {
		if usernameEntry.Text == "" || emailEntry.Text == "" {
			statusLabel.SetText("Username and Email are required")
			statusLabel.Show()
			return
		}
		if !strings.Contains(emailEntry.Text, "@") {
			statusLabel.SetText("Invalid email address")
			statusLabel.Show()
			return
		}
		
		// Use the currently configured server address
		addr := s.ServerAddress
		if addr == "" {
			addr = "ws://localhost:8080/cable" // Default to local if unset
		}

		c, _, err := websocket.DefaultDialer.Dial(addr, nil)
		if err != nil {
			statusLabel.SetText("Cannot connect to " + addr)
			statusLabel.Show()
			return
		}

		c.WriteJSON(NexusMessage{
			Type:   "register",
			Sender: usernameEntry.Text,
			Body:   passwordEntry.Text,
			Email:  emailEntry.Text,
			Mood:   moodEntry.Text,
		})

		var result NexusMessage
		c.ReadJSON(&result)
		c.Close()

		if result.Error != "" {
			statusLabel.SetText(result.Error)
			statusLabel.Show()
		} else {
			dialog.ShowInformation("Success", "Account created! You can now sign in.", win)
			win.Close()
		}
	})
	registerBtn.Importance = widget.HighImportance

	win.SetContent(container.NewCenter(
		container.NewVBox(
			widget.NewLabelWithStyle("Create Your Account", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			container.NewPadded(usernameEntry),
			container.NewPadded(passwordEntry),
			container.NewPadded(confirmEntry),
			statusLabel,
			container.NewPadded(registerBtn),
		),
	))
	win.Show()
}

// ---------- Main ----------

func main() {
	tazher := NewTazherApp()
	tazher.App.Settings().SetTheme(&ui.Tazher7Theme{})

	// Load saved avatar + settings
	var savedAvatar string
	tazher.DB.QueryRow("SELECT value FROM Profile WHERE key = 'avatar'").Scan(&savedAvatar)
	if savedAvatar != "" {
		tazher.AvatarPath = savedAvatar
	}
	var soundVal string
	tazher.DB.QueryRow("SELECT value FROM Profile WHERE key = 'notify_sounds'").Scan(&soundVal)
	if soundVal == "0" {
		tazher.SoundEnabled = false
	}
	var compactVal string
	tazher.DB.QueryRow("SELECT value FROM Profile WHERE key = 'compact_mode'").Scan(&compactVal)
	if compactVal == "1" {
		tazher.CompactMode = true
	}

	tazher.ShowLoginWindow()
	tazher.App.Run()
}

func (s *TazherApp) CheckForUpdates() {
	// 1. Check Nexus for latest version
	go func() {
		// ALWAYS attempt to check the Production Master for updates.
		productionURL := "https://tazher7-reborn.fly.dev/version"
		
		resp, err := http.Get(productionURL)
		if err != nil {
			// Fallback to currently connected server address if production is out
			u := strings.Replace(s.ServerAddress, "ws", "http", 1)
			u = strings.TrimSuffix(u, "/cable") + "/version"
			resp, err = http.Get(u)
		}

		if err == nil {
			var latest struct {
				Version string `json:"version"`
				URL     string `json:"url"`
			}
			json.NewDecoder(resp.Body).Decode(&latest)
			if latest.Version != "" && latest.Version != Version {
				log.Printf("[Update] New version available: %s", latest.Version)
				if s.MainWindow != nil {
					s.App.SendNotification(fyne.NewNotification("Tazher Update", "A new version ("+latest.Version+") is available!"))
				}
			}
			resp.Body.Close()
		}
	}()
}
