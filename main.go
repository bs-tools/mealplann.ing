package main

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/foodplann.ing/internal/apilimit"
	"github.com/bs-tools/foodplann.ing/internal/authn"
	"github.com/bs-tools/foodplann.ing/internal/blob"
	"github.com/bs-tools/foodplann.ing/internal/errreport"
	"github.com/bs-tools/foodplann.ing/internal/gen/auth/v1/authv1connect"
	"github.com/bs-tools/foodplann.ing/internal/gen/oauth/v1/oauthv1connect"
	"github.com/bs-tools/foodplann.ing/internal/gen/users/v1/usersv1connect"
	"github.com/bs-tools/foodplann.ing/internal/html"
	"github.com/bs-tools/foodplann.ing/internal/mcp"
	"github.com/bs-tools/foodplann.ing/internal/oauth"
	"github.com/bs-tools/foodplann.ing/internal/services/auth"
	oauthsvc "github.com/bs-tools/foodplann.ing/internal/services/oauth"
	"github.com/bs-tools/foodplann.ing/internal/services/user"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/bs-tools/foodplann.ing/internal/telemetry"
	"github.com/bs-tools/foodplann.ing/internal/timeouts"
	"github.com/bs-tools/foodplann.ing/internal/users"
	"github.com/johanbrandhorst/reload"
	"gitlab.com/greyxor/slogor"
	"google.golang.org/api/option"
)

const (
	defaultHostname = "localhost"
	defaultPort     = "8080"
	// mcpPath is the path of the MCP server.
	mcpPath = "/mcp"
)

// Timeouts of incoming requests. Cloud Run lets a request run for 15
// minutes, so without them a slow or stalled client could hold one of the
// instance's 80 concurrent requests, and its memory and database
// connections, for that long.
const (
	// readHeaderTimeout bounds reading a request's headers.
	readHeaderTimeout = 10 * time.Second
	// bodyReadTimeout bounds reading a request's body, which is up to 32 MiB
	// for uploads of photos from phones on slow connections.
	bodyReadTimeout = 2 * time.Minute
	// handlerTimeout bounds how long requests run.
	handlerTimeout = 2 * time.Minute
	// idleTimeout is how long an idle connection is kept open. Google's front
	// ends keep idle connections to backends for up to 600 seconds, and one
	// closed by the server first can fail the request the front end sends on
	// it, so this is longer.
	idleTimeout = 620 * time.Second
	// shutdownTimeout bounds waiting for requests to finish on shutdown.
	// Cloud Run stops an instance 10 seconds after sending SIGTERM, which
	// leaves the rest for sending the remaining traces.
	shutdownTimeout = 5 * time.Second
)

