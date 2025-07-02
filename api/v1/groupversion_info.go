package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion = schema.GroupVersion{Group: "vlanman.dialo.ai", Version: "v1"}

	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	AddToScheme = SchemeBuilder.AddToScheme
)
