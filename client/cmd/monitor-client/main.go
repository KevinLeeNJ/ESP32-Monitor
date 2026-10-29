// Command monitor-client is the Wails GUI host for the ESP32-Monitor desktop
// client. It supervises the monitor-agent child process and proxies every
// privileged operation over the peer-authenticated IPC socket, so the webview
// never sees tokens, file paths or collector internals.
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"esp32-monitor/client/internal/guiassets"
	"esp32-monitor/client/internal/guibridge"
)

// trayController owns the optional status bar item. Settings changes are
// applied immediately so the icon appears or disappears without a restart.
type trayController struct {
	app    *application.App
	window *application.WebviewWindow
	mu     sync.Mutex
	tray   *application.SystemTray
}

func newTrayController(app *application.App, window *application.WebviewWindow) *trayController {
	return &trayController{app: app, window: window}
}

func (t *trayController) Apply(settings guibridge.Settings) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !settings.Launch.ShowInStatusBar {
		if t.tray != nil {
			t.tray.Destroy()
			t.tray = nil
		}
		return
	}
	if t.tray != nil {
		return
	}
	tray := t.app.SystemTray.New()
	if runtime.GOOS == "windows" {
		tray.SetIcon(guibridge.TrayAppIcon())
		tray.OnClick(t.showWindow)
	} else {
		tray.SetTemplateIcon(guibridge.MenuBarIcon())
	}
	tray.SetTooltip("ESP32 Monitor")
	menu := application.NewMenu()
	menu.Add("打开主窗口").OnClick(func(*application.Context) { t.showWindow() })
	menu.AddSeparator()
	menu.Add("退出 ESP32 Monitor").OnClick(func(*application.Context) { t.app.Quit() })
	tray.SetMenu(menu)
	t.tray = tray
}

func (t *trayController) showWindow() {
	t.app.Show()
	t.window.Show().Focus()
}

func main() {
	loginItem := flag.Bool("login-item", false, "start from the login item without showing the window")
	flag.Parse()
	if err := ensureWebView2(); err != nil {
		log.Fatal(err)
	}
	if err := run(*loginItem); err != nil {
		log.Fatal(err)
	}
}

func run(loginItem bool) error {
	settings, err := guibridge.LoadSettings()
	if err != nil {
		log.Printf("gui_settings_fallback: %v", err)
	}
	socketPath, err := guibridge.DefaultSocketPath()
	if err != nil {
		return err
	}
	supervisor, err := guibridge.NewSupervisor(settings, socketPath)
	if err != nil {
		return err
	}

	appService := guibridge.NewAppService(supervisor, func() { application.Get().Quit() })
	macOptions := application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false}
	if loginItem {
		// Login launches stay out of the Dock; the status bar icon (or opening
		// the app again) reveals the window.
		macOptions.ActivationPolicy = application.ActivationPolicyAccessory
	}
	app := application.New(application.Options{Name: "ESP32-Monitor",
		Icon: guibridge.AppIcon(),
		Services: []application.Service{
			application.NewService(appService),
			application.NewService(guibridge.NewCollectorService(supervisor)),
			application.NewService(guibridge.NewCompanionService(supervisor)),
			application.NewService(guibridge.NewMonitorService(supervisor)),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(guiassets.Assets()),
		},
		Mac:      macOptions,
		LogLevel: slog.LevelWarn,
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:           "ESP32-Monitor",
		Width:           1280,
		Height:          800,
		MinWidth:        1024,
		MinHeight:       700,
		URL:             "/",
		Hidden:          loginItem,
		DevToolsEnabled: os.Getenv("MONITOR_CLIENT_DEVTOOLS") == "1",
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if supervisor.CurrentSettings().Launch.CloseToBackground {
			event.Cancel()
			window.Hide()
		}
	})
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		if !supervisor.CurrentSettings().Launch.CloseToBackground {
			application.Get().Quit()
		}
	})

	tray := newTrayController(app, window)
	appService.SetSettingsListener(tray.Apply)
	tray.Apply(settings)

	// On darwin, [NSApp terminate:] exits the process; shutdown hooks run
	// inside applicationShouldTerminate, so the Agent is stopped reliably even
	// if Go defers after app.Run() never execute.
	app.OnShutdown(supervisor.Stop)
	defer supervisor.Stop()

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	// Wails' own signal handler also quits on SIGINT/SIGTERM; the extra notify
	// makes a single signal win even when its early-launch dispatch races the
	// initializing AppKit runloop.
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		<-signals
		application.Get().Quit()
	}()

	// The management loop keeps restarting the Agent in the background; the
	// window opens immediately and reports Agent state through Status calls.
	go func() {
		if err := supervisor.Start(ctx); err != nil {
			log.Printf("agent_supervisor: %v", err)
		}
	}()

	if err := app.Run(); err != nil {
		return err
	}
	return nil
}