var (
	logLevel = flag.String(
		"log-level",
		"info",
		"The log level to use",
	)
	logFormat = flag.String(
		"log-format",
		"text",
		"The log format: \"text\" for terminals, or \"json\" for structured logging platforms like Cloud Logging",
	)
	errorReportingProject = flag.String(
		"error-reporting-project",
		"",
		"The Google Cloud project to report errors to with Error Reporting, using the application default credentials. If unset, errors are logged",
	)
	otlpEndpoint = flag.String(
		"otlp-endpoint",
		telemetry.LocalEndpoint,
		"The OTLP gRPC endpoint to export traces to: a local collector like Grafana Tempo, or https://telemetry.googleapis.com with -trace-project. Empty disables tracing",
	)
	traceProject = flag.String(
		"trace-project",
		"",
		"The Google Cloud project to export traces to, using the application default credentials",
	)
	traceSampleRate = flag.Float64(
		"trace-sample-rate",
		1,
		"The fraction of traces to sample, from 0 to 1",
	)
	databaseURL = flag.String(
		"database-url",
		"",
		"The URL of the postgres database in which to store the state of the application",
	)
	hostname = flag.String(
		"hostname",
		defaultHostname,
		"The hostname on which to serve the application",
	)
	port = flag.String(
		"port",
		defaultPort,
		"The port on which to serve the application",
	)
	tlsKeyPath = flag.String(
		"tls-key",
		"",
		"The path to the private key used for serving the application over HTTPS",
	)
	tlsCertPath = flag.String(
		"tls-cert",
		"",
		"The path to the certificate used for serving the application over HTTPS",
	)
	serveDir = flag.String(
		"serve-dir",
		"",
		"Optionally, the directory to serve as the root of the web application."+
			" If unset, will serve the compiled web application from the web package",
	)
	publicURL = flag.String(
		"public-url",
		"",
		"The public URL of the application, used for OAuth redirects and share links."+
			" Defaults to http(s)://<hostname>:<port>",
	)
	adminBootstrapToken = flag.String(
		"admin-bootstrap-token",
		"",
		"A secret that can be used as an invitation to create the first administrator,"+
			" at /invite#<token>, while there are no administrators",
	)
	localSignIn = flag.Bool(
		"local-sign-in",
		false,
		"Let browsers and agents on the server's machine sign in without a passkey at "+auth.LocalSignInPath+
			", as the first administrator, which is created if there is none. For testing a local server only:"+
			" the server refuses to start with it unless its URL is on localhost",
	)
	avatarBucket = flag.String(
		"avatar-bucket",
		"",
		"The Google Cloud Storage bucket to store users' avatars in, using the application default credentials."+
			" If unset, avatars are stored in -avatar-dir",
	)
	avatarDir = flag.String(
		"avatar-dir",
		"avatars",
		"The local directory to store users' avatars in, if -avatar-bucket is unset",
	)
)

func main() {
	flag.Parse()
	if err := setFlagsFromEnv(flag.CommandLine); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set flags from the environment: %v\n", err)
		os.Exit(1)
	}
	logger, err := newLogger(*logLevel, *logFormat)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set up logging: %v\n", err)
		os.Exit(1)
	}
	stopTracing, err := initTracing(context.Background(), *otlpEndpoint, *traceProject, *traceSampleRate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set up tracing: %v\n", err)
		os.Exit(1)
	}
	report, err := newReporter(context.Background(), logger, *errorReportingProject)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set up error reporting: %v\n", err)
		os.Exit(1)
	}
	telemetry.TraceDefaultTransport(report)
	// Cloud Run sends SIGTERM to stop instances.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = run(
		ctx,
		logger,
		report,
		*databaseURL,
		*hostname,
		*port,
		*tlsKeyPath,
		*tlsCertPath,
		*serveDir,
		*publicURL,
		*adminBootstrapToken,
		*localSignIn,
		*avatarBucket,
		*avatarDir,
	)
	stop()
	if err != nil {
		report.Report(context.Background(), "Failed running app", err)
	}
	// Send the traces and errors not yet sent before exiting. Error
	// Reporting goes last, to report failing to send the traces.
	if traceErr := stopTracing(); traceErr != nil {
		report.Report(context.Background(), "Failed to send the remaining traces", traceErr)
	}
	if closeErr := report.Close(); closeErr != nil {
		logger.Warn("Failed to send the remaining error reports", slog.String("error", closeErr.Error()))
	}
	if err != nil {
		os.Exit(1)
	}
}

// newReporter returns a reporter sending errors to Error Reporting in a
// project, from the Cloud Run service or job it runs in, or logging them if
// the project is empty.
func newReporter(ctx context.Context, log *slog.Logger, project string) (*errreport.Reporter, error) {
	if project == "" {
		return errreport.NewLocal(log), nil
	}
	return errreport.New(ctx, project, serviceName(), serviceVersion(), log,
		option.WithGRPCDialOption(telemetry.GRPCDialOption()))
}

// serviceName returns the name of the Cloud Run service or job the app runs
// as, or "foodplanning" outside of Cloud Run.
func serviceName() string {
	return cmp.Or(os.Getenv("K_SERVICE"), os.Getenv("CLOUD_RUN_JOB"), "foodplanning")
}

// serviceVersion returns the Cloud Run revision or job execution the app
// runs as, if any.
func serviceVersion() string {
	return cmp.Or(os.Getenv("K_REVISION"), os.Getenv("CLOUD_RUN_EXECUTION"))
}

