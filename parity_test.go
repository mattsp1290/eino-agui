package einoagui_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestParityGoldenFixtures(t *testing.T) {
	tests := []struct {
		name    string
		pkg     string
		pattern string
	}{
		{
			name: "convert",
			pkg:  "./convert",
			pattern: strings.Join([]string{
				"TestToEinoMessagesMatchesNormalizedGoldenFixture",
				"TestMessageTextMatchesNormalizedGoldenFixture",
				"TestToEinoImagePartMatchesNormalizedGoldenFixture",
				"TestToolCallsMatchNormalizedGoldenFixture",
				"TestToAGUIMessagesMatchesNormalizedGoldenFixture",
			}, "|"),
		},
		{
			name:    "emitter",
			pkg:     "./emitter",
			pattern: "TestMessagesSnapshotMatchesNormalizedGoldenFixture",
		},
		{
			name:    "stream",
			pkg:     "./stream",
			pattern: "TestStreamTurnMatchesNormalizedGoldenFixture",
		},
		{
			name:    "tools",
			pkg:     "./tools",
			pattern: "TestToolBindingMatchesNormalizedGoldenFixture",
		},
		{
			name: "golden fixture contracts",
			pkg:  "./internal/golden",
			pattern: strings.Join([]string{
				"TestGoldenFixtureFilesAreNormalized",
				"TestGoldenFixtureContracts",
			}, "|"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("go", "test", tt.pkg, "-run", "^("+tt.pattern+")$", "-count=1")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", strings.Join(cmd.Args, " "), err, out)
			}
			t.Logf("%s", out)
		})
	}
}
