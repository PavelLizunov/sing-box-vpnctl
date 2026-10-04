package device

// Wire contract reference: amnezia-vpn/amneziawg-go v3.1.20260828,
// b5928efb6ca19f0153958460c3d141f04abc5c2e, device/{send,receive,uapi}.go.
// Run from submodules/wireguard-go: go test ./device -run AWG31 -count=1.
import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/chacha20"
	"golang.org/x/crypto/chacha20poly1305"
)

func awg31Bare() *Device {
	d := &Device{log: NewLogger(LogLevelSilent, "")}
	d.headers.init = &magicHeader{start: 1, end: 1}
	d.headers.response = &magicHeader{start: 2, end: 2}
	d.headers.cookie = &magicHeader{start: 3, end: 3}
	d.headers.transport = &magicHeader{start: 4, end: 4}
	d.tun.mtu.Store(1420)
	return d
}

// Independently open output of the real encryption worker. This fails on the
// old implementation even without running two copies of the same broken code.
func TestAWG31TransportWire(t *testing.T) {
	for _, prefix := range []int{0, 12, 16, 24} {
		for _, length := range []int{0, 28, 700} {
			t.Run(fmt.Sprintf("S%d/L%d", prefix, length), func(t *testing.T) {
				d := awg31Bare()
				d.paddings.transport = prefix
				var hp HeaderCipherKey
				for i := range hp {
					hp[i] = byte(i + 1)
				}
				if prefix != 0 {
					d.headerProtection.key.Store(&hp)
				}
				var addition UintRange
				addition.FromUint32(1, 1)
				d.contentPaddingAddition.Store(addition)
				d.randomTrailers.Store(true) // CPA must take precedence.
				peer := &Peer{device: d}
				peer.udpWindow.Store(2000)
				aead, err := chacha20poly1305.New(make([]byte, 32))
				if err != nil {
					t.Fatal(err)
				}
				plain := bytes.Repeat([]byte{0x45}, length)
				elem := &QueueOutboundElement{buffer: d.GetOutboundBuffer(48 + length), peer: peer, nonce: 0x102030405, keypair: &Keypair{send: aead, remoteIndex: 0x12345678}}
				elem.packet = elem.buffer[16 : 16+length]
				copy(elem.packet, plain)
				defer func() { d.PutOutboundBuffer(elem.buffer) }()
				container := &QueueOutboundElementsContainer{elems: []*QueueOutboundElement{elem}}
				container.filling.Add(1)
				d.queue.encryption = &outboundQueue{c: make(chan *QueueOutboundElementsContainer, 1)}
				d.queue.encryption.c <- container
				close(d.queue.encryption.c)
				d.RoutineEncryption(0)
				container.filling.Wait()
				wire := bytes.Clone(elem.packet)
				if len(wire) != prefix+32+length+1 {
					t.Fatalf("length=%d, want %d (one authenticated CPA byte)", len(wire), prefix+32+length+1)
				}
				if prefix > 0 {
					cipher, err := chacha20.NewUnauthenticatedCipher(hp[:], wire[:12])
					if err != nil {
						t.Fatal(err)
					}
					cipher.XORKeyStream(wire[prefix:prefix+16], wire[prefix:prefix+16])
				}
				header := wire[prefix : prefix+16]
				if binary.LittleEndian.Uint32(header) != 4 || binary.LittleEndian.Uint32(header[4:]) != 0x12345678 || binary.LittleEndian.Uint64(header[8:]) != elem.nonce {
					t.Fatal("incorrect protected transport header")
				}
				var nonce [12]byte
				binary.LittleEndian.PutUint64(nonce[4:], elem.nonce)
				opened, err := aead.Open(nil, nonce[:], wire[prefix+16:], nil)
				if err != nil || !bytes.Equal(opened, append(plain, 0)) {
					t.Fatalf("authenticated plaintext mismatch: %v", err)
				}
			})
		}
	}
}

