package main

import (
	"log"
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
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/ws", nil)
	if err != nil {
		log.Fatal("Dial error:", err)
	}
	defer conn.Close()

	// Auth
	auth := NexusMessage{
		Type:   "auth",
		Sender: "tester",
		Body:   "secure123",
	}
	conn.WriteJSON(auth)
	var resp NexusMessage
	conn.ReadJSON(&resp)
	log.Println("Authenticated.")

	// Talk to Bot
	log.Println("Talking to PhazeBot...")
	msg := NexusMessage{
		Type:      "msg",
		Sender:    "tester",
		Recipient: "PhazeBot",
		Body:      "/mesh",
	}
	conn.WriteJSON(msg)

	conn.ReadJSON(&resp)
	log.Printf("Bot Response: %+v", resp)
}
