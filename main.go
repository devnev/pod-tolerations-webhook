package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	prommetrics "github.com/slok/go-http-metrics/metrics/prometheus"
	"github.com/slok/go-http-metrics/middleware"
	mwstd "github.com/slok/go-http-metrics/middleware/std"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
	podResource   = metav1.GroupVersionResource{Version: "v1", Resource: "pods"}
)

const (
	tlsFolder      = `/etc/secrets/tls`
	tlsCertificate = `tls.crt`
	tlsPrivateKey  = `tls.key`
	serverPort     = `:8443`
	statusPort     = `:8080`
	shutdownPeriod = 30 * time.Second
)

func main() {
	haveToleration := false
	flag.Func("toleration", fmt.Sprintf("Toleration to apply to pods, format %s", strings.TrimSpace(tolerationFormatDesc)), func(s string) error {
		var err error
		toleration, err = parseToleration(s)
		haveToleration = haveToleration || err == nil
		return err
	})
	tlsCertPath := flag.String("tls-cert", path.Join(tlsFolder, tlsCertificate), "Path to TLS certificate")
	tlsKeyPath := flag.String("tls-key", path.Join(tlsFolder, tlsPrivateKey), "Path to TLS private key")
	address := flag.String("address", serverPort, "Address/port to serve on")
	statusAddress := flag.String("status-address", statusPort, "Address/port to serve status&metrics on")
	shutdownPeriod := flag.Duration("shutdown-period", shutdownPeriod, "Graceful shutdown period on interrupt")
	flag.Parse()

	if !haveToleration {
		fmt.Fprintln(flag.CommandLine.Output(), "Missing required option --toleration")
		flag.Usage()
		os.Exit(2)
	}

	shutdown, statusServer := newStatusServer(*statusAddress)
	webhookServer := newWebhookServer(*address, *tlsCertPath, *tlsKeyPath)

	g, ctx := errgroup.WithContext(context.Background())

	g.Go(func() error {
		slog.Info("Serving status", slog.String("address", statusServer.Addr))
		err := statusServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			slog.Info("Status server terminated", slog.String("address", *statusAddress))
			return nil
		}
		slog.Error("Status server failed", errAttr(err), slog.String("address", *statusAddress))
		return fmt.Errorf("status server failed: %w", err)
	})

	g.Go(func() error {
		slog.Info("Serving webhook", slog.String("address", webhookServer.Addr))
		err := webhookServer.ListenAndServeTLS("", "")
		if errors.Is(err, http.ErrServerClosed) {
			slog.Info("Webhook server terminated", errAttr(err), slog.String("address", *address))
			return nil
		}
		slog.Error("Server failed", errAttr(err), slog.String("address", *address))
		return fmt.Errorf("webhook server failed: %w", err)
	})

	g.Go(func() error {
		<-ctx.Done()
		//nolint:contextcheck
		err := statusServer.Shutdown(context.Background())
		if err != nil {
			slog.Error("Status server shutdown failed", errAttr(err))
		}
		return fmt.Errorf("status server shutdown failed: %w", err)
	})

	g.Go(func() error {
		<-ctx.Done()
		//nolint:contextcheck
		err := webhookServer.Shutdown(context.Background())
		if err != nil {
			slog.Error("Webhook server shutdown failed", errAttr(err))
		}
		return fmt.Errorf("webhook server shutdown failed: %w", err)
	})

	g.Go(func() error {
		sigctx, stopsig := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		<-sigctx.Done()
		stopsig()
		shutdown.Store(true)
		if ctx.Err() == nil {
			sigctx, stopsig = context.WithTimeout(ctx, *shutdownPeriod)
			signal.NotifyContext(sigctx, syscall.SIGINT, syscall.SIGTERM)
			<-sigctx.Done()
			stopsig()
		}
		return nil
	})

	if g.Wait() != nil {
		os.Exit(1)
	}
}

func newStatusServer(address string) (shutdown *atomic.Bool, server *http.Server) {
	shutdown = &atomic.Bool{}

	statusMux := http.NewServeMux()
	statusMux.Handle("/metrics", promhttp.Handler())
	statusMux.Handle("/status", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		status := http.StatusOK
		if shutdown.Load() {
			status = http.StatusServiceUnavailable
		}

		w.WriteHeader(status)

		if _, err := io.Copy(w, strings.NewReader(`{}`)); err != nil {
			panic(err)
		}
	}))

	server = &http.Server{
		Addr:              address,
		Handler:           statusMux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return shutdown, server
}

func newWebhookServer(address string, tlsCertPath string, tlsKeyPath string) *http.Server {
	mw := middleware.New(middleware.Config{Recorder: prommetrics.NewRecorder(prommetrics.Config{})})
	mux := http.NewServeMux()
	mux.Handle("/mutate", mwstd.Handler("mutate", mw, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serve(w, r, addToleration)
	})))

	server := &http.Server{
		Addr:    address,
		Handler: mux,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
		},
		ReadHeaderTimeout: 10 * time.Second,
	}

	keyPair, err := tls.LoadX509KeyPair(tlsCertPath, tlsKeyPath)
	if err != nil {
		slog.Error("Failed to load TLS keypair", errAttr(err), slog.String("tls-cert", tlsCertPath), slog.String("tls-key", tlsKeyPath))
		os.Exit(1)
	}

	server.TLSConfig.Certificates = append(server.TLSConfig.Certificates, keyPair)

	return server
}

func errAttr(err error) slog.Attr {
	return slog.Any("error", err)
}
