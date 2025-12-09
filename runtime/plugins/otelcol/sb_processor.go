// Package otelcol implements a tracer [backend.Tracer] client interface for the OpenTelemetry collector.
package otelcol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bits-and-blooms/bloom"
	"github.com/blueprint-uservices/blueprint/runtime/core/backend"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

type StructuralBridgeProcessor struct {
	mu sync.RWMutex

	// OTLP gRPC client for sending custom protobuf messages
	client otlptrace.Client

	// Configuration
	agentEndpoint string
	ancestryMode  AncestryMode

	bloomFilter *bloom.BloomFilter

	delayedEndEvents []string
	deeLock          sync.Mutex

	// Background processing
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Metrics for monitoring
	eventsSent int64

	// Buffer for batch export
	eventsBuf  []*tracepb.ResourceSpans
	eventsLock sync.Mutex

	// AI_ADDED: Removed serverSideSpans map and ssLock - now using hasChildren attribute instead

	// Config discovery
	configDiscoveryPort int
	httpClient          *http.Client
	configMap           map[string]interface{}
	configLock          sync.RWMutex

	// Checkpoint distance (parsed from config, default: 1)
	checkpointDistance int64
}

// Darby: this gets run once per service (when initialized)
func NewStructuralBridgeProcessor(ctx context.Context, agentEndpoint string, configDiscoveryPort string) (*StructuralBridgeProcessor, error) {
	slog.Info("🔵 Creating new StructuralBridgeProcessor", "agentEndpoint", agentEndpoint)

	bloomFilter := bloom.New(10, 7)

	// Extract host from agent endpoint
	var host string
	if strings.Contains(agentEndpoint, ":") {
		parts := strings.Split(agentEndpoint, ":")
		if len(parts) >= 2 {
			host = parts[0]
		}
	}
	if host == "" {
		host = "localhost" // Fallback to localhost
	}

	// Create endpoint
	endpoint := fmt.Sprintf("%s:4317", host)

	slog.Info("🔵 Using endpoint", "endpoint", endpoint)

	// Create OTLP gRPC client
	client := otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)

	slog.Info("🔵 OTLP client created, starting connection")

	// Start the client
	if err := client.Start(ctx); err != nil {
		slog.Error("❌ Failed to start OTLP client", "error", err)
		return nil, fmt.Errorf("failed to start OTLP client: %w", err)
	}

	slog.Info("✅ StructuralBridgeProcessor created successfully")

	// Resolve ancestry mode from environment (default: bloom)
	// mode := AncestryMode(os.Getenv("ANCESTRY_MODE"))
	// if mode == "" {
	// 	// mode = AncestryModeBloom
	// 	mode = AncestryModeHash
	// }

	// Parse config discovery port
	configDiscoveryPortInt, err := strconv.Atoi(configDiscoveryPort)
	if err != nil {
		slog.Error("❌ Failed to convert configDiscoveryPort to int", "error", err)
		return nil, fmt.Errorf("failed to convert configDiscoveryPort to int: %w", err)
	}

	// Create HTTP client for config discovery
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	processor := &StructuralBridgeProcessor{
		client:              client,
		agentEndpoint:       agentEndpoint,
		bloomFilter:         bloomFilter,
		stopChan:            make(chan struct{}),
		ancestryMode:        AncestryModePB,
		configDiscoveryPort: configDiscoveryPortInt,
		httpClient:          httpClient,
		configMap:           make(map[string]interface{}),
		checkpointDistance:  1, // Default: every span is a checkpoint
		delayedEndEvents:    make([]string, 0),
		deeLock:             sync.Mutex{},
	}

	slog.Info("🔵 Ancestry mode configured", "mode", AncestryModePB)

	// Fetch full config from config discovery endpoint
	slog.Info("🔵 About to fetch full config")
	if err := processor.fetchFullConfig(ctx); err != nil {
		slog.Error("❌ Failed to fetch full config", "error", err)
		// Don't fail initialization if config fetch fails - continue with empty config
		slog.Warn("⚠️ Continuing with empty config map")
	} else {
		slog.Info("🟢 Successfully fetched full config", "config_keys", len(processor.configMap))
	}

	// Start background worker for batch export
	processor.wg.Add(1)
	go processor.processEvents()

	return processor, nil
}

