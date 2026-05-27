package main

// spanRecord holds the data for one OTel span.
type spanRecord struct {
	spanID       [8]byte
	parentSpanID [8]byte
	name         string
	startNano    int64
	endNano      int64
	kind         int32
	attrs        [][2]string
}
