//go:build with_mieru

package mieru

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/sagernet/sing-box/option"
	"google.golang.org/protobuf/proto"
)

const Type = option.MieruType

func transportProtocol(value string) (appctlpb.TransportProtocol, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "TCP":
		return appctlpb.TransportProtocol_TCP, nil
	case "UDP":
		return appctlpb.TransportProtocol_UDP, nil
	default:
		return appctlpb.TransportProtocol_UNKNOWN_TRANSPORT_PROTOCOL, fmt.Errorf("mieru: unsupported transport %q, use TCP or UDP", value)
	}
}

func portRangeBounds(value string) (int, int, error) {
	beginText, endText, found := strings.Cut(strings.TrimSpace(value), "-")
	if !found {
		return 0, 0, fmt.Errorf("mieru: invalid port_range %q, expected begin-end", value)
	}
	begin, err := strconv.Atoi(strings.TrimSpace(beginText))
	if err != nil {
		return 0, 0, fmt.Errorf("mieru: invalid port_range %q: %w", value, err)
	}
	end, err := strconv.Atoi(strings.TrimSpace(endText))
	if err != nil {
		return 0, 0, fmt.Errorf("mieru: invalid port_range %q: %w", value, err)
	}
	if begin < 1 || begin > 65535 {
		return 0, 0, fmt.Errorf("mieru: port_range begin %d is outside 1-65535", begin)
	}
	if end < 1 || end > 65535 {
		return 0, 0, fmt.Errorf("mieru: port_range end %d is outside 1-65535", end)
	}
	if begin > end {
		return 0, 0, fmt.Errorf("mieru: port_range begin %d is greater than end %d", begin, end)
	}
	return begin, end, nil
}

func buildPortBindings(bindings []option.MieruPortBinding, port uint16, portRange, transport string) ([]*appctlpb.PortBinding, error) {
	if len(bindings) > 0 {
		if port != 0 || portRange != "" {
			return nil, fmt.Errorf("mieru: set either port_bindings or port/port_range, not both")
		}
		result := make([]*appctlpb.PortBinding, 0, len(bindings))
		for index, binding := range bindings {
			if binding.Port != 0 && binding.PortRange != "" {
				return nil, fmt.Errorf("mieru: port_bindings[%d] sets both port and port_range", index)
			}
			if binding.Port == 0 && binding.PortRange == "" {
				return nil, fmt.Errorf("mieru: port_bindings[%d] sets neither port nor port_range", index)
			}
			protocol, err := transportProtocol(binding.Transport)
			if binding.Transport == "" {
				protocol, err = transportProtocol(transport)
			}
			if err != nil {
				return nil, fmt.Errorf("mieru: port_bindings[%d]: %w", index, err)
			}
			pbBinding := &appctlpb.PortBinding{Protocol: protocol.Enum()}
			if binding.Port != 0 {
				pbBinding.Port = proto.Int32(int32(binding.Port))
			} else {
				if _, _, err := portRangeBounds(binding.PortRange); err != nil {
					return nil, fmt.Errorf("mieru: port_bindings[%d]: %w", index, err)
				}
				pbBinding.PortRange = proto.String(strings.TrimSpace(binding.PortRange))
			}
			result = append(result, pbBinding)
		}
		return result, nil
	}
	if port != 0 && portRange != "" {
		return nil, fmt.Errorf("mieru: set either port or port_range, not both")
	}
	if port == 0 && portRange == "" {
		return nil, fmt.Errorf("mieru: missing port or port_range")
	}
	protocol, err := transportProtocol(transport)
	if err != nil {
		return nil, err
	}
	pbBinding := &appctlpb.PortBinding{Protocol: protocol.Enum()}
	if port != 0 {
		pbBinding.Port = proto.Int32(int32(port))
	} else {
		if _, _, err := portRangeBounds(portRange); err != nil {
			return nil, err
		}
		pbBinding.PortRange = proto.String(strings.TrimSpace(portRange))
	}
	return []*appctlpb.PortBinding{pbBinding}, nil
}

func handshakeMode(value string) (appctlpb.HandshakeMode, error) {
	switch strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), "-", "_")) {
	case "", "DEFAULT", "HANDSHAKE_DEFAULT":
		return appctlpb.HandshakeMode_HANDSHAKE_DEFAULT, nil
	case "STANDARD", "HANDSHAKE_STANDARD":
		return appctlpb.HandshakeMode_HANDSHAKE_STANDARD, nil
	case "NO_WAIT", "NOWAIT", "HANDSHAKE_NO_WAIT":
		return appctlpb.HandshakeMode_HANDSHAKE_NO_WAIT, nil
	default:
		return appctlpb.HandshakeMode_HANDSHAKE_DEFAULT, fmt.Errorf("mieru: unsupported handshake_mode %q, use HANDSHAKE_STANDARD or HANDSHAKE_NO_WAIT", value)
	}
}

func multiplexingLevel(value string) (appctlpb.MultiplexingLevel, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "DEFAULT", "MULTIPLEXING_DEFAULT":
		return appctlpb.MultiplexingLevel_MULTIPLEXING_DEFAULT, nil
	case "OFF", "MULTIPLEXING_OFF":
		return appctlpb.MultiplexingLevel_MULTIPLEXING_OFF, nil
	case "LOW", "MULTIPLEXING_LOW":
		return appctlpb.MultiplexingLevel_MULTIPLEXING_LOW, nil
	case "MIDDLE", "MEDIUM", "MULTIPLEXING_MIDDLE":
		return appctlpb.MultiplexingLevel_MULTIPLEXING_MIDDLE, nil
	case "HIGH", "MULTIPLEXING_HIGH":
		return appctlpb.MultiplexingLevel_MULTIPLEXING_HIGH, nil
	default:
		return appctlpb.MultiplexingLevel_MULTIPLEXING_DEFAULT, fmt.Errorf("mieru: unsupported multiplexing %q, use off, low, middle or high", value)
	}
}
