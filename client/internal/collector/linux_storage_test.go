//go:build linux

package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type linuxStorageFixture struct {
	t          *testing.T
	root       string
	classBlock string
	btrfs      string
	mountInfo  string
	mounts     []string
	usage      map[string][2]uint64
}

func newLinuxStorageFixture(t *testing.T) *linuxStorageFixture {
	t.Helper()
	root := t.TempDir()
	f := &linuxStorageFixture{
		t:          t,
		root:       root,
		classBlock: filepath.Join(root, "class", "block"),
		btrfs:      filepath.Join(root, "btrfs"),
		mountInfo:  filepath.Join(root, "mountinfo"),
		usage:      map[string][2]uint64{},
	}
	for _, dir := range []string{f.classBlock, f.btrfs} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	f.disk("sda", 480000)
	f.part("sda", "sda1", 200)
	f.part("sda", "sda2", 120000)
	f.part("sda", "sda3", 350000)
	f.part("sda", "sda4", 9800)
	f.disk("sdb", 4000000)
	f.part("sdb", "sdb5", 3900000)
	f.disk("sdc", 8000000)
	f.part("sdc", "sdc1", 8000000)
	f.disk("sdd", 8000000)
	f.part("sdd", "sdd1", 8000000)
	f.disk("sde", 8000000)
	f.part("sde", "sde1", 8000000)
	f.array("md0", "raid1", 350000)
	f.array("md2", "raid1", 3900000)
	f.array("md127", "raid5", 16000000)
	f.member("md0", "sda", "sda3", 175000)
	f.member("md2", "sdb", "sdb5", 1950000)
	f.member("md127", "sdc", "sdc1", 4000000)
	f.member("md127", "sdd", "sdd1", 4000000)
	f.member("md127", "sde", "sde1", 4000000)
	f.dm("dm-0", "md0")
	f.dm("dm-2", "md2")
	f.dm("dm-3", "md127")
	f.btrfsVolume("vol2", "dm-0")
	f.btrfsVolume("vol1", "dm-3")
	f.mount("/", "ext4", f.deviceSource("sda2", "sda/sda2"), 28000, 60000)
	f.mount("/boot/efi", "vfat", f.deviceSource("sda1", "sda/sda1"), 10, 100)
	f.mount("/unmounted-btrfs", "btrfs", f.deviceSource("mapper/vol2", "dm-0"), 39000, 179000)
	f.mount("/vol1", "btrfs", f.deviceSource("mapper/vol1", "dm-3"), 750000, 8000000)
	return f
}

func (f *linuxStorageFixture) write(path, content string) {
	f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *linuxStorageFixture) link(name, path string) {
	if err := os.MkdirAll(path, 0700); err != nil {
		panic(err)
	}
	rel, err := filepath.Rel(f.classBlock, path)
	if err != nil {
		panic(err)
	}
	if err := os.Symlink(rel, filepath.Join(f.classBlock, name)); err != nil {
		panic(err)
	}
}

func (f *linuxStorageFixture) node(name string) string {
	return filepath.Join(f.root, "devices", "block", name)
}

func (f *linuxStorageFixture) disk(name string, size uint64) {
	f.link(name, f.node(name))
	f.write(filepath.Join(f.node(name), "device", "model"), "Fixture\n")
	f.write(filepath.Join(f.node(name), "size"), strconv.FormatUint(size, 10))
}

func (f *linuxStorageFixture) part(disk, name string, size uint64) {
	f.link(name, filepath.Join(f.node(disk), name))
	f.write(filepath.Join(f.node(disk), name, "partition"), "1\n")
	f.write(filepath.Join(f.node(disk), name, "size"), strconv.FormatUint(size, 10))
}

func (f *linuxStorageFixture) array(name, level string, size uint64) {
	f.link(name, f.node(name))
	f.write(filepath.Join(f.node(name), "md", "level"), level+"\n")
	f.write(filepath.Join(f.node(name), "size"), strconv.FormatUint(size, 10))
}

func (f *linuxStorageFixture) member(md, parent, name string, sizeKiB uint64) {
	dir := filepath.Join(f.node(md), "md", "dev-"+name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		panic(err)
	}
	f.write(filepath.Join(dir, "size"), strconv.FormatUint(sizeKiB, 10))
	if err := os.Symlink("../../block/"+parent+"/"+name, filepath.Join(dir, "block")); err != nil {
		panic(err)
	}
}

