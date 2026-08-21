// SPDX-FileCopyrightText: 2026 Lars Tønder
// SPDX-License-Identifier: Apache-2.0

package contract

import "testing"

const kbExample = `{
  "name": "knowledge-base",
  "extensions": { "dev.whippletree.v1": {
    "contractVersion": "1.0.0",
    "requires": [
      {"id":"stop-gate","kind":"blocking-gate","event":"turn-end","minTier":"T1",
       "hardRequired":true,"loopGuardRequired":true,"handler":"./handlers/capture.sh"},
      {"id":"session-start-signal","kind":"lifecycle-signal","event":"session-start",
       "minTier":"T2","hardRequired":false,"handler":"./handlers/pull.sh"},
      {"id":"file-read-signal","kind":"observation-signal","event":"file-read",
       "minTier":"T4","hardRequired":false,"handler":"./handlers/log-read.sh"},
      {"id":"bin-reachable","kind":"executable-path","minTier":"T1",
       "hardRequired":true,"path":"./bin/kb"}
    ]}}}`

func TestParseExtractsContract(t *testing.T) {
	c, err := Parse([]byte(kbExample))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Requires) != 4 {
		t.Fatalf("want 4 requirements, got %d", len(c.Requires))
	}
	if c.Requires[0].MinTier != T1 || !*c.Requires[0].HardRequired {
		t.Errorf("stop-gate parsed wrong: %+v", c.Requires[0])
	}
	if c.Requires[2].Event != "file-read" {
		t.Errorf("alias event lost: %+v", c.Requires[2])
	}
}

func TestParseMissingNamespaceFails(t *testing.T) {
	if _, err := Parse([]byte(`{"name":"x","extensions":{}}`)); err == nil {
		t.Fatal("want error for missing namespace")
	}
}
