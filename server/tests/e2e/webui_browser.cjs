/* Real browser + production Server. Requires Playwright and Chrome.
 * NODE_PATH=<playwright modules> node tests/e2e/webui_browser.cjs <server> <screenshots>
 */
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const net = require('node:net');
const { spawn } = require('node:child_process');
const { once } = require('node:events');
const crypto = require('node:crypto');

(async () => {
 const tmp = await fs.mkdtemp(path.join(os.tmpdir(), 'monitor-webui-'));
 const output = path.resolve(process.argv[3] || path.join(tmp,'screenshots'));
 await fs.mkdir(output,{recursive:true});
 const listener = net.createServer();listener.listen(0,'127.0.0.1');await once(listener,'listening');const port=listener.address().port;await new Promise(r=>listener.close(r));
 const base='http://127.0.0.1:'+port;
 const readToken=crypto.randomBytes(32).toString('hex');const config=path.join(tmp,'monitor.yaml');await fs.writeFile(config,JSON.stringify({listen:'127.0.0.1:'+port,public_base_url:base,database:path.join(tmp,'monitor.db'),read_token:readToken}),{mode:0o600});
 const server=spawn(path.resolve(process.argv[2]),['serve','--config',config],{stdio:['ignore','ignore','pipe']});let logs='';server.stderr.on('data',b=>logs+=b);
 let browser;
 try {
 for(let i=0;i<100&&!logs.includes('WebUI 临时密码:');i++)await new Promise(r=>setTimeout(r,100));
 const bootstrap=logs.match(/WebUI 临时密码: ([a-f0-9]{32})/)?.[1];assert(bootstrap,'Server bootstrap failed');
 browser=await chromium.launch({headless:true,channel:'chrome'});
 const context=await browser.newContext({viewport:{width:1440,height:1000}});
 const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(base);await page.getByRole('heading',{name:'登录控制台'}).waitFor();await page.screenshot({path:path.join(output,'login.png')});
 await page.locator('#login-password').fill(bootstrap);await page.getByRole('button',{name:'登录',exact:true}).click();await page.getByRole('heading',{name:'设置你的登录密码'}).waitFor();
 assert.equal(await page.locator('#sidebar').isVisible(),false);
 await page.locator('#current-password').fill(bootstrap);await page.locator('#new-password').fill('browser permanent passphrase');await page.locator('#repeat-password').fill('browser permanent passphrase');await page.getByRole('button',{name:'保存新密码'}).click();
 await page.getByRole('heading',{name:'设备注册',exact:true}).waitFor();
 await page.locator('[data-page=token]').click();assert.equal(await page.locator('#read-token').inputValue(),'');await page.getByRole('button',{name:'显示 Token'}).click();await page.waitForFunction(t=>document.querySelector('#read-token').value===t,readToken);assert.equal(await page.locator('#read-token').getAttribute('type'),'text');await page.getByRole('button',{name:'隐藏 Token'}).click();assert.equal(await page.locator('#read-token').getAttribute('type'),'password');await page.locator('[data-page=devices]').click();
 await page.locator('#device-name').fill('测试工作站');await page.getByRole('button',{name:'添加设备并生成验证码'}).click();await page.locator('.code-value').waitFor();
 const code=await page.locator('.code-value').innerText();
 await page.screenshot({path:path.join(output,'devices.png'),fullPage:true});
 const registration=await page.request.post(base+'/v2/collector/register',{data:{code}});assert.equal(registration.status(),200);const claim=await registration.json();
 const selections=[['cpu.model'],['system.os'],['memory.pressure'],['drive.model',3],['drive.total',3],['drive.used',3],['drive.read_rate',3],['drive.write_rate',3],['drive.smart_health',3],['interface.medium',5],['interface.up',5],['interface.upload_rate',5],['interface.download_rate',5],['interface.ipv4',5],['battery.level',7],['battery.charging',7],['gpu.usage',9],['core.usage',11]];
 const profile={version:2,interval_seconds:10,enabled:selections.map(([metric,id])=>id?{metric,objects:[id]}:{metric}),inventory:{objects:[{id:3,kind:'drive',name:'Browser Fixture SSD',model:'Browser Fixture SSD'},{id:5,kind:'interface',name:'以太网 2',model:'Intel Ethernet Controller'},{id:7,kind:'battery',name:'系统电池'},{id:9,kind:'gpu',name:'NVIDIA GeForce RTX 5080',model:'NVIDIA GeForce RTX 5080'},{id:11,kind:'core',name:'逻辑核 0'},{id:12,kind:'gpu',name:'Unselected GPU'}],cores:[{id:11,index:0}],gpus:[{id:9}],cpu:{model:'Browser Fixture CPU'},os:{family:'darwin',name:'macOS',version:'test fixture'},memory_total:'17179869184',drives:[{id:3,model:'Browser Fixture SSD',total:'1099511627776'}],interfaces:[{id:5,medium:'ethernet'}],batteries:[{id:7}]}};
 const headers={Authorization:'Bearer '+claim.token};const prof=await page.request.put(base+'/v2/collector/profile',{headers:{...headers,'If-None-Match':'*'},data:profile});assert.equal(prof.status(),200);
 const now=Math.floor(Date.now()/1000);
 for(let i=0;i<6;i++){const t=now-5+i;const res=await page.request.post(base+'/v2/collector/samples',{headers,data:{r:1,id:crypto.randomBytes(16).toString('hex'),s:[{t,c:1250+i*100,m:6200,u:86400+i,mem:{u:'10651518976',p:0},disk:[{i:3,u:'0',r:'0',w:'1048576',sm:{t:now-5,h:true}}],net:[{i:5,up:false,tx:'0',rx:'1048576',ip:[]}],bat:[{i:7,l:8500,c:false}],gpu:[{i:9,u:5000}],cores:[{i:11,u:2500}]}]}});assert.equal(res.status(),204)}
 await page.getByRole('button',{name:'查看指标',exact:true}).click();await page.getByRole('heading',{name:'CPU',exact:true}).waitFor();
 assert.equal(await page.getByRole('heading',{name:'Unselected GPU',exact:true}).count(),0);
 const diskCard=page.locator('.hardware-card[data-object-kind=drive][data-object-id="3"]');assert.equal(await diskCard.locator('h2').innerText(),'Browser Fixture SSD');assert(await diskCard.getByText('1 TiB',{exact:true}).count()>0);assert(await diskCard.getByText('0 B',{exact:true}).count()>0);assert(await page.locator('.hardware-card').filter({has:page.getByRole('heading',{name:'CPU',exact:true})}).getByText('Browser Fixture CPU',{exact:true}).count()>0);assert(await page.locator('.hardware-card').filter({has:page.getByRole('heading',{name:'内存',exact:true})}).getByText('16 GiB',{exact:true}).count()>0);assert(await page.getByText('未连接',{exact:true}).count()>0);assert.equal(await page.locator('.hardware-card[data-object-kind=interface][data-object-id="5"] h2').innerText(),'以太网 2');assert.equal(await page.locator('.hardware-card[data-object-kind=gpu][data-object-id="9"] h2').innerText(),'NVIDIA GeForce RTX 5080');assert.equal(await page.locator('.hardware-card[data-object-kind=battery][data-object-id="7"] h2').innerText(),'系统电池');assert(await page.getByText('逻辑核 0',{exact:true}).count()>0);assert(await page.getByText('无地址',{exact:true}).count()>0);
 await page.locator('#panel-disk').click();assert(await page.getByRole('listbox',{name:'DISK 指标来源'}).isVisible());await page.keyboard.press('End');await page.keyboard.press('Enter');assert((await page.locator('#panel-disk').innerText()).includes('Browser Fixture SSD'));await page.locator('#panel-disk').press('ArrowDown');await page.keyboard.press('Escape');assert.equal(await page.locator('#panel-disk').getAttribute('aria-expanded'),'false');
 assert.equal(await page.locator('.sidebar').evaluate(e=>getComputedStyle(e).backgroundColor),'rgb(37, 37, 37)');
 assert((await page.locator('.sidebar').evaluate(e=>getComputedStyle(e).fontFamily)).includes('FusionPixel'));
 await page.screenshot({path:path.join(output,'metrics.png'),fullPage:true});
 const patch=await page.request.patch(base+'/v2/collector/profile',{headers:{...headers,'If-Match':JSON.stringify('1')},data:[{op:'replace',path:'/inventory/objects/1/name',value:'Ethernet renamed'}]});assert.equal(patch.status(),200);await page.getByRole('heading',{name:'Ethernet renamed',exact:true}).waitFor();
 // An actively edited settings form must survive refresh without losing its value or focus.
 await page.locator('#panel-name').fill('EDITING');await page.waitForTimeout(5500);assert.equal(await page.locator('#panel-name').inputValue(),'EDITING');assert.equal(await page.locator('#panel-name').evaluate(e=>document.activeElement===e),true);
 await page.locator('.sample-chart').first().waitFor();assert((await page.locator('.sample-chart').first().getAttribute('aria-label')).includes('最新'));assert.equal(await page.locator('.sr-only table').count(),2);
 // Polling must keep keyboard focus on device selectors and action buttons.
 const selectorButton=page.locator('#device-selector button').first();await selectorButton.focus();await page.waitForTimeout(5500);assert.equal(await selectorButton.evaluate(e=>document.activeElement===e),true);
 await page.locator('[data-page=devices]').click();await page.getByRole('heading',{name:'设备注册',exact:true}).waitFor();
 const metricsButton=page.getByRole('button',{name:'查看指标',exact:true});await metricsButton.focus();await page.waitForTimeout(5500);assert.equal(await metricsButton.evaluate(e=>document.activeElement===e),true);
 await page.reload();await page.getByRole('heading',{name:'设备注册',exact:true}).waitFor();assert.equal(await page.locator('#login-password').count(),0);
 const mobile=await context.newPage();await mobile.setViewportSize({width:390,height:844});assert.equal(await mobile.evaluate(()=>innerWidth),390);await mobile.goto(base);await mobile.getByRole('heading',{name:'设备注册',exact:true}).waitFor();assert(await mobile.locator('#logout-mobile').isVisible());
 assert.equal(await mobile.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
 await mobile.screenshot({path:path.join(output,'mobile-devices.png'),fullPage:true});await mobile.getByRole('button',{name:'查看指标',exact:true}).click();await mobile.getByRole('heading',{name:'Browser Fixture SSD',exact:true}).waitFor();assert.equal(await mobile.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);await mobile.screenshot({path:path.join(output,'mobile-metrics.png'),fullPage:true});await mobile.locator('[data-page=devices]').click();await mobile.getByRole('heading',{name:'设备注册',exact:true}).waitFor();
 await mobile.getByRole('button',{name:'删除设备',exact:true}).click();await mobile.locator('dialog[open]').waitFor();await mobile.getByRole('button',{name:'取消',exact:true}).click();assert(await mobile.getByRole('heading',{name:'测试工作站',exact:true}).count()>0);
 await page.getByRole('button',{name:'退出登录',exact:true}).click();await page.getByRole('heading',{name:'登录控制台'}).waitFor();
 assert.deepEqual(errors,[]);assert(!logs.includes(claim.token));
 console.log('PASS: browser login/forced change, device registration, live metrics, zero/false values, pixel theme, persistent Cookie, focused form, mobile and safe delete dialog');
 } finally {
 if(browser)await browser.close();server.kill('SIGTERM');await once(server,'exit');await fs.rm(tmp,{recursive:true,force:true});
 }
})().catch(e=>{console.error(e.message);process.exitCode=1});
