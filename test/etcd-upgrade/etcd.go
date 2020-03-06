/*
Copyright 2020 Gravitational, Inc.

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

package etcd

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	etcdconf "github.com/gravitational/coordinate/config"
	backup "github.com/gravitational/etcd-backup/lib/etcd"
	"github.com/gravitational/trace"
	"github.com/sirupsen/logrus"
	clientv2 "go.etcd.io/etcd/client"
	"go.etcd.io/etcd/clientv3"
)

const etcdTestContainerName = "planet-etcd-upgrade-test-0"
const etcdImage = "gcr.io/etcd-development/etcd"
const etcdPort = "22379"
const etcdUpgradePort = "22380"

// TestUpgradeBetweenVersions runs a mock-up of the etcd upgrade process used by planet and triggered by gravity on
// a multi-node cluster. The overall upgrade process is described by gravity here:
// https://github.com/gravitational/gravity/blob/ddcf66dbb599138cc79fc1b9be427d706c6c8fb8/lib/update/cluster/phases/etcd.go
//
// The specific items this test is meant to validate are:
// - Watches - watches rely on the revision for tracking changes, so the upgrade process needs to preserve the revision
// - Internals - To maintain the revision, we have to internally change the etcd database, so test for stability
//
// Note - Only watches against etcd v3 are being fixed, etcd v2 clients need to be restarted when doing an upgrade
// as in the future, all etcd clients will be upgraded to v3, or we'll use the v2 API emulation with the v3 datastore
// which as of Jan 2020 is experimental.
func TestUpgradeBetweenVersions(from, to string) error {
	etcdDir, err := ioutil.TempDir("", "etcd")
	assertNoErr(err)
	defer os.RemoveAll(etcdDir)
	logrus.WithField("dir", etcdDir).Info("temp directory")

	dataPathFrom := filepath.Join(etcdDir, from)
	dataPathTo := filepath.Join(etcdDir, to)
	backupPath := filepath.Join(etcdDir, "backup.json")

	assertNoErr(os.MkdirAll(dataPathFrom, 0777))
	assertNoErr(os.MkdirAll(dataPathTo, 0777))

	logrus.Info("Starting etcd")
	assertNoErr(startEtcd(config{
		dataDir: dataPathFrom,
		port:    etcdPort,
		version: from,
	}))

	logrus.Info("Waiting for etcd to become healthy")
	assertNoErr(waitEtcdHealthy(context.TODO(), etcdPort))

	logrus.Info("Setting up watcher")
	watcher := newWatcher()
	assertNoErr(watcher.test())

	logrus.Info("Writing test data to etcd")
	err = writeEtcdTestData()
	if err != nil {
		return trace.Wrap(err)
	}

	logrus.Info("re-checking the watch")
	assertNoErr(watcher.test())

	logrus.Info("backing up the etcd cluster")
	writer, err := os.Create(backupPath)
	assertNoErr(err)
	backupConf := backup.BackupConfig{
		EtcdConfig: etcdconf.Config{
			Endpoints: []string{fmt.Sprintf("http://127.0.0.1:%v", etcdPort)},
		},
		Prefix: []string{""},
		Writer: writer,
		Log:    logrus.StandardLogger(),
	}
	assertNoErr(backup.Backup(context.TODO(), backupConf))
	assertNoErr(writer.Close())

	logrus.Info("Stopping etcd for upgrade")
	assertNoErr(stopEtcd())

	logrus.Info("Starting temporary etcd cluster to initialize database")
	assertNoErr(startEtcd(config{
		dataDir: dataPathTo,
		port:    etcdUpgradePort,
		version: from,
	}))

	logrus.Info("Waiting for etcd to become healthy")
	assertNoErr(waitEtcdHealthy(context.TODO(), etcdUpgradePort))

	logrus.Info("Stopping temporary etcd cluster")
	assertNoErr(stopEtcd())

	logrus.Info("Running offline restore against etcd DB")
	restoreConf := backup.RestoreConfig{
		File: backupPath,
		Log:  logrus.StandardLogger(),
	}
	assertNoErr(backup.OfflineRestore(context.TODO(), restoreConf, dataPathTo))

	logrus.Info("Starting temporary etcd cluster to restore backup")
	assertNoErr(startEtcd(config{
		dataDir: dataPathTo,
		port:    etcdUpgradePort,
		version: from,
	}))

	logrus.Info("Waiting for etcd to become healthy")
	assertNoErr(waitEtcdHealthy(context.TODO(), etcdUpgradePort))

	logrus.Info("Restoring V2 etcd data and migrating v2 keys to v3")
	restoreConf = backup.RestoreConfig{
		EtcdConfig: etcdconf.Config{
			Endpoints: []string{fmt.Sprintf("http://127.0.0.1:%v", etcdUpgradePort)},
		},
		Prefix:        []string{"/"},        // Restore all etcd data
		MigratePrefix: []string{"/migrate"}, // migrate kubernetes data to etcd3 datastore
		File:          backupPath,
		SkipV3:        true,
		Log:           logrus.StandardLogger(),
	}
	assertNoErr(backup.Restore(context.TODO(), restoreConf))

	logrus.Info("Stopping temporary etcd cluster")
	assertNoErr(stopEtcd())

	logrus.Info("Starting etcd cluster as new version")
	assertNoErr(startEtcd(config{
		dataDir: dataPathTo,
		port:    etcdPort,
		version: from,
	}))

	logrus.Info("Waiting for etcd to become healthy")
	assertNoErr(waitEtcdHealthy(context.TODO(), etcdPort))

	logrus.Info("re-checking the watch")
	assertNoErr(watcher.test())

	logrus.Info("Validating all data exists / is expected value")
	assertNoErr(validateEtcdTestData())

	logrus.Info("re-checking the watch")
	assertNoErr(watcher.test())

	logrus.Info("shutting down etcd, test complete")
	assertNoErr(stopEtcd())

	return nil
}

type config struct {
	dataDir         string
	forceNewCluster bool
	port            string
	version         string
}

func startEtcd(c config) error {
	cli := dockerClient()

	// Check if a container already exists, and if it does, clean up the container
	containers, err := cli.ContainerList(context.TODO(), types.ContainerListOptions{All: true})
	assertNoErr(err)
	for _, container := range containers {
		if len(container.Names) > 0 && container.Names[0] == fmt.Sprintf("/%v", etcdTestContainerName) {
			logrus.Info("Stopping Container ID: ", container.ID)
			_ = cli.ContainerStop(context.TODO(), container.ID, nil)
			_ = cli.ContainerRemove(context.TODO(), container.ID, types.ContainerRemoveOptions{})
		}
	}

	// Pull the image from the upstream repository
	image := fmt.Sprintf("%v:%v", etcdImage, c.version)
	reader, err := cli.ImagePull(context.TODO(), image, types.ImagePullOptions{})
	assertNoErr(err)
	io.Copy(os.Stdout, reader)

	etcdClientPort, err := nat.NewPort("tcp", "2379")
	if err != nil {
		return trace.Wrap(err)
	}

	cmd := []string{"/usr/local/bin/etcd",
		"--data-dir", "/etcd-data",
		"--enable-v2=true",
		"--listen-client-urls", "http://0.0.0.0:2379",
		"--advertise-client-urls", "http://127.0.0.1:2379",
		"--snapshot-count", "100",
		"--initial-cluster-state", "new",
	}
	if c.forceNewCluster {
		cmd = append(cmd, "--force-new-cluster")
	}

	cont, err := cli.ContainerCreate(context.TODO(),
		&container.Config{
			User:  fmt.Sprint(os.Getuid()),
			Image: image,
			Cmd:   cmd,
		},
		&container.HostConfig{
			PortBindings: nat.PortMap{
				etcdClientPort: []nat.PortBinding{
					nat.PortBinding{
						HostIP:   "127.0.0.1",
						HostPort: c.port,
					},
				},
			},
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: c.dataDir,
					Target: "/etcd-data",
				},
			},
		},
		&network.NetworkingConfig{},
		etcdTestContainerName)
	if err != nil {
		return trace.Wrap(err)
	}

	err = cli.ContainerStart(context.TODO(), cont.ID, types.ContainerStartOptions{})
	if err != nil {
		return trace.Wrap(err)
	}
	logrus.Printf("Container %s (%s) is started\n", etcdTestContainerName, cont.ID)

	return nil
}

func stopEtcd() error {
	cli := dockerClient()
	t := 15 * time.Second
	return trace.Wrap(cli.ContainerStop(context.TODO(), etcdTestContainerName, &t))
}

func dockerClient() *client.Client {
	cli, err := client.NewEnvClient()
	if err != nil {
		panic(err)
	}
	cli.NegotiateAPIVersion(context.TODO())
	return cli
}

func getEtcdClients(port string) (*clientv2.Client, *clientv3.Client) {
	cfg := clientv2.Config{
		Endpoints: []string{fmt.Sprintf("http://127.0.0.1:%v", port)},
		Transport: clientv2.DefaultTransport,
		// set timeout per request to fail fast when the target endpoint is unavailable
		HeaderTimeoutPerRequest: time.Second,
	}
	clientv2, err := clientv2.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	clientv3, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{fmt.Sprintf("http://127.0.0.1:%v", port)},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}

	return &clientv2, clientv3
}

type watcher struct {
	v2               clientv2.Watcher
	v3               clientv3.WatchChan
	lastSeenRevision int64
}

func newWatcher() watcher {
	cv2, cv3 := getEtcdClients(etcdPort)
	kapi := clientv2.NewKeysAPI(*cv2)

	return watcher{
		v3: cv3.Watch(context.TODO(), "v3-watch"),
		v2: kapi.Watcher("/v2-watch", &clientv2.WatcherOptions{}),
	}
}

func (w *watcher) test() error {
	_, cv3 := getEtcdClients(etcdPort)
	value := fmt.Sprint(rand.Uint64())

	// trigger v3 watch
	resp, err := cv3.Put(context.TODO(), "v3-watch", value)
	if err != nil {
		return trace.Wrap(err)
	}
	logrus.WithFields(logrus.Fields{
		"last_seen_revision": w.lastSeenRevision,
		"put_revision":       resp.Header.GetRevision(),
	}).Info("put v3-watch")

	// check that watch was triggered
	ev := <-w.v3
	w.lastSeenRevision = ev.Header.GetRevision()
	if string(ev.Events[0].Kv.Key) != "v3-watch" || string(ev.Events[0].Kv.Value) != value {
		return trace.BadParameter("Unexpected v3 watcher event, value or key doesn't match expected value").
			AddFields(map[string]interface{}{
				"key":   string(ev.Events[0].Kv.Key),
				"value": string(ev.Events[0].Kv.Value),
			})
	}
	logrus.Info("v3 watch is good.",
		" Key: ", string(ev.Events[0].Kv.Key),
		" Revision: ", ev.CompactRevision,
		" Header.Revision: ", ev.Header.GetRevision(),
	)

	return nil
}

const numKeys = 4000
const numWritesPerKey = 3

// create a sample set of data so that the revision index moves sufficiently forward
func writeEtcdTestData() error {
	cv2, cv3 := getEtcdClients(etcdPort)
	kapi := clientv2.NewKeysAPI(*cv2)

	// etcdv2
	// create test keys
	for i := 1; i <= numKeys; i++ {
		// write to each key more than once
		for k := 1; k <= numWritesPerKey; k++ {
			_, err := kapi.Set(context.TODO(), fmt.Sprintf("/etcdv2/%v", i), fmt.Sprintf("%v:%v", i, k), nil)
			if err != nil {
				return trace.Wrap(err)
			}
		}
	}

	// migration keys
	// these are keys that should be migrated from the v2 to v3 store during an upgrade
	for i := 1; i <= numKeys; i++ {
		// write to each key more than once
		for k := 1; k <= numWritesPerKey; k++ {
			_, err := kapi.Set(context.TODO(), fmt.Sprintf("/migrate/%v", i), fmt.Sprintf("%v:%v", i, k), nil)
			if err != nil {
				return trace.Wrap(err)
			}
		}
	}

	// etcd v3
	// create 10k keys
	for i := 1; i <= numKeys; i++ {
		// write to each key more than once
		for k := 1; k <= numWritesPerKey; k++ {
			_, err := cv3.Put(context.TODO(), fmt.Sprintf("/etcdv3/%v", i), fmt.Sprintf("%v:%v", i, k))
			if err != nil {
				return trace.Wrap(err)
			}
		}
	}

	return nil
}

// validateEtcdTestData checks all the expected keys exist after the upgrade
func validateEtcdTestData() error {
	cv2, cv3 := getEtcdClients(etcdPort)
	kapi := clientv2.NewKeysAPI(*cv2)

	// etcdv2
	logrus.Info("validating etcdv2")
	for i := 1; i <= numKeys; i++ {
		// write to each key more than once

		key := fmt.Sprintf("/etcdv2/%v", i)
		resp, err := kapi.Get(context.TODO(), key, &clientv2.GetOptions{})
		if err != nil {
			return trace.Wrap(err)
		}

		expected := fmt.Sprintf("%v:%v", i, numWritesPerKey)
		if resp.Node.Value != expected {
			return trace.BadParameter("unexpected value for key").AddFields(map[string]interface{}{
				"key":      key,
				"expected": expected,
				"value":    resp.Node.Value,
			})
		}

	}

	// migration keys
	// these are keys that should be migrated from the v2 to v3 store during an upgrade
	logrus.Info("validating migration from v2 to v3")
	for i := 1; i <= numKeys; i++ {
		// write to each key more than once

		key := fmt.Sprintf("/migrate/%v", i)
		resp, err := cv3.Get(context.TODO(), key)
		if err != nil {
			return trace.Wrap(err)
		}

		expected := fmt.Sprintf("%v:%v", i, numWritesPerKey)
		if len(resp.Kvs) != 1 {
			return trace.BadParameter("expected to retrieve a key").AddFields(map[string]interface{}{
				"key":      key,
				"expected": expected,
			})
		}
		if string(resp.Kvs[0].Value) != expected {
			return trace.BadParameter("unexpected value for key").AddFields(map[string]interface{}{
				"key":      key,
				"expected": expected,
				"value":    string(resp.Kvs[0].Value),
			})
		}
	}

	// etcd v3
	logrus.Info("validating etcdv3")
	for i := 1; i <= numKeys; i++ {
		// write to each key more than once

		key := fmt.Sprintf("/etcdv3/%v", i)
		resp, err := cv3.Get(context.TODO(), key)
		if err != nil {
			return trace.Wrap(err)
		}

		expected := fmt.Sprintf("%v:%v", i, numWritesPerKey)
		if string(resp.Kvs[0].Value) != expected {
			return trace.BadParameter("unexpected value for key").AddFields(map[string]interface{}{
				"key":      key,
				"expected": expected,
				"value":    string(resp.Kvs[0].Value),
			})
		}

	}

	return nil
}

func waitEtcdHealthy(ctx context.Context, port string) error {
	cv2, _ := getEtcdClients(port)
	mapi := clientv2.NewMembersAPI(*cv2)
	for {
		leader, _ := mapi.Leader(ctx)
		if leader != nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
}

func assertNoErr(err error) {
	if err != nil {
		panic(trace.DebugReport(err))
	}
}
