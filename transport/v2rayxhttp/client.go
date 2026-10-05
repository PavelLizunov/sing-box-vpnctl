package v2rayxhttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	sHTTP "github.com/sagernet/sing/protocol/http"
	"github.com/sagernet/sing/service"

	"golang.org/x/net/http2"
)

const (
	modeAuto      = "auto"
	modePacketUp  = "packet-up"
	modeStreamUp  = "stream-up"
	modeStreamOne = "stream-one"
)

var _ adapter.V2RayClientTransport = (*Client)(nil)

type Client struct {
	ctx            context.Context
	dialer         N.Dialer
	serverAddr     M.Socksaddr
	xmux           *xmuxManager
	scheme         string
	host           string
	path           string
	mode           string
	headers        http.Header
	paddingRange   intRange
	meta           metaConfig
	realityEnabled bool
	noGRPCHeader   bool
}

func NewClient(ctx context.Context, dialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayXHTTPOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	mode := options.Mode
	if mode == "" {
		mode = modeAuto
	}
	switch mode {
	case modeAuto, modePacketUp, modeStreamUp, modeStreamOne:
	default:
		return nil, E.New("v2ray-xhttp: unknown mode: ", mode)
	}

	paddingRange, err := parseRangeOr(options.XPaddingBytes, "x_padding_bytes", intRange{100, 1000})
	if err != nil {
		return nil, err
	}

	meta, err := normalizeMeta(metaOptions{
		SessionPlacement:     options.SessionPlacement,
		SessionKey:           options.SessionKey,
		SeqPlacement:         options.SeqPlacement,
		SeqKey:               options.SeqKey,
		SessionTable:         options.SessionTable,
		SessionLength:        options.SessionLength,
		UplinkDataPlacement:  options.UplinkDataPlacement,
		UplinkDataKey:        options.UplinkDataKey,
		UplinkChunkSize:      options.UplinkChunkSize,
		UplinkHTTPMethod:     options.UplinkHTTPMethod,
		XPaddingObfsMode:     options.XPaddingObfsMode,
		XPaddingKey:          options.XPaddingKey,
		XPaddingHeader:       options.XPaddingHeader,
		XPaddingPlacement:    options.XPaddingPlacement,
		XPaddingMethod:       options.XPaddingMethod,
		ScMaxEachPostBytes:   options.ScMaxEachPostBytes,
		ScMinPostsIntervalMs: options.ScMinPostsIntervalMs,
	}, mode)
	if err != nil {
		return nil, err
	}

	xmuxConfig, err := normalizeXmux(options.Xmux)
	if err != nil {
		return nil, err
	}

	var (
		scheme       string
		newTransport func() *http2.Transport
	)
	if tlsConfig == nil {
		scheme = "http"
		newTransport = func() *http2.Transport {
			return &http2.Transport{
				AllowHTTP:       true,
				ReadIdleTimeout: xmuxConfig.keepAlivePeriod,
				DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.STDConfig) (net.Conn, error) {
					return dialer.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddr(addr))
				},
			}
		}
	} else {
		scheme = "https"
		if len(tlsConfig.NextProtos()) == 0 {
			tlsConfig.SetNextProtos([]string{http2.NextProtoTLS})
		}
		tlsDialer := tls.NewDialer(dialer, tlsConfig)
		newTransport = func() *http2.Transport {
			return &http2.Transport{
				ReadIdleTimeout: xmuxConfig.keepAlivePeriod,
				DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.STDConfig) (net.Conn, error) {
					return tlsDialer.DialTLSContext(ctx, M.ParseSocksaddr(addr))
				},
			}
		}
	}

	var host string
	if options.Host != "" {
		host = options.Host
	} else if tlsConfig != nil && tlsConfig.ServerName() != "" {
		host = tlsConfig.ServerName()
	} else {
		host = serverAddr.String()
	}

	// Quirk: Preserve trailing slashes because reverse proxies may redirect bare paths and download requests ignore redirects.
	path := options.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	headers := make(http.Header)
	for key, value := range options.Headers {
		headers[key] = value
	}

	xmux := newXmuxManager(xmuxConfig, func() xmuxConn {
		return &http2XmuxConn{transport: newTransport()}
	})
	if logFactory := service.FromContext[log.Factory](ctx); logFactory != nil {
		xmuxLogger := logFactory.NewLogger("xhttp")
		xmux.onEvent = func(format string, args ...any) {
			xmuxLogger.Debug(fmt.Sprintf(format, args...))
		}
	}

	return &Client{
		ctx:            ctx,
		dialer:         dialer,
		serverAddr:     serverAddr,
		xmux:           xmux,
		scheme:         scheme,
		host:           host,
		path:           path,
		mode:           mode,
		headers:        headers,
		paddingRange:   paddingRange,
		meta:           meta,
		realityEnabled: tlsConfigIsReality(tlsConfig),
		noGRPCHeader:   options.NoGRPCHeader,
	}, nil
}

