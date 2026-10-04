package device

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func newAWGRekeyTestDevice() *Device {
	dev := &Device{log: NewLogger(LogLevelSilent, "")}
	dev.headers.init = &magicHeader{start: MessageInitiationType, end: MessageInitiationType}
	dev.headers.response = &magicHeader{start: MessageResponseType, end: MessageResponseType}
	dev.headers.cookie = &magicHeader{start: MessageCookieReplyType, end: MessageCookieReplyType}
	dev.headers.transport = &magicHeader{start: MessageTransportType, end: MessageTransportType}
	return dev
}

func TestAWGRekeyAfterTimeUAPI(t *testing.T) {
	dev := newAWGRekeyTestDevice()
	if got := dev.rekeyAfterTime(); got != RekeyAfterTime {
		t.Fatalf("default = %v, want %v", got, RekeyAfterTime)
	}
	for _, value := range []string{"45", "50-60", "0"} {
		if err := dev.IpcSet("rekey_after_time=" + value + "\n\n"); err != nil {
			t.Fatalf("IpcSet(%q): %v", value, err)
		}
		config, err := dev.IpcGet()
		if err != nil {
			t.Fatal(err)
		}
		if value != "0" && !strings.Contains(config, "rekey_after_time="+value+"\n") {
			t.Fatalf("IpcGet omitted rekey range %q", value)
		}
		for i := 0; i < 100; i++ {
			got := dev.rekeyAfterTime()
			switch value {
			case "45":
				if got != 45*time.Second {
					t.Fatalf("fixed timing = %v", got)
				}
			case "50-60":
				if got < 50*time.Second || got > 60*time.Second {
					t.Fatalf("range timing = %v", got)
				}
			case "0":
				if got != RekeyAfterTime || strings.Contains(config, "rekey_after_time=") {
					t.Fatalf("zero did not restore default: %v", got)
				}
			}
		}
	}
	for _, value := range []string{"-1", "60-50", "not-a-range", "4294967296"} {
		if err := dev.IpcSet("rekey_after_time=" + value + "\n\n"); err == nil {
			t.Errorf("accepted invalid timing %q", value)
		}
		if got := dev.rekeyAfterTime(); got != RekeyAfterTime {
			t.Errorf("invalid update changed timing: %v", got)
		}
	}
}

func TestAWGRekeyAfterTimeConcurrentUpdate(t *testing.T) {
	dev := newAWGRekeyTestDevice()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			got := dev.rekeyAfterTime()
			if got != RekeyAfterTime && (got < 50*time.Second || got > 60*time.Second) {
				t.Errorf("unexpected timing: %v", got)
				return
			}
		}
	}()
	for i := 0; i < 1000; i++ {
		if err := dev.IpcSet("rekey_after_time=50-60\n\n"); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
