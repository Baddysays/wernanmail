package imapd

import (
	"bytes"
	"io"
	"testing"
)

func TestPeekLiteral_chunkedReadMatchesLen(t *testing.T) {
	payload := bytes.Repeat([]byte("abcdefghij"), 300) // 3000 bytes
	lit := &peekLiteral{b: payload}
	if lit.Len() != len(payload) {
		t.Fatalf("Len=%d want %d", lit.Len(), len(payload))
	}
	var got []byte
	buf := make([]byte, 64)
	for {
		n, err := lit.Read(buf)
		if n > 0 {
			got = append(got, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("read %d bytes, want %d", len(got), len(payload))
	}
	// Discard pass must yield nothing (simulates go-imap writeLiteral).
	extra, err := io.Copy(io.Discard, lit)
	if extra != 0 || err != nil {
		t.Fatalf("extra=%d err=%v", extra, err)
	}
}

func TestPeekLiteral_noWriterTo(t *testing.T) {
	lit := &peekLiteral{b: []byte("x")}
	if _, ok := any(lit).(io.WriterTo); ok {
		t.Fatal("peekLiteral must not implement WriterTo (breaks go-imap CopyN+Discard)")
	}
}
