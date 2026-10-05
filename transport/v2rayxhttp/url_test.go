package v2rayxhttp

import (
	"context"
	"strings"
	"testing"

	M "github.com/sagernet/sing/common/metadata"
)

func newPathClient() *Client {
	meta, _ := normalizeMeta(metaOptions{}, modePacketUp)
	return &Client{
		scheme:       "https",
		host:         "example.com",
		serverAddr:   M.ParseSocksaddr("example.com:443"),
		path:         "/xhttp",
		paddingRange: intRange{0, 0},
		meta:         meta,
	}
}

func newHeaderSessionClient(path string) *Client {
	meta, _ := normalizeMeta(metaOptions{
		SessionPlacement: placementHeader,
		SeqPlacement:     placementQuery,
	}, modePacketUp)
	return &Client{
		scheme:       "https",
		host:         "example.com",
		serverAddr:   M.ParseSocksaddr("example.com:443"),
		path:         path,
		paddingRange: intRange{0, 0},
		meta:         meta,
	}
}

func TestRequestURLPaths(t *testing.T) {
	c := newPathClient()

	cases := []struct {
		name      string
		sessionID string
		seqStr    string
		want      string
	}{
		{"stream-one bare path (no sessionId)", "", "", "/xhttp/"},
		{"stream-up/packet-up download (sessionId)", "sid123", "", "/xhttp/sid123"},
		{"packet-up upload (sessionId + seq)", "sid123", "7", "/xhttp/sid123/7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := c.newRequest(context.Background(), "GET", tc.sessionID, tc.seqStr, nil)
			if err != nil {
				t.Fatalf("newRequest(%q,%q) error: %v", tc.sessionID, tc.seqStr, err)
			}
			if req.URL.Path != tc.want {
				t.Fatalf("newRequest(%q,%q).URL.Path = %q, want %q", tc.sessionID, tc.seqStr, req.URL.Path, tc.want)
			}
		})
	}
}

func TestTrailingSlashPreservedOffPath(t *testing.T) {
	c := newHeaderSessionClient("/upload/")

	cases := []struct {
		name      string
		sessionID string
		seqStr    string
		want      string
	}{
		{"download keeps trailing slash (session in header)", "sid123", "", "/upload/"},
		{"upload keeps trailing slash (session in header, seq in query)", "sid123", "7", "/upload/"},
		{"stream-one keeps configured path verbatim (session off path)", "", "", "/upload/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := c.newRequest(context.Background(), "GET", tc.sessionID, tc.seqStr, nil)
			if err != nil {
				t.Fatalf("newRequest(%q,%q) error: %v", tc.sessionID, tc.seqStr, err)
			}
			if req.URL.Path != tc.want {
				t.Fatalf("newRequest(%q,%q).URL.Path = %q, want %q", tc.sessionID, tc.seqStr, req.URL.Path, tc.want)
			}
		})
	}
}

func TestStreamOnePathPrefixMatchesServer(t *testing.T) {
	serverPath := func(configured string) string {
		if !strings.HasSuffix(configured, "/") {
			return configured + "/"
		}
		return configured
	}

	for _, configured := range []string{"/api/v1/feed", "/api/v1/feed/", "/"} {
		t.Run(configured, func(t *testing.T) {
			c := newPathClient()
			c.path = configured

			req, err := c.newRequest(context.Background(), "POST", "", "", nil)
			if err != nil {
				t.Fatalf("newRequest: %v", err)
			}
			want := serverPath(configured)
			if !strings.HasPrefix(req.URL.Path, want) {
				t.Fatalf("stream-one path %q does not match server prefix %q — server would 404", req.URL.Path, want)
			}
			if req.URL.Path != want {
				t.Fatalf("stream-one path = %q, want exactly %q (no sessionId)", req.URL.Path, want)
			}
		})
	}
}
