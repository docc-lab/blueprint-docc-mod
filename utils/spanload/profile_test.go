package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

func testConfig(t *testing.T, args ...string) config {
	t.Helper()
	c, err := parseConfig(args, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func testProfile(t *testing.T, c config) profile {
	t.Helper()
	p, err := loadProfile(c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func tempJSON(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProfilesOnProtobufWire(t *testing.T) {
	for _, base := range []string{"zero", "semconv10-example"} {
		for _, bridge := range []string{"none", "pb", "cgpb", "sb"} {
			t.Run(base+"/"+bridge, func(t *testing.T) {
				args := []string{"--profile", base, "--bridge", bridge, "--batch-size", "8", "--cpd", "3", "--depth", "300", "--ordinal", "129"}
				if bridge != "none" {
					args = append(args, "--bridge-bytes", "25")
				}
				c := testConfig(t, args...)
				p := testProfile(t, c)
				b := newBatchBuilder(c, p, [8]byte{1})
				original, checkpoints := b.build(0, 8)
				wire, err := proto.Marshal(original)
				if err != nil {
					t.Fatal(err)
				}
				req := &collector.ExportTraceServiceRequest{}
				if err := proto.Unmarshal(wire, req); err != nil {
					t.Fatal(err)
				}
				rs := req.ResourceSpans[0]
				scope := rs.ScopeSpans[0]
				if rs.Resource != nil || scope.Scope != nil {
					t.Fatal("hidden resource/scope metadata")
				}
				wantBase := 0
				if base == "semconv10-example" {
					wantBase = 10
				}
				wantCP := uint64(0)
				if bridge != "none" {
					wantCP = 3
				}
				if checkpoints != wantCP {
					t.Fatalf("checkpoints %d, want %d", checkpoints, wantCP)
				}
				for i, s := range scope.Spans {
					if len(s.TraceId) != 16 || len(s.SpanId) != 8 || len(s.ParentSpanId) != 8 || binary.BigEndian.Uint64(s.SpanId) != uint64(i+1) {
						t.Fatal("invalid IDs")
					}
					if s.EndTimeUnixNano <= s.StartTimeUnixNano || s.Name != "" || len(s.Events) != 0 || len(s.Links) != 0 || s.Status != nil {
						t.Fatal("unexpected baseline fields")
					}
					want := wantBase
					if bridge != "none" {
						want++
					}
					if len(s.Attributes) != want {
						t.Fatalf("attributes=%d want=%d", len(s.Attributes), want)
					}
					if bridge == "none" {
						continue
					}
					a := s.Attributes[want-1]
					value, ok := a.Value.Value.(*commonpb.AnyValue_BytesValue)
					if !ok {
						t.Fatalf("metadata uses %T", a.Value.Value)
					}
					if i%3 == 0 {
						if a.Key != "_br" || len(value.BytesValue) != 25 {
							t.Fatal("checkpoint shape")
						}
					} else {
						data := value.BytesValue
						if bridge == "sb" {
							if a.Key != "_o" {
								t.Fatal("SB key")
							}
							ordinal, n := binary.Uvarint(data)
							if ordinal != 129 || n <= 0 {
								t.Fatal("ordinal")
							}
							data = data[n:]
						} else if a.Key != "_d" {
							t.Fatal("depth key")
						}
						depth, n := binary.Uvarint(data)
						if depth != 300 || n != len(data) {
							t.Fatal("depth varint")
						}
					}
				}
				// Reusing a shorter batch must not send stale tail spans.
				next, _ := b.build(8, 1)
				if len(next.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
					t.Fatal("stale spans in reused batch")
				}
			})
		}
	}
}

func TestCustomAttributesAndSizes(t *testing.T) {
	file := tempJSON(t, `[{"key":"s","type":"string","value":"value"},{"key":"i","type":"int","value":9223372036854775807},{"key":"f","type":"double","value":1.25},{"key":"b","type":"bool","value":true},{"key":"raw","type":"bytes","value":"AP+A"}]`)
	sizes := tempJSON(t, `{"source":"test fixture","sizes":{"cgpb":{"6":37}}}`)
	c := testConfig(t, "--profile", "custom", "--attributes-file", file, "--resource-attributes-file", file, "--bridge", "cgpb", "--sizes-file", sizes)
	p := testProfile(t, c)
	if len(p.attrs) != 5 || len(p.resource) != 5 || p.attrs[1].Value.GetIntValue() != 9223372036854775807 || !bytes.Equal(p.attrs[4].Value.GetBytesValue(), []byte{0, 255, 128}) {
		t.Fatal("custom typed values changed")
	}
	if p.CheckpointBytes != 37 || p.SizeSource != "test fixture" {
		t.Fatal("size table not applied")
	}
	if !bytes.Equal(p.checkpoint.Value.GetBytesValue(), testProfile(t, c).checkpoint.Value.GetBytesValue()) {
		t.Fatal("payload seed not reproducible")
	}
	c.Seed++
	if bytes.Equal(p.checkpoint.Value.GetBytesValue(), testProfile(t, c).checkpoint.Value.GetBytesValue()) {
		t.Fatal("seed ignored")
	}
	c.CPD = 8
	if _, err := loadProfile(c); err == nil {
		t.Fatal("missing CPD size accepted")
	}
}

func TestInvalidInputs(t *testing.T) {
	for _, args := range [][]string{
		{"--rates", "NaN"}, {"--rates", "-1"}, {"--rates", "Inf"}, {"--rates", "1,"},
		{"--duration", "0s"}, {"--workers", "0"}, {"--batch-size", "0"}, {"--cpd", "257"},
		{"--profile", "custom"}, {"--attributes-file", "x"}, {"--bridge", "nope"},
		{"--bridge-bytes", "20"}, {"--bridge", "pb", "--bridge-bytes", "20", "--sizes-file", "x"},
		{"--metrics-url", "http://localhost:8888/metrics"}, {"--protocol", "http", "--insecure"},
		{"--endpoint", "localhost"}, {"--endpoint", "localhost:4317", "--endpoint", "localhost:4317"},
	} {
		if _, err := parseConfig(args, io.Discard); err == nil {
			t.Errorf("accepted invalid args %v", args)
		}
	}
	for _, contents := range []string{
		`[{"key":"x","type":"string","value":null}]`,
		`[{"key":"x","type":"int","value":1.5}]`,
		`[{"key":"x","type":"string","value":1}]`,
		`[{"key":"x","type":"bytes","value":"%%%"}]`,
		`[{"key":"_br","type":"string","value":"x"}]`,
		`[{"key":"x","type":"bool","value":true},{"key":"x","type":"bool","value":false}]`,
		`[{"key":"x","type":"string","value":"x","typo":1}]`,
		`[] {}`, `{"key":"x"}`,
	} {
		c := testConfig(t, "--profile", "custom", "--attributes-file", tempJSON(t, contents))
		if _, err := loadProfile(c); err == nil {
			t.Errorf("accepted invalid profile %s", contents)
		}
	}
	c := testConfig(t, "--bridge", "pb")
	if _, err := loadProfile(c); err == nil {
		t.Fatal("missing bridge size accepted")
	}
	c = testConfig(t, "--bridge", "pb", "--sizes-file", tempJSON(t, `{"sizes":{"pb":{"6":20}}}`))
	if _, err := loadProfile(c); err == nil {
		t.Fatal("missing source accepted")
	}
}

func TestDryRunDoesNotConnect(t *testing.T) {
	var out, errOut bytes.Buffer
	code := execute(t.Context(), []string{"--dry-run", "--endpoint", "127.0.0.1:1", "--insecure", "--profile", "zero", "--bridge", "sb", "--bridge-bytes", "17"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	d := json.NewDecoder(&out)
	var m map[string]any
	if err := d.Decode(&m); err != nil || m["type"] != "manifest" {
		t.Fatal("missing manifest", err)
	}
	if err := d.Decode(&m); err != nil || m["type"] != "sample" {
		t.Fatal("missing sample", err)
	}
}
