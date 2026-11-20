package pass

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ProtonMail/gopenpgp/v3/crypto"
)

var (
	// ErrLocked indicates that the private key has not been unlocked yet.
	ErrLocked = errors.New("приватный PGP-ключ заблокирован: требуется мастер-пароль")
	// ErrNoKey indicates that no private key is configured in the keyring.
	ErrNoKey = errors.New("PGP-ключ не найден в связке приложения")
)

// KeyMeta describes a stored key without exposing secret material.
type KeyMeta struct {
	Fingerprint string `json:"fingerprint"`
	KeyID       string `json:"key_id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	HasSecret   bool   `json:"has_secret"`
}

type storedKeyRecord struct {
	KeyMeta
	PublicArmor string `json:"public_armor"`
	SecretArmor string `json:"secret_armor,omitempty"`
}

// Keyring manages OpenPGP keys on disk and keeps an unlocked private key
// in memory for the active session.
type Keyring struct {
	Dir string

	mu       sync.RWMutex
	unlocked map[string]*crypto.Key // fingerprint -> unlocked private key
}

// DefaultKeyringDir returns the default directory for GoPass keys inside the
// user's OS config directory.
func DefaultKeyringDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("не удалось определить каталог конфигурации: %w", err)
	}
	return filepath.Join(base, "go-pass", "keyring"), nil
}

// OpenKeyring opens (or initializes) a Keyring at dir.
func OpenKeyring(dir string) (*Keyring, error) {
	if dir == "" {
		var err error
		dir, err = DefaultKeyringDir()
		if err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("не удалось создать каталог ключей %s: %w", dir, err)
	}
	return &Keyring{
		Dir:      dir,
		unlocked: make(map[string]*crypto.Key),
	}, nil
}

func (kr *Keyring) indexPath() string {
	return filepath.Join(kr.Dir, "keys.json")
}

func (kr *Keyring) loadRecords() ([]storedKeyRecord, error) {
	data, err := os.ReadFile(kr.indexPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать связку ключей: %w", err)
	}
	var records []storedKeyRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("повреждённый файл связки ключей: %w", err)
	}
	return records, nil
}

func (kr *Keyring) saveRecords(records []storedKeyRecord) error {
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	tmp := kr.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("не удалось сохранить связку ключей: %w", err)
	}
	return os.Rename(tmp, kr.indexPath())
}

// ListKeys returns metadata for all keys stored in the keyring.
func (kr *Keyring) ListKeys() ([]KeyMeta, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	records, err := kr.loadRecords()
	if err != nil {
		return nil, err
	}
	out := make([]KeyMeta, len(records))
	for i, r := range records {
		out[i] = r.KeyMeta
	}
	return out, nil
}

// HasPrivateKey reports whether at least one private key is stored.
func (kr *Keyring) HasPrivateKey() bool {
	keys, err := kr.ListKeys()
	if err != nil {
		return false
	}
	for _, k := range keys {
		if k.HasSecret {
			return true
		}
	}
	return false
}

// GenerateAndSave creates a new key pair, stores it in the keyring, and
// caches the unlocked key in memory for immediate use.
func (kr *Keyring) GenerateAndSave(name, email, passphrase string) (KeyMeta, error) {
	kp, err := GenerateKeyPair(name, email, passphrase)
	if err != nil {
		return KeyMeta{}, err
	}
	if err := kr.ImportArmor(kp.SecretArmor); err != nil {
		return KeyMeta{}, err
	}
	if err := kr.Unlock(passphrase); err != nil {
		return KeyMeta{}, err
	}
	return KeyMeta{
		Fingerprint: kp.Fingerprint,
		KeyID:       kp.KeyID,
		Name:        kp.Name,
		Email:       kp.Email,
		HasSecret:   true,
	}, nil
}

// ImportArmor imports an armored public or private key into the keyring.
func (kr *Keyring) ImportArmor(armor string) error {
	k, err := crypto.NewKeyFromArmored(strings.TrimSpace(armor))
	if err != nil {
		return fmt.Errorf("не удалось распознать PGP-ключ: %w", err)
	}

	fp := strings.ToUpper(k.GetFingerprint())
	kid := strings.ToUpper(k.GetHexKeyID())

	var name, email string
	for _, id := range k.GetEntity().Identities {
		name = id.UserId.Name
		email = id.UserId.Email
		break
	}

	var pubArmor, secArmor string
	if k.IsPrivate() {
		secArmor, err = k.Armor()
		if err != nil {
			return err
		}
		pub, err := k.ToPublic()
		if err != nil {
			return err
		}
		pubArmor, err = pub.Armor()
		if err != nil {
			return err
		}
	} else {
		pubArmor, err = k.Armor()
		if err != nil {
			return err
		}
	}

	kr.mu.Lock()
	defer kr.mu.Unlock()

	records, err := kr.loadRecords()
	if err != nil {
		return err
	}

	rec := storedKeyRecord{
		KeyMeta: KeyMeta{
			Fingerprint: fp,
			KeyID:       kid,
			Name:        name,
			Email:       email,
			HasSecret:   k.IsPrivate(),
		},
		PublicArmor: pubArmor,
		SecretArmor: secArmor,
	}

	replaced := false
	for i := range records {
		if records[i].Fingerprint == fp {
			// Не затираем существующий приватный ключ при повторном импорте только публичного.
			if !rec.HasSecret && records[i].HasSecret {
				rec.HasSecret = true
				rec.SecretArmor = records[i].SecretArmor
			}
			records[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		records = append(records, rec)
	}
	return kr.saveRecords(records)
}

// ExportPublicArmor returns the armored public key for the given fingerprint.
func (kr *Keyring) ExportPublicArmor(fingerprint string) (string, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	records, err := kr.loadRecords()
	if err != nil {
		return "", err
	}
	fp := strings.ToUpper(strings.TrimSpace(fingerprint))
	for _, r := range records {
		if r.Fingerprint == fp {
			return r.PublicArmor, nil
		}
	}
	return "", ErrNoKey
}

// PublicArmors returns all armored public keys in the keyring.
func (kr *Keyring) PublicArmors() ([]string, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	records, err := kr.loadRecords()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(records))
	for _, r := range records {
		if r.PublicArmor != "" {
			out = append(out, r.PublicArmor)
		}
	}
	return out, nil
}

// Unlock decrypts all stored private keys matching passphrase and keeps them in memory.
func (kr *Keyring) Unlock(passphrase string) error {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	records, err := kr.loadRecords()
	if err != nil {
		return err
	}

	hasAnySecret := false
	unlockedCount := 0
	var lastErr error

	for _, r := range records {
		if !r.HasSecret || r.SecretArmor == "" {
			continue
		}
		hasAnySecret = true
		uk, err := UnlockPrivateKey(r.SecretArmor, passphrase)
		if err != nil {
			lastErr = err
			continue
		}
		if old, ok := kr.unlocked[r.Fingerprint]; ok && old != nil {
			old.ClearPrivateParams()
		}
		kr.unlocked[r.Fingerprint] = uk
		unlockedCount++
	}

	if !hasAnySecret {
		return ErrNoKey
	}
	if unlockedCount == 0 {
		if lastErr != nil {
			return lastErr
		}
		return ErrLocked
	}
	return nil
}

// Lock clears all unlocked private keys from memory.
func (kr *Keyring) Lock() {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	for fp, k := range kr.unlocked {
		if k != nil {
			k.ClearPrivateParams()
		}
		delete(kr.unlocked, fp)
	}
}

// IsUnlocked reports whether at least one private key is currently unlocked in memory.
func (kr *Keyring) IsUnlocked() bool {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return len(kr.unlocked) > 0
}

// UnlockedKeys returns all currently unlocked private keys.
func (kr *Keyring) UnlockedKeys() []*crypto.Key {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	out := make([]*crypto.Key, 0, len(kr.unlocked))
	for _, k := range kr.unlocked {
		if k != nil {
			out = append(out, k)
		}
	}
	return out
}
