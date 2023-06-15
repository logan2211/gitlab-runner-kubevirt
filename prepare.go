// Copyright 2023, Franklin "Snaipe" Mathieu <me@snai.pe>
//
// Use of this source-code is govered by the MIT license, which
// can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	kubevirtapi "kubevirt.io/api/core/v1"
	kubevirt "kubevirt.io/client-go/kubecli"
)

type PrepareCmd struct {
	DefaultImage                   string        `name:"default-image"`
	DefaultImagePullPolicy         string        `name:"default-image-pull-policy"`
	DefaultImagePullSecret         string        `name:"default-image-pull-secret"`
	DefaultCPURequest              string        `name:"default-cpu-request" default:"1"`
	DefaultCPULimit                string        `name:"default-cpu-limit" default:"1"`
	DefaultMemoryRequest           string        `name:"default-memory-request" default:"1Gi"`
	DefaultMemoryLimit             string        `name:"default-memory-limit" default:"1Gi"`
	DefaultEphemeralStorageRequest string        `name:"default-ephemeral-storage-request"`
	DefaultEphemeralStorageLimit   string        `name:"default-ephemeral-storage-limit"`
	Timeout                        time.Duration `name:"timeout" default:"1h"`
}

func (cmd *PrepareCmd) Run(ctx context.Context, client kubevirt.KubevirtClient, jctx *JobContext) error {
	if jctx.CPURequest == "" {
		jctx.CPURequest = cmd.DefaultCPURequest
	}
	if jctx.CPULimit == "" {
		jctx.CPULimit = cmd.DefaultCPULimit
	}
	if jctx.MemoryRequest == "" {
		jctx.MemoryRequest = cmd.DefaultMemoryRequest
	}
	if jctx.MemoryLimit == "" {
		jctx.MemoryLimit = cmd.DefaultMemoryLimit
	}
	if jctx.EphemeralStorageRequest == "" {
		jctx.EphemeralStorageRequest = cmd.DefaultEphemeralStorageRequest
	}
	if jctx.EphemeralStorageLimit == "" {
		jctx.EphemeralStorageLimit = cmd.DefaultEphemeralStorageLimit
	}
	if jctx.ImagePullPolicy == "" {
		jctx.ImagePullPolicy = cmd.DefaultImagePullPolicy
	}
	if jctx.ImagePullSecret == "" {
		jctx.ImagePullSecret = cmd.DefaultImagePullSecret
	}
	if jctx.Image == "" {
		jctx.Image = cmd.DefaultImage
	}

	fmt.Fprintf(os.Stderr, "Creating Virtual Machine instance\n")

	vm, err := CreateJobVM(ctx, client, jctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Waiting for Virtual Machine instance %s to be ready...\n", vm.ObjectMeta.Name)

	// Wait for new VM to get an IP

	timeout, stop := context.WithTimeout(ctx, cmd.Timeout)
	defer stop()

	watch, err := client.VirtualMachineInstance(jctx.Namespace).Watch(ctx, *Selector(jctx))
	if err != nil {
		return err
	}
	defer watch.Stop()

	ch := watch.ResultChan()
	for {
		select {
		case event := <-ch:
			val, ok := event.Object.(*kubevirtapi.VirtualMachineInstance)
			if !ok {
				panic(fmt.Sprintf("unexpected object type %T", event.Object))
			}
			vm = val
			if len(vm.Status.Interfaces) == 0 || vm.Status.Interfaces[0].IP == "" {
				continue
			}
			var ready bool
			for _, cond := range vm.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					ready = true
					break
				}
			}
			if !ready {
				continue
			}
		case <-timeout.Done():
			return timeout.Err()
		}
		break
	}

	fmt.Fprintf(os.Stderr, "Virtual Machine instance %s is ready and has IP %v\n", vm.ObjectMeta.Name, vm.Status.Interfaces[0].IP)
	return nil
}
