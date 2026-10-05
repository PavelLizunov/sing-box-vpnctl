package v2rayxhttp

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/tls"
	M "github.com/sagernet/sing/common/metadata"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type xrayLikeServer struct {
	uplinkSeen chan struct{}
	server     *http.Server
	addr       string
}

func newXrayLikeServer(t *testing.T) *xrayLikeServer {
	t.Helper()
	s := &xrayLikeServer{uplinkSeen: make(chan struct{})}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isUpload := r.URL.Query().Get("chunk_id") != "" || r.ContentLength != 0 || r.Method != http.MethodGet
		if isUpload {
			buffer := make([]byte, 1)
			r.Body.Read(buffer)
			select {
			case <-s.uplinkSeen:
			default:
				close(s.uplinkSeen)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		select {
		case <-s.uplinkSeen:
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		w.Write([]byte("downlink"))
		w.(http.Flusher).Flush()
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s.addr = listener.Addr().String()
	s.server = &http.Server{Handler: h2c.NewHandler(handler, &http2.Server{})}
	go s.server.Serve(listener)
	t.Cleanup(func() { s.server.Close() })
	return s
}

func h2cClient(t *testing.T, addr, mode string) *Client {
	t.Helper()
	meta, err := normalizeMeta(metaOptions{
		SessionPlacement: placementHeader,
		SessionKey:       "X-Upload-Token",
		SeqPlacement:     placementQuery,
		SeqKey:           "chunk_id",
	}, mode)
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
		path:         "/upload/",
		mode:         mode,
		headers:      make(http.Header),
		paddingRange: intRange{0, 0},
		meta:         meta,
	}
}

func TestDialDoesNotBlockOnDownloadResponse(t *testing.T) {
	for _, mode := range []string{modePacketUp, modeStreamUp} {
		t.Run(mode, func(t *testing.T) {
			server := newXrayLikeServer(t)
			client := h2cClient(t, server.addr, mode)

			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()

			dialed := make(chan net.Conn, 1)
			dialErr := make(chan error, 1)
			go func() {
				conn, err := client.DialContext(ctx)
				if err != nil {
					dialErr <- err
					return
				}
				dialed <- conn
			}()

			var conn net.Conn
			select {
			case conn = <-dialed:
			case err := <-dialErr:
				t.Fatalf("dial failed: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("dial blocked waiting for the download response — the deadlock is back")
			}
			defer conn.Close()

			if _, err := conn.Write([]byte("uplink")); err != nil {
				t.Fatalf("write: %v", err)
			}

			read := make(chan error, 1)
			go func() {
				buffer := make([]byte, len("downlink"))
				_, err := io.ReadFull(conn, buffer)
				read <- err
			}()
			select {
			case err := <-read:
				if err != nil {
					t.Fatalf("read downlink: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("downlink never arrived after the uplink")
			}
		})
	}
}
