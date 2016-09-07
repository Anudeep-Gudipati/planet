package utils

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
)

func TestUtils(t *testing.T) { TestingT(t) }

type UtilsSuite struct {
}

var _ = Suite(&UtilsSuite{})

func (s *UtilsSuite) TestHosts(c *C) {
	tcs := []struct {
		input    string
		expected string
		comment  string
		entries  []HostEntry
	}{
		{
			input:    "",
			expected: "127.0.0.1 example.com\n",
			entries:  []HostEntry{{Hostnames: "example.com", IP: "127.0.0.1"}},
			comment:  "Inserts new entry",
		},
		{
			input:    "",
			expected: "127.0.0.1 example.com\n127.0.0.2 localhost.localdomain\n",
			entries: []HostEntry{
				{Hostnames: "example.com", IP: "127.0.0.1"},
				{Hostnames: "localhost.localdomain", IP: "127.0.0.2"},
			},
			comment: "Inserts multiple new entries",
		},
		{
			input:    "127.0.0.2 example.com",
			expected: "127.0.0.1 example.com\n",
			entries: []HostEntry{
				{Hostnames: "example.com", IP: "127.0.0.1"},
			},
			comment: "Updates an existing entry",
		},
		{
			input: `# The following lines are desirable for IPv4 capable hosts
127.0.0.1       localhost
146.82.138.7    master.debian.org      master
127.0.3.4       example.com example
`,
			expected: `# The following lines are desirable for IPv4 capable hosts
127.0.0.1       localhost
146.82.138.7    master.debian.org      master
127.0.3.4       example.com example
127.0.0.1 example.com
`,
			entries: []HostEntry{{Hostnames: "example.com", IP: "127.0.0.1"}},
			comment: "Does not update entries if Hostnames does not match existing entry entirely",
		},

		{
			input: `# The following lines are desirable for IPv4 capable hosts
127.0.0.1       localhost
146.82.138.7 master.debian.org master
127.0.3.4       example.com example
`,
			expected: `# The following lines are desirable for IPv4 capable hosts
127.0.0.1       localhost
127.0.0.5 master.debian.org master
127.0.0.1 example.com example
`,
			entries: []HostEntry{
				{Hostnames: "example.com example", IP: "127.0.0.1"},
				{Hostnames: "master master.debian.org", IP: "127.0.0.5"},
			},
			comment: "Updates an existing entry (IP) based on Hostnames",
		},
		{
			input: `127.0.0.1   localhost localhost.localdomain localhost4 localhost4.localdomain4
::1         localhost localhost.localdomain localhost6 localhost6.localdomain6
192.168.122.1 opscenter.localhost.localdomain`,
			expected: `127.0.0.1 localhost localhost.localdomain localhost4 localhost4.localdomain4
::1 localhost localhost.localdomain localhost6 localhost6.localdomain6
192.168.122.1 opscenter.localhost.localdomain
`,
			entries: []HostEntry{
				{Hostnames: "localhost localhost.localdomain localhost4 localhost4.localdomain4", IP: "127.0.0.1"},
				{Hostnames: "localhost localhost.localdomain localhost6 localhost6.localdomain6", IP: "::1"},
			},
			comment: "Does not duplicate when Hostnames is a list of entries",
		},
	}
	tempDir := c.MkDir()
	for i, tc := range tcs {
		// test file
		buf := &bytes.Buffer{}
		err := UpsertHostsLines(strings.NewReader(tc.input), buf, tc.entries)
		c.Assert(buf.String(), Equals, tc.expected, Commentf(tc.comment))

		// test file
		testFile := filepath.Join(tempDir, fmt.Sprintf("test_case_%v", i+1))
		err = ioutil.WriteFile(testFile, []byte(tc.input), 0666)
		c.Assert(err, IsNil)

		err = UpsertHostsFile(tc.entries, testFile)
		c.Assert(err, IsNil)
		out, err := ioutil.ReadFile(testFile)
		c.Assert(err, IsNil)
		c.Assert(string(out), Equals, tc.expected, Commentf(tc.comment))
	}
}

func (s *UtilsSuite) TestDNS(c *C) {
	var tcs = []struct {
		input  string
		output string
		want   *DNSConfig
	}{
		{
			input: `# /etc/resolv.conf

domain localdomain
nameserver 8.8.8.8
nameserver 2001:4860:4860::8888
nameserver fe80::1%lo0
options ndots:5 timeout:10 attempts:3 rotate
options attempts 3
`,
			output: `domain localdomain
nameserver 8.8.8.8
nameserver 2001:4860:4860::8888
nameserver fe80::1%lo0
options ndots:5 timeout:10 attempts:3 rotate
`,
			want: &DNSConfig{
				Servers:    []string{"8.8.8.8", "2001:4860:4860::8888", "fe80::1%lo0"},
				Search:     []string{"localdomain"},
				Domain:     "localdomain",
				Ndots:      5,
				Timeout:    10,
				Attempts:   3,
				Rotate:     true,
				UnknownOpt: true, // the "options attempts 3" line
			},
		},
		{
			input: `# /etc/resolv.conf

search test invalid
domain localdomain
nameserver 8.8.8.8
`,
			output: `domain localdomain
nameserver 8.8.8.8
options ndots:1 timeout:5 attempts:2
`,
			want: &DNSConfig{
				Servers:  []string{"8.8.8.8"},
				Search:   []string{"localdomain"},
				Domain:   "localdomain",
				Ndots:    1,
				Timeout:  5,
				Attempts: 2,
			},
		},
		{
			input: `# /etc/resolv.conf

domain localdomain
search test invalid
nameserver 8.8.8.8
`,
			output: `domain localdomain
search test invalid
nameserver 8.8.8.8
options ndots:1 timeout:5 attempts:2
`,
			want: &DNSConfig{
				Servers:  []string{"8.8.8.8"},
				Search:   []string{"test", "invalid"},
				Domain:   "localdomain",
				Ndots:    1,
				Timeout:  5,
				Attempts: 2,
			},
		},
		{
			input: `# /etc/resolv.conf
`,
			output: `nameserver 127.0.0.1
nameserver ::1
options ndots:1 timeout:5 attempts:2
`,
			want: &DNSConfig{
				Servers:  defaultNS,
				Ndots:    1,
				Timeout:  5,
				Attempts: 2,
			},
		},
		{
			input: `# Generated by vio0 dhclient
search c.symbolic-datum-552.internal.
nameserver 169.254.169.254
nameserver 10.240.0.1
lookup file bind
`,
			output: `search c.symbolic-datum-552.internal.
nameserver 169.254.169.254
nameserver 10.240.0.1
options ndots:1 timeout:5 attempts:2
lookup file bind
`,
			want: &DNSConfig{
				Ndots:    1,
				Timeout:  5,
				Attempts: 2,
				Lookup:   []string{"file", "bind"},
				Servers:  []string{"169.254.169.254", "10.240.0.1"},
				Search:   []string{"c.symbolic-datum-552.internal."},
			},
		},
	}
	for i, tc := range tcs {
		comment := Commentf("test #%d (%v)", i+1)
		config, err := DNSReadConfig(strings.NewReader(tc.input))
		c.Assert(err, IsNil, comment)
		c.Assert(config, DeepEquals, tc.want, comment)
		c.Assert(config.String(), Equals, tc.output, comment)
	}
}
