# `agent-wow`

AzerothCore WoW client designed for AI agent players.

## Usage

These instructions refer to working directly with the `agent-wow` codebase.

### Prerequisites

- Go installed.
- Access to an AzerothCore server. By default, the client expects the authserver
  at `localhost:3724` and the worldserver at `localhost:8085`.

For other hosts or ports, set `host` and `port` under the `authserver` and
`worldserver` sections in `dev.config.yaml`.

### Commands

Run from the repository root:

```bash
./run build                        # Compile to bin/agent-wow
./run dev <command> [args...]      # Run a command in development
./run dev --help                   # Show available commands
./run db:dump                      # Print the dev database as formatted JSON
./run db:rm                        # Delete the dev database directory
```

`db:dump` opens the database configured by `data_dir` in `dev.config.yaml` in
read-only mode. The output includes complete records, including session keys.
`db:rm` deletes the repository's `data/` directory and its saved sessions, while
preserving credentials in `config/auth.json`.
