# Gameplay modules

agent-wow owns the authenticated world connection and its lifecycle. Modules own
gameplay state, protocol decoding, acknowledgements, actions, and coordination.
Installing a module does not require rebuilding agent-wow.

The Go packages live under `pkg/modules`: `discovery` (package `moddisc`) reads
manifests and validates descriptors and dependencies; `runner` (package
`modrunner`) manages Compose service startup, overrides, and cleanup; `callback`
(package `modcb`) implements the module-facing session gRPC handler; `runtime`
(package `modrt`) manages session lifecycle and routes calls and packets; `v1`
provides the generated `modv1` protobuf messages and gRPC interfaces used by modules.
`session` (package `modsession`) defines the session capabilities passed to the runtime
and the selected character and realm identity.

Use `moddisc.Discover` for offline listing and `moddisc.Load` to build a validated
registry for `modrt.New`. The registry exposes dependency order and module
contracts through read-only accessors; returned manifests are independent copies.
`modrt.New` accepts a `modrunner.Runner`; passing nil selects
`modrunner.ComposeRunner`.
`Runtime.Start` and `Manager.Start` accept a `modsession.Session`, containing a
`modsession.Identity` and the session's packet transport and clock callbacks.
The runtime registers one `modcb.Server` per module, supplying session handlers
and binding the caller's identity. `modcb` depends on those handlers rather than
the runtime implementation; dependency checks and typed routing remain in `modrt`.

## Installation and discovery

Place each module in `$config_dir/modules/<name>/`. Names use lowercase letters,
digits, underscores, and hyphens, begin with a letter, and cannot be `session`.
The session discovers modules once at startup. Changes take effect next session.

```yaml
# module.yaml
api_version: 1
enabled: true
description: Coordinates task-specific actions
requires: [movement, combat]
compose:
  file: compose.yaml
  service: module
grpc:
  descriptor_set: module.pb
rpc:
  engage: /example.v1.Tactics/Engage
packets:
  SMSG_LOGIN_VERIFY_WORLD: /example.v1.Tactics/OnPacket
lifecycle:
  before_logout: /example.v1.Tactics/BeforeLogout
```

`enabled` defaults to false; `requires`, `rpc`, `packets`, and `lifecycle` are
optional. Referenced files must be regular files inside the module directory.
The descriptor set must include imports, for example using protoc's
`--include_imports --descriptor_set_out=module.pb`. It is validated offline;
server reflection is not required. Only unary methods can be configured.

Dependencies must be enabled, installed, and acyclic. Each caller may invoke only
its directly declared dependencies. Dependencies start before their callers;
logout preparation and container cleanup run in the reverse order.

`agent-wow module list` lists definitions without contacting Docker or a realm.
`agent-wow module list --json` includes the mappings, dependencies, lifecycle hook,
module directory, and manifest path. Compose and descriptor paths in that output
are relative to the module directory. Disabled modules need no service assets.

## Container contract

The initial runtime requires Linux and a local Docker Engine Unix socket, plus
`docker compose`. Remote engines are rejected. Each module gets a distinct Compose
project. The selected service and its Compose dependencies start with
`up --detach --build`; use `depends_on` for services inside your own module.
Use project-scoped resources rather than fixed container names or shared ports.
The main service's restart policy is overridden to `no`.

agent-wow generates an override with a bind mount at `/run/agent-wow` and these
environment variables:

| Variable | Meaning |
| --- | --- |
| `AGENT_WOW_MODULE_SOCKET` | Socket path on which your gRPC server must listen |
| `AGENT_WOW_SESSION_SOCKET` | Socket path for your session gRPC client |
| `AGENT_WOW_MODULE_NAME` | Your registered namespace |
| `AGENT_WOW_SESSION_ID` | Unique identity for this session |
| `AGENT_WOW_CHARACTER_GUID`, `AGENT_WOW_CHARACTER_NAME` | Selected character |
| `AGENT_WOW_REALM_ID`, `AGENT_WOW_REALM_NAME` | Selected realm |
| `AGENT_WOW_CLIENT_BUILD` | Protocol build, currently 12340 |

These are **container environment variables**, not Compose interpolation inputs.
Honor the supplied socket paths at process startup. Expose plaintext gRPC over
the Unix socket and chmod the module socket to `0666` after binding, so the host
can connect when container and host UIDs differ. Each module's mounted directory
is writable by its container UID. Its parent remains private to the host user;
containers receive only their own module directory. Do not replace the callback
socket or mount the parent directory into your image.