func TestAWG31UAPIValidation(t *testing.T) {
	key := strings.Repeat("01", 32)
	for _, end := range []string{"", "\n\n"} {
		d := awg31Bare()
		if err := d.IpcSet("header_protection_key=" + key + end); err == nil {
			t.Fatal("accepted HP without nonce-sized prefixes")
		}
		if k := d.headerProtection.key.Load(); k != nil && !k.IsZero() {
			t.Fatal("invalid configuration partially installed HP")
		}
		for _, config := range []string{
			"header_protection_key=" + key + "\ns1=12\ns2=16\ns3=24\ns4=12",
			"s1=12\ns2=16\ns3=24\ns4=12\nheader_protection_key=" + key,
		} {
			if err := d.IpcSet(config + end); err != nil {
				t.Fatalf("order-independent configuration: %v", err)
			}
		}
		if err := d.IpcSet("s4=11" + end); err == nil || d.paddings.transport != 12 {
			t.Fatal("invalid prefix update accepted or partially applied")
		}
		if err := d.IpcSet("s1=65535" + end); err == nil {
			t.Fatal("accepted impossible fixed-packet size")
		}
		if err := d.IpcSet("header_protection_key=" + strings.Repeat("00", 32) + "\ns1=0\ns2=0\ns3=0\ns4=0" + end); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAWG31HandshakeWire(t *testing.T) {
	for _, prefix := range []int{0, 12, 16, 24} {
		for _, tc := range []struct {
			typ  uint32
			size int
		}{{1, 148}, {2, 92}, {3, 64}} {
			d := awg31Bare()
			d.paddings.init, d.paddings.response, d.paddings.cookie = prefix, prefix, prefix
			var hp HeaderCipherKey
			for i := range hp {
				hp[i] = byte(i + 1)
			}
			if prefix > 0 {
				d.headerProtection.key.Store(&hp)
			}
			core := bytes.Repeat([]byte{0x6b}, tc.size)
			binary.LittleEndian.PutUint32(core, tc.typ)
			wire, err := d.handshakeWireConfig(tc.typ).frameHandshake(core, DefaultUdpWindow)
			if err != nil {
				t.Fatal(err)
			}
			if len(wire) != prefix+tc.size {
				t.Fatal("wrong fixed frame size")
			}
			if prefix > 0 {
				cipher, err := chacha20.NewUnauthenticatedCipher(hp[:], wire[:12])
				if err != nil {
					t.Fatal(err)
				}
				cipher.XORKeyStream(wire[prefix:], wire[prefix:])
			}
			if !bytes.Equal(wire[prefix:], core) {
				t.Fatalf("S%d/type%d incorrect TX core boundary", prefix, tc.typ)
			}
		}
	}
}

// Interleave a complete UAPI H/S/HP update after Noise creation but before
// MACs and framing. The old receiver must still recognize and authenticate it.
func TestAWG31HandshakeConfigInterleave(t *testing.T) {
	oldConfig := "s1=12\ns2=12\ns3=12\ns4=12\nh1=101\nh2=102\nh3=103\nh4=104\nheader_protection_key=" + strings.Repeat("01", 32)
	newConfig := "s1=24\ns2=24\ns3=24\ns4=24\nh1=201\nh2=202\nh3=203\nh4=204\nheader_protection_key=" + strings.Repeat("02", 32)
	for _, typ := range []uint32{MessageInitiationType, MessageResponseType, MessageCookieReplyType} {
		d, receiver := awg31Bare(), awg31Bare()
		for _, dev := range []*Device{d, receiver} {
			if err := dev.IpcSet(oldConfig); err != nil {
				t.Fatal(err)
			}
		}
		d.indexTable.Init()
		private, err := newPrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		d.staticIdentity.privateKey = private
		d.staticIdentity.publicKey = private.publicKey()
		remote, err := newPrivateKey()
		if err != nil {
			t.Fatal(err)
		}
		peer := &Peer{device: d}
		peer.handshake.remoteStatic = remote.publicKey()
		peer.handshake.remoteEphemeral = remote.publicKey()
		peer.handshake.precomputedStaticStatic, err = private.sharedSecret(remote.publicKey())
		if err != nil {
			t.Fatal(err)
		}
		peer.cookieGenerator.Init(remote.publicKey())
		receiver.cookieChecker.Init(remote.publicKey())
		config := d.handshakeWireConfig(typ)
		core := make([]byte, config.size)
		switch typ {
		case MessageInitiationType:
			msg, err := d.createMessageInitiation(peer, config.header)
			if err != nil {
				t.Fatal(err)
			}
			_ = msg.marshal(core)
		case MessageResponseType:
			peer.handshake.state = handshakeInitiationConsumed
			msg, err := d.createMessageResponse(peer, config.header)
			if err != nil {
				t.Fatal(err)
			}
			_ = msg.marshal(core)
		case MessageCookieReplyType:
			d.cookieChecker.Init(remote.publicKey())
			msg, err := d.cookieChecker.CreateReply(make([]byte, MessageInitiationSize), 1, []byte("synthetic endpoint"), config.header)
			if err != nil {
				t.Fatal(err)
			}
			_ = msg.marshal(core)
		}
		if err := d.IpcSet(newConfig); err != nil {
			t.Fatal(err)
		}
		if typ != MessageCookieReplyType {
			peer.cookieGenerator.AddMacs(core)
		}
		wire, err := config.frameHandshake(core, DefaultUdpWindow)
		if err != nil {
			t.Fatal(err)
		}
		got, gotType, prefix := receiver.prepareReceivedPacket(wire)
		if gotType != typ || prefix != 12 || !bytes.Equal(got, core) {
			t.Fatalf("type %d mixed handshake configuration generations", typ)
		}
		if binary.LittleEndian.Uint32(got) != 100+typ {
			t.Fatal("Noise header differs from captured configuration")
		}
		if typ != MessageCookieReplyType && !receiver.cookieChecker.CheckMAC1(got) {
			t.Fatal("handshake MAC invalid after interleaved configuration update")
		}
	}
}

func TestAWG31TransportCapacity(t *testing.T) {
	for _, extra := range []int{0, 1} {
		d := awg31Bare()
		d.paddings.transport = 24
		peer := &Peer{device: d}
		peer.udpWindow.Store(DefaultUdpWindow)
		aead, err := chacha20poly1305.New(make([]byte, 32))
		if err != nil {
			t.Fatal(err)
		}
		length := awgMaxPacketSize - 56 + extra
		elem := &QueueOutboundElement{buffer: d.GetOutboundBuffer(length + 16), peer: peer, keypair: &Keypair{send: aead}}
		elem.packet = elem.buffer[16 : 16+length]
		clear(elem.packet)
		err = d.encryptTransport(elem)
		if extra == 0 && (err != nil || len(elem.packet) != awgMaxPacketSize) {
			t.Fatalf("exact capacity: %v", err)
		}
		if extra == 1 && err == nil {
			t.Fatal("oversized transport accepted")
		}
		d.PutOutboundBuffer(elem.buffer)
	}
}

func TestAWG31PrepareAndBounds(t *testing.T) {
	for _, prefix := range []int{0, 12, 16, 24} {
		for _, tc := range []struct {
			typ  uint32
			size int
		}{{1, 148}, {2, 92}, {3, 64}, {4, 32}} {
			d := awg31Bare()
			d.paddings.init, d.paddings.response, d.paddings.cookie, d.paddings.transport = prefix, prefix, prefix, prefix
			d.randomTrailers.Store(true)
			var hp HeaderCipherKey
			for i := range hp {
				hp[i] = byte(i + 1)
			}
			if prefix != 0 {
				d.headerProtection.key.Store(&hp)
			}
			core := bytes.Repeat([]byte{0x5a}, tc.size)
			binary.LittleEndian.PutUint32(core, tc.typ)
			wire := append(bytes.Repeat([]byte{0x31}, prefix), core...)
			protected := tc.size
			if tc.typ == 4 {
				protected = 16
			}
			if prefix != 0 {
				cipher, err := chacha20.NewUnauthenticatedCipher(hp[:], wire[:12])
				if err != nil {
					t.Fatal(err)
				}
				cipher.XORKeyStream(wire[prefix:prefix+protected], wire[prefix:prefix+protected])
			}
			if tc.typ != 4 {
				wire = append(wire, 0x99, 0x88, 0x77)
			}
			got, typ, padding := d.prepareReceivedPacket(bytes.Clone(wire))
			if typ != tc.typ || padding != prefix || !bytes.Equal(got, core) {
				t.Fatalf("S%d/type%d core restoration failed", prefix, tc.typ)
			}
			if tc.typ != 4 {
				d.randomTrailers.Store(false)
				if _, typ, _ := d.prepareReceivedPacket(bytes.Clone(wire)); typ != MessageUnknownType {
					t.Fatal("accepted disabled handshake trailer")
				}
			}
			for n := 0; n < prefix+tc.size; n++ {
				if _, typ, _ := d.prepareReceivedPacket(bytes.Clone(wire[:n])); typ != MessageUnknownType {
					t.Fatalf("accepted truncated packet length %d", n)
				}
			}
		}
	}
	d := awg31Bare()
	for _, prefix := range []int{-1, int(^uint(0) >> 1)} {
		d.paddings.init, d.paddings.response, d.paddings.cookie, d.paddings.transport = prefix, prefix, prefix, prefix
		if _, typ, _ := d.prepareReceivedPacket(make([]byte, 148)); typ != MessageUnknownType {
			t.Fatal("invalid prefix classified")
		}
	}
	for _, values := range [][3]int{{-1, 0, 32}, {0, -1, 32}, {int(^uint(0) >> 1), 1, 32}, {24, awgMaxPacketSize, 32}} {
		if _, ok := awgPacketSize(values[0], values[1], values[2]); ok {
			t.Fatal("invalid size accepted")
		}
	}
	if n, ok := awgPacketSize(24, awgMaxPacketSize-56, 32); !ok || n != awgMaxPacketSize {
		t.Fatal("exact maximum rejected")
	}
}

func FuzzAWG31Prepare(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, 148))
	f.Add(append([]byte{4, 0, 0, 0}, make([]byte, 28)...))
	f.Fuzz(func(t *testing.T, packet []byte) {
		for _, prefix := range []int{0, 12, 24} {
			d := awg31Bare()
			d.paddings.init, d.paddings.response, d.paddings.cookie, d.paddings.transport = prefix, prefix, prefix, prefix
			d.randomTrailers.Store(true)
			if prefix > 0 {
				var key HeaderCipherKey
				key[0] = 1
				d.headerProtection.key.Store(&key)
			}
			core, typ, _ := d.prepareReceivedPacket(bytes.Clone(packet))
			if typ != MessageUnknownType && len(core) < MinMessageSize {
				t.Fatal("classified undersized core")
			}
		}
	})
}

