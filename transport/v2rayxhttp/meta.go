package v2rayxhttp

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/sagernet/sing-box/log"

	E "github.com/sagernet/sing/common/exceptions"
)

const (
	placementPath          = "path"
	placementQuery         = "query"
	placementHeader        = "header"
	placementCookie        = "cookie"
	placementBody          = "body"
	placementAuto          = "auto"
	placementQueryInHeader = "queryInHeader"
)

const (
	methodRepeatX  = "repeat-x"
	methodTokenish = "tokenish"
)

type intRange struct {
	min int
	max int
}

func (r intRange) rand() int {
	if r.max <= r.min {
		return r.min
	}
	return r.min + randIntn(r.max-r.min+1)
}

type metaConfig struct {
	sessionPlacement string
	sessionKey       string
	seqPlacement     string
	seqKey           string

	sessionTable  string
	sessionLength intRange

	uplinkDataPlacement string
	uplinkDataKey       string
	uplinkChunkSize     intRange
	uplinkHTTPMethod    string

	xPaddingObfsMode  bool
	xPaddingKey       string
	xPaddingHeader    string
	xPaddingPlacement string
	xPaddingMethod    string

	scMaxEachPostBytes   intRange
	scMinPostsIntervalMs intRange
}

func normalizeMeta(opts metaOptions, mode string) (metaConfig, error) {
	var (
		m   metaConfig
		err error
	)

	m.sessionPlacement = orDefault(opts.SessionPlacement, placementPath)
	if err := validatePlacement("session_placement", m.sessionPlacement, placementPath, placementQuery, placementHeader, placementCookie); err != nil {
		return m, err
	}
	m.sessionKey = resolveKey(opts.SessionKey, m.sessionPlacement, "X-Session", "x_session")

	m.seqPlacement = orDefault(opts.SeqPlacement, placementPath)
	if err := validatePlacement("seq_placement", m.seqPlacement, placementPath, placementQuery, placementHeader, placementCookie); err != nil {
		return m, err
	}
	m.seqKey = resolveKey(opts.SeqKey, m.seqPlacement, "X-Seq", "x_seq")

	if m.sessionPlacement == placementPath {
		for _, char := range opts.SessionTable {
			if strings.ContainsRune("/?#%", char) || unicode.IsSpace(char) {
				return m, E.New("v2ray-xhttp: session_table contains an invalid character for session_placement=path: ", strconv.QuoteRune(char))
			}
		}
	}
	if m.sessionTable, m.sessionLength, err = resolveSessionID(opts.SessionTable, opts.SessionLength); err != nil {
		return m, err
	}

	m.uplinkDataPlacement = orDefault(opts.UplinkDataPlacement, placementAuto)
	if err := validatePlacement("uplink_data_placement", m.uplinkDataPlacement, placementBody, placementAuto, placementHeader, placementCookie); err != nil {
		return m, err
	}
	if (m.uplinkDataPlacement == placementHeader || m.uplinkDataPlacement == placementCookie) && mode != modePacketUp {
		return m, E.New("v2ray-xhttp: uplink_data_placement can be ", m.uplinkDataPlacement, " only in packet-up mode")
	}
	m.uplinkDataKey = resolveUplinkDataKey(opts.UplinkDataKey, m.uplinkDataPlacement)

	m.uplinkHTTPMethod = strings.ToUpper(orDefault(opts.UplinkHTTPMethod, http.MethodPost))
	if m.uplinkHTTPMethod == http.MethodGet && mode != modePacketUp {
		log.StdLogger().Warn("v2ray-xhttp: uplink_http_method=GET is only valid in packet-up mode (mode=", mode, "); falling back to POST")
		m.uplinkHTTPMethod = http.MethodPost
	}

	if m.scMaxEachPostBytes, err = parseRangeOr(opts.ScMaxEachPostBytes, "sc_max_each_post_bytes", intRange{1000000, 1000000}); err != nil {
		return m, err
	}
	if m.scMinPostsIntervalMs, err = parseRangeOr(opts.ScMinPostsIntervalMs, "sc_min_posts_interval_ms", intRange{30, 30}); err != nil {
		return m, err
	}

	m.uplinkChunkSize, err = resolveUplinkChunkSize(opts.UplinkChunkSize, m.uplinkDataPlacement, m.scMaxEachPostBytes)
	if err != nil {
		return m, err
	}

	m.xPaddingObfsMode = opts.XPaddingObfsMode
	m.xPaddingKey = orDefault(opts.XPaddingKey, "x_padding")
	m.xPaddingHeader = orDefault(opts.XPaddingHeader, "X-Padding")
	m.xPaddingPlacement = orDefault(opts.XPaddingPlacement, placementQueryInHeader)
	if err := validatePlacement("x_padding_placement", m.xPaddingPlacement, placementCookie, placementHeader, placementQuery, placementQueryInHeader); err != nil {
		return m, err
	}
	m.xPaddingMethod = orDefault(opts.XPaddingMethod, methodRepeatX)
	switch m.xPaddingMethod {
	case methodRepeatX, methodTokenish:
	default:
		return m, E.New("v2ray-xhttp: unknown x_padding_method: ", m.xPaddingMethod)
	}

	return m, nil
}

