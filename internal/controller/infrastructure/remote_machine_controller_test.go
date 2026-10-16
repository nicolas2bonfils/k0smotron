//go:build !envtest

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

package infrastructure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infrastructure "github.com/k0sproject/k0smotron/v2/api/infrastructure/v1beta2"
)

// TestMergedMap covers a RemoteMachine authored without labels or annotations,
// which is the standalone path where the destination map is nil.
func TestMergedMap(t *testing.T) {
	for _, tc := range []struct {
		name string
		dst  map[string]string
		src  map[string]string
		want map[string]string
	}{
		{
			name: "a nil destination takes the source instead of panicking",
			src:  map[string]string{"a": "1"},
			want: map[string]string{"a": "1"},
		},
		{
			name: "nothing to copy leaves a nil destination alone",
			want: nil,
		},
		{
			name: "nothing to copy leaves an existing destination alone",
			dst:  map[string]string{"a": "1"},
			want: map[string]string{"a": "1"},
		},
		{
			name: "the source wins on a shared key",
			dst:  map[string]string{"a": "1", "b": "2"},
			src:  map[string]string{"a": "9"},
			want: map[string]string{"a": "9", "b": "2"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, mergedMap(tc.dst, tc.src))
		})
	}

	t.Run("the result never shares storage with the source", func(t *testing.T) {
		src := map[string]string{"a": "1"}

		got := mergedMap(nil, src)
		got["b"] = "2"

		require.Equal(t, map[string]string{"a": "1"}, src,
			"handing back the source would let a later write reach the pooled machine")
	})
}

// TestReconcileFromPoolCopiesMetadataOntoBareRemoteMachine covers a hand
// authored RemoteMachine, which carries no labels or annotations to copy into.
func TestReservePooledMachineAndPopulateRemoteMachine(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, infrastructure.AddToScheme(scheme))

	pooled := &infrastructure.PooledRemoteMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pooled",
			Namespace:   "default",
			Labels:      map[string]string{"pool": "a"},
			Annotations: map[string]string{"note": "from the pool"},
		},
		Spec: infrastructure.PooledRemoteMachineSpec{
			Pool: "a",
			Machine: infrastructure.PooledMachineSpec{
				Address: "10.0.0.1",
				Port:    22,
				User:    "root",
				CleanUpCommands: []string{
					"kubeadm reset -f",
					"rm -rf /etc/kubernetes",
				},
			},
		},
	}

	// A RemoteMachine created by hand rather than by CAPI, so both metadata maps
	// are nil.
	rm := &infrastructure.RemoteMachine{
		ObjectMeta: metav1.ObjectMeta{Name: "rm", Namespace: "default"},
		Spec:       infrastructure.RemoteMachineSpec{Pool: "a"},
	}
	require.Nil(t, rm.Labels, "the fixture must have no labels or this proves nothing")
	require.Nil(t, rm.Annotations, "the fixture must have no annotations or this proves nothing")

	c := &RemoteMachineController{
		Client: fake.NewClientBuilder().WithScheme(scheme).
			WithObjects(pooled, rm).WithStatusSubresource(pooled).Build(),
	}

	require.NoError(t, c.reservePooledMachineAndPopulateRemoteMachine(context.Background(), rm))

	require.Equal(t, "10.0.0.1", rm.Spec.Address)
	require.Equal(t, map[string]string{"pool": "a"}, rm.Labels)
	require.Equal(t, map[string]string{"note": "from the pool"}, rm.Annotations)
	require.Equal(t, pooled.Spec.Machine.CleanUpCommands, rm.Spec.CleanUpCommands)
}

// TestReconcileRestoresProvisionedAfterMove covers a RemoteMachine moved by clusterctl move,
// which carries the ProviderID in the spec but not the status that marked it provisioned.
func TestReconcileRestoresProvisionedAfterMove(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, infrastructure.AddToScheme(scheme))
	require.NoError(t, clusterv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default"},
		Status: clusterv1.ClusterStatus{Conditions: []metav1.Condition{{
			Type:   clusterv1.ClusterInfrastructureReadyCondition,
			Status: metav1.ConditionTrue,
		}}},
	}
	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "m",
			Namespace: "default",
			Labels:    map[string]string{clusterv1.ClusterNameLabel: "c"},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "c",
			Bootstrap:   clusterv1.Bootstrap{DataSecretName: new("bootstrap")},
		},
	}
	rm := &infrastructure.RemoteMachine{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "rm",
			Namespace:  "default",
			Finalizers: []string{RemoteMachineFinalizer},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: clusterv1.GroupVersion.String(),
				Kind:       "Machine",
				Name:       "m",
			}},
		},
		Spec: infrastructure.RemoteMachineSpec{
			Address:    "10.0.0.1",
			Port:       22,
			SSHKeyRef:  infrastructure.SecretRef{Name: "ssh-key"},
			ProviderID: "remote-machine://10.0.0.1:22",
		},
		// Set by an earlier reconcile, which returns right after setting it.
		Status: infrastructure.RemoteMachineStatus{Conditions: []metav1.Condition{{
			Type:   clusterv1.PausedCondition,
			Status: metav1.ConditionFalse,
			Reason: clusterv1.NotPausedReason,
		}}},
	}
	secrets := []client.Object{
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "bootstrap", Namespace: "default"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "ssh-key", Namespace: "default"}},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(append(secrets, cluster, machine, rm)...).WithStatusSubresource(rm).Build()
	r := &RemoteMachineController{Client: c, SecretCachingClient: c}

	key := types.NamespacedName{Name: "rm", Namespace: "default"}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	require.NoError(t, err)

	got := &infrastructure.RemoteMachine{}
	require.NoError(t, c.Get(context.Background(), key, got))
	require.NotNil(t, got.Status.Initialization.Provisioned)
	require.True(t, *got.Status.Initialization.Provisioned)
}
