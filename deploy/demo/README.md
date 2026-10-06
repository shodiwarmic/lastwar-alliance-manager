# deploy/demo

The public demo's own deployment: two Cloud Run services and the fixtures image. Nothing here
ships to an operator — the release classifier treats this folder as neutral.

| File | What |
|---|---|
| `cloudrun-fixtures.yaml` | The fixtures service (stand-in OCR + Collabora). Deploy first. |
| `cloudrun-app.yaml` | The app, the ordinary released image with `DEMO_MODE=true`. |
| `Dockerfile.fixtures` | Builds `cmd/demo-fixtures`. |

The procedure, the environment variables and the checks after a deploy are in
[`docs/DEMO.md`](../../docs/DEMO.md).
