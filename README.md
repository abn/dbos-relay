# Relay

An open-source control plane for DBOS Transact applications, compatible with
the Conductor executor protocol and HTTP API. Not affiliated with or endorsed
by DBOS, Inc.

DBOS Transact applications keep their workflow state in their own database and
recover workflows when the owning process restarts. Distributed recovery, a
fleet view, and workflow management come from a separate control plane,
Conductor, which the vendor's public documentation describes as proprietary
and licensed for production use. Relay is that control plane, rebuilt in the
open, under MIT.

The goal is that an unmodified application connects by changing two settings,
the Conductor URL and the key, and that the published HTTP API is served
closely enough that the existing command-line client and clients generated
from the OpenAPI document work against Relay unchanged.

## Status

Discovery, and nothing on this page is a shipped feature. There is no binary.
The current work is the executor protocol specification, the vendored REST
contract, and the stack decision.

## How it is built

Relay is built clean-room from public sources. The proprietary implementation
is never downloaded, run, observed, or benchmarked. The canonical list of what
may and may not be used as a source is in [the clean-room
rules](docs/contribution/clean-room.md), and it binds every contributor.

## Documentation

The wiki lives in [`docs/`](docs/index.md): the [overview](docs/overview.md),
the [design](docs/design/index.md), the
[architecture](docs/architecture/index.md), and the [decision
records](docs/adr/index.md).

## Contributing

Start with the [contributor guide](docs/contribution/guide.md) and
[CONTRIBUTING.md](CONTRIBUTING.md). `AGENTS.md` is the operational contract for
humans and agents alike.

## Licence

MIT. See [LICENSE](LICENSE).