// processEvents runs in the background to periodically send events
func (p *StructuralBridgeProcessor) processEvents() {
	defer p.wg.Done()

	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			// Send any remaining buffered events before shutting down
			p.flushBuffer()
			return
		case <-ticker.C:
			// Send buffered events
			go p.flushBuffer()
		}
	}
}

// flushBuffer sends all buffered events
func (p *StructuralBridgeProcessor) flushBuffer() {
	// Get events from buffer and reset the buffer
	p.eventsLock.Lock()
	events := p.eventsBuf
	p.eventsBuf = make([]*tracepb.ResourceSpans, 0, len(p.eventsBuf)) // Reset length, keep capacity
	p.eventsLock.Unlock()

	if len(events) > 0 {
		if err := p.sendData(events); err != nil {
			slog.Error("Failed to send events", "error", err, "count", len(events))
		} else {
			slog.Debug("Successfully sent events", "count", len(events))
			p.eventsSent += int64(len(events))
		}
	}
}

// sendData sends data to the endpoint
func (p *StructuralBridgeProcessor) sendData(events []*tracepb.ResourceSpans) error {
	if len(events) == 0 {
		return nil
	}

	slog.Debug("🔵 Sending data", "count", len(events))

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := p.client.UploadTraces(ctx, events)
	if err != nil {
		slog.Error("❌ Failed to send data", "error", err, "count", len(events))
		return fmt.Errorf("failed to send data: %w", err)
	}

	slog.Debug("✅ Data sent successfully", "count", len(events))
	return nil
}