func (c *Client) DialContext(ctx context.Context) (net.Conn, error) {
	sessionID := c.newSessionID()
	xmuxClient, err := c.xmux.getContext(ctx)
	if err != nil {
		return nil, err
	}
	xmuxClient.addOpenUsage(1)
	conn, err := c.dialMode(ctx, sessionID, xmuxClient)
	if err != nil {
		xmuxClient.addOpenUsage(-1)
		return nil, err
	}
	return conn, nil
}

func (c *Client) dialMode(ctx context.Context, sessionID string, xmuxClient *xmuxClient) (net.Conn, error) {
	switch c.mode {
	case modeAuto:
		if c.realityEnabled {
			return c.dialStreamOne(ctx, sessionID, xmuxClient)
		}
		return c.dialPacketUp(ctx, sessionID, xmuxClient)
	case modePacketUp:
		return c.dialPacketUp(ctx, sessionID, xmuxClient)
	case modeStreamUp:
		return c.dialStreamUp(ctx, sessionID, xmuxClient)
	case modeStreamOne:
		return c.dialStreamOne(ctx, sessionID, xmuxClient)
	default:
		return nil, E.New("v2ray-xhttp: unknown mode: ", c.mode)
	}
}

func (c *Client) Close() error {
	c.xmux.Close()
	return nil
}

func (c *Client) baseURL() (*url.URL, error) {
	u := &url.URL{
		Scheme: c.scheme,
		Host:   c.serverAddr.String(),
	}
	if err := sHTTP.URLSetPath(u, c.path); err != nil {
		return nil, E.Cause(err, "parse path")
	}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	return u, nil
}

func (c *Client) newRequest(ctx context.Context, method, sessionID, seqStr string, body interface{ Read([]byte) (int, error) }) (*http.Request, error) {
	u, err := c.baseURL()
	if err != nil {
		return nil, err
	}
	basePath := u.Path
	request := &http.Request{
		Method: method,
		URL:    u,
		Header: c.headers.Clone(),
		Host:   c.host,
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	c.applyMeta(request, basePath, sessionID, seqStr)
	c.applyXPadding(request)
	if body != nil {
		request.Body = readCloser{body}
	}
	return request.WithContext(ctx), nil
}

func (c *Client) newSessionID() string {
	if c.meta.sessionTable == "" {
		return newUUIDSessionID()
	}
	table := c.meta.sessionTable
	length := c.meta.sessionLength.min
	if c.meta.sessionLength.max > length {
		length += sessionRandomInt(c.meta.sessionLength.max - length + 1)
	}
	id := make([]byte, length)
	for i := range id {
		id[i] = table[sessionRandomInt(len(table))]
	}
	return string(id)
}

func sessionRandomInt(n int) int {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(err)
	}
	return int(value.Int64())
}

func newUUIDSessionID() string {
	var b [16]byte
	rand.Read(b[:])

	var buf [36]byte
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf[:])
}

type readCloser struct {
	r interface{ Read([]byte) (int, error) }
}

func (r readCloser) Read(p []byte) (int, error) { return r.r.Read(p) }
func (r readCloser) Close() error               { return nil }

func drainAndClose(body interface {
	Read([]byte) (int, error)
	Close() error
},
) {
	buffer := buf.Get(buf.BufferSize)
	for {
		if _, err := body.Read(buffer); err != nil {
			break
		}
	}
	buf.Put(buffer)
	_ = body.Close()
}
