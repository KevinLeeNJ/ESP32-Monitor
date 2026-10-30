// Package cli implements the interactive configuration workflow without running a server.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"esp32-monitor/server/internal/config"
)

type ServeFunc func(config.Config) error

func Run(args []string, in io.Reader, out, errOut io.Writer, interactive bool, serve ServeFunc) error {
	if len(args) == 0 {
		return errors.New("usage: monitor-server {configure|check-config|serve} --config PATH")
	}
	action := args[0]
	if action != "configure" && action != "check-config" && action != "serve" {
		return errors.New("unknown command; use configure, check-config or serve")
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(errOut)
	path := flags.String("config", "./config/monitor.yaml", "configuration file")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if action == "configure" {
		if !interactive {
			return errors.New("configure requires an interactive terminal")
		}
		return configure(*path, charmUI{in: in, out: out}, out)
	}
	if action == "serve" {
		original, e := config.Read(*path)
		if e != nil {
			return e
		}
		if original == nil {
			c := config.Defaults()
			c.ReadToken, e = config.GenerateToken()
			if e != nil {
				return e
			}
			if e = config.Save(*path, c, nil); e != nil {
				return e
			}
		}
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	if action == "check-config" {
		_, err = fmt.Fprintln(out, "配置校验通过")
		return err
	}
	return serve(c)
}
