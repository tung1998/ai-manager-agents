# Media

- `agent-office-launch.mp4`: the launch video (1:44, Vietnamese captions), linked from `README.md` and `README.vi.md`; `agent-office-launch.jpg` is its poster.

The video is not built in this repo. Its source (a Remotion project: scenes, copy, screenshots, music) lives in **`../agent-office-video`**, the folder next to this repo; edit and render it there (`pnpm install && pnpm music && pnpm render`, see its README), then copy `out/demo.mp4` here as `agent-office-launch.mp4` (compressed: `ffmpeg -i out/demo.mp4 -crf 27 -preset slow -c:a aac -b:a 128k -movflags +faststart agent-office-launch.mp4`).
