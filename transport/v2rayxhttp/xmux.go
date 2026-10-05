package v2rayxhttp

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/net/http2"
)

type xmuxConn interface {
	Close()
	IsClosed() bool
	roundTripper() http.RoundTripper
}

type xmuxClient struct {
	conn         xmuxConn
	manager      *xmuxManager
	openUsage    int
	leftUsage    int
	leftRequests int32
	unreusableAt time.Time

	consecFails atomic.Int32
	failing     atomic.Bool

	closed bool
	access sync.Mutex
}

func (c *xmuxClient) close() {
	c.access.Lock()
	defer c.access.Unlock()
	c.closed = true
	if c.openUsage <= 0 {
		c.conn.Close()
	}
}

func (c *xmuxClient) addOpenUsage(delta int) {
	c.access.Lock()
	defer c.access.Unlock()
	c.openUsage += delta
	if c.closed && c.openUsage <= 0 {
		c.conn.Close()
	}
}

func (c *xmuxClient) getOpenUsage() int {
	c.access.Lock()
	defer c.access.Unlock()
	return c.openUsage
}

func (c *xmuxClient) takeRequest() {
	atomic.AddInt32(&c.leftRequests, -1)
}

type http2XmuxConn struct {
	transport *http2.Transport
	closed    atomic.Bool
}

func (c *http2XmuxConn) Close() {
	if c.closed.Swap(true) {
		return
	}
	c.transport.CloseIdleConnections()
}

func (c *http2XmuxConn) IsClosed() bool {
	return c.closed.Load()
}

func (c *http2XmuxConn) roundTripper() http.RoundTripper {
	return c.transport
}

var (
	xmuxBreakerThreshold = int32(3)
	xmuxBackoffInitial   = 100 * time.Millisecond
	xmuxBackoffCap       = 3 * time.Second
)

func (c *xmuxClient) noteFailure() {
	if c.consecFails.Add(1) != xmuxBreakerThreshold {
		return
	}
	c.failing.Store(true)
	if c.manager != nil {
		c.manager.noteBreakerTrip()
	}
}

func (c *xmuxClient) noteSuccess() {
	c.consecFails.Store(0)
	if c.manager != nil {
		c.manager.resetBackoff()
	}
}

func (c *xmuxClient) roundTrip(request *http.Request) (*http.Response, error) {
	c.takeRequest()
	response, err := c.conn.roundTripper().RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusOK {
		c.noteFailure()
	}
	return response, err
}

type xmuxRelease struct {
	client *xmuxClient
	once   sync.Once
}

func newXmuxRelease(client *xmuxClient) *xmuxRelease {
	return &xmuxRelease{client: client}
}

func (r *xmuxRelease) release() {
	if r == nil || r.client == nil {
		return
	}
	r.once.Do(func() {
		r.client.addOpenUsage(-1)
	})
}

func (r *xmuxRelease) roundTrip(request *http.Request) (*http.Response, error) {
	return r.client.roundTrip(request)
}

func (r *xmuxRelease) noteSuccess() {
	if r == nil || r.client == nil {
		return
	}
	r.client.noteSuccess()
}

func (r *xmuxRelease) noteFailure() {
	if r == nil || r.client == nil {
		return
	}
	r.client.noteFailure()
}

func singleTransportXmux(transport http.RoundTripper) *xmuxManager {
	conn := &fixedXmuxConn{transport: transport}
	manager := newXmuxManager(xmuxConfig{}, func() xmuxConn { return conn })
	return manager
}

type fixedXmuxConn struct {
	transport http.RoundTripper
	closed    atomic.Bool
}

func (c *fixedXmuxConn) Close()                          { c.closed.Store(true) }
func (c *fixedXmuxConn) IsClosed() bool                  { return c.closed.Load() }
func (c *fixedXmuxConn) roundTripper() http.RoundTripper { return c.transport }

type xmuxConfig struct {
	maxConcurrency   intRange
	maxConnections   intRange
	cMaxReuseTimes   intRange
	hMaxRequestTimes intRange
	hMaxReusableSecs intRange
	keepAlivePeriod  time.Duration
}

type xmuxManager struct {
	config      xmuxConfig
	concurrency int
	connections int
	newConn     func() xmuxConn
	clients     []*xmuxClient
	access      sync.Mutex
	onEvent     func(format string, args ...any)

	backoffDelay time.Duration
	blockedUntil time.Time
	backoffArmed atomic.Bool
}

func (m *xmuxManager) noteBreakerTrip() {
	m.access.Lock()
	if m.backoffDelay == 0 {
		m.backoffDelay = xmuxBackoffInitial
	} else if m.backoffDelay *= 2; m.backoffDelay > xmuxBackoffCap {
		m.backoffDelay = xmuxBackoffCap
	}
	m.blockedUntil = timeNow().Add(m.backoffDelay)
	m.backoffArmed.Store(true)
	delay := m.backoffDelay
	m.access.Unlock()
	m.logf("xmux: breaker tripped, new-transport backoff %s", delay)
}

func (m *xmuxManager) resetBackoff() {
	if !m.backoffArmed.Load() {
		return
	}
	m.access.Lock()
	m.backoffDelay = 0
	m.blockedUntil = time.Time{}
	m.backoffArmed.Store(false)
	m.access.Unlock()
	m.logf("xmux: breaker backoff reset")
}

func newXmuxManager(config xmuxConfig, newConn func() xmuxConn) *xmuxManager {
	return &xmuxManager{
		config:      config,
		concurrency: config.maxConcurrency.rand(),
		connections: config.maxConnections.rand(),
		newConn:     newConn,
		clients:     make([]*xmuxClient, 0),
	}
}

