package box

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/gravitational/configure/cstrings"
	"github.com/gravitational/trace"
)

type Config struct {
	//InitArgs list of arguments to exec as an init process
	InitArgs []string
	// InitEnv list of env variables to pass to executable
	InitEnv []string
	// InitUser is a user running the init process
	InitUser string

	// EnvFiles has a list of files that will generated when process starts
	EnvFiles []EnvFile
	// Files is an optional list of files that will be placed
	// in the container when started
	Files []File

	// Rootfs is a root filesystem of the container
	Rootfs string

	// SocketPath is a path to the socket file for remote command control.
	// Ignored with systemd socket-activation.
	SocketPath string

	// Mounts is a list of device moutns passed to the server
	Mounts Mounts
	// Devices is a list of devices to create inside the container
	Devices Devices
	// Capabilities is a list of capabilities of this container
	Capabilities []string
	// DataDir is a directory where libcontainer stores the container state
	DataDir string
}

// ClientConfig defines a configuration to connect to a running box.
type ClientConfig struct {
	Rootfs     string
	SocketPath string
}

// File is a file that will be placed in the container before start
type File struct {
	Path     string
	Contents io.Reader
	Mode     os.FileMode
	Owners   *FileOwner
}

type FileOwner struct {
	UID int
	GID int
}

// environment file to write when container starts
type EnvFile struct {
	Path string
	Env  EnvVars
}

// TTY is a tty settings passed to the device when allocating terminal
type TTY struct {
	W int
	H int
}

// ProcessConfig is a configuration passed to the process started
// in the namespace of the container
type ProcessConfig struct {
	In   io.Reader `json:"-"`
	Out  io.Writer `json:"-"`
	TTY  *TTY      `json:"tty"`
	Args []string  `json:"args"`
	User string    `json:"user"`
	Env  EnvVars   `json:"env"`
}

// Environment returns a slice of environment variables in key=value
// format as required by libcontainer
func (e *ProcessConfig) Environment() []string {
	if len(e.Env) == 0 {
		return []string{}
	}
	out := []string{}
	for _, keyval := range e.Env {
		out = append(out, fmt.Sprintf("%v=%v", keyval.Name, keyval.Val))
	}
	return out
}

type EnvPair struct {
	Name string `json:"name"`
	Val  string `json:"val"`
}

type EnvVars []EnvPair

func (vars *EnvVars) Get(v string) string {
	for _, p := range *vars {
		if p.Name == v {
			return p.Val
		}
	}
	return ""
}

func (vars *EnvVars) Append(k, v, delim string) {
	if existing := vars.Get(k); existing != "" {
		vars.Upsert(k, strings.Join([]string{existing, v}, delim))
	} else {
		vars.Upsert(k, v)
	}
}

func (vars *EnvVars) Upsert(k, v string) {
	for i, p := range *vars {
		if p.Name == k {
			(*vars)[i].Val = v
			return
		}
	}
	*vars = append(*vars, EnvPair{Name: k, Val: v})
}

func (vars *EnvVars) Set(v string) error {
	for _, i := range cstrings.SplitComma(v) {
		if err := vars.setItem(i); err != nil {
			return err
		}
	}
	return nil
}

func (vars *EnvVars) setItem(v string) error {
	vals := strings.Split(v, ":")
	if len(vals) != 2 {
		return trace.Errorf(
			"set environment variable separated by ':', e.g. KEY:VAL")
	}
	*vars = append(*vars, EnvPair{Name: vals[0], Val: vals[1]})
	return nil
}

func (vars *EnvVars) String() string {
	if len(*vars) == 0 {
		return ""
	}
	b := &bytes.Buffer{}
	for i, v := range *vars {
		fmt.Fprintf(b, "%v=%v", v.Name, v.Val)
		if i != len(*vars)-1 {
			fmt.Fprintf(b, " ")
		}
	}
	return b.String()
}

type Mount struct {
	Src      string
	Dst      string
	Readonly bool
}

type Mounts []Mount

