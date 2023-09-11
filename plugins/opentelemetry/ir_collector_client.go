package opentelemetry

import (
	"bytes"
	"fmt"
	"text/template"

	"gitlab.mpi-sws.org/cld/blueprint/blueprint/pkg/blueprint"
	"gitlab.mpi-sws.org/cld/blueprint/plugins/golang"
)

type OpenTelemetryCollectorClient struct {
	golang.Node
	golang.Instantiable

	ClientName string
	ServerAddr *OpenTelemetryCollectorAddr
}

func newOpenTelemetryCollectorClient(name string, addr blueprint.IRNode) (*OpenTelemetryCollectorClient, error) {
	addrNode, is_addr := addr.(*OpenTelemetryCollectorAddr)
	if !is_addr {
		return nil, fmt.Errorf("unable to create OpenTelemetryCollectorClient node because %s is not an address", addr.Name())
	}

	node := &OpenTelemetryCollectorClient{}
	node.ClientName = name
	node.ServerAddr = addrNode
	return node, nil
}

func (node *OpenTelemetryCollectorClient) Name() string {
	return node.ClientName
}

func (node *OpenTelemetryCollectorClient) String() string {
	return node.Name() + " = OTClient(" + node.ServerAddr.Name() + ")"
}

var collectorClientBuildFuncTemplate = `func(ctr golang.Container) (any, error) {

		// TODO: generated OT collector client constructor

		return nil, nil

	}`

func (node *OpenTelemetryCollectorClient) AddInstantiation(builder golang.GraphBuilder) error {
	// Only generate instantiation code for this instance once
	if builder.Visited(node.ClientName) {
		return nil
	}

	// TODO: generate the OT wrapper code

	// Instantiate the code template
	t, err := template.New(node.ClientName).Parse(collectorClientBuildFuncTemplate)
	if err != nil {
		return err
	}

	// Generate the code
	buf := &bytes.Buffer{}
	err = t.Execute(buf, node)
	if err != nil {
		return err
	}

	return builder.Declare(node.ClientName, buf.String())
}

func (node *OpenTelemetryCollectorClient) ImplementsGolangNode() {}