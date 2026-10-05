package device

import (
	"encoding/binary"
	"testing"

	"golang.org/x/crypto/chacha20"
)

func TestAWGClassifierHandshakeTransportCollision(t *testing.T) {
	d := awg31Bare()
	d.paddings.init = 12
	d.paddings.transport = 0
	d.headers.transport = &magicHeader{start: 2147483648, end: 4294967295}
	d.indexTable.Init()

	peer := &Peer{device: d}
	index, err := d.indexTable.NewIndexForHandshake(peer, &peer.handshake)
	if err != nil {
		t.Fatal(err)
	}

	packet := make([]byte, 160)
	binary.LittleEndian.PutUint32(packet[0:4], 2147483648)
	binary.LittleEndian.PutUint32(packet[4:8], index)
	binary.LittleEndian.PutUint32(packet[12:16], 1)

	// A handshake-only index must not trigger the transport fast return.
	typ, padding := d.DeterminePacketTypeAndPadding(packet, MessageUnknownType)
	if typ != MessageInitiationType || padding != 12 {
		t.Fatalf("without keypair: got (%d, %d), want (%d, 12)",
			typ, padding, MessageInitiationType)
	}

	// An explicit transport request retains the original header-only result.
	typ, padding = d.DeterminePacketTypeAndPadding(packet, MessageTransportType)
	if typ != MessageTransportType || padding != 0 {
		t.Fatalf("explicit transport: got (%d, %d), want (%d, 0)",
			typ, padding, MessageTransportType)
	}

	// Keep the packet unchanged and install its receiver index via the table API.
	d.indexTable.SwapIndexForKeypair(index, &Keypair{})
	typ, padding = d.DeterminePacketTypeAndPadding(packet, MessageUnknownType)
	if typ != MessageTransportType || padding != 0 {
		t.Fatalf("with keypair: got (%d, %d), want (%d, 0)",
			typ, padding, MessageTransportType)
	}
}

func TestAWGClassifierUnknownTransportIndex(t *testing.T) {
	d := awg31Bare()
	d.paddings.transport = 12
	d.indexTable.Init()

	packet := make([]byte, 12+MessageTransportSize)
	binary.LittleEndian.PutUint32(packet[12:16], MessageTransportType)
	binary.LittleEndian.PutUint32(packet[16:20], 0x12345678)

	// This packet matches no fixed handshake size and has no registered index.
	typ, padding := d.DeterminePacketTypeAndPadding(packet, MessageUnknownType)
	if typ != MessageTransportType || padding != 12 {
		t.Fatalf("unknown index: got (%d, %d), want (%d, 12)",
			typ, padding, MessageTransportType)
	}
}

func TestAWGClassifierProtectedReceiverIndex(t *testing.T) {
	d := awg31Bare()
	d.paddings.init = 12
	d.paddings.transport = 12
	// Deliberately overlap H1 and H4 so a missed index lookup is observable.
	d.headers.transport = &magicHeader{start: 1, end: 1}
	d.indexTable.Init()

	var hp HeaderCipherKey
	for i := range hp {
		hp[i] = byte(i + 1)
	}
	d.headerProtection.key.Store(&hp)

	peer := &Peer{device: d}
	index, err := d.indexTable.NewIndexForHandshake(peer, &peer.handshake)
	if err != nil {
		t.Fatal(err)
	}

	packet := make([]byte, 12+MessageInitiationSize)
	for i := 0; i < 12; i++ {
		packet[i] = byte(i + 1)
	}
	binary.LittleEndian.PutUint32(packet[12:16], 1)
	binary.LittleEndian.PutUint32(packet[16:20], index)

	cipher, err := chacha20.NewUnauthenticatedCipher(hp[:], packet[:12])
	if err != nil {
		t.Fatal(err)
	}
	cipher.XORKeyStream(packet[12:28], packet[12:28])

	typ, padding := d.DeterminePacketTypeAndPadding(packet, MessageUnknownType)
	if typ != MessageInitiationType || padding != 12 {
		t.Fatalf("protected handshake-only index: got (%d, %d), want (%d, 12)",
			typ, padding, MessageInitiationType)
	}

	d.indexTable.SwapIndexForKeypair(index, &Keypair{})
	typ, padding = d.DeterminePacketTypeAndPadding(packet, MessageUnknownType)
	if typ != MessageTransportType || padding != 12 {
		t.Fatalf("protected keypair index: got (%d, %d), want (%d, 12)",
			typ, padding, MessageTransportType)
	}
}