// tracingShutdownTimeout bounds sending the remaining traces on exit.
const tracingShutdownTimeout = 5 * time.Second

// initTracing exports traces to an OTLP endpoint. The returned function
// sends the remaining traces.
func initTracing(ctx context.Context, endpoint, project string, sampleRate float64) (func() error, error) {
	shutdown, err := telemetry.Init(ctx, &telemetry.Config{
		Endpoint:       endpoint,
		GCPProject:     project,
		ServiceName:    serviceName(),
		ServiceVersion: serviceVersion(),
		SampleRate:     sampleRate,
	})
	if err != nil {
		return nil, err
	}
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), tracingShutdownTimeout)
		defer cancel()
		return shutdown(ctx)
	}, nil
}

func run(
	ctx context.Context,
	log *slog.Logger,
	report *errreport.Reporter,
	databaseURL string,
	hostname string,
	port string,
	tlsKeyPath string,
	tlsCertPath string,
	serveDir string,
	publicURL string,
	adminBootstrapToken string,
	localSignIn bool,
	avatarBucket string,
	avatarDir string,
) error {
	switch {
	case databaseURL == "":
		return errors.New("database-url is required")
	case hostname == "":
		return errors.New("address is required")
	case port == "":
		return errors.New("port is required")
	case tlsKeyPath != "" && tlsCertPath == "" || tlsKeyPath == "" && tlsCertPath != "":
		return errors.New("tls-cert and tls-key must be set together")
	case avatarBucket == "" && avatarDir == "":
		return errors.New("avatar-bucket or avatar-dir is required")
	}
	dbURL, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse database-url: %w", err)
	}
	address := net.JoinHostPort(hostname, port)
	baseURL := "http://" + address
	var cert tls.Certificate
	if tlsCertPath != "" && tlsKeyPath != "" {
		cert, err = tls.LoadX509KeyPair(tlsCertPath, tlsKeyPath)
		if err != nil {
			return fmt.Errorf("failed to load TLS certificate: %w", err)
		}
		baseURL = "https://" + address
	}
	if publicURL != "" {
		baseURL = strings.TrimSuffix(publicURL, "/")
	}
	if localSignIn && !auth.IsLocalURL(baseURL) {
		return fmt.Errorf("local-sign-in signs anyone on the server's machine in as an administrator,"+
			" so it's only allowed for a server on localhost, not %s", baseURL)
	}

	db, err := store.New(ctx, log, dbURL)
	if err != nil {
		return fmt.Errorf("failed to create store: %w", err)
	}
	users, err := users.NewRepository(log, db)
	if err != nil {
		return fmt.Errorf("failed to create user directory: %w", err)
	}
	// Every outgoing request is traced: clients are made by the telemetry
	// package, and Google's API clients take theirs from the context.
	googleCtx := telemetry.WithClient(ctx, telemetry.NewClient(0))

	// Avatars are stored in Cloud Storage on Cloud Run, whose file system
	// doesn't outlive an instance, and in a local directory otherwise.
	avatars, err := newBlobStore(googleCtx, log, "avatars", avatarBucket, avatarDir)
	if err != nil {
		return err
	}

	userService := user.NewService(log, report, users, users, users, avatars)
	authService, err := auth.NewService(log, report, users, baseURL, adminBootstrapToken)
	if err != nil {
		return fmt.Errorf("failed to create auth service: %w", err)
	}
	// MCP clients that require OAuth, like claude.ai, authorize with this
	// authorization server instead of API tokens.
	oauthServer := oauth.NewServer(log, report, users, baseURL, mcpPath)
	oauthService := oauthsvc.NewService(log, report, oauthServer, users)

	// Register all services on a single Connect server, served by the API mux
	server := connect.NewServer()
	usersv1connect.RegisterUserServiceHandler(server, userService)
	authv1connect.RegisterAuthServiceHandler(server, authService)
	oauthv1connect.RegisterOAuthServiceHandler(server, oauthService)
	// Requests are bounded, so one can't exhaust the server's memory. Only
	// signed in users can upload files, like avatars.
	apiMux := http.NewServeMux()
	apilimit.Mount(apiMux, server)

	// Wrap the API mux in the authn middleware. Each service checks
	// authentication itself.
	authHandler, err := authn.NewAuthnMiddleware(ctx, log, report, users, apiMux)
	if err != nil {
		return fmt.Errorf("failed to create authentication middleware: %w", err)
	}
	// Create the core mux. This contains both the Connect API and HTML handlers.
	// Only the Connect API endpoints (registered to apiMux) are authenticated.
	mux := http.NewServeMux()
	// Serve the Connect API handlers through the authn middleware
	for _, path := range servicePaths(server) {
		mux.Handle(path, authHandler)
	}

	// Serve the same API to AI agents over MCP. MCP clients authenticate
	// with API tokens or OAuth access tokens.
	mcpServer, err := mcp.NewServer(log, report,
		mcp.Service{Name: usersv1connect.UserServiceName, Handler: userService},
	)
	if err != nil {
		return fmt.Errorf("failed to create MCP server: %w", err)
	}
	resourceMetadataURL := oauthServer.ResourceMetadataURL()
	mcpHandler, err := authn.NewAuthnMiddleware(ctx, log, report, users,
		authn.RequireUser(resourceMetadataURL, mcpServer.Handler()),
		authn.WithResourceMetadata(resourceMetadataURL),
	)
	if err != nil {
		return fmt.Errorf("failed to create MCP authentication middleware: %w", err)
	}
	mux.Handle(mcpPath, mcpHandler)

	// Register the logout handler directly on the mux
	authn.RegisterLogoutHandler(report, mux, users)

	// Serve the frontend's bundle, embedded in the server or, while watching
	// for changes, from its directory.
	fileServer := html.ServeBundle()
	if serveDir != "" {
		// The watched bundle can be rebuilt within the second the browser
		// cached it, which Last-Modified can't tell apart, so browsers
		// mustn't keep it at all.
		next := http.FileServerFS(os.DirFS(serveDir))
		fileServer = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
		})
	}

	// Serve index.js, index.js.map, index.css, favicon.svg, img/ and fonts/
	// directly, and the web app manifest and service worker, which make the
	// app installable and must be at the root to control all its pages.
	mux.Handle("/index.js", fileServer)
	mux.Handle("/sw.js", fileServer)
	mux.Handle("/manifest.webmanifest", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Browsers only take a manifest with its media type, which Go
		// doesn't know the extension of.
		w.Header().Set("Content-Type", "application/manifest+json")
		fileServer.ServeHTTP(w, r)
	}))
	mux.Handle("/index.js.map", fileServer)
	mux.Handle("/index.css", fileServer)
	mux.Handle("/favicon.svg", fileServer)
	mux.Handle("/img/", fileServer)
	mux.Handle("/fonts/", fileServer)
	// For all other requests, serve the contents of index.html
	indexHandler := html.ServeIndexHTML(serveDir != "")
	// A local server without an administrator sends its first visitor to
	// the invitation to create one.
	mux.Handle("/", authService.BootstrapHandler(indexHandler))
	if localSignIn {
		mux.Handle(auth.LocalSignInPath, authService.LocalSignInHandler())
		log.InfoContext(ctx, "Local sign-in enabled", slog.String("url", baseURL+auth.LocalSignInPath))
	}
	// Serve the OAuth endpoints, with the frontend's consent page for valid
	// authorization requests.
	oauthServer.RegisterHandlers(mux, indexHandler)
	mux.Handle(user.AvatarPath, userService.AvatarHandler())

	// Requests are traced, and named after the routes they match.
	srvHandler := telemetry.Routes(mux)
	// Add auto-reloading middleware if serving from a directory
	if serveDir != "" {
		srvHandler, err = reload.NewMiddleware(srvHandler, serveDir, log)
		if err != nil {
			return fmt.Errorf("failed to create reload middleware: %w", err)
		}
	}

	// Serve HTTP/2 without TLS (h2c) alongside HTTP/1.1.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{
		Addr: address,
		Handler: timeouts.Handler(telemetry.Handler(srvHandler), timeouts.Config{
			BodyRead: bodyReadTimeout,
			Handler:  handlerTimeout,
			// The reload middleware's websocket stays open while developing.
			LongPaths: []string{"/watch"},
		}),
		Protocols:         protocols,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}
	serveFn := srv.ListenAndServe
	if cert.Certificate != nil {
		// Over TLS, negotiate HTTP/2 via ALPN instead of h2c.
		protocols.SetHTTP2(true)
		srv.TLSConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
		}
		serveFn = func() error {
			return srv.ListenAndServeTLS("", "")
		}
	}
	// Serving stops as soon as the shutdown starts, so wait for it to let the
	// requests in flight finish.
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		log.InfoContext(ctx, "Interrupt received, shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			report.Report(ctx, "Failed to finish the requests in flight before shutting down, canceling them", err)
			_ = srv.Close()
		}
	}()
	log.InfoContext(ctx, "Serving on", slog.String("addr", address))
	if err := serveFn(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to serve: %w", err)
	}
	<-shutdownDone
	return nil
}

