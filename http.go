package fluxway

import (
	"context"
	"fluxway/internal"
	"fluxway/net"
	"fluxway/proxy"
	"fluxway/proxy/http"
	"fluxway/proxy/route"
	"fluxway/proxy/tcp"
	"fmt"
)

var (
	_ proxy.Server = (*HttpServer)(nil)
)

type HttpOptions struct {
	Disabled bool `yaml:"disabled"`
	// TLS
	TLSCertFile string `yaml:"tls_cert_file"`
	TLSKeyFile  string `yaml:"tls_key_file"`
}

type HttpServer struct {
	isHttps bool
	options HttpOptions
	*DispatchServer
}

func NewHttpServer(serverOpts ServerOptions, httpOptions HttpOptions, isHttps bool) *HttpServer {
	return &HttpServer{
		isHttps:        isHttps,
		options:        httpOptions,
		DispatchServer: NewGenericServer(serverOpts),
	}
}

func (s *HttpServer) Init(ctx context.Context) error {
	serverOpts := s.Options()
	listener := http.NewHttpListener(s.isHttps)
	router := route.NewProxyRouter()
	tcpConnector := tcp.NewTcpConnector()
	hstrConnector := http.NewHrtpConnector()
	s.SetListener(listener)
	s.SetRouter(router)
	s.SetResolver(internal.NewDNSResolver())
	s.SetConnectorSelector(func(conn *net.Connection) (proxy.Connector, bool) {
		switch conn.Destination.Network {
		case net.Network_TCP:
			return tcpConnector, true
		case net.Network_HRTP:
			return hstrConnector, true
		default:
			return nil, false
		}
	})
	// Listener init
	var serverPort int
	if s.isHttps {
		serverPort = serverOpts.HttpsPort
		if len(s.options.TLSCertFile) < 3 {
			return fmt.Errorf("http.tls_cert_file is required in config")
		}
		if len(s.options.TLSKeyFile) < 3 {
			return fmt.Errorf("http.tls_key_file is required in config")
		}
	} else {
		serverPort = serverOpts.HttpPort
	}
	return listener.Init(proxy.ListenerOptions{
		Address: serverOpts.Bind,
		Port:    serverPort,
		// TLS
		TLSCertFile: s.options.TLSCertFile,
		TLSKeyFile:  s.options.TLSKeyFile,
	})
}
