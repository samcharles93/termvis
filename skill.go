package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// skillFS embeds the bundled agent skill so `termvis skill install` works
// regardless of how the binary was obtained (go install, a release
// tarball, a local build) — the skill directory only exists in the git
// checkout otherwise.
//
//go:embed skills/termvis
var skillFS embed.FS

const skillSrcDir = "skills/termvis"

func runSkillCommand(args []string) {
	usage := func() {
		fmt.Fprintf(os.Stderr, "termvis skill - manage the bundled agent skill\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n  termvis skill install [flags]\n  termvis skill show\n")
	}

	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	switch args[0] {
	case "install":
		runSkillInstall(args[1:])
	case "show":
		runSkillShow()
	default:
		fmt.Fprintf(os.Stderr, "termvis skill: unknown subcommand %q (want \"install\" or \"show\")\n\n", args[0])
		usage()
		os.Exit(1)
	}
}

func runSkillShow() {
	data, err := skillFS.ReadFile(skillSrcDir + "/SKILL.md")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading embedded skill: %v\n", err)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(data)
	fmt.Print("\n---\nAlso bundled: references/examples.md, scripts/check-deps.sh\n")
	fmt.Print("Run `termvis skill install` to install this skill (default: ~/.agents/skills/termvis).\n")
}

func runSkillInstall(args []string) {
	flags := flag.NewFlagSet("termvis skill install", flag.ExitOnError)
	project := flags.Bool("project", false, "install to the current project's skills directory (./.agents/skills/termvis) instead of the personal one (~/.agents/skills/termvis)")
	dest := flags.String("dest", "", "install to this exact directory instead (advanced; overrides -project)")
	force := flags.Bool("force", false, "overwrite an existing install")

	flags.Usage = func() {
		fmt.Fprintf(os.Stderr, "termvis skill install - install the bundled agent skill\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n  termvis skill install [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flags.PrintDefaults()
	}
	_ = flags.Parse(args)

	target, err := resolveSkillInstallDir(*dest, *project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving install directory: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stat(target); err == nil && !*force {
		fmt.Fprintf(os.Stderr, "termvis skill install: %s already exists (use -force to overwrite)\n", target)
		os.Exit(1)
	}

	if err := copyEmbeddedDir(skillFS, skillSrcDir, target); err != nil {
		fmt.Fprintf(os.Stderr, "Error installing skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Installed termvis skill to %s\n", target)
}

func resolveSkillInstallDir(dest string, project bool) (string, error) {
	if dest != "" {
		return dest, nil
	}
	if project {
		return filepath.Join(".agents", "skills", "termvis"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %v", err)
	}
	return filepath.Join(home, ".agents", "skills", "termvis"), nil
}

// copyEmbeddedDir copies the embedded srcDir tree to destDir. embed.FS
// normalizes file modes on read, so the source executable bit (needed for
// scripts/check-deps.sh) doesn't survive the copy automatically — it's
// restored here by extension instead.
func copyEmbeddedDir(fsys embed.FS, srcDir, destDir string) error {
	return fs.WalkDir(fsys, srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := fsys.ReadFile(path)
		if err != nil {
			return err
		}

		mode := os.FileMode(0o644)
		if strings.HasSuffix(path, ".sh") {
			mode = 0o755
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, mode); err != nil {
			return err
		}
		// WriteFile only applies mode to newly created files; chmod
		// explicitly so -force overwrites still get the right bit.
		return os.Chmod(target, mode)
	})
}
