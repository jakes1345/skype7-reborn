package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
	"github.com/pion/webrtc/v3"
	"github.com/zalando/go-keyring"

	"private-skype/internal/chat"
	"private-skype/internal/ui"

	"github.com/faiface/beep"
	"github.com/faiface/beep/speaker"
	"github.com/faiface/beep/wav"
)

const keyringService = "private-skype"

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
	ConvoID   string   `json:"convo_id,omitempty"`
	ConvoName string   `json:"convo_name,omitempty"`
	Members   []string `json:"members,omitempty"`
}

// SkypeApp holds all application state
type SkypeApp struct {
	App        fyne.App
	MainWindow fyne.Window

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

	// UI State
	ChatLogs         map[string]*fyne.Container
	ChatTypingLabels map[string]*widget.Label
	SearchResult     *widget.List
	ContactList      *widget.List
	Discovered       []string
	Friends          []FriendInfo
	PendingInbound   []string
	AvatarPath       string
	TypingTimers     map[string]*time.Timer

	// Notifications
	UnreadCounts map[string]int

	Calls *chat.CallManager
}

type FriendInfo struct {
	Username string
	Status   string
	Avatar   string
}

func NewSkypeApp() *SkypeApp {
	a := app.New()

	home, _ := os.UserHomeDir()
	dbDir := filepath.Join(home, ".private_skype")
	os.MkdirAll(dbDir, 0755)
	dbPath := filepath.Join(dbDir, "main.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}

	// Create Skype 7 Compatible Schema
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
		skypename TEXT UNIQUE,
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

	s := &SkypeApp{
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

	s.Calls.OnFile = func(peerName string, fileName string, totalSize int, data []byte) {
		home, _ := os.UserHomeDir()
		downloadPath := filepath.Join(home, "Downloads", fileName)
		if err := os.WriteFile(downloadPath, data, 0644); err != nil {
			log.Printf("save file: %v", err)
			return
		}

		// Persist the transfer record (Skype-7-compatible schema)
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

	return s
}

// ---------- Network ----------

func (s *SkypeApp) ConnectToServer() error {
	s.ConnMu.Lock()
	defer s.ConnMu.Unlock()

	if s.Conn != nil {
		s.Conn.Close()
	}

	c, _, err := websocket.DefaultDialer.Dial(s.ServerAddress, nil)
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	s.Conn = c

	// Load password from OS keyring (never persisted in plaintext)
	password, err := keyring.Get(keyringService, s.Username)
	if err != nil {
		password = ""
	}

	auth := NexusMessage{Type: "auth", Sender: s.Username, Body: password}
	s.Conn.WriteJSON(auth)

	go s.ReadLoop()
	return nil
}

func (s *SkypeApp) SendMessage(msg NexusMessage) {
	s.ConnMu.Lock()
	defer s.ConnMu.Unlock()
	if s.Conn != nil {
		s.Conn.WriteJSON(msg)
	}
}

func (s *SkypeApp) ReadLoop() {
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
					if err := s.ConnectToServer(); err == nil {
						log.Println("Reconnected!")
						return
					}
				}
				log.Println("Failed to reconnect after 10 attempts")
			}()
			return
		}

		switch msg.Type {
		case "auth_result":
			if msg.Error != "" {
				log.Println("Auth failed:", msg.Error)
			} else {
				log.Println("Authenticated as", msg.Sender)
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
			s.DB.Exec("INSERT OR IGNORE INTO Contacts (skypename, status) VALUES (?, 'Online')", msg.Sender)
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
			// WebRTC answer received - in a full implementation this connects the media

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
			s.App.SendNotification(fyne.NewNotification("Skype", msg.Body))
			log.Println("Kicked:", msg.Body)

		case "friend_removed":
			s.DB.Exec("DELETE FROM Contacts WHERE skypename = ?", msg.Sender)
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
			// Body holds the message id/timestamp acknowledged; surface it in UI if we want.
			log.Printf("%s read our message %s", msg.Sender, msg.Body)

		case "register_result":
			if msg.Error != "" {
				log.Println("Registration failed:", msg.Error)
			} else {
				log.Println("Registration successful")
			}
		}
	}
}

