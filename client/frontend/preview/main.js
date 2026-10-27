const pages = [['overview','pixelart-icons-font-layout','总览'],['metrics','pixelart-icons-font-chart-bar-big','指标采集'],['server','pixelart-icons-font-server','Server 连接'],['companion','pixelart-icons-font-monitor','上位机'],['startup','pixelart-icons-font-power-off','启动与后台'],['diagnostics','pixelart-icons-font-tools','诊断']];
// Example capabilities follow Agent v2 semantic IDs; only available rows render.
const categories = {
 'CPU': [
  ['cpu.usage','总体 CPU 占用率','全部逻辑核心','24.8%',true,false,true],
  ['cpu.model','CPU 信息','型号与核心数 · 启动时读取','Apple M4 · 10 核',false,true,true],
  ['core.usage','逐核 CPU 占用率','按逻辑核心选择',[['cpu0','12.5%'],['cpu1','18.2%']],false,false,true],
  ['cpu.load1','CPU 负载','1 / 5 / 15 分钟','1.42 / 1.18 / 1.06',false,false,true],
  ['cpu.temperature','CPU 平均温度','当前可读传感器','47.9 °C',false,false,true]],
 '内存（Swap）': [
  ['memory.usage','内存','占用率、已用与总量','58.4% · 9.34 / 16 GiB',true,false,true],
  ['memory.app','内存构成','App / Wired / 压缩','38.25% / 7.56% / 5.25%',false,false,true],
  ['memory.swap_total','Swap','已用 / 总量','1.2 / 2 GiB',false,false,true],
  ['memory.pressure','内存压力','macOS','正常',false,false,true]],
 'GPU': [
  ['gpu.usage','GPU 占用率','按设备选择',[['集成 GPU','36%'],['外置 GPU','24%']],false,false,true],
  ['gpu.temperature','GPU 平均温度','按设备选择',[['集成 GPU','39.0 °C'],['外置 GPU','42.1 °C']],false,false,true]],
 '磁盘': [
  ['drive.model','磁盘信息','型号、容量与已用',[['disk0','内置 SSD · 295 / 512 GiB'],['disk1','外置 SSD · 120 / 1024 GiB']],false,false,true],
  ['drive.read_rate','磁盘读写','读取 / 写入',[['disk0','4.2 / 1.1 MiB/s'],['disk1','0.8 / 0.2 MiB/s']],false,false,true],
  ['drive.smart_health','SMART 健康状态','最近观测',[['disk0','健康'],['disk1','健康']],false,false,true],
  ['drive.smart_attribute','SMART 细项','介质错误计数',[['disk0','0'],['disk1','0']],false,false,true]],
 '网络': [
  ['interface.medium','网络连接','类型与状态',[['en0','Wi-Fi · 已连接'],['en1','有线 · 已连接']],false,false,true],
  ['interface.upload_rate','网络速度','上传 / 下载',[['en0','128 KiB/s / 2.4 MiB/s'],['en1','24 / 110 KiB/s']],false,false,true],
  ['interface.ipv4','IP 地址','本地 / 公网 · 公网地址需外部查询',[['en0','192.168.1.20 / 待查询'],['en1','192.168.2.20 / 待查询']],false,false,true]],
 '电池': [
  ['battery.level','电池状态','内置电池 · 电量与充电状态','85% · 未充电',false,false,true]],
 '系统指标': [
  ['system.uptime','运行时间','本机','2 天 4 小时',true,false,true],
  ['system.os','设备信息','启动时读取','macOS · MacBook Air',false,true,true],
  ['fan.rpm','系统风扇转速','此示例设备无风扇','—',false,false,false]]
};
function availableCategories(){return Object.keys(categories).filter(category=>availableRows(category).length>0)}
function availableRows(category){return categories[category].filter(row=>row[6]&&(category!=='GPU'||gpuAvailable))}
let page='overview', tab='CPU', lastRenderedPage=null, overviewSample=0;
const sampleValues=[['24.8%','58.4%','64.2%'],['38.1%','61.7%','64.5%'],['17.2%','55.3%','65.0%']];
let githubLoggedIn=false, codexConnected=false, musicState='unavailable', trackIndex=0, gpuAvailable=true;
const tracks=[['The Night We Met','Lord Huron','Spotify'],['Dreams','Fleetwood Mac','系统播放器'],['日落大道','梁博','系统播放器']];
const metricEnabled = Object.fromEntries(Object.values(categories).flat().map(row => [row[0], row[4]]));
const metricParents={'interface.upload_rate':'interface.medium','interface.ipv4':'interface.medium','gpu.temperature':'gpu.usage','drive.read_rate':'drive.model','drive.smart_health':'drive.model','drive.smart_attribute':'drive.model'};
let metricInterval=10;
const app=document.querySelector('#app');
function button(label,action,primary=false){return `<button class="btn ${primary?'primary':''}" data-action="${action}">${label}</button>`}
function title(eyebrow,name,desc,action=''){return `<div class="heading"><div><div class="eyebrow">${eyebrow}</div><h1>${name}</h1><p>${desc}</p></div>${action}</div>`}
function card(title,body,span='span6',extra=''){return `<section class="card ${span}"><div class="card-title"><h2>${title}</h2>${extra}</div>${body}</section>`}
function row(name,detail,right){return `<div class="row"><div><strong>${name}</strong><small>${detail}</small></div><div class="row-right">${right}</div></div>`}
function toggle(checked=false, label='切换', disabled=false){return `<input class="switch" type="checkbox" aria-label="${label}" ${checked?'checked':''} ${disabled?'disabled':''}>`}
function stat(name,value,sub,icon){return `<section class="card span3"><div class="stat-head">${name}<span class="stat-icon"><i class="pixelart-icons-font-${icon}" aria-hidden="true"></i></span></div><div class="stat-value">${value}</div><div class="stat-sub">${sub}</div></section>`}
function overview(){return title('OVERVIEW','总览','查看采集和设备状态。',button('模拟数据变化','simulate',true))+`<div class="banner"><div><b>开始配置</b>　连接 Server，选择要采集的指标。</div>${button('查看配置步骤','setup')}</div><div class="grid">`+
stat('采集状态','运行中','示例 · 最近采样 10 秒前','chart')+stat('最近上传','刚刚','示例 · 10 秒上传一次','upload')+stat('Server 连接','已连接','示例 · 延迟 28 ms','server')+stat('ESP32 设备','1 台','示例 · 局域网连接','cpu')+
card('本机指标',row('CPU 使用率','总体 · 最近一次采样',`<span class="value" data-roll="cpu">${sampleValues[overviewSample][0]}</span>`)+row('内存使用率','已使用 9.3 / 16 GB',`<span class="value" data-roll="memory">${sampleValues[overviewSample][1]}</span>`)+row('磁盘使用率','系统磁盘 /',`<span class="value" data-roll="disk">${sampleValues[overviewSample][2]}</span>`),'span5',button('管理指标','go:metrics'))+
card('数据活动',`<div class="muted">采集示意 · 最近 30 分钟</div><div class="chart">${[24,35,27,42,34,46,38,55,44,38,50,61,48,42,54,46,62,55,43,52,46,58,50,65,51,46,58,48,56,42].map(x=>`<i style="height:${x}%"></i>`).join('')}</div>`,'span7','<span class="pill">● 采集中</span>')+
card('ESP32 设备',`<div class="device"><div class="device-icon" aria-hidden="true"><i class="pixelart-icons-font-cpu"></i></div><div class="device-info"><h3>Studio Monitor　<span class="pill">在线</span></h3><p>局域网 · 192.168.1.88<br>上位机模式 · 最近更新 2 秒前<br>断开后自动回退至独立模式</p></div></div>`,'span7',button('设备详情','go:companion'))+
card('上位机数据源',row('GitHub 贡献墙','示例账号 · 15 分钟刷新','<span class="pill">已授权</span>')+row('Codex 桌面端','额度与所选任务','<span class="pill">已连接</span>')+row('系统播放','当前播放信息','<span class="pill gray">未播放</span>'),'span5',button('管理','go:companion'))+'</div>'}
function metrics(){
 if(!availableCategories().includes(tab))tab=availableCategories()[0]||'CPU';
 const rows=availableRows(tab).map(([id,name,detail,value,required,fixed])=>{
  const info=`<div class="metric-title"><strong>${name}</strong></div>`;
  if(Array.isArray(value)){
   const objects=value.map(([object,shown],index)=>`<div class="metric-object" data-object-row="${object}"><span class="metric-object-name">${object}</span><span class="value">${shown}</span>${id==='core.usage'?(index===0?`<label><input class="switch" type="checkbox" aria-label="上传 逐核 CPU 占用率" data-metric="${id}" ${metricEnabled[id]?'checked':''}></label>`:'<span></span>'):`<label><input class="switch" type="checkbox" aria-label="上传 ${name} ${object}" data-metric="${id}" data-object="${object}" ${metricEnabled[`${id}:${object}`]?'checked':''} ${metricParents[id]&&!metricEnabled[`${metricParents[id]}:${object}`]?'disabled title="请先启用该设备的主指标"':''}></label>`}</div>`).join('');
   return `<div class="metric-row object-row" data-metric-row="${id}">${info}<div class="metric-objects">${objects}</div></div>`;
  }
  return `<div class="metric-row" data-metric-row="${id}">${info}<div class="value">${value}</div><div>${required?'<span class="pill required-metric" title="自动采集并上传">必选</span>':`<input class="switch" type="checkbox" aria-label="上传 ${name}" data-metric="${id}" ${metricEnabled[id]?'checked':''}>`}</div></div>`
 }).join('');
 return title('COLLECTION','指标采集','总体 CPU、内存占用率和运行时间自动上传，其余按需选择。',button('应用更改','apply',true))+`<div class="banner"><div><b>仅显示当前可获取的指标。</b> 固定信息启动时读取一次，周期上传不重复携带。</div>${button('重新检测','rescan-metrics')}</div><div class="grid">${card('采集配置',`<div class="row"><div><strong>采样周期</strong><small>动态指标采集与上传周期 · 默认 10 秒</small></div><select data-metric-interval aria-label="采样周期">${[5,10,30,60].map(n=>`<option value="${n}" ${n===metricInterval?'selected':''}>${n} 秒</option>`).join('')}</select></div><div class="preview-controls"><span>示例设备能力</span><label>GPU <select data-preview="gpu" aria-label="GPU 能力预览"><option value="unavailable" ${!gpuAvailable?'selected':''}>不可用</option><option value="available" ${gpuAvailable?'selected':''}>可用</option></select></label></div><div class="tabs" role="tablist">${availableCategories().map(t=>`<button role="tab" aria-selected="${t===tab}" class="${t===tab?'active':''}" data-tab="${t}">${t}</button>`).join('')}</div><div class="metric-content"><div class="table-head"><span>指标 / 对象</span><span>示例值</span><span>上传</span></div>${rows}<p class="hint">必选项无需设置；可选项启用后自动采集。IP 地址包含公网查询；示例数据不代表已连接 Agent。</p></div>`,'span12')}</div>`
}

