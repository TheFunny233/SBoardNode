package metrics

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Snapshot struct {
	CPUPercent       float64
	MemoryUsedBytes  uint64
	MemoryTotalBytes uint64
	UptimeSeconds    uint64
}

type CPUSampler struct {
	mu            sync.Mutex
	initialized   bool
	previousTotal uint64
	previousIdle  uint64
}

func (s *CPUSampler) Sample(procRoot string) (float64, error) {
	total, idle, err := readCPU(filepath.Join(procRoot, "stat"))
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		s.initialized = true
		s.previousTotal = total
		s.previousIdle = idle
		return 0, nil
	}
	if total <= s.previousTotal {
		return 0, nil
	}
	totalDelta := total - s.previousTotal
	idleDelta := idle - s.previousIdle
	s.previousTotal = total
	s.previousIdle = idle
	if idleDelta >= totalDelta {
		return 0, nil
	}
	return float64(totalDelta-idleDelta) * 100 / float64(totalDelta), nil
}

func Collect(procRoot string, sampler *CPUSampler) (Snapshot, error) {
	return CollectFrom(procRoot, "/sys/fs/cgroup", sampler)
}

func CollectFrom(procRoot, cgroupRoot string, sampler *CPUSampler) (Snapshot, error) {
	cpu, err := sampler.Sample(procRoot)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read CPU: %w", err)
	}
	used, total, err := readMemory(filepath.Join(procRoot, "meminfo"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read memory: %w", err)
	}
	if cgroupUsed, cgroupTotal, ok := readCgroupMemory(cgroupRoot); ok && cgroupTotal < total {
		used, total = cgroupUsed, cgroupTotal
	}
	uptime, err := readUptime(filepath.Join(procRoot, "uptime"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("read uptime: %w", err)
	}
	return Snapshot{
		CPUPercent:       cpu,
		MemoryUsedBytes:  used,
		MemoryTotalBytes: total,
		UptimeSeconds:    uptime,
	}, nil
}

func readCgroupMemory(root string) (uint64, uint64, bool) {
	if used, total, ok := readMemoryPair(
		filepath.Join(root, "memory.current"),
		filepath.Join(root, "memory.max"),
	); ok {
		return used, total, true
	}
	return readMemoryPair(
		filepath.Join(root, "memory", "memory.usage_in_bytes"),
		filepath.Join(root, "memory", "memory.limit_in_bytes"),
	)
}

func readMemoryPair(usedPath, totalPath string) (uint64, uint64, bool) {
	usedRaw, err := os.ReadFile(usedPath)
	if err != nil {
		return 0, 0, false
	}
	totalRaw, err := os.ReadFile(totalPath)
	if err != nil || strings.TrimSpace(string(totalRaw)) == "max" {
		return 0, 0, false
	}
	used, err := strconv.ParseUint(strings.TrimSpace(string(usedRaw)), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	total, err := strconv.ParseUint(strings.TrimSpace(string(totalRaw)), 10, 64)
	if err != nil || total == 0 || used > total {
		return 0, 0, false
	}
	return used, total, true
}

func BootID(procRoot string) (string, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, "sys/kernel/random/boot_id"))
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errors.New("boot_id is empty")
	}
	return value, nil
}

func readCPU(path string) (uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0, 0, errors.New("missing aggregate CPU line")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, errors.New("invalid aggregate CPU line")
	}
	values := make([]uint64, 0, len(fields)-1)
	for _, raw := range fields[1:] {
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse CPU counter: %w", err)
		}
		values = append(values, value)
	}
	var total uint64
	for index, value := range values {
		if index >= 8 {
			break
		}
		total += value
	}
	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	return total, idle, nil
}

func readMemory(path string) (uint64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()
	values := make(map[string]uint64, 5)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		switch key {
		case "MemTotal", "MemAvailable", "MemFree", "Buffers", "Cached":
			value, parseErr := strconv.ParseUint(fields[1], 10, 64)
			if parseErr != nil {
				return 0, 0, parseErr
			}
			values[key] = value * 1024
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	total := values["MemTotal"]
	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	if total == 0 || available > total {
		return 0, 0, errors.New("invalid memory counters")
	}
	return total - available, total, nil
}

func readUptime(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, errors.New("uptime is empty")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 {
		return 0, errors.New("invalid uptime")
	}
	return uint64(seconds), nil
}
