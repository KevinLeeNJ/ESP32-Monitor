//go:build linux

package collector

import (
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

type linuxDiskStat struct {
	Used      uint64
	Total     uint64
	UsedKnown bool
}

type linuxRAIDArray struct {
	Level     string
	Total     uint64
	Used      uint64
	UsedKnown bool
}

type linuxMount struct {
	Device string
	FSType string
	Point  string
}

type linuxDisk struct {
	Name  string
	Model string
}

// Only metadata and statfs(2) are read. No block device is opened and no SMART
// command is issued, so spun-down disks stay asleep.
func linuxStatfs(path string) (used, total uint64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	block := uint64(st.Frsize)
	if block == 0 {
		block = uint64(st.Bsize)
	}
	if block == 0 {
		return 0, 0, false
	}
	return (uint64(st.Blocks) - uint64(st.Bfree)) * block, uint64(st.Blocks) * block, true
}

// The kernel records the device path used at mount time; /proc/self/mountinfo
// avoids opening devices or invoking blkid.
func linuxMounts(path, classBlock string) []linuxMount {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []linuxMount
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		sep := -1
		for i, field := range fields {
			if field == "-" {
				sep = i
				break
			}
		}
		if len(fields) < 6 || sep < 1 || sep+2 >= len(fields) {
			continue
		}
		source := strings.ReplaceAll(fields[sep+2], `\040`, " ")
		real, err := filepath.EvalSymlinks(source)
		if err != nil {
			continue
		}
		name := filepath.Base(real)
		if seen[name] {
			continue
		}
		if _, err := os.Stat(filepath.Join(classBlock, name)); err != nil {
			continue
		}
		seen[name] = true
		out = append(out, linuxMount{
			Device: name,
			FSType: fields[sep+1],
			Point:  strings.ReplaceAll(fields[4], `\040`, " "),
		})
	}
	return out
}

func linuxSize(path string, unit uint64) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || value == 0 || value > ^uint64(0)/unit {
		return 0
	}
	return value * unit
}

func linuxSectors(path string) uint64 {
	return linuxSize(path, 512)
}

func linuxBlockSize(classBlock, name string) uint64 {
	return linuxSectors(filepath.Join(classBlock, name, "size"))
}

// Direct physical attribution only: a mounted partition or whole disk. Usage
// behind LVM, RAID or encryption belongs to the array, not to one member.
func linuxDirectDisk(classBlock, name string) (string, bool) {
	if strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "md") {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(classBlock, name, "partition")); err == nil {
		real, err := filepath.EvalSymlinks(filepath.Join(classBlock, name))
		if err != nil {
			return "", false
		}
		parent := filepath.Base(filepath.Dir(real))
		if parent == "" || parent == "block" {
			return "", false
		}
		return parent, true
	}
	if _, err := os.Stat(filepath.Join(classBlock, name, "device")); err == nil {
		return name, true
	}
	return "", false
}

func linuxDeviceArrays(classBlock, name string) []string {
	var out []string
	seen := map[string]bool{}
	var walk func(string, int)
	walk = func(device string, depth int) {
		if depth > 4 || seen[device] {
			return
		}
		seen[device] = true
		if strings.HasPrefix(device, "md") {
			if st, err := os.Stat(filepath.Join(classBlock, device, "md")); err == nil && st.IsDir() {
				out = append(out, device)
			}
		}
		slaves, _ := os.ReadDir(filepath.Join(classBlock, device, "slaves"))
		for _, slave := range slaves {
			walk(slave.Name(), depth+1)
		}
	}
	walk(name, 0)
	return out
}

func linuxSingleDeviceBtrfs(root, device string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "features" {
			continue
		}
		devices, err := os.ReadDir(filepath.Join(root, entry.Name(), "devices"))
		if err != nil {
			continue
		}
		for _, candidate := range devices {
			if candidate.Name() == device {
				return len(devices) == 1
			}
		}
	}
	return false
}

