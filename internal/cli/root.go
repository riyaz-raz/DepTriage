// Package cli defines the deptriage command-line interface.
package cli

import (
	"errors"
	"fmt"

	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/deptriage/deptriage/internal/config"
	"github.com/spf13/cobra"
)

// Execute runs the command and exits with an actionable status code.
func Execute(version string) {
	if err := NewRoot(version).Execute(); err != nil {
		var usageErr *UsageError
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(exitErr.Code)
		}
		if errors.As(err, &usageErr) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// UsageError represents invalid command input or configuration.
type UsageError struct{ Message string }

// Error returns the usage error message.
func (e *UsageError) Error() string { return e.Message }

// NewRoot constructs the root command tree.
func NewRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "deptriage",
		Short:         "Triage dependency update pull requests",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &ExitError{Code: 2, Err: err}
	})
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), version)
			return err
		},
	})
	root.AddCommand(configCommand())
	root.AddCommand(newCheckCommand())
	return root
}

func configCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage deptriage configuration"}
	cmd.AddCommand(&cobra.Command{
		Use: "init", Short: "Create a config file", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil {
				return &UsageError{Message: fmt.Sprintf("config already exists at %s", path)}
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("check config path: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return fmt.Errorf("create config directory: %w", err)
			}
			if err := os.WriteFile(path, []byte(config.Template()), 0o600); err != nil {
				return fmt.Errorf("write config %q: %w", path, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", path)
			return err
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "show", Short: "Show config with secrets redacted", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, path, err := config.LoadDefault()
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "# %s\n", path); err != nil {
				return err
			}
			return toml.NewEncoder(cmd.OutOrStdout()).Encode(cfg.Redacted())
		},
	})
	cmd.AddCommand(setKeyCommand())
	return cmd
}

func setKeyCommand() *cobra.Command {
	return &cobra.Command{
		Use: "set-key <provider> <key>", Short: "Store a provider API key", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider, key := strings.ToLower(args[0]), args[1]
			if provider != "gemini" && provider != "groq" && provider != "openai" {
				return &UsageError{Message: "provider must be gemini, groq, or openai"}
			}
			if strings.TrimSpace(key) == "" {
				return &UsageError{Message: "API key must not be empty"}
			}
			cfg, path, err := config.LoadDefault()
			if err != nil {
				return err
			}
			if cfg.Keys == nil {
				cfg.Keys = map[string]string{}
			}
			cfg.Keys[provider] = key
			if err := config.Save(path, cfg); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Stored %s API key in %s\n", provider, path)
			return err
		},
	}
}
