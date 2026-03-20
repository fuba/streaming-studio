# Streaming Studio

Streaming Studio is a web-based live composition tool built with a Go backend, a Svelte frontend, and a server-side FFmpeg pipeline.

It is designed as a lightweight "web OBS"-style studio for HLS-first workflows:

- multiple HLS video inputs
- image overlays
- text overlays
- uploaded fonts and images
- drag-and-drop layout editing in the browser
- server-side streaming that continues after the browser is closed
- HLS output or direct YouTube Live delivery
- a REST API that exposes the same project operations used by the UI

The application runs with Docker Compose and keeps all runtime state in a Docker volume.

This project should be treated as a trusted control plane, not as a public-facing web app. The recommended deployment is to keep the container bound to localhost and publish it to your private tailnet with Tailscale Serve.

## What It Does

Streaming Studio manages a single project state made of:

- a canvas
- a list of sources
- uploaded assets
- output settings

The server converts that project state into an FFmpeg `filter_complex` graph and runs FFmpeg as a long-lived background process. Because the actual streaming process lives on the server, the browser is only a control surface. Closing the tab does not stop the stream.

## Main Features

- Compose multiple HLS inputs on one canvas
- Upload images and place them as overlays
- Upload custom font files and use them for text rendering
- Add static text overlays
- Add remote text overlays that poll an HTTP endpoint and keep updating
- Control text background opacity independently from text color
- Move and resize sources visually from the canvas editor
- Control source order with z-index
- Configure source opacity and corner radius
- Edit canvas CSS and per-source CSS for browser-side preview
- Output as local HLS
- Output directly to YouTube Live through FFmpeg
- Use the REST API directly without going through the UI
- Keep the app private behind Tailscale instead of exposing it directly to the internet

## Architecture

The project is intentionally simple.

- Backend: Go
- Frontend: Svelte + Vite
- Renderer / streamer: FFmpeg
- Persistence: JSON project state on disk
- Runtime asset storage: local files under `/data`

At runtime, the system looks like this:

1. The browser edits project state through the REST API.
2. The server stores that state in `/data/state.json`.
3. When streaming is started, the backend builds the FFmpeg command line from the stored state.
4. FFmpeg reads HLS inputs, images, and generated text files and produces either HLS output or an RTMP stream for YouTube.
5. Remote text sources are refreshed by a background Go worker that rewrites text files under `/data/runtime/text`.
6. If FFmpeg exits unexpectedly, the backend retries the stream automatically unless the stop was explicitly requested.

## Quick Start

Start the application:

```bash
docker compose up --build -d
```

Then open:

```text
http://127.0.0.1:28080
```

The host port is `28080`, bound to `127.0.0.1` only. The container still listens on `8080`, but the host-side mapping is intentionally loopback-only so the app is not exposed directly to the LAN or the public internet.

To stop the stack:

```bash
docker compose down
```

To follow logs:

```bash
docker compose logs -f
```

## Docker Compose

The included `docker-compose.yml` is intentionally small:

- one service: `studio`
- one persistent volume: `studio-data`
- host port: `127.0.0.1:28080`

Persistent data is stored in the Docker volume and survives container recreation.

## Recommended Remote Access: Tailscale Serve

Streaming Studio has no built-in authentication. If you need remote access, the safest simple setup is:

1. keep the container bound to `127.0.0.1:28080`
2. install Tailscale on the Docker host
3. publish the local service to your tailnet with `tailscale serve`

Example:

```bash
docker compose up --build -d
tailscale serve --bg 28080
tailscale serve status
```

With this setup:

- the app is reachable from devices in your tailnet
- the app is not exposed directly on the host's LAN interface
- Tailscale account access becomes the effective authentication layer

If you want every tailnet member to access the app, keep your tailnet policy permissive for that node. If you want to restrict it, use Tailscale ACLs or grants.

## Persistent Files

The container stores data under `/data`.

Important locations:

- `/data/state.json`
  The persisted project state.

- `/data/assets/images`
  Uploaded image files.

- `/data/assets/fonts`
  Uploaded font files.

- `/data/runtime/text`
  Generated text files used by FFmpeg `drawtext`.

- `/data/output/hls`
  HLS output files when output mode is `hls`.

- `/data/logs/server.log`
  Backend server log.

- `/data/logs/ffmpeg.log`
  FFmpeg log output.

## User Interface

The browser UI is split into three main areas:

- Sources panel
  Create HLS, image, and text sources, and upload assets.

- Canvas editor
  Drag and resize sources visually.

- Inspector panel
  Edit the selected source or global project/output settings.

The UI uses the same REST API that external clients can use.

