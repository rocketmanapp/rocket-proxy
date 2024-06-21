package tcp

import (
	"context"
	"fmt"
	"github.com/bytepowered/assert-go"
	"github.com/sirupsen/logrus"
	"io"
	ionet "net"
	"time"
	"vanity/common"
	"vanity/net"
	"vanity/proxy"
)

var (
	_ proxy.Forwarder = (*Forwarder)(nil)
)

type Forwarder struct {
	defaults net.TcpOptions
}

func NewForwarder() *Forwarder {
	return &Forwarder{
		defaults: net.TcpOptions{
			ReadTimeout:  time.Second * 30,
			WriteTimeout: time.Second * 10,
			ReadBuffer:   1024,
			WriteBuffer:  1024,
			NoDelay:      true,
			KeepAlive:    time.Second * 10,
		},
	}
}

func (d *Forwarder) DailServe(ctx context.Context, target *net.Link) (err error) {
	assert.MustTrue(target.Destination.Network == net.Network_TCP, "unsupported network: %s", target.Destination.Network)
	remoteCtx, remoteCancel := context.WithCancel(ctx)
	defer remoteCancel()
	logrus.Info("tcp-forwarder dail: ", target.Destination)
	remoteTCPConn, err := ionet.DialTCP("tcp", nil, &ionet.TCPAddr{
		IP:   target.Destination.Address.IP(),
		Port: int(target.Destination.Port),
	})
	if err != nil {
		return fmt.Errorf("tcp-forwarder dail: %w", err)
	}
	defer func() {
		logrus.Infof("tcp-forwarder dail-serve terminated, address: %s, %s ", target.Connection.Address, target.Destination)
		_ = remoteTCPConn.Close()
	}()
	if err := net.SetTcpOptions(remoteTCPConn, d.defaults); err != nil {
		return fmt.Errorf("tcp-forwarder set remote options: %w", err)
	}
	localTCPConn := target.Connection.TCPConn
	send := func() error {
		defer logrus.Warn("tcp-forwarder send-loop terminated, destination: ", target.Destination)
		return common.LoopTask(remoteCtx, func() error {
			if err := net.ResetReadDeadline(localTCPConn, d.defaults); err != nil {
				return fmt.Errorf("set local read-timeout: %w", err)
			}
			if _, err := io.Copy(remoteTCPConn, localTCPConn); err != nil {
				return fmt.Errorf("send to remote: %w", err)
			}
			return nil
		})
	}
	receive := func() error {
		defer logrus.Warn("tcp-forwarder receive-loop terminated, destination: ", target.Destination)
		return common.LoopTask(remoteCtx, func() error {
			if err := net.ResetReadDeadline(remoteTCPConn, d.defaults); err != nil {
				return fmt.Errorf("set remote read-timeout: %w", err)
			}
			if _, err := io.Copy(localTCPConn, remoteTCPConn); err != nil {
				return fmt.Errorf("receive from remote: %w", err)
			}
			return nil
		})
	}
	return common.RunTasks(remoteCtx, send, receive)
}