// OnStart implements SpanProcessor.OnStart
func (p *StructuralBridgeProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	// No mutex needed - checkpointDistance and ancestryMode are read-only after initialization
	slog.Debug("🔵 StructuralBridgeProcessor OnStart called", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	parentSpan := trace.SpanFromContext(parent)

	// if s.SpanKind() == trace.SpanKindServer {
	// 	totalSpanID := s.SpanContext().TraceID().String() + ":" + s.SpanContext().SpanID().String()
	// 	// AI_ADDED: No longer need to initialize map entry - using hasChildren attribute instead
	// 	s.SetAttributes(attribute.String("selfTotalID", totalSpanID))
	// } else {
	// 	slog.Info("🔵 Client-side span", "span_name", s.Name())
	// 	parentTotalSpanID := parentSpan.SpanContext().TraceID().String() + ":" + parentSpan.SpanContext().SpanID().String()
	// 	// AI_ADDED: No longer need map-based counting - server template sets hasChildren attribute via context
	// 	s.SetAttributes(attribute.String("parentTotalID", parentTotalSpanID))
	// }

	// Handle depth tracking in baggage
	depth := 0
	var hashArrayStr string
	var endEventsStr string
	var delayedEndEventsStr string

	baggage := backend.GetBaggageFromContext(parent)

	if baggage != nil {
		if depthStr, exists := baggage["depth"]; exists {
			if depthInt, err := strconv.Atoi(depthStr); err == nil {
				depth = depthInt + 1
			}
		}

		hashArrayStr = baggage["ha"]
		endEventsStr = baggage["ee"]
		delayedEndEventsStr = baggage["dee"]
	} else {
		hashArrayStr = ""
		endEventsStr = ""
		delayedEndEventsStr = ""
	}

	depth %= int(p.checkpointDistance)
	s.SetAttributes(attribute.Int("__bag.depth", depth))

	// // Handle bloom filter in baggage (same pattern as depth)
	// // var bloomFilter *bloom.BloomFilter

	// // Check if bloom filter exists in baggage (same as depth)
	// if baggage != nil {
	// 	// if bfStr, exists := baggage["bf"]; exists {
	// 	// 	// Deserialize existing bloom filter (like depth parsing)
	// 	// 	bloomFilter, err = deserializeBloomFilter(bfStr)
	// 	// 	if err != nil {
	// 	// 		slog.Warn("Failed to deserialize existing bloom filter, creating new one", "error", err)
	// 	// 		bloomFilter = createEmptyBloomFilter()
	// 	// 	}
	// 	// } else {
	// 	// 	// Create new bloom filter if none exists (like depth = 0)
	// 	// 	bloomFilter = createEmptyBloomFilter()
	// 	// }
	// 	bloomFilter, err = deserializeBloomFilter(baggage["bf"])
	// 	if err != nil {
	// 		slog.Warn("Failed to deserialize existing bloom filter, creating new one", "error", err)
	// 	}
	// } else {
	// 	// Create new bloom filter if no baggage (like depth = 0)
	// 	bloomFilter = createEmptyBloomFilter()
	// }

	// Add current span ID to bloom filter (like depth + 1)
	spanID := s.SpanContext().SpanID().String()

	seqNum := -1
	var ok bool

	if seqNum, ok = parent.Value("seqNum").(int); !ok {
		seqNum = 1
	}

	hashArrayStr += "," + spanID + ":" + strconv.Itoa(seqNum) + ":" + strconv.Itoa(depth)

	if endEvents, ok := parent.Value("endEvents").([]string); ok {
		for _, endEvent := range endEvents {
			endEventsStr += "," + endEvent
		}
	}

	p.deeLock.Lock()
	if len(p.delayedEndEvents) > 0 {
		delayedEndEvents := p.delayedEndEvents[:]
		p.delayedEndEvents = make([]string, 0)
		for _, delayedEndEvent := range delayedEndEvents {
			delayedEndEventsStr += "," + delayedEndEvent
		}
	}
	p.deeLock.Unlock()

	isRoot := !parentSpan.SpanContext().IsValid()
	slog.Debug("🔵 Updated hash array for span", "span_id", spanID, "is_root", isRoot)

	// // Serialize updated bloom filter and set baggage attribute for propagation
	// bfStr, err := serializeBloomFilter(bloomFilter)
	// if err != nil {
	// 	slog.Error("Failed to serialize bloom filter", "error", err)
	// 	return
	// }
	s.SetAttributes(attribute.String(BAG_HASH_ARRAY, hashArrayStr))
	s.SetAttributes(attribute.String(BAG_END_EVENTS, endEventsStr))
	s.SetAttributes(attribute.String(BAG_DELAYED_END_EVENTS, delayedEndEventsStr))

	// Assign priority based on checkpoint distance (cpd) from config
	// High priority (1) if depth % cpd == 0 (root span at depth 0 is always checkpointed)
	// Low priority (0) otherwise
	priority := 0

	// Get checkpoint distance (already parsed and stored, no lock needed for read)
	// cpd := p.checkpointDistance

	// // Calculate priority: high priority if depth % cpd == 0
	// if cpd > 0 && depth%int(cpd) == 0 {
	// 	priority = 1
	// }
	if depth == 0 {
		priority = 1
	}

	// Set priority as baggage attribute for propagation
	s.SetAttributes(attribute.Int("__bag.prio", priority))

	// Write ancestry tags (only ancestry_mode and ancestry payload)
	// ancestryPayload := ""
	// switch p.ancestryMode {
	// case AncestryModeBloom:
	ancestryPayload := hashArrayStr + ";" + endEventsStr
	// case AncestryModeHash:
	// 	ancestryPayload = hashArrayStr
	// case AncestryModeHybrid:
	// 	// TODO: implement hybrid encoder later
	// 	ancestryPayload = ""
	// default:
	// 	ancestryPayload = bfStr
	// }

	// Always set ancestry data - will be stripped for low-priority spans in convertAttributes
	s.SetAttributes(attribute.String(AncestryModeKey, string(p.ancestryMode)))
	s.SetAttributes(attribute.String(AncestryKey, ancestryPayload))
	if delayedEndEventsStr != "" {
		s.SetAttributes(attribute.String(AncestryExtraKey, delayedEndEventsStr))
	}

	// Reset bloom filter and hash array after each checkpoint (only for high priority)
	if priority == 1 {
		// hashArrayStr = spanID
		s.SetAttributes(attribute.String(BAG_HASH_ARRAY, spanID))
	}
	// else {
	// 	s.SetAttributes(attribute.String("prio", "low"))
	// }

	// Add depth attribute for verification (not baggage)
	s.SetAttributes(attribute.Int("depth", depth))

	slog.Debug("🔵 Set priority baggage and attribute", "priority", priority, "depth", depth, "span_name", s.Name())
}

// OnEnd implements SpanProcessor.OnEnd
func (p *StructuralBridgeProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	// No mutex needed - only reading span attributes and routing to buffers (which have their own locks)
	slog.Debug("🔴 StructuralBridgeProcessor OnEnd called", "span_name", s.Name(), "trace_id", s.SpanContext().TraceID())

	// Extract priority from span attributes
	var priority int
	var hasPriority bool
	var depth int
	var hasDepth bool
	var hasChildren bool
	var remEndEvents string

	// Iterate through attributes to find __bag.prio, __bag.depth, and hasChildren
	for _, attr := range s.Attributes() {
		if attr.Key == "__bag.prio" {
			val := attr.Value.AsInt64()
			priority = int(val)
			hasPriority = true
		} else if attr.Key == "__bag.depth" {
			val := attr.Value.AsInt64()
			depth = int(val)
			hasDepth = true
		} else if attr.Key == "childCount" {
			// AI_ADDED: Check for hasChildren attribute to determine if server span is a leaf
			hasChildren = attr.Value.AsInt64() > 0
		} else if attr.Key == "remEndEvents" {
			remEndEvents = attr.Value.AsString()
			if remEndEvents != "" {
				p.deeLock.Lock()
				p.delayedEndEvents = append(p.delayedEndEvents, s.SpanContext().TraceID().String()+"::"+remEndEvents)
				p.deeLock.Unlock()
			}
		}
	}

	if s.SpanKind() == trace.SpanKindServer {
		// AI_ADDED: Use hasChildren attribute instead of map-based counting
		if hasChildren {
			// Non-leaf server span - force to low priority (priority = 0)
			slog.Info("🔵 Non-leaf server span (hasChildren=true)", "span_name", s.Name())
			priority += 0
		} else {
			// Leaf server span - always checkpoint (priority = 1)
			slog.Info("🔵 Leaf server span (hasChildren=false or missing)", "span_name", s.Name())
			priority = 1
		}
	}

	// if !hasPriority && priority == 0 {
	if !hasPriority {
		// Default to low priority if no priority found
		priority = 0
		slog.Debug("🔴 No priority found, defaulting to low priority", "span_name", s.Name())
	}
	if !hasDepth {
		depth = 0
		slog.Debug("🔴 No depth found, defaulting to 0", "span_name", s.Name())
	}

	slog.Debug("🔴 Routing span based on priority",
		"priority", priority,
		"depth", depth,
		"span_name", s.Name(),
		"trace_id", s.SpanContext().TraceID(),
		"span_id", s.SpanContext().SpanID())

	// Route span to pipeline
	p.routeToPipeline(s, priority == 1)

	// Note: All baggage attributes (including __bag.prio and __bag.bloom_filter)
	// are now exported as span attributes for analysis and debugging.
}

// routeToPipeline adds the span to the buffer
func (p *StructuralBridgeProcessor) routeToPipeline(s sdktrace.ReadOnlySpan, highPriority bool) {
	// Convert span to ResourceSpans and add to buffer
	resourceSpans := p.createResourceSpans(s, highPriority)

	p.eventsLock.Lock()
	p.eventsBuf = append(p.eventsBuf, resourceSpans)
	p.eventsLock.Unlock()

	slog.Debug("🔴 Routed to pipeline", "span_name", s.Name(), "buffer_size", len(p.eventsBuf))
}

// createResourceSpans converts a ReadOnlySpan to ResourceSpans protobuf format
func (p *StructuralBridgeProcessor) createResourceSpans(s sdktrace.ReadOnlySpan, highPriority bool) *tracepb.ResourceSpans {
	// Get trace and span IDs as byte arrays
	traceID := s.SpanContext().TraceID()
	spanID := s.SpanContext().SpanID()

	// Create span with trace ID, span ID, start time, end time, and attributes
	spanProto := &tracepb.Span{
		TraceId:           traceID[:], // Convert [16]byte to []byte
		SpanId:            spanID[:],  // Convert [8]byte to []byte
		StartTimeUnixNano: uint64(s.StartTime().UnixNano()),
		EndTimeUnixNano:   uint64(s.EndTime().UnixNano()),
		Name:              s.Name(),
		Kind:              p.convertSpanKind(s.SpanKind()),
		Status:            p.convertStatus(s.Status()),
		Attributes:        p.convertAttributes(s.Attributes(), highPriority),
		Events:            p.convertEvents(s.Events()),
		Links:             p.convertLinks(s.Links()),
	}

	// Add parent span ID if exists
	if s.Parent().IsValid() {
		parentSpanID := s.Parent().SpanID()
		spanProto.ParentSpanId = parentSpanID[:] // Convert [8]byte to []byte
	}

	// Get resource from the span and convert to protobuf format
	resourceProto := p.convertResourceToProto(s.Resource())

	return &tracepb.ResourceSpans{
		Resource: resourceProto,
		ScopeSpans: []*tracepb.ScopeSpans{
			{
				Scope: &commonpb.InstrumentationScope{
					Name:    s.InstrumentationScope().Name,
					Version: s.InstrumentationScope().Version,
				},
				Spans: []*tracepb.Span{spanProto},
			},
		},
	}
}

// convertResourceToProto converts an OpenTelemetry resource to protobuf format
func (p *StructuralBridgeProcessor) convertResourceToProto(resource interface{}) *resourcepb.Resource {
	if resource == nil {
		return &resourcepb.Resource{}
	}

	// Try to get the resource's iterator
	var iter attribute.Iterator
	if r, ok := resource.(interface{ Iter() attribute.Iterator }); ok {
		iter = r.Iter()
	} else {
		// Fallback to empty resource
		return &resourcepb.Resource{}
	}

	// Convert attributes using the iterator
	attrs := p.convertAttributeIterator(iter)

	return &resourcepb.Resource{
		Attributes: attrs,
	}
}

// convertAttributeIterator converts an attribute iterator to protobuf format
func (p *StructuralBridgeProcessor) convertAttributeIterator(iter attribute.Iterator) []*commonpb.KeyValue {
	if iter.Len() == 0 {
		return nil
	}

	attrs := make([]*commonpb.KeyValue, 0, iter.Len())
	for iter.Next() {
		attr := iter.Attribute()
		attrs = append(attrs, p.convertAttribute(attr))
	}
	return attrs
}

// convertAttribute converts a single attribute to protobuf format
func (p *StructuralBridgeProcessor) convertAttribute(kv attribute.KeyValue) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   string(kv.Key),
		Value: p.convertAttributeValue(kv.Value),
	}
}

