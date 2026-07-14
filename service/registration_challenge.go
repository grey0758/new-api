package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	RegistrationChallengeVersion = "newapi-register-v1"

	defaultRegistrationChallengeTTL        = 2 * time.Minute
	defaultRegistrationChallengeDifficulty = 3
	defaultRegistrationChallengeCapacity   = 10000
	registrationChallengeCleanupInterval   = 30 * time.Second
)

var (
	ErrRegistrationChallengeInvalid     = errors.New("registration challenge is invalid, expired, or already used")
	ErrRegistrationChallengeUnavailable = errors.New("registration challenge service is temporarily unavailable")
)

type RegistrationChallenge struct {
	Version     string `json:"version"`
	ChallengeID string `json:"challengeId"`
	Seed        string `json:"seed"`
	TargetHash  string `json:"targetHash"`
	Difficulty  int    `json:"difficulty"`
	ExpiresAt   int64  `json:"expiresAt"`
	ExpiresIn   int64  `json:"expiresIn"`
}

type registrationChallengeRecord struct {
	seed       string
	targetHash [sha256.Size]byte
	difficulty int
	expiresAt  time.Time
}

type registrationChallengeManager struct {
	mu          sync.Mutex
	challenges  map[string]registrationChallengeRecord
	now         func() time.Time
	ttl         time.Duration
	difficulty  int
	maxActive   int
	nextCleanup time.Time
}

func newRegistrationChallengeManager(ttl time.Duration, difficulty int, maxActive int) *registrationChallengeManager {
	return &registrationChallengeManager{
		challenges: make(map[string]registrationChallengeRecord),
		now:        time.Now,
		ttl:        ttl,
		difficulty: difficulty,
		maxActive:  maxActive,
	}
}

var defaultRegistrationChallengeManager = newRegistrationChallengeManager(
	defaultRegistrationChallengeTTL,
	defaultRegistrationChallengeDifficulty,
	defaultRegistrationChallengeCapacity,
)

func IssueRegistrationChallenge(target string) (*RegistrationChallenge, error) {
	return defaultRegistrationChallengeManager.issue(target)
}

func ConsumeRegistrationChallenge(target string, challengeToken string) error {
	return defaultRegistrationChallengeManager.consume(target, challengeToken)
}

func (m *registrationChallengeManager) issue(target string) (*RegistrationChallenge, error) {
	targetHash, ok := registrationChallengeTargetHash(target)
	if !ok {
		return nil, ErrRegistrationChallengeInvalid
	}

	now := m.now().UTC()
	expiresAt := now.Add(m.ttl)
	seed, err := randomRegistrationChallengeComponent()
	if err != nil {
		return nil, fmt.Errorf("%w: generate seed", ErrRegistrationChallengeUnavailable)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.nextCleanup.IsZero() || !now.Before(m.nextCleanup) || len(m.challenges) >= m.maxActive {
		m.deleteExpiredLocked(now)
		m.nextCleanup = now.Add(registrationChallengeCleanupInterval)
	}
	if len(m.challenges) >= m.maxActive {
		return nil, ErrRegistrationChallengeUnavailable
	}

	var challengeID string
	for attempt := 0; attempt < 3; attempt++ {
		challengeID, err = randomRegistrationChallengeComponent()
		if err != nil {
			return nil, fmt.Errorf("%w: generate id", ErrRegistrationChallengeUnavailable)
		}
		if _, exists := m.challenges[challengeID]; !exists {
			break
		}
		challengeID = ""
	}
	if challengeID == "" {
		return nil, ErrRegistrationChallengeUnavailable
	}

	m.challenges[challengeID] = registrationChallengeRecord{
		seed:       seed,
		targetHash: targetHash,
		difficulty: m.difficulty,
		expiresAt:  expiresAt,
	}

	return &RegistrationChallenge{
		Version:     RegistrationChallengeVersion,
		ChallengeID: challengeID,
		Seed:        seed,
		TargetHash:  hex.EncodeToString(targetHash[:]),
		Difficulty:  m.difficulty,
		ExpiresAt:   expiresAt.Unix(),
		ExpiresIn:   int64(m.ttl / time.Second),
	}, nil
}

func (m *registrationChallengeManager) consume(target string, challengeToken string) error {
	version, challengeID, nonce, suppliedDigest, ok := parseRegistrationChallengeToken(challengeToken)
	if !ok || version != RegistrationChallengeVersion {
		return ErrRegistrationChallengeInvalid
	}
	targetHash, ok := registrationChallengeTargetHash(target)
	if !ok {
		return ErrRegistrationChallengeInvalid
	}

	now := m.now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()

	record, exists := m.challenges[challengeID]
	if !exists {
		return ErrRegistrationChallengeInvalid
	}
	if !now.Before(record.expiresAt) {
		delete(m.challenges, challengeID)
		return ErrRegistrationChallengeInvalid
	}
	if subtle.ConstantTimeCompare(targetHash[:], record.targetHash[:]) != 1 {
		return ErrRegistrationChallengeInvalid
	}

	targetHashHex := hex.EncodeToString(record.targetHash[:])
	material := registrationChallengeProofMaterial(challengeID, record.seed, targetHashHex, nonce)
	expectedDigest := sha256.Sum256([]byte(material))
	if subtle.ConstantTimeCompare(suppliedDigest, expectedDigest[:]) != 1 {
		return ErrRegistrationChallengeInvalid
	}
	if !registrationChallengeMeetsDifficulty(expectedDigest[:], record.difficulty) {
		return ErrRegistrationChallengeInvalid
	}

	delete(m.challenges, challengeID)
	return nil
}

func (m *registrationChallengeManager) deleteExpiredLocked(now time.Time) {
	for challengeID, record := range m.challenges {
		if !now.Before(record.expiresAt) {
			delete(m.challenges, challengeID)
		}
	}
}

func registrationChallengeTargetHash(target string) ([sha256.Size]byte, bool) {
	normalized := strings.ToLower(strings.TrimSpace(target))
	if normalized == "" {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256([]byte(normalized)), true
}

func registrationChallengeProofMaterial(challengeID string, seed string, targetHash string, nonce uint64) string {
	return strings.Join([]string{
		RegistrationChallengeVersion,
		challengeID,
		seed,
		targetHash,
		strconv.FormatUint(nonce, 10),
	}, ":")
}

func parseRegistrationChallengeToken(token string) (string, string, uint64, []byte, bool) {
	if len(token) == 0 || len(token) > 256 {
		return "", "", 0, nil, false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 || len(parts[1]) != 22 || len(parts[2]) == 0 || len(parts[2]) > 20 || len(parts[3]) != sha256.Size*2 {
		return "", "", 0, nil, false
	}
	if _, err := base64.RawURLEncoding.DecodeString(parts[1]); err != nil {
		return "", "", 0, nil, false
	}
	nonce, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return "", "", 0, nil, false
	}
	digest, err := hex.DecodeString(parts[3])
	if err != nil || len(digest) != sha256.Size {
		return "", "", 0, nil, false
	}
	return parts[0], parts[1], nonce, digest, true
}

func registrationChallengeMeetsDifficulty(digest []byte, difficulty int) bool {
	if difficulty < 1 || difficulty > sha256.Size*2 {
		return false
	}
	fullZeroBytes := difficulty / 2
	for i := 0; i < fullZeroBytes; i++ {
		if digest[i] != 0 {
			return false
		}
	}
	if difficulty%2 == 1 && digest[fullZeroBytes]>>4 != 0 {
		return false
	}
	return true
}

func randomRegistrationChallengeComponent() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
