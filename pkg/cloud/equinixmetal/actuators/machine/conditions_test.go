package machine

import (
	"testing"

	"github.com/openshift/cluster-api-provider-equinix-metal/pkg/apis/equinixmetal/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

func TestShouldUpdateCondition(t *testing.T) {
	testCases := []struct {
		oldCondition v1beta1.EquinixMetalMachineProviderCondition
		newCondition v1beta1.EquinixMetalMachineProviderCondition
		expected     bool
	}{
		{
			oldCondition: v1beta1.EquinixMetalMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			newCondition: v1beta1.EquinixMetalMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			expected: false,
		},
		{
			oldCondition: v1beta1.EquinixMetalMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			newCondition: v1beta1.EquinixMetalMachineProviderCondition{
				Reason:  "different reason",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			expected: true,
		},
		{
			oldCondition: v1beta1.EquinixMetalMachineProviderCondition{
				Reason:  "foo",
				Message: "different message",
				Status:  corev1.ConditionTrue,
			},
			newCondition: v1beta1.EquinixMetalMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			expected: true,
		},
		{
			oldCondition: v1beta1.EquinixMetalMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionTrue,
			},
			newCondition: v1beta1.EquinixMetalMachineProviderCondition{
				Reason:  "foo",
				Message: "bar",
				Status:  corev1.ConditionFalse,
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		if got := shouldUpdateCondition(tc.oldCondition, tc.newCondition); got != tc.expected {
			t.Errorf("Expected %t, got %t", got, tc.expected)
		}
	}
}
