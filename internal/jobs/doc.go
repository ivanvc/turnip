// Package jobs builds and manages the Kubernetes Jobs that run Runner Pods:
// resolving per-tool binary versions, constructing the Job spec (including
// the initContainer that provisions the tool binary and the environment
// variables the Runner reads at startup), and creating/deleting Jobs via
// client-go.
package jobs
