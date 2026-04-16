package sentinel

import (
	"log"
	"time"
)

type Sentinel struct {
	LastHeartbeat time.Time
	OnIssue       func(issue string)
	IsHealthy     bool
}

func NewSentinel(onIssue func(string)) *Sentinel {
	return &Sentinel{
		LastHeartbeat: time.Now(),
		OnIssue:       onIssue,
		IsHealthy:     true,
	}
}

func (s *Sentinel) Watch(reconnectFn func() error) {
	log.Println("[Sentinel] Initializing health watch...")
	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for range ticker.C {
			// Check if connection was active in the last 60 seconds
			if time.Since(s.LastHeartbeat) > 60*time.Second {
				log.Printf("[Sentinel] Detected stale connection (Last: %v). Repairing...", s.LastHeartbeat)
				s.IsHealthy = false
				s.OnIssue("stale_connection")
				
				if err := reconnectFn(); err != nil {
					log.Printf("[Sentinel] Repair failed: %v", err)
				} else {
					log.Println("[Sentinel] Repair successful. Connection restored.")
					s.IsHealthy = true
					s.LastHeartbeat = time.Now()
				}
			}
		}
	}()
}

func (s *Sentinel) Heartbeat() {
	s.LastHeartbeat = time.Now()
}
