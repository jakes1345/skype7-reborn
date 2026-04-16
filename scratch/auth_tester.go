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

	// Verify
	log.Println("Verifying account 'tester'...")
	verify := NexusMessage{
		Type:   "verify_email",
		Sender: "tester",
		Body:   "222700",
	}
	conn.WriteJSON(verify)

	var resp NexusMessage
	conn.ReadJSON(&resp)
	log.Printf("Verify Response: %+v", resp)

	// Login
	log.Println("Authenticating account 'tester'...")
	auth := NexusMessage{
		Type:   "auth",
		Sender: "tester",
		Body:   "secure123",
	}
	conn.WriteJSON(auth)

	conn.ReadJSON(&resp)
	log.Printf("Auth Response: %+v", resp)
}
