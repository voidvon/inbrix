package web

import "sync/atomic"

// realtimeMetrics holds process-local counters for the SSE channel. They are
// intentionally cheap atomics so Publish/Subscribe hot paths stay lock-free
// apart from the hub mutex itself.
type realtimeMetrics struct {
	connections         atomic.Int64
	connects            atomic.Int64
	disconnects         atomic.Int64
	connectionsRejected atomic.Int64
	eventsPublished     atomic.Int64
	eventsDropped       atomic.Int64
	overflows           atomic.Int64
	idleWatchers        atomic.Int64
	idleWatcherFailures atomic.Int64
	idleWatcherSkipped  atomic.Int64
}

// RealtimeMetricsSnapshot is the JSON payload of GET /api/realtime/metrics.
type RealtimeMetricsSnapshot struct {
	Connections         int64 `json:"connections"`
	Connects            int64 `json:"connects"`
	Disconnects         int64 `json:"disconnects"`
	ConnectionsRejected int64 `json:"connectionsRejected"`
	EventsPublished     int64 `json:"eventsPublished"`
	EventsDropped       int64 `json:"eventsDropped"`
	Overflows           int64 `json:"overflows"`
	IdleWatchers        int64 `json:"idleWatchers"`
	IdleWatcherFailures int64 `json:"idleWatcherFailures"`
	IdleWatcherSkipped  int64 `json:"idleWatcherSkipped"`
}

func (m *realtimeMetrics) snapshot() RealtimeMetricsSnapshot {
	return RealtimeMetricsSnapshot{
		Connections:         m.connections.Load(),
		Connects:            m.connects.Load(),
		Disconnects:         m.disconnects.Load(),
		ConnectionsRejected: m.connectionsRejected.Load(),
		EventsPublished:     m.eventsPublished.Load(),
		EventsDropped:       m.eventsDropped.Load(),
		Overflows:           m.overflows.Load(),
		IdleWatchers:        m.idleWatchers.Load(),
		IdleWatcherFailures: m.idleWatcherFailures.Load(),
		IdleWatcherSkipped:  m.idleWatcherSkipped.Load(),
	}
}
