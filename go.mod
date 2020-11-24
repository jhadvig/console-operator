module github.com/openshift/console-operator

go 1.13

require (
	github.com/blang/semver v3.5.0+incompatible
	github.com/davecgh/go-spew v1.1.1
	github.com/getsentry/raven-go v0.2.1-0.20190513200303-c977f96e1095 // indirect
	github.com/ghodss/yaml v1.0.0
	github.com/go-test/deep v1.0.5
	github.com/openshift/api v0.0.0-20201019163320-c6a5ec25f267
	github.com/openshift/build-machinery-go v0.0.0-20200917070002-f171684f77ab
	github.com/openshift/client-go v0.0.0-20200827190008-3062137373b5
	github.com/openshift/library-go v0.0.0-20200907120738-ea57b121ba1a
	github.com/pkg/profile v1.4.0 // indirect
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	gopkg.in/yaml.v2 v2.3.0
	k8s.io/api v0.19.2
	k8s.io/apimachinery v0.19.2
	k8s.io/client-go v0.19.2
	k8s.io/component-base v0.19.2
	k8s.io/klog v0.3.0
	k8s.io/klog/v2 v2.3.0
	monis.app/go v0.0.0-20190702030534-c65526068664
)

replace (
	github.com/openshift/api => github.com/jhadvig/api v0.0.0-20201120110223-105453730373
	github.com/openshift/client-go => github.com/jhadvig/client-go v0.0.0-20201120114228-b390d4a67199
)
