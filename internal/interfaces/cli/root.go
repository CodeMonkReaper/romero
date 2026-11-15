// Package cli implements Cobra administrative commands for ROMERO database migrations,
// Zero-Trust provisioning, and local task_shared authorization grants.
package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/romero-project/romero/internal/application/provisioning"
	"github.com/romero-project/romero/internal/domain/identity"
)

type globalFlags struct {
	configPath string
	dsn        string
}

// NewRootCmd constructs the root `romero` Cobra command tree using deps.
func NewRootCmd(deps Dependencies) *cobra.Command {
	if deps.Migrator == nil {
		deps.Migrator = gooseMigrator{}
	}
	if deps.OpenProvisioner == nil {
		deps.OpenProvisioner = openPostgresProvisioner
	}

	var gf globalFlags
	root := &cobra.Command{
		Use:           "romero",
		Short:         "ROMERO cognitive memory administration CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&gf.configPath, "config", defaultConfigPath, "path to romero.toml configuration")
	root.PersistentFlags().StringVar(&gf.dsn, "dsn", "", "PostgreSQL connection DSN (overrides ROMERO_DSN and config)")

	root.AddCommand(
		newVersionCmd(),
		newMigrateCmd(&gf, deps),
		newBootstrapCmd(&gf, deps),
		newPrincipalCmd(&gf, deps),
		newTaskPermissionCmd(&gf, deps),
	)
	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the ROMERO binary version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), Version)
			return err
		},
	}
}

func newMigrateCmd(gf *globalFlags, deps Dependencies) *cobra.Command {
	var direction string
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply embedded Goose SQL migrations to PostgreSQL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dsn, err := resolveDSN(gf.dsn, gf.configPath)
			if err != nil {
				return err
			}
			ver, err := deps.Migrator.Run(cmd.Context(), dsn, direction)
			if err != nil {
				return err
			}
			if direction == "" {
				direction = "up"
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"direction": direction,
				"version":   ver,
			})
		},
	}
	cmd.Flags().StringVar(&direction, "direction", "up", "migration action: up or status")
	return cmd
}

func newBootstrapCmd(gf *globalFlags, deps Dependencies) *cobra.Command {
	var (
		tenantSlug    string
		tenantName    string
		projectSlug   string
		projectName   string
		projectRepo   string
		projectDesc   string
		agentKey      string
		agentName     string
		agentRole     string
		principalName string
		principalType string
		scopesRaw     []string
		isAdmin       bool
		expiresIn     time.Duration
		taskGrantsRaw []string
	)

	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Initialize a tenant, project, optional agent, and root API key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scopes, err := parseScopes(scopesRaw)
			if err != nil {
				return err
			}
			grants, err := parseTaskGrants(taskGrantsRaw)
			if err != nil {
				return err
			}
			var expiresAt *time.Time
			if expiresIn > 0 {
				t := time.Now().UTC().Add(expiresIn)
				expiresAt = &t
			}

			dsn, err := resolveDSN(gf.dsn, gf.configPath)
			if err != nil {
				return err
			}
			client, cleanup, err := deps.OpenProvisioner(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer cleanup()

			out, err := client.Service.Bootstrap(cmd.Context(), provisioning.BootstrapSpec{
				TenantSlug:        tenantSlug,
				TenantName:        tenantName,
				ProjectSlug:       projectSlug,
				ProjectName:       projectName,
				ProjectRepoURL:    projectRepo,
				ProjectDesc:       projectDesc,
				AgentExternalKey:  agentKey,
				AgentName:         agentName,
				AgentRoleType:     agentRole,
				PrincipalName:     principalName,
				PrincipalType:     identity.PrincipalType(principalType),
				AllowedScopes:     scopes,
				IsAdmin:           isAdmin,
				ExpiresAt:         expiresAt,
				InitialTaskGrants: grants,
			})
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"tenant":    out.Tenant,
				"project":   out.Project,
				"agent":     out.Agent,
				"principal": out.Principal,
				"api_key":   out.RawAPIKey,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&tenantSlug, "tenant-slug", "", "unique tenant slug")
	f.StringVar(&tenantName, "tenant-name", "", "human-readable tenant name")
	f.StringVar(&projectSlug, "project-slug", "", "project slug inside tenant")
	f.StringVar(&projectName, "project-name", "", "human-readable project name")
	f.StringVar(&projectRepo, "project-repo", "", "optional repository URL")
	f.StringVar(&projectDesc, "project-desc", "", "optional project description")
	f.StringVar(&agentKey, "agent-key", "", "optional agent external key")
	f.StringVar(&agentName, "agent-name", "", "optional agent display name")
	f.StringVar(&agentRole, "agent-role", "general", "agent role type")
	f.StringVar(&principalName, "principal-name", "", "root principal display name")
	f.StringVar(&principalType, "principal-type", string(identity.PrincipalOrchestrator), "principal type (user|service|orchestrator|agent)")
	f.StringSliceVar(&scopesRaw, "scopes", []string{string(identity.ScopeProjectShared)}, "allowed scope types")
	f.BoolVar(&isAdmin, "admin", false, "grant tenant administrator privileges")
	f.DurationVar(&expiresIn, "expires-in", 0, "optional API key lifetime (e.g. 720h)")
	f.StringArrayVar(&taskGrantsRaw, "task-grant", nil, "initial task grants (<task_id>:read,write)")

	_ = cmd.MarkFlagRequired("tenant-slug")
	_ = cmd.MarkFlagRequired("tenant-name")
	_ = cmd.MarkFlagRequired("project-slug")
	_ = cmd.MarkFlagRequired("project-name")
	_ = cmd.MarkFlagRequired("principal-name")
	return cmd
}