Register the standard `grpc.health.v1.Health` service with both `Check` and
`Watch`, using the empty service name for whole-module health. Report `SERVING`
when the service can accept packets and calls. Do not wait for character state
before reporting health: that state arrives only after every module is healthy
and player login is sent. Public methods may return `FailedPrecondition` until
their gameplay state has initialized.

`char play --module-timeout 2m` controls the complete module startup budget.
`--timeout` controls network startup phases separately. Ping and Warden handling
continue on the authenticated transport while modules start. With no enabled
modules, Docker is not needed.

## RPC methods

Public callers use `POST /rpc` with the existing local-process restrictions:

```json
{"jsonrpc":"2.0","id":1,"method":"tactics.engage","params":{"targetGuid":"9007199254740993"}}
```

The alias `engage` resolves to your configured typed gRPC method. Parameters must
be an object; omitted or null parameters become `{}`. Conversion follows ProtoJSON:
64-bit integers are emitted as strings and bytes as base64. Unknown fields and
invalid values are rejected. Results preserve their protobuf JSON representation,
including null and scalar results. Batches are unsupported; notifications execute
without returning a JSON-RPC result.

Unknown or disabled methods return `-32601`, gateway parameter failures return
`-32602`, and target failures return `-32000` with module, method, and gRPC status.
Structured status details are retained as `grpc_details`, an array of protobuf
Any objects with `type_url` and base64 `value` fields.
`session.logout` is the only built-in public method; `session.getState` is removed.
Modules can expose their own state methods.

## Calling the session and other modules

The shared contract is `api/module/v1/session.proto`, with Go bindings in
`pkg/modules/v1`. Import it into your module's own protobuf build.

- `SendPacket` accepts an opcode and opaque payload. The session serializes writes
  and preserves transport encryption. Success means the write completed, not
  that the worldserver accepted the action. A canceled or failed call may have
  already written bytes: never retry an action automatically.
- `GetClock` returns client milliseconds since this session's monotonic origin,
  modulo 2^32. This is the exact clock used by core time-sync replies. Sample it,
  anchor it to a local monotonic reading (accounting for call latency), and
  resample as needed. Calculate differences using unsigned 32-bit subtraction
  across wraparound. Do not substitute wall-clock time or process uptime.
- `InvokeModule` takes a dependency's module name, public method alias, and a
  protobuf `Any` matching that method's declared request type. It returns the
  typed result as `Any`. There is no intermediate JSON conversion. Your own
  build can include the dependency's protobuf contract without rebuilding
  agent-wow.

For example, a tactics module can invoke `movement.stop` and then `combat.cast`.
The session identifies the caller through its assigned callback socket, resolves
the target, and checks the dependency declaration and request type. Packet
handlers and lifecycle hooks cannot be invoked through this gateway.

Propagate the inbound gRPC context into nested calls, including deadlines and
cancellation. Each forwarded call has a maximum 30-second deadline, shortened by
the caller's remaining budget. `PermissionDenied`, `NotFound`, `InvalidArgument`,
and `FailedPrecondition` indicate routing, type, or lifecycle errors. Target
application statuses and details are forwarded. Do not use `Unavailable` for
ordinary gameplay rejection: it terminates the session.

Long-running actions should return an operation ID immediately and expose their
own status and cancellation methods. Background operations may call `SendPacket`
without waiting for an incoming request. Stop those operations when the session
callback connection fails. Coordinate through a single controller for each
gameplay responsibility; do not create two independent movement writers.

## Packet handling

Configured handlers accept `agentwow.module.v1.WorldPacket` and return
`google.protobuf.Empty`. Subscribe with canonical server/bidirectional opcode
names or numeric values. Core packets are forwarded after core handling.
Gameplay packets, including compressed movement/object payloads, are not decoded
by the session. Modules must decode and acknowledge the protocol they require.

Subscribers each receive the same unchanged packet in receive order. Handlers
run sequentially per module, with at most 64 queued packets and 8 MiB of queued
payloads. Each handler has a five-second deadline. Do not wait for future packets
inside a handler: update state or enqueue background work and return. Public
methods, internal invocations, and lifecycle hooks can run concurrently with
handlers, so synchronize shared state appropriately.
Core packets arriving during module startup are queued until handlers are ready.
On confirmed logout, the session drains accepted packets, including logout
completion, before stopping the module workers; this drain is bounded to five
seconds and by the remaining logout budget.

