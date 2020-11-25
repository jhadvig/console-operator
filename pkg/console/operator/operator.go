package operator

import (
	// standard lib
	"context"
	"fmt"
	"time"

	// kube
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	corev1 "k8s.io/client-go/informers/core/v1"
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"

	// openshift
	configv1 "github.com/openshift/api/config/v1"
	operatorsv1 "github.com/openshift/api/operator/v1"
	oauthclientv1 "github.com/openshift/client-go/oauth/clientset/versioned/typed/oauth/v1"
	oauthinformersv1 "github.com/openshift/client-go/oauth/informers/externalversions/oauth/v1"
	routeclientv1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"monis.app/go/openshift/operator"

	// informers
	configinformer "github.com/openshift/client-go/config/informers/externalversions"
	consoleinformersv1 "github.com/openshift/client-go/console/informers/externalversions/console/v1"
	operatorinformerv1 "github.com/openshift/client-go/operator/informers/externalversions/operator/v1"

	routesinformersv1 "github.com/openshift/client-go/route/informers/externalversions/route/v1"
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"

	// clients
	configclientv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	consoleclientv1 "github.com/openshift/client-go/console/clientset/versioned/typed/console/v1"
	operatorclientv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"

	// operator
	"github.com/openshift/console-operator/pkg/console/subresource/configmap"
	"github.com/openshift/console-operator/pkg/console/subresource/deployment"
	"github.com/openshift/console-operator/pkg/console/subresource/oauthclient"
	"github.com/openshift/console-operator/pkg/console/subresource/secret"
)

const (
	controllerName = "Console"
)

type consoleOperator struct {
	// configs
	operatorClient             v1helpers.OperatorClient
	operatorConfigClient       operatorclientv1.ConsoleInterface
	consoleConfigClient        configclientv1.ConsoleInterface
	infrastructureConfigClient configclientv1.InfrastructureInterface
	proxyConfigClient          configclientv1.ProxyInterface
	oauthConfigClient          configclientv1.OAuthInterface
	consolePluginClient        consoleclientv1.ConsolePluginInterface
	// core kube
	secretsClient    coreclientv1.SecretsGetter
	configMapClient  coreclientv1.ConfigMapsGetter
	serviceClient    coreclientv1.ServicesGetter
	deploymentClient appsv1.DeploymentsGetter
	// openshift
	routeClient   routeclientv1.RoutesGetter
	oauthClient   oauthclientv1.OAuthClientsGetter
	versionGetter status.VersionGetter
	// recorder
	recorder       events.Recorder
	resourceSyncer resourcesynccontroller.ResourceSyncer
	// context
	ctx context.Context
}

func NewConsoleOperator(
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
	//plugins
	consolePluginClient consoleclientv1.ConsolePluginInterface,
	consolePluginInformer consoleinformersv1.ConsolePluginInformer,
	// openshift managed
	managedCoreV1 corev1.Interface,
	// event handling
	versionGetter status.VersionGetter,
	recorder events.Recorder,
	resourceSyncer resourcesynccontroller.ResourceSyncer,
	// context
	ctx context.Context,
) operator.Runner {
	c := &consoleOperator{
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
		routeClient:         routev1Client,
		oauthClient:         oauthv1Client,
		consolePluginClient: consolePluginClient,
		versionGetter:       versionGetter,
		// recorder
		recorder:       recorder,
		resourceSyncer: resourceSyncer,
		ctx:            ctx,
	}

	secretsInformer := coreV1.Secrets()
	configMapInformer := coreV1.ConfigMaps()
	managedConfigMapInformer := managedCoreV1.ConfigMaps()
	serviceInformer := coreV1.Services()
	configV1Informers := configInformer.Config().V1()

	configNameFilter := operator.FilterByNames(api.ConfigResourceName)
	targetNameFilter := operator.FilterByNames(api.OpenShiftConsoleName)

	return operator.New(controllerName, c,
		// configs
		operator.WithInformer(configV1Informers.Consoles(), configNameFilter),
		operator.WithInformer(operatorConfigInformer, configNameFilter),
		operator.WithInformer(configV1Informers.Infrastructures(), configNameFilter),
		operator.WithInformer(configV1Informers.Proxies(), configNameFilter),
		operator.WithInformer(configV1Informers.OAuths(), configNameFilter),
		// console resources
		operator.WithInformer(deployments, targetNameFilter),
		operator.WithInformer(routes, targetNameFilter),
		operator.WithInformer(serviceInformer, targetNameFilter),
		operator.WithInformer(oauthClients, targetNameFilter),
		// special resources with unique names
		operator.WithInformer(configMapInformer, operator.FilterByNames(api.OpenShiftConsoleConfigMapName, api.ServiceCAConfigMapName, api.OpenShiftCustomLogoConfigMapName, api.TrustedCAConfigMapName)),
		operator.WithInformer(managedConfigMapInformer, operator.FilterByNames(api.OpenShiftConsoleConfigMapName, api.OpenShiftConsolePublicConfigMapName)),
		operator.WithInformer(secretsInformer, operator.FilterByNames(deployment.ConsoleOauthConfigName)),
		// plugins
		// operator.WithInformer(consolePluginInformer, acceptAllFilter()),
	)
}

