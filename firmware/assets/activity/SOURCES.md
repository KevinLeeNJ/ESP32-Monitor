# Activity icons

- `openai.svg`: OpenAI black monoblossom, from the [official OpenAI logo package](https://cdn.openai.com/brand/OpenAI-Logos-2025.zip); see [OpenAI brand guidelines](https://openai.com/brand/). The mark remains OpenAI's trademark.
- `github.svg`: GitHub mark, reproduced from [GitHub icon SVG](https://narakcode.vercel.app/blocks); GitHub is a trademark of GitHub, Inc.
- Other SVGs: [Lucide](https://github.com/lucide-icons/lucide/tree/main/icons), ISC license; see `LUCIDE-LICENSE`.

The assets in `firmware/app/ui/activity_assets.c` are alpha masks, rasterized from these SVGs and thresholded to black and transparent pixels for the RLCD. SVGs are retained as source assets.

To regenerate from the repository root, install Node.js and `sharp`, then run `node firmware/assets/activity/generate.cjs`. The script trims and resizes the two brand marks to 24 x 24. Task icons use 28 x 28 canvases with their original SVG margins preserved. All masks use a thresholded alpha channel.
