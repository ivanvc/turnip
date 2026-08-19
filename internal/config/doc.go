// Package config parses and validates turnip.yaml configuration files, and
// matches the projects they declare against a repository's modified files.
// It is a pure library: it accepts turnip.yaml content as bytes and has no
// dependency on GitHub, Redis, gRPC, or the plugin system.
package config
