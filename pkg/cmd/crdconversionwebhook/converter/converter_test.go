package converter

import (
	"fmt"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/go-test/deep"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sdiff "k8s.io/apimachinery/pkg/util/diff"
)

func TestConverterCRD(t *testing.T) {
	cases := []struct {
		originalObject string
		wantedObject   string
	}{
		{
			originalObject: `apiVersion: console.openshift.io/v1alpha1
kind: ConsolePlugin
metadata:
  name: console-plugin
  annotations:
    console.openshift.io/use-i18n: "true" 
spec:
  displayName: plugin
  service:
    name: console-demo-plugin
    namespace: console-demo-plugin
    port: 9001
    basePath: /
  proxy:
  - type: Service
    alias: thanos-querier
    authorize: true
    caCertificate: certContent
    service:
      name: thanos-querier
      namespace: openshift-monitoring
      port: 9091
`,
			wantedObject: `apiVersion: console.openshift.io/v1
kind: ConsolePlugin
metadata:
  annotations:
    console.openshift.io/use-i18n: "true"
  name: console-plugin
spec:
  backend:
    service:
      basePath: /
      name: console-demo-plugin
      namespace: console-demo-plugin
      port: 9001
    type: Service
  displayName: plugin
  i18n:
    loadType: Preload
  proxy:
  - alias: thanos-querier
    authorize: true
    caCertificate: certContent
    endpoint:
      service:
        name: thanos-querier
        namespace: openshift-monitoring
        port: 9091
      type: Service
`,
		}}
	for _, tc := range cases {
		t.Run("test 1", func(t *testing.T) {
			originalCR := &unstructured.Unstructured{}
			jsonObj, _ := yaml.YAMLToJSON([]byte(tc.originalObject))
			if err := originalCR.UnmarshalJSON(jsonObj); err != nil {
				t.Errorf("error unmarshalling v1alpha1 object: %q", err)
			}

			convertedCR, err := convertPlugins(originalCR)
			if err != nil {
				t.Errorf("error converting object: %q", err)
			}

			rawConvertedCR, err := convertedCR.MarshalJSON()
			if err != nil {
				t.Errorf("error converting marshaling object: %q", err)
			}

			rawYaml, err := yaml.JSONToYAML(rawConvertedCR)
			if err != nil {
				t.Errorf("error converting parsing object: %q", err)
			}

			if diff := deep.Equal(string(rawYaml), tc.wantedObject); diff != nil {
				fmt.Printf("\n%s\n", k8sdiff.ObjectReflectDiff(string(rawYaml), tc.wantedObject))
				t.Error(diff)
			}

		})
	}
}
