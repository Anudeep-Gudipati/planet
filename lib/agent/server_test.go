package agent

import (
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gravitational/planet/lib/agent/health"
	pb "github.com/gravitational/planet/lib/agent/proto/agentpb"
	serf "github.com/hashicorp/serf/client"
	"github.com/jonboulle/clockwork"
	"golang.org/x/net/context"
	. "gopkg.in/check.v1"
)

func init() {
	if testing.Verbose() {
		log.SetOutput(os.Stderr)
		log.SetLevel(log.InfoLevel)
	}
}

func TestAgent(t *testing.T) { TestingT(t) }

type AgentSuite struct{}

var _ = Suite(&AgentSuite{})

func (_ *AgentSuite) TestSetsSystemStatusFromMemberStatuses(c *C) {
	resp := &pb.StatusResponse{Status: &pb.SystemStatus{}}
	resp.Status.Nodes = []*pb.NodeStatus{
		{
			MemberStatus: &pb.MemberStatus{
				Name:   "foo",
				Status: pb.MemberStatus_Alive,
				Tags:   map[string]string{"role": string(RoleNode)},
			},
		},
		{
			MemberStatus: &pb.MemberStatus{
				Name:   "bar",
				Status: pb.MemberStatus_Failed,
				Tags:   map[string]string{"role": string(RoleMaster)},
			},
		},
	}

	setSystemStatus(resp)
	c.Assert(resp.Status.Status, Equals, pb.SystemStatus_Degraded)
}

func (_ *AgentSuite) TestSetsSystemStatusFromNodeStatuses(c *C) {
	resp := &pb.StatusResponse{Status: &pb.SystemStatus{}}
	resp.Status.Nodes = []*pb.NodeStatus{
		{
			Name:   "foo",
			Status: pb.NodeStatus_Running,
			MemberStatus: &pb.MemberStatus{
				Name:   "foo",
				Status: pb.MemberStatus_Alive,
				Tags:   map[string]string{"role": string(RoleNode)},
			},
		},
		{
			Name:   "bar",
			Status: pb.NodeStatus_Degraded,
			MemberStatus: &pb.MemberStatus{
				Name:   "bar",
				Status: pb.MemberStatus_Alive,
				Tags:   map[string]string{"role": string(RoleMaster)},
			},
			Probes: []*pb.Probe{
				{
					Checker: "qux",
					Status:  pb.Probe_Failed,
					Error:   "not available",
				},
			},
		},
	}

	setSystemStatus(resp)
	c.Assert(resp.Status.Status, Equals, pb.SystemStatus_Degraded)
}

func (_ *AgentSuite) TestDetectsNoMaster(c *C) {
	resp := &pb.StatusResponse{Status: &pb.SystemStatus{}}
	resp.Status.Nodes = []*pb.NodeStatus{
		{
			MemberStatus: &pb.MemberStatus{
				Name:   "foo",
				Status: pb.MemberStatus_Alive,
				Tags:   map[string]string{"role": string(RoleNode)},
			},
		},
		{
			MemberStatus: &pb.MemberStatus{
				Name:   "bar",
				Status: pb.MemberStatus_Alive,
				Tags:   map[string]string{"role": string(RoleNode)},
			},
		},
	}

	setSystemStatus(resp)
	c.Assert(resp.Status.Status, Equals, pb.SystemStatus_Degraded)
	c.Assert(resp.Summary, Equals, errNoMaster.Error())
}

func (_ *AgentSuite) TestSetsOkSystemStatus(c *C) {
	resp := &pb.StatusResponse{Status: &pb.SystemStatus{}}
	resp.Status.Nodes = []*pb.NodeStatus{
		{
			Name:   "foo",
			Status: pb.NodeStatus_Running,
			MemberStatus: &pb.MemberStatus{
				Name:   "foo",
				Status: pb.MemberStatus_Alive,
				Tags:   map[string]string{"role": string(RoleNode)},
			},
		},
		{
			Name:   "bar",
			Status: pb.NodeStatus_Running,
			MemberStatus: &pb.MemberStatus{
				Name:   "bar",
				Status: pb.MemberStatus_Alive,
				Tags:   map[string]string{"role": string(RoleMaster)},
			},
		},
	}

	expectedStatus := pb.SystemStatus_Running
	setSystemStatus(resp)
	c.Assert(resp.Status.Status, Equals, expectedStatus)
}

func (_ *AgentSuite) TestAgentProvidesStatus(c *C) {
	for _, testCase := range agentTestCases {
		c.Logf("running test %s", testCase.comment)

		clock := clockwork.NewFakeClock()
		localNode := testCase.members[0].Name
		remoteNode := testCase.members[1].Name
		localAgent := newLocalNode(localNode, remoteNode, testCase.rpcPort,
			testCase.members[:], testCase.checkers[0], clock, c)
		remoteAgent, err := newRemoteNode(remoteNode, localNode, testCase.rpcPort,
			testCase.members[:], testCase.checkers[1], clock, c)
		c.Assert(err, IsNil)

		clock.BlockUntil(2)
		clock.Advance(statusUpdateTimeout + time.Second)
		// Ensure that the status update loop has finished updating status
		clock.BlockUntil(2)

		req := &pb.StatusRequest{}
		resp, err := localAgent.rpc.Status(context.TODO(), req)
		c.Assert(err, IsNil)

		c.Assert(resp.Status.Status, Equals, testCase.status)
		c.Assert(resp.Status.Nodes, HasLen, len(testCase.members))
		localAgent.Close()
		remoteAgent.Close()
	}
}

var healthyTest = &fakeChecker{
	name: "healthy service",
}

