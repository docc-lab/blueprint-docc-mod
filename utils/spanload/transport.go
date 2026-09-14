package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type sender interface {
	Export(context.Context, *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, int, error)
	Close() error
}

type exportError struct {
	class string
	err   error
}

func (e *exportError) Error() string { return e.err.Error() }

func errorClass(err error) string {
	var e *exportError
	if errors.As(err, &e) {
		return e.class
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "grpc/" + status.Code(err).String()
}

func tlsOptions(path string) (*tls.Config, error) {
	c := &tls.Config{MinVersion: tls.VersionTLS12}
	if path != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA file contains no PEM certificates")
		}
		c.RootCAs = pool
	}
	return c, nil
}

func newSender(c config, endpoint string) (sender, error) {
	tlsConfig, err := tlsOptions(c.CAFile)
	if err != nil {
		return nil, err
	}
	if c.Protocol == "http" {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = tlsConfig
		t.MaxIdleConns, t.MaxIdleConnsPerHost = c.Workers*2, c.Workers*2
		t.MaxConnsPerHost = c.Workers
		return &httpSender{endpoint: endpoint, transport: t, client: &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
	}
	var creds credentials.TransportCredentials = credentials.NewTLS(tlsConfig)
	if c.Insecure {
		creds = insecure.NewCredentials()
	}
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(creds), grpc.WithDisableRetry())
	if err != nil {
		return nil, err
	}
	return &grpcSender{conn: conn, client: collector.NewTraceServiceClient(conn)}, nil
}

type grpcSender struct {
	conn   *grpc.ClientConn
	client collector.TraceServiceClient
}

func (s *grpcSender) Close() error { return s.conn.Close() }
func (s *grpcSender) Export(ctx context.Context, req *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, int, error) {
	n := proto.Size(req)
	response, err := s.client.Export(ctx, req)
	return response, n, err
}

type httpSender struct {
	endpoint  string
	client    *http.Client
	transport *http.Transport
}

func (s *httpSender) Close() error { s.transport.CloseIdleConnections(); return nil }
func (s *httpSender) Export(ctx context.Context, request *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, int, error) {
	body, err := proto.Marshal(request)
	if err != nil {
		return nil, 0, &exportError{"marshal", err}
	}
	// Tomislav-RetCtx: no application retries. GetBody is cleared so net/http
	// cannot replay an export after a redirect or a stale-connection failure.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, len(body), &exportError{"http/request", err}
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Accept", "application/x-protobuf")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, len(body), &exportError{"http/transport", err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, len(body), &exportError{"http/body", err}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, len(body), &exportError{fmt.Sprintf("http/%d", resp.StatusCode), fmt.Errorf("HTTP %s: %.256s", resp.Status, data)}
	}
	if len(data) > 1<<20 {
		return nil, len(body), &exportError{"http/response", errors.New("OTLP response exceeds 1 MiB")}
	}
	if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/x-protobuf") {
		return nil, len(body), &exportError{"http/content-type", fmt.Errorf("expected protobuf response, got %q", contentType)}
	}
	result := &collector.ExportTraceServiceResponse{}
	if err := proto.Unmarshal(data, result); err != nil {
		return nil, len(body), &exportError{"http/decode", err}
	}
	return result, len(body), nil
}
