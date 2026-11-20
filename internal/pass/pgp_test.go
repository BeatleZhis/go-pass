package pass

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPGPRoundTripAndKeyLock(t *testing.T) {
	kp, err := GenerateKeyPair("Test User", "test@example.com", "master-pass-123")
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if kp.Fingerprint == "" || kp.KeyID == "" {
		t.Fatalf("пустой fingerprint или keyID: %+v", kp)
	}

	// Неверный пароль должен отклоняться.
	if _, err := UnlockPrivateKey(kp.SecretArmor, "wrong-pass"); err == nil {
		t.Fatal("ожидалась ошибка при неверном мастер-пароле")
	}

	// Верный пароль разблокирует ключ.
	unlocked, err := UnlockPrivateKey(kp.SecretArmor, "master-pass-123")
	if err != nil {
		t.Fatalf("UnlockPrivateKey: %v", err)
	}
	defer unlocked.ClearPrivateParams()

	ring, err := BuildRecipientKeyRing([]string{kp.Fingerprint}, []string{kp.PublicArmor})
	if err != nil {
		t.Fatalf("BuildRecipientKeyRing: %v", err)
	}

	want := "s3cr3t-P@ssw0rd\nlogin: alice\nurl: https://example.com\n"
	cipher, err := EncryptBinary([]byte(want), ring)
	if err != nil {
		t.Fatalf("EncryptBinary: %v", err)
	}

	got, err := DecryptBinary(cipher, unlocked)
	if err != nil {
		t.Fatalf("DecryptBinary: %v", err)
	}
	if string(got) != want {
		t.Errorf("расшифрованный текст = %q, want %q", string(got), want)
	}
}

func TestResolveRecipientsHierarchy(t *testing.T) {
	dir := t.TempDir()

	// Корневой .gpg-id
	if err := os.WriteFile(filepath.Join(dir, ".gpg-id"), []byte("# root key\nAABBCCDD11223344\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Переопределённый .gpg-id в подпапке work
	workDir := filepath.Join(dir, "work")
	if err := os.MkdirAll(filepath.Join(workDir, "aws"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, ".gpg-id"), []byte("work@example.com\nsecond-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	gotRoot, err := ResolveRecipients(dir, "personal/mail")
	if err != nil || len(gotRoot) != 1 || gotRoot[0] != "AABBCCDD11223344" {
		t.Fatalf("ResolveRecipients(personal/mail) = %v, err=%v", gotRoot, err)
	}

	gotWork, err := ResolveRecipients(dir, "work/aws/root")
	if err != nil || len(gotWork) != 2 || gotWork[0] != "work@example.com" || gotWork[1] != "second-key" {
		t.Fatalf("ResolveRecipients(work/aws/root) = %v, err=%v", gotWork, err)
	}
}
