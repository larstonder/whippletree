// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package bundles

import (
	"bytes"
	"os"
	"testing"
)

// TestAuthoringSkillReferenceInSync fails when the copy of the authoring
// guide that ships inside the authoring bundle's skill drifts from the
// canonical docs/AUTHORING.md. The fix is always the same one-liner.
func TestAuthoringSkillReferenceInSync(t *testing.T) {
	canonical, err := os.ReadFile("../docs/AUTHORING.md")
	if err != nil {
		t.Fatalf("read canonical AUTHORING.md: %v", err)
	}
	bundled, err := os.ReadFile("authoring/skills/whippletree-authoring/references/AUTHORING.md")
	if err != nil {
		t.Fatalf("read bundled AUTHORING.md copy: %v", err)
	}
	if !bytes.Equal(canonical, bundled) {
		t.Fatal("bundled AUTHORING.md is out of sync with docs/AUTHORING.md; run: cp docs/AUTHORING.md bundles/authoring/skills/whippletree-authoring/references/AUTHORING.md")
	}
}
