package prometheus

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// nolint
const (
	KeyPrefix       = "cassini_"
	KeyQueueSize    = "queue_size"
	KeyQueue        = "queue"
	KeyAdaptors     = "adaptors"
	KeyTxsWait      = "txs_wait"
	KeyTxCost       = "tx_cost"
	KeyTxsPerSecond = "txs_per_second"
	KeyErrors       = "errors"
)

var collector *cassiniCollector
var txsSecMetric *CassiniMetric

func init() {
	collector = &cassiniCollector{
		descs: make(map[string]*prometheus.Desc)}

	collector.descs[KeyQueueSize] = prometheus.NewDesc(
		fmt.Sprint(KeyPrefix, KeyQueueSize),
		"Size of queue",
		[]string{"type"}, nil)
	collector.descs[KeyQueue] = prometheus.NewDesc(
		fmt.Sprint(KeyPrefix, KeyQueue),
		"Current size of tx in queue",
		nil, nil)
	collector.descs[KeyTxsPerSecond] = prometheus.NewDesc(
		fmt.Sprint(KeyPrefix, KeyTxsPerSecond),
		"Number of relayed tx per second",
		nil, nil)
	collector.descs[KeyTxsWait] = prometheus.NewDesc(
		fmt.Sprint(KeyPrefix, KeyTxsWait),
		"Number of tx waiting to be relayed",
		nil, nil)
	collector.descs[KeyTxCost] = prometheus.NewDesc(
		fmt.Sprint(KeyPrefix, KeyTxCost),
		"Time(milliseconds) cost of lastest tx relay",
		nil, nil)
	collector.descs[KeyAdaptors] = prometheus.NewDesc(
		fmt.Sprint(KeyPrefix, KeyAdaptors),
		"Number of available adaptors",
		[]string{"node"}, nil)
	// []string{"from", "to"}, nil)
	collector.descs[KeyErrors] = prometheus.NewDesc(
		fmt.Sprint(KeyPrefix, KeyErrors),
		"Count of running errors",
		nil, nil)

	txsSecMetric = &CassiniMetric{
		value: 0,
		Type:  prometheus.GaugeValue}

	// testing _error metric
	// Set(KeyAdaptors, "panic test")
	SetGauge(KeyQueue, 0)
	// SetGauge(KeyAdaptors, 0)
	SetGauge(KeyTxsWait, 0)
	SetGauge(KeyTxCost, 0)
	Set(KeyTxsPerSecond, txsSecMetric)
	Count(KeyErrors, 0)

	t := time.NewTicker(time.Duration(1) * time.Second)

	go func() {
		for {
			select {
			case <-t.C:
				{
					txsSecMetric.Set(0)
				}
			}
		}
	}()

	// go func() {
	// 	t := time.NewTicker(time.Duration(100) * time.Millisecond)
	// 	for {
	// 		select {
	// 		case <-t.C:
	// 			{
	// 				TxCount(3)
	// 			}
	// 		}
	// 	}
	// }()
}

// CassiniMetric wraps prometheus export data
type CassiniMetric struct {
	value       float64
	Type        prometheus.ValueType
	LabelValues []string
	mux         sync.RWMutex
}

// Value returns the metric's value
func (m *CassiniMetric) Value() float64 {
	m.mux.RLock()
	defer m.mux.RUnlock()

	return m.value
}

// Set the metric's value
func (m *CassiniMetric) Set(v float64) {
	m.mux.Lock()
	defer m.mux.Unlock()

	m.value = v
}

// Count the value to the collector mapper
func (m *CassiniMetric) Count(increase float64) {
	m.mux.Lock()
	defer m.mux.Unlock()

	m.value += increase
}

// Collector returns a collector
// which exports metrics about status code of network service response
func Collector(ch chan<- error) prometheus.Collector {
	collector.SetErrorChannel(ch)
	return collector
}

type cassiniCollector struct {
	descs  map[string]*prometheus.Desc
	mapper sync.Map
	ch     chan<- error
}

// SetErrorChannel set a channel for error
func (c *cassiniCollector) SetErrorChannel(ch chan<- error) {
	c.ch = ch
}

// Describe returns all descriptions of the collector.
func (c *cassiniCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range c.descs {
		ch <- desc
	}
}

// Collect returns the current state of all metrics of the collector.
func (c *cassiniCollector) Collect(ch chan<- prometheus.Metric) {
	exports := func(k, v interface{}) bool {
		key, ok := k.(string)
		if !ok {
			c.ch <- fmt.Errorf("%s%s%s",
				"Collect error: can not convert key(",
				key, ") into a string")
			return true
		}
		var metric *CassiniMetric
		metric, ok = v.(*CassiniMetric)
		if !ok {
			var metrics []*CassiniMetric
			metrics, ok = v.([]*CassiniMetric)
			if !ok {
				c.ch <- fmt.Errorf("%s%s%s",
					"Collect error: can not convert value(", key,
					") into a *cassiniMetric or a []*cassiniMetric")
				return true
			}
			for _, metric = range metrics {
				c.export(ch, key, metric)
			}
		} else {
			c.export(ch, key, metric)
		}
		return true
	}
	c.mapper.Range(exports)
}

func (c *cassiniCollector) export(ch chan<- prometheus.Metric,
	key string, metric *CassiniMetric) {
	desc, ok := c.descs[key]
	if !ok {
		c.ch <- fmt.Errorf("Collect error: can not find desc(%s)", key)
		return
	}
	ch <- prometheus.MustNewConstMetric(
		desc,
		metric.Type,
		metric.Value(), metric.LabelValues...)
}

func (c *cassiniCollector) Set(key string, value interface{}) {
	c.mapper.Store(key, value)
}

func (c *cassiniCollector) Count(key string, increase float64) {
	v, loaded := c.mapper.Load(key)
	if v == nil || !loaded {
		metric := &CassiniMetric{
			value: float64(increase),
			Type:  prometheus.CounterValue}
		if v, loaded = c.mapper.LoadOrStore(key, metric); !loaded {
			return
		}
	}
	metric, ok := v.(*CassiniMetric)
	if !ok {
		c.ch <- fmt.Errorf("%s%s%s",
			"Count error: can not convert value(",
			key, ") into a *cassiniMetric")
		return
	}
	metric.Count(increase)
}

// Set the value to the collector mapper
func Set(key string, value interface{}) {
	collector.Set(key, value)
}

// SetGauge set a single gauge value
func SetGauge(key string, value float64, labelValues ...string) {
	metric := &CassiniMetric{
		Type:        prometheus.GaugeValue,
		LabelValues: labelValues}
	metric.Set(value)
	Set(key, metric)
}

// Count the value to the collector mapper
func Count(key string, increase float64) {
	collector.Count(key, increase)
}

// TxCount the number of relayed tx
func TxCount(increase float64) {
	txsSecMetric.Count(increase)
}

// StartMetrics prometheus exporter("/metrics") service
func StartMetrics(ch chan<- error) {

	prometheus.MustRegister(Collector(ch))

	http.Handle("/metrics", promhttp.Handler())
	ch <- http.ListenAndServe(":39099", nil)
}
