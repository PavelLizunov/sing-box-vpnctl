package v2rayxhttp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/tls"
	M "github.com/sagernet/sing/common/metadata"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

func stubClient(t *testing.T, mode string, transport http.RoundTripper) *Client {
	t.Helper()
	meta, err := normalizeMeta(metaOptions{}, mode)
	if err != nil {
		t.Fatalf("normalizeMeta: %v", err)
	}
	return &Client{
		ctx:          context.Background(),
		serverAddr:   M.ParseSocksaddr("127.0.0.1:443"),
		xmux:         singleTransportXmux(transport),
		scheme:       "http",
		host:         "example.com",
		path:         "/xhttp/",
		mode:         mode,
		headers:      make(http.Header),
		paddingRange: intRange{0, 0},
		meta:         meta,
	}
}

type errorRoundTripper struct {
	err error
}

func (rt errorRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return nil, rt.err
}

type statusRoundTripper struct {
	code int
}

func (rt statusRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: rt.code,
		Status:     http.StatusText(rt.code),
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}

type methodRoundTripper struct {
	get  http.RoundTripper
	post http.RoundTripper
}

func (rt methodRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodGet {
		return rt.get.RoundTrip(request)
	}
	return rt.post.RoundTrip(request)
}

type hangRoundTripper struct {
	observed atomic.Int32
}

func (rt *hangRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	<-request.Context().Done()
	rt.observed.Add(1)
	return nil, request.Context().Err()
}

func writeUnderTest(conn net.Conn, payload []byte) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, err := conn.Write(payload)
		result <- err
	}()
	return result
}

const writeFreeBudget = 3 * time.Second

func TestStreamOneWriteFreedOnRoundTripError(t *testing.T) {
	t.Parallel()
	dialErr := errors.New("pooled connection is dead")
	client := stubClient(t, modeStreamOne, errorRoundTripper{err: dialErr})
	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	select {
	case err := <-writeUnderTest(conn, []byte("vless request header")):
		if err == nil {
			t.Fatal("Write succeeded on a conn whose RoundTrip failed")
		}
		if !strings.Contains(err.Error(), dialErr.Error()) {
			t.Fatalf("Write error %q does not carry the RoundTrip failure %q", err, dialErr)
		}
	case <-time.After(writeFreeBudget):
		t.Fatal("Write still blocked after the raise failed — upload pipe was not broken")
	}
}

func TestStreamOneWriteFreedOnBadStatus(t *testing.T) {
	t.Parallel()
	client := stubClient(t, modeStreamOne, statusRoundTripper{code: http.StatusBadGateway})
	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	select {
	case err := <-writeUnderTest(conn, []byte("vless request header")):
		if err == nil {
			t.Fatal("Write succeeded on a conn whose raise answered 502")
		}
	case <-time.After(writeFreeBudget):
		t.Fatal("Write still blocked after a non-200 raise — upload pipe was not broken")
	}
}

func TestStreamUpWriteFreedOnDownloadError(t *testing.T) {
	t.Parallel()
	downErr := errors.New("download stream refused")
	hang := &hangRoundTripper{}
	client := stubClient(t, modeStreamUp, methodRoundTripper{
		get:  errorRoundTripper{err: downErr},
		post: hang,
	})
	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	select {
	case err := <-writeUnderTest(conn, []byte("vless request header")):
		if err == nil {
			t.Fatal("Write succeeded on a conn whose download raise failed")
		}
	case <-time.After(writeFreeBudget):
		t.Fatal("Write still blocked after the download raise failed — upload pipe was not broken")
	}
}

func TestStreamUpWriteCarriesUploadError(t *testing.T) {
	t.Parallel()
	upErr := errors.New("upload stream refused")
	hang := &hangRoundTripper{}
	client := stubClient(t, modeStreamUp, methodRoundTripper{
		get:  hang,
		post: errorRoundTripper{err: upErr},
	})
	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	select {
	case err := <-writeUnderTest(conn, []byte("vless request header")):
		if err == nil {
			t.Fatal("Write succeeded on a conn whose upload raise failed")
		}
		if !strings.Contains(err.Error(), upErr.Error()) {
			t.Fatalf("Write error %q lost the upload failure %q", err, upErr)
		}
	case <-time.After(writeFreeBudget):
		t.Fatal("Write still blocked after the upload raise failed")
	}
}