// convertAttributeValue converts an attribute value to protobuf format
func (p *StructuralBridgeProcessor) convertAttributeValue(v attribute.Value) *commonpb.AnyValue {
	av := new(commonpb.AnyValue)
	switch v.Type() {
	case attribute.STRING:
		av.Value = &commonpb.AnyValue_StringValue{
			StringValue: v.AsString(),
		}
	case attribute.INT64:
		av.Value = &commonpb.AnyValue_IntValue{
			IntValue: v.AsInt64(),
		}
	case attribute.FLOAT64:
		av.Value = &commonpb.AnyValue_DoubleValue{
			DoubleValue: v.AsFloat64(),
		}
	case attribute.BOOL:
		av.Value = &commonpb.AnyValue_BoolValue{
			BoolValue: v.AsBool(),
		}
	case attribute.BOOLSLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertBoolSlice(v.AsBoolSlice()),
			},
		}
	case attribute.INT64SLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertInt64Slice(v.AsInt64Slice()),
			},
		}
	case attribute.FLOAT64SLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertFloat64Slice(v.AsFloat64Slice()),
			},
		}
	case attribute.STRINGSLICE:
		av.Value = &commonpb.AnyValue_ArrayValue{
			ArrayValue: &commonpb.ArrayValue{
				Values: p.convertStringSlice(v.AsStringSlice()),
			},
		}
	default:
		// Fallback to string representation
		av.Value = &commonpb.AnyValue_StringValue{
			StringValue: fmt.Sprintf("%v", v.AsInterface()),
		}
	}
	return av
}

