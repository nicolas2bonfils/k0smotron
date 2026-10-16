/*
Copyright 2026.

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

package v1beta1

import (
	"github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

var _ conversion.Convertible = &RemoteCluster{}

// ConvertTo converts this version (v1beta1) to the hub version (v1beta2).
func (rc *RemoteCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.RemoteCluster)
	dst.ObjectMeta = *rc.ObjectMeta.DeepCopy()
	dst.Spec.ControlPlaneEndpoint = rc.Spec.ControlPlaneEndpoint
	dst.Status = v1beta2.RemoteClusterStatus{
		Initialization: v1beta2.RemoteClusterInitializationStatus{
			Provisioned: &rc.Status.Ready,
		},
	}
	return nil
}

// ConvertFrom converts from the hub version (v1beta2) to this version.
func (rc *RemoteCluster) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.RemoteCluster)
	rc.ObjectMeta = src.ObjectMeta
	rc.Spec.ControlPlaneEndpoint = src.Spec.ControlPlaneEndpoint
	rc.Status = RemoteClusterStatus{
		Ready: src.Status.Initialization.Provisioned != nil && *src.Status.Initialization.Provisioned,
	}
	return nil
}
