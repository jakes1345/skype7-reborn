package sentinel

import (
	"encoding/binary"
	"testing"
)

func TestParseSTUNMappedAddress_tooShort(t *testing.T) {
	if got := parseSTUNMappedAddress(nil); got != "" {
		t.Fatalf("nil: got %q", got)
	}
	if got := parseSTUNMappedAddress(make([]byte, 19)); got != "" {
		t.Fatalf("19 bytes: got %q", got)
	}
}

func TestParseSTUNMappedAddress_xorMappedIPv4(t *testing.T) {
	const magic = 0x2112A442
	// Real endpoint 192.0.2.1:3478 (TEST-NET-1); STUN XOR-MAPPED-ADDRESS encoding.
	ip := uint32(0xC0000201) // 192.0.2.1
	port := uint16(3478)
	xport := port ^ uint16(magic>>16)
	xip := ip ^ magic

	buf := make([]byte, 36)
	// 20-byte STUN header (content ignored by parser)
	pos := 20
	binary.BigEndian.PutUint16(buf[pos:pos+2], 0x0020) // XOR-MAPPED-ADDRESS
	binary.BigEndian.PutUint16(buf[pos+2:pos+4], 8)
	val := pos + 4
	buf[val] = 0
	buf[val+1] = 0x01 // IPv4
	binary.BigEndian.PutUint16(buf[val+2:val+4], xport)
	binary.BigEndian.PutUint32(buf[val+4:val+8], xip)

	if got := parseSTUNMappedAddress(buf); got != "192.0.2.1:3478" {
		t.Fatalf("got %q want 192.0.2.1:3478", got)
	}
}

func TestParseSTUNMappedAddress_mappedAddressLegacy(t *testing.T) {
	buf := make([]byte, 36)
	pos := 20
	binary.BigEndian.PutUint16(buf[pos:pos+2], 0x0001) // MAPPED-ADDRESS
	binary.BigEndian.PutUint16(buf[pos+2:pos+4], 8)
	val := pos + 4
	buf[val] = 0
	buf[val+1] = 0x01
	binary.BigEndian.PutUint16(buf[val+2:val+4], 3478)
	copy(buf[val+4:val+8], []byte{192, 0, 2, 1})

	if got := parseSTUNMappedAddress(buf); got != "192.0.2.1:3478" {
		t.Fatalf("got %q want 192.0.2.1:3478", got)
	}
}

func TestParseSTUNMappedAddress_truncatedAttribute(t *testing.T) {
	buf := make([]byte, 26)
	pos := 20
	binary.BigEndian.PutUint16(buf[pos:pos+2], 0x0020)
	binary.BigEndian.PutUint16(buf[pos+2:pos+4], 8) // claims 8 but only 2 value bytes follow
	if got := parseSTUNMappedAddress(buf); got != "" {
		t.Fatalf("truncated: got %q want empty", got)
	}
}
