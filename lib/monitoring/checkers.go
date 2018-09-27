package monitoring

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/gravitational/planet/lib/constants"

	etcdconf "github.com/gravitational/coordinate/config"
	"github.com/gravitational/satellite/agent"
	"github.com/gravitational/satellite/agent/health"
	"github.com/gravitational/satellite/monitoring"
	"github.com/gravitational/trace"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Config represents configuration for setting up monitoring checkers.
type Config struct {
	// Role is the current agent's role
	Role agent.Role
	// KubeAddr is the address of the kubernetes API server
	KubeAddr string
	// ClusterDNS is the IP of the kubernetes DNS service
	ClusterDNS string
	// UpstreamNameservers lists additional upstream nameserver added to the DNS configuration
	UpstreamNameservers []string
	// DNSZones maps DNS zone to a list of nameservers
	DNSZones map[string][]string
	// RegistryAddr is the address of the private docker registry
	RegistryAddr string
	// NettestContainerImage is the name of the container image used for
	// networking test
	NettestContainerImage string
	// DisableInterPodCheck disables inter-pod communication tests
	DisableInterPodCheck bool
	// ETCDConfig defines etcd-specific configuration
	ETCDConfig etcdconf.Config
	// CloudProvider is the cloud provider backend this cluster is using
	CloudProvider string
	// NodeName is the name of this node as see by kubernetes
	NodeName string
	// HighWatermark is the usage limit percentage of monitored directories and devicemapper
	HighWatermark uint
}

// Check validates monitoring configuration
func (c Config) Check() error {
	if c.HighWatermark > 100 {
		return trace.BadParameter("high watermark percentage should be 0-100")
	}
	return nil
}

// LocalTransport returns http transport that is set up with local certificate authority
// and client certificates
func (c *Config) LocalTransport() (*http.Transport, error) {
	cert, err := tls.LoadX509KeyPair(c.ETCDConfig.CertFile, c.ETCDConfig.KeyFile)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	roots, err := newCertPool([]string{c.ETCDConfig.CAFile})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &http.Transport{
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS10,
			RootCAs:      roots,
		}}, nil
}

// GetKubeClient returns a Kubernetes client that uses kubelet
// certificate for authentication
func GetKubeClient() (*kubernetes.Clientset, error) {
	return getKubeClient(constants.KubectlConfigPath)
}

// GetPrivilegedKubeClient returns a Kubernetes client that uses scheduler
// certificate for authentication
func GetPrivilegedKubeClient() (*kubernetes.Clientset, error) {
	return getKubeClient(constants.SchedulerConfigPath)
}

