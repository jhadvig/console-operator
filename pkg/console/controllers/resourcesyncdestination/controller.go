package resourcesyncdestination

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreinformerv1 "k8s.io/client-go/informers/core/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"

	operatorsv1 "github.com/openshift/api/operator/v1"
	operatorclientv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	operatorinformersv1 "github.com/openshift/client-go/operator/informers/externalversions/operator/v1"
	"github.com/openshift/console-operator/pkg/api"
	"github.com/openshift/console-operator/pkg/console/controllers/util"
	"github.com/openshift/console-operator/pkg/console/status"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resourcesynccontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

const (
	controllerWorkQueueKey = "resource-sync-destination-work-queue-key"
	controllerName         = "ConsoleResourceSyncDestinationController"
)

type ResourceSyncDestinationController struct {
	operatorClient v1helpers.OperatorClient
	// operatorconfig
	operatorConfigClient operatorclientv1.ConsoleInterface
	configMapClient      coreclientv1.ConfigMapsGetter
	corev1Informer       coreinformerv1.Interface
	// events
	resourceSyncer resourcesynccontroller.ResourceSyncer
}

func NewResourceSyncDestinationController(
	operatorClient v1helpers.OperatorClient,
	// operatorconfig
	operatorConfigClient operatorclientv1.ConsoleInterface,
	operatorConfigInformer operatorinformersv1.ConsoleInformer,
	// configmap
	corev1Client coreclientv1.CoreV1Interface,
	corev1Informer coreinformerv1.Interface,
	// events
	recorder events.Recorder,
	resourceSyncer resourcesynccontroller.ResourceSyncer,
) factory.Controller {
	corev1Client.ConfigMaps(api.OpenShiftConsoleNamespace)
	configMapInformer := corev1Informer.ConfigMaps()

	ctrl := &ResourceSyncDestinationController{
		operatorClient:       operatorClient,
		operatorConfigClient: operatorConfigClient,
		configMapClient:      corev1Client,
		// events
		resourceSyncer: resourceSyncer,
	}

	configNameFilter := util.NamesFilter(api.ConfigResourceName)

	return factory.New().
		WithFilteredEventsInformers( // configs
			configNameFilter,
			operatorConfigInformer.Informer(),
		).WithFilteredEventsInformers(
		util.NamesFilter(api.OpenShiftConsoleConfigMapName, api.DefaultIngressCertConfigMapName, api.OpenShiftCustomLogoConfigMapName),
		configMapInformer.Informer(),
	).WithSync(ctrl.Sync).
		ToController("ConsoleResourceSyncDestinationController", recorder.WithComponentSuffix("console-resource-sync-destination-controller"))
}

func (c *ResourceSyncDestinationController) Sync(ctx context.Context, controllerContext factory.SyncContext) error {
	operatorConfig, err := c.operatorConfigClient.Get(ctx, api.ConfigResourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	statusHandler := status.NewStatusHandler(c.operatorClient)

	switch operatorConfig.Spec.ManagementState {
	case operatorsv1.Managed:
		klog.V(4).Infoln("console is in a managed state: syncing configmaps")
	case operatorsv1.Unmanaged:
		klog.V(4).Infoln("console is in an unmanaged state: skipping configmaps sync")
		return nil
	case operatorsv1.Removed:
		klog.V(4).Infoln("console is in an removed state: removing synced configmaps")
	default:
		return fmt.Errorf("unknown state: %v", operatorConfig.Spec.ManagementState)
	}

	syncIngressCertErr := c.syncIngressCert(ctx)
	if syncIngressCertErr != nil {
		return statusHandler.FlushAndReturn(syncIngressCertErr)
	}
	syncCustomLogoErr := c.syncCustomLogo(*operatorConfig)
	if syncCustomLogoErr != nil {
		return statusHandler.FlushAndReturn(syncCustomLogoErr)
	}

	return statusHandler.FlushAndReturn(nil)
}

func (c *ResourceSyncDestinationController) syncCustomLogo(operatorConfig operatorsv1.Console) error {
	source := resourcesynccontroller.ResourceLocation{}
	logoConfigMapName := operatorConfig.Spec.Customization.CustomLogoFile.Name

	if logoConfigMapName != "" {
		source.Name = logoConfigMapName
		source.Namespace = api.OpenShiftConfigNamespace
	}
	// if no custom logo provided, sync an empty source to delete
	return c.resourceSyncer.SyncConfigMap(
		resourcesynccontroller.ResourceLocation{Namespace: api.OpenShiftConsoleNamespace, Name: api.OpenShiftCustomLogoConfigMapName},
		source,
	)
}

func (c *ResourceSyncDestinationController) syncIngressCert(ctx context.Context) error {
	_, err := c.configMapClient.ConfigMaps(api.OpenShiftConfigManagedNamespace).Get(ctx, api.DefaultIngressCertConfigMapName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return c.resourceSyncer.SyncConfigMap(
				resourcesynccontroller.ResourceLocation{Name: api.DefaultIngressCertConfigMapName, Namespace: api.OpenShiftConsoleNamespace},
				resourcesynccontroller.ResourceLocation{},
			)
		}
		return err
	}

	// sync: 'default-ingress-cert' configmap
	// from: 'openshift-config-managed' namespace
	// to:   'openshift-console' namespace
	return c.resourceSyncer.SyncConfigMap(
		resourcesynccontroller.ResourceLocation{Name: api.DefaultIngressCertConfigMapName, Namespace: api.OpenShiftConsoleNamespace},
		resourcesynccontroller.ResourceLocation{Name: api.DefaultIngressCertConfigMapName, Namespace: api.OpenShiftConfigManagedNamespace},
	)
}
