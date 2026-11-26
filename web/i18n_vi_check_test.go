package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func requireNode(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; skipping i18n-vi check")
	}
	return node
}

// TestI18nViCheck runs scripts/i18n-vi-check.mjs when node is available so
// mixed English–Vietnamese / leftover CJK dictionary values fail CI early.
func TestI18nViCheck(t *testing.T) {
	node := requireNode(t)
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "i18n-vi-check.mjs")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("missing %s: %v", script, err)
	}
	cmd := exec.Command(node, script)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("i18n-vi-check failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "0 mixed entries") {
		t.Fatalf("unexpected i18n-vi-check output: %s", out)
	}
}

// TestI18nViCheckUnit runs scripts/i18n-vi-check.test.mjs (node --test) so
// must-fail / must-pass review cases stay wired into go test ./web/...
func TestI18nViCheckUnit(t *testing.T) {
	node := requireNode(t)
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "i18n-vi-check.test.mjs")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("missing %s: %v", script, err)
	}
	cmd := exec.Command(node, "--test", script)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("i18n-vi-check unit tests failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "# fail 0") && !strings.Contains(string(out), "fail 0") {
		// node --test prints "# fail 0" on success
		if strings.Contains(string(out), "not ok") {
			t.Fatalf("i18n-vi-check unit tests reported failures:\n%s", out)
		}
	}
}