// key is actually the pivot point for the operator, which is our Console custom resource
func (c *consoleOperator) Key() (metav1.Object, error) {
	return c.operatorConfigClient.Get(c.ctx, api.ConfigResourceName, metav1.GetOptions{})
}

// func acceptAllFilter() *controller.FilterFuncs {
// 	return &controller.FilterFuncs{
// 		AddFunc:    func(obj, metav1.Object) { return true },
// 		UpdateFunc: func(_, _, metav1.Object) { return true },
// 		DeleteFunc: func(_, metav1.Object) { return true },
// 	}
// }

type configSet struct {
	Console        *configv1.Console
	Operator       *operatorsv1.Console
	Infrastructure *configv1.Infrastructure
	Proxy          *configv1.Proxy
	OAuth          *configv1.OAuth
}

func (c *consoleOperator) Sync(obj metav1.Object) error {
	startTime := time.Now()
	klog.V(4).Infof("started syncing operator %q (%v)", obj.GetName(), startTime)
	defer klog.V(4).Infof("finished syncing operator %q (%v)", obj.GetName(), time.Since(startTime))

	// we need to cast the operator config
	operatorConfig := obj.(*operatorsv1.Console)

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

	configs := configSet{
		Console:        consoleConfig,
		Operator:       operatorConfig,
		Infrastructure: infrastructureConfig,
		Proxy:          proxyConfig,
		OAuth:          oauthConfig,
	}

	if err := c.handleSync(configs); err != nil {
		return err
	}

	return nil
}

func (c *consoleOperator) handleSync(configs configSet) error {
	updatedStatus := configs.Operator.DeepCopy()

	switch updatedStatus.Spec.ManagementState {
	case operatorsv1.Managed:
		klog.V(4).Infoln("console is in a managed state.")
		// handled below
	case operatorsv1.Unmanaged:
		klog.V(4).Infoln("console is in an unmanaged state.")
		return nil
	case operatorsv1.Removed:
		klog.V(4).Infoln("console has been removed.")
		return c.removeConsole()
	default:
		return fmt.Errorf("console is in an unknown state: %v", updatedStatus.Spec.ManagementState)
	}

	return c.sync_v400(updatedStatus, configs)
}

// this may need to move to sync_v400 if versions ever have custom delete logic
func (c *consoleOperator) removeConsole() error {
	klog.V(2).Info("deleting console resources")
	defer klog.V(2).Info("finished deleting console resources")
	var errs []error
	// configmaps
	errs = append(errs, c.configMapClient.ConfigMaps(api.TargetNamespace).Delete(c.ctx, configmap.Stub().Name, metav1.DeleteOptions{}))
	errs = append(errs, c.configMapClient.ConfigMaps(api.TargetNamespace).Delete(c.ctx, configmap.ServiceCAStub().Name, metav1.DeleteOptions{}))
	// secret
	errs = append(errs, c.secretsClient.Secrets(api.TargetNamespace).Delete(c.ctx, secret.Stub().Name, metav1.DeleteOptions{}))
	// existingOAuthClient is not a delete, it is a deregister/neutralize
	existingOAuthClient, getAuthErr := c.oauthClient.OAuthClients().Get(c.ctx, oauthclient.Stub().Name, metav1.GetOptions{})
	errs = append(errs, getAuthErr)
	if len(existingOAuthClient.RedirectURIs) != 0 {
		_, updateAuthErr := c.oauthClient.OAuthClients().Update(c.ctx, oauthclient.DeRegisterConsoleFromOAuthClient(existingOAuthClient), metav1.UpdateOptions{})
		errs = append(errs, updateAuthErr)
	}
	// deployment
	// NOTE: CVO controls the deployment for downloads, console-operator cannot delete it.
	errs = append(errs, c.deploymentClient.Deployments(api.TargetNamespace).Delete(c.ctx, deployment.Stub().Name, metav1.DeleteOptions{}))
	// clear the console URL from the public config map in openshift-config-managed
	_, _, updateConfigErr := resourceapply.ApplyConfigMap(c.configMapClient, c.recorder, configmap.EmptyPublicConfig())
	errs = append(errs, updateConfigErr)

	return utilerrors.FilterOut(utilerrors.NewAggregate(errs), errors.IsNotFound)
}