function server(){return title('CONNECTION','Server 连接','连接 Server 并测试上传。')+`<div class="grid">${card('连接设置',`<div class="field"><label for="url">Server 地址</label><input id="url" type="url" value="https://monitor.example.local" placeholder="https://"></div><div class="field"><label for="pair">设备配对码</label><input id="pair" placeholder="输入 Server 提供的配对码" autocomplete="off"></div><div class="actions">${button('配对设备','mock',true)}${button('测试连接','mock')}</div>`,'span7')}${card('连接状态',row('Server','示例地址','<span class="pill">已连接</span>')+row('设备','Studio Mac · 示例','<span class="pill">已配对</span>')+row('上传测试','最近一次成功 · 示例','<span class="pill">通过</span>')+`<div class="section-note">连接异常时，请检查地址和网络。</div>`,'span5')}</div>`}
function companion(){
const wall=Array.from({length:371},(_,i)=>`<i class="level-${(i*17+Math.floor(i/7)*13)%11<5?0:((i*7+Math.floor(i/9)*3)%4+1)}"></i>`).join('');
const github=githubLoggedIn?`<div class="wall-top"><div><strong>示例用户</strong><small>最近一年贡献 · 今天 3 次</small></div><span class="pill">已登录</span></div><div class="wall-months">1月　　　　 3月　　　　 5月　　　　 7月　　　　 9月　　　　 11月</div><div class="contribution-wall" aria-label="示例 GitHub 贡献墙">${wall}</div><div class="wall-legend">较少 <i class="level-0"></i><i class="level-1"></i><i class="level-2"></i><i class="level-3"></i><i class="level-4"></i> 较多 <span>示例数据</span></div><button class="text-action" data-action="github-logout">退出示例登录</button>`:`<div class="source-empty"><div class="source-icon"><i class="pixelart-icons-font-github" aria-hidden="true"></i></div><strong>连接 GitHub</strong><p>登录后，已配对设备可显示贡献墙。</p>${button('登录 GitHub','github-login',true)}</div>`;
const codex=codexConnected?`<div class="quota"><div><span>5 小时额度 <small>剩余</small></span><strong>72%</strong></div><div class="quota-bar"><i style="width:72%"></i></div><small>示例 · 2 小时后重置</small></div><div class="quota"><div><span>周额度 <small>剩余</small></span><strong>48%</strong></div><div class="quota-bar"><i style="width:48%"></i></div><small>示例 · 4 天后重置</small></div><div class="task-status"><span class="task-dot"></span><div><small>当前任务</small><strong>运行中 · 1 个任务</strong></div></div>`:`<div class="source-empty compact"><div class="source-icon"><i class="pixelart-icons-font-circle" aria-hidden="true"></i></div><strong>未连接</strong><p>尚未获取 Codex 桌面端信息。</p></div>`;
let music;if(musicState==='unavailable'||musicState==='empty'){music=`<div class="source-empty compact"><div class="source-icon"><i class="pixelart-icons-font-music" aria-hidden="true"></i></div><strong>${musicState==='unavailable'?'未获取':'未播放'}</strong><p>${musicState==='unavailable'?'尚未获取系统播放信息。':'当前没有歌曲信息。'}</p></div>`}else{let [song,artist,source]=tracks[trackIndex];music=`<div class="music-meta"><div><strong data-roll="song">${song}</strong><small data-roll="artist">${artist}</small></div><span class="pill ${musicState==='paused'?'gray':''}" data-roll="music-status">${musicState==='paused'?'暂停':'播放中'}</span></div><div class="music-source">${source} · 示例</div><div class="music-progress"><i></i></div><div class="music-times"><span>1:24</span><span>3:48</span></div><div class="music-controls"><button data-action="previous" aria-label="上一曲"><i class="pixelart-icons-font-arrow-left"></i></button><button class="play-button" data-action="play-pause" aria-label="${musicState==='playing'?'暂停':'播放'}"><i class="${musicState==='playing'?'pixelart-icons-font-pause':'pixelart-icons-font-play'}"></i></button><button data-action="next" aria-label="下一曲"><i class="pixelart-icons-font-arrow-right"></i></button></div>`}
return title('COMPANION','上位机','管理数据源和 ESP32 设备。')+`<div class="preview-controls"><span>预览状态</span><label>Codex <select data-preview="codex" aria-label="Codex 预览状态"><option value="disconnected" ${!codexConnected?'selected':''}>未连接</option><option value="connected" ${codexConnected?'selected':''}>已获取</option></select></label><label>音乐 <select data-preview="music" aria-label="音乐预览状态"><option value="unavailable" ${musicState==='unavailable'?'selected':''}>未获取</option><option value="empty" ${musicState==='empty'?'selected':''}>未播放</option><option value="paused" ${musicState==='paused'?'selected':''}>暂停</option><option value="playing" ${musicState==='playing'?'selected':''}>播放中</option></select></label></div><div class="grid activity-layout">${card('GitHub 贡献墙',github,'span12','<i class="pixelart-icons-font-github source-heading-icon" aria-hidden="true"></i>')}${card('Codex',codex,'span6','<span class="pill '+(codexConnected?'':'gray')+'">'+(codexConnected?'已连接':'未连接')+'</span>')}${card('系统当前播放',music,'span6','<i class="pixelart-icons-font-music source-heading-icon" aria-hidden="true"></i>')}</div>${deviceSection()}`}

