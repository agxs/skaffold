package util

import (
	"testing"

	"github.com/GoogleContainerTools/skaffold/testutil"
)

func TestHumanReadableBytesSizeIEC(t *testing.T) {
	tests := []struct {
		description string
		bytesSize   int64
		expected    string
	}{
		{
			description: "68993024 -> 66MB",
			bytesSize:   int64(68993024),
			expected:    "65.8 MiB",
		},
	}
	for _, test := range tests {
		testutil.Run(t, test.description, func(t *testutil.T) {
			got := HumanReadableBytesSizeIEC(test.bytesSize)

			t.CheckDeepEqual(test.expected, got)
		})
	}
}
