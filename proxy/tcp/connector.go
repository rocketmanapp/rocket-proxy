package tcp

import (
	"context"
	"fluxway/helper"
	"fluxway/net"
	"fluxway/proxy"
	"fmt"
	"github.com/bytepowered/assert-go"
	stdnet "net"
	"time"
)

var (
	_ proxy.Connector = (*Connector)(nil)
)

type Connector struct {
	opts net.TcpOptions
}

func NewTcpConnector() *Connector {
	return &Connector{
		opts: net.TcpOptions{
			ReadTimeout:  time.Second * 30,
			WriteTimeout: time.Second * 10,
			ReadBuffer:   1024,
			WriteBuffer:  1024,
			NoDelay:      true,
			KeepAlive:    time.Second * 10,
		},
	}
}

func (c *Connector) DialServe(srcConnCtx context.Context, link *net.Connection) error {
	assert.MustTrue(link.Destination.Network == net.Network_TCP, "dest network is not tcp, was: %s", link.Destination.Network)
	assert.MustTrue(link.Destination.Address.Family().IsIP(), "dest addr is not an ip, was: %s", link.Destination.Address.String())
	srcConn := link.TCPConn
	dstConn, err := stdnet.DialTCP("tcp", nil, &stdnet.TCPAddr{IP: link.Destination.Address.IP(), Port: int(link.Destination.Port)})
	if err != nil {
		return fmt.Errorf("tcp-dial: %w", err)
	}
	defer helper.Close(dstConn)
	if err := net.SetTcpOptions(dstConn, c.opts); err != nil {
		return fmt.Errorf("tcp-dial: set options: %w", err)
	}
	dstCtx, dstCancel := context.WithCancel(srcConnCtx)
	defer dstCancel()
	// Hook: dail
	if hook := proxy.HookFuncDialPhased(srcConnCtx); hook != nil {
		if err := hook(srcConnCtx, link); err != nil {
			return err
		}
	}
	errors := make(chan error, 2)
	copier := func(_ context.Context, name string, from, to net.Conn) {
		errors <- net.Copier(from, to)
	}
	go copier(dstCtx, "src-to-dest", srcConn, dstConn)
	go copier(dstCtx, "dest-to-src", dstConn, srcConn)
	return <-errors
}
