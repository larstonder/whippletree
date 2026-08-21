# "whippletree-compatible" — a sketch

**Status: sketch. Nothing here is in force.** No mark exists, no certification is
offered, and no one may claim one. This records the design while it is cheap to
change, and to make the intent legible before there is anything to argue about.
The terms need a trademark attorney before publication.

## The problem it addresses

Apache-2.0 lets anyone fork whippletree and ship it. That is intended. What the
licence cannot do is stop a fork from being *wrong* while still being called
whippletree — and whippletree's entire proposition is that its verdicts can be
trusted. A `SATISFY` from a build that quietly relaxed the tier rules, or from
one carrying a target definition last probed two years ago, is worse than no
answer, because it looks like an answer.

The lesson from the licence wars is that the licence was never the lever.
HashiCorp's move to BUSL did not stop the Terraform fork; the **trademark** did,
and OpenTF had to become OpenTofu. Elastic's relicence did not stop AWS's fork;
the trademark suit settled in 2022 did. The name is the enforceable part.

So the mark is not a restriction on forking. It is a claim about currency and
correctness that a fork can *earn*, and that a stale or modified one cannot
truthfully make.

## What it would certify

Three things, in decreasing order of how much they matter:

1. **The verdicts are unmodified.** Tier assignment, the SATISFY/DEGRADE/REFUSE/
   ABSENT classification, and the `contract.T3Fidelity` disclosure behave as
   upstream. This is the substance.
2. **The probe corpus is current.** Every shipped target definition's
   `metadata.testedVersions` covers a harness version released within some
   window, and the e2e suite passed against it. This is what a stale fork cannot
   fake, and it is why `whippletree version` prints the corpus: the question is
   about the binary in your hand, not about a repository.
3. **The contract surface is honest.** `contractVersion` is enforced, and no
   field of `dev.whippletree.v1` has been reinterpreted.

## How it would be tested

The conformance suite is not new work. It already exists in pieces:

- `go test ./...` covers the tier and classification rules.
- `test/e2e/run-*.sh` drives each real harness end to end.
- The nightly `e2e` workflow is the currency check, and files a drift issue when
  a harness moves out from under a definition.

What is missing is packaging those as something a third party can run against
*their* build and get a signed result from, plus a published list of who has
passed. That is the actual work, and it is deliberately after v1.0.

## Prior art worth copying

Certified Kubernetes is the model: an open test suite anyone can run, a
trademark licence granted on passing it, and a public list of conformant
distributions. It works because the suite is the product of the community and
the mark is merely the receipt. Notably it permits forks and vendor
distributions — it constrains only the *claim*, not the code.

## Open questions

- Self-certification with published results, or an approval step? Kubernetes
  uses self-certification plus a PR to a public repo, which scales to one
  maintainer. That is the likely answer.
- How current is current? Harnesses ship weekly; a 30-day window is probably too
  tight and a year is meaningless. `MAINTENANCE.md` already tracks the real
  cadence, so use the measured number rather than a guess.
- Does the mark cover target definitions authored by third parties? A definition
  is a claim about a harness, so a wrong one is exactly the failure mode this
  exists to catch — probably yes, and probably the most valuable part.
- What happens when a conformant build later goes stale? A mark with no expiry
  is a mark that eventually certifies nothing.
