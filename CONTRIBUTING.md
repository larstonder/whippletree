# Contributing to Whippletree

## Sign your commits (DCO)

Whippletree uses the Developer Certificate of Origin rather than a CLA. There is
nothing to sign up for and no copyright to assign: you certify the origin of
what you contribute by adding a `Signed-off-by` line to each commit.

```
git commit -s -m "your message"
```

which appends:

```
Signed-off-by: Your Name <your.email@example.com>
```

Use your real name and an address you can be reached at. The full text you are
certifying:

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.


Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.```

## Before you open a pull request

```
gofmt -l .            # must print nothing
go vet ./...
GOOS=windows go vet ./...
go test ./... -race
```

CI runs all of that on Linux, macOS and Windows, plus a cross-compile of every
platform a release ships, a build against the Go version `go.mod` declares, and
a check that `whippletree build` still reproduces the committed
`examples/kb-shaped` artifacts byte-for-byte. That last one catches a silent
change to what every user's bundle looks like, so if it fails, look before you
regenerate.

Conventions worth knowing before your first change are in
[`CLAUDE.md`](CLAUDE.md) — in particular, comment what the code cannot say, and
put the rationale for a change in its commit message rather than in a source
file where it will rot.

## Adding or changing a target

Target definitions are claims about how a real harness behaves, so they are
established by probing that harness, not by reading its documentation. See
`docs/opencode-probe-findings.md` and `docs/skill-discovery-probe.md` for the
method: a `mktemp -d` sandbox, an isolated harness home, and an honest record of
anything the probe could not verify.

`metadata.testedVersions` is the claim that a definition was actually exercised
against a version range. Do not widen it for a version nobody ran.

## Changing the contract surface

`dev.whippletree.v1` is versioned by `contractVersion`, and
`contract.SupportedContractVersion` is what a build will accept. Additive
changes are minor bumps; anything that changes the meaning of an existing field
is a major bump and needs discussion first. Open an issue before writing the
code.

## Licence

Contributions are accepted under Apache-2.0, the licence the project ships
under. See [`LICENSE`](LICENSE), and [`TRADEMARK.md`](TRADEMARK.md) for what the
licence does not cover.
