// Package interceptors traces gRPC calls with gRPC interceptors instead of generated wrapper services.
//
// The OpenTelemetry Blueprint plugin can trace a service in two ways:
//
//   - Wrappers (opentelemetry.Instrument): generated code around the service that adds a traceCtx
//     argument and a retCtx return value to every method.  The trace context then travels as extra
//     fields of the gRPC request/response messages.
//
//   - Interceptors (opentelemetry.InstrumentGRPC, this package): the service interface is left alone.
//     A server interceptor and a client interceptor do the same work as the wrappers, and the
//     trace context travels in gRPC metadata instead:
//
//     forward context (span context + baggage):  request metadata  "bp-trace-ctx" (carrier.go)
//     reverse context (bridges' retCtx):          response trailer  "bp-ret-ctx"
//
// The interceptors are meant to behave exactly like the wrappers for the same bridge kind: same span
// names, same tracer names, same attributes, same values on the context for the span processors and
// for the client side of nested calls.  They also use the same context keys as the wrappers, so a
// service traced with wrappers (e.g. an HTTP service) can call a service traced with interceptors.
//
// The bridge kind (v, pb, cgpb, sb) is chosen when the interceptors are created.  It must match the
// span processor picked at run time through BRIDGE_KIND, exactly as the wrapper templates must.
//
// Files:
//
//	interceptors.go  constructors, the per-kind variant table, and what both sides use
//	                 (methodCache, mergeSpanBaggage)
//	carrier.go       the compact trace carrier (encodeCarrier, decodeCarrier, ...)
//	header.go        how the carrier and the reverse context are written as header text
//	server.go        the server interceptor and the helpers only it uses
//	client.go        the client interceptor and the helpers only it uses
package interceptors

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

// Metadata keys (gRPC metadata keys must be lower case).  Used by server.go and client.go.
const (
	// Request metadata: the forward trace carrier, in the compact form of carrier.go, as header text
	// (header.go).
	TraceCtxKey = "bp-trace-ctx"
	// Response trailer: the reverse context (bridges only), re-encoded as header text (header.go).
	RetCtxKey = "bp-ret-ctx"
)

// NewServerInterceptor returns the tracing server interceptor for the given bridge kind.
//
// tracer is the collector client (it provides the TracerProvider, like it does for the wrappers).
// kind is the value of OT_BRIDGE at build time; see [variantFor] for the accepted values.
//
// The tracer provider is fetched once, here: the collector clients always return the same provider
// (the wrappers fetch it on every call).
func NewServerInterceptor(ctx context.Context, tracer backend.Tracer, kind string) (grpc.UnaryServerInterceptor, error) {
	tp, err := tracer.GetTracerProvider(ctx)
	if err != nil {
		return nil, err
	}
	return serverInterceptor(tp, variantFor(kind), startAttributeBaggage(tracer)), nil // server.go; below
}

// NewClientInterceptor returns the tracing client interceptor for the given bridge kind.
// See [NewServerInterceptor] for the arguments.
func NewClientInterceptor(ctx context.Context, tracer backend.Tracer, kind string) (grpc.UnaryClientInterceptor, error) {
	tp, err := tracer.GetTracerProvider(ctx)
	if err != nil {
		return nil, err
	}
	return clientInterceptor(tp, variantFor(kind), startAttributeBaggage(tracer)), nil // client.go; below
}

// A variant is what one bridge kind adds on top of plain (vanilla) tracing.  The four wrapper
// templates differ from each other only in these points, so one server and one client interceptor
// with these switches cover all of them.
type variant struct {
	name string

	// countChildren: the server puts a child counter on ctx ("childCount"), and the client takes
	// the next child ordinal from it, putting it on ctx ("seqNum", read by the bridge processor when
	// the client span starts) and in the baggage ("__seq", read by the server on the other side).
	countChildren bool

	// childAttr: the attribute the server sets on its span after the call, reporting its children.
	childAttr childAttr

	// endEvents: the SB end-event list.  The server puts a list and a mutex on ctx ("endEvents",
	// "childrenMutex"); each client appends its child ordinal when its call returns; after the call
	// the server encodes the list into the "remEndEvents" attribute for the SB processor.
	endEvents bool
}

type childAttr int

const (
	noChildAttr     childAttr = iota // vanilla
	hasChildrenAttr                  // pb:          bool  "hasChildren"
	childCountAttr                   // cgpb and sb: int   "childCount"
)

// variantFor maps OT_BRIDGE values to variants, exactly as the wrapper templates are chosen in
// plugins/opentelemetry (ir_ot_server.go / ir_ot_client.go): "v"/"vanilla", "cgpb", "sb", and
// "pb"/"path"/"rc"/"" for the path bridge.  Tomislav-RetCtx: any other value is a broken invariant
// (it used to mean the path bridge silently); the template generator refuses it the same way at build
// time.  Called by the constructors, so a bad kind stops the process at startup.
func variantFor(kind string) variant {
	switch kind {
	case "v", "vanilla":
		return variant{name: "vanilla"}
	case "cgpb":
		return variant{name: "cgpb", countChildren: true, childAttr: childCountAttr}
	case "sb":
		return variant{name: "sb", countChildren: true, childAttr: childCountAttr, endEvents: true}
	case "pb", "path", "rc", "":
		return variant{name: "pb", countChildren: true, childAttr: hasChildrenAttr}
	}
	backend.InvariantBroken("OT_BRIDGE is one of v, vanilla, pb, path, rc, cgpb, sb or empty", "kind=", kind) // runtime/core/backend/invariant.go
	return variant{}
}

