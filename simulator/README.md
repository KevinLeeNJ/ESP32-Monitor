# Desktop UI Simulator

This directory is the canonical host-side display simulator for the ESP32-S3
RLCD project. It renders real LVGL screens on a computer and writes a
pixel-accurate 400 x 300 PNG without connecting the board.

It is designed for future UI development: new screens are registered as
simulator scenes, and the same scene can be selected from the command line.
The simulator is not a mockup and does not use SDL.

## How it works

```text
registered LVGL scene
        |
        v
LVGL 9.4 RGB565 render
        |
        v
firmware/src/display/monitor_pack.c (RLCD 1bpp, 1=white / 0=black)
        |
        +--> <scene>_normal.png       visual preview from packed bytes
        +--> <scene>_normal.packed    exact panel bytes
        +--> <scene>_normal.rgb565    raw LVGL output
        +--> <scene>_normal.sha256    packed-frame digest
        +--> <scene>_normal.json      scene and artifact metadata
```

The PNG is reconstructed from the packed frame, not copied directly from the
LVGL RGB565 buffer. This means panel polarity, bit layout, and packing defects
are visible in the preview.

## Quick start

From any directory:

```bash
/Users/kevinlee/Documents/ESP32-Monitor/simulator/run.sh
```

The default scene is `monitor`; the primary preview is:

```text
simulator/output/latest/normal.png
```

The script also generates the `inverted` diagnostic frame. To simulate another
registered scene:

```bash
./simulator/run.sh dashboard
```

List available scenes:

```bash
./simulator/run.sh --list-scenes
```

## Adding a future LVGL screen

1. Put the real widget code under `firmware/app/ui/`, not under `simulator/`.
2. Expose a factory with the shape
   `lv_obj_t *my_screen_create(lv_obj_t *parent)`.
3. Register the factory in `simulator/src/scene_registry.c`.
4. Run `./simulator/run.sh my_screen`.
5. Inspect `simulator/output/latest/my_screen_normal.png`.
6. Run the same scene in the firmware when integrating it.

A scene that has an inverted diagnostic mode should also expose a factory
accepting `monitor_frame_variant_t`; the current monitor scene uses
`monitor_ui_create_variant()` for that purpose. A normal-only future scene can
ignore the variant.

The registry is intentionally small and explicit. Do not fork an existing
screen into a simulator-only implementation: the simulator must render the
same UI source that will be deployed.

## CLI

```bash
simulator/build/monitor_simulator --list-scenes
simulator/build/monitor_simulator --scene monitor --variant normal \
  --output simulator/output/latest
```

| Option | Meaning |
|---|---|
| `--scene NAME` | Select a registered LVGL scene; default `monitor` |
| `--variant normal\|inverted` | Render the normal or inverted diagnostic state |
| `--output DIR` | Output directory; nested directories are created |
| `--list-scenes` | Print registered scene names and descriptions |
| `--help` | Show CLI help |

## Agent workflow

1. Read the relevant UI source under `firmware/app/ui/` and the headers under `firmware/include/`.
2. Change the shared UI rather than creating a simulator-only copy.
3. Run `./simulator/run.sh <scene>`.
4. Inspect the scene's `*_normal.png` output.
5. Read the matching `.json` and `.sha256` for machine-readable results.
6. Treat a failing unit/e2e test or missing preview as an unsuccessful change.

Keep simulated frames deterministic: avoid wall-clock text, random values, and
unbounded animations in scenes used for visual regression checks. If a screen
uses time-dependent content, add a fixed test state or a deterministic scene
factory instead of changing the production UI contract.

## Output contract

For the default `monitor` scene:

| File | Meaning |
|---|---|
| `normal.png` | 400 x 300 visual preview reconstructed from packed bytes |
| `normal.rgb565` | 240,000-byte raw LVGL RGB565 frame |
| `normal.packed` | 15,000-byte RLCD 1bpp frame |
| `normal.sha256` | SHA-256 of `normal.packed` |
| `normal.json` | Scene, dimensions, formats, polarity and digest |
| `inverted.*` | Matching diagnostic output for the inverted variant |

For a non-default scene, files are named `<scene>_normal.*` and
`<scene>_inverted.*`.

The Waveshare RLCD reference driver uses `ColorWhite = 0xff` and
`ColorBlack = 0`, so the packed contract is **bit 1 = white, bit 0 = black**.
The packing layout and polarity are covered by unit tests and must remain
shared with the firmware.

## Tests

`./simulator/run.sh` runs both layers:

- `monitor_unit_tests`: SHA-256 vectors, RGB565/1bpp round trips, panel corner
  mapping and polarity, deterministic LVGL rendering, and PNG generation.
- `monitor_simulator_e2e`: executes the real simulator CLI for both variants,
  validates file sizes, PNG signature and 400 x 300 dimensions, packed-frame
  hashes, scene metadata, scene listing, and unknown-scene rejection.

Run only the tests after a build with:

```bash
ctest --test-dir simulator/build --output-on-failure
```

## Source locations

| Path | Role |
|---|---|
| `simulator/src/main.c` | Scene-aware CLI and artifact writer |
| `simulator/src/scene_registry.c` | Registry of simulated LVGL screens |
| `simulator/tests/` | Unit and end-to-end checks |
| `firmware/app/ui/monitor_ui.c` | Shared LVGL UI used by simulator and firmware |
| `firmware/src/display/monitor_render.c` | Reusable off-screen RGB565 renderer |
| `firmware/src/display/monitor_pack.c` | Shared RLCD 1bpp conversion |
| `third_party/lvgl/` | Pinned LVGL v9.4.0 submodule |
| `third_party/lv_conf.h` | Shared LVGL configuration |
