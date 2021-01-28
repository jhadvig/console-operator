package configmap

import (
	"context"
	"fmt"
	"net/url"

	// k8s
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreinformerv1 "k8s.io/client-go/informers/core/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"

	// openshift
	"github.com/openshift/api/console/v1alpha1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	configclientv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configinformer "github.com/openshift/client-go/config/informers/externalversions"
	consoleinformersv1alpha1 "github.com/openshift/client-go/console/informers/externalversions/console/v1alpha1"
	listerv1alpha1 "github.com/openshift/client-go/console/listers/console/v1alpha1"
	oauthclientv1 "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	oauthinformersv1 "github.com/openshift/client-go/oauth/informers/externalversions/oauth/v1"
	operatorclientv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	operatorinformerv1 "github.com/openshift/client-go/operator/informers/externalversions/operator/v1"
	routeclientv1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	routesinformersv1 "github.com/openshift/client-go/route/informers/externalversions/route/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	// console-operator
	"github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/console-operator/pkg/console/controllers/util"
	"github.com/openshift/console-operator/pkg/console/status"
	configmapsub "github.com/openshift/console-operator/pkg/console/subresource/configmap"
	oauthsub "github.com/openshift/console-operator/pkg/console/subresource/oauthclient"
	routesub "github.com/openshift/console-operator/pkg/console/subresource/route"
)

type ConfigMapSyncController struct {
	// config clients
	operatorClient             v1helpers.OperatorClient
	operatorConfigClient       operatorclientv1.ConsoleInterface
	consoleConfigClient        configclientv1.ConsoleInterface
	infrastructureConfigClient configclientv1.InfrastructureInterface
	proxyConfigClient          configclientv1.ProxyInterface
	oauthConfigClient          configclientv1.OAuthInterface
	// clients
	routeClient routeclientv1.RoutesGetter
	// core kube
	secretsClient   coreclientv1.SecretsGetter
	configMapClient coreclientv1.ConfigMapsGetter
	oauthClient     oauthclientv1.OAuthClientsGetter
	// lister
	consolePluginLister listerv1alpha1.ConsolePluginLister
	// events
	resourceSyncer resourcesynccontroller.ResourceSyncer
}

func NewConfigMapSyncController(
	// top level config
	configClient configclientv1.ConfigV1Interface,
	configInformer configinformer.SharedInformerFactory,
	// operator
	operatorClient v1helpers.OperatorClient,
	operatorConfigClient operatorclientv1.OperatorV1Interface,
	operatorConfigInformer operatorinformerv1.ConsoleInformer,
	// core resources
	corev1Client coreclientv1.CoreV1Interface,
	corev1Informer coreinformerv1.Interface,
	// routes
	routev1Client routeclientv1.RoutesGetter,
	routeInformer routesinformersv1.RouteInformer,
	// oauth
	oauthv1Client oauthclientv1.OAuthClientsGetter,
	oauthClients oauthinformersv1.OAuthClientInformer,
	// plugins
	consolePluginInformer consoleinformersv1alpha1.ConsolePluginInformer,
	// events
	recorder events.Recorder,
	resourceSyncer resourcesynccontroller.ResourceSyncer,
) factory.Controller {
	ctrl := &ConfigMapSyncController{
		// config clients
		operatorClient:             operatorClient,
		operatorConfigClient:       operatorConfigClient.Consoles(),
		consoleConfigClient:        configClient.Consoles(),
		infrastructureConfigClient: configClient.Infrastructures(),
		proxyConfigClient:          configClient.Proxies(),
		oauthConfigClient:          configClient.OAuths(),
		// clients
		routeClient: routev1Client,
		oauthClient: oauthv1Client,
		// core kube
		configMapClient: corev1Client,
		// events
		resourceSyncer: resourceSyncer,
	}

	configNameFilter := util.NamesFilter(api.ConfigResourceName)
	targetNameFilter := util.NamesFilter(api.OpenShiftConsoleName)

	return factory.New().
		WithFilteredEventsInformers( // configs
			configNameFilter,
			operatorConfigInformer.Informer(),
		).WithFilteredEventsInformers( // console resources
		targetNameFilter,
		routeInformer.Informer(),
	).WithSync(ctrl.Sync).
		ToController("ConsoleConfigMapController", recorder.WithComponentSuffix("console-configmap-controller"))
}