// AddCheckers adds checkers to the agent.
func AddCheckers(node agent.Agent, config *Config, kubeConfig monitoring.KubeConfig) (err error) {
	etcdConfig := &monitoring.ETCDConfig{
		Endpoints: config.ETCDConfig.Endpoints,
		CAFile:    config.ETCDConfig.CAFile,
		CertFile:  config.ETCDConfig.CertFile,
		KeyFile:   config.ETCDConfig.KeyFile,
	}
	switch config.Role {
	case agent.RoleMaster:
		err = addToMaster(node, config, etcdConfig, kubeConfig)
	case agent.RoleNode:
		err = addToNode(node, config, etcdConfig, kubeConfig)
	}
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

func addToMaster(node agent.Agent, config *Config, etcdConfig *monitoring.ETCDConfig, kubeConfig monitoring.KubeConfig) error {
	localTransport, err := config.LocalTransport()
	if err != nil {
		return trace.Wrap(err)
	}

	etcdChecker, err := monitoring.EtcdHealth(etcdConfig)
	if err != nil {
		return trace.Wrap(err)
	}

	node.AddChecker(monitoring.KubeAPIServerHealth(kubeConfig))
	node.AddChecker(monitoring.DockerHealth("/var/run/docker.sock"))
	node.AddChecker(dockerRegistryHealth(config.RegistryAddr, localTransport))
	node.AddChecker(etcdChecker)
	node.AddChecker(monitoring.SystemdHealth())
	node.AddChecker(monitoring.NewIPForwardChecker())
	node.AddChecker(monitoring.NewBridgeNetfilterChecker())
	node.AddChecker(monitoring.NewMayDetachMountsChecker())
	node.AddChecker(monitoring.NewInotifyChecker())
	node.AddChecker(monitoring.NewNodeStatusChecker(kubeConfig, config.NodeName))
	if !config.DisableInterPodCheck {
		node.AddChecker(monitoring.InterPodCommunication(kubeConfig, config.NettestContainerImage))
	}
	node.AddChecker(NewVersionCollector())
	node.AddChecker(monitoring.NewStorageChecker(monitoring.StorageConfig{
		Path:          constants.GravityDataDir,
		HighWatermark: config.HighWatermark,
	}))
	// the following checker will be no-op if docker driver is not devicemapper
	node.AddChecker(monitoring.NewDockerDevicemapperChecker(
		monitoring.DockerDevicemapperConfig{
			HighWatermark: config.HighWatermark,
		}))

	// Add checkers specific to cloud provider backend
	switch strings.ToLower(config.CloudProvider) {
	case constants.CloudProviderAWS:
		node.AddChecker(monitoring.NewAWSHasProfileChecker())
	}
	return nil
}

func addToNode(node agent.Agent, config *Config, etcdConfig *monitoring.ETCDConfig, kubeConfig monitoring.KubeConfig) error {
	etcdChecker, err := monitoring.EtcdHealth(etcdConfig)
	if err != nil {
		return trace.Wrap(err)
	}
	node.AddChecker(monitoring.KubeletHealth("http://127.0.0.1:10248"))
	node.AddChecker(monitoring.DockerHealth("/var/run/docker.sock"))
	node.AddChecker(etcdChecker)
	node.AddChecker(monitoring.SystemdHealth())
	node.AddChecker(NewVersionCollector())
	node.AddChecker(monitoring.NewIPForwardChecker())
	node.AddChecker(monitoring.NewBridgeNetfilterChecker())
	node.AddChecker(monitoring.NewMayDetachMountsChecker())
	node.AddChecker(monitoring.NewInotifyChecker())
	node.AddChecker(monitoring.NewNodeStatusChecker(kubeConfig, config.NodeName))
	node.AddChecker(monitoring.NewStorageChecker(monitoring.StorageConfig{
		Path:          constants.GravityDataDir,
		HighWatermark: config.HighWatermark,
	}))
	// the following checker will be no-op if docker driver is not devicemapper
	node.AddChecker(monitoring.NewDockerDevicemapperChecker(
		monitoring.DockerDevicemapperConfig{
			HighWatermark: config.HighWatermark,
		}))

	// Add checkers specific to cloud provider backend
	switch strings.ToLower(config.CloudProvider) {
	case constants.CloudProviderAWS:
		node.AddChecker(monitoring.NewAWSHasProfileChecker())
	}
	return nil
}

func dockerRegistryHealth(addr string, transport *http.Transport) health.Checker {
	return monitoring.NewHTTPHealthzCheckerWithTransport("docker-registry", fmt.Sprintf("%v/v2/", addr), transport, noopResponseChecker)
}

func noopResponseChecker(response io.Reader) error {
	return nil
}

// newCertPool creates x509 certPool with provided CA files.
func newCertPool(CAFiles []string) (*x509.CertPool, error) {
	certPool := x509.NewCertPool()

	for _, CAFile := range CAFiles {
		pemByte, err := ioutil.ReadFile(CAFile)
		if err != nil {
			return nil, trace.Wrap(err)
		}

		for {
			var block *pem.Block
			block, pemByte = pem.Decode(pemByte)
			if block == nil {
				break
			}
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, trace.Wrap(err)
			}
			certPool.AddCert(cert)
		}
	}

	return certPool, nil
}

func getKubeClient(kubeconfigPath string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return client, nil
}
