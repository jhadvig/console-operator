package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	// openshift
	configv1 "github.com/openshift/api/config/v1"
	operatorsv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	// operator
)

type ConfigSet struct {
	Console        *configv1.Console
	Operator       *operatorsv1.Console
	Infrastructure *configv1.Infrastructure
	Proxy          *configv1.Proxy
	OAuth          *configv1.OAuth
}

func NamesFilter(names ...string) factory.EventFilterFunc {
	nameSet := sets.NewString(names...)
	return func(obj interface{}) bool {
		metaObj := obj.(metav1.Object)
		if nameSet.Has(metaObj.GetName()) {
			return true
		}
		return false
	}
}