func TestAWG31PaddingSelection(t *testing.T) {
	d := awg31Bare()
	peer := &Peer{device: d}
	peer.udpWindow.Store(500)
	if peer.randomPaddingAddition(100) != -1 || peer.randomTrailer(100) != -1 {
		t.Fatal("disabled padding must be distinct from zero")
	}
	var addition UintRange
	addition.FromUint32(1, 1)
	d.contentPaddingAddition.Store(addition)
	if peer.randomPaddingAddition(100) != 1 || peer.randomPaddingAddition(500) != 0 {
		t.Fatal("CPA clipping failed")
	}
	addition.FromUint32(0, 1)
	d.contentPaddingAddition.Store(addition)
	if peer.randomPaddingAddition(500) != 0 {
		t.Fatal("enabled zero must not fall through")
	}
	d.randomTrailers.Store(true)
	if peer.randomTrailer(500) != 0 || peer.randomTrailer(501) != 0 || peer.randomTrailer(499) != 0 {
		t.Fatal("trailer boundary failed")
	}
}

func TestAWG31RejectModifiedTransport(t *testing.T) {
	aead, err := chacha20poly1305.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	var nonce [12]byte
	plain := []byte("authenticated transport test content")
	ciphertext := aead.Seal(nil, nonce[:], plain, nil)
	for _, mutation := range []string{"valid", "extra-byte", "changed-tag", "short-tag", "wrong-counter", "wrong-key"} {
		d := awg31Bare()
		d.randomTrailers.Store(true)
		content := bytes.Clone(ciphertext)
		if mutation == "extra-byte" {
			content = append(content, 0)
		}
		if mutation == "changed-tag" {
			content[len(content)-1] ^= 1
		}
		if mutation == "short-tag" {
			content = content[:len(content)-1]
		}
		header := make([]byte, MessageTransportHeaderSize)
		binary.LittleEndian.PutUint32(header, MessageTransportType)
		binary.LittleEndian.PutUint32(header[4:], 1)
		if mutation == "wrong-counter" {
			binary.LittleEndian.PutUint64(header[8:], 1)
		}
		receive := aead
		if mutation == "wrong-key" {
			var key [32]byte
			if _, err := rand.Read(key[:]); err != nil {
				t.Fatal(err)
			}
			receive, err = chacha20poly1305.New(key[:])
			if err != nil {
				t.Fatal(err)
			}
		}
		wire := append(header, content...)
		packet, typ, _ := d.prepareReceivedPacket(wire)
		if typ != MessageTransportType || len(packet) != len(wire) {
			t.Fatalf("%s: transport preparation rejected or truncated ciphertext", mutation)
		}
		elem := &QueueInboundElement{packet: packet, keypair: &Keypair{receive: receive}}
		container := &QueueInboundElementsContainer{elems: []*QueueInboundElement{elem}}
		container.filling.Add(1)
		d.queue.decryption = &inboundQueue{c: make(chan *QueueInboundElementsContainer, 1)}
		d.queue.decryption.c <- container
		close(d.queue.decryption.c)
		d.RoutineDecryption(0)
		container.filling.Wait()
		if mutation == "valid" {
			if !bytes.Equal(elem.packet, plain) {
				t.Fatal("valid transport control failed decryption")
			}
		} else if elem.packet != nil {
			t.Fatalf("accepted %s", mutation)
		}
	}
}
