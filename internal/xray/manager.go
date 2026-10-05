package xray

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Status struct {
	State        string
	PID          int
	Ports        []int
	RestartCount uint64
	Message      string
}

type Manager struct {
	procRoot      string
	checkInterval time.Duration
	restartDelay  time.Duration
	logger        *log.Logger

	mu           sync.RWMutex
	lastRestart  time.Time
	restartCount uint64
	lastMessage  string
}

func NewManager(logger *log.Logger) *Manager {
	return &Manager{
		procRoot:      "/proc",
		checkInterval: 15 * time.Second,
		restartDelay:  time.Minute,
		logger:        logger,
	}
}

func (m *Manager) Run(ctx context.Context) {
	m.ensureRunning(ctx)
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.ensureRunning(ctx)
		}
	}
}

func (m *Manager) CheckAndRepair(ctx context.Context) Status {
	m.ensureRunning(ctx)
	return m.Status()
}

func (m *Manager) Status() Status {
	pids, err := findProcesses(m.procRoot)
	if err != nil {
		m.mu.RLock()
		restarts := m.restartCount
		message := m.lastMessage
		m.mu.RUnlock()
		if message == "" {
			message = err.Error()
		}
		return Status{State: "error", Ports: []int{}, RestartCount: restarts, Message: message}
	}
	ports, portsErr := findPorts(m.procRoot, pids)
	m.mu.RLock()
	restarts := m.restartCount
	message := m.lastMessage
	m.mu.RUnlock()
	if portsErr != nil && message == "" {
		message = "read xray ports: " + portsErr.Error()
	}
	if len(pids) == 0 {
		return Status{State: "stopped", Ports: []int{}, RestartCount: restarts, Message: message}
	}
	return Status{
		State:        "running",
		PID:          pids[0],
		Ports:        ports,
		RestartCount: restarts,
		Message:      message,
	}
}

func (m *Manager) ensureRunning(ctx context.Context) {
	pids, err := findProcesses(m.procRoot)
	if err != nil {
		m.setMessage("inspect xray process: " + err.Error())
		return
	}
	if len(pids) > 0 {
		m.setMessage("")
		return
	}

	m.mu.RLock()
	tooSoon := !m.lastRestart.IsZero() && time.Since(m.lastRestart) < m.restartDelay
	m.mu.RUnlock()
	if tooSoon {
		return
	}

	m.mu.Lock()
	m.lastRestart = time.Now()
	m.mu.Unlock()
	manager, command, args, err := serviceCommand()
	if err != nil {
		m.setMessage(err.Error())
		return
	}
	restartCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	output, err := exec.CommandContext(restartCtx, command, args...).CombinedOutput()
	message := strings.TrimSpace(string(output))
	if len(message) > 512 {
		message = message[:512]
	}
	if err != nil {
		if message == "" {
			message = err.Error()
		} else {
			message = fmt.Sprintf("%v: %s", err, message)
		}
		m.setMessage(manager + " restart failed: " + message)
		m.logger.Printf("xray is stopped; %s restart failed: %s", manager, message)
		return
	}
	m.mu.Lock()
	m.restartCount++
	m.lastMessage = ""
	m.mu.Unlock()
	m.logger.Printf("xray was stopped; restarted with %s", manager)
}

func (m *Manager) setMessage(message string) {
	m.mu.Lock()
	m.lastMessage = message
	m.mu.Unlock()
}

func serviceCommand() (string, string, []string, error) {
	if fileExists("/run/openrc/softlevel") || fileExists("/sbin/openrc") {
		if path := findExecutable("rc-service", "/sbin/rc-service", "/usr/sbin/rc-service"); path != "" {
			return "OpenRC", path, []string{"xray", "restart"}, nil
		}
	}
	if fileExists("/run/systemd/system") {
		if path := findExecutable("systemctl", "/bin/systemctl", "/usr/bin/systemctl"); path != "" {
			return "systemd", path, []string{"restart", "xray"}, nil
		}
	}
	if path := findExecutable("rc-service", "/sbin/rc-service", "/usr/sbin/rc-service"); path != "" {
		return "OpenRC", path, []string{"xray", "restart"}, nil
	}
	if path := findExecutable("systemctl", "/bin/systemctl", "/usr/bin/systemctl"); path != "" {
		return "systemd", path, []string{"restart", "xray"}, nil
	}
	return "", "", nil, fmt.Errorf("neither rc-service nor systemctl is available")
}

func findExecutable(name string, candidates ...string) string {
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func findProcesses(procRoot string) ([]int, error) {
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		base := filepath.Join(procRoot, entry.Name())
		if isXrayProcess(base) {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	return pids, nil
}

func isXrayProcess(procPath string) bool {
	if isZombie(procPath) {
		return false
	}
	if data, err := os.ReadFile(filepath.Join(procPath, "comm")); err == nil {
		if strings.EqualFold(strings.TrimSpace(string(data)), "xray") {
			return true
		}
	}
	data, err := os.ReadFile(filepath.Join(procPath, "cmdline"))
	if err != nil || len(data) == 0 {
		return false
	}
	first := strings.SplitN(string(data), "\x00", 2)[0]
	return strings.EqualFold(filepath.Base(first), "xray")
}

func isZombie(procPath string) bool {
	data, err := os.ReadFile(filepath.Join(procPath, "stat"))
	if err != nil {
		return false
	}
	closingParenthesis := strings.LastIndex(string(data), ") ")
	if closingParenthesis < 0 || closingParenthesis+2 >= len(data) {
		return false
	}
	return data[closingParenthesis+2] == 'Z'
}

func findPorts(procRoot string, pids []int) ([]int, error) {
	inodes := make(map[string]struct{})
	for _, pid := range pids {
		entries, err := os.ReadDir(filepath.Join(procRoot, strconv.Itoa(pid), "fd"))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join(procRoot, strconv.Itoa(pid), "fd", entry.Name()))
			if err != nil || !strings.HasPrefix(target, "socket:[") {
				continue
			}
			inodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = struct{}{}
		}
	}
	if len(inodes) == 0 {
		return []int{}, nil
	}
	ports := make(map[int]struct{})
	files := []struct {
		name  string
		state string
	}{
		{"tcp", "0A"}, {"tcp6", "0A"}, {"udp", "07"}, {"udp6", "07"},
	}
	for _, item := range files {
		if err := collectPorts(filepath.Join(procRoot, "net", item.name), item.state, inodes, ports); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	result := make([]int, 0, len(ports))
	for port := range ports {
		result = append(result, port)
	}
	sort.Ints(result)
	return result, nil
}

func collectPorts(path, wantedState string, inodes map[string]struct{}, ports map[int]struct{}) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		// Skip the header.
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[3] != wantedState {
			continue
		}
		if _, ok := inodes[fields[9]]; !ok {
			continue
		}
		addressParts := strings.Split(fields[1], ":")
		if len(addressParts) != 2 {
			continue
		}
		port, parseErr := strconv.ParseUint(addressParts[1], 16, 16)
		if parseErr == nil && port > 0 {
			ports[int(port)] = struct{}{}
		}
	}
	return scanner.Err()
}
