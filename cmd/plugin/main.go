package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"github.com/kborup-redhat/placement-discovery-plugin/pkg/handlers"
	"github.com/kborup-redhat/placement-discovery-plugin/pkg/kubernetes"
)

const (
	defaultPort       = 9002
	defaultStaticPath = "/app/web/dist"
)

func main() {
	var port int
	var staticPath string
	var certFile string
	var keyFile string

	// Create a new FlagSet to avoid conflicts with klog
	fs := flag.NewFlagSet("placement-discovery-plugin", flag.ExitOnError)
	fs.IntVar(&port, "port", defaultPort, "Port to listen on")
	fs.StringVar(&staticPath, "static-path", defaultStaticPath, "Path to static files")
	fs.StringVar(&certFile, "cert-file", "", "Path to TLS certificate file")
	fs.StringVar(&keyFile, "key-file", "", "Path to TLS key file")
	klog.InitFlags(fs)
	fs.Parse(os.Args[1:])

	klog.Infof("Starting placement-discovery-plugin server on port %d", port)

	// Create Kubernetes client
	client, err := kubernetes.NewClient()
	if err != nil {
		klog.Fatalf("Failed to create Kubernetes client: %v", err)
	}
	klog.Info("Successfully created Kubernetes client")

	// Create handlers
	placementHandler := handlers.NewPlacementHandler(client)

	// Set up HTTP routes
	mux := http.NewServeMux()

	// Validate and resolve static path
	absStaticPath, err := filepath.Abs(staticPath)
	if err != nil {
		klog.Fatalf("Invalid static path: %v", err)
	}

	// Plugin manifest (required for OpenShift console plugin)
	// Serve at both root and /plugin-manifest.json for compatibility
	manifestHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, filepath.Join(absStaticPath, "plugin-manifest.json"))
	}
	mux.HandleFunc("/plugin-manifest.json", manifestHandler)

	// API routes
	mux.Handle("/api/placement/", placementHandler)

	// Health and readiness probes
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler(client))

	// Serve static files and manifest at root
	// Check if static path exists
	if _, err := os.Stat(absStaticPath); os.IsNotExist(err) {
		klog.Warningf("Static path does not exist, static files will not be served")
	} else {
		// Custom handler for root that serves manifest or static files
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// Serve manifest at root path for console
			if r.URL.Path == "/" && (r.Header.Get("Accept") == "application/json" || r.Method == "GET") {
				manifestHandler(w, r)
				return
			}
			// Serve other static files
			http.FileServer(http.Dir(absStaticPath)).ServeHTTP(w, r)
		})
		klog.Info("Serving static files")
		klog.Info("Plugin manifest available at / and /plugin-manifest.json")
	}

	// Create server with middleware chain
	handler := securityHeadersMiddleware(rateLimitMiddleware(loggingMiddleware(corsMiddleware(mux))))
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		klog.Infof("Server listening on :%d", port)
		var err error
		if certFile != "" && keyFile != "" {
			klog.Info("Starting HTTPS server")
			err = server.ListenAndServeTLS(certFile, keyFile)
		} else {
			klog.Info("Starting HTTP server (no TLS)")
			err = server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			klog.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	klog.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		klog.Fatalf("Server forced to shutdown: %v", err)
	}

	klog.Info("Server exited")
}

// healthHandler returns 200 OK if the server is running
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "OK")
}

// readyHandler returns 200 OK if the server is ready to serve requests
func readyHandler(client *kubernetes.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if we can connect to Kubernetes API
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		_, err := client.K8s.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1})
		if err != nil {
			klog.Errorf("Readiness check failed: %v", err)
			http.Error(w, "Not ready", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Ready")
	}
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		klog.V(4).Infof("%s %s %s", r.Method, r.RequestURI, time.Since(start))
	})
}

// corsMiddleware adds CORS headers
// In OpenShift, the console proxies plugin requests via the same origin,
// so CORS is only needed for local development.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			// Only allow same-origin requests in production.
			// The OpenShift console proxies plugin API calls, so cross-origin
			// requests should not occur. Allow GET only.
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// securityHeadersMiddleware sets standard HTTP security headers
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware implements a simple token bucket rate limiter per-client IP.
// Allows a burst of 50 requests and refills at 10 requests/second.
func rateLimitMiddleware(next http.Handler) http.Handler {
	type client struct {
		tokens    float64
		lastSeen  time.Time
	}

	var (
		mu      sync.Mutex
		clients = make(map[string]*client)
	)

	const (
		rate     = 10.0 // tokens per second
		burst    = 50.0 // max tokens
		cleanupInterval = 5 * time.Minute
	)

	// Background cleanup of stale entries
	go func() {
		for {
			time.Sleep(cleanupInterval)
			mu.Lock()
			for ip, c := range clients {
				if time.Since(c.lastSeen) > cleanupInterval {
					delete(clients, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for health/ready probes
		if r.URL.Path == "/health" || r.URL.Path == "/ready" {
			next.ServeHTTP(w, r)
			return
		}

		ip := r.RemoteAddr

		mu.Lock()
		c, exists := clients[ip]
		now := time.Now()
		if !exists {
			c = &client{tokens: burst, lastSeen: now}
			clients[ip] = c
		}

		// Refill tokens based on elapsed time
		elapsed := now.Sub(c.lastSeen).Seconds()
		c.tokens += elapsed * rate
		if c.tokens > burst {
			c.tokens = burst
		}
		c.lastSeen = now

		if c.tokens < 1 {
			mu.Unlock()
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		c.tokens--
		mu.Unlock()

		next.ServeHTTP(w, r)
	})
}