`SendPacket` is allowed after player login begins and until session termination,
including logout preparation. Known client/bidirectional gameplay opcodes are
accepted, with payloads smaller than 10236 bytes. Authentication, login, ping,
keepalive, time-sync, Warden, and logout opcodes remain session-owned.

An expected gameplay event, such as being rooted, is a successful packet handler
that changes operation state. Returning an error means packet handling failed
and ends the session.

## Logout and failure

An optional `before_logout` handler accepts and returns `google.protobuf.Empty`.
It must cancel and settle autonomous actions and in-flight work before returning.
Modules without this hook must not own autonomous gameplay work requiring a stop.

Logout closes public admission and prepares modules in reverse dependency order.
The entire preparation phase has five seconds within the 30-second logout budget.
Hooks may call dependencies that have not yet prepared. Calls to already-prepared
modules are rejected. Once preparation finishes, all new internal invocations are
rejected and the session sends the server logout request. Packet processing,
`SendPacket`, `GetClock`, and health remain available for protocol acknowledgements.
Packet handlers must not require `InvokeModule` during this final logout exchange.

Keep the health service `SERVING` throughout preparation and the logout exchange.
A rejected server logout reopens public/internal admission, allowing new actions;
canceled actions are not resumed automatically. Concurrent logout callers share
one operation, and canceling a caller's wait does not cancel that operation.

Health updates are watched, with additional checks every second and a two-second
deadline. Unhealthy/unreachable modules, failed packet handlers, queue overflow,
or failed logout hooks terminate the gameplay session. Modules are not restarted.
The session closes its world connection and cleans up its own Compose projects
without deleting persistent volumes. Cleanup has a bounded budget; failures name
the project involved so leftover resources can be inspected.

## Development and verification

Install `protoc` (currently generated with 36.2) and put it on `PATH`. Regenerate
the checked-in shared and fixture contracts from the repository root:

```sh
protoc --proto_path=. \
  --plugin=protoc-gen-go="$(go tool -n protoc-gen-go)" \
  --plugin=protoc-gen-go-grpc="$(go tool -n protoc-gen-go-grpc)" \
  --go_out=. --go_opt=module=github.com/hazim-j/agent-wow \
  --go-grpc_out=. --go-grpc_opt=module=github.com/hazim-j/agent-wow \
  --include_imports --descriptor_set_out=internal/modulefixture/fixture.pb \
  api/module/v1/session.proto internal/modulefixture/fixture.proto
```

The Go module pins the Go plugin versions; `go tool -n` resolves their executables
without global plugin installs. The descriptor set includes the imported shared
and standard protobuf contracts. Module authors may use their language's own
tooling.

`internal/modulefixture` supplies a worker and an orchestrator implementation,
their source contract and descriptor set, and example module manifests. To stage
both examples in a disposable directory from the repository root:

```sh
fixture_root="$(mktemp -d)/modules"
mkdir -p "$fixture_root/worker" "$fixture_root/orchestrator"
CGO_ENABLED=0 GOOS=linux go build -o "$fixture_root/fixture" ./internal/modulefixture/cmd
for name in worker orchestrator; do
  cp internal/modulefixture/examples/"$name"/* "$fixture_root/$name/"
  cp internal/modulefixture/Dockerfile "$fixture_root/$name/"
  cp internal/modulefixture/fixture.pb "$fixture_root/$name/module.pb"
  cp "$fixture_root/fixture" "$fixture_root/$name/fixture"
done
```

The worker echoes typed data, samples the clock, counts subscribed packets, and
can send a requested opaque packet. The orchestrator forwards to the worker,
including during logout preparation. A background clock sampler demonstrates
autonomous callbacks and shutdown. These are protocol fixtures, not gameplay
capabilities; use the fake-world integration test for packet-sending experiments.

```sh
go test ./...
go test -race ./...
AGENT_WOW_DOCKER_TEST=1 go test -count=1 -run '^TestDockerModuleSession$' -v ./pkg/world
```

The opt-in test builds a local scratch-based image, installs two modules in a
temporary configuration directory, and exercises the public gateway, internal
composition, packet routing, clock, logout, and fatal module loss against a fake
worldserver. It removes only its own containers and networks. It never changes a
running AzerothCore server or database.
