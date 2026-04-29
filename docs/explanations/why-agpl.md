---
type: explanation
---

# Why AGPL

Stowage is licensed AGPL-3.0-or-later. This page explains the choice
in plain English. The legal text is in
[`LICENSE`](https://github.com/stowage-dev/stowage/blob/main/LICENSE).

## What this means in practice

**For self-hosters and homelab operators.** Run Stowage on your own
infrastructure for any purpose, including commercial use. You are
not required to publish anything. The AGPL only adds obligations if
you *modify* Stowage and *expose those modifications to other users
over a network*.

**For internal tools at companies.** Same as above. Running an
unmodified Stowage as an internal dashboard for your team is not
"conveying" the software in the sense the AGPL cares about. If you
fork it and add private features that only your employees use
internally, the AGPL also does not require you to publish those
changes.

**For SaaS / managed-service operators.** If you offer Stowage to
users over a network *and* you have modified it, you must publish
those modifications under AGPL-3.0-or-later. This is the clause that
closes the gap the GPL leaves open. It exists deliberately.

**For contributors.** Contributions are accepted under the same
license as the project, certified through a Developer Certificate of
Origin (see
[`CONTRIBUTING.md`](https://github.com/stowage-dev/stowage/blob/main/CONTRIBUTING.md)).
Copyright in your contribution remains yours; you license it to the
project rather than transferring it.

## Why this license, specifically

Stowage exists in part because in May 2025, MinIO removed
administrative features from the open-source MinIO Console and moved
them behind a commercial product. The community's reaction was about
trust as much as features — the change happened without warning, in
what was described as a bug-fix release.

Stowage's positioning is the inverse: a backend-agnostic dashboard
that won't be quietly stripped down later.

A copyleft license is the structural commitment that makes that
positioning credible. A permissive license (MIT, Apache) would let
any vendor — including the next MinIO — fork Stowage, embed it in a
paid product, and starve the upstream. AGPL prevents that, both for
distributed builds and for managed-service offerings.

The "or later" suffix means future FSF revisions of the AGPL are
automatically acceptable, without requiring a relicensing exercise.

## What this is not

This is not a "source-available" license. It is OSI-approved open
source. The Open Source Initiative recognises AGPL-3.0 as conforming
to the Open Source Definition. Stowage is not under the SSPL, the
BSL, the Elastic License, or any other restricted license that loses
OSI approval.

## Future commercial licensing

The maintainer holds copyright on all original Stowage code, and
contributions are accepted under DCO without copyright transfer.
This preserves the option to offer commercial licenses to
organisations that cannot adopt AGPL for policy reasons, without
affecting the freely available AGPL-licensed version.

**No commercial license is offered today.** The paragraph is here so
the option is documented.

## Decision tree

```
Will you run Stowage?
├── On your own hardware, for your own purposes
│   ├── Modified or unmodified?
│   │   ├── Either way: no publication obligation. ✅
├── Inside a company, for your colleagues
│   ├── Modified or unmodified?
│   │   ├── Either way: no publication obligation. ✅
├── Offered over a network to outside users (SaaS)
│   ├── Unmodified
│   │   ├── No source-publication obligation; you must still convey the
│   │   │   AGPL itself with the offering. ✅
│   ├── Modified
│   │   ├── You must publish your modifications under AGPL-3.0-or-later. ⚠️
│   │   │   Either contribute them upstream or host a public source repo.
```

## What you don't need to do

- You don't have to sign a CLA.
- You don't have to transfer copyright.
- You don't have to register your deployment with anyone.
- You don't have to phone home — Stowage doesn't.
