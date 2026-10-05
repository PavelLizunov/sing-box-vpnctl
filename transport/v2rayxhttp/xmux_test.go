package v2rayxhttp

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
)

type fakeXmuxConn struct {
	access     sync.Mutex
	closeCount int
	dead       bool
}

func (c *fakeXmuxConn) Close() {
	c.access.Lock()
	defer c.access.Unlock()
	c.closeCount++
}

func (c *fakeXmuxConn) IsClosed() bool {
	c.access.Lock()
	defer c.access.Unlock()
	return c.dead
}

func (c *fakeXmuxConn) roundTripper() http.RoundTripper { return nil }

func (c *fakeXmuxConn) closes() int {
	c.access.Lock()
	defer c.access.Unlock()
	return c.closeCount
}

func (c *fakeXmuxConn) kill() {
	c.access.Lock()
	defer c.access.Unlock()
	c.dead = true
}

func poolOf(t *testing.T, config xmuxConfig) (*xmuxManager, func() []*fakeXmuxConn) {
	t.Helper()
	var (
		access sync.Mutex
		conns  []*fakeXmuxConn
	)
	manager := newXmuxManager(config, func() xmuxConn {
		access.Lock()
		defer access.Unlock()
		conn := &fakeXmuxConn{}
		conns = append(conns, conn)
		return conn
	})
	return manager, func() []*fakeXmuxConn {
		access.Lock()
		defer access.Unlock()
		return append([]*fakeXmuxConn(nil), conns...)
	}
}

func TestXmuxReusesConnection(t *testing.T) {
	manager, conns := poolOf(t, xmuxConfig{maxConcurrency: intRange{4, 4}})
	first, _ := manager.get()
	first.addOpenUsage(1)
	second, _ := manager.get()
	if first != second {
		t.Fatal("second stream opened a new connection while the first had concurrency to spare")
	}
	if got := len(conns()); got != 1 {
		t.Fatalf("opened %d connections, want 1", got)
	}
}

func TestXmuxConcurrencyLimit(t *testing.T) {
	manager, conns := poolOf(t, xmuxConfig{maxConcurrency: intRange{1, 1}})
	first, _ := manager.get()
	first.addOpenUsage(1)
	second, _ := manager.get()
	if first == second {
		t.Fatal("a second stream joined a connection already at max_concurrency=1")
	}
	if got := len(conns()); got != 2 {
		t.Fatalf("opened %d connections, want 2", got)
	}
}

func TestXmuxMaxConnections(t *testing.T) {
	manager, conns := poolOf(t, xmuxConfig{maxConnections: intRange{2, 2}})
	manager.get()
	manager.get()
	if got := len(conns()); got != 2 {
		t.Fatalf("opened %d connections while below max_connections, want 2", got)
	}
	manager.get()
	if got := len(conns()); got != 2 {
		t.Fatalf("opened %d connections, want the pool to stop at max_connections=2", got)
	}
}

func TestXmuxEviction(t *testing.T) {
	t.Run("closed", func(t *testing.T) {
		manager, conns := poolOf(t, xmuxConfig{maxConcurrency: intRange{4, 4}})
		first, _ := manager.get()
		conns()[0].kill()
		if second, _ := manager.get(); first == second {
			t.Fatal("a dead connection was handed out again")
		}
	})
	t.Run("reuse", func(t *testing.T) {
		manager, _ := poolOf(t, xmuxConfig{
			maxConcurrency: intRange{4, 4},
			cMaxReuseTimes: intRange{1, 1},
		})
		first, _ := manager.get()
		if second, _ := manager.get(); first == second {
			t.Fatal("connection was handed out past c_max_reuse_times")
		}
	})
	t.Run("requests", func(t *testing.T) {
		manager, _ := poolOf(t, xmuxConfig{
			maxConcurrency:   intRange{4, 4},
			hMaxRequestTimes: intRange{2, 2},
		})
		first, _ := manager.get()
		first.takeRequest()
		first.takeRequest()
		if second, _ := manager.get(); first == second {
			t.Fatal("connection was handed out past h_max_request_times")
		}
	})
	t.Run("expired", func(t *testing.T) {
		manager, _ := poolOf(t, xmuxConfig{
			maxConcurrency:   intRange{4, 4},
			hMaxReusableSecs: intRange{1, 1},
		})
		first, _ := manager.get()
		restore := freezeTime(t, time.Now().Add(2*time.Second))
		defer restore()
		if second, _ := manager.get(); first == second {
			t.Fatal("connection was handed out past h_max_reusable_secs")
		}
	})
}

func TestXmuxDeferredClose(t *testing.T) {
	manager, conns := poolOf(t, xmuxConfig{maxConcurrency: intRange{4, 4}})
	client, _ := manager.get()
	client.addOpenUsage(1)

	client.close()
	if got := conns()[0].closes(); got != 0 {
		t.Fatalf("connection was closed with a live stream on it (closes=%d)", got)
	}

	client.addOpenUsage(-1)
	if got := conns()[0].closes(); got != 1 {
		t.Fatalf("connection was not closed after its last stream left (closes=%d)", got)
	}
}

func TestXmuxReleaseIsIdempotent(t *testing.T) {
	manager, conns := poolOf(t, xmuxConfig{maxConcurrency: intRange{4, 4}})
	client, _ := manager.get()
	client.addOpenUsage(1)
	release := newXmuxRelease(client)

	release.release()
	release.release()
	release.release()

	if got := client.getOpenUsage(); got != 0 {
		t.Fatalf("openUsage = %d after repeated release, want 0", got)
	}
	client.close()
	if got := conns()[0].closes(); got != 1 {
		t.Fatalf("connection closed %d times, want exactly 1", got)
	}
}