function deviceSection(){return `<div class="device-section"><div class="section-heading"><div><div class="eyebrow">DEVICE MANAGEMENT</div><h2>ESP32 设备</h2><p class="muted">管理已配对的设备。</p></div>${button('发现设备','mock',true)}</div><div class="grid">${card('已配对设备',`<div class="device"><div class="device-icon" aria-hidden="true"><i class="pixelart-icons-font-cpu"></i></div><div class="device-info"><h3>Studio Monitor　<span class="pill">在线</span></h3><p>局域网 · 192.168.1.88<br>上位机模式 · 示例设备<br>断开时自动回退独立模式</p></div></div><div class="row"><strong>连接方式</strong><span class="muted">局域网</span></div><div class="row"><strong>屏幕刷新档位</strong><select aria-label="屏幕刷新档位"><option>标准</option><option>流畅</option></select></div><div class="actions">${button('设备自检','mock')}${button('管理配对','mock')}</div>`,'span7')}${card('添加设备',`<p class="muted">通过局域网或串口连接设备。</p><div class="field"><label for="address">手动输入设备地址</label><input id="address" placeholder="例如 192.168.1.88"></div><div class="actions">${button('查找地址','mock')}${button('选择串口','mock')}</div><div class="section-note" style="margin-top:16px">首次配对请使用可信网络，并在设备和电脑上确认短码。</div>`,'span5')}</div></div>`}

