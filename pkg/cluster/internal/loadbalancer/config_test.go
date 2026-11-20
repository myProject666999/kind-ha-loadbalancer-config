/*
Copyright The Kubernetes Authors.

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
	"strings"
	"testing"
)

func TestConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		data            *ConfigData
		configTemplate  string
		wantConfig      string
		wantErrContains string
	}{
		{
			name: "renders lds template",
			data: &ConfigData{
				ControlPlanePort: 6443,
				BackendServers: map[string]string{
					"node0": "node0:6443",
				},
				IPv6: false,
			},
			configTemplate: ProxyLDSConfigTemplate,
			wantConfig: "\n" +
				"resources:\n" +
				"- \"@type\": type.googleapis.com/envoy.config.listener.v3.Listener\n" +
				"  name: listener_apiserver\n" +
				"  address:\n" +
				"    socket_address:\n" +
				"      address: \"0.0.0.0\"\n" +
				"      port_value: 6443\n" +
				"  filter_chains:\n" +
				"  - filters:\n" +
				"    - name: envoy.filters.network.tcp_proxy\n" +
				"      typed_config:\n" +
				"        \"@type\": type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy\n" +
				"        stat_prefix: ingress_tcp\n" +
				"        cluster: kube_apiservers\n",
		},
		{
			name: "renders cds template",
			data: &ConfigData{
				BackendServers: map[string]string{
					"node0": "node0:6443",
				},
				IPv6: false,
			},
			configTemplate: ProxyCDSConfigTemplate,
			wantConfig: "\n" +
				"resources:\n" +
				"- \"@type\": type.googleapis.com/envoy.config.cluster.v3.Cluster\n" +
				"  name: kube_apiservers\n" +
				"  connect_timeout: 0.25s\n" +
				"  type: STRICT_DNS\n" +
				"  lb_policy: ROUND_ROBIN\n" +
				"  dns_lookup_family: V4_PREFERRED\n" +
				"  health_checks:\n" +
				"  - timeout: 3s\n" +
				"    interval: 2s\n" +
				"    unhealthy_threshold: 2\n" +
				"    healthy_threshold: 1\n" +
				"    initial_jitter: 0s\n" +
				"    no_traffic_interval: 3s\n" +
				"    always_log_health_check_failures: true\n" +
				"    always_log_health_check_success: true\n" +
				"    event_log_path: /dev/stdout\n" +
				"    http_health_check:\n" +
				"      path: /healthz\n" +
				"    transport_socket_match_criteria:\n" +
				"      tls_mode: \"true\"\n" +
				"  transport_socket_matches:\n" +
				"  - name: \"health_check_tls\"\n" +
				"    match:\n" +
				"      tls_mode: \"true\"\n" +
				"    transport_socket:\n" +
				"      name: envoy.transport_sockets.tls\n" +
				"      typed_config:\n" +
				"        \"@type\": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext\n" +
				"        common_tls_context:\n" +
				"          validation_context:\n" +
				"            trust_chain_verification: ACCEPT_UNTRUSTED\n" +
				"  load_assignment:\n" +
				"    cluster_name: kube_apiservers\n" +
				"    endpoints:\n" +
				"    - lb_endpoints:\n" +
				"      - endpoint:\n" +
				"          address:\n" +
				"            socket_address:\n" +
				"              address: node0\n" +
				"              port_value: 6443\n",
		},
		{
			name: "renders cds template ipv6",
			data: &ConfigData{
				BackendServers: map[string]string{
					"node0": "node0:6443",
				},
				IPv6: true,
			},
			configTemplate: ProxyCDSConfigTemplate,
			wantConfig: "\n" +
				"resources:\n" +
				"- \"@type\": type.googleapis.com/envoy.config.cluster.v3.Cluster\n" +
				"  name: kube_apiservers\n" +
				"  connect_timeout: 0.25s\n" +
				"  type: STRICT_DNS\n" +
				"  lb_policy: ROUND_ROBIN\n" +
				"  dns_lookup_family: AUTO\n" +
				"  health_checks:\n" +
				"  - timeout: 3s\n" +
				"    interval: 2s\n" +
				"    unhealthy_threshold: 2\n" +
				"    healthy_threshold: 1\n" +
				"    initial_jitter: 0s\n" +
				"    no_traffic_interval: 3s\n" +
				"    always_log_health_check_failures: true\n" +
				"    always_log_health_check_success: true\n" +
				"    event_log_path: /dev/stdout\n" +
				"    http_health_check:\n" +
				"      path: /healthz\n" +
				"    transport_socket_match_criteria:\n" +
				"      tls_mode: \"true\"\n" +
				"  transport_socket_matches:\n" +
				"  - name: \"health_check_tls\"\n" +
				"    match:\n" +
				"      tls_mode: \"true\"\n" +
				"    transport_socket:\n" +
				"      name: envoy.transport_sockets.tls\n" +
				"      typed_config:\n" +
				"        \"@type\": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext\n" +
				"        common_tls_context:\n" +
				"          validation_context:\n" +
				"            trust_chain_verification: ACCEPT_UNTRUSTED\n" +
				"  load_assignment:\n" +
				"    cluster_name: kube_apiservers\n" +
				"    endpoints:\n" +
				"    - lb_endpoints:\n" +
				"      - endpoint:\n" +
				"          address:\n" +
				"            socket_address:\n" +
				"              address: node0\n" +
				"              port_value: 6443\n",
		},
		{
			name:            "returns wrapped parse error",
			data:            &ConfigData{},
			configTemplate:  `{{`,
			wantErrContains: "failed to parse config template",
		},
		{
			name: "returns validation error for malformed backend address",
			data: &ConfigData{
				BackendServers: map[string]string{
					"node0": "node0",
				},
			},
			configTemplate:  ProxyCDSConfigTemplate,
			wantErrContains: "invalid backend address",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Config(tt.data, tt.configTemplate)

			if tt.wantErrContains != "" {
				if err == nil {
					t.Fatalf("Config() error = nil, want error containing %q", tt.wantErrContains)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Fatalf("Config() error = %q, want to contain %q", err.Error(), tt.wantErrContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Config() unexpected error: %v", err)
			}

			if got != tt.wantConfig {
				t.Fatalf("Config() output mismatch\n got:\n%q\nwant:\n%q", got, tt.wantConfig)
			}
		})
	}
}

// cdsConfigFor is a test helper that generates the CDS config for the
// given backends and fails the test on any error.
func cdsConfigFor(t *testing.T, ipv6 bool, backends map[string]string) string {
	t.Helper()
	got, err := Config(&ConfigData{
		ControlPlanePort: 6443,
		BackendServers:   backends,
		IPv6:             ipv6,
	}, ProxyCDSConfigTemplate)
	if err != nil {
		t.Fatalf("Config() unexpected error: %v", err)
	}
	return got
}

// The same node set must produce byte-identical output no matter in which
// order the nodes were discovered or inserted into the backend map.
func TestConfigDeterministicForAllPermutations(t *testing.T) {
	t.Parallel()

	nodes := [][2]string{
		{"node-a", "10.0.0.1:6443"},
		{"node-b", "10.0.0.2:6443"},
		{"node-c", "10.0.0.3:6443"},
	}
	permutations := [][]int{
		{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0},
	}

	var want string
	for i, perm := range permutations {
		backends := map[string]string{}
		for _, idx := range perm {
			backends[nodes[idx][0]] = nodes[idx][1]
		}
		got := cdsConfigFor(t, false, backends)
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("Config() output for insertion order %v differs from first permutation\n got:\n%q\nwant:\n%q", perm, got, want)
		}
	}
}

// Adding A then B must equal adding B then A, and removing B from {A,B,C}
// must yield exactly the config generated for {A,C} from the start.
func TestConfigAddRemoveOrderIndependent(t *testing.T) {
	t.Parallel()

	a := []string{"node-a", "10.0.0.1:6443"}
	b := []string{"node-b", "10.0.0.2:6443"}
	c := []string{"node-c", "10.0.0.3:6443"}

	// add A then B vs add B then A
	ab := cdsConfigFor(t, false, map[string]string{a[0]: a[1], b[0]: b[1]})
	ba := cdsConfigFor(t, false, map[string]string{b[0]: b[1], a[0]: a[1]})
	if ab != ba {
		t.Fatalf("add order changed the output\n add A,B:\n%q\n add B,A:\n%q", ab, ba)
	}

	// remove B from {A,B,C} vs only ever having {A,C}
	abc := map[string]string{a[0]: a[1], b[0]: b[1], c[0]: c[1]}
	delete(abc, b[0])
	afterRemove := cdsConfigFor(t, false, abc)
	fromScratch := cdsConfigFor(t, false, map[string]string{a[0]: a[1], c[0]: c[1]})
	if afterRemove != fromScratch {
		t.Fatalf("removing a node changed the remaining entries\n after remove:\n%q\n from scratch:\n%q", afterRemove, fromScratch)
	}

	// full-text golden for {A,C}: exactly these two entries, sorted by
	// node name, nothing else
	wantAC := "\n" +
		"resources:\n" +
		"- \"@type\": type.googleapis.com/envoy.config.cluster.v3.Cluster\n" +
		"  name: kube_apiservers\n" +
		"  connect_timeout: 0.25s\n" +
		"  type: STRICT_DNS\n" +
		"  lb_policy: ROUND_ROBIN\n" +
		"  dns_lookup_family: V4_PREFERRED\n" +
		"  health_checks:\n" +
		"  - timeout: 3s\n" +
		"    interval: 2s\n" +
		"    unhealthy_threshold: 2\n" +
		"    healthy_threshold: 1\n" +
		"    initial_jitter: 0s\n" +
		"    no_traffic_interval: 3s\n" +
		"    always_log_health_check_failures: true\n" +
		"    always_log_health_check_success: true\n" +
		"    event_log_path: /dev/stdout\n" +
		"    http_health_check:\n" +
		"      path: /healthz\n" +
		"    transport_socket_match_criteria:\n" +
		"      tls_mode: \"true\"\n" +
		"  transport_socket_matches:\n" +
		"  - name: \"health_check_tls\"\n" +
		"    match:\n" +
		"      tls_mode: \"true\"\n" +
		"    transport_socket:\n" +
		"      name: envoy.transport_sockets.tls\n" +
		"      typed_config:\n" +
		"        \"@type\": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext\n" +
		"        common_tls_context:\n" +
		"          validation_context:\n" +
		"            trust_chain_verification: ACCEPT_UNTRUSTED\n" +
		"  load_assignment:\n" +
		"    cluster_name: kube_apiservers\n" +
		"    endpoints:\n" +
		"    - lb_endpoints:\n" +
		"      - endpoint:\n" +
		"          address:\n" +
		"            socket_address:\n" +
		"              address: 10.0.0.1\n" +
		"              port_value: 6443\n" +
		"      - endpoint:\n" +
		"          address:\n" +
		"            socket_address:\n" +
		"              address: 10.0.0.3\n" +
		"              port_value: 6443\n"
	if fromScratch != wantAC {
		t.Fatalf("Config() output mismatch\n got:\n%q\nwant:\n%q", fromScratch, wantAC)
	}
}

// Scaling from three control-plane nodes to five must not change the
// entries of the existing three nodes: they must appear byte-identically
// and in the same relative order in the larger config.
func TestConfigScaleOutKeepsExistingEntries(t *testing.T) {
	t.Parallel()

	three := map[string]string{
		"node-b": "10.0.0.2:6443",
		"node-c": "10.0.0.3:6443",
		"node-e": "10.0.0.5:6443",
	}
	five := map[string]string{
		"node-a": "10.0.0.1:6443",
		"node-b": "10.0.0.2:6443",
		"node-c": "10.0.0.3:6443",
		"node-d": "10.0.0.4:6443",
		"node-e": "10.0.0.5:6443",
	}

	configThree := cdsConfigFor(t, false, three)
	configFive := cdsConfigFor(t, false, five)

	entry := func(ip string) string {
		return "      - endpoint:\n" +
			"          address:\n" +
			"            socket_address:\n" +
			"              address: " + ip + "\n" +
			"              port_value: 6443\n"
	}
	prev := -1
	for _, ip := range []string{"10.0.0.2", "10.0.0.3", "10.0.0.5"} {
		e := entry(ip)
		if !strings.Contains(configThree, e) {
			t.Fatalf("3-node config does not contain expected entry for %s\nconfig:\n%q", ip, configThree)
		}
		idx := strings.Index(configFive, e)
		if idx < 0 {
			t.Fatalf("5-node config changed the entry for %s\nconfig:\n%q", ip, configFive)
		}
		if idx < prev {
			t.Fatalf("5-node config reordered existing entries: entry for %s moved before an earlier entry\nconfig:\n%q", ip, configFive)
		}
		prev = idx
	}
}

// Dual-stack: the cluster may have nodes with IPv4 and IPv6 addresses;
// both must render correctly, with the IPv6 listener and dns family.
func TestConfigDualStack(t *testing.T) {
	t.Parallel()

	got := cdsConfigFor(t, true, map[string]string{
		"node-a": "10.0.0.1:6443",
		"node-b": "[fd00::2]:6443",
	})
	want := "\n" +
		"resources:\n" +
		"- \"@type\": type.googleapis.com/envoy.config.cluster.v3.Cluster\n" +
		"  name: kube_apiservers\n" +
		"  connect_timeout: 0.25s\n" +
		"  type: STRICT_DNS\n" +
		"  lb_policy: ROUND_ROBIN\n" +
		"  dns_lookup_family: AUTO\n" +
		"  health_checks:\n" +
		"  - timeout: 3s\n" +
		"    interval: 2s\n" +
		"    unhealthy_threshold: 2\n" +
		"    healthy_threshold: 1\n" +
		"    initial_jitter: 0s\n" +
		"    no_traffic_interval: 3s\n" +
		"    always_log_health_check_failures: true\n" +
		"    always_log_health_check_success: true\n" +
		"    event_log_path: /dev/stdout\n" +
		"    http_health_check:\n" +
		"      path: /healthz\n" +
		"    transport_socket_match_criteria:\n" +
		"      tls_mode: \"true\"\n" +
		"  transport_socket_matches:\n" +
		"  - name: \"health_check_tls\"\n" +
		"    match:\n" +
		"      tls_mode: \"true\"\n" +
		"    transport_socket:\n" +
		"      name: envoy.transport_sockets.tls\n" +
		"      typed_config:\n" +
		"        \"@type\": type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext\n" +
		"        common_tls_context:\n" +
		"          validation_context:\n" +
		"            trust_chain_verification: ACCEPT_UNTRUSTED\n" +
		"  load_assignment:\n" +
		"    cluster_name: kube_apiservers\n" +
		"    endpoints:\n" +
		"    - lb_endpoints:\n" +
		"      - endpoint:\n" +
		"          address:\n" +
		"            socket_address:\n" +
		"              address: 10.0.0.1\n" +
		"              port_value: 6443\n" +
		"      - endpoint:\n" +
		"          address:\n" +
		"            socket_address:\n" +
		"              address: fd00::2\n" +
		"              port_value: 6443\n"
	if got != want {
		t.Fatalf("Config() output mismatch\n got:\n%q\nwant:\n%q", got, want)
	}
}

// IPv6-only: bracketed IPv6 backends must be split and rendered correctly,
// and the listener must bind to "::".
func TestConfigIPv6Only(t *testing.T) {
	t.Parallel()

	gotCDS := cdsConfigFor(t, true, map[string]string{
		"node-a": "[fd00::1]:6443",
		"node-b": "[fd00::2]:6443",
	})
	for _, want := range []string{
		"dns_lookup_family: AUTO\n",
		"              address: fd00::1\n",
		"              address: fd00::2\n",
	} {
		if !strings.Contains(gotCDS, want) {
			t.Fatalf("IPv6 CDS config missing %q\nconfig:\n%q", want, gotCDS)
		}
	}

	gotLDS, err := Config(&ConfigData{
		ControlPlanePort: 6443,
		BackendServers: map[string]string{
			"node-a": "[fd00::1]:6443",
		},
		IPv6: true,
	}, ProxyLDSConfigTemplate)
	if err != nil {
		t.Fatalf("Config() unexpected error: %v", err)
	}
	wantLDS := "\n" +
		"resources:\n" +
		"- \"@type\": type.googleapis.com/envoy.config.listener.v3.Listener\n" +
		"  name: listener_apiserver\n" +
		"  address:\n" +
		"    socket_address:\n" +
		"      address: \"::\"\n" +
		"      port_value: 6443\n" +
		"  filter_chains:\n" +
		"  - filters:\n" +
		"    - name: envoy.filters.network.tcp_proxy\n" +
		"      typed_config:\n" +
		"        \"@type\": type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy\n" +
		"        stat_prefix: ingress_tcp\n" +
		"        cluster: kube_apiservers\n"
	if gotLDS != wantLDS {
		t.Fatalf("Config() LDS output mismatch\n got:\n%q\nwant:\n%q", gotLDS, wantLDS)
	}
}

// Two nodes resolving to the same address must fail generation, including
// addresses that are only textually different spellings of the same IP.
func TestConfigDuplicateAddresses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		backends map[string]string
	}{
		{
			name: "identical ipv4 address",
			backends: map[string]string{
				"node-a": "10.0.0.1:6443",
				"node-b": "10.0.0.1:6443",
			},
		},
		{
			name: "equivalent ipv6 spellings",
			backends: map[string]string{
				"node-a": "[2001:db8::1]:6443",
				"node-b": "[2001:0db8:0:0:0:0:0:1]:6443",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Config(&ConfigData{
				ControlPlanePort: 6443,
				BackendServers:   tt.backends,
			}, ProxyCDSConfigTemplate)
			if err == nil {
				t.Fatal("Config() error = nil, want duplicate address error")
			}
			if !strings.Contains(err.Error(), "same address") {
				t.Fatalf("Config() error = %q, want to contain %q", err.Error(), "same address")
			}
		})
	}
}

// An empty backend set must fail generation rather than produce a
// loadbalancer with no control-plane backends.
func TestConfigEmptyBackends(t *testing.T) {
	t.Parallel()

	_, err := Config(&ConfigData{
		ControlPlanePort: 6443,
		BackendServers:   map[string]string{},
	}, ProxyCDSConfigTemplate)
	if err == nil {
		t.Fatal("Config() error = nil, want empty backends error")
	}
	if !strings.Contains(err.Error(), "no backend servers") {
		t.Fatalf("Config() error = %q, want to contain %q", err.Error(), "no backend servers")
	}
}

func TestHostPortEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		a, b    string
		want    bool
		wantErr bool
	}{
		{name: "identical ipv4", a: "10.0.0.1:6443", b: "10.0.0.1:6443", want: true},
		{name: "ipv6 abbreviated vs full", a: "[2001:db8::1]:6443", b: "[2001:0db8:0:0:0:0:0:1]:6443", want: true},
		{name: "ipv4-mapped ipv6 vs ipv4", a: "[::ffff:10.0.0.1]:6443", b: "10.0.0.1:6443", want: true},
		{name: "hostname case insensitive", a: "NODE-A:6443", b: "node-a:6443", want: true},
		{name: "port with leading zero", a: "10.0.0.1:06443", b: "10.0.0.1:6443", want: true},
		{name: "different ipv4", a: "10.0.0.1:6443", b: "10.0.0.2:6443", want: false},
		{name: "different port", a: "10.0.0.1:6443", b: "10.0.0.1:6444", want: false},
		{name: "different hostnames", a: "node-a:6443", b: "node-b:6443", want: false},
		{name: "ipv4 vs ipv6", a: "10.0.0.1:6443", b: "[fd00::1]:6443", want: false},
		{name: "missing port", a: "10.0.0.1", b: "10.0.0.1:6443", wantErr: true},
		{name: "non-numeric port", a: "10.0.0.1:http", b: "10.0.0.1:6443", wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := HostPortEqual(tt.a, tt.b)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("HostPortEqual(%q, %q) error = nil, want error", tt.a, tt.b)
				}
				return
			}
			if err != nil {
				t.Fatalf("HostPortEqual(%q, %q) unexpected error: %v", tt.a, tt.b, err)
			}
			if got != tt.want {
				t.Fatalf("HostPortEqual(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
