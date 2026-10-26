// sim-esp32 models a Monitor's UDP -> HTTP pairing -> MQTT behavior on a LAN.
package main

import (
	"context"
	"encoding/json"
	"esp32-monitor/client/internal/transport/simesp32"
	"flag"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	address := flag.String("listen", "127.0.0.1:18444", "UDP listen address; select a LAN IP for another host")
	name := flag.String("name", "Simulated Monitor", "Monitor name")
	flag.Parse()
	m, e := simesp32.New(*name, *address)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	defer m.Close()
	m.Start()
	fmt.Fprintf(os.Stderr, "Monitor %s (%s), physical pairing window simulated open\n", m.Name, m.DeviceID)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	enc := json.NewEncoder(os.Stdout)
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-m.Snapshots:
			_ = enc.Encode(s)
		case e := <-m.Errors:
			fmt.Fprintln(os.Stderr, e)
		}
	}
}
