# Output Presets

Streaming Studio can save output settings as named presets. A preset stores the full
`OutputSettings` object, including HLS settings, YouTube RTMP settings, bitrate,
frame rate, audio source, and additional FFmpeg arguments.

The active preset ID is stored in `activeOutputPresetId`. Selecting a preset copies
its settings into the current `output` settings. The user must save the project to
persist the selected preset and current output settings.

When an HLS source is deleted, any matching `audioSourceId` is cleared from both
the current output settings and all saved output presets. This keeps later project
saves from failing validation after source cleanup.

The project state is saved to `/data/state.json` in the container and mirrored to
`/sanjou9000/backup/streaming-studio/project-state/state.json`. The mirror path is
controlled by `STATE_BACKUP_PATH` and is mounted through docker compose from
`/sanjou9000/backup` on the host.

Preset validation uses the same output validation as the current project output:

- HLS output paths must stay inside the data directory.
- HLS public paths must be local paths.
- Audio source IDs must reference an HLS source.
- Output mode must be `hls` or `youtube`.