// envPrefix is the prefix of environment variables that set flags.
const envPrefix = "FOODPLANNING_"

// setFlagsFromEnv sets flags that weren't set on the command line from
// environment variables named FOODPLANNING_<FLAG>, e.g. FOODPLANNING_DATABASE_URL
// for -database-url. This allows passing secrets through the environment.
func setFlagsFromEnv(fs *flag.FlagSet) error {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	var err error
	fs.VisitAll(func(f *flag.Flag) {
		if set[f.Name] || err != nil {
			return
		}
		name := envPrefix + strings.ToUpper(strings.ReplaceAll(f.Name, "-", "_"))
		if value, ok := os.LookupEnv(name); ok {
			if setErr := f.Value.Set(value); setErr != nil {
				err = fmt.Errorf("invalid value for %s: %w", name, setErr)
			}
		}
	})
	return err
}

// newBlobStore returns a store of what it stores in the Cloud Storage
// bucket, if set, or else in the local directory.
func newBlobStore(ctx context.Context, log *slog.Logger, what, bucket, dir string) (blob.Store, error) {
	if bucket != "" {
		blobs, err := blob.NewGCS(ctx, bucket)
		if err != nil {
			return nil, fmt.Errorf("failed to create the %s bucket client: %w", what, err)
		}
		log.InfoContext(ctx, "Storing "+what+" in Cloud Storage", slog.String("bucket", bucket))
		return blobs, nil
	}
	blobs, err := blob.NewDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create the %s directory: %w", what, err)
	}
	log.InfoContext(ctx, "Storing "+what+" in a local directory", slog.String("dir", dir))
	return blobs, nil
}