// ---------- Friend Management ----------

func (s *SkypeApp) updateFriendStatus(username, status string) {
	s.DB.Exec("UPDATE Contacts SET status = ? WHERE skypename = ?", status, username)
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

func (s *SkypeApp) loadFriends() {
	s.Friends = nil
	rows, err := s.DB.Query("SELECT skypename, COALESCE(status, 'Offline'), COALESCE(avatar_path, '') FROM Contacts ORDER BY status DESC, skypename ASC")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var f FriendInfo
		rows.Scan(&f.Username, &f.Status, &f.Avatar)
		s.Friends = append(s.Friends, f)
	}
}

func (s *SkypeApp) showFriendRequestDialog(from string) {
	if s.MainWindow == nil {
		return
	}
	dialog.ShowConfirm("Friend Request",
		from+" wants to add you as a contact.\nAccept?",
		func(accept bool) {
			if accept {
				s.SendMessage(NexusMessage{Type: "friend_accept", Sender: from})
				s.DB.Exec("INSERT OR IGNORE INTO Contacts (skypename, status) VALUES (?, 'Online')", from)
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

func (s *SkypeApp) removeContact(name string) {
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
			s.DB.Exec("DELETE FROM Contacts WHERE skypename = ?", name)
			s.loadFriends()
			if s.ContactList != nil {
				s.ContactList.Refresh()
			}
		}, s.MainWindow)
}

// ---------- Sound ----------

func (s *SkypeApp) PlaySound(name string) {
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
func (s *SkypeApp) StartCall(name string) {
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
func (s *SkypeApp) AnswerCall(name string) {
	if _, exists := s.CallWindows[name]; exists {
		return
	}
	s.openCallWindow(name, "Connecting...")
}

func (s *SkypeApp) openCallWindow(name, initialStatus string) {
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

func (s *SkypeApp) showIncomingCallDialog(from, sdp string) {
	if s.MainWindow == nil {
		return
	}
	dialog.ShowConfirm("Incoming Call",
		from+" is calling you. Answer?",
		func(answer bool) {
			if answer {
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
			} else {
				s.SendMessage(NexusMessage{
					Type:      "call_reject",
					Sender:    s.Username,
					Recipient: from,
				})
			}
		}, s.MainWindow)
}

// ---------- Chat Window ----------

func (s *SkypeApp) OpenChatWindow(name string) {
	if win, ok := s.ChatWindows[name]; ok {
		win.RequestFocus()
		return
	}

	// Clear unread count
	delete(s.UnreadCounts, name)

	// Determine if this is a group conversation
	var isGroup bool
	var convoName string
	s.DB.QueryRow("SELECT displayname FROM Conversations WHERE identity = ? AND type = 2", name).Scan(&convoName)
	if convoName != "" {
		isGroup = true
	}

	// Send read receipt for the latest message we've seen in this thread
	if !isGroup {
		var lastTS int64
		s.DB.QueryRow("SELECT COALESCE(MAX(timestamp), 0) FROM Messages WHERE chatname = ?", name).Scan(&lastTS)
		if lastTS > 0 {
			s.SendMessage(NexusMessage{
				Type:      "read_receipt",
				Sender:    s.Username,
				Recipient: name,
				Body:      fmt.Sprintf("%d", lastTS),
			})
		}
	}

	title := "Chat: " + name
	if isGroup {
		title = "Group: " + convoName
	}
	win := s.App.NewWindow(title)
	win.SetOnClosed(func() {
		delete(s.ChatWindows, name)
		delete(s.ChatLogs, name)
	})
	win.Resize(fyne.NewSize(600, 500))

	historyContainer := container.NewVBox()
	s.ChatLogs[name] = historyContainer

	// Load history from DB
	rows, err := s.DB.Query("SELECT author, body_xml FROM Messages WHERE chatname = ? ORDER BY timestamp ASC", name)
	if err == nil {
		for rows.Next() {
			var author, body string
			rows.Scan(&author, &body)
			isMe := author == "Me"
			historyContainer.Add(ui.NewMessageBubble(author, body, isMe))
		}
		rows.Close()
	}

	// Typing indicator
	typingLabel := widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Italic: true})
	typingLabel.Hide()

	input := widget.NewMultiLineEntry()
	input.SetPlaceHolder("Type a message...")
	input.Wrapping = fyne.TextWrapWord
	input.SetMinRowsVisible(2)

	// Typing indicator: send at most once per 2s while actively typing.
	var lastTypingSent time.Time
	input.OnChanged = func(val string) {
		if val == "" {
			return
		}
		if time.Since(lastTypingSent) < 2*time.Second {
			return
		}
		lastTypingSent = time.Now()
		s.SendMessage(NexusMessage{
			Type:      "typing",
			Sender:    s.Username,
			Recipient: name,
		})
	}

	sendMsg := func() {
		if strings.TrimSpace(input.Text) == "" {
			return
		}
		body := input.Text

		// Save to local DB
		ts := time.Now().Unix()
		s.DB.Exec("INSERT INTO Messages (chatname, author, body_xml, timestamp, type) VALUES (?, ?, ?, ?, 61)", name, "Me", body, ts)

		// Send to server (1:1 or group)
		if isGroup {
			s.SendMessage(NexusMessage{
				Type:    "convo_msg",
				Sender:  s.Username,
				ConvoID: name,
				Body:    body,
			})
		} else {
			s.SendMessage(NexusMessage{
				Type:      "msg",
				Sender:    s.Username,
				Recipient: name,
				Body:      body,
			})
		}

		// Render in UI
		historyContainer.Add(ui.NewMessageBubble("Me", body, true))
		input.SetText("")
	}

	sendBtn := widget.NewButton("Send", sendMsg)
	sendBtn.Importance = widget.HighImportance

	callBtn := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), func() {
		s.StartCall(name)
	})

	fileBtn := widget.NewButtonWithIcon("", theme.FileIcon(), func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err == nil && reader != nil {
				data, _ := os.ReadFile(reader.URI().Path())
				if err := s.Calls.SendFile(name, reader.URI().Name(), data); err != nil {
					log.Printf("SendFile: %v", err)
					dialog.ShowError(err, win)
					return
				}
				label := "[Sent File: " + reader.URI().Name() + "]"
				ts := time.Now().Unix()
				s.DB.Exec(`INSERT INTO Messages (chatname, author, body_xml, timestamp, type)
					VALUES (?, ?, ?, ?, 68)`, name, "Me", label, ts)
				s.DB.Exec(`INSERT INTO Transfers (type, partner_handle, partner_dispname, status, filename, filepath, filesize, bytestransferred)
					VALUES (1, ?, ?, 8, ?, ?, ?, ?)`,
					name, name, reader.URI().Name(), reader.URI().Path(), len(data), len(data))
				historyContainer.Add(ui.NewMessageBubble("Me", label, true))
			}
		}, win)
		fd.Show()
	})

	// Header
	blueHeader := container.NewStack(
		canvas.NewRectangle(ui.SkypeBlue),
		container.NewPadded(container.NewHBox(
			ui.NewAvatarWithStatus(24, s.getFriendStatus(name), s.getFriendAvatar(name)),
			widget.NewLabelWithStyle(name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			layout.NewSpacer(),
			fileBtn,
			callBtn,
		)),
	)

	inputBar := container.NewBorder(nil, nil, nil, sendBtn, input)

	content := container.NewBorder(
		blueHeader,
		container.NewVBox(typingLabel, container.NewPadded(inputBar)),
		nil, nil,
		container.NewScroll(historyContainer),
	)

	win.SetContent(content)
	s.ChatWindows[name] = win
	s.ChatTypingLabels[name] = typingLabel
	win.Show()
}

