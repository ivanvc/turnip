//go:build tools

package tools

import (
	_ "google.golang.org/grpc"
	_ "google.golang.org/protobuf/proto"
	_ "k8s.io/api/core/v1"
	_ "k8s.io/apimachinery/pkg/apis/meta/v1"
	_ "k8s.io/client-go/kubernetes"
)
