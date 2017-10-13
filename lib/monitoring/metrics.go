package monitoring

import (
	"github.com/gravitational/satellite/agent"
	"github.com/gravitational/satellite/monitoring"
	"github.com/gravitational/satellite/monitoring/collector"
	"github.com/gravitational/trace"
	"github.com/prometheus/client_golang/prometheus"
)

// AddMetrics exposes specific metrics to Prometheus
func AddMetrics(node agent.Agent, config *Config) error {
	etcdConfig := &monitoring.ETCDConfig{
		Endpoints: config.ETCDConfig.Endpoints,
		CAFile:    config.ETCDConfig.CAFile,
		CertFile:  config.ETCDConfig.CertFile,
		KeyFile:   config.ETCDConfig.KeyFile,
	}

	var mc *collector.MetricsCollector
	var err error

	switch config.Role {
	case agent.RoleMaster:
		mc, err = collector.NewMetricsCollector(etcdConfig, config.KubeAddr, agent.RoleMaster)
	case agent.RoleNode:
		mc, err = collector.NewMetricsCollector(etcdConfig, config.KubeAddr, agent.RoleNode)
	}
	if err = prometheus.Register(mc); err != nil {
		return trace.Wrap(err)
	}
	return nil
}
