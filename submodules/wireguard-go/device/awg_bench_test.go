package device

import (
	"testing"
)

func BenchmarkDeterminePacketTypeAndPadding(b *testing.B) {
	dev := &Device{}
	dev.headers.init = &magicHeader{start: MessageInitiationType, end: MessageInitiationType}
	dev.headers.response = &magicHeader{start: MessageResponseType, end: MessageResponseType}
	dev.headers.cookie = &magicHeader{start: MessageCookieReplyType, end: MessageCookieReplyType}
	dev.headers.transport = &magicHeader{start: MessageTransportType, end: MessageTransportType}
	dev.paddings.transport = 20

	packet := make([]byte, 20+MessageTransportHeaderSize+100)
	packet[20] = byte(MessageTransportType)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = dev.DeterminePacketTypeAndPadding(packet, MessageUnknownType)
	}
}
