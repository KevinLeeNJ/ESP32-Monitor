package logexport

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the archive manifest contract version.
const SchemaVersion = 1

const readme = `ESP32 Monitor 排查日志
=====================

内容
- logs/client.log*：GUI 主进程日志，按大小轮转。
- logs/agent.log*：后台 Agent 日志，按大小轮转。
- diagnostics.json：脱敏的健康状态快照（若导出时 Agent 可达）。
- manifest.json：导出时间、版本、系统信息与文件清单。

已自动脱敏
- 用户主目录路径替换为 ~。
- Bearer 令牌、GitHub 令牌、访问密钥、密钥类字段值被替换。
- 长十六进制摘要完整隐藏；设备 UUID 仅保留前 8 位，便于跨行关联。
- 不包含采集样本、任务标题、播放元数据或任何凭据。

可能仍包含
- 本机局域网地址、操作系统版本、应用版本、组件健康状态与错误码。

提交 Issue 前请快速浏览本文件内容，确认没有不希望公开的信息。
`

// Options describes one export. ReportJSON is the pre-marshalled diagnostic
// snapshot; it is optional because the Agent may be unreachable when the user
// exports logs.
type Options struct {
	LogDir      string
	SavePath    string
	AppVersion  string
	ReportJSON  []byte
	ReportError string
	AgentError  string
	RedactPaths []string
	Now         time.Time
}

// Manifest documents what the archive contains.
type Manifest struct {
	SchemaVersion       int       `json:"schema_version"`
	GeneratedAt         time.Time `json:"generated_at"`
	App                 string    `json:"app"`
	AppVersion          string    `json:"app_version,omitempty"`
	OS                  string    `json:"os"`
	Arch                string    `json:"arch"`
	GoVersion           string    `json:"go_version"`
	DiagnosticsIncluded bool      `json:"diagnostics_included"`
	ReportError         string    `json:"report_error,omitempty"`
	AgentError          string    `json:"agent_error,omitempty"`
	Logs                []LogFile `json:"logs"`
	Notes               []string  `json:"notes,omitempty"`
}

// LogFile records one archived log and its original size.
type LogFile struct {
	Name    string    `json:"name"`
	Bytes   int64     `json:"bytes"`
	ModTime time.Time `json:"mod_time"`
}

// Result reports where the archive landed and how large it is.
type Result struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

// Export writes a ZIP atomically: a temporary file in the destination
// directory is filled first and only renamed into place after it is complete.
func Export(opts Options) (Result, error) {
	var result Result
	if strings.TrimSpace(opts.SavePath) == "" {
		return result, errors.New("log_export_path_required")
	}
	savePath := opts.SavePath
	if !strings.EqualFold(filepath.Ext(savePath), ".zip") {
		savePath += ".zip"
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	redactor := NewRedactor(opts.RedactPaths...)
	logs, readErr := listLogs(opts.LogDir)
	tmp, err := os.CreateTemp(filepath.Dir(savePath), ".esp32-monitor-logs-*.tmp")
	if err != nil {
		return result, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return result, err
	}
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   now.UTC(),
		App:           "ESP32 Monitor",
		AppVersion:    opts.AppVersion,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GoVersion:     runtime.Version(),
		Logs:          logs,
	}
	if len(opts.ReportJSON) > 0 {
		manifest.DiagnosticsIncluded = true
	}
	manifest.ReportError = redactor.Text(opts.ReportError)
	manifest.AgentError = redactor.Text(opts.AgentError)
	if readErr != nil {
		manifest.Notes = append(manifest.Notes, redactor.Text(readErr.Error()))
	}
	archive := zip.NewWriter(tmp)
	if err := addEntry(archive, "README.txt", now, []byte(readme)); err != nil {
		_ = archive.Close()
		_ = tmp.Close()
		return result, err
	}
	if len(opts.ReportJSON) > 0 {
		if err := addEntry(archive, "diagnostics.json", now, []byte(redactor.Text(string(opts.ReportJSON)))); err != nil {
			_ = archive.Close()
			_ = tmp.Close()
			return result, err
		}
	}
	files, err := addLogs(archive, opts.LogDir, logs, redactor)
	if err != nil {
		_ = archive.Close()
		_ = tmp.Close()
		return result, err
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = archive.Close()
		_ = tmp.Close()
		return result, err
	}
	if err := addEntry(archive, "manifest.json", now, []byte(redactor.Text(string(manifestData)))); err != nil {
		_ = archive.Close()
		_ = tmp.Close()
		return result, err
	}
	if err := archive.Close(); err != nil {
		_ = tmp.Close()
		return result, err
	}
	if err := tmp.Close(); err != nil {
		return result, err
	}
	if err := os.Rename(tmpPath, savePath); err != nil {
		// Windows refuses to replace an existing file; the save dialog already
		// confirmed overwriting, so drop the old archive and retry once.
		if removeErr := os.Remove(savePath); removeErr != nil {
			return result, err
		}
		if err := os.Rename(tmpPath, savePath); err != nil {
			return result, err
		}
	}
	info, err := os.Stat(savePath)
	if err != nil {
		return result, err
	}
	result = Result{Path: savePath, Bytes: info.Size(), Files: files}
	return result, nil
}

var logNamePattern = regexp.MustCompile(`^(client|agent)\.log(?:\.(\d+))?$`)

func listLogs(dir string) ([]LogFile, error) {
	if dir == "" {
		return nil, errors.New("log_dir_unavailable")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("log_dir_missing")
		}
		return nil, err
	}
	type candidate struct {
		name  string
		stem  string
		index int
		file  LogFile
	}
	var candidates []candidate
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := logNamePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		index := 0
		if match[2] != "" {
			index, _ = strconv.Atoi(match[2])
		}
		candidates = append(candidates, candidate{
			name: entry.Name(), stem: match[1], index: index,
			file: LogFile{Name: entry.Name(), Bytes: info.Size(), ModTime: info.ModTime().UTC()},
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].stem != candidates[j].stem {
			return candidates[i].stem == "client"
		}
		return candidates[i].index < candidates[j].index
	})
	logs := make([]LogFile, 0, len(candidates))
	for _, item := range candidates {
		logs = append(logs, item.file)
	}
	return logs, nil
}

func addLogs(archive *zip.Writer, dir string, logs []LogFile, redactor *Redactor) (int, error) {
	files := 0
	for _, log := range logs {
		source, err := os.Open(filepath.Join(dir, log.Name))
		if err != nil {
			continue
		}
		entry, err := newEntry(archive, "logs/"+log.Name, log.ModTime)
		if err != nil {
			_ = source.Close()
			return files, err
		}
		if err := copyRedacted(entry, source, redactor); err != nil {
			_ = source.Close()
			return files, err
		}
		if err := source.Close(); err != nil {
			return files, err
		}
		files++
	}
	return files, nil
}

func copyRedacted(dst io.Writer, src io.Reader, redactor *Redactor) error {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := redactor.Text(scanner.Text())
		if _, err := fmt.Fprintln(dst, line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func addEntry(archive *zip.Writer, name string, modTime time.Time, data []byte) error {
	entry, err := newEntry(archive, name, modTime)
	if err != nil {
		return err
	}
	_, err = entry.Write(data)
	return err
}

func newEntry(archive *zip.Writer, name string, modTime time.Time) (io.Writer, error) {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0600)
	if !modTime.IsZero() {
		header.SetModTime(modTime)
	}
	return archive.CreateHeader(header)
}
