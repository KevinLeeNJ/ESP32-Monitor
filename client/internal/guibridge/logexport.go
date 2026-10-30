package guibridge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"esp32-monitor/client/internal/logexport"
	"esp32-monitor/client/internal/logging"
	"esp32-monitor/client/internal/platform"
)

// LogExportResult tells the UI whether a file was written or the dialog was
// dismissed.
type LogExportResult struct {
	Canceled bool   `json:"canceled"`
	Path     string `json:"path,omitempty"`
	Bytes    int64  `json:"bytes,omitempty"`
	Files    int    `json:"files,omitempty"`
}

// ExportLogs asks for a destination and packages both log streams plus a
// diagnostic snapshot. The Agent may be unreachable: its absence is recorded
// in the archive instead of failing the export.
func (s *AppService) ExportLogs(ctx context.Context) (LogExportResult, error) {
	path, err := pickLogSavePath()
	if err != nil {
		logging.L("diagnostics").Warn("log_export_dialog_failed", "error", err)
		return LogExportResult{}, err
	}
	if path == "" {
		logging.L("diagnostics").Info("log_export_canceled")
		return LogExportResult{Canceled: true}, nil
	}
	return s.exportLogsTo(ctx, path)
}

func (s *AppService) exportLogsTo(ctx context.Context, path string) (LogExportResult, error) {
	log := logging.L("diagnostics")
	reportCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	report, reportErr := s.DiagnosticsReport(reportCtx)
	cancel()
	var reportJSON []byte
	reportError := ""
	if reportErr != nil {
		reportError = reportErr.Error()
		log.Warn("log_export_report_unavailable", "error", reportErr)
	} else if data, err := json.MarshalIndent(report, "", "  "); err != nil {
		reportError = err.Error()
		log.Warn("log_export_report_encode_failed", "error", err)
	} else {
		reportJSON = data
	}
	logDir, err := logging.Dir()
	if err != nil {
		logDir = ""
	}
	home, _ := os.UserHomeDir()
	dataDir, _ := platform.AppDataDir()
	result, err := logexport.Export(logexport.Options{
		LogDir:      logDir,
		SavePath:    path,
		AppVersion:  AppVersion,
		ReportJSON:  reportJSON,
		ReportError: reportError,
		AgentError:  s.LastAgentError(),
		RedactPaths: []string{home, dataDir},
	})
	if err != nil {
		log.Error("log_export_failed", "error", err)
		return LogExportResult{}, err
	}
	log.Info("log_export", "bytes", result.Bytes, "files", result.Files, "diagnostics", len(reportJSON) > 0)
	return LogExportResult{Path: result.Path, Bytes: result.Bytes, Files: result.Files}, nil
}

func pickLogSavePath() (string, error) {
	app := application.Get()
	if app == nil {
		return "", errors.New("dialog_unavailable")
	}
	directory := ""
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "Downloads")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			directory = candidate
		} else {
			directory = home
		}
	}
	filename := "esp32-monitor-logs-" + time.Now().Format("20060102-150405") + ".zip"
	return app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:      "导出排查日志",
		Message:    "将 GUI 与 Agent 的最近日志和脱敏诊断信息保存为 ZIP。",
		Directory:  directory,
		Filename:   filename,
		ButtonText: "导出",
		Filters:    []application.FileFilter{{DisplayName: "ZIP 压缩包 (*.zip)", Pattern: "*.zip"}},
	}).PromptForSingleSelection()
}
