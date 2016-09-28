package monitoring

import (
	"os/exec"

	"github.com/gravitational/satellite/agent/health"
	pb "github.com/gravitational/satellite/agent/proto/agentpb"
)

// NewVersionCollector returns new instance of version collector probe
func NewVersionCollector() *VersionCollector {
	return &VersionCollector{}
}

// VersionCollector is a special type of probe that collects
// and reports versions of the internal components of planet
type VersionCollector struct {
}

// Name returns name of this collector
func (r *VersionCollector) Name() string { return "versions" }

// Check collects versions of all components and adds information to reporter
func (r *VersionCollector) Check(reporter health.Reporter) {
	for _, checker := range infoCheckers {
		output, err := exec.Command(checker.command[0], checker.command[1:]...).CombinedOutput()
		out := string(output)
		if err != nil {
			out += err.Error()
		}
		reporter.Add(&pb.Probe{
			Checker: checker.component,
			Detail:  string(output),
			Status:  pb.Probe_Running,
		})
	}
}

type infoChecker struct {
	command   []string
	component string
}

var infoCheckers = []infoChecker{
	{command: []string{"/bin/uname", "-a"}, component: "system-version"},
	{command: []string{"/bin/systemd", "--version"}, component: "systemd-version"},
	{command: []string{"/usr/bin/docker", "version"}, component: "docker-version"},
	{command: []string{"/usr/bin/etcd", "--version"}, component: "etcd-version"},
	{command: []string{"/usr/bin/kubelet", "--version"}, component: "kubelet-version"},
	{command: []string{"/usr/sbin/dnsmasq", "--version"}, component: "dnsmasq-version"},
	{command: []string{"/usr/bin/dbus-daemon", "--version"}, component: "dbus-version"},
	{command: []string{"/usr/bin/serf", "--version"}, component: "serf-version"},
	{command: []string{"/usr/bin/flanneld", "--version"}, component: "flanneld-version"},
	{command: []string{"/usr/bin/registry", "--version"}, component: "registry-version"},
}
