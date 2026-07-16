package main

import "testing"

func TestSignInURL(t *testing.T) {
	cases := []struct {
		name  string
		base  string
		token string
		want  string
	}{
		{"empty base disables the link", "", "lyc_abc", ""},
		{"whitespace base disables the link", "   ", "lyc_abc", ""},
		{
			"builds the redemption link",
			"http://192.168.1.9:8080",
			"lyc_abc",
			"http://192.168.1.9:8080/sign-in?token=lyc_abc",
		},
		{
			"trims a trailing slash on the base",
			"http://host/",
			"lyc_abc",
			"http://host/sign-in?token=lyc_abc",
		},
		{
			"query-escapes the token",
			"http://host",
			"lyc_a+b/c",
			"http://host/sign-in?token=lyc_a%2Bb%2Fc",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := signInURL(tc.base, tc.token); got != tc.want {
				t.Errorf("signInURL(%q, %q) = %q, want %q", tc.base, tc.token, got, tc.want)
			}
		})
	}
}
