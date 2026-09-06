package device_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/sagernet/wireguard-go/conn"
	"github.com/sagernet/wireguard-go/device"
	"github.com/sagernet/wireguard-go/tun"
)

// These black-box tests must be copied unchanged to the baseline and repaired
// revisions. They prove self-to-self transfer, not official implementation
// interoperability. All keys and traffic are synthetic; never log IPC contents.
const awg31BaseConfig = "\ns1=16\ns2=24\ns3=16\ns4=16" +
	"\nh1=100000-100010\nh2=200000-200010\nh3=300000-300010\nh4=400000-400010"

const awg31HPConfig = "\nheader_protection_key=1212121212121212121212121212121212121212121212121212121212121212"
const awg31OtherHPConfig = "\nheader_protection_key=3434343434343434343434343434343434343434343434343434343434343434"
const awg31Wait = 10 * time.Second

func TestAWG31Transfer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
	}{
		{"AWG2", ""},
		{"HP", awg31HPConfig},
		{"CPA1", "\ncontent_padding_addition=1"},
		{"Trailers", "\nrandom_trailers=true"},
		{"Combined", awg31HPConfig + "\ncontent_padding_addition=1\nrandom_trailers=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := awg31Pair(t, tc.config, tc.config, false)
			// Cover all three public ingress paths, including split slices and
			// a two-packet batch. Small packets exercise CPA even with a small
			// UDP window. IP lengths 47/48/49 cross the 16-byte padding boundary.
			for _, mode := range []string{"TUN", "InputPacket", "InputPackets"} {
				if !t.Run(mode, func(t *testing.T) {
					awg31Exchange(t, client, server, mode, 2, 1)
					awg31Exchange(t, server, client, mode, 1, 2)
				}) {
					return // Do not cascade failures through a broken session.
				}
			}
		})
	}
}

func TestAWG31WrongKeys(t *testing.T) {
	// Keep a known-good control in the same test selection as the negatives.
	t.Run("CorrectKeys", func(t *testing.T) {
		client, server := awg31Pair(t, awg31HPConfig, awg31HPConfig, false)
		awg31Exchange(t, client, server, "InputPacket", 2, 1)
		awg31Exchange(t, server, client, "InputPacket", 1, 2)
	})
	for _, tc := range []struct {
		name          string
		clientConfig  string
		wrongIdentity bool
	}{
		{"WrongHP", awg31OtherHPConfig, false},
		{"WrongIdentity", awg31HPConfig, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := awg31Pair(t, tc.clientConfig, awg31HPConfig, tc.wrongIdentity)
			packet := awg31Packet(2, 1, 19)
			client.dev.InputPacket(packet[16:20], [][]byte{packet})
			// No junk or keepalive is configured: the first successful UDP
			// send/receive is evidence of an actual handshake attempt, not
			// merely enqueueing data or a failed local bind.
			awg31Await(t, client.bind.sent, "handshake was not sent")
			awg31Await(t, server.bind.received, "handshake did not reach receiver")
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			select {
			case <-server.tun.output:
				t.Fatal("wrong keys delivered application data to server")
			case <-client.tun.output:
				t.Fatal("wrong keys delivered application data to client")
			case <-timer.C:
				// Bounded rejection observation, not an assertion about silence
				// forever or an exact handshake retransmission schedule.
			}
		})
	}
}

type awg31Node struct {
	dev  *device.Device
	tun  *awg31CaptureTUN
	bind *awg31ObservedBind
}

func awg31Pair(t *testing.T, clientConfig, serverConfig string, wrongIdentity bool) (*awg31Node, *awg31Node) {
	t.Helper()
	serverPrivate, serverPublic := generateTestKeyPair(t)
	clientPrivate, clientPublic := generateTestKeyPair(t)
	if wrongIdentity {
		// A different keypair, not a bit flip that X25519 clamping can erase.
		_, clientPublic = generateTestKeyPair(t)
	}
	server := awg31Start(t, "server", serverPrivate, clientPublic, "10.0.0.2/32", serverConfig, "")
	endpoint := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), devicePort(t, server.dev))
	client := awg31Start(t, "client", clientPrivate, serverPublic, "10.0.0.1/32", clientConfig, endpoint.String())
	return client, server
}

func awg31Start(t *testing.T, name, privateKey, peerKey, allowedIP, config, endpoint string) *awg31Node {
	t.Helper()
	capture := &awg31CaptureTUN{
		testTUN: &testTUN{
			name:    name,
			inbound: make(chan []byte, 16),
			events:  make(chan tun.Event, 1),
			done:    make(chan struct{}),
		},
		output: make(chan []byte, 32),
	}
	bind := &awg31ObservedBind{
		Bind:     conn.NewStdNetBind(nil),
		sent:     make(chan struct{}, 1),
		received: make(chan struct{}, 1),
	}
	// Errors are asserted by stage, never by dumping config or device logs.
	logger := &device.Logger{
		Verbosef: func(string, ...any) {},
		Errorf:   func(string, ...any) {},
	}
	dev := device.NewDevice(context.Background(), capture, bind, logger, 0)
	t.Cleanup(dev.Close)
	ipc := "private_key=" + privateKey + awg31BaseConfig + config +
		"\npublic_key=" + peerKey + "\nallowed_ip=" + allowedIP
	if endpoint != "" {
		ipc += "\nendpoint=" + endpoint
	}
	if err := dev.IpcSet(ipc); err != nil {
		t.Fatal("synthetic device configuration rejected")
	}
	if err := dev.Up(); err != nil {
		t.Fatal("synthetic device failed to start")
	}
	return &awg31Node{dev: dev, tun: capture, bind: bind}
}