// newLogger returns a logger logging at level in format: "text" for
// terminals, or "json" for Cloud Logging.
func newLogger(level, format string) (*slog.Logger, error) {
	var slogLevel slog.Level
	if err := slogLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}
	switch format {
	case "text":
		return slog.New(slogor.NewHandler(os.Stdout, slogor.SetTimeFormat(time.Stamp), slogor.SetLevel(slogLevel))), nil
	case "json":
		return slog.New(newJSONLogHandler(slogLevel)), nil
	default:
		return nil, fmt.Errorf("invalid log format %q, expected text or json", format)
	}
}

// newJSONLogHandler returns a handler logging JSON lines in the format of
// Google Cloud Logging's structured logs.
func newJSONLogHandler(level slog.Level) slog.Handler {
	return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return a
			}
			switch a.Key {
			case slog.MessageKey:
				a.Key = "message"
			case slog.LevelKey:
				a.Key = "severity"
				if lvl, ok := a.Value.Any().(slog.Level); ok && lvl == slog.LevelWarn {
					a.Value = slog.StringValue("WARNING")
				}
			}
			return a
		},
	})
}

// servicePaths returns the URL path prefixes of the services registered on
// server, e.g. /users.v1.UserService/.
func servicePaths(server *connect.Server) []string {
	var paths []string
	for spec := range server.Specs() {
		path := spec.Procedure[:strings.LastIndexByte(spec.Procedure, '/')+1]
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	return paths
}
