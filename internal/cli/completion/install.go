package completion

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	xfilepath "github.com/gechr/x/filepath"
	xos "github.com/gechr/x/os"
	"github.com/gechr/x/shell"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/spf13/cobra"
)

func installScript(cmd *cobra.Command, runtime *cliruntime.RootRuntime, sh string) error {
	if sh == "" {
		sh = shell.Fish
	}
	if err := cmd.Context().Err(); err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	if err := validateCompletionShell(sh); err != nil {
		return err
	}
	var script bytes.Buffer
	if err := Generator(cmd.Root()).Print(&script, sh); err != nil {
		return err
	}
	path, err := completionFile(sh)
	if err != nil {
		return installFailure(runtime, path, false)
	}
	if cliruntime.DryRun(cmd) {
		if _, _, err := completionWritePath(path, false); err != nil {
			return installFailure(runtime, path, false)
		}
		return writeCompletionAction(cmd, runtime, completionAction{Action: "install", Shell: sh, Path: path, DryRun: true, WouldChange: true})
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // Retain clib's public completion-directory mode; scripts contain no credentials.
		return installFailure(runtime, path, false)
	}
	destination, mode, err := completionWritePath(path, true)
	if err != nil {
		return installFailure(runtime, path, false)
	}
	if err := cmd.Context().Err(); err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	if err := xos.AtomicWrite(destination, script.Bytes(), mode, xos.WithNoFollow()); err != nil {
		// A directory-sync failure can happen after publication. Never retry it.
		return installFailure(runtime, path, true)
	}
	return writeCompletionAction(cmd, runtime, completionAction{Action: "install", Shell: sh, Path: path, Changed: true})
}

func installFailure(runtime *cliruntime.RootRuntime, path string, mayHaveChanged bool) error {
	return completionFailure(runtime, "install", path, mayHaveChanged)
}

// Keep the six destinations used by pinned clib, including shells whose
// completion file layout is not provided by x/shell.CompletionFile.
func completionFile(sh string) (string, error) {
	switch sh {
	case shell.Pwsh, shell.Elvish:
		dir, err := shell.ConfigDir()
		if err != nil {
			return "", err
		}
		if sh == shell.Pwsh {
			return filepath.Join(dir, "powershell", "completions", "slick.ps1"), nil
		}
		return filepath.Join(dir, "elvish", "lib", "completions", "slick.elv"), nil
	default:
		return shell.CompletionFile("slick", sh)
	}
}

// Follow existing leaf links as the previous installer did. Resolve relative
// links from their real parent, and retain an existing file's mode and write
// access requirement before replacing its directory entry atomically.
func completionWritePath(path string, probeWrite bool) (string, os.FileMode, error) {
	for range 255 {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return path, 0o600, nil
		}
		if err != nil {
			return "", 0, err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if !info.Mode().IsRegular() {
				return "", 0, fmt.Errorf("completion destination is not a regular file")
			}
			if probeWrite {
				file, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // Probe the selected completion file without truncating it.
				if err != nil {
					return "", 0, err
				}
				if err := file.Close(); err != nil {
					return "", 0, err
				}
			}
			return path, info.Mode().Perm(), nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", 0, err
		}
		if !filepath.IsAbs(target) {
			parent, err := xfilepath.Resolve(filepath.Dir(path))
			if err != nil {
				return "", 0, err
			}
			target = filepath.Join(parent, target)
		}
		path = target
	}
	return "", 0, fmt.Errorf("too many completion symlinks")
}
