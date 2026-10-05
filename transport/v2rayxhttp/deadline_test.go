package v2rayxhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

type hangingTransport struct {
	entered chan struct{}
	release chan struct{}
}

func newHangingTransport() *hangingTransport {
	return &hangingTransport{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (t *hangingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	select {
	case t.entered <- struct{}{}:
	default:
	}
	<-t.release
	return nil, errors.New("released")
}

func (t *hangingTransport) Close() error {
	close(t.release)
	return nil
}

func hangingClient(t *testing.T) (*Client, *hangingTransport) {
	t.Helper()
	meta, err := normalizeMeta(metaOptions{}, modeStreamOne)
	if err != nil {
		t.Fatalf("normalizeMeta: %v", err)
	}
	transport := newHangingTransport()
	client := &Client{
		scheme:     "https",
		host:       "example.com",
		serverAddr: M.ParseSocksaddr("example.com:443"),
		path:       "/xhttp",
		mode:       modeStreamOne,
		meta:       meta,
		xmux:       singleTransportXmux(transport),
	}
	return client, transport
}

type writeResult struct {
	n   int
	err error
}

func writeAsync(conn interface{ Write([]byte) (int, error) }) <-chan writeResult {
	done := make(chan writeResult, 1)
	go func() {
		n, err := conn.Write([]byte("handshake"))
		done <- writeResult{n, err}
	}()
	return done
}

func TestStreamOneWriteDeadlineUnblocksWrite(t *testing.T) {
	client, transport := hangingClient(t)
	defer transport.Close()

	xc, _ := client.xmux.get()
	conn, err := client.dialStreamOne(context.Background(), "", xc)
	if err != nil {
		t.Fatalf("dialStreamOne: %v", err)
	}
	defer conn.Close()

	<-transport.entered

	if err := conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatalf("SetWriteDeadline: %v", err)
	}

	select {
	case result := <-writeAsync(conn):
		if result.err == nil {
			t.Fatal("Write returned without error: the body has no reader, it must not succeed")
		}
		if !errors.Is(result.err, os.ErrDeadlineExceeded) {
			t.Fatalf("Write error = %v, want os.ErrDeadlineExceeded", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Write still blocked after the write deadline expired (zombie goroutine)")
	}
}

func TestStreamOneDialCancelUnblocksWrite(t *testing.T) {
	client, transport := hangingClient(t)
	defer transport.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	xc, _ := client.xmux.get()
	conn, err := client.dialStreamOne(ctx, "", xc)
	if err != nil {
		t.Fatalf("dialStreamOne: %v", err)
	}
	defer conn.Close()

	<-transport.entered

	done := writeAsync(conn)
	select {
	case result := <-done:
		t.Fatalf("Write returned before cancellation: n=%d err=%v", result.n, result.err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case result := <-done:
		if result.err == nil {
			t.Fatal("Write succeeded after the dial context was cancelled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling the dial context did not release the pending Write")
	}
}

type liveTransport struct {
	body io.ReadCloser
}

func (t *liveTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	go io.Copy(io.Discard, request.Body)
	return &http.Response{StatusCode: http.StatusOK, Body: t.body}, nil
}

func TestStreamOneCancelAfterStreamUpKeepsConnAlive(t *testing.T) {
	bodyReader, bodyWriter := io.Pipe()
	defer bodyWriter.Close()

	meta, err := normalizeMeta(metaOptions{}, modeStreamOne)
	if err != nil {
		t.Fatalf("normalizeMeta: %v", err)
	}
	client := &Client{
		scheme:     "https",
		host:       "example.com",
		serverAddr: M.ParseSocksaddr("example.com:443"),
		path:       "/xhttp",
		mode:       modeStreamOne,
		meta:       meta,
		xmux:       singleTransportXmux(&liveTransport{body: bodyReader}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	xc, _ := client.xmux.get()
	conn, err := client.dialStreamOne(ctx, "", xc)
	if err != nil {
		t.Fatalf("dialStreamOne: %v", err)
	}
	defer conn.Close()

	streamConn := conn.(*streamConn)
	<-streamConn.created
	cancel()

	select {
	case result := <-writeAsync(conn):
		if result.err != nil {
			t.Fatalf("Write on a live conn failed after the dial context was cancelled: %v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Write on a live conn blocked after the dial context was cancelled")
	}
}

func TestStreamOneDeadlineDoesNotBreakLiveConn(t *testing.T) {
	pipeReader, pipeWriter := io.Pipe()
	conn := newStreamConn(pipeReader, pipeWriter, M.ParseSocksaddr("example.com:443"), nil)
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		t.Fatalf("clearing the write deadline must be accepted: %v", err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatalf("clearing the read deadline must be accepted: %v", err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		t.Fatalf("clearing both deadlines must be accepted: %v", err)
	}
}