type metaOptions struct {
	SessionPlacement     string
	SessionKey           string
	SeqPlacement         string
	SeqKey               string
	SessionTable         string
	SessionLength        string
	UplinkDataPlacement  string
	UplinkDataKey        string
	UplinkChunkSize      string
	UplinkHTTPMethod     string
	XPaddingObfsMode     bool
	XPaddingKey          string
	XPaddingHeader       string
	XPaddingPlacement    string
	XPaddingMethod       string
	ScMaxEachPostBytes   string
	ScMinPostsIntervalMs string
}

var predefinedSessionTables = map[string]string{
	"ALPHABET": "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	"Alphabet": "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz",
	"BASE36":   "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	"Base62":   "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz",
	"HEX":      "0123456789ABCDEF",
	"alphabet": "abcdefghijklmnopqrstuvwxyz",
	"base36":   "0123456789abcdefghijklmnopqrstuvwxyz",
	"hex":      "0123456789abcdef",
	"number":   "0123456789",
}

// Invariant: Session IDs need at least 2^31 combinations to prevent independent clients from sharing a server-side session.
const minSessionIDSpace int64 = 1 << 31

func resolveSessionID(table, length string) (string, intRange, error) {
	table = strings.TrimSpace(table)
	length = strings.TrimSpace(length)
	if table == "" && length == "" {
		return "", intRange{}, nil
	}
	if table == "" || length == "" {
		return "", intRange{}, E.New("v2ray-xhttp: session_table and session_length must be set together")
	}
	if predefined, ok := predefinedSessionTables[table]; ok {
		table = predefined
	}
	var seen [128]bool
	unique := make([]byte, 0, 128)
	for i := 0; i < len(table); i++ {
		if table[i] > unicode.MaxASCII {
			return "", intRange{}, E.New("v2ray-xhttp: session_table must be ASCII")
		}
		if !seen[table[i]] {
			seen[table[i]] = true
			unique = append(unique, table[i])
		}
	}
	table = string(unique)
	r, err := parseRange(length, "session_length")
	if err != nil {
		return "", intRange{}, err
	}
	if r.min <= 0 {
		return "", intRange{}, E.New("v2ray-xhttp: session_length floor must be above 0")
	}
	if !sessionIDSpaceSufficient(len(table), r.min) {
		return "", intRange{}, E.New("v2ray-xhttp: session_table/session_length yield fewer than ", minSessionIDSpace,
			" possible ids (alphabet ", len(table), " chars ^ min length ", r.min, "); widen either to avoid session collisions")
	}
	return table, r, nil
}

func sessionIDSpaceSufficient(size, length int) bool {
	if size <= 1 {
		return false
	}
	space := int64(1)
	for i := 0; i < length; i++ {
		space *= int64(size)
		if space >= minSessionIDSpace {
			return true
		}
	}
	return false
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func validatePlacement(field, value string, allowed ...string) error {
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return E.New("v2ray-xhttp: unsupported ", field, ": ", value)
}

func resolveKey(configured, placement, headerDefault, otherDefault string) string {
	if configured != "" {
		return configured
	}
	switch placement {
	case placementHeader:
		return headerDefault
	case placementQuery, placementCookie:
		return otherDefault
	default:
		return ""
	}
}

func resolveUplinkDataKey(configured, placement string) string {
	if configured != "" {
		return configured
	}
	switch placement {
	case placementHeader, placementAuto:
		return "X-Data"
	case placementCookie:
		return "x_data"
	default:
		return ""
	}
}

func resolveUplinkChunkSize(raw, placement string, scMaxEachPost intRange) (intRange, error) {
	if strings.TrimSpace(raw) != "" {
		r, err := parseRange(raw, "uplink_chunk_size")
		if err != nil {
			return intRange{}, err
		}
		return clampChunkFloor(r), nil
	}
	switch placement {
	case placementCookie:
		return intRange{2048, 3072}, nil
	case placementHeader:
		return intRange{3000, 4000}, nil
	default:
		return scMaxEachPost, nil
	}
}

func clampChunkFloor(r intRange) intRange {
	if r.min < 64 {
		r.min = 64
	}
	if r.max < r.min {
		r.max = r.min
	}
	return r
}

func parseRangeOr(raw, field string, def intRange) (intRange, error) {
	if strings.TrimSpace(raw) == "" {
		return def, nil
	}
	return parseRange(raw, field)
}

func parseRange(raw, field string) (intRange, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "-") {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return intRange{}, E.Cause(err, "parse "+field)
		}
		return intRange{v, v}, nil
	}
	parts := strings.SplitN(raw, "-", 2)
	minV, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return intRange{}, E.Cause(err, "parse "+field+" min")
	}
	maxV, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return intRange{}, E.Cause(err, "parse "+field+" max")
	}
	if maxV < minV {
		minV, maxV = maxV, minV
	}
	return intRange{minV, maxV}, nil
}

