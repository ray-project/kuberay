package eventserver

import (
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gzipBytes(t *testing.T, src []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, err := gw.Write(src)
	require.NoError(t, err)
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

func TestIsValidEventFile(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		want     bool
	}{
		{"legacy plain", "node-abcd-2026-05-13-10", true},
		{"new jsonl", "node-abcd-2026-05-13-10.jsonl", true},
		{"new jsonl gz", "node-abcd-2026-05-13-10.jsonl.gz", true},
		{"nano suffix jsonl.gz", "node-1-2026-05-13-10-1747123200000000000.jsonl.gz", true},
		{"nano suffix jsonl", "node-1-2026-05-13-10-1747123200000000000.jsonl", true},
		{"directory", "some-dir/", false},
		{"unrelated", "foo.txt", false},
		{"wrong hour", "node-abcd-2026-05-13-1", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isValidEventFile(tc.fileName))
		})
	}
}

func TestDecodeEventFileBytes_JSONArrayLegacy(t *testing.T) {
	raw := []byte(`[{"eventId":"a","x":1},{"eventId":"b","x":2}]`)
	out, err := decodeEventFileBytes("node-2026-05-13-10", raw, true)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "a", out[0]["eventId"])
	assert.Equal(t, "b", out[1]["eventId"])
}

func TestDecodeEventFileBytes_JSONLPlain(t *testing.T) {
	raw := []byte(`{"eventId":"a"}
{"eventId":"b"}

{"eventId":"c"}
`)
	out, err := decodeEventFileBytes("node-2026-05-13-10.jsonl", raw, true)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "a", out[0]["eventId"])
	assert.Equal(t, "b", out[1]["eventId"])
	assert.Equal(t, "c", out[2]["eventId"])
}

func TestDecodeEventFileBytes_JSONLGzip(t *testing.T) {
	plain := []byte(`{"eventId":"a"}` + "\n" + `{"eventId":"b"}` + "\n")
	gz := gzipBytes(t, plain)
	out, err := decodeEventFileBytes("node-2026-05-13-10.jsonl.gz", gz, true)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "a", out[0]["eventId"])
	assert.Equal(t, "b", out[1]["eventId"])
}

func TestDecodeEventFileBytes_JSONArrayGzip(t *testing.T) {
	plain := []byte(`[{"eventId":"x"}]`)
	gz := gzipBytes(t, plain)
	out, err := decodeEventFileBytes("legacy-2026-05-13-10.gz", gz, true)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "x", out[0]["eventId"])
}

func TestDecodeEventFileBytes_EmptyBytes(t *testing.T) {
	out, err := decodeEventFileBytes("empty.jsonl", []byte{}, true)
	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestDecodeEventFileBytes_MalformedJSONLLinesSkipped(t *testing.T) {
	raw := []byte(`{"eventId":"a"}
not a json line
{"eventId":"b"}
`)
	out, err := decodeEventFileBytes("malformed.jsonl", raw, true)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "a", out[0]["eventId"])
	assert.Equal(t, "b", out[1]["eventId"])
}

func TestDecodeEventFileBytes_GzipCorrupt(t *testing.T) {
	_, err := decodeEventFileBytes("bad.jsonl.gz", []byte("not gzipped"), true)
	require.Error(t, err)
}

// --- Tests with eventDecodingEnabled=false ---
//
// When the switch is off, format auto-detection (JSON array vs JSONL) is still
// applied so the historyserver stays forward-compatible with new collectors
// that emit JSONL even when compression is disabled. Only gzip decompression
// is gated by the switch.

// When the decoding switch is off, gzip files are NOT decompressed;
// raw gzip bytes are not valid JSON array nor parseable JSONL, so an error or
// empty result is returned. We assert no events are returned.
func TestDecodeEventFileBytes_DecodingDisabled_GzipNotDecompressed(t *testing.T) {
	plain := []byte(`{"eventId":"a"}` + "\n" + `{"eventId":"b"}` + "\n")
	gz := gzipBytes(t, plain)
	out, err := decodeEventFileBytes("node-2026-05-13-10.jsonl.gz", gz, false)
	// Either the parser surfaces an error or returns no events; in any case
	// the original gzip-encoded events MUST NOT be decoded when the switch is off.
	if err == nil {
		assert.Empty(t, out, "gzip bytes must not yield decoded events when switch is off")
	}
}

// When the decoding switch is off, JSON array input is still parsed correctly
// (backward compatibility with legacy data).
func TestDecodeEventFileBytes_DecodingDisabled_JSONArrayParsed(t *testing.T) {
	raw := []byte(`[{"eventId":"a","x":1},{"eventId":"b","x":2}]`)
	out, err := decodeEventFileBytes("node-2026-05-13-10", raw, false)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "a", out[0]["eventId"])
	assert.Equal(t, "b", out[1]["eventId"])
}

// When the decoding switch is off, plain (uncompressed) JSONL must still be
// parsed correctly so historyserver stays forward-compatible with new
// collectors that emit JSONL even when compression is disabled.
func TestDecodeEventFileBytes_DecodingDisabled_PlainJSONLParsed(t *testing.T) {
	raw := []byte(`{"eventId":"a"}
{"eventId":"b"}

{"eventId":"c"}
`)
	out, err := decodeEventFileBytes("node-2026-05-13-10.jsonl", raw, false)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "a", out[0]["eventId"])
	assert.Equal(t, "b", out[1]["eventId"])
	assert.Equal(t, "c", out[2]["eventId"])
}

// Empty input still returns nil, regardless of switch state.
func TestDecodeEventFileBytes_DecodingDisabled_EmptyBytes(t *testing.T) {
	out, err := decodeEventFileBytes("empty.jsonl", []byte{}, false)
	require.NoError(t, err)
	assert.Nil(t, out)
}