// Helper functions for slice conversions
func (p *StructuralBridgeProcessor) convertBoolSlice(slice []bool) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_BoolValue{BoolValue: v},
		}
	}
	return values
}

func (p *StructuralBridgeProcessor) convertInt64Slice(slice []int64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_IntValue{IntValue: v},
		}
	}
	return values
}

func (p *StructuralBridgeProcessor) convertFloat64Slice(slice []float64) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_DoubleValue{DoubleValue: v},
		}
	}
	return values
}

func (p *StructuralBridgeProcessor) convertStringSlice(slice []string) []*commonpb.AnyValue {
	values := make([]*commonpb.AnyValue, len(slice))
	for i, v := range slice {
		values[i] = &commonpb.AnyValue{
			Value: &commonpb.AnyValue_StringValue{StringValue: v},
		}
	}
	return values
}

// convertSpanKind converts OpenTelemetry span kind to protobuf format
func (p *StructuralBridgeProcessor) convertSpanKind(kind trace.SpanKind) tracepb.Span_SpanKind {
	switch kind {
	case trace.SpanKindInternal:
		return tracepb.Span_SPAN_KIND_INTERNAL
	case trace.SpanKindServer:
		return tracepb.Span_SPAN_KIND_SERVER
	case trace.SpanKindClient:
		return tracepb.Span_SPAN_KIND_CLIENT
	case trace.SpanKindProducer:
		return tracepb.Span_SPAN_KIND_PRODUCER
	case trace.SpanKindConsumer:
		return tracepb.Span_SPAN_KIND_CONSUMER
	default:
		return tracepb.Span_SPAN_KIND_UNSPECIFIED
	}
}

