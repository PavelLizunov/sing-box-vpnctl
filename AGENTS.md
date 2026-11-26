# Maintaining the network core

At each core update, and when the weekly upstream report flags relevant changes, review the sing-box stable release and development registries, Hiddify's sing-box, core, app, ray2sing and sing-tun, and the transport projects tracked in [the inventory](docs/vpnctl/upstreams.json). Inspect the registered Go types, build tags, module pins and server implementation at an immutable revision. Record the decision and evidence in [the transport guide](docs/vpnctl/transports.md) before moving the reviewed baseline.

Keep new transports behind the existing sing-box registries and dialer. Update the exact fork boundary in `release/FORK_OWNED` and exercise both the default release and the opt-in build. Compare claims about reachability with measurements on the actual network and distinguish those measurements from loopback interop.
