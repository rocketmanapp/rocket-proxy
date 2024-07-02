package main

import (
	"context"
	"fmt"
	"github.com/bytepowered/goes"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/rocketmanapp/rocket-proxy"
	"github.com/rocketmanapp/rocket-proxy/helper"
	"github.com/rocketmanapp/rocket-proxy/server"
	"github.com/sirupsen/logrus"
	"runtime/debug"
)

// Configuration
var k = koanf.NewWithConf(koanf.Conf{
	Delim:       ".",
	StrictMerge: true,
})

func init() {
	goes.SetPanicHandler(func(ctx context.Context, r interface{}) {
		logrus.Errorf("goroutine panic %v: %s", r, debug.Stack())
	})
}

func runAsAutoServer(runCtx context.Context, args []string) error {
	return runCommandAs(runCtx, args, server.RunServerModeAuto)
}

func runAsForwardServer(runCtx context.Context, args []string) error {
	return runCommandAs(runCtx, args, server.RunServerModeForward)
}

func runAsProxyServer(runCtx context.Context, args []string) error {
	return runCommandAs(runCtx, args, server.RunServerModeProxy)
}

func runCommandAs(runCtx context.Context, args []string, serverMode string) error {
	confpath := "config.yml"
	if len(args) > 0 {
		confpath = args[0]
	}
	if err := k.Load(file.Provider(confpath), yaml.Parser()); err != nil {
		return fmt.Errorf("load config file %s: %w", confpath, err)
	}
	switch k.String("log.format") {
	case "json":
		logrus.SetFormatter(&logrus.JSONFormatter{})
	default:
		logrus.SetFormatter(&logrus.TextFormatter{
			DisableColors:    false,
			DisableTimestamp: false,
			FullTimestamp:    true,
		})
	}
	logrus.SetReportCaller(false)
	logrus.Infof("main: load config file: %s", confpath)
	// Instance
	runCtx = context.WithValue(runCtx, rocket.CtxKeyConfiger, k)
	inst := server.NewInstance()
	if err := inst.Init(runCtx, serverMode); err != nil {
		return fmt.Errorf("main: instance start: %w", err)
	}
	return helper.ErrIf(inst.Serve(runCtx), "main: instance serve")
}
