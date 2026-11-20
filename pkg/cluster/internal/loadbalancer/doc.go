/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package loadbalancer contains external loadbalancer related constants and configuration
//
// # Deterministic config generation
//
// The generated envoy config is a pure function of the cluster's
// control-plane node set. Inputs are classified as follows:
//
// Must affect the output:
//   - the set of control-plane node names (one backend entry each)
//   - each node's backend address ("host:port")
//   - the control-plane port the loadbalancer listens on
//   - the cluster IP family (listen address and dns_lookup_family)
//
// Must NOT affect the output:
//   - the order in which nodes were discovered or joined the cluster
//     (backends are sorted by node name before rendering; map iteration
//     order never reaches the template)
//   - generation time, random port selection, or any other runtime
//     randomness (none exist: the port is the fixed APIServerInternalPort
//     and no timestamps are rendered)
//   - the order in which the container runtime returns a node's addresses
//     (backend hosts are container names; the address family envoy picks
//     is fixed by dns_lookup_family, not by runtime return order)
//
// Backends are sorted by node name, not by address: node names are stable
// for the whole cluster lifecycle, while addresses may be re-assigned by
// the container runtime, so sorting by address would produce spurious
// diffs and entry reordering after a container restart.
//
// # Address discovery
//
// Config generation does NOT discover or cache container IPs. Backends
// use container names, resolved at connection time by the runtime's
// embedded DNS (envoy STRICT_DNS), so IP re-assignment after a container
// restart is absorbed by envoy re-resolving DNS rather than by
// regenerating this config. Each node's backend string is constructed
// exactly once per generation, and any failure fails the whole
// generation — a backend is never silently omitted, which would
// otherwise produce a plausible-looking config missing part of the
// control plane.
//
// # Reload mechanism
//
// Envoy is bootstrapped with dynamic filesystem xDS (see
// GenerateBootstrapCommand): it watches the CDS/LDS files and applies
// updates itself. There is no SIGHUP and no process restart. Updates are
// written to a temporary file and atomically renamed into place, so
// envoy never observes a partial file. The effect on existing
// connections depends on what changed: endpoint-only (CDS) changes do
// not reset established connections, while listener (LDS) replacement
// may drain them. Callers therefore compare the newly generated config
// with the currently applied one and skip the update entirely when the
// content is unchanged.
//
// # Edge cases
//
//   - A single control-plane node gets no loadbalancer at all (the
//     container is only provisioned for 2+ control planes); this package
//     is not involved.
//   - An empty backend set is an error: a loadbalancer with zero
//     backends can only blackhole traffic, so generation fails loudly
//     instead of writing a plausible but useless config. When nodes are
//     re-added, generation succeeds again because it is a pure function
//     of the current set.
//   - Two nodes resolving to the same address (compared semantically,
//     see HostPortEqual) is an error: they are indistinguishable to the
//     loadbalancer, which indicates broken address assignment, and
//     failing loudly beats silently degrading the control plane.
package loadbalancer