var failedTest = &fakeChecker{
	name: "failing service",
	err:  errInvalidState,
}

var agentTestCases = []struct {
	comment  string
	status   pb.SystemStatus_Type
	members  [2]serf.Member
	checkers [][]health.Checker
	rpcPort  int
}{
	{
		comment: "Degraded due to a failed checker",
		status:  pb.SystemStatus_Degraded,
		members: [2]serf.Member{
			newMember("master", "alive"),
			newMember("node", "alive"),
		},
		checkers: [][]health.Checker{{healthyTest, failedTest}, {healthyTest, healthyTest}},
		rpcPort:  7676,
	},
	{
		comment: "Degraded due to a missing master node",
		status:  pb.SystemStatus_Degraded,
		members: [2]serf.Member{
			newMember("node-1", "alive"),
			newMember("node-2", "alive"),
		},
		checkers: [][]health.Checker{{healthyTest, healthyTest}, {healthyTest, healthyTest}},
		rpcPort:  7677,
	},
	{
		comment: "Running with all systems running",
		status:  pb.SystemStatus_Running,
		members: [2]serf.Member{
			newMember("master", "alive"),
			newMember("node", "alive"),
		},
		checkers: [][]health.Checker{{healthyTest, healthyTest}, {healthyTest, healthyTest}},
		rpcPort:  7678,
	},
}

func newLocalNode(node, peerNode string, rpcPort int, members []serf.Member,
	checkers []health.Checker, clock clockwork.Clock, c *C) *agent {
	agent := newAgent(node, peerNode, rpcPort, members, checkers, clock, c)
	agent.rpc = &fakeServer{&server{agent: agent}}
	err := agent.Start()
	c.Assert(err, IsNil)
	return agent
}

func newRemoteNode(node, peerNode string, rpcPort int, members []serf.Member,
	checkers []health.Checker, clock clockwork.Clock, c *C) (*agent, error) {
	network := "tcp"
	addr := fmt.Sprintf(":%d", rpcPort)
	listener, err := net.Listen(network, addr)
	if err != nil {
		return nil, err
	}

	agent := newAgent(node, peerNode, rpcPort, members, checkers, clock, c)
	err = agent.Start()
	c.Assert(err, IsNil)
	server := newRPCServer(agent, []net.Listener{listener})
	agent.rpc = server

	return agent, nil
}

func newMember(name string, status string) serf.Member {
	result := serf.Member{
		Name:   name,
		Status: status,
		Tags:   map[string]string{"role": string(RoleNode)},
	}
	if name == "master" {
		result.Tags["role"] = string(RoleMaster)
	}
	return result
}

// fakeSerfClient implements serfClient
type fakeSerfClient struct {
	members []serf.Member
}

func (r *fakeSerfClient) Members() ([]serf.Member, error) {
	return r.members, nil
}

func (r *fakeSerfClient) Stream(filter string, eventc chan<- map[string]interface{}) (serf.StreamHandle, error) {
	return serf.StreamHandle(0), nil
}

func (r *fakeSerfClient) Stop(handle serf.StreamHandle) error {
	return nil
}

func (r *fakeSerfClient) Close() error {
	return nil
}

func (r *fakeSerfClient) Join(peers []string, replay bool) (int, error) {
	return 0, nil
}

// fakeCache implements cache.Cache
type fakeCache struct {
	*pb.SystemStatus
	c *C
}

func (r *fakeCache) Update(status *pb.SystemStatus) error {
	r.SystemStatus = status
	return nil
}

func (r *fakeCache) UpdateNode(status *pb.NodeStatus) error {
	for i, node := range r.Nodes {
		if node.Name == status.Name {
			r.Nodes[i] = status
			return nil
		}
	}
	r.Nodes = append(r.Nodes, status)
	return nil
}

func (r fakeCache) RecentStatus() (*pb.SystemStatus, error) {
	return r.SystemStatus, nil
}

func (r fakeCache) RecentNodeStatus(name string) (*pb.NodeStatus, error) {
	for _, node := range r.Nodes {
		if node.Name == name {
			return node, nil
		}
	}
	return nil, nil
}

func (r *fakeCache) Close() error {
	return nil
}

// fakeServer implements RPCServer
type fakeServer struct {
	*server
}

func (_ *fakeServer) Stop() {}

func testDialRPC(port int) dialRPC {
	return func(member *serf.Member) (*client, error) {
		addr := fmt.Sprintf(":%d", port)
		client, err := NewClient(addr)
		if err != nil {
			return nil, err
		}
		return client, err
	}
}

func newAgent(node, peerNode string, rpcPort int, members []serf.Member,
	checkers []health.Checker, clock clockwork.Clock, c *C) *agent {
	return &agent{
		name:       node,
		serfClient: &fakeSerfClient{members: members},
		dialRPC:    testDialRPC(rpcPort),
		cache:      &fakeCache{c: c, SystemStatus: &pb.SystemStatus{Status: pb.SystemStatus_Unknown}},
		Checkers:   checkers,
		clock:      clock,
	}
}

var errInvalidState = errors.New("invalid state")

type fakeChecker struct {
	err  error
	name string
}

func (r fakeChecker) Name() string { return r.name }

func (r *fakeChecker) Check(reporter health.Reporter) {
	if r.err != nil {
		reporter.Add(&pb.Probe{
			Checker: r.name,
			Error:   r.err.Error(),
			Status:  pb.Probe_Failed,
		})
		return
	}
	reporter.Add(&pb.Probe{
		Checker: r.name,
		Status:  pb.Probe_Running,
	})
}
