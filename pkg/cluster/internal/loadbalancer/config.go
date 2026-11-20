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

package loadbalancer

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"sigs.k8s.io/kind/pkg/errors"
)

// ConfigData is supplied to the loadbalancer config template
//
// The generated config is a pure function of this input. See the package
// documentation for which inputs must and must not affect the output.
type ConfigData struct {
	ControlPlanePort int
	// BackendServers maps a control-plane node name to its "host:port"
	// backend address. Iteration order of this map never affects the
	// generated config: entries are sorted by node name before rendering.
	BackendServers map[string]string
	IPv6           bool
}

// Backend is a single resolved backend server entry, ready for rendering.
type Backend struct {
	// Name is the control-plane node name. Backends are sorted by Name.
	Name string
	// Host and Port are the parsed backend address.
	Host string
	Port string
}

// DynamicFilesystemConfigTemplate holds the Envoy bootstrap configuration template
// for file-based dynamic xDS (CDS and LDS).
// https://www.envoyproxy.io/docs/envoy/latest/start/quick-start/configuration-dynamic-filesystem
const DynamicFilesystemConfigTemplate = `
node:
  cluster: %s
  id: %s

dynamic_resources:
  cds_config:
    resource_api_version: V3
    path_config_source:
      path: %s
  lds_config:
    resource_api_version: V3
    path_config_source:
      path: %s

admin:
  access_log_path: /dev/stdout
  address:
    socket_address:
      address: 0.0.0.0
      port_value: 10000
`

// keep in sync with dynamicFilesystemConfig
const (
	ProxyConfigPathCDS = "/home/envoy/cds.yaml"
	ProxyConfigPathLDS = "/home/envoy/lds.yaml"
	ProxyConfigPath    = "/home/envoy/envoy.yaml"
	ProxyConfigDir     = "/home/envoy"
)

// ProxyLDSConfigTemplate is the loadbalancer config template for listeners
const ProxyLDSConfigTemplate = `
resources:
- "@type": type.googleapis.com/envoy.config.listener.v3.Listener
  name: listener_apiserver
  address:
    socket_address:
      address: {{ if .IPv6 }}"::"{{ else }}"0.0.0.0"{{ end }}
      port_value: {{ .ControlPlanePort }}
  filter_chains:
  - filters:
    - name: envoy.filters.network.tcp_proxy
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy
        stat_prefix: ingress_tcp
        cluster: kube_apiservers
`

// ProxyCDSConfigTemplate is the loadbalancer config template for clusters
// https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/core/v3/health_check.proto#envoy-v3-api-msg-config-core-v3-healthcheck-httphealthcheck
const ProxyCDSConfigTemplate = `
resources:
- "@type": type.googleapis.com/envoy.config.cluster.v3.Cluster
  name: kube_apiservers
  connect_timeout: 0.25s
  type: STRICT_DNS
  lb_policy: ROUND_ROBIN
  dns_lookup_family: {{ if $.IPv6 -}} AUTO {{- else -}} V4_PREFERRED {{- end }}
  health_checks:
  - timeout: 3s
    interval: 2s
    unhealthy_threshold: 2
    healthy_threshold: 1
    initial_jitter: 0s
    no_traffic_interval: 3s
    always_log_health_check_failures: true
    always_log_health_check_success: true
    event_log_path: /dev/stdout
    http_health_check:
      path: /healthz
    transport_socket_match_criteria:
      tls_mode: "true"
  transport_socket_matches:
  - name: "health_check_tls"
    match:
      tls_mode: "true"
    transport_socket:
      name: envoy.transport_sockets.tls
      typed_config:
        "@type": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext
        common_tls_context:
          validation_context:
            trust_chain_verification: ACCEPT_UNTRUSTED
  load_assignment:
    cluster_name: kube_apiservers
    endpoints:
    - lb_endpoints:
{{- range .Backends }}
      - endpoint:
          address:
            socket_address:
              address: {{ .Host }}
              port_value: {{ .Port }}
{{- end }}
`

// configTemplateData is the view of ConfigData actually passed to the
// templates: backends are resolved, validated and sorted exactly once by
// Config, so rendering never depends on map iteration order.
type configTemplateData struct {
	ControlPlanePort int
	IPv6             bool
	Backends         []Backend
}