// Protocol: Path metadata puts the session ID before the sequence number because the server reads segments positionally.
func (c *Client) applyMeta(request *http.Request, basePath, sessionID, seqStr string) {
	m := &c.meta
	path := basePath

	// Quirk: Path placement requires a trailing slash even without a session ID because the server prefix-matches it.
	if sessionID == "" {
		path = barePathForStreamOne(path, m)
	} else {
		switch m.sessionPlacement {
		case placementPath:
			path = appendPathSegment(path, sessionID)
		case placementQuery:
			setQuery(request.URL, m.sessionKey, sessionID)
		case placementHeader:
			request.Header.Set(m.sessionKey, sessionID)
		case placementCookie:
			request.AddCookie(&http.Cookie{Name: m.sessionKey, Value: sessionID, Path: "/"})
		}
	}

	if seqStr != "" {
		switch m.seqPlacement {
		case placementPath:
			path = appendPathSegment(path, seqStr)
		case placementQuery:
			setQuery(request.URL, m.seqKey, seqStr)
		case placementHeader:
			request.Header.Set(m.seqKey, seqStr)
		case placementCookie:
			request.AddCookie(&http.Cookie{Name: m.seqKey, Value: seqStr, Path: "/"})
		}
	}

	request.URL.Path = path
}

func (c *Client) applyUplinkData(request *http.Request, payload []byte) {
	m := &c.meta
	switch m.uplinkDataPlacement {
	case placementHeader:
		for i, chunk := range chunkEncoded(payload, m.uplinkChunkSize) {
			request.Header.Set(fmt.Sprintf("%s-%d", m.uplinkDataKey, i), chunk)
		}
	case placementCookie:
		for i, chunk := range chunkEncoded(payload, m.uplinkChunkSize) {
			request.AddCookie(&http.Cookie{Name: fmt.Sprintf("%s_%d", m.uplinkDataKey, i), Value: chunk, Path: "/"})
		}
	default:
		request.Body = readCloser{&byteReader{data: payload}}
		request.Header.Set("Content-Type", "application/octet-stream")
		request.ContentLength = int64(len(payload))
		// Quirk: A replayable body allows the transport to retry uploads after graceful GOAWAY without killing the session.
		request.GetBody = func() (io.ReadCloser, error) {
			return readCloser{&byteReader{data: payload}}, nil
		}
	}
}

func chunkEncoded(payload []byte, size intRange) []string {
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	var chunks []string
	for len(encoded) > 0 {
		n := size.rand()
		if n <= 0 || n > len(encoded) {
			n = len(encoded)
		}
		chunks = append(chunks, encoded[:n])
		encoded = encoded[n:]
	}
	return chunks
}

var timeNow = time.Now

func barePathForStreamOne(path string, m *metaConfig) string {
	if path == "" {
		return "/"
	}
	if m.sessionPlacement != placementPath && m.seqPlacement != placementPath {
		return path
	}
	if !strings.HasSuffix(path, "/") {
		return path + "/"
	}
	return path
}

func appendPathSegment(path, seg string) string {
	if strings.HasSuffix(path, "/") {
		return path + seg
	}
	return path + "/" + seg
}

func setQuery(u *url.URL, key, value string) {
	if u.RawQuery == "" {
		u.RawQuery = url.QueryEscape(key) + "=" + url.QueryEscape(value)
		return
	}
	q := u.Query()
	if _, exists := q[key]; !exists {
		u.RawQuery += "&" + url.QueryEscape(key) + "=" + url.QueryEscape(value)
		return
	}
	q.Set(key, value)
	u.RawQuery = q.Encode()
}
