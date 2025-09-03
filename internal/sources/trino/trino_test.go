// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package trino

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/google/go-cmp/cmp"
	"github.com/googleapis/genai-toolbox/internal/server"
	"github.com/googleapis/genai-toolbox/internal/testutils"
)

func TestBuildTrinoDSN(t *testing.T) {
	tests := []struct {
		name            string
		host            string
		port            string
		user            string
		password        string
		catalog         string
		schema          string
		queryTimeout    string
		accessToken     string
		kerberosEnabled bool
		sslEnabled      bool
		want            string
		wantErr         bool
	}{
		{
			name:    "basic configuration",
			host:    "localhost",
			port:    "8080",
			user:    "testuser",
			catalog: "hive",
			schema:  "default",
			want:    "http://testuser@localhost:8080?catalog=hive&schema=default",
			wantErr: false,
		},
		{
			name:     "with password",
			host:     "localhost",
			port:     "8080",
			user:     "testuser",
			password: "testpass",
			catalog:  "hive",
			schema:   "default",
			want:     "http://testuser:testpass@localhost:8080?catalog=hive&schema=default",
			wantErr:  false,
		},
		{
			name:       "with SSL",
			host:       "localhost",
			port:       "8443",
			user:       "testuser",
			catalog:    "hive",
			schema:     "default",
			sslEnabled: true,
			want:       "https://testuser@localhost:8443?catalog=hive&schema=default",
			wantErr:    false,
		},
		{
			name:        "with access token",
			host:        "localhost",
			port:        "8080",
			user:        "testuser",
			catalog:     "hive",
			schema:      "default",
			accessToken: "jwt-token-here",
			want:        "http://testuser@localhost:8080?accessToken=jwt-token-here&catalog=hive&schema=default",
			wantErr:     false,
		},
		{
			name:            "with kerberos",
			host:            "localhost",
			port:            "8080",
			user:            "testuser",
			catalog:         "hive",
			schema:          "default",
			kerberosEnabled: true,
			want:            "http://testuser@localhost:8080?KerberosEnabled=true&catalog=hive&schema=default",
			wantErr:         false,
		},
		{
			name:         "with query timeout",
			host:         "localhost",
			port:         "8080",
			user:         "testuser",
			catalog:      "hive",
			schema:       "default",
			queryTimeout: "30m",
			want:         "http://testuser@localhost:8080?catalog=hive&queryTimeout=30m&schema=default",
			wantErr:      false,
		},
		{
			name:    "anonymous access (empty user)",
			host:    "localhost",
			port:    "8080",
			catalog: "hive",
			schema:  "default",
			want:    "http://localhost:8080?catalog=hive&schema=default",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildTrinoDSN(tt.host, tt.port, tt.user, tt.password, tt.catalog, tt.schema, tt.queryTimeout, tt.accessToken, tt.kerberosEnabled, tt.sslEnabled)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildTrinoDSN() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("buildTrinoDSN() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseFromYamlTrino(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
		want server.SourceConfigs
	}{
		{
			desc: "basic example",
			in: `
			sources:
				my-trino-instance:
					kind: trino
					host: localhost
					port: "8080"
					user: testuser
					catalog: hive
					schema: default
			`,
			want: server.SourceConfigs{
				"my-trino-instance": Config{
					Name:    "my-trino-instance",
					Kind:    SourceKind,
					Host:    "localhost",
					Port:    "8080",
					User:    "testuser",
					Catalog: "hive",
					Schema:  "default",
				},
			},
		},
		{
			desc: "example with optional fields",
			in: `
			sources:
				my-trino-instance:
					kind: trino
					host: localhost
					port: "8443"
					user: testuser
					password: testpass
					catalog: hive
					schema: default
					queryTimeout: "30m"
					accessToken: "jwt-token-here"
					kerberosEnabled: true
					sslEnabled: true
			`,
			want: server.SourceConfigs{
				"my-trino-instance": Config{
					Name:            "my-trino-instance",
					Kind:            SourceKind,
					Host:            "localhost",
					Port:            "8443",
					User:            "testuser",
					Password:        "testpass",
					Catalog:         "hive",
					Schema:          "default",
					QueryTimeout:    "30m",
					AccessToken:     "jwt-token-here",
					KerberosEnabled: true,
					SSLEnabled:      true,
				},
			},
		},
		{
			desc: "anonymous access without user",
			in: `
			sources:
				my-trino-anonymous:
					kind: trino
					host: localhost
					port: "8080"
					catalog: hive
					schema: default
			`,
			want: server.SourceConfigs{
				"my-trino-anonymous": Config{
					Name:    "my-trino-anonymous",
					Kind:    SourceKind,
					Host:    "localhost",
					Port:    "8080",
					Catalog: "hive",
					Schema:  "default",
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			got := struct {
				Sources server.SourceConfigs `yaml:"sources"`
			}{}
			// Parse contents
			err := yaml.Unmarshal(testutils.FormatYaml(tc.in), &got)
			if err != nil {
				t.Fatalf("unable to unmarshal: %s", err)
			}
			if !cmp.Equal(tc.want, got.Sources) {
				t.Fatalf("incorrect parse: want %v, got %v", tc.want, got.Sources)
			}
		})
	}
}