func (f *linuxStorageFixture) dm(name string, slaves ...string) {
	f.link(name, f.node(name))
	for _, slave := range slaves {
		if err := os.MkdirAll(filepath.Join(f.node(name), "slaves", slave), 0700); err != nil {
			panic(err)
		}
	}
}

func (f *linuxStorageFixture) btrfsVolume(uuid, device string) {
	if err := os.MkdirAll(filepath.Join(f.btrfs, uuid, "devices", device), 0700); err != nil {
		panic(err)
	}
}

func (f *linuxStorageFixture) deviceSource(name, block string) string {
	path := filepath.Join(f.root, "dev", name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		panic(err)
	}
	rel, err := filepath.Rel(filepath.Dir(path), f.node(block))
	if err != nil {
		panic(err)
	}
	if err := os.Symlink(rel, path); err != nil {
		panic(err)
	}
	return path
}

func (f *linuxStorageFixture) mount(point, fstype, source string, used, total uint64) {
	f.mounts = append(f.mounts, fmt.Sprintf("1 0 0:0 / %s rw - %s %s rw", point, fstype, source))
	f.usage[point] = [2]uint64{used, total}
}

func (f *linuxStorageFixture) collect(t *testing.T) (map[string]linuxDiskStat, map[string]linuxRAIDArray) {
	t.Helper()
	writeLinuxFile(t, f.mountInfo, strings.Join(f.mounts, "\n"))
	return linuxStorageWith(f.mountInfo, f.classBlock, f.btrfs, func(point string) (uint64, uint64, bool) {
		value, ok := f.usage[point]
		return value[0], value[1], ok
	})
}

func TestLinuxStorageAttributesPhysicalDisksAndArrays(t *testing.T) {
	f := newLinuxStorageFixture(t)
	physical, arrays := f.collect(t)
	want := map[string]linuxDiskStat{
		"sda": {Used: 67010, Total: 240742400, UsedKnown: true},
		"sdb": {Total: 1996800000},
		"sdc": {Used: 375000, Total: 4096000000, UsedKnown: true},
		"sdd": {Used: 375000, Total: 4096000000, UsedKnown: true},
		"sde": {Used: 375000, Total: 4096000000, UsedKnown: true},
	}
	if !reflect.DeepEqual(physical, want) {
		t.Fatalf("unexpected physical usage:\n got %v\nwant %v", physical, want)
	}
	if len(arrays) != 3 {
		t.Fatalf("unexpected arrays: %+v", arrays)
	}
	if got := arrays["md0"]; got != (linuxRAIDArray{Level: "raid1", Total: 179200000, Used: 39000, UsedKnown: true}) {
		t.Fatalf("md0: %+v", got)
	}
	if got := arrays["md127"]; got != (linuxRAIDArray{Level: "raid5", Total: 8192000000, Used: 750000, UsedKnown: true}) {
		t.Fatalf("md127: %+v", got)
	}
	if got := arrays["md2"]; got.UsedKnown || got.Total != 1996800000 {
		t.Fatalf("unmounted md2 must report capacity without invented usage: %+v", got)
	}
	if err := os.Mkdir(filepath.Join(f.btrfs, "vol1", "devices", "dm-4"), 0700); err != nil {
		t.Fatal(err)
	}
	physical, arrays = f.collect(t)
	if arrays["md127"].UsedKnown {
		t.Fatal("multi-device btrfs must not be attributed to one array")
	}
	if got := physical["sdc"]; got.UsedKnown || got.Total != 4096000000 {
		t.Fatalf("unknown array usage must not be invented on members: %+v", got)
	}
}

func TestLinuxPhysicalDiskDirectory(t *testing.T) {
	f := newLinuxStorageFixture(t)
	disks := linuxPhysicalDisks(f.classBlock)
	if len(disks) != 5 {
		t.Fatalf("unexpected disks: %+v", disks)
	}
	for _, disk := range disks {
		if disk.Name == "md0" || disk.Name == "dm-0" || disk.Name == "sda1" {
			t.Fatalf("virtual or partition node listed as disk: %+v", disk)
		}
		if disk.Name == "sda" && disk.Model != "Fixture" {
			t.Fatalf("sda directory: %+v", disk)
		}
	}
}
