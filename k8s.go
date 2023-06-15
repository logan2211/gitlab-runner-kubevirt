// Copyright 2023, Franklin "Snaipe" Mathieu <me@snai.pe>
//
// Use of this source-code is govered by the MIT license, which
// can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	k8sapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	kubevirtapi "kubevirt.io/api/core/v1"
	kubevirt "kubevirt.io/client-go/kubecli"
)

const (
	labelPrefix = "gitlab-runner-kubevirt.snai.pe"
)

func KubeConfig() (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err == rest.ErrNotInCluster {
		var kubeconfig string
		if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
		if kc := os.Getenv("KUBECONFIG"); kc != "" {
			kubeconfig = kc
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		return nil, err
	}
	return config, nil
}

func KubeClient() (kubevirt.KubevirtClient, error) {
	cfg, err := KubeConfig()
	if err != nil {
		return nil, err
	}
	return kubevirt.GetKubevirtClientFromRESTConfig(cfg)
}

func CreateJobVM(ctx context.Context, client kubevirt.KubevirtClient, jctx *JobContext) (*kubevirtapi.VirtualMachineInstance, error) {

	resources := kubevirtapi.ResourceRequirements{
		Requests: k8sapi.ResourceList{},
		Limits:   k8sapi.ResourceList{},
	}

	type entry struct {
		List  k8sapi.ResourceList
		Key   k8sapi.ResourceName
		Value string
	}
	toParse := []entry{
		{resources.Requests, k8sapi.ResourceCPU, jctx.CPURequest},
		{resources.Limits, k8sapi.ResourceCPU, jctx.CPULimit},
		{resources.Requests, k8sapi.ResourceMemory, jctx.MemoryRequest},
		{resources.Limits, k8sapi.ResourceMemory, jctx.MemoryLimit},
		{resources.Requests, k8sapi.ResourceEphemeralStorage, jctx.EphemeralStorageRequest},
		{resources.Limits, k8sapi.ResourceEphemeralStorage, jctx.EphemeralStorageLimit},
	}

	for _, e := range toParse {
		if e.Value == "" {
			continue
		}
		var err error
		if e.List[e.Key], err = resource.ParseQuantity(e.Value); err != nil {
			return nil, fmt.Errorf("parsing %s quantity: %w", e.Key, err)
		}
	}

	if jctx.Image == "" {
		return nil, fmt.Errorf("must specify a containerdisk image")
	}

	instanceTemplate := kubevirtapi.VirtualMachineInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kubevirtapi.GroupVersion.String(),
			Kind:       kubevirtapi.VirtualMachineInstanceGroupVersionKind.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: jctx.BaseName,
			Labels: map[string]string{
				labelPrefix + "/id": jctx.ID,
			},
		},
		Spec: kubevirtapi.VirtualMachineInstanceSpec{
			Domain: kubevirtapi.DomainSpec{
				Resources: resources,
				Machine: &kubevirtapi.Machine{
					Type: jctx.MachineType,
				},
				Devices: kubevirtapi.Devices{
					Disks: []kubevirtapi.Disk{
						{
							Name: "root",
						},
					},
				},
			},
			Volumes: []kubevirtapi.Volume{
				{
					Name: "root",
					VolumeSource: kubevirtapi.VolumeSource{
						ContainerDisk: &kubevirtapi.ContainerDiskSource{
							Image:           jctx.Image,
							ImagePullPolicy: k8sapi.PullPolicy(jctx.ImagePullPolicy),
							ImagePullSecret: jctx.ImagePullSecret,
						},
					},
				},
			},
		},
	}

	return client.VirtualMachineInstance(jctx.Namespace).Create(ctx, &instanceTemplate)
}

func Selector(jctx *JobContext) *metav1.ListOptions {
	return &metav1.ListOptions{
		LabelSelector: fmt.Sprintf(labelPrefix+"/id=%s", jctx.ID),
	}
}

func FindJobVM(ctx context.Context, client kubevirt.KubevirtClient, jctx *JobContext) (*kubevirtapi.VirtualMachineInstance, error) {
	list, err := client.VirtualMachineInstance(jctx.Namespace).List(ctx, Selector(jctx))
	if err != nil {
		return nil, err
	}

	if len(list.Items) == 0 {
		return nil, fmt.Errorf("Virtual Machine instance disappeared while the job was running!")
	}
	if len(list.Items) > 1 {
		return nil, fmt.Errorf("Virtual Machine instance has ambiguous ID! %d instances found with ID %v", len(list.Items), jctx.ID)
	}
	return &list.Items[0], nil
}