// convertStatus converts OpenTelemetry span status to protobuf format
func (p *StructuralBridgeProcessor) convertStatus(status sdktrace.Status) *tracepb.Status {
	return &tracepb.Status{
		Code:    tracepb.Status_StatusCode(status.Code),
		Message: status.Description,
	}
}

// convertAttributes converts span attributes to protobuf format, including all baggage attributes
func (p *StructuralBridgeProcessor) convertAttributes(attrs []attribute.KeyValue, highPriority bool) []*commonpb.KeyValue {
	if len(attrs) == 0 {
		return nil
	}

	// Include all attributes (including baggage attributes)
	protoAttrs := make([]*commonpb.KeyValue, len(attrs))
	for i, attr := range attrs {
		if strings.HasPrefix(string(attr.Key), "__bag.") {
			continue
		}
		switch attr.Key {
		case "depth", "hasChildren", "childCount", "remEndEvents":
			continue
		}
		if highPriority {
			protoAttrs[i] = p.convertAttribute(attr)
		} else {
			switch attr.Key {
			case AncestryKey, AncestryModeKey:
				continue
			}
			protoAttrs[i] = p.convertAttribute(attr)
		}
	}
	return protoAttrs
}

// convertEvents converts span events to protobuf format
func (p *StructuralBridgeProcessor) convertEvents(events []sdktrace.Event) []*tracepb.Span_Event {
	if len(events) == 0 {
		return nil
	}

	protoEvents := make([]*tracepb.Span_Event, len(events))
	for i, event := range events {
		protoEvents[i] = &tracepb.Span_Event{
			TimeUnixNano: uint64(event.Time.UnixNano()),
			Name:         event.Name,
			Attributes:   p.convertAttributes(event.Attributes, true), // This will include all attributes
		}
	}
	return protoEvents
}

