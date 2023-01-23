package controllers_test

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kyma-project/lifecycle-manager/api/v1alpha1"
	. "github.com/kyma-project/lifecycle-manager/pkg/testutils"

	ocm "github.com/gardener/component-spec/bindings-go/apis/v2"
	manifestV1alpha1 "github.com/kyma-project/module-manager/api/v1alpha1"
	"github.com/kyma-project/module-manager/pkg/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	ErrManifestRemoteIsNotMatch              = errors.New("Manifest.Spec.Remote is not match")
	ErrContainsUnexpectedCredSecretSelector  = errors.New("contains unexpected credSecretSelector")
	ErrNotContainsExpectedCredSecretSelector = errors.New("not contains expected credSecretSelector")
)

const (
	//nolint:gosec
	credSecretLabel = "operator.kyma-project.io/oci-registry-cred"
	credSecretValue = "test-operator"
)

func expectManifestSpecRemoteMatched(kymaName string, remoteFlag bool) func() error {
	return func() error {
		createdKyma, err := GetKyma(ctx, controlPlaneClient, kymaName, "")
		if err != nil {
			return err
		}
		for _, module := range createdKyma.Spec.Modules {
			moduleInCluster, err := getModule(kymaName, module.Name)
			if err != nil {
				return err
			}
			manifestSpec := UnmarshalManifestSpec(moduleInCluster)
			if manifestSpec.Remote != remoteFlag {
				return ErrManifestRemoteIsNotMatch
			}
		}
		if err != nil {
			return err
		}
		return nil
	}
}

func expectManifestSpecNotContainsCredSecretSelector(kymaName string) func() error {
	return func() error {
		createdKyma, err := GetKyma(ctx, controlPlaneClient, kymaName, "")
		if err != nil {
			return err
		}
		for _, module := range createdKyma.Spec.Modules {
			moduleInCluster, err := getModule(kymaName, module.Name)
			if err != nil {
				return err
			}
			manifestSpec := UnmarshalManifestSpec(moduleInCluster)
			if manifestSpec.Config.CredSecretSelector != nil {
				return ErrContainsUnexpectedCredSecretSelector
			}
			installImageSpec := extractInstallImageSpec(manifestSpec.Installs)

			if installImageSpec.CredSecretSelector != nil {
				return ErrContainsUnexpectedCredSecretSelector
			}
		}
		return nil
	}
}

func expectManifestSpecContainsCredSecretSelector(kymaName string) func() error {
	return func() error {
		createdKyma, err := GetKyma(ctx, controlPlaneClient, kymaName, "")
		if err != nil {
			return err
		}
		for _, module := range createdKyma.Spec.Modules {
			moduleInCluster, err := getModule(kymaName, module.Name)
			if err != nil {
				return err
			}
			manifestSpec := UnmarshalManifestSpec(moduleInCluster)
			if err := expectCredSecretSelectorCorrect(&manifestSpec.Config); err != nil {
				return err
			}

			installImageSpec := extractInstallImageSpec(manifestSpec.Installs)
			if err := expectCredSecretSelectorCorrect(installImageSpec); err != nil {
				return err
			}
		}
		return nil
	}
}

func extractInstallImageSpec(installInfo []manifestV1alpha1.InstallInfo) *types.ImageSpec {
	Expect(len(installInfo)).To(Equal(1))
	var installImageSpec *types.ImageSpec
	err := yaml.Unmarshal(installInfo[0].Source.Raw, &installImageSpec)
	Expect(err).ToNot(HaveOccurred())
	return installImageSpec
}

func expectCredSecretSelectorCorrect(installImageSpec *types.ImageSpec) error {
	if installImageSpec.CredSecretSelector == nil {
		return ErrNotContainsExpectedCredSecretSelector
	}

	value, found := installImageSpec.CredSecretSelector.MatchLabels[credSecretLabel]
	Expect(found).To(BeTrue())
	Expect(value).To(Equal(credSecretValue))
	return nil
}

