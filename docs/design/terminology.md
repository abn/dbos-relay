---
type: Reference
title: Terminology
description: The vocabulary used across the wiki and the code.
status: draft
---

# Terminology

The wiki uses the ecosystem's own vocabulary so that upstream documentation
reads correctly against Relay. Where Relay needs a word the ecosystem does not
have, it is defined here rather than invented per page.

## Ecosystem terms

**Executor.** One running process of an application that uses the workflow
library. It opens an outbound WebSocket to the control plane, and it owns its
system database, which the control plane does not touch. It carries an
identifier, an application name, and an application version. How the
identifier is generated and when it changes are open questions until the
protocol specification closes them.

**Application.** A named unit that executors belong to. Names are constrained
by the published API specification, and Relay copies those constraints
exactly.

**Organisation.** The tenancy boundary above applications, as modelled by the
published API.

**System database.** The application's own Postgres database, holding workflow
and step state. Off limits to Relay.

**Workflow, step, queue, schedule.** The library's own concepts. Relay never
redefines them; it lists, reads, and manipulates them through executors.

**Recovery.** Asking a healthy executor to take over the pending workflows of
an executor that has been declared dead. The public documentation describes
the library's execution guarantees as at-least-once for steps and exactly-once
for outcomes, which is what makes a repeated recovery request cheap. Relay's
design leans on that, and the claim is still to be confirmed against the SDK
source.

## Relay terms

**Instance.** One running Relay process. Several instances share one Relay
database.

**Ownership.** The relationship between an instance and an executor
connection. Exactly one instance owns a given connection and holds a lease on
it in the database. A request arriving at a non-owning instance is forwarded
to the owner.

**Hub.** The component that accepts executor connections and correlates
requests with responses over them.

**Router.** The component that picks a healthy executor for a request, and
forwards to a peer instance when the connection is owned elsewhere.

## Words this project does not use for itself

Relay does not use the ecosystem's trademarks in its own name, package names,
module names, binary name, logo, or domain. Module names are covered because a
module name ends up as a package name. Descriptive prose such as "compatible
with the Conductor protocol" is fine, and is how the compatibility target is
named throughout this wiki.

For the same reason Relay's own dashboard is called the dashboard, never the
console. Console is the vendor's product name and appears in this wiki only
when referring to that product.