## Security Model

Streaming Studio is intentionally lightweight and does not implement application-layer authentication or authorization.

That means:

- anyone who can reach the HTTP service can read state, upload assets, edit sources, and start or stop streams
- YouTube stream keys and other sensitive output settings must be protected by network-layer access control

Recommended practice:

- do not expose `28080` directly on a public interface
- keep the service bound to localhost
- use Tailscale Serve, a VPN, or an authenticated reverse proxy in front of it

## Source Types

### HLS source

An HLS source reads an external HLS playlist URL and places the decoded video on the canvas.

Fields:

- `hls.url`
- `layout.x`
- `layout.y`
- `layout.width`
- `layout.height`
- `layout.opacity`
- `layout.radius`
- `layout.zIndex`
- `enabled`

Audio is taken from one HLS source only. You can choose it with `output.audioSourceId`. If you leave that empty, the first enabled HLS source is used.

### Image source

An image source uses an uploaded asset and places it on the canvas.

Fields:

- `image.assetId`
- `layout.x`
- `layout.y`
- `layout.width`
- `layout.height`
- `layout.opacity`
- `layout.radius`
- `layout.zIndex`
- `enabled`

### Text source

A text source renders with FFmpeg `drawtext`.

Fields:

- `text.content`
- `text.fontAssetId`
- `text.fontSize`
- `text.color`
- `text.backgroundColor`
- `text.borderColor`
- `text.borderWidth`
- `text.lineSpacing`
- `layout.x`
- `layout.y`
- `layout.opacity`
- `layout.zIndex`
- `enabled`

If no custom font is selected, the container uses Noto CJK as the default fallback font so Japanese text can render correctly.

### Remote text source

A text source can also be configured to poll a plain text endpoint continuously.

Fields:

- `text.remote.url`
- `text.remote.refreshIntervalSeconds`

Behavior:

- The backend periodically fetches the remote URL.
- The response body is written into `/data/runtime/text/<source-id>.txt`.
- FFmpeg uses `drawtext=textfile=...:reload=1`, so the visible text updates without restarting FFmpeg.
- If the remote fetch fails, the static `text.content` field is used as a fallback.

This is useful for status panels, telemetry overlays, scoreboards, or machine-generated captions.

## Output Modes

### HLS output

In `hls` mode, FFmpeg writes an HLS manifest and segments to the local data directory and the backend serves them over HTTP.

Relevant fields:

- `output.mode = "hls"`
- `output.hls.segmentDuration`
- `output.hls.listSize`
- `output.hls.path`
- `output.hls.publicPath`

`output.hls.path` must be a relative path inside the data directory. Absolute paths and parent-directory traversal such as `../...` are rejected.

Default public path:

```text
/live/live.m3u8
```

### YouTube Live output

In `youtube` mode, FFmpeg pushes directly to YouTube over RTMP.

Relevant fields:

- `output.mode = "youtube"`
- `output.youTube.rtmpUrl`
- `output.youTube.streamKey`
- `output.youTube.preset`
- `output.youTube.additionalArgs`

The default preset is:

```text
youtube-default
```

That preset adds:

- `-maxrate`
- `-bufsize`
- `-tune zerolatency`

to produce a reasonable low-latency default for YouTube delivery.

When `youtube` mode is active, the UI does not show a local HLS program preview because the actual output is no longer the local HLS manifest.

## Streaming Lifecycle

The application exposes explicit start and stop operations.

- Starting the stream launches FFmpeg from the saved project state.
- Stopping the stream sends `SIGTERM` to the FFmpeg process.
- Saving the full project or saving a source while a stream is already running causes the backend to restart FFmpeg so layout changes take effect immediately.
- Source-level saves, deletions, uploads, and stop/start operations preserve unrelated unsaved edits in the browser instead of wiping the local draft state.
- Remote text updates do not require FFmpeg restart because they update text files in place.

## REST API

The frontend uses the following API endpoints.

### Project state

- `GET /api/v1/state`
  Returns the full project and the current stream status.

- `PUT /api/v1/state`
  Replaces the full project state.

### Sources

- `POST /api/v1/sources`
  Creates a new source.

- `PUT /api/v1/sources/:id`
  Replaces one source by ID.

- `DELETE /api/v1/sources/:id`
  Deletes one source.

Notes:

- `layout.opacity = 0` is valid and preserved when explicitly sent.
- If `layout.opacity` is omitted on source creation, the backend defaults it to `1`.

### Runtime text

- `GET /api/v1/runtime/texts`
  Returns the currently resolved text content for text sources, including remote text content after polling.

### Assets

