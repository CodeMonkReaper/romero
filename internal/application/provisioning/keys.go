package provisioning

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/romero-project/romero/internal/domain/identity"
)

// APIKeyPrefixLength is the fixed prefix length stored in principals.api_key_prefix.
const APIKeyPrefixLength = 20

const (
	apiKeyHeader    = "rmr"
	idSegmentHexLen = 8
	secretBytesLen  = 16
	secretHexLen    = secretBytesLen * 2
)

// GenerateAPIKey generates a cryptographic key formatted as rmr_<type>_<id8>_<secret32>
// along with its 20-character lookup prefix and lowercase SHA-256 hex digest.
func GenerateAPIKey(principalType identity.PrincipalType, principalID uuid.UUID) (rawKey, prefix, sha256Hex string, err error) {
	if !validPrincipalType(principalType) {
		return "", "", "", fmt.Errorf("%w: unsupported principal_type %q", ErrInvalidInput, principalType)
	}
	if principalID == uuid.Nil {
		return "", "", "", fmt.Errorf("%w: principal_id is required", ErrInvalidInput)
	}

	secret := make([]byte, secretBytesLen)
	if _, err := rand.Read(secret); err != nil {
		return "", "", "", fmt.Errorf("generate api key entropy: %w", err)
	}

	id8 := hex.EncodeToString(principalID[:4])
	secret32 := hex.EncodeToString(secret)
	rawKey = fmt.Sprintf("%s_%s_%s_%s", apiKeyHeader, principalType, id8, secret32)
	prefix = rawKey[:APIKeyPrefixLength]
	sum := sha256.Sum256([]byte(rawKey))
	sha256Hex = hex.EncodeToString(sum[:])
	return rawKey, prefix, sha256Hex, nil
}

// ParseAPIKey validates structural format of rawKey and returns its lookup prefix and SHA-256 digest.
func ParseAPIKey(rawKey string) (prefix string, sha256Hex string, ok bool) {
	if rawKey == "" || strings.TrimSpace(rawKey) != rawKey || len(rawKey) < APIKeyPrefixLength {
		return "", "", false
	}
	parts := strings.Split(rawKey, "_")
	if len(parts) != 4 || parts[0] != apiKeyHeader {
		return "", "", false
	}
	if !validPrincipalType(identity.PrincipalType(parts[1])) {
		return "", "", false
	}
	if !isLowerHex(parts[2], idSegmentHexLen) || !isLowerHex(parts[3], secretHexLen) {
		return "", "", false
	}

	sum := sha256.Sum256([]byte(rawKey))
	return rawKey[:APIKeyPrefixLength], hex.EncodeToString(sum[:]), true
}

func validPrincipalType(pt identity.PrincipalType) bool {
	switch pt {
	case identity.PrincipalUser, identity.PrincipalService, identity.PrincipalOrchestrator, identity.PrincipalAgent:
		return true
	default:
		return false
	}
}

func validScopeType(s identity.ScopeType) bool {
	switch s {
	case identity.ScopeAgentPrivate,
		identity.ScopeTaskShared,
		identity.ScopeProjectShared,
		identity.ScopeOrgShared,
		identity.ScopeGlobalPublic:
		return true
	default:
		return false
	}
}

func isLowerHex(s string, wantLen int) bool {
	if len(s) != wantLen {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
