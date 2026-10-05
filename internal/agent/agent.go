package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/TheFunny233/SBoardNode/internal/config"
	"github.com/TheFunny233/SBoardNode/internal/heartbeat"
	"github.com/TheFunny233/SBoardNode/internal/metrics"
	"github.com/TheFunny233/SBoardNode/internal/xray"
)

type Agent struct {
	config    config.Config
	version   string
	bootID    string
	sequence  uint64
	cpu       *metrics.CPUSampler
	heartbeat *heartbeat.Client
	xray      *xray.Manager
	logger    *log.Logger
}

func New(cfg config.Config, version string, logger *log.Logger) (*Agent, error) {
	bootID, err := metrics.BootID("/proc")
	if err != nil {
		return nil, fmt.Errorf("read boot id: %w", err)
	}
	return &Agent{
		config:    cfg,
		version:   version,
		bootID:    bootID,
		sequence:  uint64(time.Now().Unix()),
		cpu:       &metrics.CPUSampler{},
		heartbeat: heartbeat.New(cfg.HeartbeatURL(), cfg.Token),
		xray:      xray.NewManager(logger),
		logger:    logger,
	}, nil
}

func (a *Agent) Run(ctx context.Context) error {
	go a.xray.Run(ctx)
	if err := a.sendHeartbeat(ctx); err != nil {
		a.logger.Printf("heartbeat failed: %v", err)
	}
	ticker := time.NewTicker(a.config.Interval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			a.logger.Printf("stopping")
			return nil
		case <-ticker.C:
			if err := a.sendHeartbeat(ctx); err != nil {
				a.logger.Printf("heartbeat failed: %v", err)
			}
		}
	}
}

func (a *Agent) RunOnce(ctx context.Context) error {
	a.xray.CheckAndRepair(ctx)
	return a.sendHeartbeat(ctx)
}

func (a *Agent) sendHeartbeat(ctx context.Context) error {
	snapshot, err := metrics.Collect("/proc", a.cpu)
	if err != nil {
		return err
	}
	status := a.xray.Status()
	a.sequence++
	if snapshot.UptimeSeconds > a.sequence {
		a.sequence = snapshot.UptimeSeconds
	}

	var pid *int
	if status.PID > 0 {
		value := status.PID
		pid = &value
	}
	var message *string
	if status.Message != "" {
		value := status.Message
		message = &value
	}
	result, err := a.heartbeat.Send(ctx, heartbeat.Payload{
		SchemaVersion:        1,
		NodeID:               a.config.NodeID,
		Version:              a.version,
		SentAt:               time.Now().UTC(),
		BootID:               a.bootID,
		Sequence:             a.sequence,
		CPUPercent:           snapshot.CPUPercent,
		MemoryUsedBytes:      snapshot.MemoryUsedBytes,
		MemoryTotalBytes:     snapshot.MemoryTotalBytes,
		UptimeSeconds:        snapshot.UptimeSeconds,
		XrayStatus:           status.State,
		XrayPID:              pid,
		XrayPorts:            status.Ports,
		RestartCount:         status.RestartCount,
		XrayMessage:          message,
		AppliedConfigVersion: 0,
		Capabilities:         []string{"heartbeat", "xray-monitor", "xray-restart"},
	})
	if err != nil {
		return err
	}
	if result.ConfigChanged {
		a.logger.Printf(
			"server has config version %d; config sync is reserved for v2",
			result.DesiredConfigVersion,
		)
	}
	a.logger.Printf(
		"heartbeat accepted cpu=%.1f%% memory=%d/%d xray=%s",
		snapshot.CPUPercent,
		snapshot.MemoryUsedBytes,
		snapshot.MemoryTotalBytes,
		status.State,
	)
	return nil
}