- `POST /api/v1/assets/images`
  Upload an image asset as multipart form data.

- `POST /api/v1/assets/fonts`
  Upload a font asset as multipart form data.

### Stream control

- `POST /api/v1/stream/start`
  Starts FFmpeg using the saved project state.

- `POST /api/v1/stream/stop`
  Stops the current FFmpeg process.

### Static and generated output

- `GET /uploads/...`
  Serves uploaded assets.

- `GET /live/...`
  Serves generated HLS files.

## API Example

Get the current state:

```bash
curl -s http://localhost:28080/api/v1/state
```

Start streaming:

```bash
curl -s -X POST http://localhost:28080/api/v1/stream/start
```

Create a remote text source:

```bash
curl -s -X POST http://localhost:28080/api/v1/sources \
  -H 'Content-Type: application/json' \
  --data '{
    "id": "remote-text",
    "name": "Remote Text",
    "kind": "text",
    "enabled": true,
    "layout": {
      "x": 40,
      "y": 40,
      "width": 520,
      "height": 240,
      "radius": 0,
      "opacity": 1,
      "rotation": 0,
      "zIndex": 10
    },
    "styleCSS": "",
    "text": {
      "content": "loading...",
      "fontAssetId": "",
      "fontSize": 24,
      "color": "#ffffff",
      "backgroundColor": "#111827",
      "borderColor": "#000000",
      "borderWidth": 0,
      "lineSpacing": 0,
      "remote": {
        "url": "http://example.internal/info.txt",
        "refreshIntervalSeconds": 5
      }
    }
  }'
```

Fetch currently resolved text values:

```bash
curl -s http://localhost:28080/api/v1/runtime/texts
```

## CSS and Preview Behavior

The browser UI supports:

- canvas-level custom CSS
- source-level custom CSS

This is useful for editing and previewing in the browser, but it is important to understand the boundary:

- Browser preview can reflect arbitrary CSS.
- FFmpeg output only reflects the properties that the backend explicitly translates into FFmpeg filters.

In practice, the server-side output reliably reflects:

- position
- size
- z-index ordering
- opacity
- corner radius for HLS/image sources
- text content
- fonts
- text colors
- text border
- text line spacing

Arbitrary browser CSS is not automatically translated into equivalent FFmpeg filters.

## Limitations

Current intentional limitations:

- No authentication
- One shared project state
- No multi-user coordination
- No scene collection system
- No source cropping or masking UI
- No advanced transition system
- No direct browser-based video input; HLS is the supported live input format
- No official YouTube API integration for reading YouTube-side stream health or viewer metrics

Operational caveats:

- This project is designed for trusted local or internal network environments unless you add your own auth/reverse proxy layer.
- The browser preview is an approximation of FFmpeg output, not a pixel-perfect renderer.
- HLS preview is only available when output mode is `hls`.
- Saving while streaming can restart FFmpeg when the change affects the composed output. Plan around that if you need a fully uninterrupted pipeline.

## Development

Backend tests:

```bash
GOCACHE=/tmp/go-build go test ./...
```

Run the full application through Docker Compose:

```bash
docker compose up --build
```

If you need the frontend tooling directly:

```bash
cd frontend
npm install
npm run dev
```

The Vite development server proxies:

- `/api`
- `/uploads`
- `/live`

to the backend on port `8080`.

## Troubleshooting

### The browser UI loads but the stream does not start

Check:

- `/data/logs/server.log`
- `/data/logs/ffmpeg.log`
- the current project state from `GET /api/v1/state`
- whether `output.hls.path` is a safe relative path inside `/data`

### Japanese text does not render

The container already installs Noto CJK fonts. If text still does not render, verify:

- the text source is enabled
- the text content is not empty
- the selected font file is valid if you uploaded one

### Remote text is not updating

Check:

- the remote URL is reachable from inside the container
- `text.remote.refreshIntervalSeconds`
- `/data/runtime/text/<source-id>.txt`
- `/data/logs/server.log`

### The program preview does not match the editor

Check the current output mode first.

- In `hls` mode, the UI can preview the generated manifest.
- In `youtube` mode, the UI cannot preview the actual YouTube output directly and instead shows a placeholder.

For text overlays, remember that the browser editor is only an approximation of FFmpeg rendering.

### A source save reverted other unsaved changes

Current behavior is that unrelated unsaved browser edits should be preserved across source saves, source deletes, uploads, and stop/start actions. If you still see state rollback, verify:

- the browser has refreshed to the latest frontend build
- the project was not reloaded from another client
- the request completed successfully and did not return a validation error

## License

The code in this repository is intended to be CC0, following the development policy used for this project.
