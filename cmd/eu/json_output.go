package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type jsonEnvelope struct {
	OK      bool   `json:"ok"`
	Command string `json:"command,omitempty"`
	Target  string `json:"target,omitempty"`
	Error   string `json:"error,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type jsonCommandError struct {
	Command string
	Target  string
	Err     error
	Data    any
}

func (e *jsonCommandError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Emit machine-readable JSON")
}

func addNoPromptFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("no-prompt", false, "Use existing config and environment overrides without interactive prompts")
}

func commandJSONEnabled(cmd *cobra.Command) bool {
	enabled, err := cmd.Flags().GetBool("json")
	return err == nil && enabled
}

func commandNoPrompt(cmd *cobra.Command) bool {
	enabled, err := cmd.Flags().GetBool("no-prompt")
	return err == nil && enabled
}

func commandShouldPrompt(cmd *cobra.Command) bool {
	return !commandJSONEnabled(cmd) && !commandNoPrompt(cmd)
}

func emitJSONSuccess(cmd *cobra.Command, target string, data any) error {
	payload := struct {
		OK      bool   `json:"ok"`
		Command string `json:"command,omitempty"`
		Target  string `json:"target,omitempty"`
		Data    any    `json:"data,omitempty"`
	}{
		OK:      true,
		Command: commandName(cmd),
		Target:  strings.TrimSpace(target),
		Data:    data,
	}
	return json.NewEncoder(os.Stdout).Encode(payload)
}

func emitJSONError(err error) {
	payload := jsonEnvelope{
		OK:      false,
		Command: commandNameFromArgs(os.Args[1:]),
		Error:   err.Error(),
	}
	var structured *jsonCommandError
	if errors.As(err, &structured) && structured != nil {
		payload.Command = structured.Command
		payload.Target = structured.Target
		payload.Error = structured.Err.Error()
		payload.Data = structured.Data
	}
	_ = json.NewEncoder(os.Stdout).Encode(payload)
}

func commandName(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	name := strings.TrimSpace(cmd.Name())
	if name == "eu" {
		return ""
	}
	return name
}

func commandNameFromArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if strings.TrimSpace(arg) == "eu" {
			continue
		}
		return arg
	}
	return ""
}

func argsWantJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func newJSONCommandError(cmd *cobra.Command, target string, err error, data any) error {
	return &jsonCommandError{
		Command: commandName(cmd),
		Target:  strings.TrimSpace(target),
		Err:     err,
		Data:    data,
	}
}
