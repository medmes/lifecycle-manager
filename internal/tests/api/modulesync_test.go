package api_test

import (
	"testing"

	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
)

func TestSyncEnabled(t *testing.T) {
	t.Parallel()

	t.Run("sync enabled by default for nil labels map", func(t *testing.T) {
		t.Parallel()
		module := v1beta2.ModuleTemplate{}
		module.Labels = nil
		actual := module.SyncEnabled(false, false)
		if !actual {
			t.Error("Incorrect SyncEnabled value")
		}
	})

	tests := []struct {
		name               string
		betaLabelValue     string
		internalLabelValue string
		betaEnabled        bool
		internalEnabled    bool
		mandatoryModule    bool
		expected           bool
	}{
		{
			expected:           true,
			name:               "sync is enabled for missing or empty labels",
			betaLabelValue:     "",
			internalLabelValue: "",
			betaEnabled:        false,
			internalEnabled:    false,
			mandatoryModule:    false,
		},
		{
			expected:           true,
			name:               "sync is enabled for explicit label value",
			betaLabelValue:     "",
			internalLabelValue: "",
			betaEnabled:        false,
			internalEnabled:    false,
			mandatoryModule:    false,
		},
		{
			expected:           false,
			name:               "sync is disabled for explicit label value",
			betaLabelValue:     "",
			internalLabelValue: "",
			betaEnabled:        false,
			internalEnabled:    false,
			mandatoryModule:    false,
		},
		{
			expected:           false,
			name:               "beta sync is disabled by default",
			betaLabelValue:     "true",
			internalLabelValue: "",
			betaEnabled:        false,
			internalEnabled:    false,
			mandatoryModule:    false,
		},
		{
			expected:           true,
			name:               "beta sync is enabled if explicitly enabled",
			betaLabelValue:     "true",
			internalLabelValue: "",
			betaEnabled:        true,
			internalEnabled:    false,
			mandatoryModule:    false,
		},
		{
			expected:           false,
			name:               "internal sync is disabled by default",
			betaLabelValue:     "",
			internalLabelValue: "true",
			betaEnabled:        false,
			internalEnabled:    false,
			mandatoryModule:    false,
		},
		{
			expected:           true,
			name:               "internal sync is enabled if explicitly enabled",
			betaLabelValue:     "",
			internalLabelValue: "true",
			betaEnabled:        false,
			internalEnabled:    true,
			mandatoryModule:    false,
		},
		{
			expected:           false,
			name:               "beta+internal sync is disabled by default",
			betaLabelValue:     "true",
			internalLabelValue: "true",
			betaEnabled:        false,
			internalEnabled:    false,
			mandatoryModule:    false,
		},
		{
			expected:           false,
			name:               "beta+internal sync is disabled in only internal is enabled",
			betaLabelValue:     "true",
			internalLabelValue: "true",
			betaEnabled:        false,
			internalEnabled:    true,
			mandatoryModule:    false,
		},
		{
			expected:           true,
			name:               "beta+internal sync is enabled if both beta and internal are explicitly enabled",
			betaLabelValue:     "true",
			internalLabelValue: "true",
			betaEnabled:        true,
			internalEnabled:    true,
			mandatoryModule:    false,
		},
		{
			expected:           false,
			name:               "sync is disabled for mandatory module",
			betaLabelValue:     "",
			internalLabelValue: "",
			betaEnabled:        false,
			internalEnabled:    false,
			mandatoryModule:    true,
		},
	}

	for _, testCase := range tests {
		tcase := testCase
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			module := v1beta2.ModuleTemplate{}
			module.Labels = map[string]string{}
			module.Labels[shared.BetaLabel] = tcase.betaLabelValue
			module.Labels[shared.InternalLabel] = tcase.internalLabelValue
			module.Spec.Mandatory = tcase.mandatoryModule

			actual := module.SyncEnabled(tcase.betaEnabled, tcase.internalEnabled)
			if actual != tcase.expected {
				t.Error("Incorrect SyncEnabled value")
			}
		})
	}
}
