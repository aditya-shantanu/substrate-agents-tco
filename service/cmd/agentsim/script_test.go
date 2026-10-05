package main

import (
	"bytes"
	"testing"
)

// The glutton proto package is internal to substrate, so the request bodies
// are hand-encoded; pin the wire layout of the two messages script mode adds
// (field numbers from substrate/internal/proto/glutton/glutton.proto).
func TestBurnCPUBody(t *testing.T) {
	// duration_ms=1 varint 500 (0xf4 0x03), parallelism=2 varint 2
	want := []byte{0x08, 0xf4, 0x03, 0x10, 0x02}
	if got := burnCPUBody(500, 2); !bytes.Equal(got, want) {
		t.Errorf("burnCPUBody = % x, want % x", got, want)
	}
	// parallelism 0 is the proto3 default and must be omitted
	if got := burnCPUBody(1, 0); !bytes.Equal(got, []byte{0x08, 0x01}) {
		t.Errorf("burnCPUBody(1,0) = % x", got)
	}
}

func TestIngestBody(t *testing.T) {
	payload := []byte{0xaa, 0xbb, 0xcc}
	got := ingestBody("repo", payload)
	want := append([]byte{0x0a, 0x04, 'r', 'e', 'p', 'o', 0x12, 0x03}, payload...)
	if !bytes.Equal(got, want) {
		t.Errorf("ingestBody = % x, want % x", got, want)
	}
	// length prefix must be a varint for payloads past 127 bytes
	big := make([]byte, 300)
	got = ingestBody("k", big)
	if got[3] != 0x12 || got[4] != 0xac || got[5] != 0x02 || len(got) != 6+300 {
		t.Errorf("ingestBody 300-byte length prefix wrong: % x", got[:8])
	}
}

func TestReadDiskModeBody(t *testing.T) {
	if got := readDiskModeBody("f", readModeData); !bytes.Equal(got, []byte{0x0a, 0x01, 'f'}) {
		t.Errorf("READ_MODE_DATA must omit the zero enum: % x", got)
	}
	if got := readDiskModeBody("f", readModeDigest); !bytes.Equal(got, []byte{0x0a, 0x01, 'f', 0x10, 0x01}) {
		t.Errorf("digest body = % x", got)
	}
	if !bytes.Equal(readDiskBody("f"), readDiskModeBody("f", readModeDigest)) {
		t.Error("legacy readDiskBody must stay digest-only")
	}
}

func TestWriteRAMOverwriteMode(t *testing.T) {
	got := writeRAMBody("heap", "8388608", writeModeOverwrite)
	if got[len(got)-2] != 0x18 || got[len(got)-1] != 0x01 {
		t.Errorf("write_mode OVERWRITE should encode as field 3 = 1, tail % x", got[len(got)-2:])
	}
}
