package sub

import (
	"encoding/base64"
	"fmt"
	"net"
	"strconv"
	"strings"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

// A native mieru:// URL carries the complete client configuration. Unlike a
// mierus:// profile URL, it can be imported on a fresh Mieru installation.
func mieruFullConfigLink(address, username, password string, entries []struct {
	port, protocol string
}, mtu int, multiplexing, handshake string,
) (string, error) {
	address = strings.TrimSpace(strings.Trim(address, "[]"))
	if address == "" {
		return "", fmt.Errorf("Mieru server address is empty")
	}
	server := &pb.ServerEndpoint{}
	if ip := net.ParseIP(address); ip != nil {
		server.IpAddress = proto.String(ip.String())
	} else {
		server.DomainName = proto.String(address)
	}
	for _, entry := range entries {
		protocol, ok := pb.TransportProtocol_value[entry.protocol]
		if !ok {
			return "", fmt.Errorf("invalid Mieru transport %q", entry.protocol)
		}
		binding := &pb.PortBinding{Protocol: pb.TransportProtocol(protocol).Enum()}
		if strings.Contains(entry.port, "-") {
			binding.PortRange = proto.String(entry.port)
		} else {
			port, err := strconv.Atoi(entry.port)
			if err != nil {
				return "", err
			}
			binding.Port = proto.Int32(int32(port))
		}
		server.PortBindings = append(server.PortBindings, binding)
	}
	level, ok := pb.MultiplexingLevel_value[multiplexing]
	if !ok {
		return "", fmt.Errorf("invalid Mieru multiplexing %q", multiplexing)
	}
	mode, ok := pb.HandshakeMode_value[handshake]
	if !ok {
		return "", fmt.Errorf("invalid Mieru handshake %q", handshake)
	}
	config := &pb.ClientConfig{
		Profiles: []*pb.ClientProfile{{
			ProfileName: proto.String("default"),
			User:        &pb.User{Name: proto.String(username), Password: proto.String(password)},
			Servers:     []*pb.ServerEndpoint{server},
			Mtu:         proto.Int32(int32(mtu)),
			Multiplexing: &pb.MultiplexingConfig{
				Level: pb.MultiplexingLevel(level).Enum(),
			},
			HandshakeMode: pb.HandshakeMode(mode).Enum(),
		}},
		ActiveProfile: proto.String("default"),
		RpcPort:       proto.Int32(8964),
		Socks5Port:    proto.Int32(1080),
	}
	data, err := proto.Marshal(config)
	if err != nil {
		return "", err
	}
	return "mieru://" + base64.StdEncoding.EncodeToString(data), nil
}
