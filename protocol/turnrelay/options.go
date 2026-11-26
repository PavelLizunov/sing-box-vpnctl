//go:build with_turnrelay

package turnrelay

import (
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"
)

const (
	Type               = "turnrelay"
	defaultConnections = 2
	maxConnections     = 60
)

type Options struct {
	option.DialerOptions
	option.ServerOptions
	Provider          string `json:"provider,omitempty"`
	ServerType        string `json:"server_type,omitempty"`
	TURNServer        string `json:"turn_server,omitempty"`
	TURNUsername      string `json:"turn_username,omitempty"`
	TURNPassword      string `json:"turn_password,omitempty"`
	Password          string `json:"password,omitempty"`
	ServerFingerprint string `json:"server_fingerprint,omitempty"`
	Connections       int    `json:"connections,omitempty"`
	Mode              string `json:"mode,omitempty"`
	UDP               *bool  `json:"udp,omitempty"`
}

type runtimeConfig struct {
	server       netip.AddrPort
	turnServer   string
	turnUsername string
	turnPassword string
	password     string
	fingerprint  []byte
	connections  int
	udp          bool
	mode         string
}

func (o Options) build() (runtimeConfig, error) {
	provider := strings.TrimSpace(o.Provider)
	if provider != "" && provider != "static" {
		return runtimeConfig{}, errors.New("turnrelay: provider " + strconv.Quote(provider) + " is not supported; only explicit static TURN credentials are available")
	}
	serverType := strings.TrimSpace(o.ServerType)
	if serverType != "" && serverType != "exit" {
		return runtimeConfig{}, errors.New("turnrelay: server_type " + strconv.Quote(serverType) + " is not supported; this build supports exit only")
	}
	mode := strings.TrimSpace(o.Mode)
	if mode == "" {
		mode = "srtp"
	}
	if mode != "srtp" && mode != "dtls" {
		return runtimeConfig{}, errors.New("turnrelay: mode " + strconv.Quote(mode) + " is not supported; use srtp or dtls with a pinned certificate")
	}
	turnServer := strings.TrimSpace(o.TURNServer)
	if turnServer == "" {
		return runtimeConfig{}, errors.New("turnrelay: turn_server is required")
	}
	host, portText, err := net.SplitHostPort(turnServer)
	if err != nil {
		return runtimeConfig{}, errors.New("turnrelay: turn_server must be host:port: " + err.Error())
	}
	if host == "" {
		return runtimeConfig{}, errors.New("turnrelay: turn_server host is required")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return runtimeConfig{}, errors.New("turnrelay: turn_server port must be 1..65535")
	}
	if o.TURNUsername == "" || o.TURNPassword == "" {
		return runtimeConfig{}, errors.New("turnrelay: turn_username and turn_password are required")
	}
	if o.Password == "" {
		return runtimeConfig{}, errors.New("turnrelay: password is required for the exit server")
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(o.Server))
	if err != nil {
		return runtimeConfig{}, errors.New("turnrelay: server must be a literal IP address: " + err.Error())
	}
	if addr.IsUnspecified() || addr.Zone() != "" {
		return runtimeConfig{}, errors.New("turnrelay: server must be a specific IP address without a zone")
	}
	if o.ServerPort == 0 {
		return runtimeConfig{}, errors.New("turnrelay: server_port is required")
	}
	fingerprint, err := hex.DecodeString(strings.TrimSpace(o.ServerFingerprint))
	if err != nil || len(fingerprint) != 32 {
		return runtimeConfig{}, errors.New("turnrelay: server_fingerprint must be the server certificate SHA-256 as 64 hex characters")
	}
	connections := o.Connections
	if connections == 0 {
		connections = defaultConnections
	}
	if connections < 1 || connections > maxConnections {
		return runtimeConfig{}, errors.New("turnrelay: connections must be 1..60")
	}
	udp := true
	if o.UDP != nil {
		udp = *o.UDP
	}
	return runtimeConfig{
		server:       netip.AddrPortFrom(addr, o.ServerPort),
		turnServer:   turnServer,
		turnUsername: o.TURNUsername,
		turnPassword: o.TURNPassword,
		password:     o.Password,
		fingerprint:  fingerprint,
		connections:  connections,
		udp:          udp,
		mode:         mode,
	}, nil
}
