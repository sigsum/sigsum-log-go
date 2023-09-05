package handler

import (
	"github.com/google/trillian/monitoring"
	"github.com/google/trillian/monitoring/prometheus"
)

var (
	reqcnt  monitoring.Counter   // number of incoming http requests
	rspcnt  monitoring.Counter   // number of valid http responses
	latency monitoring.Histogram // request-response latency
)

func init() {
	mf := prometheus.MetricFactory{}
	reqcnt = mf.NewCounter("http_req", "number of http requests", "logid", "endpoint")
	rspcnt = mf.NewCounter("http_rsp", "number of http requests", "logid", "endpoint", "status")
	// Interval 1ms to 10s, with thresholds roughly a factor
	// 10^{1/4} \appr 1.8 apart.
	buckets := []float64{1e-3, 2e-3, 3e-3, 6e-3, 10e-3, 20e-3, 30e-3, 60e-3, 0.1, 0.2, 0.3, 0.6, 1, 2, 3, 6, 10}
	latency = mf.NewHistogramWithBuckets("http_latency", "http request-response latency",
		buckets, "logid", "endpoint", "status")
}
