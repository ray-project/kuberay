package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"k8s.io/kube-openapi/pkg/common"
	"k8s.io/kube-openapi/pkg/validation/spec"

	"github.com/ray-project/kuberay/ray-operator/pkg/generated/openapi"
)

// Outputs OpenAPI schema JSON containing the schema definitions in
// zz_generated.openapi.go. This is consumed by applyconfiguration-gen
// to produce complete structured-merge-diff type schemas.
func main() {
	if err := output(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed: %v\n", err)
		os.Exit(1)
	}
}

func output() error {
	refFunc := func(name string) spec.Ref {
		return spec.MustCreateRef(fmt.Sprintf("#/definitions/%s", friendlyName(name)))
	}
	defs := openapi.GetOpenAPIDefinitions(refFunc)

	schemaDefs := make(map[string]spec.Schema, len(defs))
	for k, v := range defs {
		if schema, ok := v.Schema.Extensions[common.ExtensionV2Schema]; ok {
			if v2Schema, isOpenAPISchema := schema.(spec.Schema); isOpenAPISchema {
				schemaDefs[friendlyName(k)] = v2Schema
				continue
			}
		}
		schemaDefs[friendlyName(k)] = v.Schema
	}

	data, err := json.Marshal(&spec.Swagger{
		SwaggerProps: spec.SwaggerProps{
			Definitions: schemaDefs,
			Info: &spec.Info{
				InfoProps: spec.InfoProps{
					Title:   "KubeRay API",
					Version: "unversioned",
				},
			},
			Swagger: "2.0",
		},
	})
	if err != nil {
		return fmt.Errorf("error serializing API definitions: %w", err)
	}
	os.Stdout.Write(data)
	return nil
}

// friendlyName converts a Go package path to an OpenAPI-friendly name.
// From k8s.io/apiserver/pkg/endpoints/openapi/openapi.go
func friendlyName(name string) string {
	nameParts := strings.Split(name, "/")
	if len(nameParts) > 0 && strings.Contains(nameParts[0], ".") {
		parts := strings.Split(nameParts[0], ".")
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		nameParts[0] = strings.Join(parts, ".")
	}
	return strings.Join(nameParts, ".")
}
