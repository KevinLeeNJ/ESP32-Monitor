//go:build !windows

package elevatedtask

import "errors"

// Name is the Task Scheduler task name used on Windows.
const Name = "ESP32-Monitor Elevated Agent"

func Exists() bool                  { return false }
func Run() error                    { return errors.New("elevated_task_unsupported") }
func Create(string, []string) error { return errors.New("elevated_task_unsupported") }
func Delete() error                 { return nil }
