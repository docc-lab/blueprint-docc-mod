package payloadbench

import (
	"context"
	"fmt"
)

// EdgeService is the entry point. A client calls Call with two sizes:
//   - reqSize: forward-path payload the edge generates and sends to the internal service
//   - resSize: return-path payload the internal service sends back
//
// The client request itself carries only the two sizes (it stays tiny), so the
// measured cost is purely the edge<->internal payload in each direction. The
// edge returns only an OK/not-OK signal: nil error = OK; non-nil error = not OK
// (mapped to HTTP 500 by the Blueprint http plugin).
type EdgeService interface {
	Call(ctx context.Context, reqSize int64, resSize int64) error
}

// EdgeServiceImpl implements EdgeService.
type EdgeServiceImpl struct {
	EdgeService
	internal InternalService
}

// NewEdgeServiceImpl is the Blueprint constructor; it depends on InternalService.
func NewEdgeServiceImpl(ctx context.Context, internal InternalService) (EdgeService, error) {
	return &EdgeServiceImpl{internal: internal}, nil
}

func (e *EdgeServiceImpl) Call(ctx context.Context, reqSize int64, resSize int64) error {
	req := makePayload(reqSize)
	resp, err := e.internal.Echo(ctx, req, resSize)
	if err != nil {
		return err
	}
	if int64(len(resp)) != resSize {
		return fmt.Errorf("not ok: internal returned %d bytes, want %d", len(resp), resSize)
	}
	return nil
}

