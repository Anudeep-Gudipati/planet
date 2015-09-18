/*
client.go implements the client interface to Planet box. When a user runs any command, like
"planet stop", it connects to the running instance of itself via a POSIX socket.

It is done via client.Enter(), which executes the command (any process name) and
proxies stdin/stdout to it.
*/
package box

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/url"
	"os"

	"github.com/gravitational/planet/Godeps/_workspace/src/github.com/gravitational/log"
	"github.com/gravitational/planet/Godeps/_workspace/src/github.com/gravitational/trace"
	"github.com/gravitational/planet/Godeps/_workspace/src/golang.org/x/net/websocket"
)

type client struct {
	path string
}

func Connect(path string) (ContainerServer, error) {
	path, err := checkPath(serverSockPath(path), false)
	if err != nil {
		return nil, checkError(err)
	}
	return &client{path: path}, nil
}

// Enter connects to a socket and
func (c *client) Enter(cfg ProcessConfig) error {
	u := url.URL{Host: "planet", Scheme: "ws", Path: "/v1/enter"}
	data, err := json.Marshal(cfg)
	if err != nil {
		return trace.Wrap(err)
	}
	q := u.Query()
	q.Set("params", hex.EncodeToString(data))
	u.RawQuery = q.Encode()

	wscfg, err := websocket.NewConfig(u.String(), "http://localhost")
	if err != nil {
		return trace.Wrap(err, "failed to enter container")
	}
	conn, err := net.Dial("unix", c.path)
	if err != nil {
		return checkError(
			trace.Wrap(err, "failed to connect to planet socket"))
	}
	clt, err := websocket.NewClient(wscfg, conn)
	if err != nil {
		return trace.Wrap(err)
	}
	defer clt.Close()

	exitC := make(chan error, 2)

	go func() {
		_, err := io.Copy(cfg.Out, clt)
		log.Infof("oops! output is done!")
		os.Stdin.Close()
		exitC <- err
	}()

	go func() {
		_, err := io.Copy(clt, cfg.In)
		log.Infof("oops! input is done!")
		exitC <- err
	}()

	log.Infof("connected to container namespace")

	for i := 0; i < 2; i++ {
		<-exitC
	}
	//<-exitC
	//<-exitC
	return nil
}

func checkError(err error) error {
	var o error // original error
	if e, ok := err.(*trace.TraceErr); ok {
		o = e.OrigError()
	} else {
		o = err
	}

	if os.IsNotExist(o) {
		return &ErrConnect{Err: err}
	}
	if _, ok := err.(*net.OpError); ok {
		return &ErrConnect{Err: err}
	}
	return err
}

// IsConnectError returns true if it was a connection error
func IsConnectError(e error) bool {
	_, ok := e.(*ErrConnect)
	return ok
}
