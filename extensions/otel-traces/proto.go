//go:build wasip1

package main

// proto.go — hand-written protobuf encoder for OTLP trace export.
//
// Implements ExportTraceServiceRequest wire format from opentelemetry-proto
// without any external dependencies.
//
// Wire types used:
//   0 = varint
//   1 = fixed64 (8-byte little-endian) — used for timestamp nanoseconds
//   2 = length-delimited (string, bytes, embedded messages, repeated fields)

// spanRecord holds the data for one OTel span.
type spanRecord struct {
	spanID       [8]byte
	parentSpanID [8]byte
	name         string
	startNano    int64
	endNano      int64
	kind         int32       // 1=INTERNAL
	attrs        [][2]string // [][key, value]
}

// ─── Low-level encoding helpers ───────────────────────────────────────────────

// appendVarint appends a protobuf varint to b.
func appendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

// fieldTag returns the protobuf field tag for the given field number and wire type.
// wireType: 0=varint, 1=fixed64, 2=length-delimited
func fieldTag(fieldNum int, wireType int) uint64 {
	return uint64(fieldNum<<3) | uint64(wireType)
}

// appendLenDelim appends a length-delimited field (tag + varint len + data).
func appendLenDelim(b []byte, fieldNum int, data []byte) []byte {
	b = appendVarint(b, fieldTag(fieldNum, 2))
	b = appendVarint(b, uint64(len(data)))
	b = append(b, data...)
	return b
}

// appendString appends a string as a length-delimited field.
func appendString(b []byte, fieldNum int, s string) []byte {
	return appendLenDelim(b, fieldNum, []byte(s))
}

// appendVarintField appends a varint field (tag + value).
func appendVarintField(b []byte, fieldNum int, v uint64) []byte {
	b = appendVarint(b, fieldTag(fieldNum, 0))
	b = appendVarint(b, v)
	return b
}

// appendFixed64Field appends a fixed64 field (tag + 8-byte little-endian value).
// Used for OTLP timestamp fields (start_time_unix_nano, end_time_unix_nano)
// which are proto type fixed64, wire type 1.
func appendFixed64Field(b []byte, fieldNum int, v uint64) []byte {
	b = appendVarint(b, fieldTag(fieldNum, 1))
	b = append(
		b,
		byte(v),
		byte(v>>8),
		byte(v>>16),
		byte(v>>24),
		byte(v>>32),
		byte(v>>40),
		byte(v>>48),
		byte(v>>56),
	)
	return b
}

// ─── OTLP message encoders ────────────────────────────────────────────────────

// encodeAnyValue encodes an AnyValue proto with a string_value (field 1).
func encodeAnyValue(s string) []byte {
	var b []byte
	b = appendString(b, 1, s)
	return b
}

// encodeKeyValue encodes a KeyValue proto: field 1 = key, field 2 = AnyValue.
func encodeKeyValue(key, value string) []byte {
	var b []byte
	b = appendString(b, 1, key)
	b = appendLenDelim(b, 2, encodeAnyValue(value))
	return b
}

// encodeStatus encodes a Status proto: field 2 = code (varint).
// code: 0=UNSET, 1=OK, 2=ERROR
func encodeStatus(code int) []byte {
	var b []byte
	b = appendVarintField(b, 2, uint64(code))
	return b
}

// encodeInstrumentationScope encodes an InstrumentationScope with name='wllr/otel-traces'.
func encodeInstrumentationScope() []byte {
	var b []byte
	b = appendString(b, 1, "wllr/otel-traces")
	return b
}

// encodeResource encodes a Resource with service.name='wllr'.
func encodeResource() []byte {
	var b []byte
	b = appendLenDelim(b, 1, encodeKeyValue("service.name", "wllr"))
	return b
}

// isZeroSpanID returns true if all bytes of id are zero.
func isZeroSpanID(id [8]byte) bool {
	for _, v := range id {
		if v != 0 {
			return false
		}
	}
	return true
}

// encodeSpan encodes a Span proto.
//
// Field numbers per opentelemetry-proto/trace/v1/trace.proto:
//
//	1  trace_id               bytes (16)
//	2  span_id                bytes (8)
//	4  parent_span_id         bytes (8) — omitted if zero
//	5  name                   string
//	6  kind                   varint (SpanKind enum)
//	7  start_time_unix_nano   fixed64 (wire type 1)
//	8  end_time_unix_nano     fixed64 (wire type 1)
//	9  attributes             repeated KeyValue
//	15 status                 Status
func encodeSpan(traceID [16]byte, s spanRecord) []byte {
	var b []byte

	// field 1: trace_id
	b = appendLenDelim(b, 1, traceID[:])

	// field 2: span_id
	b = appendLenDelim(b, 2, s.spanID[:])

	// field 4: parent_span_id (only if non-zero)
	if !isZeroSpanID(s.parentSpanID) {
		b = appendLenDelim(b, 4, s.parentSpanID[:])
	}

	// field 5: name
	b = appendString(b, 5, s.name)

	// field 6: kind
	b = appendVarintField(b, 6, uint64(s.kind))

	// fields 7 & 8: timestamps — fixed64, wire type 1 (little-endian 8 bytes)
	b = appendFixed64Field(b, 7, uint64(s.startNano))
	b = appendFixed64Field(b, 8, uint64(s.endNano))

	// field 9: attributes (one field per KeyValue)
	for _, attr := range s.attrs {
		b = appendLenDelim(b, 9, encodeKeyValue(attr[0], attr[1]))
	}

	// field 15: status (OK)
	b = appendLenDelim(b, 15, encodeStatus(1))

	return b
}

// encodeScopeSpans encodes a ScopeSpans proto:
//
//	field 1: InstrumentationScope
//	field 2: repeated Span
func encodeScopeSpans(traceID [16]byte, spans []spanRecord) []byte {
	var b []byte
	b = appendLenDelim(b, 1, encodeInstrumentationScope())
	for _, s := range spans {
		b = appendLenDelim(b, 2, encodeSpan(traceID, s))
	}
	return b
}

// encodeResourceSpans encodes a ResourceSpans proto:
//
//	field 1: Resource
//	field 2: ScopeSpans
func encodeResourceSpans(traceID [16]byte, spans []spanRecord) []byte {
	var b []byte
	b = appendLenDelim(b, 1, encodeResource())
	b = appendLenDelim(b, 2, encodeScopeSpans(traceID, spans))
	return b
}

// encodeTraceRequest encodes an ExportTraceServiceRequest proto:
//
//	field 1: repeated ResourceSpans
func encodeTraceRequest(traceID [16]byte, spans []spanRecord) []byte {
	if len(spans) == 0 {
		return nil
	}
	var b []byte
	b = appendLenDelim(b, 1, encodeResourceSpans(traceID, spans))
	return b
}
