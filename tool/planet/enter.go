package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"

	"github.com/gravitational/planet/Godeps/_workspace/src/github.com/docker/docker/pkg/term"
	"github.com/gravitational/planet/Godeps/_workspace/src/github.com/gravitational/log"
	"github.com/gravitational/planet/Godeps/_workspace/src/github.com/gravitational/trace"
	"github.com/gravitational/planet/lib/box"
	"github.com/gravitational/planet/lib/monitoring"
)

// commandOutput is a io.Writer that decodes structured process output and delegates
// the plain text to w
type commandOutput struct {
	w        io.Writer
	exitCode int
}

// exitError is an error that describes the event of a process exiting with a non-zero value
type exitError struct {
	exitCode int
}

func enterConsole(rootfs, cmd, user string, tty bool, args []string) (err error) {
	cmdOut := &commandOutput{w: os.Stdout}

	cfg := box.ProcessConfig{
		In:   os.Stdin,
		Out:  cmdOut,
		Args: append([]string{cmd}, args...),
	}

	if tty {
		s, err := term.GetWinsize(os.Stdin.Fd())
		if err != nil {
			return trace.Wrap(err)
		}
		cfg.TTY = &box.TTY{H: int(s.Height), W: int(s.Width)}
	}

	err = enter(rootfs, cfg)
	if err == nil && cmdOut.exitCode != 0 {
		err = trace.Wrap(&exitError{cmdOut.exitCode})
	}
	return err
}

// enter initiates the process in the namespaces of the container
// managed by the planet master process and mantains websocket connection
// to proxy input and output
func enter(rootfs string, cfg box.ProcessConfig) error {
	log.Infof("enter: %v %#v", rootfs, cfg)
	if cfg.TTY != nil {
		oldState, err := term.SetRawTerminal(os.Stdin.Fd())
		if err != nil {
			return err
		}
		defer term.RestoreTerminal(os.Stdin.Fd(), oldState)
	}
	s, err := box.Connect(rootfs)
	if err != nil {
		return err
	}

	return s.Enter(cfg)
}

// stop interacts with systemctl's halt feature
func stop(path string) error {
	log.Infof("stop: %v", path)
	cfg := box.ProcessConfig{
		User: "root",
		Args: []string{"/bin/systemctl", "halt"},
		In:   os.Stdin,
		Out:  os.Stdout,
	}

	return enter(path, cfg)
}

// status checks status of the running planet cluster and outputs results as
// JSON to stdout.
func status(rootfs string) (err error) {
	log.Infof("checking status in %s", rootfs)

	var statusCmd = []string{"/usr/bin/planet", "--from-container", "status"}
	var data []byte

	data, err = enterCommand(rootfs, statusCmd)
	if data != nil {
		if _, errWrite := os.Stdout.Write(data); errWrite != nil {
			return trace.Wrap(errWrite, "failed to output status")
		}
	}
	return err
}

// containerStatus reports the current cluster status.
// It assumes the context of the planet container.
func containerStatus() (ok bool, err error) {
	systemStatus, err := monitoring.Status()
	if err != nil {
		return false, trace.Wrap(err, "failed to check system status")
	}
	ok = systemStatus.Status == monitoring.SystemStatusRunning

	data, err := json.Marshal(systemStatus)
	if err != nil {
		return ok, trace.Wrap(err, "failed to unmarshal status data")
	}
	if _, err = os.Stdout.Write(data); err != nil {
		return ok, trace.Wrap(err, "failed to output status")
	}

	return ok, nil
}

// enterCommand is a helper function that runs a command as a root
// in the namespace of planet's container. It returns error
// if command failed, or command standard output otherwise
func enterCommand(rootfs string, args []string) ([]byte, error) {
	buf := &bytes.Buffer{}
	cmdOut := &commandOutput{w: buf}
	cfg := box.ProcessConfig{
		User: "root",
		Args: args,
		In:   os.Stdin,
		Out:  cmdOut,
	}
	err := enter(rootfs, cfg)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if cmdOut.exitCode != 0 {
		err = trace.Wrap(&exitError{cmdOut.exitCode})
	}
	return buf.Bytes(), err
}

func (r *commandOutput) Write(p []byte) (n int, err error) {
	var msg box.Message

	err = json.Unmarshal(p, &msg)
	r.w.Write([]byte(msg.Payload))
	r.exitCode = msg.ExitCode
	return len(p), err
}

func (err exitError) Error() string {
	return "exit status " + strconv.FormatInt(int64(err.exitCode), 10)
}
