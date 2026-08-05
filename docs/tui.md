# TUI

The TUI is the full-screen terminal control plane. Start it with `infr` or
`infr tui`. It starts the local control daemon when needed and runs first-time
setup on a new installation.

Use `infr tui --socket <path>` to connect to another control socket. Local
daemon and web-gateway controls are disabled when using a custom socket.

## Global keys

| Key | Action |
| --- | --- |
| `1`–`4` | Open Services, Models, System, or Activity. |
| `r` | Refresh data. |
| `Ctrl+C` | Quit. |

## Services

Shows the control daemon, web gateway, and running profiles.

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Move between panels. |
| `Left` / `Right` | Select an action or profile. |
| `Enter` | Run the selected action. |
| `a` | Toggle autostart for the selected running profile. |
| `Up` / `Down` | Scroll. |

The control daemon offers Start, Stop, and Status. The web gateway offers
Start, Stop, and Open. A gateway started elsewhere is shown as external and
must be stopped where it was started.

## Models

Contains Profiles, Catalog, Local, and Downloads panes.

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Move between panes. |
| `[` / `]` | Change backend. |
| `Up` / `Down` | Select a row. |
| `/` | Search the catalog. |
| `Esc` | Clear search. |
| `Enter` | Start a profile or apply a completed download. |
| `g` | Download the selected profile's model. |
| `c` | Cancel the selected download. |
| `n` | Create a profile from the selected local model. |
| `R` | Restart the selected running profile. |
| `i` | Install the selected backend when supported. |
| `d`, then `y` | Confirm profile cleanup or local-model deletion. |

## System

Shows CPU, memory, accelerator, disk, model-storage, build, and control-plane
status. Use `Up` and `Down` to scroll.

## Activity

Contains Events, Control, Engine, and Web log panes.

| Key | Action |
| --- | --- |
| `Tab` / `Shift+Tab` | Move between panes. |
| `Up` / `Down` | Scroll. |
| `/` | Search the active pane. |
| `Esc` | Clear search. |
