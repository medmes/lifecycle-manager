package templatelookup

import (
	"errors"
	"fmt"

	"github.com/kyma-project/lifecycle-manager/api/shared"
	"github.com/kyma-project/lifecycle-manager/api/v1beta2"
)

var (
	ErrInvalidModuleInSpec   = errors.New("invalid configuration in Kyma spec.modules")
	ErrInvalidModuleInStatus = errors.New("invalid module entry in Kyma status")
)

type ModuleStatusInfo struct {
	v1beta2.Module
	IsEnabled       bool
	ValidationError error
	IsUnmanaged     bool
}

func (a ModuleStatusInfo) IsInstalledByVersion() bool {
	return a.configuredWithVersionInSpec() || a.installedwithVersionInStatus()
}

// configuredWithVersionInSpec returns true if the Module is enabled in Spec using a specific version instead of a channel.
func (a ModuleStatusInfo) configuredWithVersionInSpec() bool {
	return a.IsEnabled && a.Version != "" && a.Channel == ""
}

// installedwithVersionInStatus returns true if the Module installed using a specific version (instead of a channel) is reported in Status.
func (a ModuleStatusInfo) installedwithVersionInStatus() bool {
	return !a.IsEnabled && shared.NoneChannel.Equals(a.Channel) && a.Version != ""
}

// FetchModuleStatusInfo returns a list of ModuleStatusInfo objects based on the Kyma CR Spec and Status.
func FetchModuleStatusInfo(kyma *v1beta2.Kyma) []ModuleStatusInfo {
	moduleMap := make(map[string]bool)
	modules := make([]ModuleStatusInfo, 0)
	for _, module := range kyma.Spec.Modules {
		moduleMap[module.Name] = true
		if shared.NoneChannel.Equals(module.Channel) {
			modules = append(modules, ModuleStatusInfo{
				Module:          module,
				IsEnabled:       true,
				ValidationError: fmt.Errorf("%w for module %s: Channel \"none\" is not allowed", ErrInvalidModuleInSpec, module.Name),
				IsUnmanaged:     !module.Managed,
			})
			continue
		}
		if module.Version != "" && module.Channel != "" {
			modules = append(modules, ModuleStatusInfo{
				Module:          module,
				IsEnabled:       true,
				ValidationError: fmt.Errorf("%w for module %s: Version and channel are mutually exclusive options", ErrInvalidModuleInSpec, module.Name),
				IsUnmanaged:     !module.Managed,
			})
			continue
		}
		modules = append(modules, ModuleStatusInfo{Module: module, IsEnabled: true, IsUnmanaged: !module.Managed})
	}

	for _, moduleInStatus := range kyma.Status.Modules {
		_, exist := moduleMap[moduleInStatus.Name]
		if exist {
			continue
		}

		modules = append(modules, ModuleStatusInfo{
			Module: v1beta2.Module{
				Name:    moduleInStatus.Name,
				Channel: moduleInStatus.Channel,
				Version: moduleInStatus.Version,
			},
			IsEnabled:       false,
			ValidationError: determineModuleValidity(moduleInStatus),
		})
	}
	return modules
}

func determineModuleValidity(moduleStatus v1beta2.ModuleStatus) error {
	if moduleStatus.Template == nil {
		return fmt.Errorf("%w for module %s: ModuleTemplate reference is missing", ErrInvalidModuleInStatus, moduleStatus.Name)
	}
	return nil
}