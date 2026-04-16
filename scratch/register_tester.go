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
	Results   []string `json:"results"`
	Email     string   `json:"email,omitempty"`
}

func main() {
	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080/ws", nil)
	if err != nil {
		log.Fatal("Dial error:", err)
	}
	defer conn.Close()

	// 1. Register
	log.Println("Registering forensic account 'tester'...")
	reg := NexusMessage{
		Type:   "register",
		Sender: "tester",
		Email:  "tester@tazher.com",
		Body:   "secure123",
	}
	conn.WriteJSON(reg)

	var resp NexusMessage
	conn.ReadJSON(&resp)
	log.Printf("Reg Response: %+v", resp)

	// Since we are simulating, we know the code is printed to server logs
	// But let's assume we can bypass for now or just check if pending_verification
	if resp.Status == "pending_verification" {
		log.Println("Account created! Verification pending (SIM).")
	}

	// 2. Auth (Wait, need verification first)
	// We'll simulate verification logic on server - already did in nexus_server/main.go
}
