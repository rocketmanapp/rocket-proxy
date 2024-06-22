package fluxway

import (
	"context"
	"fluxway/common"
	"fluxway/net"
	"fluxway/proxy"
	"fluxway/proxy/tcp"
	"fluxway/proxy/udp"
	"github.com/bytepowered/assert-go"
	"github.com/sirupsen/logrus"
)

type Server struct {
	tag        string
	listener   proxy.Listener
	router     proxy.Router
	connectors map[net.Network]proxy.Connector
}

func NewServer(tag string) *Server {
	assert.MustNotEmpty(tag, "server tag is required")
	return &Server{
		tag: tag,
	}
}

func (s *Server) Init() error {
	s.listener = tcp.NewTcpListener()
	s.connectors = map[net.Network]proxy.Connector{
		net.Network_TCP: tcp.NewTcpConnector(),
		net.Network_UDP: udp.NewUdpConnector(),
	}
	s.router = proxy.NewStaticDispatcher()
	assert.MustNotNil(s.listener, "server %s listener is required", s.tag)
	assert.MustNotNil(s.router, "server %s router is required", s.tag)
	assert.MustNotNil(len(s.connectors) != 0, "server %s forwarder is required", s.tag)
	return s.listener.Init(proxy.ListenerOptions{
		Network: net.Network_TCP,
		Address: "0.0.0.0",
		Port:    9999,
	})
}

func (s *Server) Serve(servContext context.Context) error {
	return s.listener.Serve(servContext, func(ctx context.Context, conn net.Connection) {
		connID := common.NewID()
		ctx = proxy.ContextWithProxyType(
			proxy.ContextWithID(ctx, connID),
			s.listener.ProxyType(),
		)
		fields := logrus.Fields{
			"server":  s.tag,
			"network": s.listener.Network(),
			"source":  conn.Address,
			"id":      connID,
		}
		ctx = proxy.ContextWithConnection(ctx, &conn)
		// Route
		routed, err := s.router.Route(ctx, &conn)
		if err != nil {
			logrus.WithFields(fields).Errorf("router error: %s", err)
			return
		}
		assert.MustNotNil(routed.ReadWriter, "routed.read-writer is nil")
		assert.MustTrue(routed.Destination.IsValid(), "routed.dest is invalid")
		if s.listener.Network() == net.Network_TCP {
			assert.MustNotNil(routed.TCPConn, "routed.TCPConn is nil")
		} else {
			assert.MustNil(routed.TCPConn, "routed.TCPConn is not nil")
		}
		fields["destination"] = routed.Destination
		// Connect
		connector, ok := s.connectors[routed.Destination.Network]
		if !ok {
			logrus.WithFields(fields).Errorf("unsupported network type error, %s: %s", routed.Destination.Network, err)
		}
		if err := connector.DailServe(ctx, &routed); err != nil {
			logrus.WithFields(fields).Errorf("connector error: %s", err)
			return
		}
	})
}
