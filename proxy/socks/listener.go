package socks

import "C"
import (
	"context"
	"fluxway/internal"
	"fluxway/net"
	"fluxway/proxy"
	"fmt"
	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
	"io"
	stdnet "net"
	"strings"
)

var (
	_ proxy.Listener = (*Listener)(nil)
)

type socks5Handler func(context.Context, io.Writer, *socks5.Request) error

type Listener struct {
	*internal.TcpListener
	socks *socks5.Server
}

func NewSocksListener() *Listener {
	return &Listener{
		TcpListener: internal.NewTcpListener("socks", net.DefaultTcpOptions()),
		socks:       nil,
	}
}

func (t *Listener) Listen(serveCtx context.Context, handler proxy.ListenerHandler) error {
	t.socks = socks5.NewServer(
		socks5.WithConnectHandle(t.newSocksHandler(statute.CommandConnect, handler)),
		socks5.WithBindHandle(t.newSocksHandler(statute.CommandBind, handler)),
		socks5.WithAssociateHandle(t.newSocksHandler(statute.CommandAssociate, handler)),
		socks5.WithRewriter(nil), // Ensure: no rewrite default
		socks5.WithResolver(nil), // Ensure: no resolve default
	)
	return t.TcpListener.Listen(serveCtx, func(connCtx context.Context, conn net.Connection) error {
		return t.socks.ServeConn(connCtx, conn.TCPConn)
	})
}

func (t *Listener) newSocksHandler(cmd byte, handler proxy.ListenerHandler) socks5Handler {
	return func(connCtx context.Context, w io.Writer, r *socks5.Request) error {
		switch cmd {
		case statute.CommandConnect:
			return t.handleConnect(connCtx, w, r, handler)
		case statute.CommandAssociate:
			return t.handleAssociate(connCtx, w, r, handler)
		case statute.CommandBind:
			return t.handleBind(connCtx, w, r, handler)
		default:
			return t.handleNotSupported(connCtx, w, r)
		}
	}
}

func (t *Listener) handleConnect(connCtx context.Context, w io.Writer, r *socks5.Request, next proxy.ListenerHandler) error {
	var conn = w.(net.Conn)
	// Send success
	if err := socks5.SendReply(w, statute.RepSuccess, conn.LocalAddr()); err != nil {
		return fmt.Errorf("socks send reply: %w", err)
	}
	// Next
	var destAddr net.Address
	if r.DestAddr.FQDN != "" {
		destAddr = net.DomainAddress(r.DestAddr.FQDN)
	} else {
		destAddr = net.IPAddress(r.DestAddr.IP)
	}
	err := next(connCtx, net.Connection{
		Network:     t.Network(),
		Address:     net.IPAddress((conn.RemoteAddr().(*stdnet.TCPAddr)).IP),
		TCPConn:     conn.(*net.TCPConn),
		UserContext: context.Background(),
		Destination: net.Destination{
			Network: net.Network_TCP,
			Address: destAddr,
			Port:    net.Port(r.DestAddr.Port),
		},
		ReadWriter: conn,
	})
	// Complete
	if err != nil {
		msg := err.Error()
		resp := statute.RepHostUnreachable
		if strings.Contains(msg, "refused") {
			resp = statute.RepConnectionRefused
		} else if strings.Contains(msg, "network is unreachable") {
			resp = statute.RepNetworkUnreachable
		}
		if err := socks5.SendReply(w, resp, conn.LocalAddr()); err != nil {
			return fmt.Errorf("socks send reply, %v", err)
		}
		return err
	} else {
		return nil
	}
}

func (t *Listener) handleAssociate(connCtx context.Context, w io.Writer, r *socks5.Request, handler proxy.ListenerHandler) error {
	return t.handleNotSupported(connCtx, w, r)
}

func (t *Listener) handleBind(connCtx context.Context, w io.Writer, r *socks5.Request, _ proxy.ListenerHandler) error {
	return t.handleNotSupported(connCtx, w, r)
}

func (t *Listener) handleNotSupported(_ context.Context, w io.Writer, req *socks5.Request) error {
	if err := socks5.SendReply(w, statute.RepCommandNotSupported, nil); err != nil {
		return fmt.Errorf("socks send reply: %w", err)
	}
	return fmt.Errorf("socks unsupported command: %v", req.Command)
}
