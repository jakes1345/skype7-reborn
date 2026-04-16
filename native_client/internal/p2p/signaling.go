package p2p

import (
	"encoding/json"
	"io"
	"log"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const ProtocolID = "/tazher/signaling/1.0.0"

// SignalingHandler processes incoming signaling messages over libp2p
type SignalingHandler func(msg interface{})

func (n *TazherNode) SetupSignalingHandler(handler SignalingHandler) {
	n.Host.SetStreamHandler(protocol.ID(ProtocolID), func(s network.Stream) {
		defer s.Close()
		decoder := json.NewDecoder(s)
		for {
			var msg interface{} // This will be unmarshaled into NexusMessage later
			if err := decoder.Decode(&msg); err != nil {
				if err != io.EOF {
					log.Printf("P2P Signaling Read Error: %v", err)
				}
				return
			}
			handler(msg)
		}
	})
}

// SendSignaling sends a message to a peer over libp2p
func (n *TazherNode) SendSignaling(username string, msg interface{}) error {
	s, err := n.ConnectToUser(username)
	if err != nil {
		return err
	}
	defer s.Close()

	return json.NewEncoder(s).Encode(msg)
}
