// Package metrics defines the Prometheus metrics exposed by the mirror
// service (job/queue throughput, webhook activity, mirror/encryption/push
// durations) and the HTTP handler that serves them at /metrics.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

// Namespace prefixes every metric name, e.g. "forgejo_mirror_jobs_total".
const namespace = "forgejo_mirror"

var (
	// WebhookRequests counts incoming webhook requests by outcome (e.g.
	// "accepted", "invalid_signature", "replay", "ignored", "error").
	WebhookRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "webhook_requests_total",
		Help:      "Total number of webhook requests received, by outcome.",
	}, []string{"outcome"})

	// JobsTotal counts completed jobs by terminal status ("succeeded" or
	// "failed").
	JobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "jobs_total",
		Help:      "Total number of mirror jobs processed, by terminal status.",
	}, []string{"status"})

	// JobRetries counts job retry attempts.
	JobRetries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "job_retries_total",
		Help:      "Total number of job retry attempts.",
	})

	// JobDuration observes end-to-end job processing time in seconds.
	JobDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "job_duration_seconds",
		Help:      "Time taken to process a mirror job, in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.5, 2, 12), // 0.5s .. ~512s
	})

	// QueueDepth reports the current number of jobs waiting/running,
	// derived from queue status snapshots.
	QueueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "queue_depth",
		Help:      "Current number of jobs in the queue, by status.",
	}, []string{"status"})

	// StageDuration observes how long each pipeline stage takes, in
	// seconds (stage: "clone_fetch", "encrypt", "github_push").
	StageDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "stage_duration_seconds",
		Help:      "Time taken by each mirror pipeline stage, in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.1, 2, 14),
	}, []string{"stage"})

	// CommitsEncrypted counts encrypted commits produced.
	CommitsEncrypted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "commits_encrypted_total",
		Help:      "Total number of source commits encrypted into the mirrored history.",
	})

	// GithubPushes counts pushes to GitHub by outcome ("success" or
	// "failure").
	GithubPushes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "github_pushes_total",
		Help:      "Total number of pushes to GitHub, by outcome.",
	}, []string{"outcome"})

	// KeyRotations counts detected encryption-recipient rotations.
	KeyRotations = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "key_rotations_total",
		Help:      "Total number of encryption recipient rotations detected at startup.",
	})

	// BuildInfo exposes build/version metadata as a gauge with a constant
	// value of 1, labeled with version info (standard Prometheus pattern).
	BuildInfo = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Build information, value is always 1.",
	}, []string{"version"})
)

// StageTimer starts timing a pipeline stage and returns a function that
// records the elapsed duration to StageDuration when called (typically via
// defer).
func StageTimer(stage string) func() {
	start := time.Now()
	return func() {
		StageDuration.WithLabelValues(stage).Observe(time.Since(start).Seconds())
	}
}

// Handler returns the HTTP handler that serves metrics in the Prometheus
// text exposition format, for mounting at /metrics.
func Handler() http.Handler {
	return promhttp.Handler()
}
