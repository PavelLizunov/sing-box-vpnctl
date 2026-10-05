package v2rayxhttp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

type reviewReadCloser struct {
	closed chan struct{}
	once   sync.Once
}

func (r *reviewReadCloser) Read([]byte) (int, error) {
	<-r.closed
	return 0, net.ErrClosed
}

func (r *reviewReadCloser) Close() error {
	r.once.Do(func() {
		close(r.closed)
	})
	return nil
}

func TestReviewExpiredReadDeadlineWins(t *testing.T) {
	type deadlineConn interface {
		net.Conn
		setupReader(io.ReadCloser, error)
	}
	tests := []struct {
		name string
		new  func(*testing.T) deadlineConn
	}{
		{
			name: "stream-one",
			new: func(t *testing.T) deadlineConn {
				reader, writer := io.Pipe()
				t.Cleanup(func() { reader.Close() })
				t.Cleanup(func() { writer.Close() })
				return newStreamConn(reader, writer, M.Socksaddr{}, nil)
			},
		},
		{
			name: "stream-up",
			new: func(t *testing.T) deadlineConn {
				reader, writer := io.Pipe()
				t.Cleanup(func() { reader.Close() })
				t.Cleanup(func() { writer.Close() })
				return newSplitConn(reader, writer, M.Socksaddr{}, nil)
			},
		},
		{
			name: "packet-up",
			new: func(t *testing.T) deadlineConn {
				return newPacketConn(context.Background(), &Client{}, "", M.Socksaddr{}, nil)
			},
		},
	}
	for _, test := range tests {
		for _, late := range []bool{false, true} {
			name := test.name + "/ready"
			if late {
				name = test.name + "/late"
			}
			t.Run(name, func(t *testing.T) {
				conn := test.new(t)
				defer conn.Close()
				body := &reviewReadCloser{closed: make(chan struct{})}
				defer body.Close()
				if !late {
					conn.setupReader(body, nil)
				}
				if err := conn.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
					t.Fatal(err)
				}
				if late {
					conn.setupReader(body, nil)
				}
				select {
				case <-body.closed:
				default:
					t.Fatal("expired deadline did not close the reader")
				}
				for i := 0; i < 8; i++ {
					n, err := conn.Read(make([]byte, 1))
					if n != 0 || !errors.Is(err, os.ErrDeadlineExceeded) {
						t.Fatalf("Read = (%d, %v), want (0, os.ErrDeadlineExceeded)", n, err)
					}
				}
			})
		}
	}
}