// convertLinks converts span links to protobuf format
func (p *StructuralBridgeProcessor) convertLinks(links []sdktrace.Link) []*tracepb.Span_Link {
	if len(links) == 0 {
		return nil
	}

	protoLinks := make([]*tracepb.Span_Link, len(links))
	for i, link := range links {
		traceID := link.SpanContext.TraceID()
		spanID := link.SpanContext.SpanID()

		protoLinks[i] = &tracepb.Span_Link{
			TraceId:    traceID[:],
			SpanId:     spanID[:],
			Attributes: p.convertAttributes(link.Attributes, true), // This will include all attributes
		}
	}
	return protoLinks
}

// getConfigDiscoveryEndpoint converts the agent endpoint to the config discovery endpoint
func (p *StructuralBridgeProcessor) getConfigDiscoveryEndpoint() string {
	// Extract host from agent endpoint
	if strings.Contains(p.agentEndpoint, ":") {
		parts := strings.Split(p.agentEndpoint, ":")
		if len(parts) >= 2 {
			host := parts[0]
			// Use configurable port for config discovery (same host, different port)
			return fmt.Sprintf("%s:%d", host, p.configDiscoveryPort)
		}
	}
	// Fallback to localhost with configurable port
	return fmt.Sprintf("localhost:%d", p.configDiscoveryPort)
}

// parseCheckpointDistance extracts and parses the checkpoint distance (cpd) from the config map
func (p *StructuralBridgeProcessor) parseCheckpointDistance(config map[string]interface{}) int64 {
	const defaultCPD = int64(1) // Default: every span is a checkpoint

	if config == nil {
		return defaultCPD
	}

	if cpdVal, exists := config["cpd"]; exists {
		// Handle different possible types for cpd (int, int64, float64, string)
		switch v := cpdVal.(type) {
		case int64:
			if v > 0 {
				return v
			}
			slog.Warn("cpd must be positive, using default", "cpd", v)
			return defaultCPD
		case int:
			if v > 0 {
				return int64(v)
			}
			slog.Warn("cpd must be positive, using default", "cpd", v)
			return defaultCPD
		case float64:
			cpd := int64(v)
			if cpd > 0 && float64(cpd) == v {
				return cpd
			}
			slog.Warn("cpd must be a positive integer, using default", "cpd", v)
			return defaultCPD
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err == nil {
				if parsed > 0 {
					return parsed
				}
				slog.Warn("cpd must be positive, using default", "cpd", v)
				return defaultCPD
			}
			slog.Warn("Failed to parse cpd as int64, using default", "cpd", v, "error", err)
			return defaultCPD
		default:
			slog.Warn("cpd has unexpected type, using default", "cpd", v, "type", fmt.Sprintf("%T", v))
			return defaultCPD
		}
	}

	// cpd not found in config, use default
	return defaultCPD
}

