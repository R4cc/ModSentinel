package secrets

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand: %v", err)
	}
	keyHex := hex.EncodeToString(key)
	os.Setenv("SECRET_KEYSET", fmt.Sprintf(`{"primary":"k1","keys":{"k1":"%s"}}`, keyHex))
	t.Cleanup(func() { os.Unsetenv("SECRET_KEYSET") })

	mgr, err := Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	ct, iv, keyID, err := mgr.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if keyID != "k1" {
		t.Fatalf("expected keyID k1, got %s", keyID)
	}
	pt, err := mgr.Decrypt(ct, iv, keyID)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(pt) != "hello" {
		t.Fatalf("roundtrip mismatch: %s", pt)
	}
}
