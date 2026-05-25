// Package linuxgen implements code generation for the goproc plugin.
package linuxgen

import (
	"os"

	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/plugins/linuxcontainer/linuxgen"
)

// Compile-time env-var hooks for baking Go runtime tuning into the
// generated run.sh script (matches the per-instance compose env-var
// injection in dockercompose/dockergen). Unset → nothing emitted;
// set → an `export VAR=${VAR:-<value>}` line is prepended inside the
// `run_<svc>_proc` function so manual `./run.sh` invocations and direct
// docker exec both pick up the tuning.
const (
	BlueprintGCIntervalEnv = "BLUEPRINT_GC_INTERVAL_SEC"
	BlueprintGOGCEnv       = "BLUEPRINT_GOGC"
)

/*
Generates command-line function to run a goproc
*/
func GenerateRunFunc(procName string, args ...ir.IRNode) (string, error) {
	templateArgs := newRunFuncTemplateArgs(procName, args)
	return linuxgen.ExecuteTemplate("goproc_runfunc", runFuncTemplate, templateArgs)
}

/*
Generates command-line function to run a goproc that has been built to a binary
using `go build`
*/
func GenerateBinaryRunFunc(procName string, args ...ir.IRNode) (string, error) {
	templateArgs := newRunFuncTemplateArgs(procName, args)
	return linuxgen.ExecuteTemplate("goproc_binaryrunfunc", binaryRunFuncTemplate, templateArgs)
}

type runFuncTemplateArgs struct {
	Name          string
	Args          []ir.IRNode
	GCIntervalSec string // "" => omit export
	GOGC          string // "" => omit export
}

func newRunFuncTemplateArgs(procName string, args []ir.IRNode) runFuncTemplateArgs {
	return runFuncTemplateArgs{
		Name:          procName,
		Args:          args,
		GCIntervalSec: os.Getenv(BlueprintGCIntervalEnv),
		GOGC:          os.Getenv(BlueprintGOGCEnv),
	}
}

var binaryRunFuncTemplate = `
run_{{RunFuncName .Name}} {
	{{- if .GCIntervalSec}}
	export GC_INTERVAL_SEC=${GC_INTERVAL_SEC:-{{.GCIntervalSec}}}
	{{- end}}
	{{- if .GOGC}}
	export GOGC=${GOGC:-{{.GOGC}}}
	{{- end}}
	cd {{.Name}}
    ./{{.Name}}
	{{- range $i, $arg := .Args}} --{{$arg.Name}}=${{EnvVarName $arg.Name}}{{end}} &
	{{EnvVarName .Name}}=$!
	return $?
}`

var runFuncTemplate = `
run_{{RunFuncName .Name}} {
	export CGO_ENABLED=1
	{{- if .GCIntervalSec}}
	export GC_INTERVAL_SEC=${GC_INTERVAL_SEC:-{{.GCIntervalSec}}}
	{{- end}}
	{{- if .GOGC}}
	export GOGC=${GOGC:-{{.GOGC}}}
	{{- end}}
	cd {{.Name}}/{{.Name}}
	go run .
	{{- range $i, $arg := .Args}} --{{$arg.Name}}=${{EnvVarName $arg.Name}}{{end}} &
	{{EnvVarName .Name}}=$!
	return $?
}`
