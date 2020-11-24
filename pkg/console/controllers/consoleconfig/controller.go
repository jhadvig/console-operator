package consoleconfig

import (
	// standard lib
	"context"
	"fmt"

	// kube
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	corev1 "k8s.io/client-go/informers/core/v1"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	// openshift
	operatorv1 "github.com/openshift/api/operator/v1"
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
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"

	// informers

	// clients
	configclientv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	consoleclientv1 "github.com/openshift/client-go/console/clientset/versioned/typed/console/v1"
	operatorclientv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"

	// operator
	"github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/console-operator/pkg/console/status"
	routesub "github.com/openshift/console-operator/pkg/console/subresource/route"
)

const (
	controllerWorkQueueKey = "console-config-sync-work-queue-key"
	controllerName         = "ConsoleConsoleConfigSyncController"
)

type ConsoleConfigSyncController struct {
	// configs
	operatorClient             v1helpers.OperatorClient
	operatorConfigClient       operatorclientv1.ConsoleInterface
	consoleConfigClient        configclientv1.ConsoleInterface
	infrastructureConfigClient configclientv1.InfrastructureInterface
	proxyConfigClient          configclientv1.ProxyInterface
	oauthConfigClient          configclientv1.OAuthInterface
	// core kube
	secretsClient    coreclientv1.SecretsGetter
	configMapClient  coreclientv1.ConfigMapsGetter
	serviceClient    coreclientv1.ServicesGetter
	deploymentClient appsv1.DeploymentsGetter
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
	coreV1 corev1.Interface,
	// deployments
	deploymentClient appsv1.DeploymentsGetter,
	deployments appsinformersv1.DeploymentInformer,
	// routes
	routev1Client routeclientv1.RoutesGetter,
	routes routesinformersv1.RouteInformer,
	// oauth
	oauthv1Client oauthclientv1.OAuthClientsGetter,
	oauthClients oauthinformersv1.OAuthClientInformer,
	// plugins
	consolePluginInformer consoleinformersv1.ConsolePluginInformer,
	consolePluginClient consoleclientv1.ConsolePluginInterface,
	// openshift managed
	managedCoreV1 corev1.Interface,
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
		proxyConfigClient:          configClient.Proxies(),
		oauthConfigClient:          configClient.OAuths(),
		// console resources
		// core kube
		secretsClient:    corev1Client,
		configMapClient:  corev1Client,
		serviceClient:    corev1Client,
		deploymentClient: deploymentClient,
		// openshift
		routeClient: routev1Client,
		oauthClient: oauthv1Client,
		// recorder
		recorder: recorder,
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

	route, routeErr := c.routeClient.Routes(api.TargetNamespace).Get(c.ctx, routeName, metav1.GetOptions{})
	statusHandler.AddConditions(status.HandleProgressingOrDegraded("SyncLoopRefresh", "InProgress", routeErr))
	if routeErr != nil {
		return statusHandler.FlushAndReturn(routeErr)
	}

	// ensure we have top level console config
	consoleConfig, err := c.consoleConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("console config error: %v", err)
		return err
	}

	// we need infrastructure config for apiServerURL
	infrastructureConfig, err := c.infrastructureConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("infrastructure config error: %v", err)
		return err
	}

	proxyConfig, err := c.proxyConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("proxy config error: %v", err)
		return err
	}

	oauthConfig, err := c.oauthConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("oauth config error: %v", err)
		return err
	}

	c.SyncConfigMap(consoleConfig, infrastructureConfig, proxyConfig, oauthConfig, route)

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
	}

	return statusHandler.FlushAndReturn(nil)
}

func (c *ConsoleConfigSyncController) SyncConfigMap(
	operatorConfig *operatorv1.Console,
	consoleConfig *configv1.Console,
	infrastructureConfig *configv1.Infrastructure,
	oauthConfig *configv1.OAuth,
	activeConsoleRoute *routev1.Route) (consoleConfigMap *corev1.ConfigMap, changed bool, reason string, err error) {

	managedConfig, mcErr := co.configMapClient.ConfigMaps(api.OpenShiftConfigManagedNamespace).Get(co.ctx, api.OpenShiftConsoleConfigMapName, metav1.GetOptions{})
	if mcErr != nil && !apierrors.IsNotFound(mcErr) {
		return nil, false, "FailedGetManagedConfig", mcErr
	}

	useDefaultCAFile := false
	// We are syncing the `default-ingress-cert` configmap from `openshift-config-managed` to `openshift-console`.
	// `default-ingress-cert` is only published in `openshift-config-managed` in OpenShift 4.4.0 and newer.
	// If the `default-ingress-cert` configmap in `openshift-console` exists, we should mount that to the console container,
	// otherwise default to `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`
	_, rcaErr := co.configMapClient.ConfigMaps(api.OpenShiftConsoleNamespace).Get(co.ctx, api.DefaultIngressCertConfigMapName, metav1.GetOptions{})
	if rcaErr != nil && apierrors.IsNotFound(rcaErr) {
		useDefaultCAFile = true
	}

	inactivityTimeoutSeconds := 0
	oauthClient, oacErr := co.oauthClient.OAuthClients().Get(co.ctx, oauthsub.Stub().Name, metav1.GetOptions{})
	if oacErr != nil {
		return nil, false, "FailedGetOAuthClient", oacErr
	}
	if oauthClient.AccessTokenInactivityTimeoutSeconds != nil {
		inactivityTimeoutSeconds = int(*oauthClient.AccessTokenInactivityTimeoutSeconds)
	} else {
		if oauthConfig.Spec.TokenConfig.AccessTokenInactivityTimeout != nil {
			inactivityTimeoutSeconds = int(oauthConfig.Spec.TokenConfig.AccessTokenInactivityTimeout.Seconds())
		}
	}

	monitoringSharedConfig, mscErr := co.configMapClient.ConfigMaps(api.OpenShiftConfigManagedNamespace).Get(co.ctx, api.OpenShiftMonitoringConfigMapName, metav1.GetOptions{})
	if mscErr != nil && !apierrors.IsNotFound(mscErr) {
		return nil, false, "FailedGetMonitoringSharedConfig", mscErr
	}

	defaultConfigmap, _, err := configmapsub.DefaultConfigMap(operatorConfig, consoleConfig, managedConfig, monitoringSharedConfig, infrastructureConfig, activeConsoleRoute, useDefaultCAFile, inactivityTimeoutSeconds)
	if err != nil {
		return nil, false, "FailedConsoleConfigBuilder", err
	}
	cm, cmChanged, cmErr := resourceapply.ApplyConfigMap(co.configMapClient, co.recorder, defaultConfigmap)
	if cmErr != nil {
		return nil, false, "FailedApply", cmErr
	}
	if cmChanged {
		klog.V(4).Infoln("new console config yaml:")
		klog.V(4).Infof("%s", cm.Data)
	}
	return cm, cmChanged, "ConsoleConfigBuilder", cmErr
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
