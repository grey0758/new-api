package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func solveRegistrationChallengeForTest(t *testing.T, challenge *RegistrationChallenge) string {
	t.Helper()
	for nonce := uint64(0); ; nonce++ {
		material := registrationChallengeProofMaterial(
			challenge.ChallengeID,
			challenge.Seed,
			challenge.TargetHash,
			nonce,
		)
		digest := sha256.Sum256([]byte(material))
		if registrationChallengeMeetsDifficulty(digest[:], challenge.Difficulty) {
			return fmt.Sprintf("%s.%s.%d.%s", challenge.Version, challenge.ChallengeID, nonce, hex.EncodeToString(digest[:]))
		}
	}
}

func TestRegistrationChallengeRejectsMissingAndMalformedTokens(t *testing.T) {
	manager := newRegistrationChallengeManager(time.Minute, 1, 10)
	require.ErrorIs(t, manager.consume("alice", ""), ErrRegistrationChallengeInvalid)
	require.ErrorIs(t, manager.consume("alice", "not-a-token"), ErrRegistrationChallengeInvalid)
}

func TestRegistrationChallengeRejectsInvalidProofWithoutConsumingChallenge(t *testing.T) {
	manager := newRegistrationChallengeManager(time.Minute, 1, 10)
	challenge, err := manager.issue("alice")
	require.NoError(t, err)

	invalidToken := fmt.Sprintf("%s.%s.0.%s", challenge.Version, challenge.ChallengeID, string(make([]byte, 64)))
	require.ErrorIs(t, manager.consume("alice", invalidToken), ErrRegistrationChallengeInvalid)

	validToken := solveRegistrationChallengeForTest(t, challenge)
	require.NoError(t, manager.consume("alice", validToken))
}

func TestRegistrationChallengeRejectsExpiredToken(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	manager := newRegistrationChallengeManager(time.Minute, 1, 10)
	manager.now = func() time.Time { return now }
	challenge, err := manager.issue("alice")
	require.NoError(t, err)
	token := solveRegistrationChallengeForTest(t, challenge)

	now = now.Add(time.Minute)
	require.ErrorIs(t, manager.consume("alice", token), ErrRegistrationChallengeInvalid)
}

func TestRegistrationChallengeBindsNormalizedTarget(t *testing.T) {
	manager := newRegistrationChallengeManager(time.Minute, 1, 10)
	challenge, err := manager.issue("  Alice  ")
	require.NoError(t, err)
	token := solveRegistrationChallengeForTest(t, challenge)

	require.ErrorIs(t, manager.consume("bob", token), ErrRegistrationChallengeInvalid)
	require.NoError(t, manager.consume("ALICE", token))
}

func TestRegistrationChallengeIsOneTime(t *testing.T) {
	manager := newRegistrationChallengeManager(time.Minute, 1, 10)
	challenge, err := manager.issue("alice")
	require.NoError(t, err)
	token := solveRegistrationChallengeForTest(t, challenge)

	require.NoError(t, manager.consume("alice", token))
	require.ErrorIs(t, manager.consume("alice", token), ErrRegistrationChallengeInvalid)
}

func TestRegistrationChallengeConcurrentConsumeAllowsExactlyOne(t *testing.T) {
	manager := newRegistrationChallengeManager(time.Minute, 1, 10)
	challenge, err := manager.issue("alice")
	require.NoError(t, err)
	token := solveRegistrationChallengeForTest(t, challenge)

	var successCount atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if manager.consume("alice", token) == nil {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), successCount.Load())
}

func TestRegistrationChallengeCapacityRecoversAfterExpiry(t *testing.T) {
	now := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	manager := newRegistrationChallengeManager(time.Minute, 1, 1)
	manager.now = func() time.Time { return now }

	_, err := manager.issue("alice")
	require.NoError(t, err)
	_, err = manager.issue("bob")
	require.ErrorIs(t, err, ErrRegistrationChallengeUnavailable)

	now = now.Add(time.Minute)
	_, err = manager.issue("bob")
	require.NoError(t, err)
}