func TestXmuxReleaseNilIsSafe(t *testing.T) {
	var release *xmuxRelease
	release.release()
}

func TestXmuxManagerCloseKeepsLiveStreams(t *testing.T) {
	manager, conns := poolOf(t, xmuxConfig{maxConcurrency: intRange{4, 4}})
	client, _ := manager.get()
	client.addOpenUsage(1)

	manager.Close()
	if got := conns()[0].closes(); got != 0 {
		t.Fatalf("pool close tore down a connection with a live stream (closes=%d)", got)
	}
	client.addOpenUsage(-1)
	if got := conns()[0].closes(); got != 1 {
		t.Fatalf("connection was not closed after the stream left (closes=%d)", got)
	}
}

func TestXmuxEventLog(t *testing.T) {
	manager, conns := poolOf(t, xmuxConfig{
		maxConcurrency: intRange{4, 4},
		cMaxReuseTimes: intRange{1, 1},
	})
	var events []string
	manager.onEvent = func(format string, args ...any) {
		events = append(events, format)
	}
	manager.get()
	conns()[0].kill()
	manager.get()

	var opened, evicted int
	for _, event := range events {
		switch {
		case strings.Contains(event, "opened connection"):
			opened++
		case strings.Contains(event, "evicted connection"):
			evicted++
		}
	}
	if opened != 2 {
		t.Fatalf("logged %d connection openings, want 2", opened)
	}
	if evicted != 1 {
		t.Fatalf("logged %d evictions, want 1", evicted)
	}
}

func freezeTime(t *testing.T, at time.Time) func() {
	t.Helper()
	previous := timeNow
	timeNow = func() time.Time { return at }
	return func() { timeNow = previous }
}

func TestNormalizeXmuxDefaults(t *testing.T) {
	config, err := normalizeXmux(nil)
	if err != nil {
		t.Fatalf("normalizeXmux(nil): %v", err)
	}
	if config.maxConcurrency != (intRange{1, 1}) {
		t.Fatalf("max_concurrency = %v, want 1-1", config.maxConcurrency)
	}
	if config.hMaxRequestTimes != (intRange{600, 900}) {
		t.Fatalf("h_max_request_times = %v, want 600-900", config.hMaxRequestTimes)
	}
	if config.hMaxReusableSecs != (intRange{1800, 3000}) {
		t.Fatalf("h_max_reusable_secs = %v, want 1800-3000", config.hMaxReusableSecs)
	}
}

func TestNormalizeXmuxMutuallyExclusive(t *testing.T) {
	_, err := normalizeXmux(&option.V2RayXHTTPXmuxOptions{
		MaxConcurrency: "4",
		MaxConnections: "2",
	})
	if err == nil {
		t.Fatal("max_connections together with max_concurrency must be rejected")
	}
}

func TestNormalizeXmuxExplicit(t *testing.T) {
	config, err := normalizeXmux(&option.V2RayXHTTPXmuxOptions{
		MaxConcurrency:   "2-8",
		CMaxReuseTimes:   "5",
		HMaxRequestTimes: "10-20",
		HMaxReusableSecs: "60",
		HKeepAlivePeriod: 30,
	})
	if err != nil {
		t.Fatalf("normalizeXmux: %v", err)
	}
	if config.maxConcurrency != (intRange{2, 8}) {
		t.Fatalf("max_concurrency = %v, want 2-8", config.maxConcurrency)
	}
	if config.cMaxReuseTimes != (intRange{5, 5}) {
		t.Fatalf("c_max_reuse_times = %v, want 5-5", config.cMaxReuseTimes)
	}
	if config.hMaxRequestTimes != (intRange{10, 20}) {
		t.Fatalf("h_max_request_times = %v, want 10-20", config.hMaxRequestTimes)
	}
	if config.keepAlivePeriod != 30*time.Second {
		t.Fatalf("h_keep_alive_period = %v, want 30s", config.keepAlivePeriod)
	}
}

func TestNormalizeXmuxEmptySectionEqualsAbsent(t *testing.T) {
	config, err := normalizeXmux(&option.V2RayXHTTPXmuxOptions{})
	if err != nil {
		t.Fatalf("normalizeXmux: %v", err)
	}
	reference, err := normalizeXmux(nil)
	if err != nil {
		t.Fatalf("normalizeXmux(nil): %v", err)
	}
	if config != reference {
		t.Fatalf("empty section = %+v, absent section = %+v", config, reference)
	}
}

func TestNormalizeXmuxPartialSectionGetsNoDefaults(t *testing.T) {
	config, err := normalizeXmux(&option.V2RayXHTTPXmuxOptions{CMaxReuseTimes: "5"})
	if err != nil {
		t.Fatalf("normalizeXmux: %v", err)
	}
	if config.cMaxReuseTimes != (intRange{5, 5}) {
		t.Fatalf("c_max_reuse_times = %v, want 5-5", config.cMaxReuseTimes)
	}
	for name, r := range map[string]intRange{
		"max_concurrency":     config.maxConcurrency,
		"max_connections":     config.maxConnections,
		"h_max_request_times": config.hMaxRequestTimes,
		"h_max_reusable_secs": config.hMaxReusableSecs,
	} {
		if r != (intRange{}) {
			t.Fatalf("%s = %v, want unset (no per-field default in a partial section)", name, r)
		}
	}
}
