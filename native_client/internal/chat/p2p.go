package chat

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
)

const ProtocolID = "/tazher/signal/1.0.0"

type SignalHandler func(data []byte)

type mdnsNotifee struct {
	h host.Host
}

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	n.h.Connect(context.Background(), pi)
}

type P2PManager struct {
	Host   host.Host
	DHT    *dht.IpfsDHT
	Ctx    context.Context
	Cancel context.CancelFunc

	Username string
	Peers    map[string]peer.ID
	Mu       sync.RWMutex
}

func NewP2PManager(username string) (*P2PManager, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create a libp2p host
	// Using a random port for now, or fixed if needed
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.EnableRelay(),
	)
	if err != nil {
		cancel()
		return nil, err
	}

	// Initialize the DHT
	kademliaDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeServer))
	if err != nil {
		cancel()
		return nil, err
	}

	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		cancel()
		return nil, err
	}

	// Bootstrap with some public nodes if available, otherwise we rely on local discovery
	// For Tazher, we might want to hardcode some Nexus bootstrap nodes later.

	// Initialize mDNS discovery
	ser := mdns.NewMdnsService(h, "tazher-mesh", &mdnsNotifee{h: h})
	if err := ser.Start(); err != nil {
		log.Printf("[P2P] mDNS start error: %v", err)
	}

	mgr := &P2PManager{
		Host:     h,
		DHT:      kademliaDHT,
		Ctx:      ctx,
		Cancel:   cancel,
		Username: username,
		Peers:    make(map[string]peer.ID),
	}

	log.Printf("[P2P] Host ID: %s", h.ID())
	for _, addr := range h.Addrs() {
		log.Printf("[P2P] Listening on: %s/p2p/%s", addr, h.ID())
	}

	return mgr, nil
}

func (p *P2PManager) Announce() {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for {
			select {
			case <-p.Ctx.Done():
				return
			case <-ticker.C:
				p.announceSelf()
			}
		}
	}()
	p.announceSelf()
}

func (p *P2PManager) announceSelf() {
	// We use the DHT to provide a service name "tazher-user:<username>"
	serviceName := fmt.Sprintf("tazher-user:%s", p.Username)
	routingDiscovery := routing.NewRoutingDiscovery(p.DHT)
	_, err := routingDiscovery.Advertise(p.Ctx, serviceName)
	if err != nil {
		log.Printf("[P2P] DHT Advertise error: %v", err)
	} else {
		log.Printf("[P2P] Announced %s to the mesh", serviceName)
	}
}

func (p *P2PManager) DiscoverPeer(username string) (peer.AddrInfo, error) {
	serviceName := fmt.Sprintf("tazher-user:%s", username)
	routingDiscovery := routing.NewRoutingDiscovery(p.DHT)
	
	ctx, cancel := context.WithTimeout(p.Ctx, 10*time.Second)
	defer cancel()

	peerChan, err := routingDiscovery.FindPeers(ctx, serviceName)
	if err != nil {
		return peer.AddrInfo{}, err
	}

	for info := range peerChan {
		if info.ID == p.Host.ID() {
			continue
		}
		log.Printf("[P2P] Discovered peer %s for user %s", info.ID, username)
		return info, nil
	}

	return peer.AddrInfo{}, fmt.Errorf("peer not found for user %s", username)
}

func (p *P2PManager) SetStreamHandler(handler SignalHandler) {
	p.Host.SetStreamHandler(ProtocolID, func(s network.Stream) {
		defer s.Close()
		buf := make([]byte, 8192) // 8KB signal buffer
		n, err := s.Read(buf)
		if err == nil && n > 0 {
			handler(buf[:n])
		}
	})
}

func (p *P2PManager) SendSignal(username string, data []byte) error {
	info, err := p.DiscoverPeer(username)
	if err != nil {
		return err
	}

	p.Host.Peerstore().AddAddrs(info.ID, info.Addrs, time.Hour)
	s, err := p.Host.NewStream(p.Ctx, info.ID, ProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

	_, err = s.Write(data)
	return err
}

func (p *P2PManager) Close() {
	p.Cancel()
	p.DHT.Close()
	p.Host.Close()
}