func echoServer(t *testing.T) string {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		if r.Method == http.MethodGet {
			w.Write([]byte("downlink"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		buffer := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buffer)
			if n > 0 {
				w.Write(buffer[:n])
				w.(http.Flusher).Flush()
			}
			if err != nil {
				return
			}
		}
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: h2c.NewHandler(handler, &http2.Server{})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return listener.Addr().String()
}

func liveH2CClient(t *testing.T, addr, mode string) *Client {
	t.Helper()
	meta, err := normalizeMeta(metaOptions{}, mode)
	if err != nil {
		t.Fatalf("normalizeMeta: %v", err)
	}
	return &Client{
		ctx:        context.Background(),
		serverAddr: M.ParseSocksaddr(addr),
		xmux: singleTransportXmux(&http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.STDConfig) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		}),
		scheme:       "http",
		host:         addr,
		path:         "/xhttp/",
		mode:         mode,
		headers:      make(http.Header),
		paddingRange: intRange{0, 0},
		meta:         meta,
	}
}

func TestStreamOneConnSurvivesDialContextExpiry(t *testing.T) {
	t.Parallel()
	addr := echoServer(t)
	client := liveH2CClient(t, addr, modeStreamOne)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer dialCancel()
	conn, err := client.DialContext(dialCtx)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("Write before deadline: %v", err)
	}
	banner := make([]byte, 5)
	if _, err := io.ReadFull(conn, banner); err != nil {
		t.Fatalf("Read before deadline: %v", err)
	}

	<-dialCtx.Done()
	time.Sleep(200 * time.Millisecond)

	if _, err := conn.Write([]byte("after deadline")); err != nil {
		t.Fatalf("Write after dial deadline: %v — dial ctx still bounds the live stream", err)
	}
	deadlineConn, _ := conn.(interface{ SetReadDeadline(time.Time) error })
	deadlineConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	probe := make([]byte, 1)
	if _, err := conn.Read(probe); err != nil {
		t.Fatalf("Read after dial deadline: %v — dial ctx still bounds the live stream", err)
	}
}

func TestPacketUpPostsSurviveDialContextExpiry(t *testing.T) {
	t.Parallel()
	addr := echoServer(t)
	client := liveH2CClient(t, addr, modePacketUp)

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer dialCancel()
	conn, err := client.DialContext(dialCtx)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("first post")); err != nil {
		t.Fatalf("Write before deadline: %v", err)
	}
	<-dialCtx.Done()
	time.Sleep(200 * time.Millisecond)
	if _, err := conn.Write([]byte("post after deadline")); err != nil {
		t.Fatalf("Write after dial deadline: %v — posts still ride the dial ctx", err)
	}
}

func TestPacketUpPostBounded(t *testing.T) {
	restore := packetUpPostTimeout
	packetUpPostTimeout = 500 * time.Millisecond
	t.Cleanup(func() { packetUpPostTimeout = restore })

	hang := &hangRoundTripper{}
	client := stubClient(t, modePacketUp, hang)
	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	select {
	case err := <-writeUnderTest(conn, []byte("payload")):
		if err == nil {
			t.Fatal("Write succeeded against a wedged pooled connection")
		}
	case <-time.After(writeFreeBudget):
		t.Fatal("Write not bounded: post still blocked on a wedged pooled connection")
	}
}

func TestPacketUpCloseAbortsPendingDownload(t *testing.T) {
	t.Parallel()
	hang := &hangRoundTripper{}
	client := stubClient(t, modePacketUp, hang)
	conn, err := client.DialContext(context.Background())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}

	conn.Close()
	deadline := time.Now().Add(writeFreeBudget)
	for hang.observed.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("pending download RoundTrip not aborted by Close — its context never died")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
