package sentinel

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// NetworkAudit performs a real STUN Binding Request to discover the public
// IP and confirm UDP egress on port 19302. Replaces the prior version that
// reported "UDP PATH CLEAR" merely on socket open.
func (s *Sentinel) NetworkAudit() (publicIP string, udpOpen bool, recommendation string) {
	stunServer := "stun.l.google.com:19302"

	addr, err := net.ResolveUDPAddr("udp4", stunServer)
	if err != nil {
		return "Unknown", false, "DNS lookup failed for STUN server."
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return "Unknown", false, "CRITICAL: Could not open UDP socket. Check firewall."
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))

	// RFC 5389 Binding Request: 20-byte header.
	// Type=0x0001, Length=0, Magic=0x2112A442, then 96-bit transaction ID.
	req := make([]byte, 20)
	binary.BigEndian.PutUint16(req[0:2], 0x0001)
	binary.BigEndian.PutUint16(req[2:4], 0x0000)
	binary.BigEndian.PutUint32(req[4:8], 0x2112A442)
	rand.Read(req[8:20])

	if _, err := conn.Write(req); err != nil {
		return "Unknown", false, "CRITICAL: UDP write failed. Check egress on port 19302."
	}

	resp := make([]byte, 1024)
	n, err := conn.Read(resp)
	if err != nil || n < 20 {
		return conn.LocalAddr().String(), false,
			"WARNING: STUN reply not received within 3s. UDP may be blocked outbound. Try `sudo ufw allow out 19302/udp`."
	}

	publicIP = parseSTUNMappedAddress(resp[:n])
	if publicIP == "" {
		publicIP = "Unknown (STUN response unparseable)"
	}
	return publicIP, true, "UDP path verified. NAT-mapped address: " + publicIP
}

// parseSTUNMappedAddress walks STUN attributes to find XOR-MAPPED-ADDRESS
// (0x0020) or MAPPED-ADDRESS (0x0001). Returns IPv4 dotted-quad or "".
func parseSTUNMappedAddress(buf []byte) string {
	if len(buf) < 20 {
		return ""
	}
	const magic = 0x2112A442
	pos := 20
	for pos+4 <= len(buf) {
		attr := binary.BigEndian.Uint16(buf[pos : pos+2])
		length := int(binary.BigEndian.Uint16(buf[pos+2 : pos+4]))
		valStart := pos + 4
		valEnd := valStart + length
		if valEnd > len(buf) {
			return ""
		}
		switch attr {
		case 0x0020: // XOR-MAPPED-ADDRESS
			if length >= 8 && buf[valStart+1] == 0x01 { // family=IPv4
				port := binary.BigEndian.Uint16(buf[valStart+2:valStart+4]) ^ uint16(magic>>16)
				ip := binary.BigEndian.Uint32(buf[valStart+4:valStart+8]) ^ magic
				return fmt.Sprintf("%d.%d.%d.%d:%d",
					byte(ip>>24), byte(ip>>16), byte(ip>>8), byte(ip), port)
			}
		case 0x0001: // MAPPED-ADDRESS (legacy)
			if length >= 8 && buf[valStart+1] == 0x01 {
				port := binary.BigEndian.Uint16(buf[valStart+2 : valStart+4])
				return fmt.Sprintf("%d.%d.%d.%d:%d",
					buf[valStart+4], buf[valStart+5], buf[valStart+6], buf[valStart+7], port)
			}
		}
		// Attributes are 32-bit aligned.
		pad := (4 - length%4) % 4
		pos = valEnd + pad
	}
	return ""
}

// GetDiagnosticSummaries returns a human-readable status of the system health.
func (s *Sentinel) GetDiagnosticSummaries() string {
	local, open, advise := s.NetworkAudit()
	status := "HEALTHY"
	if !open {
		status = "DEGRADED"
	}
	return fmt.Sprintf("Phaze SENTINEL REPORT\n-----------------------\nSystem: %s\nPublic Endpoint: %s\nMedia Path: %v\n\nADVICE:\n%s",
		status, local, open, advise)
}
