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
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/gravitational/planet/lib/constants"
	"github.com/gravitational/planet/lib/utils"

	"github.com/gravitational/satellite/agent"
	"github.com/gravitational/satellite/cmd"
	"github.com/gravitational/trace"
	log "github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

// setupResolver finds the kube-dns service address, and writes an environment file accordingly
func setupResolver(ctx context.Context, role agent.Role) error {
	client, err := cmd.GetKubeClientFromPath(constants.KubeletConfigPath)
	if err != nil {
		return trace.Wrap(err)
	}

	err = utils.Retry(ctx, math.MaxInt64, 1*time.Second, func() error {
		if role == agent.RoleMaster {
			for _, name := range []string{"kube-dns", "kube-dns-worker"} {
				err := createService(name)
				if err != nil {
					log.Warnf("Error creating service %v: %v.", name, err)
					return trace.Wrap(err)
				}
			}
		}

		err = updateEnvDNSAddresses(client, role)
		if err != nil {
			log.Warn("Error updating DNS env: ", err)
			return trace.Wrap(err)
		}
		return nil

	})
	return trace.Wrap(err)
}

func writeEnvDNSAddresses(addr []string, overwrite bool) error {
	env := fmt.Sprintf(`%v="%v"`, EnvDNSAddresses, strings.Join(addr, ","))
	env = fmt.Sprintln(env)

	if _, err := os.Stat(DNSEnvFile); !os.IsNotExist(err) && !overwrite {
		return nil
	}

	err := utils.SafeWriteFile(DNSEnvFile, []byte(env), constants.SharedReadMask)
	return trace.Wrap(err)
}

func updateEnvDNSAddresses(client *kubernetes.Clientset, role agent.Role) error {
	// try and locate the kube-dns svc clusterIP
	svcMaster, err := client.CoreV1().Services(metav1.NamespaceSystem).Get("kube-dns", metav1.GetOptions{})
	if err != nil {
		return trace.Wrap(err)
	}

	if svcMaster.Spec.ClusterIP == "" {
		return trace.BadParameter("service/kube-dns Spec.ClusterIP is empty")
	}

	svcWorker, err := client.CoreV1().Services(metav1.NamespaceSystem).Get("kube-dns-worker", metav1.GetOptions{})
	if err != nil {
		return trace.Wrap(err)
	}

	if svcWorker.Spec.ClusterIP == "" {
		return trace.BadParameter("service/kube-dns-worker Spec.ClusterIP is empty")
	}

	// If we're a master server, only use the master servers as a resolver.
	// This is because, we don't know if the second worker service will have any pods after future scaling operations
	//
	// If we're a worker, query the workers coredns first, and master second
	// This guaranteess any retries will not be handled by the same node
	if role == agent.RoleMaster {
		return trace.Wrap(writeEnvDNSAddresses([]string{svcMaster.Spec.ClusterIP}, true))
	}
	return trace.Wrap(writeEnvDNSAddresses([]string{svcWorker.Spec.ClusterIP, svcMaster.Spec.ClusterIP}, true))
}

// createService creates the kubernetes DNS service if it doesn't already exist.
// The service object is managed by gravity, but we create a placeholder here, so that we can read the IP address
// of the service, and configure kubelet with the correct DNS addresses before starting
func createService(name string) error {
	client, err := cmd.GetKubeClientFromPath(constants.SchedulerConfigPath)
	if err != nil {
		return trace.Wrap(err)
	}

	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceSystem,
			Labels: map[string]string{
				"k8s-app":                       name,
				"kubernetes.io/cluster-service": "true",
				"kubernetes.io/name":            "KubeDNS",
			},
			ResourceVersion: "0",
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"k8s-app": name,
			},
			Ports: []v1.ServicePort{
				{
					Port:       53,
					TargetPort: intstr.FromString("dns"),
					Protocol:   "UDP",
					Name:       "dns",
				}, {
					Port:       53,
					Protocol:   "TCP",
					Name:       "dns-tcp",
					TargetPort: intstr.FromString("dns-tcp"),
				}},
			SessionAffinity: "None",
		},
	}
	_, err = client.CoreV1().Services(metav1.NamespaceSystem).Create(service)
	if err != nil && !errors.IsAlreadyExists(err) {
		return trace.Wrap(err)
	}
	return nil
}