func (s *SkypeApp) getFriendStatus(name string) string {
	for _, f := range s.Friends {
		if f.Username == name {
			return f.Status
		}
	}
	return "Offline"
}

func (s *SkypeApp) getFriendAvatar(name string) string {
	for _, f := range s.Friends {
		if f.Username == name {
			return f.Avatar
		}
	}
	return ""
}

// ---------- Main Window ----------

func (s *SkypeApp) ShowMainWindow() {
	if s.MainWindow == nil {
		s.MainWindow = s.App.NewWindow("Skype™ 7.40 (Classic)")
		s.MainWindow.Resize(fyne.NewSize(1000, 700))

		skypeMenu := fyne.NewMenu("Skype",
			fyne.NewMenuItem("Online Status", nil),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Profile", nil),
			fyne.NewMenuItem("Privacy", nil),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Sign Out", func() {
				s.SendMessage(NexusMessage{Type: "presence", Sender: s.Username, Status: "Offline"})
				if s.Conn != nil {
					s.Conn.Close()
				}
				s.MainWindow.Close()
				s.MainWindow = nil
				s.ShowLoginWindow()
			}),
			fyne.NewMenuItem("Compact View", func() {
				s.CompactMode = !s.CompactMode
			}),
			fyne.NewMenuItem("Quit Skype", func() {
				s.SendMessage(NexusMessage{Type: "presence", Sender: s.Username, Status: "Offline"})
				s.App.Quit()
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Close to Tray", func() { s.MainWindow.Hide() }),
		)
		contactsMenu := fyne.NewMenu("Contacts",
			fyne.NewMenuItem("Add Contact", func() {
				s.showAddContactDialog()
			}),
			fyne.NewMenuItem("New Group Chat...", func() {
				s.showNewGroupDialog()
			}),
		)
		toolsMenu := fyne.NewMenu("Tools",
			fyne.NewMenuItem("Options...", func() { s.ShowOptionsWindow() }),
		)
		helpMenu := fyne.NewMenu("Help",
			fyne.NewMenuItem("About Private Skype", func() {
				dialog.ShowInformation("About", "Private Skype 7.40\nA private, self-hosted Skype 7 clone.\nBuilt with Go + Fyne.", s.MainWindow)
			}),
		)

		mainMenu := fyne.NewMainMenu(skypeMenu, contactsMenu, toolsMenu, helpMenu)
		s.MainWindow.SetMainMenu(mainMenu)

		// System tray integration
		if desk, ok := s.App.(desktop.App); ok {
			trayMenu := fyne.NewMenu("Private Skype",
				fyne.NewMenuItem("Open Skype", func() {
					s.MainWindow.Show()
				}),
				fyne.NewMenuItemSeparator(),
				fyne.NewMenuItem("Status: Online", func() {
					s.SendMessage(NexusMessage{Type: "presence", Sender: s.Username, Status: "Online"})
				}),
				fyne.NewMenuItem("Status: Away", func() {
					s.SendMessage(NexusMessage{Type: "presence", Sender: s.Username, Status: "Away"})
				}),
				fyne.NewMenuItem("Status: Offline / Invisible", func() {
					s.SendMessage(NexusMessage{Type: "presence", Sender: s.Username, Status: "Offline"})
				}),
				fyne.NewMenuItemSeparator(),
				fyne.NewMenuItem("Quit", func() {
					s.SendMessage(NexusMessage{Type: "presence", Sender: s.Username, Status: "Offline"})
					s.App.Quit()
				}),
			)
			desk.SetSystemTrayMenu(trayMenu)
			// Use the 100% authentic Skype 7 icon extracted from the binary
			res, err := fyne.LoadResourceFromPath("assets/skype_icon.ico")
			if err == nil {
				desk.SetSystemTrayIcon(res)
			} else {
				desk.SetSystemTrayIcon(theme.HomeIcon())
			}
		}

		s.MainWindow.SetCloseIntercept(func() {
			s.MainWindow.Hide()
		})
	}

	// Status dropdown
	statusOptions := []string{"Online", "Away", "Do Not Disturb", "Invisible", "Offline"}
	statusDropdown := widget.NewSelect(statusOptions, func(val string) {
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('status', ?)", val)
		s.SendMessage(NexusMessage{Type: "presence", Sender: s.Username, Status: val})
	})

	var lastStatus string
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'status'").Scan(&lastStatus)
	if lastStatus != "" {
		statusDropdown.SetSelected(lastStatus)
	} else {
		statusDropdown.SetSelected("Online")
	}

	header := container.NewHBox(
		ui.NewAvatarWithStatus(32, lastStatus, s.AvatarPath),
		widget.NewLabelWithStyle(s.Username, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		statusDropdown,
	)

	// Search
	searchBar := widget.NewEntry()
	searchBar.SetPlaceHolder("Search people...")

	searchBar.OnChanged = func(val string) {
		if s.Conn != nil && len(val) > 1 {
			s.SendMessage(NexusMessage{Type: "search", Sender: s.Username, Body: val})
		}
	}

	searchResultList := widget.NewList(
		func() int { return len(s.Discovered) },
		func() fyne.CanvasObject {
			return container.NewHBox(
				ui.NewAvatarWithStatus(24, "Offline", ""),
				widget.NewLabel("Result"),
				widget.NewButton("Add", func() {}),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			box.Objects[1].(*widget.Label).SetText(s.Discovered[i])
			box.Objects[2].(*widget.Button).OnTapped = func() {
				name := s.Discovered[i]
				s.SendMessage(NexusMessage{
					Type:      "friend_request",
					Sender:    s.Username,
					Recipient: name,
				})
				dialog.ShowInformation("Skype™", "Friend request sent to "+name, s.MainWindow)
			}
		},
	)
	searchResultList.OnSelected = func(id widget.ListItemID) {
		s.OpenChatWindow(s.Discovered[id])
	}
	s.SearchResult = searchResultList

	// Contacts
	s.loadFriends()

	contactList := widget.NewList(
		func() int { return len(s.Friends) },
		func() fyne.CanvasObject {
			removeBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {})
			return container.NewBorder(nil, nil,
				ui.NewAvatarWithStatus(32, "Offline", ""),
				removeBtn,
				container.NewVBox(
					widget.NewLabel("Contact Name"),
					widget.NewLabelWithStyle("Offline", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
				),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			// Border layout: objects order is [center, top, bottom, left, right]
			// but we set left=avatar, right=removeBtn, center=infoBox
			f := s.Friends[i]
			for _, obj := range box.Objects {
				switch v := obj.(type) {
				case *fyne.Container:
					if len(v.Objects) == 2 {
						if lbl, ok := v.Objects[0].(*widget.Label); ok {
							lbl.SetText(f.Username)
						}
						if lbl, ok := v.Objects[1].(*widget.Label); ok {
							lbl.SetText(f.Status)
						}
					}
				case *widget.Button:
					name := f.Username
					v.OnTapped = func() { s.removeContact(name) }
				}
			}
		},
	)
	contactList.OnSelected = func(id widget.ListItemID) {
		s.OpenChatWindow(s.Friends[id].Username)
	}
	s.ContactList = contactList

	// Recent chats
	recentChats := s.getRecentChats()
	recentList := widget.NewList(
		func() int { return len(recentChats) },
		func() fyne.CanvasObject {
			return container.NewHBox(
				ui.NewAvatarWithStatus(32, "Offline", ""),
				container.NewVBox(
					widget.NewLabel("Name"),
					widget.NewLabelWithStyle("Last message...", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}),
				),
			)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			box := o.(*fyne.Container)
			infoBox := box.Objects[1].(*fyne.Container)
			infoBox.Objects[0].(*widget.Label).SetText(recentChats[i].Name)
			infoBox.Objects[1].(*widget.Label).SetText(recentChats[i].LastMsg)
		},
	)
	recentList.OnSelected = func(id widget.ListItemID) {
		s.OpenChatWindow(recentChats[id].Name)
	}

	// Sidebar tabs
	sidebarTabs := container.NewAppTabs(
		container.NewTabItem("Recent", recentList),
		container.NewTabItem("Contacts", contactList),
		container.NewTabItem("Search", searchResultList),
	)

	bottomBar := container.NewHBox(
		widget.NewButtonWithIcon("", theme.HomeIcon(), func() {}),
	)

	sidebarContent := container.NewBorder(
		container.NewVBox(header, container.NewPadded(searchBar)),
		bottomBar, nil, nil,
		sidebarTabs,
	)

	sidebar := container.NewStack(
		canvas.NewRectangle(ui.SkypeLightBlue),
		sidebarContent,
	)

	// Home content
	homeAvatar := ui.NewAvatarWithStatus(128, "Online", s.AvatarPath)
	welcomeLabel := widget.NewLabelWithStyle("Welcome, "+s.Username, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})

	statusEntry := widget.NewEntry()
	var lastMood string
	s.DB.QueryRow("SELECT value FROM Profile WHERE key = 'mood'").Scan(&lastMood)
	statusEntry.SetText(lastMood)
	statusEntry.SetPlaceHolder("Tell your friends what you're up to...")
	var moodDebounce *time.Timer
	statusEntry.OnChanged = func(val string) {
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('mood', ?)", val)
		if moodDebounce != nil {
			moodDebounce.Stop()
		}
		moodDebounce = time.AfterFunc(600*time.Millisecond, func() {
			s.SendMessage(NexusMessage{
				Type:   "presence",
				Sender: s.Username,
				Status: "Online",
				Body:   val,
			})
		})
	}

	changeAvatarBtn := widget.NewButton("Change Picture...", func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err == nil && reader != nil {
				s.AvatarPath = reader.URI().Path()
				s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('avatar', ?)", s.AvatarPath)
				log.Println("Avatar saved:", s.AvatarPath)
			}
		}, s.MainWindow)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".png", ".jpg", ".jpeg"}))
		fd.Show()
	})

	homeContent := container.NewCenter(
		container.NewVBox(
			container.NewCenter(homeAvatar),
			container.NewCenter(changeAvatarBtn),
			welcomeLabel,
			container.NewPadded(statusEntry),
			widget.NewButton("Find Friends", func() {
				sidebarTabs.SelectIndex(2)
				searchBar.FocusGained()
			}),
		),
	)

	mainContentArea := container.NewStack(
		canvas.NewRectangle(color.White),
		homeContent,
	)

	split := container.NewHSplit(sidebar, mainContentArea)
	if s.CompactMode {
		split.Offset = 0.2
	} else {
		split.Offset = 0.3
	}

	s.MainWindow.SetContent(split)
	s.MainWindow.Show()
}

