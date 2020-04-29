package service

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/openshift/console-operator/pkg/api"
	routesub "github.com/openshift/console-operator/pkg/console/subresource/route"
	"github.com/openshift/console-operator/pkg/console/subresource/util"
)

const (
	// this annotation should generate us a serving certificate
	ServingCertSecretAnnotation = "service.alpha.openshift.io/serving-cert-secret-name"
)

func DefaultService(cr *operatorv1.Console) *corev1.Service {
	labels := util.LabelsForConsole()
	meta := util.SharedMeta()
	meta.Annotations = map[string]string{
		ServingCertSecretAnnotation: api.ConsoleServingCertName,
	}
	ports := []corev1.ServicePort{
		{
			Name:       api.ConsoleContainerPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       api.ConsoleContainerPort,
			TargetPort: intstr.FromInt(api.ConsoleContainerTargetPort),
		},
	}
	if routesub.IsCustomRouteSet(cr) {
		ports = append(ports, corev1.ServicePort{
			Name:       api.RedirectContainerPortName,
			Protocol:   corev1.ProtocolTCP,
			Port:       api.RedirectContainerPort,
			TargetPort: intstr.FromInt(api.RedirectContainerTargetPort),
		})
	}

	service := Stub()
	service.Spec = corev1.ServiceSpec{
		Ports:           ports,
		Selector:        labels,
		Type:            "ClusterIP",
		SessionAffinity: "None",
	}

	util.AddOwnerRef(service, util.OwnerRefFrom(cr))
	return service
}

func Stub() *corev1.Service {
	meta := util.SharedMeta()
	meta.Annotations = map[string]string{
		ServingCertSecretAnnotation: api.ConsoleServingCertName,
	}
	service := &corev1.Service{
		ObjectMeta: meta,
	}
	return service
}
