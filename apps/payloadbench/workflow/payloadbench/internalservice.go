// Package payloadbench is a two-service Blueprint application for measuring the
// effect of inter-service payload size on throughput and response time. The
// EdgeService is the entry point; it forwards a caller-specified number of bytes
// to the InternalService and asks for a caller-specified number of bytes back.
//
// The two directions are controlled independently (reqSize = forward path,
// A->B; resSize = return path, B->A) so the forward- vs return-path payload
// asymmetry can be characterized separately.
package payloadbench

import "context"

// InternalService is the downstream service. It receives the forward-path
// payload and returns a return-path payload of the requested size.
type InternalService interface {
	// Echo receives `payload` (the forward-path bytes, already paid for on the
	// wire) and returns `resSize` bytes on the return path. It never inspects
	// the payload contents.
	Echo(ctx context.Context, payload []byte, resSize int64) ([]byte, error)
}

// InternalServiceImpl implements InternalService.
type InternalServiceImpl struct {
	InternalService
}

// NewInternalServiceImpl is the Blueprint constructor for the internal service.
func NewInternalServiceImpl(ctx context.Context) (InternalService, error) {
	return &InternalServiceImpl{}, nil
}

func (s *InternalServiceImpl) Echo(ctx context.Context, payload []byte, resSize int64) ([]byte, error) {
	// payload has already traversed the wire (forward-path cost paid). Return
	// resSize bytes on the return path.
	return makePayload(resSize), nil
}

