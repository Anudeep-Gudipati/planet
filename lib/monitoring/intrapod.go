package monitoring

import (
	"fmt"
	"strings"
	"time"

	"github.com/gravitational/log"
	"github.com/gravitational/trace"
	"k8s.io/kubernetes/pkg/api"
	kube "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/util"
)

// testNamespace is the namespace for functional k8s tests.
const testNamespace = "planettest"

// serviceName is the prefix used to name test pods.
const serviceName = "nettest"

// intraPodChecker is a checker that runs a networking test in the cluster
// by scheduling actual pods and verifying they can communicate.
type intraPodChecker struct {
	*kubeChecker
	registryAddr string
}

// newIntraPodChecker returns an instance of intraPodChecker.
func newIntraPodChecker(kubeAddr, registryAddr string) checker {
	checker := &intraPodChecker{
		registryAddr: registryAddr,
	}
	kubeChecker := &kubeChecker{
		hostPort:    kubeAddr,
		checkerFunc: checker.testIntraPodCommunication,
	}
	checker.kubeChecker = kubeChecker
	return checker
}

// testIntraPodCommunication implements the intra-pod communication test.
func (r *intraPodChecker) testIntraPodCommunication(client *kube.Client) error {
	if err := createNamespaceIfNeeded(client, testNamespace); err != nil {
		return trace.Wrap(err, "faile to create test namespace `%s`", testNamespace)
	}
	svc, err := client.Services(testNamespace).Create(&api.Service{
		ObjectMeta: api.ObjectMeta{
			Name: serviceName,
			Labels: map[string]string{
				"name": serviceName,
			},
		},
		Spec: api.ServiceSpec{
			Ports: []api.ServicePort{{
				Protocol:   "TCP",
				Port:       8080,
				TargetPort: util.NewIntOrStringFromInt(8080),
			}},
			Selector: map[string]string{
				"name": serviceName,
			},
		},
	})
	if err != nil {
		return trace.Wrap(err, "failed to create test service named `%s`", serviceName)
	}

	cleanupService := func() {
		if err = client.Services(testNamespace).Delete(svc.Name); err != nil {
			log.Infof("failed to delete service %v: %v", svc.Name, err)
		}
	}
	defer cleanupService()

	nodes, err := client.Nodes().List(labels.Everything(), fields.Everything())
	if err != nil {
		return trace.Wrap(err, "failed to list nodes")
	}

	if len(nodes.Items) < 2 {
		return trace.Errorf("expected at least 2 ready nodes - got %d (%v)", len(nodes.Items), nodes.Items)
	}

	testContainer := fmt.Sprintf("%s/nettest:1.6", r.registryAddr)
	podNames, err := launchNetTestPodPerNode(client, nodes, serviceName, testContainer)
	if err != nil {
		return trace.Wrap(err, "failed to start `nettest` pod")
	}

	cleanupPods := func() {
		for _, podName := range podNames {
			if err = client.Pods(testNamespace).Delete(podName, nil); err != nil {
				log.Infof("failed to delete pod %s: %v", podName, err)
			}
		}
	}
	defer cleanupPods()

	// By("waiting for the webserver pods to transition to Running state")
	for _, podName := range podNames {
		err = waitTimeoutForPodRunningInNamespace(client, podName, testNamespace, podStartTimeout)
		if err != nil {
			return trace.Wrap(err, "pod %s failed to transition to Running state", podName)
		}
	}

	// By("waiting for connectivity to be verified")
	passed := false

	var body []byte
	getDetails := func() ([]byte, error) {
		return client.Get().
			Namespace(testNamespace).
			Prefix("proxy").
			Resource("services").
			Name(svc.Name).
			Suffix("read").
			DoRaw()
	}

	getStatus := func() ([]byte, error) {
		return client.Get().
			Namespace(testNamespace).
			Prefix("proxy").
			Resource("services").
			Name(svc.Name).
			Suffix("status").
			DoRaw()
	}

	timeout := time.Now().Add(2 * time.Minute)
	for i := 0; !passed && timeout.After(time.Now()); i++ {
		time.Sleep(2 * time.Second)
		body, err = getStatus()
		if err != nil {
			log.Infof("attempt %v: service/pod still starting: %v)", i, err)
			continue
		}
		// validate if the container was able to find peers
		switch {
		case string(body) == "pass":
			passed = true
		case string(body) == "running":
			log.Infof("attempt %v: test still running", i)
		case string(body) == "fail":
			if body, err = getDetails(); err != nil {
				return trace.Wrap(err, "failed to read test details")
			} else {
				return trace.Wrap(err, "containers failed to find peers")
			}
		case strings.Contains(string(body), "no endpoints available"):
			log.Infof("attempt %v: waiting on service/endpoints", i)
		default:
			return trace.Errorf("unexpected response: [%s]", body)
		}
	}
	return nil
}

