package consoleconfigmap

import (
	// standard lib
	"context"
	"fmt"
	"time"

	// kube
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformersv1 "k8s.io/client-go/informers/core/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	// openshift
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	configinformer "github.com/openshift/client-go/config/informers/externalversions"
	consoleinformersv1 "github.com/openshift/client-go/console/informers/externalversions/console/v1"
	oauthclientv1 "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	oauthinformersv1 "github.com/openshift/client-go/oauth/informers/externalversions/oauth/v1"
	operatorinformerv1 "github.com/openshift/client-go/operator/informers/externalversions/operator/v1"
	routeclientv1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	routesinformersv1 "github.com/openshift/client-go/route/informers/externalversions/route/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	// informers

	// clients
	configclientv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	consoleclientv1 "github.com/openshift/client-go/console/clientset/versioned/typed/console/v1"
	operatorclientv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"

	// operator
	"github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/console-operator/pkg/console/status"
	configmapsub "github.com/openshift/console-operator/pkg/console/subresource/configmap"
	oauthsub "github.com/openshift/console-operator/pkg/console/subresource/oauthclient"
	routesub "github.com/openshift/console-operator/pkg/console/subresource/route"
)

const (
	controllerWorkQueueKey = "console-config-sync-work-queue-key"
	controllerName         = "ConsoleConsoleConfigSyncController"
)

type configSet struct {
	Console        *configv1.Console
	Operator       *operatorv1.Console
	Infrastructure *configv1.Infrastructure
	Proxy          *configv1.Proxy
	OAuth          *configv1.OAuth
}

type ConsoleConfigSyncController struct {
	// configs
	operatorClient             v1helpers.OperatorClient
	operatorConfigClient       operatorclientv1.ConsoleInterface
	consoleConfigClient        configclientv1.ConsoleInterface
	infrastructureConfigClient configclientv1.InfrastructureInterface
	oauthConfigClient          configclientv1.OAuthInterface
	// core kube
	configMapClient coreclientv1.ConfigMapsGetter
	// openshift
	routeClient         routeclientv1.RoutesGetter
	oauthClient         oauthclientv1.OAuthClientsGetter
	consolePluginClient consoleclientv1.ConsolePluginInterface

	// events
	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
	recorder     events.Recorder
	// context
	ctx context.Context
}

func NewConsoleConfigSyncController(
	// top level config
	configClient configclientv1.ConfigV1Interface,
	configInformer configinformer.SharedInformerFactory,
	// operator
	operatorClient v1helpers.OperatorClient,
	operatorConfigClient operatorclientv1.OperatorV1Interface,
	operatorConfigInformer operatorinformerv1.ConsoleInformer,
	// core resources
	corev1Client coreclientv1.CoreV1Interface,
	coreV1 coreinformersv1.Interface,
	// routes
	routev1Client routeclientv1.RoutesGetter,
	routesInformer routesinformersv1.RouteInformer,
	// oauth
	oauthv1Client oauthclientv1.OAuthClientsGetter,
	oauthInformer oauthinformersv1.OAuthClientInformer,
	// plugins
	consolePluginClient consoleclientv1.ConsolePluginInterface,
	consolePluginInformer consoleinformersv1.ConsolePluginInformer,
	// openshift managed
	managedCoreV1 coreinformersv1.Interface,
	// event handling
	recorder events.Recorder,
	// context
	ctx context.Context,
) *ConsoleConfigSyncController {

	ctrl := &ConsoleConfigSyncController{
		// configs
		operatorClient:             operatorClient,
		operatorConfigClient:       operatorConfigClient.Consoles(),
		consoleConfigClient:        configClient.Consoles(),
		infrastructureConfigClient: configClient.Infrastructures(),
		oauthConfigClient:          configClient.OAuths(),
		// console resources
		// core kube
		configMapClient: corev1Client,
		// openshift
		routeClient: routev1Client,
		oauthClient: oauthv1Client,
		// recorder
		recorder:     recorder,
		cachesToSync: nil,
		queue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), controllerName),
		ctx:          ctx,
	}

	oauthInformer.Informer().AddEventHandler(ctrl.newEventHandler())
	operatorClient.Informer().AddEventHandler(ctrl.newEventHandler())
	operatorConfigInformer.Informer().AddEventHandler(ctrl.newEventHandler())
	consolePluginInformer.Informer().AddEventHandler(ctrl.newEventHandler())
	routesInformer.Informer().AddEventHandler(ctrl.newEventHandler())

	ctrl.cachesToSync = append(ctrl.cachesToSync,
		oauthInformer.Informer().HasSynced,
		operatorClient.Informer().HasSynced,
		operatorConfigInformer.Informer().HasSynced,
		consolePluginInformer.Informer().HasSynced,
		routesInformer.Informer().HasSynced,
	)

	return ctrl
}

