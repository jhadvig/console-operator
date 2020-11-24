package plugins

import (
	// standard lib
	"context"
	"fmt"

	// kube
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	// openshift

	operatorsv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	// informers
	consoleinformersv1 "github.com/openshift/client-go/console/informers/externalversions/console/v1"
	operatorinformersv1 "github.com/openshift/client-go/operator/informers/externalversions/operator/v1"

	// clients
	consoleclientv1 "github.com/openshift/client-go/console/clientset/versioned/typed/console/v1"
	operatorclientv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"

	// operator
	"github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/console-operator/pkg/console/status"
)

const (
	controllerWorkQueueKey = "plugins-sync-work-queue-key"
	controllerName         = "ConsolePluginsSyncController"
)

type PluginsSyncController struct {
	operatorClient       v1helpers.OperatorClient
	operatorConfigClient operatorclientv1.ConsoleInterface
	// live clients, we dont need listers w/caches
	consolePluginClient consoleclientv1.ConsolePluginInterface
	// events
	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
	recorder     events.Recorder
	// context
	ctx context.Context
}

func NewPluginsSyncController(
	// clients
	operatorClient v1helpers.OperatorClient,
	operatorConfigClient operatorclientv1.OperatorV1Interface,
	consolePluginClient consoleclientv1.ConsolePluginInterface,
	// informers
	operatorConfigInformer operatorinformersv1.ConsoleInformer,
	consolePluginInformer consoleinformersv1.ConsolePluginInformer,
	// recorder
	recorder events.Recorder,
	// context
	ctx context.Context,
) *PluginsSyncController {

	ctrl := &PluginsSyncController{
		// clients
		operatorClient:       operatorClient,
		consolePluginClient:  consolePluginClient,
		operatorConfigClient: operatorConfigClient.Consoles(),
		// events
		recorder: recorder,
		queue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ConsolePluginsSyncer"),
		ctx:      ctx,
	}

	operatorClient.Informer().AddEventHandler(ctrl.newEventHandler())
	operatorConfigInformer.Informer().AddEventHandler(ctrl.newEventHandler())
	consolePluginInformer.Informer().AddEventHandler(ctrl.newEventHandler())

	ctrl.cachesToSync = append(ctrl.cachesToSync,
		operatorClient.Informer().HasSynced,
		operatorConfigInformer.Informer().HasSynced,
		consolePluginInformer.Informer().HasSynced,
	)

	return ctrl
}

func (c *PluginsSyncController) sync() error {
	operatorConfig, err := c.operatorConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	updatedOperatorConfig := operatorConfig.DeepCopy()

	switch updatedOperatorConfig.Spec.ManagementState {
	case operatorsv1.Managed:
		klog.V(4).Infoln("console is in a managed state: syncing ConsolePlugins custom resources")
	case operatorsv1.Unmanaged:
		klog.V(4).Infoln("console is in an unmanaged state: skipping ConsolePlugins custom resources sync")
		return nil
	case operatorsv1.Removed:
		klog.V(4).Infoln("console is in a removed state: skipping ConsolePlugins custom resources sync")
		return nil
	default:
		return fmt.Errorf("console is in an unknown state: %v", updatedOperatorConfig.Spec.ManagementState)
	}

	statusHandler := status.NewStatusHandler(c.operatorClient)

	pluginsList, pluginsListErr := c.consolePluginClient.List(c.ctx, metav1.ListOptions{})
	if pluginsListErr != nil {
		return statusHandler.FlushAndReturn(pluginsListErr)
	}

	availablePluginsNames := make([]string, len(pluginsList.Items))
	enabledPluginsNames := updatedOperatorConfig.Spec.Plugins
	for _, plugin := range pluginsList.Items {
		availablePluginsNames = append(availablePluginsNames, plugin.ObjectMeta.Name)
	}

	pluginsListSame := equality.Semantic.DeepEqual(enabledPluginsNames, availablePluginsNames)
	if pluginsListSame != true {
		newEnabledPlugins := removeUnavailablePlugins(enabledPluginsNames, availablePluginsNames)
		specCopy := updatedOperatorConfig.Spec.DeepCopy()
		specCopy.Plugins = newEnabledPlugins
		c.operatorClient.UpdateOperatorSpec(updatedOperatorConfig.ObjectMeta.ResourceVersion, specCopy)
	}

	return statusHandler.FlushAndReturn(nil)
}

func (c *PluginsSyncController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *PluginsSyncController) processNextWorkItem() bool {
	processKey, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(processKey)
	err := c.sync()
	if err == nil {
		c.queue.Forget(processKey)
		return true
	}
	runtime.HandleError(fmt.Errorf("%v failed with : %v", processKey, err))
	c.queue.AddRateLimited(processKey)
	return true
}

func (c *PluginsSyncController) newEventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(controllerWorkQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(controllerWorkQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(controllerWorkQueueKey) },
	}
}

// intersection of enabled and available plugins
func removeUnavailablePlugins(enabledPlugins, availablePlugins []string) []string {
	m := make(map[string]bool)

	for _, item := range enabledPlugins {
		m[item] = true
	}

	intersection := []string{}
	for _, item := range availablePlugins {
		if _, ok := m[item]; ok {
			intersection = append(intersection, item)
		}
	}
	return intersection
}
