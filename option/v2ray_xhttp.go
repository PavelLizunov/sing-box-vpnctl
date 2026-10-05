package option

import "github.com/sagernet/sing/common/json/badoption"

type V2RayXHTTPOptions struct {
	Host          string                 `json:"host,omitempty"`
	Path          string                 `json:"path,omitempty"`
	Mode          string                 `json:"mode,omitempty"`
	Headers       badoption.HTTPHeader   `json:"headers,omitempty"`
	XPaddingBytes string                 `json:"x_padding_bytes,omitempty"`
	Xmux          *V2RayXHTTPXmuxOptions `json:"xmux,omitempty"`

	NoGRPCHeader bool `json:"no_grpc_header,omitempty"`

	SessionPlacement string `json:"session_placement,omitempty"`
	SessionKey       string `json:"session_key,omitempty"`
	SeqPlacement     string `json:"seq_placement,omitempty"`
	SeqKey           string `json:"seq_key,omitempty"`

	SessionTable  string `json:"session_table,omitempty"`
	SessionLength string `json:"session_length,omitempty"`

	UplinkDataPlacement string `json:"uplink_data_placement,omitempty"`
	UplinkDataKey       string `json:"uplink_data_key,omitempty"`
	UplinkChunkSize     string `json:"uplink_chunk_size,omitempty"`
	UplinkHTTPMethod    string `json:"uplink_http_method,omitempty"`

	XPaddingObfsMode  bool   `json:"x_padding_obfs_mode,omitempty"`
	XPaddingKey       string `json:"x_padding_key,omitempty"`
	XPaddingHeader    string `json:"x_padding_header,omitempty"`
	XPaddingPlacement string `json:"x_padding_placement,omitempty"`
	XPaddingMethod    string `json:"x_padding_method,omitempty"`

	ScMaxEachPostBytes   string `json:"sc_max_each_post_bytes,omitempty"`
	ScMinPostsIntervalMs string `json:"sc_min_posts_interval_ms,omitempty"`

	ScMaxConcurrentPosts int `json:"sc_max_concurrent_posts,omitempty"`

	ServerMaxHeaderBytes int    `json:"server_max_header_bytes,omitempty"`
	NoSSEHeader          bool   `json:"no_sse_header,omitempty"`
	ScMaxBufferedPosts   int64  `json:"sc_max_buffered_posts,omitempty"`
	ScStreamUpServerSecs string `json:"sc_stream_up_server_secs,omitempty"`
}

type V2RayXHTTPXmuxOptions struct {
	MaxConcurrency   XmuxRange `json:"max_concurrency,omitempty"`
	MaxConnections   XmuxRange `json:"max_connections,omitempty"`
	CMaxReuseTimes   XmuxRange `json:"c_max_reuse_times,omitempty"`
	HMaxRequestTimes XmuxRange `json:"h_max_request_times,omitempty"`
	HMaxReusableSecs XmuxRange `json:"h_max_reusable_secs,omitempty"`
	HKeepAlivePeriod int64     `json:"h_keep_alive_period,omitempty"`
}