func linuxMDArrays(classBlock string) map[string]linuxRAIDArray {
	entries, err := os.ReadDir(classBlock)
	if err != nil {
		return nil
	}
	out := map[string]linuxRAIDArray{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "md") {
			continue
		}
		md := filepath.Join(classBlock, name, "md")
		if st, err := os.Stat(md); err != nil || !st.IsDir() {
			continue
		}
		total := linuxBlockSize(classBlock, name)
		if total == 0 {
			continue
		}
		level, _ := os.ReadFile(filepath.Join(md, "level"))
		out[name] = linuxRAIDArray{Level: strings.TrimSpace(string(level)), Total: total}
	}
	return out
}

// Physical capacity is what the disk actually carries: directly mounted
// partitions plus md member partitions. RAID usage is apportioned to members by
// capacity, so RAID5 parity is counted on the disks that store it.
func linuxStorage(mountInfo, classBlock, btrfsRoot string) (map[string]linuxDiskStat, map[string]linuxRAIDArray) {
	return linuxStorageWith(mountInfo, classBlock, btrfsRoot, linuxStatfs)
}

func linuxStorageWith(mountInfo, classBlock, btrfsRoot string, statfs func(string) (uint64, uint64, bool)) (map[string]linuxDiskStat, map[string]linuxRAIDArray) {
	physical := map[string]linuxDiskStat{}
	arrays := linuxMDArrays(classBlock)
	for _, mount := range linuxMounts(mountInfo, classBlock) {
		used, fsTotal, ok := statfs(mount.Point)
		if !ok || fsTotal == 0 || used > fsTotal {
			continue
		}
		if mount.FSType == "btrfs" && !linuxSingleDeviceBtrfs(btrfsRoot, mount.Device) {
			continue
		}
		if disk, ok := linuxDirectDisk(classBlock, mount.Device); ok {
			if size := linuxBlockSize(classBlock, mount.Device); size > 0 && used <= size {
				stat := physical[disk]
				stat.Used += used
				stat.Total += size
				stat.UsedKnown = true
				physical[disk] = stat
			}
		}
		for _, name := range linuxDeviceArrays(classBlock, mount.Device) {
			array := arrays[name]
			array.Used += used
			array.UsedKnown = true
			arrays[name] = array
		}
	}
	for name, array := range arrays {
		if array.UsedKnown && array.Used > array.Total {
			array.Used, array.UsedKnown = 0, false
			arrays[name] = array
		}
	}
	for name, array := range arrays {
		members, _ := filepath.Glob(filepath.Join(classBlock, name, "md", "dev-*"))
		for _, member := range members {
			target, err := os.Readlink(filepath.Join(member, "block"))
			if err != nil {
				continue
			}
			disk := linuxMDParentDevice(member)
			size := linuxBlockSize(classBlock, filepath.Base(target))
			// md dev-*/size is in KiB, unlike class/block size in 512-byte sectors.
			component := linuxSize(filepath.Join(member, "size"), 1024)
			if disk == "" || size == 0 {
				continue
			}
			stat := physical[disk]
			stat.Total += size
			if array.UsedKnown && component > 0 && array.Total > 0 {
				stat.Used += linuxRAIDShare(array.Used, component, array.Total)
				stat.UsedKnown = true
			}
			physical[disk] = stat
		}
	}
	return physical, arrays
}

func linuxRAIDShare(used, memberSize, arrayTotal uint64) uint64 {
	hi, lo := bits.Mul64(used, memberSize)
	share, _ := bits.Div64(hi, lo, arrayTotal)
	return share
}

func linuxPhysicalDisks(classBlock string) []linuxDisk {
	entries, err := os.ReadDir(classBlock)
	if err != nil {
		return nil
	}
	var out []linuxDisk
	for _, entry := range entries {
		name := entry.Name()
		if _, err := os.Stat(filepath.Join(classBlock, name, "device")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(classBlock, name, "partition")); err == nil {
			continue
		}
		if linuxBlockSize(classBlock, name) == 0 {
			continue
		}
		model, _ := os.ReadFile(filepath.Join(classBlock, name, "device", "model"))
		out = append(out, linuxDisk{Name: name, Model: strings.TrimSpace(string(model))})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}
