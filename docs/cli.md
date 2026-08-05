# CLI

`infr` is the command-line control plane. Run `infr` with no command to open
the TUI, or run `infr <command> --help` for built-in help.

Most commands talk to the control daemon over its Unix socket. Start it with
`infr serve --detach`. RPC commands accept `--socket <path>`; otherwise they
use the default socket or `INFERENCERIG_CONTROL_SOCKET`.

## Commands

| Command | Purpose |
| --- | --- |
| `infr health` | Check daemon health. |
| `infr info` | Show daemon, backend, profile, and build information. |
| `infr signals` | Show host CPU, memory, disk, and accelerator signals. |
| `infr setup` | Run first-time setup. |
| `infr serve [--detach]` | Run the control daemon; `-d` also detaches. |
| `infr daemon status` | Show detached daemon status. |
| `infr daemon stop` | Stop the detached daemon. |
| `infr web` | Run the browser and API gateway. |
| `infr tui [--socket <path>]` | Open the terminal dashboard. |
| `infr doctor` | Check configuration, permissions, and daemon state. |
| `infr version [--json]` | Print build information. |
| `infr upgrade [version]` | Install a release and restart services. `update` is an alias. |
| `infr uninstall` | Interactively remove services, settings, and the command. |

### Profiles

| Command | Purpose |
| --- | --- |
| `infr profile list` | List profiles. |
| `infr profile get <name>` | Show one profile. |
| `infr profile create <name> <yaml-file>` | Create a profile from YAML. |
| `infr profile edit <name> <yaml-file>` | Replace a profile from YAML. |
| `infr profile delete <name>` | Delete a profile. |
| `infr profile cleanup <name>` | Delete a profile and its unshared local model. |
| `infr profile autostart <name> <true\|false>` | Enable or disable autostart. |

### Models

| Command | Purpose |
| --- | --- |
| `infr model search <backend> [query]` | Search the remote catalog. |
| `infr model watch` | Watch catalog refreshes. |
| `infr model list <backend>` | List local models. |
| `infr model resolve <profile>` | Resolve the model used by a profile. |
| `infr model download <profile>` | Download a profile's model. Add `--detach` to return immediately. |
| `infr model get <id>` | Show download status. |
| `infr model cancel <id>` | Cancel a download. |
| `infr model apply <profile> <id>` | Apply a completed download to a profile. |
| `infr model rm <backend> <path>` | Delete a local model. |

### Backends and runtimes

| Command | Purpose |
| --- | --- |
| `infr backend list` | List backends. |
| `infr backend status <backend>` | Show installation status. |
| `infr backend install <backend> [version]` | Install a backend. |
| `infr backend rollback <backend>` | Restore the previous installation. |
| `infr backend params <backend>` | List supported profile parameters. |
| `infr runtime status <profile>` | Show runtime status. |
| `infr runtime start <profile>` | Start a profile. Add `--replace` to stop the profile already being served. |
| `infr runtime stop <profile>` | Stop a profile. |
| `infr runtime restart <profile>` | Restart a profile. |
| `infr runtime reset` | Stop all profiles and clear the active backend. |

### Events, configuration, and services

| Command | Purpose |
| --- | --- |
| `infr events list` | List control events. |
| `infr events watch` | Stream control events. |
| `infr config validate` | Validate the config as startup would. |
| `infr config startup [service...]` | Set startup services. |
| `infr service generate <systemd\|launchd>` | Print a user-service definition. |
| `infr service install` | Install and start the native user service. |
| `infr service uninstall` | Stop and remove the native user service. |

## Output and diagnosis

Use `--output text` for human-readable output or `--output json` for scripts.
Without the flag, terminals get text and pipes get JSON.

`infr doctor` is read-only unless passed `--fix` or `--fix-with <repair>`.
Use `--verify-models` to re-hash model files; it reads all model data.
