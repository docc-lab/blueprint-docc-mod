// Package burstload provides a Blueprint modifier for generating fixed interval burst load on service calls.
//
// The plugin wraps clients with a burst loader that generates multiple concurrent requests
// at fixed intervals for a specified duration.
//
// Usage:
//
//	import "github.com/blueprint-uservices/blueprint/plugins/burstload"
//	 burstload.AddBurstLoad(spec, "my_service", 5, 10, "30s") // 5 concurrent requests every 10ms for 30 seconds
package burstload

import (
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/blueprint"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/coreplugins/pointer"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/ir"
	"github.com/blueprint-uservices/blueprint/blueprint/pkg/wiring"
	"github.com/blueprint-uservices/blueprint/plugins/golang"
	"golang.org/x/exp/slog"
)

// Add burst generation for the specified service.
func AddBurstLoad(spec wiring.WiringSpec, serviceName string, burst_size int64, burst_duration string, burst_interval string) {
	clientWrapper := serviceName + ".client.burstloader"

	ptr := pointer.GetPointer(spec, serviceName)
	if ptr == nil {
		slog.Error("Unable to add burst load to " + serviceName + " as it is not a pointer")
		return
	}

	clientNext := ptr.AddSrcModifier(spec, clientWrapper)

	spec.Define(clientWrapper, &BurstLoadGenerator{BurstSize: burst_size, BurstDuration: burst_duration, BurstInterval: burst_interval}, func(ns wiring.Namespace) (ir.IRNode, error) {
		var wrapped golang.Service

		if err := ns.Get(clientNext, &wrapped); err != nil {
			return nil, blueprint.Errorf("BurstLoad %s expected %s to be a golang.Service, but encountered %s", clientWrapper, clientNext, err)
		}

		return newBurstLoadGenerator(clientWrapper, wrapped, burst_size, burst_duration, burst_interval)
	})
}


