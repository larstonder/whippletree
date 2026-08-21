# whippletree trademark policy

**This policy is a draft and has not been reviewed by a trademark attorney.
Do not rely on it as legal advice.** It is written down now so the intent is on
the record from the first release rather than asserted retroactively.

## The short version

The **code** is Apache-2.0. Do what the licence says: use it, fork it, sell it,
build a product on it.

The **name** and the **logo** are not covered by that licence. Apache-2.0 says so
itself, in section 6:

> This License does not grant permission to use the trade names, trademarks,
> service marks, or product names of the Licensor, except as required for
> reasonable and customary use in describing the origin of the Work.

## What you may do without asking

- Say your project works with, is built on, targets, or is compatible with
  whippletree.
- Use the name in truthful descriptive prose, talks, articles and tutorials.
- Keep the name in an unmodified redistribution of whippletree itself.
- Publish bundles, target definitions or tooling that use the
  `dev.whippletree.v1` contract namespace. That namespace is part of the
  interface, and interoperating with it is the entire point.

## What needs permission

- Naming your own product, service or company "whippletree", or anything close
  enough to be confused with it.
- Using the name or logo in a way that suggests your fork is the official
  project, or that it is endorsed by or affiliated with it.
- Distributing a **modified** whippletree under the whippletree name. Fork
  freely; call the fork something else, the way OpenTofu did.
- Using the name for a conformance or compatibility claim. See below.

## Modified versions

Apache-2.0 section 4(b) already requires modified files to carry prominent
notices stating that you changed them. This policy adds the name: if you ship
something that behaves differently from upstream whippletree, do not call it
whippletree.

This matters more here than in most projects, because whippletree's whole
proposition is that its verdicts are trustworthy. A `SATISFY` from a modified
build that quietly relaxed the tier rules would poison the one thing the tool
sells.

## "whippletree-compatible" (planned)

A conformance mark is planned but **does not exist yet** — see
[`docs/CONFORMANCE.md`](docs/CONFORMANCE.md). Until it does, please do not claim
compatibility certification. Describing interoperability factually is always
fine.

## Contact

Open an issue at https://github.com/larstonder/whippletree/issues for anything
this page does not answer.
