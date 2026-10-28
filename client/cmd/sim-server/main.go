// Command sim-server is a local ESP32-Monitor Server + ESP32 device simulator.
// The HTTP side implements the Client → Server v2 collector protocol from
// client/docs/COLLECTOR_UPLOAD_V2.md (profile GET/PUT/PATCH with ETag
// conditions, sample batches with idempotency and validation) plus the WebUI
// device registration flow: add a device, receive a six-digit code, and let the
// Client exchange it for a bearer token. With -esp32-listen it also runs the LAN
// pairing behavior of cmd/sim-esp32 in the same process. Every request and
// protocol event is logged to stdout as JSON lines.
package main

import (
	"context"
	"encoding/json"
	"esp32-monitor/client/internal/transport/simesp32"
	"esp32-monitor/client/internal/transport/simserver"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18080", "HTTP listen address")
	token := flag.String("token", "", "legacy fixed Bearer token for tests; when empty the WebUI registration flow is required")
	esp32Listen := flag.String("esp32-listen", "", "optional UDP listen address to also simulate the ESP32 monitor (e.g. 127.0.0.1:18444)")
	esp32Name := flag.String("esp32-name", "Simulated Monitor", "ESP32 monitor name")
	flag.Parse()

	logger := log.New(os.Stdout, "", 0)
	var server *simserver.Server
	mode := "registration"
	if *token != "" {
		server = simserver.New(*token, logger)
		mode = "fixed-token"
	} else {
		server = simserver.NewRegistration(logger)
	}

	if *esp32Listen != "" {
		monitor, err := simesp32.New(*esp32Name, *esp32Listen)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer monitor.Close()
		monitor.Start()
		logger.Printf(`{"at":%q,"event":"esp32","op":"start","name":%q,"device":%q,"listen":%q}`,
			now(), monitor.Name, monitor.DeviceID, *esp32Listen)
		go func() {
			enc := json.NewEncoder(os.Stdout)
			for snapshot := range monitor.Snapshots {
				_ = enc.Encode(map[string]any{"at": now(), "event": "esp32", "op": "snapshot", "snapshot": snapshot})
			}
		}()
		go func() {
			for err := range monitor.Errors {
				logger.Printf(`{"at":%q,"event":"esp32","op":"error","error":%q}`, now(), fmt.Sprint(err))
			}
		}()
	}

	httpServer := &http.Server{Addr: *listen, Handler: server.Handler()}
	go func() {
		logger.Printf(`{"at":%q,"event":"server","op":"start","listen":%q,"mode":%q,"webui":%q}`,
			now(), *listen, mode, "http://"+*listen+"/")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()
	_ = httpServer.Close()
}

func now() string { return time.Now().Format("2006-01-02T15:04:05.000Z07:00") }
