const sharp = require('sharp');
const fs = require('fs');
const dir = 'firmware/assets/activity/';
const icons = ['openai', 'github', 'circle-question-mark', 'circle-minus', 'loader-circle', 'message-circle', 'shield-alert', 'circle-check', 'circle-x', 'circle-slash'];
(async () => {
 let out = '/* Generated from SVGs: OpenAI (https://cdn.openai.com/brand/OpenAI-Logos-2025.zip), GitHub (https://narakcode.vercel.app/blocks), and Lucide (ISC; see firmware/assets/activity/LUCIDE-LICENSE). */\n#include "lvgl.h"\n\n';
 for (const icon of icons) {
  const id = icon.replaceAll('-', '_');
  const task = icon !== 'openai' && icon !== 'github';
  const size = task ? 28 : 24;
  const src = task
   ? await sharp(dir + icon + '.svg').resize(26, 26).extend({top: 1, bottom: 1, left: 1, right: 1, background: '#00000000'}).png().toBuffer()
   : await sharp(dir + icon + '.svg').resize(96, 96).ensureAlpha().trim({background: '#00000000', threshold: 10}).png().toBuffer();
  const raw = task ? await sharp(src).ensureAlpha().raw().toBuffer()
                   : await sharp(src).resize(24, 24, {fit: 'contain'}).ensureAlpha().raw().toBuffer();
  const bytes=[];
  for (let i=3; i<raw.length; i+=4) bytes.push(raw[i] >= 96 ? 255 : 0);
  out += `static const uint8_t ${id}_data[] = {\n`;
  for(let y=0;y<size;y++) out += '    '+bytes.slice(y*size,(y+1)*size).join(', ')+',\n';
  out += `};\nconst lv_image_dsc_t activity_${id} = {\n    .header = {.magic = LV_IMAGE_HEADER_MAGIC, .cf = LV_COLOR_FORMAT_A8, .w = ${size}, .h = ${size}, .stride = ${size}},\n    .data_size = sizeof(${id}_data), .data = ${id}_data,\n};\n\n`;
 }
 fs.writeFileSync('firmware/app/ui/activity_assets.c', out.trimEnd() + '\n');
})().catch(e => { console.error(e); process.exit(1); });
