package einoagui_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestParityGoldenFixtures(t *testing.T) {
	tests := []struct {
		name      string
		pkg       string
		testNames []string
	}{
		{
			name: "convert",
			pkg:  "./convert",
			testNames: []string{
				"TestToEinoMessagesMatchesNormalizedGoldenFixture",
				"TestMessageTextMatchesNormalizedGoldenFixture",
				"TestToEinoImagePartMatchesNormalizedGoldenFixture",
				"TestToolCallsMatchNormalizedGoldenFixture",
				"TestToAGUIMessagesMatchesNormalizedGoldenFixture",
			},
		},
		{
			name:      "emitter",
			pkg:       "./emitter",
			testNames: []string{"TestMessagesSnapshotMatchesNormalizedGoldenFixture"},
		},
		{
			name:      "stream",
			pkg:       "./stream",
			testNames: []string{"TestStreamTurnMatchesNormalizedGoldenFixture"},
		},
		{
			name:      "tools",
			pkg:       "./tools",
			testNames: []string{"TestToolBindingMatchesNormalizedGoldenFixture"},
		},
		{
			name: "golden fixture contracts",
			pkg:  "./internal/golden",
			testNames: []string{
				"TestGoldenFixtureFilesAreNormalized",
				"TestGoldenFixtureContracts",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := strings.Join(tt.testNames, "|")
			cmd := exec.Command("go", "test", "-json", tt.pkg, "-run", "^("+pattern+")$", "-count=1")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", strings.Join(cmd.Args, " "), err, out)
			}
			assertTestsPassed(t, out, tt.testNames)
		})
	}
}

func assertTestsPassed(t *testing.T, output []byte, testNames []string) {
	t.Helper()
	want := make(map[string]bool, len(testNames))
	for _, name := range testNames {
		want[name] = false
	}

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var event testEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode go test JSON event: %v\n%s", err, output)
		}
		if event.Action == "pass" {
			if _, ok := want[event.Test]; ok {
				want[event.Test] = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read go test JSON output: %v", err)
	}

	var missing []string
	for name, passed := range want {
		if !passed {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("child parity tests did not pass or did not run: %s\n%s", strings.Join(missing, ", "), output)
	}
}

type testEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
}
