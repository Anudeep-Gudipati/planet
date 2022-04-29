/*
Copyright 2018 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/gravitational/planet/lib/box"
	"github.com/gravitational/planet/lib/constants"

	"github.com/gravitational/trace"
	"github.com/jochenvg/go-udev"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/devices"
	log "github.com/sirupsen/logrus"
)

// newUdevListener creates a new udev event listener listening
// for events on block devices of type `disk`
func newUdevListener(seLinux bool) (*udevListener, error) {
	udev := udev.Udev{}
	monitor := udev.NewMonitorFromNetlink("udev")
	if monitor == nil {
		return nil, trace.BadParameter("failed to create udev monitor")
	}

	monitor.FilterAddMatchSubsystemDevtype("block", "disk")
	monitor.FilterAddMatchSubsystemDevtype("block", "partition")
	monitor.FilterAddMatchTag("systemd")

	ctx, cancel := context.WithCancel(context.Background())

	recvC, err := monitor.DeviceChan(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	listener := &udevListener{
		monitor: monitor,
		ctx:     ctx,
		cancel:  cancel,
		recvC:   recvC,
		seLinux: seLinux,
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
	r.cancel()
	removeFilters()
	return nil
}

// udevListener defines the task of listening to udev events
// and dispatching corresponding device commands into the planet container
type udevListener struct {
	monitor *udev.Monitor
	ctx     context.Context
	cancel  context.CancelFunc
	recvC   <-chan *udev.Device
	seLinux bool
}

// loop runs the actual udev event loop
func (r *udevListener) loop() {
	const cgroupPermissions = "rwm"

	for {
		select {
		case device := <-r.recvC:
			switch device.Action() {
			case "add":
				deviceData, err := devices.DeviceFromPath(device.Devnode(), cgroupPermissions)
				if err != nil {
					log.Warnf("failed to query device: %v", err)
					continue
				}
				if err := r.createDevice(deviceData); err != nil {
					log.Warnf("failed to create device `%v` in container: %v", device.Devnode(), err)
				}
			case "remove":
				if err := r.removeDevice(device.Devnode()); err != nil {
					log.Warnf("failed to remove device `%v` in container: %v", device.Devnode(), err)
				}
			default:
				log.Warnf("unknown action %v for %v", device.Action(), device.Devnode())
			}
		case <-r.ctx.Done():
			log.Warn("Udev listener stopped")
			return
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

	err = enter(r.deviceCmd("add", "--data", string(deviceJson)))
	return trace.Wrap(err)
}

// removeDevice dispatches a command to remove a device in the container
func (r *udevListener) removeDevice(node string) error {
	log.Infof("removeDevice: %v", node)

	err := enter(r.deviceCmd("remove", "--node", node))
	return trace.Wrap(err)
}

// deviceCmd creates a configuration object to invoke the device agent
// with the specified arguments
func (r *udevListener) deviceCmd(args ...string) box.EnterConfig {
	const cmd = "/usr/bin/planet"
	return box.EnterConfig{
		Process: box.ProcessConfig{
			User:         "root",
			Args:         append([]string{cmd, "--debug", "device"}, args...),
			In:           os.Stdin,
			Out:          os.Stdout,
			ProcessLabel: constants.ContainerRuntimeProcessLabel,
		},
		SELinux: r.seLinux,
	}
}
