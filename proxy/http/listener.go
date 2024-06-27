package http

import (
	"context"
	"crypto/tls"
	"fluxway/helper"
	"fluxway/internal"
	"fluxway/net"
	"fluxway/proxy"
	"github.com/bytepowered/assert-go"
	"github.com/sirupsen/logrus"
	stdnet "net"
	"net/http"
	"strconv"
	"strings"
)

var (
	_ proxy.Listener = (*Listener)(nil)
)

type Listener struct {
	isHttps      bool
	listenerOpts proxy.ListenerOptions
	roundTripper http.RoundTripper
}

func NewHttpListener(isHttps bool) *Listener {
	return &Listener{
		isHttps: isHttps,
		roundTripper: &http.Transport{
			TLSClientConfig: &tls.Config{},
			Proxy:           http.ProxyFromEnvironment,
		},
	}
}

func (l *Listener) Network() net.Network {
	return net.Network_TCP
}

func (l *Listener) ProxyType() proxy.ProxyType {
	return proxy.ProxyType_HTTPS
}

func (l *Listener) Init(options proxy.ListenerOptions) error {
	l.listenerOpts = options
	return nil
}

func (l *Listener) Serve(serveCtx context.Context, handler proxy.ListenerHandler) error {
	addr := stdnet.JoinHostPort(l.listenerOpts.Address, strconv.Itoa(l.listenerOpts.Port))
	if l.isHttps {
		logrus.Infof("http: serve start, https, address: %s", addr)
	} else {
		logrus.Infof("http: serve start, address: %s", addr)
	}
	server := &http.Server{
		Addr:    addr,
		Handler: l.newServeHandler(handler),
		BaseContext: func(l stdnet.Listener) context.Context {
			return serveCtx
		},
		ConnContext: func(ctx context.Context, conn stdnet.Conn) context.Context {
			return internal.SetupTcpContextLogger(serveCtx, conn.(*net.TCPConn))
		},
	}
	defer func() {
		logrus.Infof("http serve stop, address: %s", addr)
		_ = server.Shutdown(serveCtx)
	}()
	if l.isHttps {
		return server.ListenAndServeTLS(l.listenerOpts.TLSCertFile, l.listenerOpts.TLSKeyFile)
	} else {
		return server.ListenAndServe()
	}
}

func (l *Listener) newServeHandler(handler proxy.ListenerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		proxy.Logger(r.Context()).Infof("http: %s %s", r.Method, r.RequestURI)

		// Auth: nop
		removeHopByHopHeaders(r.Header)

		if r.Method == "CONNECT" {
			l.handleConnectStream(w, r, handler)
		} else {
			l.handlePlainHttp(w, r, handler)
		}
	}
}

func (l *Listener) handleConnectStream(w http.ResponseWriter, r *http.Request, next proxy.ListenerHandler) {
	connCtx, connCancel := context.WithCancel(r.Context())
	defer connCancel()
	// Hijacker
	r = r.WithContext(connCtx)
	hijacker, ok := w.(http.Hijacker)
	assert.MustTrue(ok, "http: not support hijack")
	hijConn, _, hijErr := hijacker.Hijack()
	if hijErr != nil {
		_, _ = w.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		proxy.Logger(connCtx).Error("http: not support hijack")
		return
	}
	defer helper.Close(hijConn)

	// Phase hook
	connCtx = proxy.ContextWithHookFuncDialPhased(connCtx, func(ctx context.Context, conn *net.Connection) error {
		if _, hiwErr := hijConn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")); hiwErr != nil {
			if !helper.IsConnectionClosed(hiwErr) {
				proxy.Logger(connCtx).Errorf("http: write back ok response: %s", hiwErr)
			}
			return hiwErr
		}
		return nil
	})
	// Next
	addr, port, _ := parseHostToAddress(r.URL.Host)
	hErr := next(connCtx, net.Connection{
		Network:    l.Network(),
		Address:    net.IPAddress((hijConn.RemoteAddr().(*stdnet.TCPAddr)).IP),
		TCPConn:    hijConn.(*net.TCPConn),
		ReadWriter: hijConn,
		Destination: net.Destination{
			Network: net.Network_TCP,
			Address: addr,
			Port:    port,
		},
	})
	// Complete
	if hErr != nil {
		_, _ = hijConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		proxy.Logger(connCtx).Errorf("http: conn handle: %s", hErr)
	}
}

func (l *Listener) handlePlainHttp(w http.ResponseWriter, r *http.Request, next proxy.ListenerHandler) {
	defer helper.Close(r.Body)

	if r.URL.Host == "" || !r.URL.IsAbs() {
		// RFC 2068 (HTTP/1.1) requires URL to be absolute URL in HTTP proxy.
		response := &http.Response{
			Status:        "Bad Request",
			StatusCode:    400,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        http.Header(make(map[string][]string)),
			Body:          nil,
			ContentLength: 0,
			Close:         true,
		}
		response.Header.Set("Proxy-Connection", "close")
		response.Header.Set("Connection", "close")
		_ = response.Write(w)
		return
	}
	// Forward host
	if len(r.URL.Host) > 0 {
		r.Host = r.URL.Host
	}
	// Header
	removeHopByHopHeaders(r.Header)
	// ---- Prevent UA from being set to golang's default ones
	if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", "")
	}

	connCtx := r.Context()

	// Next
	addr, port, _ := parseHostToAddress(r.URL.Host)
	hErr := next(connCtx, net.Connection{
		Network:     l.Network(),
		Address:     net.ParseAddress(r.RemoteAddr),
		UserContext: setWithUserContext(context.Background(), w, r),
		TCPConn:     nil,
		ReadWriter:  nil,
		Destination: net.Destination{
			Network: net.Network_HRTP,
			Address: addr,
			Port:    port,
		},
	})
	// Complete
	if hErr != nil {
		_, _ = w.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		proxy.Logger(connCtx).Errorf("http: conn handle: %s", hErr)
	}

}

func parseHostToAddress(urlHost string) (addr net.Address, port net.Port, err error) {
	if strings.LastIndexByte(urlHost, ':') > 0 {
		h, p, e := stdnet.SplitHostPort(urlHost)
		if e != nil {
			return nil, 0, e
		}
		addr = net.ParseAddress(h)
		port = net.ParsePort(p, 80)
	} else {
		addr = net.ParseAddress(urlHost)
		port = net.Port(80)
	}
	return addr, port, nil
}

func removeHopByHopHeaders(header http.Header) {
	// Strip hop-by-hop header based on RFC:
	// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html#sec13.5.1
	// https://www.mnot.net/blog/2011/07/11/what_proxies_must_do

	header.Del("Proxy-Connection")
	header.Del("Proxy-Authenticate")
	header.Del("Proxy-Authorization")
	header.Del("TE")
	header.Del("Trailers")
	header.Del("Transfer-Encoding")
	header.Del("Upgrade")
	header.Del("Keep-Alive")

	connections := header.Get("Connection")
	header.Del("Connection")
	if connections == "" {
		return
	}
	for _, h := range strings.Split(connections, ",") {
		header.Del(strings.TrimSpace(h))
	}
}