func newPrincipalCmd(gf *globalFlags, deps Dependencies) *cobra.Command {
	principalCmd := &cobra.Command{
		Use:   "principal",
		Short: "Manage Zero-Trust principals and API keys",
	}
	principalCmd.AddCommand(newPrincipalCreateCmd(gf, deps))
	return principalCmd
}

func newPrincipalCreateCmd(gf *globalFlags, deps Dependencies) *cobra.Command {
	var (
		tenantIDStr   string
		agentIDStr    string
		principalType string
		name          string
		projectsRaw   []string
		scopesRaw     []string
		isAdmin       bool
		expiresIn     time.Duration
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a scoped principal and issue its one-time API key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			tenantID, err := uuid.Parse(tenantIDStr)
			if err != nil {
				return fmt.Errorf("invalid --tenant-id: %w", err)
			}
			var agentID *uuid.UUID
			if agentIDStr != "" {
				parsed, err := uuid.Parse(agentIDStr)
				if err != nil {
					return fmt.Errorf("invalid --agent-id: %w", err)
				}
				agentID = &parsed
			}
			projects, err := parseUUIDList(projectsRaw)
			if err != nil {
				return err
			}
			scopes, err := parseScopes(scopesRaw)
			if err != nil {
				return err
			}
			var expiresAt *time.Time
			if expiresIn > 0 {
				t := time.Now().UTC().Add(expiresIn)
				expiresAt = &t
			}

			dsn, err := resolveDSN(gf.dsn, gf.configPath)
			if err != nil {
				return err
			}
			client, cleanup, err := deps.OpenProvisioner(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer cleanup()

			rawKey, view, err := client.Service.CreatePrincipal(cmd.Context(), provisioning.CreatePrincipalInput{
				TenantID:        tenantID,
				AgentID:         agentID,
				PrincipalType:   identity.PrincipalType(principalType),
				Name:            name,
				AllowedProjects: projects,
				AllowedScopes:   scopes,
				IsAdmin:         isAdmin,
				ExpiresAt:       expiresAt,
			})
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"principal": view,
				"api_key":   rawKey,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&tenantIDStr, "tenant-id", "", "target tenant UUID")
	f.StringVar(&agentIDStr, "agent-id", "", "optional agent UUID")
	f.StringVar(&principalType, "type", "", "principal type (user|service|orchestrator|agent)")
	f.StringVar(&name, "name", "", "principal name")
	f.StringSliceVar(&projectsRaw, "projects", nil, "allowed project UUIDs")
	f.StringSliceVar(&scopesRaw, "scopes", nil, "allowed scopes")
	f.BoolVar(&isAdmin, "admin", false, "grant tenant administrator privileges")
	f.DurationVar(&expiresIn, "expires-in", 0, "optional API key lifetime")

	_ = cmd.MarkFlagRequired("tenant-id")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func newTaskPermissionCmd(gf *globalFlags, deps Dependencies) *cobra.Command {
	tpCmd := &cobra.Command{
		Use:   "task-permission",
		Short: "Grant or revoke task_shared read/write assignments (AC-T4, AC-T5)",
	}
	tpCmd.AddCommand(
		newTaskPermissionMutateCmd(gf, deps, true),
		newTaskPermissionMutateCmd(gf, deps, false),
	)
	return tpCmd
}

func newTaskPermissionMutateCmd(gf *globalFlags, deps Dependencies, isGrant bool) *cobra.Command {
	var (
		apiKey         string
		tenantIDStr    string
		projectIDStr   string
		taskID         string
		principalIDStr string
		canRead        bool
		canWrite       bool
	)

	use := "revoke"
	short := "Revoke all task_shared read/write permissions for a principal on a task"
	if isGrant {
		use = "grant"
		short = "Grant task_shared read and/or write permissions for a principal on a task"
	}

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rawKey := resolveAPIKey(apiKey)
			if rawKey == "" {
				return errors.New("caller API key required (--api-key or ROMERO_API_KEY)")
			}
			tenantID, err := uuid.Parse(tenantIDStr)
			if err != nil {
				return fmt.Errorf("invalid --tenant-id: %w", err)
			}
			projectID, err := uuid.Parse(projectIDStr)
			if err != nil {
				return fmt.Errorf("invalid --project-id: %w", err)
			}
			principalID, err := uuid.Parse(principalIDStr)
			if err != nil {
				return fmt.Errorf("invalid --principal-id: %w", err)
			}
			if isGrant && !canRead && !canWrite {
				return errors.New("grant requires at least --read or --write")
			}

			dsn, err := resolveDSN(gf.dsn, gf.configPath)
			if err != nil {
				return err
			}
			client, cleanup, err := deps.OpenProvisioner(cmd.Context(), dsn)
			if err != nil {
				return err
			}
			defer cleanup()

			actor, err := client.Auth.Authenticate(cmd.Context(), rawKey)
			if err != nil {
				return err
			}

			rFlag, wFlag := false, false
			status := "revoked"
			if isGrant {
				rFlag, wFlag = canRead, canWrite
				status = "granted"
			}
			if err := client.Service.ManageTaskPermission(cmd.Context(), actor, provisioning.ManageTaskPermissionCommand{
				TenantID:    tenantID,
				ProjectID:   projectID,
				TaskID:      taskID,
				PrincipalID: principalID,
				CanRead:     rFlag,
				CanWrite:    wFlag,
			}); err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), map[string]any{
				"status":       status,
				"tenant_id":    tenantID,
				"project_id":   projectID,
				"task_id":      taskID,
				"principal_id": principalID,
				"can_read":     rFlag,
				"can_write":    wFlag,
			})
		},
	}

	f := cmd.Flags()
	f.StringVar(&apiKey, "api-key", "", "caller admin or project orchestrator API key (or ROMERO_API_KEY)")
	f.StringVar(&tenantIDStr, "tenant-id", "", "tenant UUID")
	f.StringVar(&projectIDStr, "project-id", "", "project UUID")
	f.StringVar(&taskID, "task-id", "", "task identifier")
	f.StringVar(&principalIDStr, "principal-id", "", "target principal UUID")
	if isGrant {
		f.BoolVar(&canRead, "read", false, "grant read permission on task")
		f.BoolVar(&canWrite, "write", false, "grant write permission on task")
	}

	_ = cmd.MarkFlagRequired("tenant-id")
	_ = cmd.MarkFlagRequired("project-id")
	_ = cmd.MarkFlagRequired("task-id")
	_ = cmd.MarkFlagRequired("principal-id")
	return cmd
}
