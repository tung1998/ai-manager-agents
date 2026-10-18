# Media

The launch video (1:44, Vietnamese captions) is on YouTube: https://youtu.be/kkyxZQOXYqA. `README.md` and `README.vi.md` link to it through its YouTube thumbnail; the guide (`docs/guide/index.html`, `vi.html`) embeds it.

The video is not built in this repo. Its source (a Remotion project: scenes, copy, screenshots, music) lives in **`../agent-office-video`**, the folder next to this repo; edit and render it there (`pnpm install && pnpm music && pnpm render`, see its README), then upload the new `out/demo.mp4` to YouTube and update the video id in the READMEs and the guide.
