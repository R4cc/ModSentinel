package logx

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/rs/zerolog"
)

func TestRedactor(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(NewRedactor(&buf))
	logger.Info().Str("access_token", "abc123").Msg("test")
	tmp := t.TempDir()
	file := tmp + "/log.txt"
	if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// grep should not find the raw token
	cmd := exec.Command("grep", "abc123", file)
	if err := cmd.Run(); err == nil {
		t.Fatalf("token leaked to log: %s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("***redacted***")) {
		t.Fatalf("redacted marker missing: %s", buf.String())
	}
}

func TestSecretHelper(t *testing.T) {
	got := Secret("abcd")
	if got == "abcd" || !bytes.Contains([]byte(got), []byte("***redacted***")) {
		t.Fatalf("unexpected output: %s", got)
	}
	if !bytes.Contains([]byte(got), []byte("4")) {
		t.Fatalf("missing length: %s", got)
	}
}