func (c *ConfigMapSyncController) Sync(ctx context.Context, controllerContext factory.SyncContext) error {
	configSet, err := c.getConfigs(ctx)
	if err != nil {
		return err
	}
	updatedOperatorConfig := configSet.Operator.DeepCopy()

	switch updatedOperatorConfig.Spec.ManagementState {
	case operatorv1.Managed:
		klog.V(4).Infoln("console is in a managed state: syncing default-ingress-cert configmap")
	case operatorv1.Unmanaged:
		klog.V(4).Infoln("console is in an unmanaged state: skipping default-ingress-cert configmap sync")
		return nil
	case operatorv1.Removed:
		klog.V(4).Infoln("console is in an removed state: removing synced default-ingress-cert configmap")
		return c.removeConfigMaps(ctx)
	default:
		return fmt.Errorf("unknown state: %v", updatedOperatorConfig.Spec.ManagementState)
	}

	statusHandler := status.NewStatusHandler(c.operatorClient)

	// TODO: this controller is no longer responsible for syncing the route.
	//   however, the route is essential for several of the components below.
	//   - is it appropraite for SyncLoopRefresh InProgress to be used here?
	//     the loop should exit early and wait until the RouteSyncController creates the route.
	//     there is nothing new in this flow, other than 2 controllers now look
	//     at the same resource.
	//     - RouteSyncController is responsible for updates
	//     - ConsoleOperatorController (future ConsoleDeploymentController) is responsible for reads only.
	route, routeErr := c.getRoute(ctx, updatedOperatorConfig)
	statusHandler.AddConditions(status.HandleProgressingOrDegraded("SyncLoopRefresh", "InProgress", routeErr))
	if routeErr != nil {
		return statusHandler.FlushAndReturn(routeErr)
	}

	_, _, cmErrReason, cmErr := c.SyncConfigMap(ctx, configSet, route, controllerContext.Recorder())
	statusHandler.AddConditions(status.HandleProgressingOrDegraded("ConfigMapSync", cmErrReason, cmErr))
	if cmErr != nil {
		return statusHandler.FlushAndReturn(cmErr)
	}

	return err
}

func (c *ConfigMapSyncController) SyncConfigMap(
	ctx context.Context,
	configSet *util.ConfigSet,
	activeConsoleRoute *routev1.Route,
	recorder events.Recorder,
) (consoleConfigMap *corev1.ConfigMap, changed bool, reason string, err error) {

	managedConfig, mcErr := c.configMapClient.ConfigMaps(api.OpenShiftConfigManagedNamespace).Get(ctx, api.OpenShiftConsoleConfigMapName, metav1.GetOptions{})
	if mcErr != nil && !apierrors.IsNotFound(mcErr) {
		return nil, false, "FailedGetManagedConfig", mcErr
	}

	useDefaultCAFile := false
	// We are syncing the `default-ingress-cert` configmap from `openshift-config-managed` to `openshift-console`.
	// `default-ingress-cert` is only published in `openshift-config-managed` in OpenShift 4.4.0 and newer.
	// If the `default-ingress-cert` configmap in `openshift-console` exists, we should mount that to the console container,
	// otherwise default to `/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`
	_, rcaErr := c.configMapClient.ConfigMaps(api.OpenShiftConsoleNamespace).Get(ctx, api.DefaultIngressCertConfigMapName, metav1.GetOptions{})
	if rcaErr != nil && apierrors.IsNotFound(rcaErr) {
		useDefaultCAFile = true
	}

	inactivityTimeoutSeconds := 0
	oauthClient, oacErr := c.oauthClient.OAuthClients().Get(ctx, oauthsub.Stub().Name, metav1.GetOptions{})
	if oacErr != nil {
		return nil, false, "FailedGetOAuthClient", oacErr
	}
	if oauthClient.AccessTokenInactivityTimeoutSeconds != nil {
		inactivityTimeoutSeconds = int(*oauthClient.AccessTokenInactivityTimeoutSeconds)
	} else {
		if configSet.OAuth.Spec.TokenConfig.AccessTokenInactivityTimeout != nil {
			inactivityTimeoutSeconds = int(configSet.OAuth.Spec.TokenConfig.AccessTokenInactivityTimeout.Seconds())
		}
	}

	pluginsEndpoingMap := c.GetPluginsEndpointMap(configSet.Operator.Spec.Plugins)
	monitoringSharedConfig, mscErr := c.configMapClient.ConfigMaps(api.OpenShiftConfigManagedNamespace).Get(ctx, api.OpenShiftMonitoringConfigMapName, metav1.GetOptions{})
	if mscErr != nil && !apierrors.IsNotFound(mscErr) {
		return nil, false, "FailedGetMonitoringSharedConfig", mscErr
	}

	defaultConfigmap, _, err := configmapsub.DefaultConfigMap(configSet.Operator, configSet.Console, managedConfig, monitoringSharedConfig, configSet.Infrastructure, activeConsoleRoute, useDefaultCAFile, inactivityTimeoutSeconds, pluginsEndpoingMap)
	if err != nil {
		return nil, false, "FailedConsoleConfigBuilder", err
	}
	cm, cmChanged, cmErr := resourceapply.ApplyConfigMap(c.configMapClient, recorder, defaultConfigmap)
	if cmErr != nil {
		return nil, false, "FailedApply", cmErr
	}
	if cmChanged {
		klog.V(4).Infoln("new console config yaml:")
		klog.V(4).Infof("%s", cm.Data)
	}
	return cm, cmChanged, "ConsoleConfigBuilder", cmErr
}

