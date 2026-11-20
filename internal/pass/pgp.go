package pass

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
)

// KeyPair holds an armored OpenPGP key pair along with its metadata.
type KeyPair struct {
	Fingerprint string
	KeyID       string
	Name        string
	Email       string
	PublicArmor string
	SecretArmor string
}

// GenerateKeyPair creates a new OpenPGP key pair protected by passphrase.
func GenerateKeyPair(name, email, passphrase string) (*KeyPair, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("имя владельца ключа не может быть пустым")
	}
	if strings.TrimSpace(passphrase) == "" {
		return nil, errors.New("мастер-пароль ключа не может быть пустым")
	}

	pgp := crypto.PGP()
	gen := pgp.KeyGeneration().
		AddUserId(name, email).
		New()

	key, err := gen.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("не удалось сгенерировать PGP-ключ: %w", err)
	}
	defer key.ClearPrivateParams()

	lockedKey, err := pgp.LockKey(key, []byte(passphrase))
	if err != nil {
		return nil, fmt.Errorf("не удалось зашифровать приватный ключ: %w", err)
	}

	secArmor, err := lockedKey.Armor()
	if err != nil {
		return nil, fmt.Errorf("не удалось сериализовать приватный ключ: %w", err)
	}

	pubKey, err := lockedKey.ToPublic()
	if err != nil {
		return nil, fmt.Errorf("не удалось извлечь публичный ключ: %w", err)
	}
	pubArmor, err := pubKey.Armor()
	if err != nil {
		return nil, fmt.Errorf("не удалось сериализовать публичный ключ: %w", err)
	}

	return &KeyPair{
		Fingerprint: strings.ToUpper(lockedKey.GetFingerprint()),
		KeyID:       strings.ToUpper(lockedKey.GetHexKeyID()),
		Name:        name,
		Email:       email,
		PublicArmor: pubArmor,
		SecretArmor: secArmor,
	}, nil
}

// UnlockPrivateKey parses an armored private key and unlocks it with passphrase.
func UnlockPrivateKey(secretArmor, passphrase string) (*crypto.Key, error) {
	lockedKey, err := crypto.NewKeyFromArmored(secretArmor)
	if err != nil {
		return nil, fmt.Errorf("повреждённый приватный ключ: %w", err)
	}
	if !lockedKey.IsPrivate() {
		return nil, errors.New("переданный ключ не является приватным")
	}

	isLocked, err := lockedKey.IsLocked()
	if err != nil {
		return nil, fmt.Errorf("не удалось проверить блокировку ключа: %w", err)
	}
	if !isLocked {
		return lockedKey, nil
	}

	unlocked, err := lockedKey.Unlock([]byte(passphrase))
	if err != nil {
		return nil, fmt.Errorf("неверный мастер-пароль ключа: %w", err)
	}
	return unlocked, nil
}

// ResolveRecipients finds the nearest .gpg-id file starting from the entry's
// directory up to storeDir and returns non-empty, non-comment recipient IDs.
func ResolveRecipients(storeDir, entryName string) ([]string, error) {
	root := filepath.Clean(storeDir)
	dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(entryName)))

	for {
		gpgIDPath := filepath.Join(dir, ".gpg-id")
		if data, err := os.ReadFile(gpgIDPath); err == nil {
			recipients := parseGPGID(string(data))
			if len(recipients) == 0 {
				return nil, fmt.Errorf("файл %s пуст", gpgIDPath)
			}
			return recipients, nil
		}
		if dir == root || !strings.HasPrefix(dir, root+string(filepath.Separator)) {
			break
		}
		dir = filepath.Dir(dir)
	}
	return nil, fmt.Errorf("не найден файл .gpg-id в %s", storeDir)
}

func parseGPGID(raw string) []string {
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// BuildRecipientKeyRing matches recipient identifiers from .gpg-id (fingerprint,
// hex key ID, or email) against available armored public keys.
func BuildRecipientKeyRing(recipients []string, armoredKeys []string) (*crypto.KeyRing, error) {
	parsed := make([]*crypto.Key, 0, len(armoredKeys))
	for _, armor := range armoredKeys {
		k, err := crypto.NewKeyFromArmored(armor)
		if err != nil {
			continue
		}
		if k.IsPrivate() {
			pub, err := k.ToPublic()
			if err != nil {
				continue
			}
			k = pub
		}
		parsed = append(parsed, k)
	}

	ring, err := crypto.NewKeyRing(nil)
	if err != nil {
		return nil, err
	}

	for _, rec := range recipients {
		matched := findMatchingKey(rec, parsed)
		if matched == nil {
			return nil, fmt.Errorf("не найден публичный ключ для получателя %q из .gpg-id", rec)
		}
		if err := ring.AddKey(matched); err != nil {
			return nil, fmt.Errorf("не удалось добавить ключ %q в связку: %w", rec, err)
		}
	}
	return ring, nil
}

func findMatchingKey(recipient string, keys []*crypto.Key) *crypto.Key {
	needle := strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(recipient), "0X"))
	needleLower := strings.ToLower(strings.TrimSpace(recipient))

	for _, k := range keys {
		fp := strings.ToUpper(k.GetFingerprint())
		kid := strings.ToUpper(k.GetHexKeyID())

		if needle != "" && (fp == needle || strings.HasSuffix(fp, needle) || kid == needle) {
			return k
		}
		for _, id := range k.GetEntity().Identities {
			if strings.Contains(strings.ToLower(id.Name), needleLower) {
				return k
			}
		}
	}
	return nil
}

// EncryptBinary encrypts plaintext into standard binary OpenPGP format
// compatible with `pass` and `gpg -d`.
func EncryptBinary(plaintext []byte, recipients *crypto.KeyRing) ([]byte, error) {
	pgp := crypto.PGP()
	encHandle, err := pgp.Encryption().Recipients(recipients).New()
	if err != nil {
		return nil, fmt.Errorf("ошибка инициализации шифрования: %w", err)
	}
	msg, err := encHandle.Encrypt(plaintext)
	if err != nil {
		return nil, fmt.Errorf("ошибка шифрования записи: %w", err)
	}
	return msg.Bytes(), nil
}

// DecryptBinary decrypts a binary OpenPGP ciphertext using an unlocked private key.
func DecryptBinary(ciphertext []byte, unlockedKey *crypto.Key) ([]byte, error) {
	pgp := crypto.PGP()
	decHandle, err := pgp.Decryption().DecryptionKey(unlockedKey).New()
	if err != nil {
		return nil, fmt.Errorf("ошибка инициализации расшифровки: %w", err)
	}
	decrypted, err := decHandle.Decrypt(ciphertext, crypto.Bytes)
	if err != nil {
		return nil, fmt.Errorf("не удалось расшифровать запись: %w", err)
	}
	return decrypted.Bytes(), nil
}
