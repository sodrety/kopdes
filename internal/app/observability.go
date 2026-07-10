package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const requestIDHeader = "X-Request-ID"

type instrumentation struct {
	registry       *prometheus.Registry
	requestsTotal  *prometheus.CounterVec
	requestLatency *prometheus.HistogramVec
}

func newInstrumentation(cfg Config) *instrumentation {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),
	)

	registerer := prometheus.WrapRegistererWith(prometheus.Labels{
		"service": cfg.observabilityServiceName(),
		"version": cfg.observabilityServiceVersion(),
	}, registry)
	factory := promauto.With(registerer)

	return &instrumentation{
		registry: registry,
		requestsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests handled by the application.",
		}, []string{"method", "route", "status"}),
		requestLatency: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route", "status"}),
	}
}

func (s *Server) requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(requestIDHeader)
		if requestID == "" {
			requestID = newRequestID()
		}
		c.Set("request_id", requestID)
		c.Writer.Header().Set(requestIDHeader, requestID)
		c.Next()
	}
}

func (s *Server) observeRequests() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := strconv.Itoa(c.Writer.Status())
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := c.Request.Method

		s.instrumentation.requestsTotal.WithLabelValues(method, route, status).Inc()
		s.instrumentation.requestLatency.WithLabelValues(method, route, status).Observe(time.Since(start).Seconds())

		attrs := []any{
			"request_id", requestIDFromContext(c),
			"method", method,
			"path", c.Request.URL.Path,
			"route", route,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "error", c.Errors.String())
		}
		if c.Writer.Status() >= http.StatusInternalServerError {
			slog.Error("http_request", attrs...)
			return
		}
		slog.Info("http_request", attrs...)
	}
}

func (s *Server) readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		slog.Error("readiness_check_failed", "request_id", requestIDFromContext(c), "error", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func requestIDFromContext(c *gin.Context) string {
	value, ok := c.Get("request_id")
	if !ok {
		return ""
	}
	requestID, ok := value.(string)
	if !ok {
		return ""
	}
	return requestID
}

func newRequestID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(bytes[:])
}
