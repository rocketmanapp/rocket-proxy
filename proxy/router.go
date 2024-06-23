package proxy

import (
	"context"
	"fluxway/net"
	"github.com/bytepowered/assert-go"
)

////

var (
	_ Router = (*StaticRouter)(nil)
)

type StaticRouter struct {
	target net.Destination
}

func NewStaticRouter() *StaticRouter {
	return &StaticRouter{
		target: net.TCPDestination(net.LocalHostIP, net.Port(1234)),
	}
}

func (d *StaticRouter) Route(ctx context.Context, income *net.Connection) (target net.Connection, err error) {
	proxyType := ProxyTypeFromContext(ctx)
	switch proxyType {
	case ProxyType_SOCKS5, ProxyType_HTTPS:
		assert.MustTrue(income.Destination.IsValid(), "proxy-type: socks5/https, income destination must be valid")
		return *income, nil
	default:
		assert.MustFalse(income.Destination.IsValid(), "proxy-type: tcp/udp/others, income destination must invalid")
		return income.WithDestination(d.target), nil
	}

}