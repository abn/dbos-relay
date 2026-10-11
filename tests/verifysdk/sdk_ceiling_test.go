package verifysdk_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Supported SDK ceiling, as recorded in docs/usage/supported-sdks.md.
//
// Upstream moves the Conductor connector out of the public SDK packages and
// into proprietary ones. Past this bound Relay can no longer read the wire
// protocol it implements, so a sample application pinned above the ceiling
// would quietly leave the supported window. This test fails when that happens.
//
// Evidence for each bound, all confirmed 2026-10-11:
//
//	Go         tag v1.6.0, commit 04337c15667adcdf5cc057be448ed9545d6f06d9 (2026-10-07),
//	           proxy.golang.org @latest v1.6.0 at 2026-10-07T16:06:46Z; the tag still
//	           carries dbos/conductor.go and dbos/conductor_protocol.go.
//	Python     tag 3.2.0, commit d324156616281d7d4923bc60851617bdbb70ea5e (2026-09-28),
//	           PyPI upload 2026-09-29T15:43:49; the release still ships dbos/_conductor/.
//	TypeScript tag v5.2, commit 3f36908f58fd8b3079cbf5372203c7a2d0ba06cc (2026-09-29),
//	           npm publish 2026-09-29T15:48:15.957Z at version 5.2.11, the latest
//	           dist-tag; only 5.3.x preview and test builds are newer.
//	Java       no ceiling: origin/main 85b3f0ed0458b72358fb7554bc20931bd61e7897
//	           still carries the public connector package, so nothing is capped yet.
//	           make watch/java-connector reports when that changes.
var sdkCeilings = map[string]string{
	"go":         "1.6.0",
	"python":     "3.2.0",
	"typescript": "5.2.11",
	"java":       "",
}

const examplesDir = "../../examples"

// compareVersions compares two dot-separated version strings. A pre-release
// suffix such as "-m7" sorts below the same version without one. It returns
// -1, 0, or 1, and an error when either string is not a version, so that a
// garbled pin fails loudly instead of reading as an old one.
func compareVersions(a, b string) (int, error) {
	coreA, preA, okA := splitVersion(a)
	coreB, preB, okB := splitVersion(b)
	if !okA {
		return 0, fmt.Errorf("%q is not a version", a)
	}
	if !okB {
		return 0, fmt.Errorf("%q is not a version", b)
	}
	for i := 0; i < len(coreA) || i < len(coreB); i++ {
		var x, y int
		if i < len(coreA) {
			x = coreA[i]
		}
		if i < len(coreB) {
			y = coreB[i]
		}
		if x != y {
			if x < y {
				return -1, nil
			}
			return 1, nil
		}
	}
	return strings.Compare(preA, preB), nil
}

// splitVersion splits a version into its numeric core and its pre-release
// suffix. It reports false when any core segment is not a number, which is how
// a malformed pin stays a failure rather than becoming a low version.
func splitVersion(v string) (core []int, pre string, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if at := strings.IndexByte(v, '-'); at >= 0 {
		pre = v[at+1:]
		v = v[:at]
	}
	if at := strings.IndexByte(v, '+'); at >= 0 {
		v = v[:at]
	}
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, "", false
		}
		core = append(core, n)
	}
	return core, pre, true
}

// readExample reads a file under examples/ and fails the test when it is
// missing, because a missing manifest means the ceiling is no longer checked.
func readExample(t *testing.T, rel string) string {
	t.Helper()
	path := filepath.Join(examplesDir, rel)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v (the supported SDK ceiling cannot be verified without it)", path, err)
	}
	return string(data)
}

func goExamplePin(t *testing.T) string {
	t.Helper()
	mod := readExample(t, "golang/go.mod")
	re := regexp.MustCompile(`(?m)^\s*github\.com/dbos-inc/dbos-transact-golang\s+(v[0-9][^\s]*)`)
	m := re.FindStringSubmatch(mod)
	if m == nil {
		t.Fatalf("examples/golang/go.mod does not pin github.com/dbos-inc/dbos-transact-golang")
	}
	return m[1]
}

func typescriptExamplePin(t *testing.T) string {
	t.Helper()
	pkg := readExample(t, "typescript/package.json")
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(pkg), &manifest); err != nil {
		t.Fatalf("examples/typescript/package.json is not valid JSON: %v", err)
	}
	pin, ok := manifest.Dependencies["@dbos-inc/dbos-sdk"]
	if !ok {
		t.Fatalf("examples/typescript/package.json does not depend on @dbos-inc/dbos-sdk")
	}
	return strings.TrimPrefix(pin, "^")
}

func pythonExamplePin(t *testing.T) string {
	t.Helper()
	script := readExample(t, "python/main.py")
	re := regexp.MustCompile(`(?m)^[ \t#]*"dbos==([0-9][^"]*)"`)
	m := re.FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("examples/python/main.py does not pin a dbos version in its PEP 723 metadata block")
	}
	return m[1]
}

func javaExamplePin(t *testing.T) string {
	t.Helper()
	pom := readExample(t, "java/pom.xml")
	re := regexp.MustCompile(`(?s)<dependency>\s*<groupId>dev\.dbos</groupId>\s*<artifactId>transact</artifactId>\s*<version>([^<]+)</version>`)
	m := re.FindStringSubmatch(pom)
	if m == nil {
		t.Fatalf("examples/java/pom.xml does not pin dev.dbos:transact")
	}
	return strings.TrimSpace(m[1])
}

// TestExampleSDKPinsWithinCeiling fails when a sample application is pinned to
// an SDK version above the supported ceiling. It reads the example manifests
// only, so it runs offline and fast, and it is part of `make test`.
func TestExampleSDKPinsWithinCeiling(t *testing.T) {
	pins := map[string]string{
		"go":         goExamplePin(t),
		"python":     pythonExamplePin(t),
		"typescript": typescriptExamplePin(t),
		"java":       javaExamplePin(t),
	}

	for _, language := range []string{"go", "python", "typescript", "java"} {
		pin := pins[language]
		ceiling := sdkCeilings[language]

		if ceiling == "" {
			t.Logf("%s: pinned %s, no ceiling applies (the Java SDK still publishes the public Conductor connector)", language, pin)
			continue
		}

		order, err := compareVersions(pin, ceiling)
		if err != nil {
			t.Errorf("%s: cannot compare example pin %q against the ceiling %q: %v", language, pin, ceiling, err)
			continue
		}
		if order > 0 {
			t.Errorf("%s: example pin %s is above the supported ceiling %s; "+
				"a newer SDK has moved the Conductor connector out of the public package, "+
				"so bump the recorded ceiling in docs/usage/supported-sdks.md and the "+
				"constants above, or pin back at or below the ceiling",
				language, pin, ceiling)
			continue
		}
		t.Logf("%s: pinned %s, at or below the supported ceiling %s", language, pin, ceiling)
	}
}

// TestSDKCeilingsAreParsable guards the constants themselves: a malformed bound
// would make every comparison against it meaningless.
func TestSDKCeilingsAreParsable(t *testing.T) {
	for language, ceiling := range sdkCeilings {
		if ceiling == "" {
			continue
		}
		core, pre, ok := splitVersion(ceiling)
		if !ok || len(core) == 0 {
			t.Fatalf("%s ceiling %q is not a version the comparison understands", language, ceiling)
		}
		if pre != "" {
			t.Fatalf("%s ceiling %q carries a pre-release suffix, which cannot be a support bound", language, ceiling)
		}
		t.Logf("%s ceiling %s is a version the comparison understands", language, ceiling)
	}
}