func (s *SkypeApp) showNewGroupDialog() {
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
				dialog.ShowInformation("Skype", "Select at least one contact", s.MainWindow)
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

func (s *SkypeApp) showAddContactDialog() {
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Enter Skype name...")

	dialog.ShowForm("Add Contact", "Send Request", "Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Skype Name", nameEntry),
		},
		func(ok bool) {
			if ok && nameEntry.Text != "" {
				s.SendMessage(NexusMessage{
					Type:      "friend_request",
					Sender:    s.Username,
					Recipient: nameEntry.Text,
				})
				dialog.ShowInformation("Skype™", "Friend request sent to "+nameEntry.Text, s.MainWindow)
			}
		}, s.MainWindow)
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

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

type RecentChat struct {
	Name    string
	LastMsg string
}

func (s *SkypeApp) getRecentChats() []RecentChat {
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

// ---------- Options Window ----------

func (s *SkypeApp) ShowOptionsWindow() {
	win := s.App.NewWindow("Skype™ - Options")
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

func (s *SkypeApp) ShowLoginWindow() {
	win := s.App.NewWindow("Skype™ - Sign In")
	win.Resize(fyne.NewSize(400, 550))
	win.SetFixedSize(true)

	logo := canvas.NewImageFromResource(theme.HomeIcon())
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(100, 100))

	usernameEntry := widget.NewEntry()
	usernameEntry.SetPlaceHolder("Skype Name")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Password")

	serverEntry := widget.NewEntry()
	serverEntry.SetText("wss://skype7-reborn.fly.dev/cable")

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

	statusLabel := widget.NewLabel("")
	statusLabel.Hide()

	loginBtn := widget.NewButton("Sign In", func() {
		if usernameEntry.Text == "" || passwordEntry.Text == "" {
			statusLabel.SetText("Please enter username and password")
			statusLabel.Show()
			return
		}
		s.Username = usernameEntry.Text
		s.ServerAddress = serverEntry.Text
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('username', ?)", s.Username)
		s.DB.Exec("INSERT OR REPLACE INTO Profile (key, value) VALUES ('server', ?)", s.ServerAddress)
		if err := keyring.Set(keyringService, s.Username, passwordEntry.Text); err != nil {
			log.Printf("keyring: %v — falling back to in-memory only", err)
		}
		// Scrub any legacy plaintext password stored in DB
		s.DB.Exec("DELETE FROM Profile WHERE key = 'password'")

		statusLabel.SetText("Connecting...")
		statusLabel.Show()

		err := s.ConnectToServer()
		if err != nil {
			statusLabel.SetText("Connection failed: " + err.Error())
			return
		}

		s.PlaySound("Login.wav")
		s.ShowMainWindow()
		win.Close()
	})
	loginBtn.Importance = widget.HighImportance

	createBtn := widget.NewButton("Create Account", func() {
		s.showRegistrationWindow(serverEntry.Text)
	})

	serverToggle := widget.NewCheck("Show server settings", func(show bool) {
		if show {
			serverEntry.Show()
		} else {
			serverEntry.Hide()
		}
	})
	serverEntry.Hide()

	win.SetContent(container.NewCenter(
		container.NewVBox(
			container.NewCenter(logo),
			widget.NewLabelWithStyle("Private Skype", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			widget.NewLabel("Sign in to your account"),
			container.NewPadded(usernameEntry),
			container.NewPadded(passwordEntry),
			container.NewPadded(serverToggle),
			container.NewPadded(serverEntry),
			statusLabel,
			container.NewPadded(loginBtn),
			container.NewCenter(createBtn),
		),
	))
	win.Show()
}

func (s *SkypeApp) showRegistrationWindow(serverAddr string) {
	win := s.App.NewWindow("Create Account")
	win.Resize(fyne.NewSize(400, 400))
	win.SetFixedSize(true)

	usernameEntry := widget.NewEntry()
	usernameEntry.SetPlaceHolder("Choose a Skype name")

	passwordEntry := widget.NewPasswordEntry()
	passwordEntry.SetPlaceHolder("Choose a password")

	confirmEntry := widget.NewPasswordEntry()
	confirmEntry.SetPlaceHolder("Confirm password")

	statusLabel := widget.NewLabel("")
	statusLabel.Hide()

	registerBtn := widget.NewButton("Create Account", func() {
		if usernameEntry.Text == "" {
			statusLabel.SetText("Please enter a username")
			statusLabel.Show()
			return
		}
		if len(passwordEntry.Text) < 4 {
			statusLabel.SetText("Password must be at least 4 characters")
			statusLabel.Show()
			return
		}
		if passwordEntry.Text != confirmEntry.Text {
			statusLabel.SetText("Passwords do not match")
			statusLabel.Show()
			return
		}

		// Connect temporarily to register
		addr := serverAddr
		if addr == "" {
			addr = "wss://skype7-reborn.fly.dev/cable"
		}
		c, _, err := websocket.DefaultDialer.Dial(addr, nil)
		if err != nil {
			statusLabel.SetText("Cannot connect to server")
			statusLabel.Show()
			return
		}

		c.WriteJSON(NexusMessage{
			Type:   "register",
			Sender: usernameEntry.Text,
			Body:   passwordEntry.Text,
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
	skype := NewSkypeApp()
	skype.App.Settings().SetTheme(&ui.Skype7Theme{})

	// Load saved avatar + settings
	var savedAvatar string
	skype.DB.QueryRow("SELECT value FROM Profile WHERE key = 'avatar'").Scan(&savedAvatar)
	if savedAvatar != "" {
		skype.AvatarPath = savedAvatar
	}
	var soundVal string
	skype.DB.QueryRow("SELECT value FROM Profile WHERE key = 'notify_sounds'").Scan(&soundVal)
	if soundVal == "0" {
		skype.SoundEnabled = false
	}
	var compactVal string
	skype.DB.QueryRow("SELECT value FROM Profile WHERE key = 'compact_mode'").Scan(&compactVal)
	if compactVal == "1" {
		skype.CompactMode = true
	}

	skype.ShowLoginWindow()
	skype.App.Run()
}
