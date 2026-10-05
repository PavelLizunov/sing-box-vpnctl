package v2rayxhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type failingRT struct {
	errs     []error
	statuses []int
	calls    int
}

func (rt *failingRT) RoundTrip(*http.Request) (*http.Response, error) {
	i := rt.calls
	rt.calls++
	if i < len(rt.errs) && rt.errs[i] != nil {
		return nil, rt.errs[i]
	}
	status := http.StatusOK
	if i < len(rt.statuses) && rt.statuses[i] != 0 {
		status = rt.statuses[i]
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func testRequest(t *testing.T) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestBreakerTripsAndEvicts(t *testing.T) {
	manager, conns := poolOf(t, xmuxConfig{maxConcurrency: intRange{4, 4}})
	client, _ := manager.get()
	for i := int32(0); i < xmuxBreakerThreshold; i++ {
		if cause := client.evictCause(); cause != "" {
			t.Fatalf("evictCause before threshold = %q", cause)
		}
		client.noteFailure()
	}
	if cause := client.evictCause(); cause != "failing" {
		t.Fatalf("evictCause after threshold = %q, want failing", cause)
	}
	manager.resetBackoff()
	replacement, _ := manager.get()
	if replacement == client {
		t.Fatal("get() returned the failing connection")
	}
	if got := conns(); len(got) != 2 || got[0].closes() == 0 {
		t.Fatalf("want 2 conns with the first closed, got %d conns, first closes=%d", len(got), got[0].closes())
	}
}

func TestBreakerSuccessResetsStreak(t *testing.T) {
	manager, _ := poolOf(t, xmuxConfig{})
	client, _ := manager.get()
	client.noteFailure()
	client.noteFailure()
	client.noteSuccess()
	client.noteFailure()
	client.noteFailure()
	if client.failing.Load() {
		t.Fatal("tripped without threshold consecutive failures")
	}
	client.noteFailure()
	if !client.failing.Load() {
		t.Fatal("did not trip on threshold consecutive failures")
	}
}

func TestBackoffDoublesAndCaps(t *testing.T) {
	manager, _ := poolOf(t, xmuxConfig{})
	want := xmuxBackoffInitial
	for i := 0; i < 10; i++ {
		manager.noteBreakerTrip()
		manager.access.Lock()
		got := manager.backoffDelay
		manager.access.Unlock()
		if got != want {
			t.Fatalf("trip %d: backoff = %s, want %s", i+1, got, want)
		}
		if want *= 2; want > xmuxBackoffCap {
			want = xmuxBackoffCap
		}
	}
	manager.resetBackoff()
	manager.access.Lock()
	defer manager.access.Unlock()
	if manager.backoffDelay != 0 || manager.backoffArmed.Load() {
		t.Fatal("resetBackoff did not disarm")
	}
}

func TestBackoffGatesOnlyEmptyPool(t *testing.T) {
	base := time.Now()
	savedNow := timeNow
	timeNow = func() time.Time { return base }
	defer func() { timeNow = savedNow }()

	manager, _ := poolOf(t, xmuxConfig{maxConcurrency: intRange{4, 4}})
	manager.noteBreakerTrip()
	client, wait := manager.get()
	if client != nil || wait <= 0 {
		t.Fatalf("empty pool in window: got (%v, %s), want (nil, >0)", client, wait)
	}
	timeNow = func() time.Time { return base.Add(xmuxBackoffInitial + time.Millisecond) }
	client, _ = manager.get()
	if client == nil {
		t.Fatal("get() still blocked after the window elapsed")
	}
	manager.noteBreakerTrip()
	pooled, wait := manager.get()
	if pooled != client || wait != 0 {
		t.Fatalf("live connection not handed out inside window: (%v, %s)", pooled, wait)
	}
}

func TestGetContextWaitsOutWindow(t *testing.T) {
	savedInitial := xmuxBackoffInitial
	xmuxBackoffInitial = 10 * time.Millisecond
	defer func() { xmuxBackoffInitial = savedInitial }()

	manager, _ := poolOf(t, xmuxConfig{})
	manager.noteBreakerTrip()
	client, err := manager.getContext(context.Background())
	if err != nil || client == nil {
		t.Fatalf("getContext = (%v, %v), want client", client, err)
	}

	manager2, _ := poolOf(t, xmuxConfig{})
	manager2.noteBreakerTrip()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager2.getContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled getContext err = %v, want context.Canceled", err)
	}
}

func TestRoundTripFeedsBreaker(t *testing.T) {
	rt := &failingRT{
		errs:     []error{errors.New("reset"), nil, nil},
		statuses: []int{0, http.StatusBadGateway, http.StatusOK},
	}
	manager := singleTransportXmux(rt)
	client, _ := manager.get()

	client.roundTrip(testRequest(t))
	if got := client.consecFails.Load(); got != 1 {
		t.Fatalf("after transport error: consecFails = %d, want 1", got)
	}
	response, _ := client.roundTrip(testRequest(t))
	response.Body.Close()
	if got := client.consecFails.Load(); got != 2 {
		t.Fatalf("after 502: consecFails = %d, want 2", got)
	}
	response, _ = client.roundTrip(testRequest(t))
	response.Body.Close()
	if got := client.consecFails.Load(); got != 2 {
		t.Fatalf("after 200: consecFails = %d, want 2 (headers are not success)", got)
	}
}

func TestNoteReadClassification(t *testing.T) {
	manager, _ := poolOf(t, xmuxConfig{})
	client, _ := manager.get()
	client.noteFailure()
	breaker := connBreaker{xmux: newXmuxRelease(client)}

	breaker.noteRead(nil)
	if got := client.consecFails.Load(); got != 0 {
		t.Fatalf("after successful read: consecFails = %d, want 0", got)
	}
	breaker.noteRead(io.EOF)
	if got := client.consecFails.Load(); got != 0 {
		t.Fatalf("after EOF: consecFails = %d, want 0", got)
	}
	breaker.noteRead(errors.New("stream error: INTERNAL_ERROR"))
	if got := client.consecFails.Load(); got != 1 {
		t.Fatalf("after remote error: consecFails = %d, want 1", got)
	}
	breaker.localClosed.Store(true)
	breaker.noteRead(errors.New("http2: response body closed"))
	if got := client.consecFails.Load(); got != 1 {
		t.Fatalf("after local close: consecFails = %d, want 1 (unchanged)", got)
	}
}

func TestUplinkBodyGetBody(t *testing.T) {
	client := &Client{}
	request := testRequest(t)
	payload := []byte("uplink payload")
	client.applyUplinkData(request, payload)
	if request.GetBody == nil {
		t.Fatal("GetBody not set on body-placement upload")
	}
	for i := 0; i < 2; i++ {
		body, err := request.GetBody()
		if err != nil {
			t.Fatal(err)
		}
		replay, err := io.ReadAll(body)
		if err != nil || string(replay) != string(payload) {
			t.Fatalf("GetBody replay %d = %q, %v", i, replay, err)
		}
	}
}