func (c *ConsoleConfigSyncController) sync() error {
	operatorConfig, err := c.operatorConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	updatedOperatorConfig := operatorConfig.DeepCopy()

	switch updatedOperatorConfig.Spec.ManagementState {
	case operatorv1.Managed:
		klog.V(4).Infoln("console is in a managed state: syncing ConsoleConfig")
	case operatorv1.Unmanaged:
		klog.V(4).Infoln("console is in an unmanaged state: skipping ConsoleConfig")
		return nil
	case operatorv1.Removed:
		klog.V(4).Infoln("console is in a removed state: skipping ConsoleConfig")
		return nil
	default:
		return fmt.Errorf("console is in an unknown state: %v", updatedOperatorConfig.Spec.ManagementState)
	}

	statusHandler := status.NewStatusHandler(c.operatorClient)

	routeName := api.OpenShiftConsoleName
	if routesub.IsCustomRouteSet(updatedOperatorConfig) {
		routeName = api.OpenshiftConsoleCustomRouteName
	}

	configSet, configSetReason, configSetErr := c.GetConfigSet()
	if configSetErr != nil {
		statusHandler.AddConditions(status.HandleProgressingOrDegraded("SyncLoopRefresh", configSetReason, configSetErr))
		return statusHandler.FlushAndReturn(configSetErr)
	}

	route, routeErr := c.routeClient.Routes(api.TargetNamespace).Get(c.ctx, routeName, metav1.GetOptions{})
	statusHandler.AddConditions(status.HandleProgressingOrDegraded("SyncLoopRefresh", "InProgress", routeErr))
	if routeErr != nil {
		statusHandler.AddConditions(status.HandleProgressingOrDegraded("SyncLoopRefresh", "Failed", routeErr))
		return statusHandler.FlushAndReturn(routeErr)
	}

	_, consoleConfigMapErrReason, consoleConfigMapErr := c.SyncConfigMap(operatorConfig, configSet.Console, configSet.Infrastructure, configSet.OAuth, route)
	if consoleConfigMapErr != nil {
		statusHandler.AddConditions(status.HandleProgressingOrDegraded("SyncLoopRefresh", consoleConfigMapErrReason, routeErr))
		return statusHandler.FlushAndReturn(consoleConfigMapErr)
	}

	return statusHandler.FlushAndReturn(nil)
}

func (c *ConsoleConfigSyncController) GetConfigSet() (*configSet, string, error) {
	// ensure we have top level console config
	consoleConfig, err := c.consoleConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		return nil, "FailedGetConsoleConfig", err
	}

	// we need infrastructure config for apiServerURL
	infrastructureConfig, err := c.infrastructureConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		return nil, "FailedGetInfrastructureConfig", err
	}

	oauthConfig, oauthConfigErr := c.oauthConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
	if oauthConfigErr != nil {
		return nil, "FailedGetOAuth", err
	}

	configs := &configSet{
		Console:        consoleConfig,
		Infrastructure: infrastructureConfig,
		OAuth:          oauthConfig,
	}
	return configs, "", nil
}

