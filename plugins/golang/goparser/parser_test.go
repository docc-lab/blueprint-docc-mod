package goparser

import (
	"os"
	"path/filepath"
	"testing"
)

func writeParserFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module blueprint-parser-fixture\n\ngo 1.23.6\n"
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParserIgnoresTestFilesDuringServiceDiscovery(t *testing.T) {
	dir := writeParserFixture(t, map[string]string{
		"service.go": `package fixture
type Service interface { Call() }
type Impl struct{}
func (*Impl) Call() {}
`,
		// Invalid test syntax must not break the production-source parser.
		"service_test.go": "package fixture\nthis is not production Go\n",
	})
	module, err := parseModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	pkg := module.Packages["blueprint-parser-fixture"]
	if pkg == nil || pkg.Structs["Impl"] == nil || pkg.Structs["Impl"].Methods["Call"] == nil {
		t.Fatal("production service methods were lost")
	}
	if len(pkg.Files) != 1 || pkg.Interfaces["Service"] == nil {
		t.Fatal("test files entered service discovery")
	}
}