// fetchFullConfig fetches the full config from the config discovery endpoint
func (p *StructuralBridgeProcessor) fetchFullConfig(ctx context.Context) error {
	// Try to fetch config from the discovery endpoint with retries
	config, err := p.fetchFullConfigFromEndpointWithRetries(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch full config: %w", err)
	}

	// Parse checkpoint distance from config
	cpd := p.parseCheckpointDistance(config)

	// Log the full config as JSON
	configJSON, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		slog.Warn("Failed to marshal config to JSON for logging", "error", err)
	} else {
		slog.Info("Fetched config JSON", "config", string(configJSON))
	}

	p.configLock.Lock()
	p.configMap = config
	p.checkpointDistance = cpd
	p.configLock.Unlock()

	slog.Info("Successfully discovered full config",
		"config_keys", len(config),
		"checkpoint_distance", cpd)
	return nil
}

// fetchFullConfigFromEndpointWithRetries fetches config from the discovery endpoint with retries
func (p *StructuralBridgeProcessor) fetchFullConfigFromEndpointWithRetries(ctx context.Context) (map[string]interface{}, error) {
	configDiscoveryEndpoint := p.getConfigDiscoveryEndpoint()
	url := fmt.Sprintf("http://%s/getFullConfig", configDiscoveryEndpoint)

	// Retry loop with 1-second intervals
	for attempt := 1; attempt <= 60; attempt++ { // Max 60 attempts (60 seconds)
		slog.Debug("Attempting config discovery", "attempt", attempt, "endpoint", url)

		// Create a new request for each attempt
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP request: %w", err)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			slog.Debug("Config discovery attempt failed, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to make HTTP request after %d attempts: %w", attempt, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			slog.Debug("Config discovery endpoint returned non-OK status, will retry", "attempt", attempt, "status", resp.StatusCode)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("config discovery endpoint returned status %d after %d attempts", resp.StatusCode, attempt)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Debug("Failed to read response body, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to read response body after %d attempts: %w", attempt, err)
		}

		var configResp ConfigResponse
		if err := json.Unmarshal(body, &configResp); err != nil {
			slog.Debug("Failed to parse config response, will retry", "attempt", attempt, "error", err)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("failed to parse config response after %d attempts: %w", attempt, err)
		}

		if configResp.Config == nil {
			slog.Debug("Empty config in response, will retry", "attempt", attempt)
			if attempt < 30 {
				time.Sleep(1 * time.Second)
				continue
			}
			return nil, fmt.Errorf("empty config in response after %d attempts", attempt)
		}

		// Success! Return the config
		slog.Info("Config discovery successful", "attempt", attempt, "config_keys", len(configResp.Config))
		return configResp.Config, nil
	}

	return nil, fmt.Errorf("config discovery failed after 60 attempts")
}

// getConfigMap returns the config map, with thread-safe access
func (p *StructuralBridgeProcessor) getConfigMap() map[string]interface{} {
	p.configLock.RLock()
	defer p.configLock.RUnlock()

	// Return a copy to prevent external modification
	result := make(map[string]interface{})
	for k, v := range p.configMap {
		result[k] = v
	}
	return result
}

// Shutdown implements SpanProcessor.Shutdown
func (p *StructuralBridgeProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	slog.Info("🔴 StructuralBridgeProcessor shutting down")

	// Stop the background workers
	close(p.stopChan)
	p.wg.Wait()

	// Stop the client
	if err := p.client.Stop(ctx); err != nil {
		slog.Error("❌ Failed to stop client", "error", err)
	}

	slog.Info("✅ StructuralBridgeProcessor shutdown complete",
		"eventsSent", p.eventsSent)
	return nil
}

// ForceFlush implements SpanProcessor.ForceFlush
func (p *StructuralBridgeProcessor) ForceFlush(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// No ForceFlush needed for OTLP client; nothing to do
	return nil
}