func (m *Mounts) Set(v string) error {
	for _, i := range cstrings.SplitComma(v) {
		if err := m.setItem(i); err != nil {
			return err
		}
	}
	return nil
}

func (m *Mounts) setItem(v string) error {
	vals := strings.Split(v, ":")
	if len(vals) != 2 {
		return trace.Errorf(
			"set mounts separated by : e.g. src:dst")
	}
	*m = append(*m, Mount{Src: vals[0], Dst: vals[1]})
	return nil
}

func (m *Mounts) String() string {
	if len(*m) == 0 {
		return ""
	}
	b := &bytes.Buffer{}
	for i, v := range *m {
		fmt.Fprintf(b, "%v:%v", v.Src, v.Dst)
		if i != len(*m)-1 {
			fmt.Fprintf(b, " ")
		}
	}
	return b.String()
}

// Device represents a device that should be created in planet
type Device struct {
	// Path is the device path, treated as a glob
	Path string
	// Permissions is the device permissions
	Permissions string
	// FileMode is the device file mode
	FileMode os.FileMode
	// UID is the device user ID
	UID uint32
	// GID is the device group ID
	GID uint32
}

// Format formats the device to a string
func (d Device) Format() string {
	parts := []string{fmt.Sprintf("%v=%v", devicePath, d.Path)}
	if d.Permissions != "" {
		parts = append(parts, fmt.Sprintf("%v=%v", devicePermissions, d.Permissions))
	}
	if d.FileMode != 0 {
		parts = append(parts, fmt.Sprintf("%v=0%o", deviceFileMode, d.FileMode))
	}
	if d.UID != 0 {
		parts = append(parts, fmt.Sprintf("%v=%v", deviceUID, d.UID))
	}
	if d.GID != 0 {
		parts = append(parts, fmt.Sprintf("%v=%v", deviceGID, d.GID))
	}
	return strings.Join(parts, ";")
}

// Devices represents a list of devices
type Devices []Device

// Set sets the devices from CLI flags
func (d *Devices) Set(v string) error {
	for _, i := range cstrings.SplitComma(v) {
		if err := d.setItem(i); err != nil {
			return err
		}
	}
	return nil
}

func (d *Devices) setItem(v string) error {
	device, err := parseDevice(v)
	if err != nil {
		return trace.Wrap(err)
	}
	*d = append(*d, *device)
	return nil
}

// String converts devices to a string
func (d *Devices) String() string {
	if len(*d) == 0 {
		return ""
	}
	var formats []string
	for _, device := range *d {
		formats = append(formats, device.Format())
	}
	return strings.Join(formats, ";")
}

// parseDevice parses a single device value in the format:
// path=/dev/nvidia*;permissions=rwm;fileMode=0666
func parseDevice(value string) (*Device, error) {
	device := &Device{}
	for _, part := range strings.Split(value, ";") {
		kv := strings.Split(part, "=")
		if len(kv) != 2 {
			return nil, trace.BadParameter("malformed device format: %q", value)
		}
		switch kv[0] {
		case devicePath:
			device.Path = kv[1]
		case devicePermissions:
			device.Permissions = kv[1]
		case deviceFileMode:
			fileMode, err := strconv.ParseUint(kv[1], 8, 32)
			if err != nil {
				return nil, trace.Wrap(err)
			}
			device.FileMode = os.FileMode(fileMode)
		case deviceUID:
			uid, err := strconv.ParseUint(kv[1], 0, 32)
			if err != nil {
				return nil, trace.Wrap(err)
			}
			device.UID = uint32(uid)
		case deviceGID:
			gid, err := strconv.ParseUint(kv[1], 0, 32)
			if err != nil {
				return nil, trace.Wrap(err)
			}
			device.GID = uint32(gid)
		}
	}
	return device, nil
}

const (
	devicePath        = "path"
	devicePermissions = "permissions"
	deviceFileMode    = "fileMode"
	deviceUID         = "uid"
	deviceGID         = "gid"
)
