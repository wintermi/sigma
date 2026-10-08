// Copyright (c) 2026 Matthew Winter
//
// This source code is licensed under the MIT license found in the LICENSE file
// in the root directory of this source tree.

package bedrock

import "testing"

func TestValidateContainerCredentialsURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		uri   string
		valid bool
	}{
		{uri: "https://credentials.example.com/role", valid: true},
		{uri: "http://127.0.0.1:8080/creds", valid: true},
		{uri: "http://127.1.2.3/creds", valid: true},
		{uri: "http://localhost/creds", valid: true},
		{uri: "http://[::1]/creds", valid: true},
		{uri: "http://169.254.170.2/v2/credentials", valid: true},
		{uri: "http://169.254.170.23/v1/credentials", valid: true},
		{uri: "http://[fd00:ec2::23]/v1/credentials", valid: true},
		{uri: "http://credentials.example.com/role", valid: false},
		{uri: "http://169.254.169.254/latest", valid: false},
		{uri: "http://127.0.0.1.example.com/creds", valid: false},
		{uri: "file:///etc/passwd", valid: false},
		{uri: "://bad", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			t.Parallel()

			err := validateContainerCredentialsURI(tt.uri)
			if (err == nil) != tt.valid {
				t.Fatalf("validateContainerCredentialsURI(%q) error = %v, want valid %v", tt.uri, err, tt.valid)
			}
		})
	}
}
