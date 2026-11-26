package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestI18nViCheck runs scripts/i18n-vi-check.mjs when node is available so
// mixed English–Vietnamese / leftover CJK dictionary values fail CI early.
func TestI18nViCheck(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; skipping i18n-vi check")
	}
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(repoRoot, "scripts", "i18n-vi-check.mjs")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("missing %s: %v", script, err)
	}
	cmd := exec.Command(node, script)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("i18n-vi-check failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "0 mixed entries") {
		t.Fatalf("unexpected i18n-vi-check output: %s", out)
	}
}