func (m *xmuxManager) logf(format string, args ...any) {
	if m.onEvent != nil {
		m.onEvent(format, args...)
	}
}

func (m *xmuxManager) Close() {
	m.access.Lock()
	clients := m.clients
	m.clients = nil
	m.access.Unlock()
	for _, client := range clients {
		client.close()
	}
}

func (m *xmuxManager) newClientLocked() *xmuxClient {
	client := &xmuxClient{
		conn:      m.newConn(),
		manager:   m,
		leftUsage: -1,
	}
	if x := m.config.cMaxReuseTimes.rand(); x > 0 {
		client.leftUsage = x - 1
	}
	client.leftRequests = maxInt32
	if x := m.config.hMaxRequestTimes.rand(); x > 0 {
		client.leftRequests = int32(x)
	}
	if x := m.config.hMaxReusableSecs.rand(); x > 0 {
		client.unreusableAt = timeNow().Add(time.Duration(x) * time.Second)
	}
	m.clients = append(m.clients, client)
	m.logf("xmux: opened connection (pool=%d, left_usage=%d, left_requests=%d)",
		len(m.clients), client.leftUsage, atomic.LoadInt32(&client.leftRequests))
	return client
}

func (c *xmuxClient) evictCause() string {
	switch {
	case c.conn.IsClosed():
		return "closed"
	case c.failing.Load():
		return "failing"
	case c.leftUsage == 0:
		return "reuse"
	case atomic.LoadInt32(&c.leftRequests) <= 0:
		return "requests"
	case !c.unreusableAt.IsZero() && timeNow().After(c.unreusableAt):
		return "expired"
	}
	return ""
}

func (m *xmuxManager) getContext(ctx context.Context) (*xmuxClient, error) {
	for {
		client, wait := m.get()
		if client != nil {
			return client, nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *xmuxManager) get() (*xmuxClient, time.Duration) {
	m.access.Lock()
	defer m.access.Unlock()

	var evicted []*xmuxClient
	for i := 0; i < len(m.clients); {
		client := m.clients[i]
		if cause := client.evictCause(); cause != "" {
			m.clients = append(m.clients[:i], m.clients[i+1:]...)
			evicted = append(evicted, client)
			m.logf("xmux: evicted connection (cause=%s, pool=%d)", cause, len(m.clients))
		} else {
			i++
		}
	}
	for _, client := range evicted {
		client.close()
	}

	if len(m.clients) == 0 {
		if m.backoffArmed.Load() {
			if wait := m.blockedUntil.Sub(timeNow()); wait > 0 {
				return nil, wait
			}
		}
		return m.newClientLocked(), 0
	}
	if m.connections > 0 && len(m.clients) < m.connections {
		return m.newClientLocked(), 0
	}

	candidates := m.clients
	if m.concurrency > 0 {
		candidates = make([]*xmuxClient, 0, len(m.clients))
		for _, client := range m.clients {
			if client.getOpenUsage() < m.concurrency {
				candidates = append(candidates, client)
			}
		}
	}
	if len(candidates) == 0 {
		return m.newClientLocked(), 0
	}

	client := candidates[randIntn(len(candidates))]
	if client.leftUsage > 0 {
		client.leftUsage--
	}
	return client, 0
}

const maxInt32 = int32(^uint32(0) >> 1)

var (
	defaultXmuxMaxConcurrency   = intRange{1, 1}
	defaultXmuxHMaxRequestTimes = intRange{600, 900}
	defaultXmuxHMaxReusableSecs = intRange{1800, 3000}
)

func normalizeXmux(options *option.V2RayXHTTPXmuxOptions) (xmuxConfig, error) {
	if options == nil || *options == (option.V2RayXHTTPXmuxOptions{}) {
		return xmuxConfig{
			maxConcurrency:   defaultXmuxMaxConcurrency,
			hMaxRequestTimes: defaultXmuxHMaxRequestTimes,
			hMaxReusableSecs: defaultXmuxHMaxReusableSecs,
		}, nil
	}
	var (
		config xmuxConfig
		err    error
	)
	config.maxConcurrency, err = parseRangeOr(string(options.MaxConcurrency), "xmux.max_concurrency", intRange{})
	if err != nil {
		return xmuxConfig{}, err
	}
	config.maxConnections, err = parseRangeOr(string(options.MaxConnections), "xmux.max_connections", intRange{})
	if err != nil {
		return xmuxConfig{}, err
	}
	if config.maxConnections.max > 0 && config.maxConcurrency.max > 0 {
		return xmuxConfig{}, E.New("v2ray-xhttp: xmux.max_connections cannot be specified together with xmux.max_concurrency")
	}
	config.cMaxReuseTimes, err = parseRangeOr(string(options.CMaxReuseTimes), "xmux.c_max_reuse_times", intRange{})
	if err != nil {
		return xmuxConfig{}, err
	}
	config.hMaxRequestTimes, err = parseRangeOr(string(options.HMaxRequestTimes), "xmux.h_max_request_times", intRange{})
	if err != nil {
		return xmuxConfig{}, err
	}
	config.hMaxReusableSecs, err = parseRangeOr(string(options.HMaxReusableSecs), "xmux.h_max_reusable_secs", intRange{})
	if err != nil {
		return xmuxConfig{}, err
	}
	config.keepAlivePeriod = time.Duration(options.HKeepAlivePeriod) * time.Second
	return config, nil
}
