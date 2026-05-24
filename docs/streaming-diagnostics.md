# Streaming Diagnostics

This note records the HLS-to-YouTube stutter investigation and the FFmpeg rules that came out of it.

## Incident Summary

The observed failure was severe YouTube playback stutter from an HLS source, with audio apparently missing during playback checks. Two early explanations were not sufficient:

- Reducing output to 30 fps should not make motion catastrophically uneven by itself.
- Doubling the video bitrate cannot turn a heavily duplicated or mistimed frame stream into smooth motion.

The real issue was timestamp handling at the FFmpeg HLS input boundary.

## Root Cause

Do not use `-use_wallclock_as_timestamps 1` for segmented HLS input in this pipeline.

With normal HLS packet timestamps, FFmpeg sees the source frames in media time. For a 60 fps source, diagnostic output from `showinfo` should advance in regular steps such as:

```text
0, 0.0166667, 0.0333333, ...
```

With `-use_wallclock_as_timestamps 1`, packet timestamps are replaced with read-time wallclock values. For an HLS playlist, FFmpeg can read a segment in a burst, so many frames collapse into the same or nearly the same timestamp range:

```text
0, 0.0057, 0.0080, 0.0086, ...
```

After that damaged input is passed through `setpts=PTS-STARTPTS,fps=<outputFrameRate>`, FFmpeg has no clean media-time cadence to preserve. The output can contain repeated identical video frames, which looks like severe stutter even when the nominal output says 30 fps or 60 fps.

## Correct HLS Input Policy

For HLS sources, the pipeline should:

- Preserve stream-native packet timestamps.
- Pace the synthetic `lavfi` background input with `-re` so it cannot become an unbounded master clock if an HLS source stalls.
- Add `-re` immediately before each HLS input so FFmpeg reads the source at media rate instead of burst-reading HLS segments faster than realtime.
- Keep reconnect options on the HLS input so transient HTTP failures can recover.
- Avoid `-use_wallclock_as_timestamps`.
- Use `setpts=PTS-STARTPTS,fps=<outputFrameRate>` in the HLS video filter before scaling, so frame dropping is explicit and happens before expensive scaling/compositing.
- Use `aresample=async=1:first_pts=0` for the selected HLS audio input to normalize audio timing at the output boundary.

The important distinction is that `-re` controls input pacing while keeping media timestamps, but `-use_wallclock_as_timestamps` replaces the timestamps and can corrupt the frame cadence for this workload.

## Expected FFmpeg Shape

For each HLS source, the generated command should contain input options similar to:

```text
-re
-thread_queue_size 1024
-fflags +genpts+igndts
-reconnect 1
-reconnect_streamed 1
-reconnect_on_network_error 1
-reconnect_on_http_error 4xx,5xx
-reconnect_delay_max 10
-rw_timeout 15000000
-i <hls-url>
```

The synthetic background input should also be paced:

```text
-re -f lavfi -i color=...
```

It should not contain:

```text
-use_wallclock_as_timestamps 1
```

For an HLS video source, the filter should reduce cadence before scaling:

```text
setpts=PTS-STARTPTS,fps=<outputFrameRate>,scale=<width>:<height>:flags=lanczos
```

For YouTube output, the generated command should also keep constant-frame-rate and GOP behavior explicit:

```text
-r <outputFrameRate>
-g <outputFrameRate * 2>
-keyint_min <outputFrameRate * 2>
-vsync cfr
-sc_threshold 0
-force_key_frames expr:gte(t,n_forced*2)
```

## Diagnosis Commands

Inspect the running command:

```bash
curl -s http://127.0.0.1:28080/api/v1/state | jq -r '.stream.command'
```

Inspect FFmpeg logs:

```bash
docker compose logs --tail=200 studio
```

Or, if the server log API is enabled in the running build:

```bash
curl -s 'http://127.0.0.1:28080/api/v1/logs?target=ffmpeg&tail=200'
```

Check whether the source has video/audio streams:

```bash
ffprobe -hide_banner -show_streams '<hls-url>'
```

Check input video timestamps without wallclock replacement:

```bash
ffmpeg -hide_banner -loglevel info -t 3 -i '<hls-url>' -an -vf 'showinfo' -f null -
```

For comparison only, this command demonstrates the bad behavior and should not be used in production:

```bash
ffmpeg -hide_banner -loglevel info -use_wallclock_as_timestamps 1 -t 3 -i '<hls-url>' -an -vf 'showinfo' -f null -
```

Check YouTube-side renditions:

```bash
yt-dlp -F '<youtube-live-url>'
```

## Healthy Signs

A healthy YouTube push should show these traits in FFmpeg logs:

- `fps` close to the configured output frame rate.
- `speed` close to `1x`.
- No repeated reconnect loop for the HLS source.
- No wallclock timestamp input option in the active command.
- YouTube exposes the expected rendition, for example `720p60` for a 1280x720 60 fps output.

Bitrate is still important for compression quality, but it is not the primary fix for timestamp-induced stutter.