// resolveBackends validates the backend servers and returns them as a list
// sorted by node name.
//
// Sorting by node name (rather than by address) keeps the output stable
// across the cluster lifecycle: node names never change, while addresses
// may be re-assigned by the container runtime. Any error — a malformed
// address, a duplicate address, or an empty backend set — fails the whole
// generation; a backend is never silently omitted.
func resolveBackends(servers map[string]string) ([]Backend, error) {
	if len(servers) == 0 {
		return nil, errors.New("no backend servers: refusing to generate a loadbalancer config without any control-plane backend")
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	backends := make([]Backend, 0, len(names))
	seen := make(map[string]string, len(servers)) // canonical address -> first node name
	for _, name := range names {
		host, port, err := net.SplitHostPort(servers[name])
		if err != nil {
			return nil, errors.Wrapf(err, "invalid backend address %q for server %q", servers[name], name)
		}
		key, err := canonicalHostPort(host, port)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid backend address %q for server %q", servers[name], name)
		}
		if prev, ok := seen[key]; ok {
			return nil, errors.Errorf(
				"backend servers %q and %q resolve to the same address %q: "+
					"two control-plane nodes indistinguishable to the loadbalancer indicate a broken address assignment; "+
					"refusing to generate a silently degraded config",
				prev, name, servers[name])
		}
		seen[key] = name
		backends = append(backends, Backend{Name: name, Host: host, Port: port})
	}
	return backends, nil
}

// canonicalHostPort returns a canonical form of a host:port pair so that
// addresses can be compared by meaning rather than by spelling:
//   - IP hosts are normalized with netip (e.g. "2001:0db8::1" and
//     "2001:db8::1" are the same address; IPv4-in-IPv6 is unmapped)
//   - hostname hosts are lower-cased (DNS names are case-insensitive)
//   - ports are compared numerically (e.g. "6443" and "06443" are equal)
func canonicalHostPort(host, port string) (string, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		host = addr.Unmap().String()
	} else {
		host = strings.ToLower(host)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return "", errors.Wrapf(err, "invalid port %q", port)
	}
	return host + ":" + strconv.Itoa(portNum), nil
}

// HostPortEqual reports whether two "host:port" backend addresses are
// semantically equal, independent of spelling differences such as IPv6
// abbreviation forms. It returns an error if either address is malformed.
func HostPortEqual(a, b string) (bool, error) {
	hostA, portA, err := net.SplitHostPort(a)
	if err != nil {
		return false, errors.Wrapf(err, "invalid address %q", a)
	}
	hostB, portB, err := net.SplitHostPort(b)
	if err != nil {
		return false, errors.Wrapf(err, "invalid address %q", b)
	}
	keyA, err := canonicalHostPort(hostA, portA)
	if err != nil {
		return false, errors.Wrapf(err, "invalid address %q", a)
	}
	keyB, err := canonicalHostPort(hostB, portB)
	if err != nil {
		return false, errors.Wrapf(err, "invalid address %q", b)
	}
	return keyA == keyB, nil
}

// Config returns a loadbalancer config generated from config data.
//
// The output is deterministic: the same ConfigData always produces
// byte-identical output, regardless of the order in which backends were
// discovered or inserted into ConfigData.BackendServers.
func Config(data *ConfigData, configTemplate string) (config string, err error) {
	// parse the template first so template syntax errors are reported
	// before any input validation error
	t, err := template.New("loadbalancer-config").Parse(configTemplate)
	if err != nil {
		return "", errors.Wrap(err, "failed to parse config template")
	}
	// resolve and validate the backends exactly once; any failure here
	// fails the whole generation rather than producing a partial config
	backends, err := resolveBackends(data.BackendServers)
	if err != nil {
		return "", err
	}
	// execute the template
	var buff bytes.Buffer
	err = t.Execute(&buff, &configTemplateData{
		ControlPlanePort: data.ControlPlanePort,
		IPv6:             data.IPv6,
		Backends:         backends,
	})
	if err != nil {
		return "", errors.Wrap(err, "error executing config template")
	}

	return buff.String(), nil
}

func GenerateBootstrapCommand(clusterName, containerName string) []string {
	// populate the values to dynamic template
	envoyConfig := fmt.Sprintf(
		DynamicFilesystemConfigTemplate,
		clusterName,   // node.cluster = Kind cluster name
		containerName, // node.id = container name
		ProxyConfigPathCDS,
		ProxyConfigPathLDS,
	)

	// Create dynamic Envoy config files with valid empty resources
	emptyConfig := "resources: []"
	return []string{"bash", "-c",
		fmt.Sprintf(`mkdir -p %s && echo -en '%s' > %s && { [ ! -f %s ] && echo -en '%s' > %s || true; } && { [ ! -f %s ] && echo -en '%s' > %s || true; } && while true; do envoy -c %s && break; sleep 1; done`,
			ProxyConfigDir,
			envoyConfig, ProxyConfigPath,
			ProxyConfigPathCDS, emptyConfig, ProxyConfigPathCDS, // Initialize CDS only if not exists
			ProxyConfigPathLDS, emptyConfig, ProxyConfigPathLDS, // Initialize LDS only if not exists
			ProxyConfigPath)}
}
