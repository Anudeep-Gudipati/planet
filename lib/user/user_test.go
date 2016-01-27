package user

import (
	"bytes"
	"strings"
	"testing"
)

const passwd = `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
bin:x:2:2:bin:/bin:/usr/sbin/nologin`

func TestReadsPasswdFile(t *testing.T) {
	rdr := strings.NewReader(passwd)
	r, err := NewPasswd(rdr)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.users) != 3 {
		t.Error("expected 3 users but got %d", len(r.users))
	}
}

func TestAddsUser(t *testing.T) {
	rw := bytes.NewBufferString(passwd)
	passwd, err := NewPasswd(rw)
	if err != nil {
		t.Fatal(err)
	}
	if passwd.w == nil {
		t.Fatal("expected w to be non-null")
	}
	passwd.Upsert(newUser(1005, 1005))
	if len(passwd.users) != 4 {
		t.Error("expected to add a user")
	}
	err = passwd.Save(nil)
	if err != nil {
		t.Fatal(err)
	}
	rdr := strings.NewReader(rw.String())
	passwd, err = NewPasswd(rdr)
	if err != nil {
		t.Fatal(err)
	}
	_, exists := passwd.Get("planet-agent")
	if !exists {
		t.Fatal("expected to find a user")
	}
}

func TestReplacesUser(t *testing.T) {
	rw := bytes.NewBufferString(passwd)
	r, err := NewPasswd(rw)
	if err != nil {
		t.Fatal(err)
	}
	u := newUser(1005, 1005)
	r.Upsert(u)
	u2 := newUser(1006, 1006)
	r.Upsert(u2)
	if len(r.users) != 4 {
		t.Error("expected to replace a user")
	}
	err = r.Save(nil)
	if err != nil {
		t.Fatal(err)
	}
	rdr := strings.NewReader(rw.String())
	r, err = NewPasswd(rdr)
	if err != nil {
		t.Fatal(err)
	}
	u3, exists := r.Get("planet-agent")
	if !exists {
		t.Fatal("expected to find a user")
	}
	if u3.Uid != 1006 || u3.Gid != 1006 {
		t.Error("unexpected uid/gid for replaced user")
	}
}

func newUser(uid, gid int) User {
	return User{
		Name:  "planet-agent",
		Pass:  "x",
		Uid:   uid,
		Gid:   gid,
		Home:  "/home/planet-agent",
		Shell: "/bin/false",
	}
}
