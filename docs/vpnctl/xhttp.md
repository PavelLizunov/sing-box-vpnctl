# XHTTP transport options

A client transport for VLESS and similar outbounds, compatible with Xray "splithttp". It is set as `"transport": {"type": "xhttp", …}` and needs a build with the `with_xhttp` tag (all release builds have it).

It speaks HTTP/2 only: h2 with TLS, h2c without. Range values are written as `"min-max"` or as a single number.

## Basics

- `host` — the HTTP Host header. Default: the TLS server name, else the server address.
- `path` — the request path prefix. A trailing slash is kept as written.
- `headers` — extra request headers sent on every request.

## Modes

- `mode` — one of:
  - `packet-up`: upload as numbered POST requests, download as one long GET;
  - `stream-up`: one streamed POST for upload, one GET for download;
  - `stream-one`: one request carrying both directions;
  - `auto` (default): `stream-one` with REALITY, otherwise `packet-up`.
- `no_grpc_header` — `true` omits the gRPC content type that the streamed modes send by default. Reverse proxies often need that header to stream without buffering.

## Session and sequence placement

- `session_placement` — where the session id goes: `path` (default), `query`, `header` or `cookie`.
- `session_key` — its name when not in the path. Default `X-Session` for a header, `x_session` for a query or cookie.
- `seq_placement` — where the upload sequence number goes in `packet-up`: `path` (default), `query`, `header` or `cookie`.
- `seq_key` — its name when not in the path. Default `X-Seq` or `x_seq`.
- `session_table` — the alphabet for a custom session id: literal characters, or one of `hex`, `HEX`, `number`, `alphabet`, `Alphabet`, `ALPHABET`, `base36`, `BASE36`, `Base62`. Default: a UUID. With the session id in the path the alphabet may not contain `/`, `?`, `#`, `%` or whitespace.
- `session_length` — the length of a custom session id, a number or a range. Used only with `session_table`. The number of possible ids must exceed 2^31.

## Uplink data

- `uplink_http_method` — the method of upload requests. Default `POST`; `GET` is valid only in `packet-up`.
- `uplink_data_placement` — where the upload payload goes in `packet-up`: `body`, `auto` (default, same as `body`), `header` or `cookie`.
- `uplink_data_key` — the base header or cookie name for the payload. Default `X-Data` or `x_data`.
- `uplink_chunk_size` — the size of each header or cookie chunk, in base64 characters. Default `2048-3072` for a cookie and `3000-4000` for a header.

## Padding

- `x_padding_bytes` — the length of the random padding on every request. Default `100-1000`.
- `x_padding_obfs_mode` — `false` (default) carries the padding in the query of a Referer header; `true` enables the four options below.
- `x_padding_placement` — `queryInHeader` (default), `query`, `header` or `cookie`.
- `x_padding_key` — the query or cookie name. Default `x_padding`.
- `x_padding_header` — the header name. Default `X-Padding`.
- `x_padding_method` — how the padding is generated: `repeat-x` (default) or `tokenish`.

## Limits in packet-up

- `sc_max_each_post_bytes` — the largest upload POST. Default `1000000`.
- `sc_min_posts_interval_ms` — the smallest pause between upload POSTs. Default `30`.

## XMUX (connection reuse)

The `xmux` object controls how HTTP connections are shared and retired. Each range is rolled once per pool or per connection, not per request.

- `max_concurrency` — how many streams may share one connection. Cannot be combined with `max_connections`.
- `max_connections` — how many connections the pool holds.
- `c_max_reuse_times` — how many times a connection is handed out for a new stream before it is retired.
- `h_max_request_times` — how many HTTP requests a connection carries before it is retired. In `packet-up` every upload POST counts.
- `h_max_reusable_secs` — how long a connection stays reusable, in seconds.
- `h_keep_alive_period` — the HTTP/2 keep-alive ping period in seconds; a negative value disables pings.

Without an `xmux` object, or with an empty one, the defaults are `max_concurrency` 1, `h_max_request_times` `600-900` and `h_max_reusable_secs` `1800-3000`. As soon as any field is set, every unset range means "no limit".

## Accepted but ignored

Server-side or retired options, accepted so that shared configurations load: `sc_max_concurrent_posts`, `server_max_header_bytes`, `no_sse_header`, `sc_max_buffered_posts`, `sc_stream_up_server_secs`.

## Example

```json
{
  "type": "vless",
  "tag": "xhttp-out",
  "server": "example.com",
  "server_port": 443,
  "uuid": "00000000-0000-0000-0000-000000000000",
  "tls": { "enabled": true, "server_name": "example.com" },
  "transport": {
    "type": "xhttp",
    "path": "/xhttp",
    "mode": "packet-up",
    "x_padding_bytes": "100-1000",
    "xmux": { "max_concurrency": "16-32" }
  }
}
```

## Not supported

- HTTP/1.1 and HTTP/3.
- Xray `downloadSettings` (a separate route for the download side).
- A write deadline in `packet-up` mode. A read deadline that expires ends the connection.
- The server side: this is a client transport only.
