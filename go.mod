module github.com/openshift/console-operator

go 1.16

require (
	github.com/blang/semver v3.5.1+incompatible
	github.com/davecgh/go-spew v1.1.1
	github.com/ghodss/yaml v1.0.0
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/go-test/deep v1.0.5
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822
	github.com/open-cluster-management/api v0.0.0-20210527013639-a6845f2ebcb1
	github.com/openshift/api v0.0.0-20220525145417-ee5b62754c68
	github.com/openshift/build-machinery-go v0.0.0-20211213093930-7e33a7eb4ce3
	github.com/openshift/client-go v0.0.0-20220525160904-9e1acff93e4a
	github.com/openshift/library-go v0.0.0-20220704153411-3ea4b775d418
	github.com/pkg/profile v1.4.0 // indirect
	github.com/spf13/cobra v1.4.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.24.0
	k8s.io/apiextensions-apiserver v0.24.0
	k8s.io/apimachinery v0.24.0
	k8s.io/client-go v0.24.0
	k8s.io/component-base v0.24.0
	k8s.io/klog/v2 v2.60.1
	k8s.io/utils v0.0.0-20220210201930-3a6ce19ff2f9
)

replace (
	github.com/openshift/api => github.com/jhadvig/api v0.0.0-20220818101750-1b7438fb9512
	github.com/openshift/client-go => github.com/jhadvig/client-go v0.0.0-20220818123851-a993fd10e1f8
)
