package main

import (
	"encoding/json"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

type NexusMessage struct {
	Type      string   `json:"type"`
	Sender    string   `json:"sender"`
	Recipient string   `json:"recipient"`
	Body      string   `json:"body"`
	Status    string   `json:"status"`
	Results   []string `json:"results"`
}

func main() {
	u := url.URL{Scheme: "wss", Host: "skype7-reborn.fly.dev", Path: "/cable"}
	log.Printf("connecting to %s", u.String())

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("read:", err)
				return
			}
			log.Printf("recv: %s", message)
		}
	}()

	// 1. Register a test user
	testUser := "AgentTest_" + time.Now().Format("150405")
	reg := NexusMessage{
		Type:   "register",
		Sender: testUser,
		Body:   "testpass123",
	}
	sendJSON(c, reg)
	time.Sleep(2 * time.Second)

	// 2. Auth
	auth := NexusMessage{
		Type:   "auth",
		Sender: testUser,
		Body:   "testpass123",
	}
	sendJSON(c, auth)
	time.Sleep(2 * time.Second)

	// 3. Search for someone
	search := NexusMessage{
		Type:   "search",
		Sender: testUser,
		Body:   "jake",
	}
	sendJSON(c, search)
	time.Sleep(2 * time.Second)

	// 4. Update presence
	presence := NexusMessage{
		Type:   "presence",
		Sender: testUser,
		Status: "Away",
		Body:   "Testing protocol features",
	}
	sendJSON(c, presence)
	time.Sleep(2 * time.Second)

	// 5. Send a message to the user!
	chatMsg := NexusMessage{
		Type:      "msg",
		Sender:    testUser,
		Recipient: "jakes1328",
		Body:      "Hello from the Antigravity test agent! Protocol check passed. 🚀",
	}
	sendJSON(c, chatMsg)
	time.Sleep(2 * time.Second)

	log.Println("Test sequence complete")
	time.Sleep(5 * time.Second)
}

func sendJSON(c *websocket.Conn, msg NexusMessage) {
	data, _ := json.Marshal(msg)
	log.Printf("send: %s", data)
	err := c.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		log.Println("write:", err)
	}
}
