package v2rayxhttp

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

type countingTransport struct {
	requests atomic.Int32
}

func (t *countingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.requests.Add(1)
	if request.Body != nil {
		io.Copy(io.Discard, request.Body)
		request.Body.Close()
	}
	if request.Method == http.MethodGet {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(newBlockingReader()),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

type blockingReader struct {
	release chan struct{}
}

func newBlockingReader() *blockingReader {
	return &blockingReader{release: make(chan struct{})}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	<-r.release
	return 0, io.EOF
}

func TestPacketUpUploadsCountAgainstRequestLimit(t *testing.T) {
	meta, err := normalizeMeta(metaOptions{
		ScMinPostsIntervalMs: "0",
	}, modePacketUp)
	if err != nil {
		t.Fatalf("normalizeMeta: %v", err)
	}
	transport := &countingTransport{}
	client := &Client{
		ctx:        context.Background(),
		scheme:     "https",
		host:       "example.com",
		serverAddr: M.ParseSocksaddr("example.com:443"),
		path:       "/xhttp",
		mode:       modePacketUp,
		meta:       meta,
		xmux:       singleTransportXmux(transport),
	}
	client.xmux.config.hMaxRequestTimes = intRange{4, 4}

	xmuxClient, _ := client.xmux.get()
	xmuxClient.addOpenUsage(1)
	conn, err := client.dialPacketUp(context.Background(), "session", xmuxClient)
	if err != nil {
		t.Fatalf("dialPacketUp: %v", err)
	}
	defer conn.Close()

	for i := 0; i < 3; i++ {
		if _, err := conn.Write([]byte("payload")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for transport.requests.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if got := transport.requests.Load(); got != 4 {
		t.Fatalf("transport saw %d requests, want 4 (1 download + 3 uploads)", got)
	}
	if left := atomic.LoadInt32(&xmuxClient.leftRequests); left != 0 {
		t.Fatalf("leftRequests = %d after 4 requests against a limit of 4, want 0", left)
	}
	if cause := xmuxClient.evictCause(); cause != "requests" {
		t.Fatalf("evictCause = %q, want %q — the connection must be retired once its request budget is spent", cause, "requests")
	}
}