func updateModuleTemplateTarget(kymaName string, target v1alpha1.Target) func() error {
	return func() error {
		createdKyma, err := GetKyma(ctx, controlPlaneClient, kymaName, "")
		if err != nil {
			return err
		}
		for _, module := range createdKyma.Spec.Modules {
			moduleTemplate, err := GetModuleTemplate(module.Name)
			if err != nil {
				return err
			}
			moduleTemplate.Spec.Target = target
			err = controlPlaneClient.Update(ctx, moduleTemplate)
			if err != nil {
				return err
			}
		}
		return nil
	}
}

func updateModuleTemplateOCIRegistryCredLabel(kymaName string) func() error {
	return func() error {
		createdKyma, err := GetKyma(ctx, controlPlaneClient, kymaName, "")
		if err != nil {
			return err
		}
		for _, module := range createdKyma.Spec.Modules {
			moduleTemplate, err := GetModuleTemplate(module.Name)
			if err != nil {
				return err
			}
			err = moduleTemplate.Spec.ModifyDescriptor(
				func(descriptor *ocm.ComponentDescriptor) error {
					for i := range descriptor.Resources {
						resource := &descriptor.Resources[i]
						resource.SetLabels([]ocm.Label{{
							Name:  v1alpha1.OCIRegistryCredLabel,
							Value: json.RawMessage(fmt.Sprintf(`{"%s": "%s"}`, credSecretLabel, credSecretValue)),
						}})
					}
					return nil
				})
			if err != nil {
				return err
			}
			err = controlPlaneClient.Update(ctx, moduleTemplate)
			if err != nil {
				return err
			}
		}
		return nil
	}
}

var _ = Describe("Test ModuleTemplate CR", Ordered, func() {
	kyma := NewTestKyma("kyma")

	kyma.Spec.Modules = append(
		kyma.Spec.Modules, v1alpha1.Module{
			ControllerName: "manifest",
			Name:           NewUniqModuleName(),
			Channel:        v1alpha1.DefaultChannel,
		})

	RegisterDefaultLifecycleForKyma(kyma)

	DescribeTable("Test ModuleTemplate.Spec.Target",
		func(givenCondition func() error, expectedBehavior func() error) {
			Eventually(givenCondition, Timeout, Interval).Should(Succeed())
			Eventually(expectedBehavior, Timeout, Interval).Should(Succeed())
		},
		Entry("When ModuleTemplate.Spec.Target not exist deployed, expect Manifest.Spec.remote=false",
			noCondition(),
			expectManifestSpecRemoteMatched(kyma.Name, false)),
		Entry("When update ModuleTemplate.Spec.Target=remote, expect Manifest.Spec.remote=true",
			updateModuleTemplateTarget(kyma.Name, v1alpha1.TargetRemote),
			expectManifestSpecRemoteMatched(kyma.Name, true)),
		Entry("When update ModuleTemplate.Spec.Target=control-plane, expect Manifest.Spec.remote=false",
			updateModuleTemplateTarget(kyma.Name, v1alpha1.TargetControlPlane),
			expectManifestSpecRemoteMatched(kyma.Name, false)),
	)
})

var _ = Describe("Test ModuleTemplate CR", Ordered, func() {
	kyma := NewTestKyma("kyma")

	kyma.Spec.Modules = append(
		kyma.Spec.Modules, v1alpha1.Module{
			ControllerName: "manifest",
			Name:           NewUniqModuleName(),
			Channel:        v1alpha1.DefaultChannel,
		})

	RegisterDefaultLifecycleForKyma(kyma)

	DescribeTable("Test ModuleTemplate.Spec.descriptor",
		func(givenCondition func() error, expectedBehavior func() error) {
			Eventually(givenCondition, Timeout, Interval).Should(Succeed())
			Eventually(expectedBehavior, Timeout, Interval).Should(Succeed())
		},
		Entry("When ModuleTemplate.Spec.descriptor.component.resources not contains RegistryCred label,"+
			"expect Manifest.Spec.installs and Manifest.Spec.Config not contains credSecretSelector",
			noCondition(),
			expectManifestSpecNotContainsCredSecretSelector(kyma.Name)),
		Entry("When ModuleTemplate.Spec.descriptor.component.resources contains RegistryCred label,"+
			"expect Manifest.Spec.installs and Manifest.Spec.Config contains credSecretSelector",
			updateModuleTemplateOCIRegistryCredLabel(kyma.Name),
			expectManifestSpecContainsCredSecretSelector(kyma.Name)),
	)
})