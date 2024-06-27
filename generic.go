package fluxway

import (
	"context"
	"errors"
	"fluxway/common"
	"fluxway/net"
	"fluxway/proxy"
	"fmt"
	"github.com/bytepowered/assert-go"
	"io"
	"strings"
)

const (
	ServerModeMixin   string = "mixin"
	ServerModeProxy   string = "proxy"
	ServerModeForward string = "forward"
)

type ServerOptions struct {
	// Generic
	Mode     string `yaml:"mode"`
	AllowLan bool   `yaml:"allow_lan"`
	Bind     string `yaml:"bind"`
	// Http proxy server only
	HttpPort  int `yaml:"http_port"`
	HttpsPort int `yaml:"https_port"`
	// Socks proxy server only
	SocksPort int `yaml:"socks_port"`
}

type GenericServer struct {
	opts     ServerOptions
	listener proxy.Listener
	router   proxy.Router
	resolver proxy.Resolver
	selector proxy.ConnectorSelector
}

func NewGenericServer(opts ServerOptions) *GenericServer {
	assert.MustNotEmpty(opts.Mode, "server mode is required")
	return &GenericServer{
		opts: opts,
	}
}

func (s *GenericServer) Options() ServerOptions {
	return s.opts
}

func (s *GenericServer) SetListener(listener proxy.Listener) {
	s.listener = listener
}

func (s *GenericServer) SetRouter(router proxy.Router) {
	s.router = router
}

func (s *GenericServer) SetResolver(resolver proxy.Resolver) {
	s.resolver = resolver
}

func (s *GenericServer) SetConnector(c proxy.Connector) {
	s.SetConnectorSelector(func(conn *net.Connection) (proxy.Connector, bool) {
		return c, true
	})
}

func (s *GenericServer) SetConnectorSelector(f proxy.ConnectorSelector) {
	s.selector = f
}

func (s *GenericServer) Serve(servContext context.Context) error {
	assert.MustNotNil(s.listener, "server listener is nil")
	assert.MustNotNil(s.router, "server router is nil")
	assert.MustNotNil(s.selector, "server connector-selector is nil")
	return s.listener.Serve(servContext, func(connCtx context.Context, conn net.Connection) error {
		assert.MustTrue(connCtx != servContext, "server context must be a new context")
		connCtx = proxy.ContextWithProxyType(connCtx, s.listener.ProxyType())
		// Route
		routed, err := s.router.Route(connCtx, &conn)
		if err != nil {
			return fmt.Errorf("server route: %w", err)
		}
		assert.MustNotNil(routed.ReadWriter, "routed.readWriter is nil")
		assert.MustTrue(routed.Destination.IsValid(), "routed.Destination is invalid")
		if s.listener.Network() == net.Network_TCP {
			assert.MustNotNil(routed.TCPConn, "routed.TCPConn is required")
		} else {
			assert.MustNil(routed.TCPConn, "routed.TCPConn must be nil")
		}
		if routed.Destination.Address.Family().IsDomain() {
			if ip, err := s.resolver.Resolve(connCtx, routed.Destination.Address.Domain()); err != nil {
				return fmt.Errorf("server resolve: %w", err)
			} else {
				routed.Destination.Address = net.IPAddress(ip)
			}
		}
		// Connect
		connector, ok := s.selector(&routed)
		assert.MustTrue(ok, "invalid connector: %s", routed.Destination.Network)
		if err := connector.DialServe(connCtx, &routed); errors.Is(err, io.EOF) {
			return nil
		} else {
			return err
		}
	})
}

func ParseDestinationWith(network net.Network, addr common.CAddress) (net.Destination, error) {
	port, err := net.PortFromInt(uint32(addr.Port))
	if err != nil {
		return net.DestinationNotset, fmt.Errorf("invalid destination port: %d, error: %w", addr.Port, err)
	}
	return net.Destination{
		Network: network,
		Address: net.ParseAddress(addr.Address),
		Port:    port,
	}, nil
}

func AssertServerModeValid(mode string) {
	valid := false
	switch strings.ToLower(mode) {
	case ServerModeForward, ServerModeMixin, ServerModeProxy:
		valid = true
	default:
		valid = false
	}
	assert.MustTrue(valid, "server mode is invalid: %s", mode)
}