func (c *ConsoleConfigSyncController) SyncConfigMap(
	operatorConfig *operatorv1.Console,
	consoleConfig *configv1.Console,
	infrastructureConfig *configv1.Infrastructure,
	oauthConfig *configv1.OAuth,
	activeConsoleRoute *routev1.Route) (*corev1.ConfigMap, string, error) {

	managedConfig, mcErr := c.configMapClient.ConfigMaps(api.OpenShiftConfigManagedNamespace).Get(c.ctx, api.OpenShiftConsoleConfigMapName, metav1.GetOptions{})
	if mcErr != nil && !apierrors.IsNotFound(mcErr) {
		return nil, "FailedGetManagedConfig", mcErr
	}

	useDefaultCAFile := false
	// We are syncing the `default-ingress-cert` configmap from `openshift-config-managed` to `openshift-console`.
	// `default-ingress-cert` is only published in `openshift-config-managed` in OpenShift 4.4.0 and newer.
	// If the `default-ingress-cert` configmap in `openshift-console` exists, we should mount that to the console container,
	// otherwise default to `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`
	_, rcaErr := c.configMapClient.ConfigMaps(api.OpenShiftConsoleNamespace).Get(c.ctx, api.DefaultIngressCertConfigMapName, metav1.GetOptions{})
	if rcaErr != nil && apierrors.IsNotFound(rcaErr) {
		useDefaultCAFile = true
	}

	inactivityTimeoutSeconds := 0
	oauthClient, oacErr := c.oauthClient.OAuthClients().Get(c.ctx, oauthsub.Stub().Name, metav1.GetOptions{})
	if oacErr != nil {
		return nil, "FailedGetOAuthClient", oacErr
	}
	if oauthClient.AccessTokenInactivityTimeoutSeconds != nil {
		inactivityTimeoutSeconds = int(*oauthClient.AccessTokenInactivityTimeoutSeconds)
	} else {
		if oauthConfig.Spec.TokenConfig.AccessTokenInactivityTimeout != nil {
			inactivityTimeoutSeconds = int(oauthConfig.Spec.TokenConfig.AccessTokenInactivityTimeout.Seconds())
		}
	}

	pluginsList, pluginsListErr := c.consolePluginClient.List(c.ctx, metav1.ListOptions{})
	klog.Infof("available---> %v", pluginsList.Items)
	if pluginsListErr != nil {
		return nil, "FailedGetConsolePlugins", pluginsListErr
	}
	enabledPlugins := getEnabledPlugins(operatorConfig, pluginsList.Items)
	klog.Infof("enabled---> %v", enabledPlugins)

	monitoringSharedConfig, mscErr := c.configMapClient.ConfigMaps(api.OpenShiftConfigManagedNamespace).Get(c.ctx, api.OpenShiftMonitoringConfigMapName, metav1.GetOptions{})
	if mscErr != nil && !apierrors.IsNotFound(mscErr) {
		return nil, "FailedGetMonitoringSharedConfig", mscErr
	}

	defaultConfigmap, _, err := configmapsub.DefaultConfigMap(operatorConfig, consoleConfig, managedConfig, monitoringSharedConfig, infrastructureConfig, activeConsoleRoute, useDefaultCAFile, inactivityTimeoutSeconds, enabledPlugins)
	if err != nil {
		return nil, "FailedConsoleConfigBuilder", err
	}
	cm, cmChanged, cmErr := resourceapply.ApplyConfigMap(c.configMapClient, c.recorder, defaultConfigmap)
	if cmErr != nil {
		return nil, "FailedApply", cmErr
	}
	if cmChanged {
		klog.V(4).Infoln("new console config yaml:")
		klog.V(4).Infof("%s", cm.Data)
	}
	return cm, "ConsoleConfigBuilder", cmErr
}

func (c *ConsoleConfigSyncController) Run(workers int, stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()
	klog.Infof("starting %v", controllerName)
	defer klog.Infof("shutting down %v", controllerName)
	if !cache.WaitForCacheSync(stopCh, c.cachesToSync...) {
		klog.Infoln("caches did not sync")
		runtime.HandleError(fmt.Errorf("caches did not sync"))
		return
	}
	// only start one worker
	go wait.Until(c.runWorker, time.Second, stopCh)
	<-stopCh
}

func (c *ConsoleConfigSyncController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *ConsoleConfigSyncController) processNextWorkItem() bool {
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

func (c *ConsoleConfigSyncController) newEventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { c.queue.Add(controllerWorkQueueKey) },
		UpdateFunc: func(old, new interface{}) { c.queue.Add(controllerWorkQueueKey) },
		DeleteFunc: func(obj interface{}) { c.queue.Add(controllerWorkQueueKey) },
	}
}

// return plugins that are:
// - enabled in the console-operator config
// - created and available as a CR on the cluster
func getEnabledPlugins(operatorConfig *operatorv1.Console, pluginsList []consolev1.ConsolePlugin) []string {
	availablePluginsNames := []string{}
	enabledPluginsNames := operatorConfig.Spec.Plugins
	for _, plugin := range pluginsList {
		availablePluginsNames = append(availablePluginsNames, plugin.ObjectMeta.Name)
	}

	pluginsListSame := equality.Semantic.DeepEqual(enabledPluginsNames, availablePluginsNames)
	if pluginsListSame == true {
		return enabledPluginsNames
	}
	newEnabledPlugins := removeUnavailablePlugins(enabledPluginsNames, availablePluginsNames)
	return newEnabledPlugins
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
