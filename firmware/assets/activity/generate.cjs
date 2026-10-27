const sharp = require('sharp');
const fs = require('fs');
const dir = 'firmware/assets/activity/';
const icons = ['openai', 'github', 'circle-question-mark', 'circle-minus', 'loader-circle', 'message-circle', 'shield-alert', 'circle-check', 'circle-x', 'circle-slash'];
(async () => {
 let out = '/* Generated from SVGs: OpenAI (https://cdn.openai.com/brand/OpenAI-Logos-2025.zip), GitHub (https://narakcode.vercel.app/blocks), and Lucide (ISC; see firmware/assets/activity/LUCIDE-LICENSE). */\n#include "lvgl.h"\n\n';
 for (const icon of icons) {
  const id = icon.replaceAll('-', '_');
  const src = await sharp(dir + icon + '.svg').resize(96, 96).ensureAlpha().trim({background: '#00000000', threshold: 10}).png().toBuffer();
  const raw = await sharp(src).resize(24, 24, {fit: 'contain'}).ensureAlpha().raw().toBuffer();
  const bytes=[];
  for (let i=3; i<raw.length; i+=4) bytes.push(raw[i] >= 96 ? 255 : 0);
  out += `static const uint8_t ${id}_data[] = {\n`;
  for(let y=0;y<24;y++) out += '    '+bytes.slice(y*24,(y+1)*24).join(', ')+',\n';
  out += `};\nconst lv_image_dsc_t activity_${id} = {\n    .header = {.magic = LV_IMAGE_HEADER_MAGIC, .cf = LV_COLOR_FORMAT_A8, .w = 24, .h = 24, .stride = 24},\n    .data_size = sizeof(${id}_data), .data = ${id}_data,\n};\n\n`;
 }
 fs.writeFileSync('firmware/app/ui/activity_assets.c', out.trimEnd() + '\n');
})().catch(e => { console.error(e); process.exit(1); });
