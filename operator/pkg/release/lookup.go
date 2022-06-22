package release

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"

	operatorv1alpha1 "github.com/kyma-project/kyma-operator/operator/api/v1alpha1"
	"github.com/kyma-project/kyma-operator/operator/pkg/index"
	"github.com/kyma-project/kyma-operator/operator/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type TemplateLookupResult struct {
	Template   *operatorv1alpha1.ModuleTemplate
	forChannel *operatorv1alpha1.Channel
}

type TemplateLookupResultsByName map[string]*TemplateLookupResult

func GetTemplates(ctx context.Context, c client.Reader, k *operatorv1alpha1.Kyma) (TemplateLookupResultsByName, error) {
	templates := make(TemplateLookupResultsByName)
	for _, component := range k.Spec.Components {
		template, err := LookupTemplate(c, component, k.Spec.Channel).WithContext(ctx)
		if err != nil {
			return nil, err
		}
		templates[component.Name] = template
	}
	return templates, nil
}

func AreTemplatesOutdated(logger *logr.Logger, k *operatorv1alpha1.Kyma, templates TemplateLookupResultsByName) bool {
	// this is a shortcut as we already know templates are outdated when the generation changes
	if k.GetGeneration() != k.Status.ObservedGeneration {
		logger.Info("new kyma spec, setting template status outdated")
		return true
	}
	// in the case that the kyma spec did not change, we only have to verify that all desired templates are still referenced in the latest spec generation
	for componentName, lookupResult := range templates {
		for _, condition := range k.Status.Conditions {
			if condition.Reason == componentName && lookupResult != nil {
				if lookupResult.Template.GetGeneration() != condition.TemplateInfo.Generation || lookupResult.Template.Spec.Channel != condition.TemplateInfo.Channel {
					logger.Info("detected outdated template",
						"condition", condition.Reason,
						"template", lookupResult.Template.Name,
						"templateGeneration", lookupResult.Template.GetGeneration(),
						"previousGeneration", condition.TemplateInfo.Generation,
						"templateChannel", lookupResult.Template.Spec.Channel,
						"previousChannel", condition.TemplateInfo.Channel,
					)
					return true
				}
			}
		}
	}
	return false
}

type Lookup interface {
	WithContext(ctx context.Context) (*TemplateLookupResult, error)
}

func LookupTemplate(client client.Reader, component operatorv1alpha1.ComponentType, defaultChannel operatorv1alpha1.Channel) Lookup {
	return &channelTemplateLookup{
		reader:         client,
		component:      component,
		defaultChannel: defaultChannel,
	}
}

type channelTemplateLookup struct {
	reader         client.Reader
	component      operatorv1alpha1.ComponentType
	defaultChannel operatorv1alpha1.Channel
}

func (c *channelTemplateLookup) WithContext(ctx context.Context) (*TemplateLookupResult, error) {
	templateList := &operatorv1alpha1.ModuleTemplateList{}

	var desiredChannel operatorv1alpha1.Channel

	if c.component.Channel != "" {
		// if component defaultChannel is set it takes precedence
		desiredChannel = c.component.Channel
	} else if c.defaultChannel != "" {
		// else if the global defaultChannel is set it takes precedence
		desiredChannel = c.defaultChannel
	} else {
		// else use the default defaultChannel
		desiredChannel = operatorv1alpha1.DefaultChannel
	}

	if err := c.reader.List(ctx, templateList,
		client.MatchingLabels{
			labels.ControllerName: c.component.Name,
		},
		index.TemplateChannelField.WithValue(string(desiredChannel)),
	); err != nil {
		return nil, err
	}

	if len(templateList.Items) > 1 {
		return nil, NewMoreThanOneTemplateCandidateErr(c.component, templateList.Items)
	}

	// if the desiredChannel cannot be found, use the next best available
	if len(templateList.Items) == 0 {
		if err := c.reader.List(ctx, templateList,
			client.MatchingLabels{
				labels.ControllerName: c.component.Name,
			},
		); err != nil {
			return nil, err
		}

		if len(templateList.Items) > 1 {
			return nil, NewMoreThanOneTemplateCandidateErr(c.component, templateList.Items)
		}

		if len(templateList.Items) == 0 {
			return nil, fmt.Errorf("no config map template found for component: %s", c.component.Name)
		}

	}

	actualChannel := templateList.Items[0].Spec.Channel

	// if the found configMap has no defaultChannel assigned to it set a sensible log output
	if actualChannel == "" {
		return nil, fmt.Errorf("no defaultChannel found on template for component: %s, specifying no defaultChannel is not allowed", c.component.Name)
	}

	if actualChannel != c.defaultChannel {
		log.FromContext(ctx).V(3).Info(fmt.Sprintf("using %s (instead of %s) for component %s", actualChannel, c.defaultChannel, c.component.Name))
	} else {
		log.FromContext(ctx).V(3).Info(fmt.Sprintf("using %s for component %s", actualChannel, c.component.Name))
	}

	return &TemplateLookupResult{
		Template:   &templateList.Items[0],
		forChannel: &actualChannel,
	}, nil
}

func NewMoreThanOneTemplateCandidateErr(component operatorv1alpha1.ComponentType, candidateTemplates []operatorv1alpha1.ModuleTemplate) error {
	candidates := make([]string, len(candidateTemplates))
	for i, candidate := range candidateTemplates {
		candidates[i] = candidate.GetName()
	}
	return fmt.Errorf("more than one config map template found for component: %s, candidates: %v", component.Name, candidates)
}