//go:build windows

// Package elevatedtask owns the per-user highest-privilege logon task that
// starts monitor-agent.exe without a UAC prompt. Only an already-elevated
// process may create it; the GUI asks the Agent over authenticated IPC.
package elevatedtask

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// Name is the Task Scheduler task name. It is referenced by the GUI startup
// probe and by the elevated Agent.
const Name = "ESP32-Monitor Elevated Agent"

// Exists reports whether the logon task is registered.
func Exists() bool {
	return exec.Command("schtasks.exe", "/Query", "/TN", Name).Run() == nil
}

// Run starts the registered task. Task Scheduler supplies the elevated token.
func Run() error {
	return exec.Command("schtasks.exe", "/Run", "/TN", Name).Run()
}

// Create registers the Agent command line to run elevated at logon. The XML
// form avoids schtasks' 261-character /TR limit; the Agent is launched with a
// long settings-derived argument list.
func Create(agentPath string, args []string) error {
	document, err := taskXML(agentPath, args)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "esp32-monitor-task-*.xml")
	if err != nil {
		return errors.New("elevated_task_temp_failed")
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(utf16LE(document)); err == nil {
		err = file.Close()
	} else {
		_ = file.Close()
	}
	if err != nil {
		return errors.New("elevated_task_write_failed")
	}
	if output, err := exec.Command("schtasks.exe", "/Create", "/TN", Name, "/XML", path, "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("create_elevated_agent_task: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Delete removes the task; a missing task is not an error.
func Delete() error {
	if !Exists() {
		return nil
	}
	if output, err := exec.Command("schtasks.exe", "/Delete", "/TN", Name, "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("delete_elevated_agent_task: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func taskXML(agentPath string, args []string) (string, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return "", errors.New("elevated_task_identity_failed")
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", errors.New("elevated_task_identity_failed")
	}
	sid := user.User.Sid.String()
	var command, arguments bytes.Buffer
	_ = xml.EscapeText(&command, []byte(agentPath))
	_ = xml.EscapeText(&arguments, []byte(CommandLine(args)))
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>false</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <RunOnlyIfIdle>false</RunOnlyIfIdle>
    <WakeToRun>false</WakeToRun>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>%s</Arguments>
    </Exec>
  </Actions>
</Task>
`, sid, sid, command.String(), arguments.String()), nil
}

func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, 2+len(units)*2)
	out[0], out[1] = 0xFF, 0xFE
	for i, unit := range units {
		binary.LittleEndian.PutUint16(out[2+i*2:], unit)
	}
	return out
}

// CommandLine joins arguments using the Windows command-line quoting rules
// that CommandLineToArgvW and the Go runtime expect.
func CommandLine(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, QuoteArgument(arg))
	}
	return strings.Join(quoted, " ")
}

// QuoteArgument quotes one argument for a Windows command line.
func QuoteArgument(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	return "\"" + strings.ReplaceAll(arg, "\"", "\\\"") + "\""
}