// What an interceptor needs for one gRPC method: the span name and the tracer to start the span
// with.  Both are worked out on the first call of the method and then reused (see methodCache).
//
// The wrappers do this work on every call: their span names are constants, but they ask the tracer
// provider for the tracer every time (tp.Tracer(name): in the OTel SDK a lock plus a map lookup;
// about 0.25 s of ComposePost's CPU in a 30 s profile at 800 req/s).  The SDK returns the same
// tracer for the same name every time, so caching it changes nothing but the cost.
type methodInfo struct {
	spanName string
	tracer   trace.Tracer
}

// The two sides name things differently; these match the wrapper templates exactly.
type side int

const (
	serverSpanNames side = iota // tracer "<Service>", span "<Service>Server_<Method>"
	clientSpanNames             // tracer "<Service>_OTServerWrapperInterface", span "<Service>Client_<Method>"
)

// A methodCache belongs to one interceptor.  Used by server.go and client.go.
type methodCache struct {
	tp      trace.TracerProvider
	side    side
	methods sync.Map // full gRPC method name -> *methodInfo
}

// get returns the methodInfo for a full method name such as "/grpc.UrlShortenService/ComposeUrls".
// The service part is the name of the Go interface Blueprint deployed (the proto package, "grpc.",
// is dropped).
func (c *methodCache) get(fullMethod string) *methodInfo {
	if m, ok := c.methods.Load(fullMethod); ok {
		return m.(*methodInfo)
	}
	service, method := splitMethod(fullMethod) // below
	actual, _ := c.methods.LoadOrStore(fullMethod, newMethodInfo(c.tp, c.side, service, method))
	return actual.(*methodInfo)
}

// newMethodInfo names the span and fetches the tracer for one method of a service, on one side.
// Used by methodCache.get and by the HTTP middleware (http_server.go, http_client.go).
func newMethodInfo(tp trace.TracerProvider, side side, service, method string) *methodInfo {
	// Tomislav-RetCtx: span and tracer names are built from both parts ("/pkg.Service/Method" for
	// gRPC, the service name and "/Method" for HTTP); once per method, the result is cached.
	if service == "" || method == "" {
		brokenValue("a traced method has a service name and a method name", "service/method", service+"/"+method) // invariants.go
	}
	var tracerName, spanName string
	if side == serverSpanNames {
		// The server wrapper names its tracer after the service.
		tracerName, spanName = service, service+"Server_"+method
	} else {
		// The client wrapper names its tracer after the interface it calls, which with wrappers
		// is "<Service>_OTServerWrapperInterface".  Odd, but kept so the traces stay identical.
		tracerName, spanName = service+"_OTServerWrapperInterface", service+"Client_"+method
	}
	return &methodInfo{spanName: spanName, tracer: tp.Tracer(tracerName)}
}

// splitMethod turns "/grpc.UrlShortenService/ComposeUrls" into ("UrlShortenService", "ComposeUrls").
// Only used by methodCache.get above.
func splitMethod(fullMethod string) (service, method string) {
	s := strings.TrimPrefix(fullMethod, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		service, method = s[:i], s[i+1:]
	} else {
		method = s
	}
	if i := strings.LastIndex(service, "."); i >= 0 {
		service = service[i+1:] // drop the proto package
	}
	return service, method
}

// startAttributeBaggage asks the tracer whether its SDK can add "__bag." attributes while a span
// starts (backend.AttributeBaggageReporter, runtime/core/backend/span_baggage.go).  A tracer that
// cannot say is assumed to.  The collector client (runtime/plugins/otelcol/trace.go) answers false
// for vanilla, SB and the ranged PB/CGPB windows.
func startAttributeBaggage(tracer backend.Tracer) bool {
	if r, ok := tracer.(backend.AttributeBaggageReporter); ok {
		return r.StartAttributeBaggage()
	}
	return true
}

// mergeSpanBaggage adds the baggage the span processor attached to a span that just started: first
// what the SDK keeps beside the span, then the span's "__bag.<key>" attributes (which win), and
// finally removes keys named by "__bagdel.<key>" attributes.  Reverse-path keys are skipped: the
// reverse context travels on the response only.  Identical to the loop in the wrapper templates.
// Used by server.go and client.go.
//
// attrBaggage false skips the attribute part: the span has only just started, so its attributes
// can only come from the SDK, and the tracer said its SDK adds no "__bag." ones (see
// startAttributeBaggage above).  Reading them costs a lock and a copy of the attribute list.
func mergeSpanBaggage(span trace.Span, baggage map[string]string, attrBaggage bool) {
	backend.AppendSpanBaggage(span, baggage) // runtime/core/backend/span_baggage.go
	if !attrBaggage {
		return
	}
	rwSpan, ok := span.(sdktrace.ReadWriteSpan)
	if !ok {
		return
	}
	for _, attr := range rwSpan.Attributes() {
		if backend.IsReverseBaggageKey(attr.Key) { // runtime/core/backend/checkpoint.go
			continue
		}
		if key, found := strings.CutPrefix(string(attr.Key), "__bag."); found {
			switch attr.Value.Type() {
			case attribute.INT64:
				baggage[key] = strconv.FormatInt(attr.Value.AsInt64(), 10)
			default:
				baggage[key] = attr.Value.AsString()
			}
		} else if key, found := strings.CutPrefix(string(attr.Key), "__bagdel."); found {
			delete(baggage, key)
		}
	}
}
