package main

import (
	"encoding/json"
	"os"

	"github.com/gravitational/planet/lib/box"
	"github.com/gravitational/trace"

	log "github.com/Sirupsen/logrus"
	"github.com/jochenvg/go-udev"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/devices"
)

// newUdevListener creates a new udev event listener listening
// for events on block devices of type `disk`
func newUdevListener(rootfs, socketPath string) (*udevListener, error) {
	udev := udev.Udev{}
	monitor := udev.NewMonitorFromNetlink("udev")
	doneC := make(chan struct{})

	monitor.FilterAddMatchSubsystemDevtype("block", "disk")
	monitor.FilterAddMatchTag("systemd")

	recvC, err := monitor.DeviceChan(doneC)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	listener := &udevListener{
		rootfs:     rootfs,
		socketPath: socketPath,
		monitor:    monitor,
		doneC:      doneC,
		recvC:      recvC,
	}
	go listener.loop()

	return listener, nil
}

// Close closes the listener and removes the installed udev filters
func (r *udevListener) Close() error {
	removeFilters := func() {
		r.monitor.FilterRemove()
		r.monitor.FilterUpdate()
	}
	close(r.doneC)
	removeFilters()
	return nil
}

// udevListener defines the task of listening to udev events
// and dispatching corresponding device commands into the planet container
type udevListener struct {
	rootfs     string
	socketPath string
	monitor    *udev.Monitor
	doneC      chan struct{}
	recvC      <-chan *udev.Device
}

// loop runs the actual udev event loop
func (r *udevListener) loop() {
	const cgroupPermissions = "rwm"

	for device := range r.recvC {
		switch device.Action() {
		case "add":
			deviceData, err := devices.DeviceFromPath(device.Devnode(), cgroupPermissions)
			if err != nil {
				log.Infof("failed to query device: %v", err)
				continue
			}
			if err := r.createDevice(deviceData); err != nil {
				log.Infof("failed to create device `%v` in container: %v", device.Devnode(), err)
			}
		case "remove":
			if err := r.removeDevice(device.Devnode()); err != nil {
				log.Infof("failed to remove device `%v` in container: %v", device.Devnode(), err)
			}
		default:
			log.Infof("unknown action %v for %v", device.Action(), device.Devnode())
		}
	}
}

// createDevice dispatches a command to add a new device in the container
func (r *udevListener) createDevice(device *configs.Device) error {
	log.Infof("createDevice: %v", device)

	deviceJson, err := json.Marshal(device)
	if err != nil {
		return trace.Wrap(err)
	}

	return enter(r.rootfs, r.socketPath, deviceCmd("add", "--data", string(deviceJson)))
}

// removeDevice dispatches a command to remove a device in the container
func (r *udevListener) removeDevice(node string) error {
	log.Infof("removeDevice: %v", node)

	return enter(r.rootfs, r.socketPath, deviceCmd("remove", "--node", node))
}

// deviceCmd creates a configuration object to invoke the device agent
// with the specified arguments
func deviceCmd(args ...string) *box.ProcessConfig {
	const cmd = "/usr/bin/planet"
	config := &box.ProcessConfig{
		User: "root",
		Args: []string{cmd, "--debug", "device"},
		In:   os.Stdin,
		Out:  os.Stdout,
	}

	config.Args = append(config.Args, args...)
	return config
}
