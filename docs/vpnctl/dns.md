# DNS recipes

These use standard sing-box DNS options; nothing here is specific to the fork. They are kept because the fork's users need them often.

## Reject HTTPS and SVCB records

Some networks reset TLS connections that use Encrypted Client Hello. Browsers learn about ECH from HTTPS and SVCB records, so rejecting those queries keeps them on plain TLS:

```json
{
  "dns": {
    "rules": [
      {
        "query_type": ["HTTPS", "SVCB"],
        "action": "reject"
      }
    ]
  }
}
```

## Resolve only through the tunnel

Send every query through a proxy outbound, so the local network sees no DNS traffic:

```json
{
  "dns": {
    "servers": [
      {
        "tag": "remote-dns",
        "type": "https",
        "server": "1.1.1.1",
        "detour": "proxy-out"
      }
    ],
    "final": "remote-dns"
  }
}
```

The full DNS reference is in the [upstream documentation](https://sing-box.sagernet.org/configuration/dns/).
