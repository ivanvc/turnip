// Package plugin defines the unified Plugin interface for infrastructure-
// as-code tools, and implements it for Helmfile. Each Plugin runs its
// tool's CLI as a subprocess and translates the result into a standardized
// ExecuteResult; the package has no dependency on gRPC, Kubernetes, or
// GitHub.
package plugin
