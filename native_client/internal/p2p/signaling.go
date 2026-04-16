package p2p

import (
	"encoding/json"
	"io"
	"log"

	"github.com/libp2p/go-libp2p/core/crypto"
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
			var raw map[string]interface{}
			if err := decoder.Decode(&raw); err != nil {
				if err != io.EOF {
					log.Printf("P2P Signaling Read Error: %v", err)
				}
				return
			}

			// Forensic Identity Verification
			sigRaw, ok := raw["signature"].(string)
			if ok {
				body, _ := json.Marshal(raw["body"])
				pubKey := s.Conn().RemotePublicKey()
				sig, _ := crypto.ConfigDecodeKey(sigRaw)
				valid, _ := pubKey.Verify(body, sig)
				if !valid {
					log.Printf("[SECURITY] Rejected unverified message from %s", s.Conn().RemotePeer())
					continue
				}
			}
			handler(raw["body"])
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

	body, _ := json.Marshal(msg)
	privKey := n.Host.Peerstore().PrivKey(n.Host.ID())
	sig, _ := privKey.Sign(body)
	sigStr := crypto.ConfigEncodeKey(sig)

	envelope := map[string]interface{}{
		"body":      msg,
		"signature": sigStr,
	}

	return json.NewEncoder(s).Encode(envelope)
}