func (c *ConfigMapSyncController) getConfigs(ctx context.Context) (*util.ConfigSet, error) {
	operatorConfig, err := c.operatorConfigClient.Get(ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Error("failed to retrieve operator config: %v", err)
		return nil, err
	}

	consoleConfig, err := c.consoleConfigClient.Get(ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("console config error: %v", err)
		return nil, err
	}

	// we need infrastructure config for apiServerURL
	infrastructureConfig, err := c.infrastructureConfigClient.Get(ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("infrastructure config error: %v", err)
		return nil, err
	}

	proxyConfig, err := c.proxyConfigClient.Get(ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("proxy config error: %v", err)
		return nil, err
	}

	oauthConfig, err := c.oauthConfigClient.Get(ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("oauth config error: %v", err)
		return nil, err
	}

	configSet := &util.ConfigSet{
		Console:        consoleConfig,
		Operator:       operatorConfig,
		Infrastructure: infrastructureConfig,
		Proxy:          proxyConfig,
		OAuth:          oauthConfig,
	}

	return configSet, err
}

func (c *ConfigMapSyncController) getRoute(ctx context.Context, operatorConfig *operatorv1.Console) (*routev1.Route, error) {
	routeName := api.OpenShiftConsoleName
	if routesub.IsCustomRouteSet(operatorConfig) {
		routeName = api.OpenshiftConsoleCustomRouteName
	}

	return c.routeClient.Routes(api.TargetNamespace).Get(ctx, routeName, metav1.GetOptions{})
}

func (c *ConfigMapSyncController) removeConfigMaps(ctx context.Context) error {
	_, err := c.operatorConfigClient.Get(ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	return err
}

func (c *ConfigMapSyncController) GetPluginsEndpointMap(enabledPluginsNames []string) map[string]string {
	pluginsEndpointMap := map[string]string{}
	for _, pluginName := range enabledPluginsNames {
		plugin, err := c.consolePluginLister.Get(pluginName)
		if err != nil {
			klog.Errorf("failed to get %q plugin: %v", pluginName, err)
			continue
		}
		pluginsEndpointMap[pluginName] = getServiceHostname(plugin)
	}
	return pluginsEndpointMap
}

func getServiceHostname(plugin *v1alpha1.ConsolePlugin) string {
	pluginURL := &url.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("%s.%s.svc.cluster.local:%d", plugin.Spec.Service.Name, plugin.Spec.Service.Namespace, plugin.Spec.Service.Port),
		Path:   plugin.Spec.Service.BasePath,
	}
	return pluginURL.String()
}
