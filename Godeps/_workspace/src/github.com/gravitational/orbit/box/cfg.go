package box

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/gravitational/cube/Godeps/_workspace/src/github.com/gravitational/trace"
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

	// Rootfs is a root filesystem of the container
	Rootfs string

	// Mounts is a list of device moutns passed to the server
	Mounts Mounts
	// Capabilities is a list of capabilities of this container
	Capabilities []string
	// DataDir is a directory where libcontainer stores the container state
	DataDir string
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
}

type EnvPair struct {
	Name string
	Val  string
}

type EnvVars []EnvPair

func (vars *EnvVars) Set(v string) error {
	vals := strings.Split(v, "=")
	if len(vals) != 2 {
		return trace.Errorf(
			"set environment variable separated by '=', e.g. KEY=VAL")
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
	Src string
	Dst string
}

type Mounts []Mount

func (m *Mounts) Set(v string) error {
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
