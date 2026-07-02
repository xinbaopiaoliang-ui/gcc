//go:build linux

package systemstats

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

type Sampler struct {
	mu      sync.Mutex
	prevCPU cpuCounters
	prevNet netCounters
	prevAt  time.Time
}

type cpuCounters struct {
	total uint64
	idle  uint64
	ok    bool
}

type netCounters struct {
	rx uint64
	tx uint64
	ok bool
}

func NewSampler() *Sampler {
	return &Sampler{}
}

func (s *Sampler) Snapshot(now time.Time) *Snapshot {
	if now.IsZero() {
		now = time.Now()
	}
	currentCPU := readCPUCounters()
	currentNet := readNetCounters()
	snapshot := Snapshot{
		CPU: CPUSnapshot{
			Cores: runtime.NumCPU(),
		},
		Memory:      readMemory(),
		Disk:        readDisk("/"),
		LoadAverage: readLoadAverage(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if currentCPU.ok && s.prevCPU.ok {
		totalDelta := currentCPU.total - s.prevCPU.total
		idleDelta := currentCPU.idle - s.prevCPU.idle
		if totalDelta > 0 && totalDelta >= idleDelta {
			snapshot.CPU.Percent = clampPercent(float64(totalDelta-idleDelta) * 100 / float64(totalDelta))
		}
	} else if snapshot.CPU.Cores > 0 {
		snapshot.CPU.Percent = clampPercent(snapshot.LoadAverage.OneMinute * 100 / float64(snapshot.CPU.Cores))
	}
	if currentCPU.ok {
		s.prevCPU = currentCPU
	}

	if currentNet.ok {
		snapshot.Network.RXBytes = currentNet.rx
		snapshot.Network.TXBytes = currentNet.tx
		if s.prevNet.ok && !s.prevAt.IsZero() {
			seconds := now.Sub(s.prevAt).Seconds()
			if seconds > 0 {
				snapshot.Network.SampleSeconds = seconds
				if currentNet.rx >= s.prevNet.rx {
					snapshot.Network.RXRateBytes = float64(currentNet.rx-s.prevNet.rx) / seconds
				}
				if currentNet.tx >= s.prevNet.tx {
					snapshot.Network.TXRateBytes = float64(currentNet.tx-s.prevNet.tx) / seconds
				}
			}
		}
		s.prevNet = currentNet
	}
	s.prevAt = now

	if !snapshot.HasData() {
		return nil
	}
	return &snapshot
}

func readCPUCounters() cpuCounters {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuCounters{}
	}
	line := strings.SplitN(string(data), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}
	}
	var values []uint64
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuCounters{}
		}
		values = append(values, value)
	}
	var total uint64
	for _, value := range values {
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return cpuCounters{total: total, idle: idle, ok: total > 0}
}

func readMemory() MemorySnapshot {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemorySnapshot{}
	}
	defer file.Close()

	var total uint64
	var available uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = value * 1024
		case "MemAvailable:":
			available = value * 1024
		}
	}
	if total == 0 {
		return MemorySnapshot{}
	}
	used := total - available
	return MemorySnapshot{
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		UsedPercent:    clampPercent(float64(used) * 100 / float64(total)),
	}
}

func readDisk(path string) DiskSnapshot {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return DiskSnapshot{Path: path}
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	if total == 0 {
		return DiskSnapshot{Path: path}
	}
	used := total - free
	return DiskSnapshot{
		Path:        path,
		TotalBytes:  total,
		UsedBytes:   used,
		FreeBytes:   free,
		UsedPercent: clampPercent(float64(used) * 100 / float64(total)),
	}
}

func readNetCounters() netCounters {
	file, err := os.Open("/proc/net/dev")
	if err != nil {
		return netCounters{}
	}
	defer file.Close()

	var rx uint64
	var tx uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		rxValue, rxErr := strconv.ParseUint(fields[0], 10, 64)
		txValue, txErr := strconv.ParseUint(fields[8], 10, 64)
		if rxErr == nil {
			rx += rxValue
		}
		if txErr == nil {
			tx += txValue
		}
	}
	return netCounters{rx: rx, tx: tx, ok: true}
}

func readLoadAverage() LoadAverageSnapshot {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return LoadAverageSnapshot{}
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return LoadAverageSnapshot{}
	}
	one, _ := strconv.ParseFloat(fields[0], 64)
	five, _ := strconv.ParseFloat(fields[1], 64)
	fifteen, _ := strconv.ParseFloat(fields[2], 64)
	return LoadAverageSnapshot{OneMinute: one, FiveMinutes: five, FifteenMinutes: fifteen}
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
