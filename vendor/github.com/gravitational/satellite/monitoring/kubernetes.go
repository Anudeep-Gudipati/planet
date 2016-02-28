package monitoring

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/url"

	"github.com/gravitational/satellite/agent/health"
	pb "github.com/gravitational/satellite/agent/proto/agentpb"
	"github.com/gravitational/trace"

	kube "k8s.io/kubernetes/pkg/client/unversioned"
)

const systemNamespace = "kube-system"

// kubeHealthz is httpResponseChecker that interprets health status of common kubernetes services.
func kubeHealthz(response io.Reader) error {
	payload, err := ioutil.ReadAll(response)
	if err != nil {
		return trace.Wrap(err)
	}
	if !bytes.Equal(payload, []byte("ok")) {
		return trace.Errorf("unexpected healthz response: %s", payload)
	}
	return nil
}

// KubeStatusChecker is a function that can check status of kubernetes services.
type KubeStatusChecker func(client *kube.Client) error

// KubeChecker implements Checker that can check and report problems
// with kubernetes services.
type KubeChecker struct {
	name     string
	hostPort string
	checker  KubeStatusChecker
}

// ConnectToKube establishes a connection to kubernetes on the specified address
// and returns an API client.
func ConnectToKube(hostPort string) (*kube.Client, error) {
	var baseURL *url.URL
	var err error
	if hostPort == "" {
		return nil, trace.Errorf("hostPort cannot be empty")
	}
	baseURL, err = url.Parse(hostPort)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	config := &kube.Config{
		Host: baseURL.String(),
	}
	client, err := kube.New(config)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return client, nil
}

// Name returns the name of this checker
func (r *KubeChecker) Name() string { return r.name }

// Check runs the wrapped kubernetes service checker function and reports
// status to the specified reporter
func (r *KubeChecker) Check(reporter health.Reporter) {
	client, err := r.connect()
	if err != nil {
		reporter.Add(NewProbeFromErr(r.name, err))
		return
	}
	err = r.checker(client)
	if err != nil {
		reporter.Add(NewProbeFromErr(r.name, err))
		return
	}
	reporter.Add(&pb.Probe{
		Status: pb.Probe_Running,
	})
}

func (r *KubeChecker) connect() (*kube.Client, error) {
	return ConnectToKube(r.hostPort)
}
