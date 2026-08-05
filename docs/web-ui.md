# Web UI

The Web UI is the browser control plane. Start the control daemon, then run:

```sh
infr serve --detach
infr web
```

Open the URL printed by `infr web`. Authentication is enabled by default; a
generated token is included in the URL fragment and saved locally. Do not
expose a gateway with authentication disabled unless the network is trusted.

## Dashboard

- View runtime state, active profiles, warnings, and the latest action.
- Start from a profile, or restart and stop active profiles.
- View current CPU, memory, accelerator, disk, and model-storage use.
- View five-minute resource trends kept by the current browser tab.

## Profiles

- Create, duplicate, edit, and delete profiles.
- Set the backend, model, listen address, and engine arguments.
- Save, reload, start, clean up, or reset an active backend.
- Unsaved changes are marked in the browser title.

## Models

- Browse local models and remote catalog models.
- Search and filter the catalog by fit.
- Resolve a model, choose a variant, download it, and apply it to a profile.
- Create a profile from a local model or delete an unused local model.
- View or cancel download progress.

## Logs

- View and filter control events.
- Search control and engine logs; pause or resume live updates.
- Choose the service and tail length for control logs.
- Read or delete archived logs.

## Settings

Use the top-right settings button to change theme colors, API base, or session
token, and to test the connection. The adjacent theme button selects system,
light, or dark mode. `Ctrl`/`Cmd` + `B` toggles the sidebar.