func TestReviewSupersededDeadlineCallbacks(t *testing.T) {
	for _, clear := range []bool{true, false} {
		name := "extend"
		if clear {
			name = "clear"
		}
		t.Run("read/"+name, func(t *testing.T) {
			fired := false
			deadline := newReadDeadline(func() { fired = true })
			defer deadline.stop()
			if err := deadline.set(time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			deadline.access.Lock()
			generation := deadline.generation
			deadline.access.Unlock()
			next := time.Now().Add(2 * time.Hour)
			if clear {
				next = time.Time{}
			}
			if err := deadline.set(next); err != nil {
				t.Fatal(err)
			}
			deadline.expire(generation)
			if deadline.isExpired() || fired {
				t.Fatal("superseded read callback expired the deadline")
			}
		})
		t.Run("write/"+name, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			deadline := writeDeadline{reader: reader}
			defer deadline.stop()
			if err := deadline.set(time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			deadline.access.Lock()
			generation := deadline.generation
			deadline.access.Unlock()
			next := time.Now().Add(2 * time.Hour)
			if clear {
				next = time.Time{}
			}
			if err := deadline.set(next); err != nil {
				t.Fatal(err)
			}
			deadline.expire(generation)
			writeDone := writeAsync(writer)
			readDone := make(chan writeResult, 1)
			go func() {
				n, err := reader.Read(make([]byte, len("handshake")))
				readDone <- writeResult{n, err}
			}()
			select {
			case result := <-readDone:
				if result.err != nil || result.n != len("handshake") {
					t.Fatalf("Read = (%d, %v), want an intact pipe", result.n, result.err)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatal("pipe Read blocked after superseding the deadline")
			}
			select {
			case result := <-writeDone:
				if result.err != nil || result.n != len("handshake") {
					t.Fatalf("Write = (%d, %v), want an intact pipe", result.n, result.err)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatal("pipe Write blocked after superseding the deadline")
			}
		})
	}
}

func TestReviewLocalCancellationDoesNotTripBreaker(t *testing.T) {
	for _, expired := range []bool{false, true} {
		name := "cancel"
		if expired {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if expired {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			} else {
				cancel()
			}
			defer cancel()
			remoteErr := errors.New("remote reset")
			rt := &failingRT{errs: []error{ctx.Err(), remoteErr}}
			manager := singleTransportXmux(rt)
			defer manager.Close()
			client, _ := manager.get()
			if _, err := client.roundTrip(testRequest(t).WithContext(ctx)); !errors.Is(err, ctx.Err()) {
				t.Fatalf("local error = %v, want %v", err, ctx.Err())
			}
			if got := client.consecFails.Load(); got != 0 {
				t.Fatalf("after local cancellation: consecFails = %d, want 0", got)
			}
			if _, err := client.roundTrip(testRequest(t)); err != remoteErr {
				t.Fatalf("remote error = %v, want %v", err, remoteErr)
			}
			if got := client.consecFails.Load(); got != 1 {
				t.Fatalf("after remote failure: consecFails = %d, want 1", got)
			}
		})
	}
}

func TestReviewPacketPostFailureIsTerminal(t *testing.T) {
	remoteErr := errors.New("POST reset")
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "transport", err: remoteErr},
		{name: "status", status: http.StatusBadGateway},
	}
	for _, test := range tests {
		for _, late := range []bool{false, true} {
			name := test.name + "/ready"
			if late {
				name = test.name + "/late"
			}
			t.Run(name, func(t *testing.T) {
				meta, err := normalizeMeta(metaOptions{ScMinPostsIntervalMs: "0"}, modePacketUp)
				if err != nil {
					t.Fatal(err)
				}
				rt := &failingRT{
					errs:     []error{test.err},
					statuses: []int{test.status},
				}
				client := &Client{
					scheme:     "http",
					host:       "example.invalid",
					serverAddr: M.ParseSocksaddr("example.invalid:80"),
					path:       "/xhttp",
					mode:       modePacketUp,
					meta:       meta,
					xmux:       singleTransportXmux(rt),
				}
				defer client.Close()
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				xc, _ := client.xmux.get()
				xc.addOpenUsage(1)
				conn := newPacketConn(ctx, client, "session", client.serverAddr, newXmuxRelease(xc))
				conn.cancel = cancel
				defer conn.Close()
				body := &reviewReadCloser{closed: make(chan struct{})}
				defer body.Close()
				if !late {
					conn.setupReader(body, nil)
				}
				n, firstErr := conn.Write([]byte("packet"))
				if n != 0 || firstErr == nil {
					t.Fatalf("first Write = (%d, %v), want a POST failure", n, firstErr)
				}
				if test.err != nil && firstErr != test.err {
					t.Fatalf("first error = %v, want %v", firstErr, test.err)
				}
				if ctx.Err() != context.Canceled {
					t.Fatalf("connection context error = %v, want context.Canceled", ctx.Err())
				}
				if err := conn.fail(errors.New("download canceled")); err != firstErr {
					t.Fatalf("secondary failure = %v, want the first error %v", err, firstErr)
				}
				if late {
					conn.setupReader(body, nil)
				}
				select {
				case <-body.closed:
				default:
					t.Fatal("POST failure did not close the download reader")
				}
				conn.Close()
				for _, payload := range [][]byte{[]byte("later"), nil} {
					n, err := conn.Write(payload)
					if n != 0 || err != firstErr {
						t.Fatalf("later Write = (%d, %v), want (0, %v)", n, err, firstErr)
					}
				}
				if rt.calls != 1 {
					t.Fatalf("RoundTrip calls = %d, want 1", rt.calls)
				}
			})
		}
	}
}

func TestReviewSetQueryReplacesEncodedKey(t *testing.T) {
	u := &url.URL{RawQuery: "keep=%2F&%78_session=old&x_session=duplicate"}
	setQuery(u, "x_session", "new value")
	query := u.Query()
	if values := query["x_session"]; len(values) != 1 || values[0] != "new value" {
		t.Fatalf("x_session values = %v, want [new value]", values)
	}
	if query.Get("keep") != "/" {
		t.Fatalf("keep = %q, want /", query.Get("keep"))
	}
	u.RawQuery = "keep=%2f&%78_session=old"
	before := u.RawQuery
	setQuery(u, "missing", "a b")
	if want := before + "&missing=a+b"; u.RawQuery != want {
		t.Fatalf("absent-key RawQuery = %q, want %q", u.RawQuery, want)
	}
}

func TestReviewSessionAlphabetRejectsUnsafePathCharacters(t *testing.T) {
	unsafe := []string{"/", "?", "#", "%", " ", "\t", "\n", "\r", "\v", "\f"}
	for _, char := range unsafe {
		opts := metaOptions{
			SessionPlacement: placementPath,
			SessionTable:     "ab" + char + "cd",
			SessionLength:    "32",
		}
		if _, err := normalizeMeta(opts, modePacketUp); err == nil {
			t.Fatalf("path alphabet containing %q was accepted", char)
		} else if !strings.Contains(err.Error(), "session_table") || !strings.Contains(err.Error(), "path") {
			t.Fatalf("unclear configuration error for %q: %v", char, err)
		}
		opts.SessionPlacement = placementQuery
		if _, err := normalizeMeta(opts, modePacketUp); err != nil {
			t.Fatalf("query alphabet containing %q was rejected: %v", char, err)
		}
	}
	for _, table := range []string{" abcd", "abcd "} {
		if _, err := normalizeMeta(metaOptions{
			SessionTable:  table,
			SessionLength: "32",
		}, modePacketUp); err == nil {
			t.Fatalf("path alphabet containing surrounding whitespace %q was accepted", table)
		}
	}
	if _, err := normalizeMeta(metaOptions{
		SessionTable:  "Base62",
		SessionLength: "8",
	}, modePacketUp); err != nil {
		t.Fatalf("safe predefined alphabet was rejected: %v", err)
	}
}
