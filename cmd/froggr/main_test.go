package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAwsRegion(t *testing.T) {
	tests := []struct {
		name          string
		awsRegion     string
		defaultRegion string
		want          string
	}{
		{"prefers AWS_REGION", "us-west-2", "eu-west-1", "us-west-2"},
		{"falls back to AWS_DEFAULT_REGION", "", "eu-west-1", "eu-west-1"},
		{"empty when neither set", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AWS_REGION", tt.awsRegion)
			t.Setenv("AWS_DEFAULT_REGION", tt.defaultRegion)
			assert.Equal(t, tt.want, awsRegion())
		})
	}
}