function startup(){return title('LIFECYCLE','启动与后台','选择应用如何运行。关闭窗口后，后台可以继续工作。')+`<div class="grid">${card('运行方式',row('登录后启动','登录电脑后启动，支持音乐与 Codex',toggle(false,'登录后启动'))+row('系统启动采集','未登录时采集指标；可能需要管理员权限',toggle(false,'系统启动采集'))+row('后台运行','关闭窗口后继续采集和服务设备',toggle(true,'后台运行'))+`<div class="section-note">更改系统采集服务可能需要管理员权限。</div>`,'span7')}${card('后台状态',row('Agent','示例 · 正常运行','<span class="pill">运行中</span>')+row('采集服务','示例 · 用户会话','<span class="pill">运行中</span>')+`<div class="actions">${button('暂停采集','mock')}${button('退出后台','mock')}</div><p class="hint">退出后台会停止采集，并中断 ESP32 的上位机连接。</p>`,'span5')}</div>`}
function diagnostics(){return title('SUPPORT','诊断','查看状态、排查问题并管理诊断信息。')+`<div class="grid">${card('系统状态',row('配置应用状态','示例 · 配置版本 12','<span class="pill">已应用</span>')+row('最近成功上传','示例 · 今天 10:24:38','<span class="pill">成功</span>')+row('程序版本','示例 · 版本信息','<span class="pill">正常</span>')+row('权限与依赖','示例 · 部分传感器不可用','<span class="pill amber">需关注</span>'),'span7')}${card('诊断工具',`<p class="muted">导出前可预览和检查诊断信息。</p><div class="actions" style="margin-top:16px">${button('预览诊断包','mock',true)}${button('重试连接','mock')}</div>`,'span5')}${card('最近事件',row('采集配置已应用','今天 10:22 · 示例','<span class="pill">成功</span>')+row('Server 上传成功','今天 10:24 · 示例','<span class="pill">成功</span>')+row('传感器能力受限','今天 10:18 · 示例','<span class="pill amber">需关注</span>'),'span12')}</div>`}
const views={overview,metrics,server,companion,startup,diagnostics};
function updateMetricTab(){const template=document.createElement('template');template.innerHTML=metrics();const content=template.content.querySelector('.metric-content');content.classList.add('metric-slide-in');app.querySelector('.metric-content').replaceWith(content);app.querySelectorAll('[data-tab]').forEach(button=>{const selected=button.dataset.tab===tab;button.classList.toggle('active',selected);button.setAttribute('aria-selected',String(selected))})}
function render(transition=''){const oldValues=lastRenderedPage===page?new Map([...app.querySelectorAll('[data-roll]')].map(el=>[el.dataset.roll,el.querySelector('.roll-current')?.textContent ?? el.textContent])):new Map();const navScroll=app.querySelector('.nav')?.scrollLeft ?? 0;app.innerHTML=`<div class="shell"><aside class="sidebar"><div class="brand"><div class="logo"><i class="pixelart-icons-font-cpu" aria-hidden="true"></i></div><div><strong>ESP32 MONITOR</strong><small>DESKTOP CLIENT</small></div></div><div class="nav-label">工作空间</div><nav class="nav" aria-label="主导航">${pages.map(([id,icon,name])=>`<button class="${page===id?'active':''}" ${page===id?'aria-current="page"':''} data-page="${id}"><span class="nav-icon"><i class="${icon}" aria-hidden="true"></i></span>${name}</button>`).join('')}</nav><div class="sidefoot">ESP32 MONITOR · V1<br>CONTROL PANEL / 本地控制台</div></aside><main class="main"><header class="topbar"><div class="crumb">工作空间　/　${pages.find(x=>x[0]===page)[2]}</div><div class="topright"><span class="demo">■ 界面预览 · 示例数据</span><span>Studio Mac</span></div></header><div class="content ${transition==='page'?'page-enter':''}">${views[page]()}</div></main></div>`;app.querySelector('.nav').scrollLeft=navScroll;app.querySelectorAll('.field input').forEach(control=>{const frame=document.createElement('span');frame.className='pixel-input-frame';control.before(frame);frame.append(control)});app.querySelectorAll('select').forEach(makePixelSelect);setupCardReveal();app.querySelectorAll('[data-roll]').forEach(el=>{const previous=oldValues.get(el.dataset.roll);if(previous!==undefined&&previous!==el.textContent)rollText(el,previous,el.textContent)});lastRenderedPage=page}
function toast(message){document.querySelector('.toast')?.remove();let el=document.createElement('div');el.className='toast';el.setAttribute('role','status');el.textContent=message;document.body.append(el);setTimeout(()=>el.remove(),3000)}
app.addEventListener('change',e=>{if(e.target.matches('[data-metric]')) {const {metric,object}=e.target.dataset;metricEnabled[object?`${metric}:${object}`:metric]=e.target.checked;if(metric==='battery.level')metricEnabled['battery.charging']=e.target.checked;if(object&&!e.target.checked)for(const [child,parent] of Object.entries(metricParents))if(parent===metric)metricEnabled[`${child}:${object}`]=false;if(object&&(Object.values(metricParents).includes(metric)||metricParents[metric]))render();} if(e.target.matches('[data-metric-interval]'))metricInterval=Number(e.target.value); if(e.target.dataset.preview==='gpu'){gpuAvailable=e.target.value==='available';if(!gpuAvailable&&tab==='GPU')tab='CPU';render()} if(e.target.dataset.preview==='codex'){codexConnected=e.target.value==='connected';render()} if(e.target.dataset.preview==='music'){musicState=e.target.value;render()}});
app.addEventListener('click',e=>{let target=e.target.closest('button');if(!target)return;if(target.dataset.page){if(page!==target.dataset.page){page=target.dataset.page;render('page')}return}if(target.dataset.tab){if(tab!==target.dataset.tab){tab=target.dataset.tab;updateMetricTab()}return}let action=target.dataset.action;if(action==='github-login'||action==='github-logout'){githubLoggedIn=action==='github-login';render()}else if(action==='play-pause'){musicState=musicState==='playing'?'paused':'playing';render()}else if(action==='previous'||action==='next'){trackIndex=(trackIndex+(action==='next'?1:-1)+tracks.length)%tracks.length;musicState='playing';render()}else if(action?.startsWith('go:')){page=action.slice(3);render('page')}else if(action==='simulate'){overviewSample=(overviewSample+1)%sampleValues.length;['cpu','memory','disk'].forEach((key,i)=>{const el=app.querySelector(`[data-roll="${key}"]`);rollText(el,el.textContent,sampleValues[overviewSample][i])})}else if(action==='setup'){toast('配置步骤：连接 Server → 选择指标 → 连接设备 → 设置启动方式。')}else if(action==='rescan-metrics'){render();toast('已刷新示例设备能力；固定信息在 Agent 重启后更新。')}else if(action==='apply'){toast('预览模式，设置不会保存。')}else if(action==='mock'){toast('预览模式，无法执行此操作。')}});
function makePixelSelect(select) {
  const container=document.createElement('div');
  container.className='pixel-select';
  const button=document.createElement('button');
  button.type='button'; button.className='pixel-select-trigger';
  button.setAttribute('aria-haspopup','listbox'); button.setAttribute('aria-expanded','false');
  button.setAttribute('aria-label',select.getAttribute('aria-label') || select.closest('label')?.textContent.trim() || '选择');
  const label=document.createElement('span'); label.className='pixel-select-label'; label.textContent=select.selectedOptions[0]?.textContent || '';
  const arrow=document.createElement('i'); arrow.className='pixelart-icons-font-chevron-down'; arrow.setAttribute('aria-hidden','true');
  button.append(label,arrow);
  const menu=document.createElement('div'); menu.className='pixel-select-menu'; menu.setAttribute('role','listbox'); menu.hidden=true;
  menu.setAttribute('aria-label',button.getAttribute('aria-label'));
  const options=Array.from(select.options).map((option,index)=>{
    const item=document.createElement('button'); item.type='button'; item.className='pixel-select-option'; item.setAttribute('role','option');
    item.setAttribute('aria-selected',String(option.selected)); item.textContent=option.textContent; item.tabIndex=-1;
    item.addEventListener('click',event=>{event.stopPropagation();select.selectedIndex=index;rollText(label,label.textContent,option.textContent);options.forEach((entry,i)=>entry.setAttribute('aria-selected',String(i===index)));close();button.focus();select.dispatchEvent(new Event('change',{bubbles:true}))});
    item.addEventListener('keydown',event=>{if(event.key==='Escape'){event.preventDefault();close();button.focus()}else if(event.key==='ArrowDown'||event.key==='ArrowUp'){event.preventDefault();options[(index+(event.key==='ArrowDown'?1:-1)+options.length)%options.length].focus()}else if(event.key==='Home'||event.key==='End'){event.preventDefault();options[event.key==='Home'?0:options.length-1].focus()}});
    menu.append(item);return item;
  });
  function close(){menu.hidden=true;button.setAttribute('aria-expanded','false');container.classList.remove('open')}
  button.addEventListener('click',event=>{event.stopPropagation();const opening=menu.hidden;document.querySelectorAll('.pixel-select.open').forEach(el=>{el.classList.remove('open');el.querySelector('.pixel-select-menu').hidden=true;el.querySelector('.pixel-select-trigger').setAttribute('aria-expanded','false')});if(opening){menu.hidden=false;button.setAttribute('aria-expanded','true');container.classList.add('open')}});
  button.addEventListener('keydown',event=>{if(event.key==='ArrowDown'||event.key==='ArrowUp'){event.preventDefault();if(menu.hidden)button.click();options[select.selectedIndex<0?0:select.selectedIndex]?.focus()}else if(event.key==='Escape'){close()}});
  container.addEventListener('focusout',event=>{if(!container.contains(event.relatedTarget))close()});
  select.before(container); select.hidden=true; container.append(select,button,menu);
}
document.addEventListener('click',event=>{if(!event.target.closest('.pixel-select'))document.querySelectorAll('.pixel-select.open .pixel-select-trigger').forEach(button=>button.click())});
function rollText(element,previous,next){
  if(element?.querySelector('.roll-current'))previous=element.querySelector('.roll-current').textContent;
  if(!element || previous===next){if(element)element.textContent=next;return}
  if(window.matchMedia?.('(prefers-reduced-motion: reduce)').matches){element.textContent=next;return}
  element.textContent='';
  const old=document.createElement('span');old.className='roll-old';old.textContent=previous;old.setAttribute('aria-hidden','true');
  const current=document.createElement('span');current.className='roll-current';current.textContent=next;
  element.classList.add('text-rolling');element.append(old,current);
  current.addEventListener('animationend',()=>{if(current.parentElement===element){element.textContent=next;element.classList.remove('text-rolling')}},{once:true});
}
let cardObserver;
function setupCardReveal(){
  cardObserver?.disconnect();
  if(!('IntersectionObserver' in window) || window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return;
  const cards=app.querySelectorAll('.card');
  cardObserver=new IntersectionObserver(entries=>{
    for(const entry of entries){
      if(entry.isIntersecting){entry.target.classList.remove('reveal-pending');entry.target.classList.add('reveal-visible')}else{entry.target.classList.remove('reveal-visible');entry.target.classList.add('reveal-pending')}
    }
  },{threshold:0,rootMargin:'0px 0px -24px 0px'});
  cards.forEach(card=>{card.classList.add('reveal-pending');cardObserver.observe(card)});
}
render();

// On compact layouts, reveal navigation when the user scrolls upward.
let lastScrollY = window.scrollY;
window.addEventListener('scroll', () => {
  const sidebar = app.querySelector('.sidebar');
  const current = window.scrollY;
  if (!sidebar) return;
  if (window.matchMedia('(max-width: 650px)').matches) {
    const delta = current - lastScrollY;
    if (current <= 10 || delta < -2) sidebar.classList.remove('nav-hidden');
    else if (delta > 2 && !sidebar.contains(document.activeElement)) sidebar.classList.add('nav-hidden');
  } else sidebar.classList.remove('nav-hidden');
  lastScrollY = current;
}, { passive: true });
