# TODO: capture TUI screenshots

`README.md`'s Screenshots section has web UI images but is missing the TUI.
Capturing the TUI needs a real terminal window on a machine with a working
X/Wayland display — it failed in the sandbox this was attempted from
(headless Xvfb, no GPU-accelerated GL context for `alacritty`). Do this on a
machine that has one.

## Steps

1. Build the binary: `go build -o /tmp/infr .`
2. Set up an isolated demo home so real profiles/models aren't required
   (or point `INFERENCERIG_HOME` at a real one with a couple of profiles —
   richer screenshots are better than the empty first-run state):
   ```bash
   export INFERENCERIG_HOME=/tmp/infr-demo-home
   mkdir -p "$INFERENCERIG_HOME"
   cat > "$INFERENCERIG_HOME/config.yaml" <<'EOF'
   listen_addr: 127.0.0.1:8734
   security:
     disable_auth: true
   EOF
   /tmp/infr serve --detach
   ```
3. Run the TUI in a terminal window sized around 120x35:
   `INFERENCERIG_HOME=/tmp/infr-demo-home /tmp/infr tui`
4. Screenshot the terminal window itself (not a text dump — a real
   screenshot, so it renders like the web UI images) for each tab worth
   showing: `[1] Services` (default), `[2] Models`, `[3] System`. Skip
   `[4] Activity` if empty.
5. Save as `docs/assets/tui-services.jpg`, `docs/assets/tui-models.jpg`,
   `docs/assets/tui-system.jpg` (jpg to match the existing web UI assets).
6. **Before committing**: scrub any real hostname, username/home-path
   (`/home/<you>/...`), or personally-identifying model names from the
   screenshots — the web UI screenshots in this repo were sanitized the same
   way (see the `git log` on `docs/assets/webui-*.jpg` for the pattern: real
   home path replaced with `/home/user/...`, uncensored/abliterated model
   names replaced with neutral placeholders). If profile/model names in your
   TUI capture need the same treatment, redact them before saving.
7. Add the TUI section to the README's Screenshots block, same table format
   as the Web UI section, then delete this file.

## Context

See PR for the web UI screenshots for the pattern this followed.
