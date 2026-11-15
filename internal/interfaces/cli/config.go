package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"

	"github.com/romero-project/romero/internal/application/eval"
	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/domain/identity"
)

// Version is the semantic binary version printed by `romero version`.
const Version = "romero v0.1.0-phase01"

const defaultConfigPath = "configs/romero.toml"

type tomlFileConfig struct {
	Server struct {
		Host     string `toml:"host"`
		RESTPort int    `toml:"rest_port"`
	} `toml:"server"`
	Database struct {
		DSN string `toml:"dsn"`
	} `toml:"database"`
}

// ProvisioningService defines the application operations used by CLI commands.
type ProvisioningService interface {
	Bootstrap(ctx context.Context, spec provisioning.BootstrapSpec) (*provisioning.BootstrapOutput, error)
	CreatePrincipal(ctx context.Context, req provisioning.CreatePrincipalInput) (string, provisioning.PrincipalView, error)
	ManageTaskPermission(ctx context.Context, actor identity.AuthContext, cmd provisioning.ManageTaskPermissionCommand) error
}

// APIAuthenticator authenticates caller credentials supplied to local administrative CLI commands.
type APIAuthenticator interface {
	Authenticate(ctx context.Context, rawAPIKey string) (identity.AuthContext, error)
}

// ProvisioningClient groups provisioning and authentication services for a CLI invocation.
type ProvisioningClient struct {
	Service ProvisioningService
	Auth    APIAuthenticator
}

// Migrator applies or inspects embedded Goose database migrations.
type Migrator interface {
	Run(ctx context.Context, dsn, direction string) (int64, error)
}

// Dependencies injects infrastructure adapters into the Cobra CLI command tree.
type Dependencies struct {
	Migrator        Migrator
	OpenProvisioner func(ctx context.Context, dsn string) (ProvisioningClient, func(), error)
	Serve           func(ctx context.Context, dsn, listenAddr string) error
	Eval            func(ctx context.Context, datasetPath string, maxTokens, hops int) (*eval.Report, error)
}

func resolveDSN(flagDSN, configPath string) (string, error) {
	if v := strings.TrimSpace(flagDSN); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(os.Getenv("ROMERO_DSN")); v != "" {
		return v, nil
	}
	path := strings.TrimSpace(configPath)
	if path == "" {
		path = defaultConfigPath
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path comes from explicit operator CLI --config flag
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("database DSN not provided (pass --dsn, set ROMERO_DSN, or provide --config)")
		}
		return "", fmt.Errorf("read config %q: %w", path, err)
	}
	var parsed tomlFileConfig
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("parse config %q: %w", path, err)
	}
	dsn := strings.TrimSpace(parsed.Database.DSN)
	if dsn == "" {
		return "", fmt.Errorf("config %q has empty [database].dsn", path)
	}
	return dsn, nil
}

func resolveAPIKey(flagKey string) string {
	if v := strings.TrimSpace(flagKey); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("ROMERO_API_KEY"))
}

func resolveListenAddr(flagAddr, configPath string) string {
	if v := strings.TrimSpace(flagAddr); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ROMERO_LISTEN_ADDR")); v != "" {
		return v
	}
	path := strings.TrimSpace(configPath)
	if path == "" {
		path = defaultConfigPath
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path comes from explicit operator CLI --config flag
	if err == nil {
		var parsed tomlFileConfig
		if toml.Unmarshal(raw, &parsed) == nil && parsed.Server.RESTPort > 0 {
			host := strings.TrimSpace(parsed.Server.Host)
			if host == "" {
				host = "127.0.0.1"
			}
			return fmt.Sprintf("%s:%d", host, parsed.Server.RESTPort)
		}
	}
	return "127.0.0.1:8420"
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func parseScopes(raw []string) ([]identity.ScopeType, error) {
	out := make([]identity.ScopeType, 0, len(raw))
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			sc := identity.ScopeType(trimmed)
			switch sc {
			case identity.ScopeAgentPrivate,
				identity.ScopeTaskShared,
				identity.ScopeProjectShared,
				identity.ScopeOrgShared,
				identity.ScopeGlobalPublic:
				out = append(out, sc)
			default:
				return nil, fmt.Errorf("unsupported scope %q", trimmed)
			}
		}
	}
	return out, nil
}

func parseUUIDList(raw []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(raw))
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			id, err := uuid.Parse(trimmed)
			if err != nil {
				return nil, fmt.Errorf("invalid UUID %q: %w", trimmed, err)
			}
			out = append(out, id)
		}
	}
	return out, nil
}

func parseTaskGrants(raw []string) ([]provisioning.BootstrapTaskGrant, error) {
	out := make([]provisioning.BootstrapTaskGrant, 0, len(raw))
	for _, item := range raw {
		taskID, modes, ok := strings.Cut(strings.TrimSpace(item), ":")
		taskID = strings.TrimSpace(taskID)
		if !ok || taskID == "" || strings.TrimSpace(modes) == "" {
			return nil, fmt.Errorf("invalid task-grant %q (expected <task_id>:read,write)", item)
		}
		var grant provisioning.BootstrapTaskGrant
		grant.TaskID = taskID
		for _, m := range strings.Split(modes, ",") {
			switch strings.TrimSpace(m) {
			case "read":
				grant.CanRead = true
			case "write":
				grant.CanWrite = true
			default:
				return nil, fmt.Errorf("invalid task-grant permission %q in %q", m, item)
			}
		}
		if !grant.CanRead && !grant.CanWrite {
			return nil, fmt.Errorf("task-grant %q must specify read or write", item)
		}
		out = append(out, grant)
	}
	return out, nil
}

func parseExpiresAt(expiresIn time.Duration) (*time.Time, error) {
	if expiresIn < 0 {
		return nil, fmt.Errorf("%w: --expires-in must be positive", provisioning.ErrInvalidInput)
	}
	if expiresIn == 0 {
		return nil, nil
	}
	t := time.Now().UTC().Add(expiresIn)
	return &t, nil
}
