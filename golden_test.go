package appmeta

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/golden")

type goldenFixture struct {
	name  string
	build func(t testing.TB) []byte
}

// goldenFixtures lists every synthetic app whose full output is pinned in
// testdata/golden/<name>.json.
func goldenFixtures() []goldenFixture {
	return []goldenFixture{
		{"apk-minimal", buildMinimalAPK},
		{"apk-acme-shop", buildAcmeShopAPK},
		{"apk-adaptive-icon", buildAdaptiveIconAPK},
		{"ipa-acme-shop", buildAcmeShopIPA},
		{"ipa-simulator", buildSimulatorIPA},
	}
}

func TestEveryFixtureMatchesItsGoldenFileAndTheSchema(t *testing.T) {
	schema := compileOutputSchema(t)
	for _, fixture := range goldenFixtures() {
		t.Run(fixture.name, func(t *testing.T) {
			info := mustParse(t, fixture.build(t))
			assertMatchesSchema(t, schema, info)
			assertMatchesGoldenFile(t, fixture.name, info)
		})
	}
}

func compileOutputSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile("schema/appmeta.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("appmeta.schema.json", doc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("appmeta.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func assertMatchesSchema(t *testing.T, schema *jsonschema.Schema, info *Info) {
	t.Helper()
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(doc); err != nil {
		t.Errorf("output does not match schema: %v\n%s", err, encoded)
	}
}

// assertMatchesGoldenFile compares everything except the icon bytes, which
// depend on the Go version's PNG encoder; icon pixels are checked by
// dedicated tests.
func assertMatchesGoldenFile(t *testing.T, name string, info *Info) {
	t.Helper()
	pinned := *info
	if pinned.Icon != nil {
		icon := *pinned.Icon
		icon.PNG = nil
		icon.SHA256 = ""
		pinned.Icon = &icon
	}
	got, err := json.MarshalIndent(pinned, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", "golden", name+".json")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from %s (run with -update and review the diff)\ngot:\n%s", path, got)
	}
}
