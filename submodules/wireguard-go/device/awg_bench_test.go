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

func BenchmarkHeaderProtectionCipher(b *testing.B) {
	dev := &Device{}
	var key HeaderCipherKey
	for i := range key {
		key[i] = byte(i)
	}
	k := new(HeaderCipherKey)
	*k = key
	dev.headerProtection.key.Store(k)
	salt := make([]byte, HeaderCipherNonceSize)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = dev.HeaderProtectionCipher(salt)
	}
}

func BenchmarkRandomTrailer(b *testing.B) {
	dev := &Device{}
	dev.randomTrailers.Store(true)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = dev.randomTrailer(100)
	}
}

