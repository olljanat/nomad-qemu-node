package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

// RAIDArray is a Linux software RAID (md) array as shown in /proc/mdstat.
type RAIDArray struct {
	Name  string `json:"name"`
	Level string `json:"level"`
	// State is active or inactive.
	State    string `json:"state"`
	ReadOnly bool   `json:"read_only,omitempty"`
	// Devices is the number of member devices the array should have and
	// ActiveDevices the number of them which are in sync.
	Devices       int      `json:"devices"`
	ActiveDevices int      `json:"active_devices"`
	Degraded      bool     `json:"degraded"`
	FailedDevices []string `json:"failed_devices,omitempty"`
	SpareDevices  []string `json:"spare_devices,omitempty"`
	// SyncAction is recovery, resync, check, repair or reshape while one is
	// running or waiting (SyncProgress 0), SyncProgress is in percent.
	SyncAction   string  `json:"sync_action,omitempty"`
	SyncProgress float64 `json:"sync_progress,omitempty"`
}

// raidStatus reads the software RAID arrays of the node. It returns nil
// when the md driver is not loaded.
func raidStatus() []RAIDArray {
	data, err := os.ReadFile("/proc/mdstat")
	if err != nil {
		return nil
	}
	return parseMdstat(string(data))
}

var (
	// [2/1] [U_]
	mdCounts = regexp.MustCompile(`\[(\d+)/(\d+)\]`)
	// recovery = 12.6% (...), resync=DELAYED
	mdSync = regexp.MustCompile(`\b(recovery|resync|check|repair|reshape)\s*=\s*([0-9.]+%|\S+)`)
)

// parseMdstat parses the content of /proc/mdstat:
//
//	md0 : active raid1 sdb2[1](F) sda2[0]
//	      976630464 blocks super 1.2 [2/1] [U_]
//	      [==>..................]  recovery = 12.6% (123/976630464) finish=81.2min speed=100000K/sec
func parseMdstat(s string) []RAIDArray {
	arrays := []RAIDArray{}
	var cur *RAIDArray
	counted := false
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			cur = nil
			name, rest, ok := strings.Cut(line, " : ")
			if !ok || name == "Personalities" || strings.HasPrefix(name, "unused") {
				continue
			}
			arrays = append(arrays, parseMdLine(strings.TrimSpace(name), rest))
			cur, counted = &arrays[len(arrays)-1], false
			continue
		}
		if cur == nil {
			continue
		}
		if m := mdCounts.FindStringSubmatch(line); m != nil && !counted {
			cur.Devices, _ = strconv.Atoi(m[1])
			cur.ActiveDevices, _ = strconv.Atoi(m[2])
			counted = true
		}
		if m := mdSync.FindStringSubmatch(line); m != nil {
			cur.SyncAction = m[1]
			if p, ok := strings.CutSuffix(m[2], "%"); ok {
				cur.SyncProgress, _ = strconv.ParseFloat(p, 64)
			}
		}
	}
	for i := range arrays {
		a := &arrays[i]
		a.Degraded = a.State == "active" && a.ActiveDevices < a.Devices
	}
	return arrays
}

// parseMdLine parses "active (auto-read-only) raid1 sdb2[1](F) sda2[0]".
// Arrays without redundancy (raid0, linear) have no [n/m] counters, so
// their counts come from the members.
func parseMdLine(name, rest string) RAIDArray {
	a := RAIDArray{Name: name}
	f := strings.Fields(rest)
	if len(f) > 0 {
		a.State, f = f[0], f[1:]
	}
	for len(f) > 0 && strings.HasPrefix(f[0], "(") {
		if strings.Contains(f[0], "read-only") {
			a.ReadOnly = true
		}
		f = f[1:]
	}
	if len(f) > 0 && !strings.Contains(f[0], "[") {
		a.Level, f = f[0], f[1:]
	}
	for _, dev := range f {
		n, flags, _ := strings.Cut(dev, "[")
		switch {
		case strings.Contains(flags, "(F)"):
			a.FailedDevices = append(a.FailedDevices, n)
			a.Devices++
		case strings.Contains(flags, "(S)"):
			a.SpareDevices = append(a.SpareDevices, n)
		default:
			a.Devices++
			a.ActiveDevices++
		}
	}
	return a
}
