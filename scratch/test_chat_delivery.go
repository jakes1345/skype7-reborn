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
}

func main() {
	u := url.URL{Scheme: "wss", Host: "phazechat.world", Path: "/ws"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()

	sender := "AutoTester"
	recipient := "AgentTest_154722"

	// Auth as AutoTester
	c.WriteJSON(NexusMessage{Type: "register", Sender: sender, Body: "pass"})
	time.Sleep(1 * time.Second)
	c.WriteJSON(NexusMessage{Type: "auth", Sender: sender, Body: "pass"})
	time.Sleep(1 * time.Second)

	// Send message
	msg := NexusMessage{
		Type:      "msg",
		Sender:    sender,
		Recipient: recipient,
		Body:      "This is a real-time integration test. Can you see this?",
	}
	data, _ := json.Marshal(msg)
	log.Printf("Sending: %s", data)
	c.WriteMessage(websocket.TextMessage, data)
	
	time.Sleep(2 * time.Second)
	log.Println("Message sent.")
}