// Embed the existing test TUN and override only Write. Copy before returning:
// the receiver returns its buffers to a pool immediately after this call.
type awg31CaptureTUN struct {
	*testTUN
	output chan []byte
}

func (t *awg31CaptureTUN) Write(bufs [][]byte, offset int) (int, error) {
	for i, buf := range bufs {
		select {
		case t.output <- append([]byte(nil), buf[offset:]...):
		case <-t.done:
			return i, os.ErrClosed
		}
	}
	return len(bufs), nil
}

// Embed the real UDP bind, observing only successful I/O. Respect Send's
// offset/batching contract by forwarding the original call unchanged.
type awg31ObservedBind struct {
	conn.Bind
	sent     chan struct{}
	received chan struct{}
}

func awg31Signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (b *awg31ObservedBind) Send(bufs [][]byte, ep conn.Endpoint, offset int) error {
	err := b.Bind.Send(bufs, ep, offset)
	if err == nil && len(bufs) > 0 {
		awg31Signal(b.sent)
	}
	return err
}

func (b *awg31ObservedBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	fns, actualPort, err := b.Bind.Open(port)
	if err != nil {
		return nil, actualPort, err
	}
	wrapped := make([]conn.ReceiveFunc, len(fns))
	for i, recv := range fns {
		wrapped[i] = func(bufs [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
			n, err := recv(bufs, sizes, eps)
			if err == nil && n > 0 {
				awg31Signal(b.received)
			}
			return n, err
		}
	}
	return wrapped, actualPort, nil
}

func awg31Await(t *testing.T, ch <-chan struct{}, failure string) {
	t.Helper()
	timer := time.NewTimer(awg31Wait)
	defer timer.Stop()
	select {
	case <-ch:
	case <-timer.C:
		t.Fatal(failure)
	}
}

func awg31Exchange(t *testing.T, from, to *awg31Node, mode string, source, destination byte) {
	t.Helper()
	packets := [][]byte{awg31Packet(source, destination, 1)}
	switch mode {
	case "TUN":
		timer := time.NewTimer(awg31Wait)
		defer timer.Stop()
		select {
		case from.tun.inbound <- packets[0]:
		case <-timer.C:
			t.Fatal("TUN injection timed out")
		}
	case "InputPacket":
		packets[0] = awg31Packet(source, destination, 19)
		from.dev.InputPacket(packets[0][16:20], [][]byte{packets[0][:20], packets[0][20:]})
	case "InputPackets":
		packets = [][]byte{awg31Packet(source, destination, 20), awg31Packet(source, destination, 21)}
		refs := make([]*device.InputPacketRef, len(packets))
		for i, packet := range packets {
			refs[i] = &device.InputPacketRef{
				Destination:  packet[16:20],
				PacketSlices: [][]byte{packet[:20], packet[20:]},
			}
		}
		if len(from.dev.InputPackets(refs)) != 0 {
			t.Fatal("batch packets did not match configured peer")
		}
	default:
		t.Fatal("unknown injection mode")
	}
	// UDP delivery order is not the assertion: each exact packet must arrive
	// once. Distinct payloads also catch stale/duplicate output across modes.
	seen := make([]bool, len(packets))
	timer := time.NewTimer(awg31Wait)
	defer timer.Stop()
	for range packets {
		select {
		case got := <-to.tun.output:
			match := -1
			for i, want := range packets {
				if !seen[i] && bytes.Equal(got, want) {
					match = i
					break
				}
			}
			if match < 0 {
				t.Fatal("received unexpected, corrupted, or duplicate application packet")
			}
			seen[match] = true
		case <-timer.C:
			t.Fatal("exact application packet transfer timed out")
		}
	}
}

func awg31Packet(source, destination byte, payloadSize int) []byte {
	packet := append(buildTestPacket(), make([]byte, payloadSize)...)
	packet[15], packet[19] = source, destination
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	binary.BigEndian.PutUint16(packet[24:26], uint16(8+payloadSize))
	for i := 28; i < len(packet); i++ {
		packet[i] = byte(i) ^ source ^ byte(payloadSize)
	}
	// Valid IPv4 header checksum; a zero UDP checksum is legal for IPv4.
	var sum uint32
	for i := 0; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(packet[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	binary.BigEndian.PutUint16(packet[10:12], ^uint16(sum))
	return packet
}