// podStartTimeout defines the amount of time to wait for a pod to start.
const podStartTimeout = 15 * time.Second

// pollInterval defines the amount of time to wait between attempts to poll pods/nodes.
const pollInterval = 2 * time.Second

// podCondition is an interface to verify the specific pod condition.
type podCondition func(pod *api.Pod) (bool, error)

// waitTimeoutForPodRunningInNamespace waits for a pod in the specified namespace
// to transition to 'Running' state within the specified amount of time.
func waitTimeoutForPodRunningInNamespace(client *kube.Client, podName string, namespace string, timeout time.Duration) error {
	return waitForPodCondition(client, namespace, podName, "running", timeout, func(pod *api.Pod) (bool, error) {
		if pod.Status.Phase == api.PodRunning {
			log.Infof("found pod '%s' on node '%s'", podName, pod.Spec.NodeName)
			return true, nil
		}
		if pod.Status.Phase == api.PodFailed {
			return true, trace.Errorf("pod in failed status: %s", fmt.Sprintf("%#v", pod))
		}
		return false, nil
	})
}

// waitForPodCondition waits until a pod is in the given condition within the specified amount of time.
func waitForPodCondition(client *kube.Client, ns, podName, desc string, timeout time.Duration, condition podCondition) error {
	log.Infof("waiting up to %v for pod %s status to be %s", timeout, podName, desc)
	for start := time.Now(); time.Since(start) < timeout; time.Sleep(pollInterval) {
		pod, err := client.Pods(ns).Get(podName)
		if err != nil {
			log.Infof("get pod %s in namespace '%s' failed, ignoring for %v: %v",
				podName, ns, pollInterval, err)
			continue
		}
		done, err := condition(pod)
		if done {
			// TODO: update to latest trace to wrap nil
			if err != nil {
				return trace.Wrap(err)
			}
			log.Infof("waiting for pod succeeded")
			return nil
		}
		log.Infof("waiting for pod %s in namespace '%s' status to be '%s'"+
			"(found phase: %q, readiness: %t) (%v elapsed)",
			podName, ns, desc, pod.Status.Phase, podReady(pod), time.Since(start))
	}
	return trace.Errorf("gave up waiting for pod '%s' to be '%s' after %v", podName, desc, timeout)
}

// launchNetTestPodPerNode schedules a new test pod on each of specified nodes
// using the specified containerImage.
func launchNetTestPodPerNode(client *kube.Client, nodes *api.NodeList, name, containerImage string) ([]string, error) {
	podNames := []string{}
	totalPods := len(nodes.Items)

	for _, node := range nodes.Items {
		pod, err := client.Pods(testNamespace).Create(&api.Pod{
			ObjectMeta: api.ObjectMeta{
				GenerateName: name + "-",
				Labels: map[string]string{
					"name": name,
				},
			},
			Spec: api.PodSpec{
				Containers: []api.Container{
					{
						Name:  "webserver",
						Image: containerImage,
						Args: []string{
							"-service=" + name,
							// `nettest` container finds peers by looking up list of service endpoints
							fmt.Sprintf("-peers=%d", totalPods),
							"-namespace=" + testNamespace},
						Ports: []api.ContainerPort{{ContainerPort: 8080}},
					},
				},
				NodeName:      node.Name,
				RestartPolicy: api.RestartPolicyNever,
			},
		})
		if err != nil {
			return nil, trace.Wrap(err, "failed to create pod")
		}
		log.Infof("created pod %s on node %s", pod.ObjectMeta.Name, node.Name)
		podNames = append(podNames, pod.ObjectMeta.Name)
	}
	return podNames, nil
}

// podReady returns whether pod has a condition of `Ready` with a status of true.
func podReady(pod *api.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == api.PodReady && cond.Status == api.ConditionTrue {
			return true
		}
	}
	return false
}

// createNamespaceIfNeeded creates a namespace if not already created.
func createNamespaceIfNeeded(client *kube.Client, namespace string) error {
	log.Infof("creating %s namespace", namespace)
	if _, err := client.Namespaces().Get(namespace); err != nil {
		log.Infof("%s namespace not found: %v", namespace, err)
		_, err = client.Namespaces().Create(&api.Namespace{ObjectMeta: api.ObjectMeta{Name: namespace}})
		if err != nil {
			return trace.Wrap(err)
		}
	}
	return nil
}
