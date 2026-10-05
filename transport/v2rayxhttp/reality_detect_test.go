package v2rayxhttp

import "testing"

type RealityClientConfig struct{}

type UTLSClientConfig struct{}

type KTLSClientConfig struct {
	Config any
}

func TestTypeIsReality(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want bool
	}{
		{"nil", nil, false},
		{"reality", &RealityClientConfig{}, true},
		{"utls", &UTLSClientConfig{}, false},
		{"ktls-wrapping-reality", &KTLSClientConfig{Config: &RealityClientConfig{}}, true},
		{"ktls-wrapping-utls", &KTLSClientConfig{Config: &UTLSClientConfig{}}, false},
		{"ktls-wrapping-nil", &KTLSClientConfig{Config: nil}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := typeIsReality(tc.in, 0); got != tc.want {
				t.Fatalf("typeIsReality(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
