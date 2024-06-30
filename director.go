package rocket

import (
	"context"
	"fmt"
	"github.com/bytepowered/assert-go"
	"github.com/rocketmanapp/rocket-proxy/helper"
	"github.com/rocketmanapp/rocket-proxy/net"
	"github.com/rocketmanapp/rocket-proxy/proxy"
	stdnet "net"
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

type DirectServer struct {
	serverType proxy.ServerType
	serverOpts ServerOptions
	listener   proxy.Listener
	router     proxy.Router
	resolver   proxy.Resolver
	selector   proxy.ConnectorSelector
}

func NewGenericServer(opts ServerOptions) *DirectServer {
	assert.MustNotEmpty(opts.Mode, "server mode is empty")
	return &DirectServer{
		serverOpts: opts,
	}
}

func (s *DirectServer) Options() ServerOptions {
	return s.serverOpts
}

func (s *DirectServer) SetListener(listener proxy.Listener) {
	s.listener = listener
}

func (s *DirectServer) SetRouter(router proxy.Router) {
	s.router = router
}

func (s *DirectServer) SetResolver(resolver proxy.Resolver) {
	s.resolver = resolver
}

func (s *DirectServer) SetConnector(c proxy.Connector) {
	s.SetConnectorSelector(func(conn *net.Connection) (proxy.Connector, bool) {
		return c, true
	})
}

func (s *DirectServer) SetConnectorSelector(f proxy.ConnectorSelector) {
	s.selector = f
}

func (s *DirectServer) SetServerType(serverType proxy.ServerType) {
	s.serverType = serverType
}

func (s *DirectServer) Serve(servContext context.Context) error {
	assert.MustNotNil(s.listener, "server listener is nil")
	assert.MustNotNil(s.router, "server router is nil")
	assert.MustNotNil(s.selector, "server connector-selector is nil")
	return s.listener.Listen(servContext, func(connCtx context.Context, conn net.Connection) error {
		// Assert
		assert.MustTrue(connCtx != servContext, "conn context is the same ref as server context")
		assert.MustNotNil(conn.UserContext, "user context is nil")
		assert.MustNotEmpty(proxy.RequiredID(connCtx), "conn id is empty")
		if conn.Network == net.Network_TCP {
			_, isTcpConn := conn.ReadWriter.(*stdnet.TCPConn)
			assert.MustNotNil(isTcpConn, "conn read-writer is not type of *net.TCPConn")
		}
		// Route
		connCtx = context.WithValue(connCtx, proxy.CtxKeyProxyType, s.serverType)
		routed, rErr := s.router.Route(connCtx, &conn)
		if rErr != nil {
			return fmt.Errorf("server route: %w", rErr)
		}
		destNetwork := routed.Destination.Network
		destAddr := routed.Destination.Address
		// ---- check route values
		assert.MustTrue(routed.Destination.IsValid(), "routed destination is invalid")
		// ---- resolve dest addr
		if destNetwork == net.Network_TCP || destNetwork == net.Network_UDP {
			if destAddr.Family().IsDomain() {
				if ip, sErr := s.resolver.Resolve(connCtx, destAddr.Domain()); sErr != nil {
					return fmt.Errorf("server resolve: %w", sErr)
				} else {
					routed.Destination.Address = net.IPAddress(ip)
				}
			}
		}
		// Connect
		connector, ok := s.selector(&routed)
		assert.MustTrue(ok, "connector not found, network: %s", destNetwork)
		if dErr := connector.DialServe(connCtx, &routed); dErr == nil {
			return nil
		} else if helper.IsConnectionClosed(dErr) {
			return nil
		} else {
			msg := dErr.Error()
			if strings.Contains(msg, "use of closed network connection") {
				return nil
			}
			if strings.Contains(msg, "i/o timeout") {
				return nil
			}
			if strings.Contains(msg, "connection reset by peer") {
				return nil
			}
			return dErr
		}
	})
}

func assertServerModeValid(mode string) {
	valid := false
	switch strings.ToLower(mode) {
	case ServerModeForward, ServerModeMixin, ServerModeProxy:
		valid = true
	default:
		valid = false
	}
	assert.MustTrue(valid, "server mode is invalid, was: %s", mode)
}
