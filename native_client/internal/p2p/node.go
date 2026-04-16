package p2p

import (
	"context"
	"fmt"
	"log"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

const DiscoveryServiceTag = "tazher-discovery"

var DefaultBootstrapNodes = []string{
	// Canonical IPFS/libp2p boots (TCP 4001)
	"/ip4/147.75.109.213/tcp/4001/p2p/QmNnooDu7zSnoRBsbe2K9XGZ24ButDz8JSRENJ4kQBYS85",
	"/ip4/147.75.70.221/tcp/4001/p2p/QmQCU2EcNmHRPyRQN2R2bt7vSVDD8MmLW4fAymmG8Q3L7f",
	"/ip4/147.75.77.187/tcp/4001/p2p/QmbLHAnMoSWSspM4Wc6kdUSccMctWUsf9iMv458aFisH1X",
	"/ip4/147.75.109.29/tcp/4001/p2p/QmcZf59bWw9AN9oqKyz9YF6fNQuB8C5GvSya84vjRzEqf7",
	// Cloudflare / Protocol Labs additional seeds
	"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvLcCmY1RbmzWBXDUBYLeRowtCr9ZESvBxAod5y",
	"/ip4/128.199.219.111/tcp/4001/p2p/QmSoLSafTMB74huz3CX33THnQMcrSmswUCkVS7nSg1rqXn",
	"/ip4/104.236.76.40/tcp/4001/p2p/QmSoLMeWqB7YGVLJN3pMvAn3XDHcc9m8k9NnSps7pDba3f",
	"/ip4/178.62.158.247/tcp/4001/p2p/QmSoLer265NRztuYAHLvM3ZPEVpLG97MHLD8Vn1CDH9P5j",
}

// TazherNode handles the P2P networking layer (DHT and Signaling)
type TazherNode struct {
	Host host.Host
	DHT  *dht.IpfsDHT
	Ctx  context.Context
}

func NewTazherNode(ctx context.Context, listenPort int) (*TazherNode, error) {
	// Create a new libp2p host with modern NAT traversal features
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort),
			"/ip4/0.0.0.0/tcp/0/quic-v1", // QUIC is faster for hole punching
		),
		libp2p.NATPortMap(),       // UPnP/NAT-PMP
		libp2p.EnableRelay(),      // Support Circuit Relay v2 (client)
		libp2p.EnableHolePunching(), // DCUtR (Direct Connection Upgrade through Relay)
	)
	if err != nil {
		return nil, err
	}

	// Initialize the DHT in client mode
	kdht, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto)) // Auto mode: server if public IP, client if not
	if err != nil {
		return nil, err
	}

	if err = kdht.Bootstrap(ctx); err != nil {
		return nil, err
	}

	node := &TazherNode{
		Host: h,
		DHT:  kdht,
		Ctx:  ctx,
	}

	// Setup mDNS discovery for LAN peers
	ser := mdns.NewMdnsService(h, DiscoveryServiceTag, &discoveryNotifee{h: h, ctx: ctx})
	if err := ser.Start(); err != nil {
		log.Printf("Failed to start mDNS service: %v", err)
	}

	return node, nil
}

type discoveryNotifee struct {
	h   host.Host
	ctx context.Context
}

func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	log.Printf("[P2P] LAN Peer Found: %s", pi.ID.String())
	if err := n.h.Connect(n.ctx, pi); err != nil {
		log.Printf("[P2P] Failed to connect to LAN peer %s: %v", pi.ID, err)
	}
}

// Bootstrap connects to known stable nodes to join the network
func (n *TazherNode) Bootstrap(addrs []string) {
	for _, addr := range addrs {
		ma, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			log.Printf("Invalid bootstrap address %s: %v", addr, err)
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			log.Printf("Invalid bootstrap peer info %s: %v", addr, err)
			continue
		}
		if err := n.Host.Connect(n.Ctx, *pi); err != nil {
			// Silent fail for background bootstrap
		} else {
			log.Printf("Connected to bootstrap node: %s", addr)
		}
	}
}

// Announce presence (Username -> PeerID)
func (n *TazherNode) Announce(username string) error {
	v, _ := multihash.Sum([]byte("tazher:user:"+username), multihash.SHA2_256, -1)
	c := cid.NewCidV1(cid.Raw, v)
	return n.DHT.Provide(n.Ctx, c, true)
}

// FindUser looks up a user's peer info by their tazher name
func (n *TazherNode) FindUser(username string) (peer.AddrInfo, error) {
	v, _ := multihash.Sum([]byte("tazher:user:"+username), multihash.SHA2_256, -1)
	c := cid.NewCidV1(cid.Raw, v)
	peers, err := n.DHT.FindProviders(n.Ctx, c)
	if err != nil {
		return peer.AddrInfo{}, err
	}
	if len(peers) == 0 {
		return peer.AddrInfo{}, fmt.Errorf("user %s not found on the Tazher network", username)
	}
	return peers[0], nil
}

// ConnectToUser establishes a signaling stream to a peer
func (n *TazherNode) ConnectToUser(username string) (network.Stream, error) {
	pi, err := n.FindUser(username)
	if err != nil {
		return nil, err
	}

	if err := n.Host.Connect(n.Ctx, pi); err != nil {
		return nil, err
	}

	return n.Host.NewStream(n.Ctx, pi.ID, protocol.ID(ProtocolID))
}
