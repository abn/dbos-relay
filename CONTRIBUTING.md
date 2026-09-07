# Contributing

Thanks for your interest in Relay.

## Before anything else: the clean-room rules

Relay implements contracts that a commercial product also implements, and it
is built only from public, permissively licensed sources.

The canonical list of permitted and forbidden sources is
[docs/contribution/clean-room.md](docs/contribution/clean-room.md), along with
the provenance and derivation requirements. Read it before you write anything.
It is deliberately the only copy of that list in the project.

Three things it says that are worth repeating here:

- The proprietary server is never run, downloaded, or observed, for any
  reason, including under any free test and development terms.
- Every protocol or API fact names its source or is proven by a test.
- If a fact is only obtainable from a forbidden source, stop and say so. Do
  not guess, and do not go and look.

A pull request that depends on a forbidden source cannot be merged, whatever
its quality.

## Making a change

Read the [contributor guide](docs/contribution/guide.md) for the workflow,
conventions, and the verification gate. In short:

```
./.agents/bootstrap.sh
make check
```

Changes are scoped, land on a conventional branch, follow Conventional
Commits with no trailers, and are not done until `make check` passes and the
wiki reflects the new behaviour.

`AGENTS.md` is the operational contract and applies to agent sessions as much
as to people.

## Licence

Contributions are made under the MIT licence, matching [LICENSE](LICENSE).
