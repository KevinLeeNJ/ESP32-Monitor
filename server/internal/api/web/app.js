'use strict';
const $ = (s) => document.querySelector(s);
const content = $('#content');
let csrf = '', authenticated = false, forced = false, page = 'devices', selected = '', devices = [], timer, generation = 0;
const messages = {
    authentication_required: '登录已过期，请重新登录。', invalid_password: '密码不正确。',
    password_requires_12_characters: '新密码至少 12 个字符，不能包含控制字符。', new_password_must_differ: '新密码必须与当前密码不同。',
    csrf_rejected: '页面验证已过期，请刷新后重试。', origin_rejected: '访问地址与服务器配置不一致。',
    rate_limited: '操作过于频繁，请稍后重试。', password_change_required: '请先修改临时密码。',
    invalid_name: '请输入 1–64 个字符的设备名称。', panel_full: '面板最多展示 4 台设备，请先取消其他设备。',
    invalid_display_name: '面板名称需要 1–8 个 ASCII 字符，不含空格。', unknown_disk: '该磁盘已不在当前设备档案中。'
};
function el(tag, attrs = {}, children = []) {
    const e = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs)) {
        if (k === 'text')
            e.textContent = v;
        else if (k.startsWith('on'))
            e.addEventListener(k.slice(2), v);
        else if (k === 'class')
            e.className = v;
        else
            e.setAttribute(k, String(v));
    }
    for (const child of [].concat(children))
        if (child != null)
            e.append(typeof child === 'string' ? document.createTextNode(child) : child);
    return e;
}
const button = (label, fn, primary = false, attrs = {}) => el('button', { type: 'button', class: 'btn' + (primary ? ' primary' : ''), text: label, onclick: fn, ...attrs });
const card = (title, children, span = 'span12') => el('section', { class: 'card ' + span }, [el('div', { class: 'card-title' }, el('h2', { text: title })), ...children]);
function notice(text) { const n = $('#notice'); n.textContent = text; n.hidden = false; clearTimeout(notice.timer); let seconds = 6; const tick = () => { n.textContent = text + ' · ' + seconds + 's'; if (seconds-- === 0)
    n.hidden = true;
else
    notice.timer = setTimeout(tick, 1000); }; tick(); }
async function api(path, method = 'GET', body) {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10000);
    try {
        const r = await fetch(path, { method, credentials: 'same-origin', cache: 'no-store', signal: controller.signal, headers: { ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...(method !== 'GET' && csrf ? { 'X-CSRF-Token': csrf } : {}) }, body: body === undefined ? undefined : JSON.stringify(body) });
        const data = r.status === 204 ? {} : await r.json();
        if (!r.ok) {
            if (data.error === 'authentication_required') {
                authenticated = false;
                csrf = '';
                renderLogin();
            }
            ;
            const e = new Error(messages[data.error] || '请求失败，请重试。');
            e.status = r.status;
            e.retry = r.headers.get('Retry-After');
            throw e;
        }
        ;
        return data;
    }
    finally {
        clearTimeout(timeout);
    }
}
function field(label, type, name, autocomplete = '', value = '') { const input = el('input', { type, name, id: name, autocomplete, value }); const frame = el('span', { class: 'pixel-input-frame' }, input); return [el('div', { class: 'field' }, [el('label', { for: name, text: label }), frame]), input]; }
function heading(title, desc, eye) { return el('div', { class: 'heading' }, el('div', {}, [el('div', { class: 'eyebrow', text: eye }), el('h1', { text: title }), el('p', { text: desc })])); }
function inlineError(form, text) { let error = form.querySelector('.form-error'); if (!error) {
    error = el('p', { class: 'form-error', role: 'alert' });
    form.append(error);
} error.textContent = text; }
function busyForm(form, fn) { form.addEventListener('submit', async (e) => { e.preventDefault(); const submit = form.querySelector('[type=submit]'); submit.disabled = true; form.querySelector('.form-error')?.remove(); try {
    await fn();
}
catch (err) {
    inlineError(form, err.message + (err.retry ? ' 等待 ' + err.retry + ' 秒。' : ''));
}
finally {
    submit.disabled = false;
} }); }
function shell(signedIn, title) { $('#shell').classList.toggle('signed-out', !signedIn); $('#sidebar').hidden = !signedIn; $('#crumb').textContent = '工作空间　/　' + title; document.querySelectorAll('[data-page]').forEach(b => { b.classList.toggle('active', b.dataset.page === page); if (b.dataset.page === page)
    b.setAttribute('aria-current', 'page');
else
    b.removeAttribute('aria-current'); }); }
function renderLogin() {
    clearTimeout(timer);
    generation++;
    shell(false, '登录');
    content.replaceChildren();
    const [f, password] = field('WebUI 密码', 'password', 'login-password', 'current-password');
    password.required = true;
    password.maxLength = 256;
    const form = el('form', {}, [el('div', { class: 'eyebrow', text: 'SERVER CONSOLE' }), el('h1', { text: '登录控制台' }), el('p', { class: 'muted', text: '首次启动的临时密码显示在 Server 终端中。' }), f, el('button', { type: 'submit', class: 'btn primary', text: '登录' })]);
    busyForm(form, async () => { const state = await api('/api/auth/login', 'POST', { password: password.value }); password.value = ''; acceptState(state); render(); });
    content.append(el('section', { class: 'card auth-card' }, form));
    password.focus();
}
function acceptState(state) { authenticated = true; csrf = state.csrf_token; forced = state.must_change_password; if (forced)
    page = 'password'; }
function passwordPage() {
    shell(!forced, '修改密码');
    content.replaceChildren();
    const [oldField, old] = field(forced ? '临时密码' : '当前密码', 'password', 'current-password', 'current-password');
    old.required = true;
    old.maxLength = 256;
    const [newField, next] = field('新密码', 'password', 'new-password', 'new-password');
    next.required = true;
    next.minLength = 12;
    next.maxLength = 256;
    const [repeatField, repeat] = field('确认新密码', 'password', 'repeat-password', 'new-password');
    repeat.required = true;
    const form = el('form', {}, [heading(forced ? '设置你的登录密码' : '修改登录密码', forced ? '首次登录，请先修改临时密码，再进入控制台。' : '保存后，其他浏览器的登录会话会失效。', 'ACCOUNT SECURITY'), oldField, newField, el('p', { class: 'hint', text: '至少 12 个字符，建议使用密码管理器生成的随机密码。' }), repeatField, el('button', { class: 'btn primary', type: 'submit', text: '保存新密码' })]);
    busyForm(form, async () => { if (next.value !== repeat.value)
        throw new Error('两次输入的新密码不一致。'); const state = await api('/api/auth/password', 'POST', { current_password: old.value, new_password: next.value }); old.value = next.value = repeat.value = ''; acceptState(state); page = 'devices'; notice('密码已更新'); render(); });
    content.append(el('section', { class: 'card auth-card' }, form));
}
async function confirmAction(title, text) { const d = $('#confirm'); $('#confirm-title').textContent = title; $('#confirm-text').textContent = text; d.returnValue = 'cancel'; d.showModal(); return new Promise(resolve => d.addEventListener('close', () => resolve(d.returnValue === 'ok'), { once: true })); }
function tokenPage() {
    shell(true, 'Read Token');
    content.replaceChildren(heading('Read Token', '用于 ESP32 读取设备摘要，请妥善保管。', 'PANEL ACCESS'));
    const [tokenField, token] = field('面板只读 Token', 'password', 'read-token', 'off');
    token.readOnly = true;
    token.placeholder = '点击显示以读取 Token';
    const toggle = button('显示 Token', async () => {
        if (token.type === 'text') { token.type = 'password'; toggle.textContent = '显示 Token'; return; }
        try {
            if (!token.value) token.value = (await api('/api/read-token', 'POST', {})).read_token;
            token.type = 'text'; toggle.textContent = '隐藏 Token';
        } catch (e) { notice(e.message); }
    });
    const copy = button('复制 Token', async () => {
        try {
            if (!token.value) token.value = (await api('/api/read-token', 'POST', {})).read_token;
        } catch (e) { notice(e.message); return; }
        try {
            if (!navigator.clipboard) throw new Error('clipboard unavailable');
            await navigator.clipboard.writeText(token.value); notice('Token 已复制');
        } catch {
            token.type = 'text'; toggle.textContent = '隐藏 Token'; token.focus(); token.select();
            notice('请手动复制已选中的 Token');
        }
    });
    content.append(card('面板访问凭据', [tokenField, el('div', { class: 'actions' }, [toggle, copy]), el('p', { class: 'hint', text: '摘要接口：/v2/devices/summary。此 Token 不用于 WebUI 登录。' })]));
}
function date(ts) { return ts ? new Date(ts * 1000).toLocaleString('zh-CN') : '—'; }
function remaining(ts) { return Math.max(0, Math.ceil((ts * 1000 - Date.now()) / 60000)) + ' 分钟内有效'; }
function statusLabel(d) { return { online: '在线', offline: '离线', pending: '待注册', expired: '注册码过期' }[d.status] || '未知'; }
function renderDevices() {
    shell(true, '设备注册');
    content.replaceChildren(heading('设备注册', '创建设备后，在 Client 的「Server 连接」页面输入服务器地址和验证码。', 'DEVICE REGISTRATION'));
    const [nameField, name] = field('设备名称', 'text', 'device-name', 'off');
    name.required = true;
    name.maxLength = 64;
    name.placeholder = '例如 My Mac';
    const form = el('form', {}, [nameField, el('button', { class: 'btn primary', type: 'submit', text: '添加设备并生成验证码' })]);
    busyForm(form, async () => { await api('/api/devices', 'POST', { name: name.value }); name.value = ''; await refreshDevices(); notice('设备已创建，验证码 10 分钟内有效'); });
    content.append(card('添加设备', [form, el('p', { class: 'hint', text: '验证码单次使用。重新注册后，设备的旧上传凭据立即失效。' })]), el('div', { id: 'device-list', class: 'device-list' }));
    drawDevices();
}
function captureFocus(container) {
    const active = document.activeElement;
    if (!active || !container.contains(active))
        return null;
    return { device: active.closest('[data-device-id]')?.dataset.deviceId || '', action: active.dataset.action || '' };
}
function restoreFocus(container, focus) {
    if (!focus)
        return;
    const card = focus.device ? container.querySelector('[data-device-id="' + focus.device + '"]') : container;
    const target = focus.action && card ? card.querySelector('[data-action="' + focus.action + '"]') : null;
    (target || container.querySelector('button') || container).focus();
}
function drawDevices() {
    const list = $('#device-list');
    if (!list)
        return;
    list.tabIndex = -1;
    const focus = captureFocus(list);
    list.replaceChildren();
    if (!devices.length) {
        list.append(card('设备列表', [el('p', { class: 'empty', text: '还没有设备。添加第一台设备，开始接收指标。' })]));
        restoreFocus(list, focus);
        return;
    }
    devices.forEach(d => {
        const actions = el('div', { class: 'actions' }, [button('查看指标', () => { selected = d.id; page = 'metrics'; render(); }, false, { 'data-action': 'metrics' }), button(d.code ? '重新生成验证码' : '生成注册码', async () => { try {
                if (d.code && !await confirmAction('重新生成验证码', '现有验证码将失效。已注册设备的上传凭据会在再次注册成功后轮换。'))
                    return;
                await api('/api/devices/' + d.id + '/code', 'POST', {});
                await refreshDevices();
                notice('已生成新验证码');
            }
            catch (e) {
                notice(e.message);
            } }, false, { 'data-action': 'code' }), button('删除设备', async () => { if (!await confirmAction('删除设备 ' + d.name, '设备、历史指标和注册凭据将一并删除，Client 需要重新注册。'))
                return; try {
                await api('/api/devices/' + d.id, 'DELETE');
                await refreshDevices();
                notice('设备已删除');
            }
            catch (e) {
                notice(e.message);
            } }, false, { 'data-action': 'delete' })]);
        const children = [el('div', { class: 'card-title' }, [el('h2', { text: d.name }), el('span', { class: 'pill ' + (d.status === 'online' ? '' : 'gray'), text: statusLabel(d) })]), el('div', { class: 'device-meta', text: '设备 ' + d.id + ' · 最近采样 ' + date(d.last_seen_at) })];
        if (d.code)
            children.push(el('div', { class: 'code-box' }, [el('strong', { class: 'code-value', text: d.code }), el('small', { class: 'code-note', text: remaining(d.code_expires_at) }), button('复制验证码', async () => { try {
                    await navigator.clipboard.writeText(d.code);
                    notice('验证码已复制');
                }
                catch {
                    notice('请手动选择并复制验证码');
                } }, false, { 'data-action': 'copy' })]));
        children.push(actions);
        list.append(el('section', { class: 'card', 'data-device-id': d.id }, children));
    });
    restoreFocus(list, focus);
}
async function refreshDevices() { const data = await api('/api/devices'); devices = data.devices; if (page === 'devices')
    drawDevices(); if (page === 'metrics')
    drawSelector(); }
function renderMetrics() { shell(true, '指标查看'); content.replaceChildren(heading('指标查看', '查看 Client 已上传的档案和指标。缺失或过期的值显示为 —。', 'DEVICE METRICS'), el('div', { id: 'device-selector', class: 'device-selector' }), el('div', { id: 'metric-detail' })); drawSelector(); }
function drawSelector() {
    const list = $('#device-selector');
    if (!list)
        return;
    list.tabIndex = -1;
    const active = document.activeElement;
    const focus = active && list.contains(active) ? active.dataset.deviceId || '' : '';
    list.replaceChildren();
    if (!devices.length) {
        $('#metric-detail').replaceChildren(card('设备指标', [el('p', { class: 'empty', text: '暂无设备，请先在设备注册页添加设备。' })]));
        if (focus)
            list.focus();
        return;
    }
    if (!devices.some(d => d.id === selected))
        selected = devices[0].id;
    for (const d of devices) {
        const b = button(d.name, () => { selected = d.id; render(); });
        b.dataset.deviceId = d.id;
        b.setAttribute('aria-pressed', String(d.id === selected));
        list.append(b);
    }
    if (focus)
        (list.querySelector('[data-device-id="' + focus + '"]') || list).focus();
}
const labels = { 'cpu.usage': 'CPU 使用率', 'memory.usage': '内存使用率', 'system.uptime': '运行时间', 'cpu.load1': '1 分钟负载', 'cpu.load5': '5 分钟负载', 'cpu.load15': '15 分钟负载', 'cpu.temperature': 'CPU 温度', 'memory.used': '内存已用', 'memory.app': 'App 内存', 'memory.wired': '有线内存', 'memory.compressed': '压缩内存', 'memory.swap_total': 'Swap 总量', 'memory.swap_used': 'Swap 已用', 'memory.pressure': '内存压力', 'core.usage': '逻辑核使用率', 'gpu.usage': 'GPU 使用率', 'gpu.temperature': 'GPU 温度', 'drive.used': '磁盘已用', 'drive.read_rate': '读取速率', 'drive.write_rate': '写入速率', 'drive.smart_health': 'SMART 健康状态', 'interface.up': '网络连接', 'interface.upload_rate': '上传速率', 'interface.download_rate': '下载速率', 'interface.ipv4': 'IPv4 地址', 'network.public_ipv4': '公网 IPv4', 'battery.level': '电量', 'battery.charging': '正在充电', 'fan.rpm': '风扇转速' };
function bytes(v) { let n = BigInt(v); const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB']; let divisor = 1n, i = 0; while (n >= divisor * 1024n && i < units.length - 1) {
    divisor *= 1024n;
    i++;
} return Number(n * 100n / divisor) / 100 + ' ' + units[i]; }
function format(key, v) { if (v === undefined)
    return '—'; const metric = key.split('/')[0]; if (typeof v === 'boolean')
    return v ? (metric === 'interface.up' ? '已连接' : metric === 'drive.smart_health' ? '正常' : '是') : (metric === 'interface.up' ? '未连接' : metric === 'drive.smart_health' ? '故障' : '否'); if (Array.isArray(v))
    return v.length ? v.join(', ') : '无地址'; if (/\.usage$|battery.level/.test(metric))
    return (v / 100).toFixed(2) + '%'; if (metric.includes('temperature'))
    return (v / 10).toFixed(1) + ' °C'; if (metric.startsWith('cpu.load'))
    return (v / 1000).toFixed(3); if (metric === 'system.uptime')
    return Math.floor(v / 86400) + '天 ' + Math.floor(v % 86400 / 3600) + '小时'; if (metric === 'memory.pressure')
    return ['正常', '警告', '严重'][v] || '—'; if (/memory\.(used|app|wired|compressed|swap_)|drive\.used|_rate$/.test(metric))
    return bytes(v) + (metric.endsWith('_rate') ? '/s' : ''); return String(v); }
function objectInfo(inv,kind,id){return inv.objects?.find(item=>item.kind===kind&&item.id===Number(id));}
function objectTitle(inv,kind,id){
 const info=objectInfo(inv,kind,id);
 if(info?.name)return info.name;
 if(info?.model)return info.model;
 if(kind==='drive')return inv.drives?.find(item=>item.id===Number(id))?.model||'磁盘（名称未上传）';
 if(kind==='interface'){const medium=inv.interfaces?.find(item=>item.id===Number(id))?.medium;return {wifi:'Wi-Fi 接口',ethernet:'以太网接口',other:'网络接口'}[medium]||'网络接口（名称未上传）'}
 return {gpu:'GPU（名称未上传）',battery:'电池（名称未上传）',fan:'风扇（名称未上传）'}[kind]||'对象（名称未上传）';
}
function metricRows(data) {
    const inv = data.profile?.inventory || {};
    const groups = new Map();
    const ensure = (key, title, kind = '', id = '') => {
        if (!groups.has(key)) groups.set(key, { title, kind, id, fixed: [], dynamic: [] });
        return groups.get(key);
    };
    const fixed = (group, label, value) => {
        if (value !== undefined && value !== '') group.fixed.push(el('div', { class: 'row fixed-metric' }, [
            el('div', {}, [el('strong', { text: label }), el('small', { text: '设备档案' })]),
            el('div', { class: 'value', text: String(value) })
        ]));
    };
    const cpu = ensure('cpu', 'CPU');
    for (const [key, label] of Object.entries({ model: '型号', physical_cores: '物理核心', logical_cores: '逻辑核心', performance_cores: '性能核心', efficiency_cores: '能效核心' })) fixed(cpu, label, inv.cpu?.[key]);
    const memory = ensure('memory', '内存');
    fixed(memory, '总量', inv.memory_total !== undefined ? bytes(inv.memory_total) : undefined);
    const system = ensure('system', '系统');
    fixed(system, '操作系统', inv.os ? [inv.os.name, inv.os.version, inv.os.build].filter(Boolean).join(' ') : undefined);
    fixed(system, '产品名称', inv.product);
    const hardware = (kind,id) => ensure(kind+'/'+id,objectTitle(inv,kind,id),kind,id);
    for(const info of inv.objects||[]){
        if(['drive','interface','gpu','battery','fan'].includes(info.kind)){
            const group=hardware(info.kind,info.id);
            fixed(group,'名称',info.name);
            fixed(group,'型号',info.model);
        }
    }
    for (const drive of inv.drives || []) {
        const group = hardware('drive', drive.id);
        if(!objectInfo(inv,'drive',drive.id)?.model)fixed(group, '型号', drive.model);
        fixed(group, '总容量', drive.total !== undefined ? bytes(drive.total) : undefined);
    }
    for (const nic of inv.interfaces || []) {
        const group = hardware('interface', nic.id);
        fixed(group, '连接介质', { wifi: 'Wi-Fi', ethernet: '以太网', other: '其他' }[nic.medium]);
    }
    for (const gpu of inv.gpus || []) hardware('gpu', gpu.id);
    for (const battery of inv.batteries || []) hardware('battery', battery.id);
    const keys = new Set(['cpu.usage', 'memory.usage', 'system.uptime']);
    for (const selection of data.profile?.enabled || []) {
        if (selection.objects?.length) {
            selection.objects.forEach(id => {
                if (!['drive.model', 'drive.total', 'interface.medium'].includes(selection.metric)) keys.add(selection.metric + '/' + id);
            });
        } else if (labels[selection.metric]) keys.add(selection.metric);
    }
    if (inv.memory_total !== undefined) keys.add('memory.used');
    Object.keys(data.latest).forEach(key => keys.add(key));
    for (const key of keys) {
        const [metric, id] = key.split('/');
        const kind = metric.split('.')[0];
        let group;
        let label = labels[metric] || metric;
        if (['drive', 'interface', 'gpu', 'battery', 'fan'].includes(kind)) group = hardware(kind, id);
        else if (kind === 'core') {
            group = ensure('core', '逻辑核心');
            const core = inv.cores?.find(item => item.id === Number(id));
            label = objectInfo(inv,'core',id)?.name || (core ? '逻辑核 ' + core.index + ({ performance: ' · 性能核', efficiency: ' · 能效核' }[core.kind] || '') : '逻辑核（信息未上传）');
        } else group = ensure(kind, { cpu: 'CPU', memory: '内存', system: '系统', network: '公网网络' }[kind] || '其他');
        const entry = data.latest[key];
        group.dynamic.push(el('div', { class: 'row' }, [
            el('div', {}, [el('strong', { text: label }), el('small', { text: entry ? date(entry.sampled_at) : '尚无有效观测' })]),
            el('div', { class: 'value', text: format(key, entry?.value) })
        ]));
    }
    const selectedObjects = new Set();
    for (const selection of data.profile?.enabled || []) {
        const kind = selection.metric.split('.')[0];
        if (['drive.model', 'drive.total', 'interface.medium'].includes(selection.metric)) continue;
        for (const id of selection.objects || []) selectedObjects.add(kind + '/' + id);
    }
    return [...groups.values()].filter(group => !group.kind || selectedObjects.has(group.kind + '/' + group.id)).map(group => {
        const header = el('div', { class: 'card-title' }, [el('h2', { text: group.title })]);
        if (group.kind) header.append(el('small', { text: ({ drive: '物理磁盘', interface: '网络接口', gpu: 'GPU', battery: '电池', fan: '风扇' }[group.kind]) + ' · ID ' + group.id }));
        return el('section', { class: 'card span6 hardware-card', 'data-object-kind': group.kind, 'data-object-id': group.id }, [header, ...group.fixed, ...group.dynamic]);
    });
}
function pixelSelect(id, options, initial) {
    let value = String(initial);
    if (!options.some(option => String(option.value) === value)) value = '0';
    const label = el('span', { class: 'pixel-select-label' });
    const trigger = el('button', { id, type: 'button', class: 'pixel-select-trigger', 'aria-haspopup': 'listbox', 'aria-expanded': 'false', 'aria-controls': id + '-menu' }, [label, el('span', { 'aria-hidden': 'true', text: '▾' })]);
    const menu = el('div', { id: id + '-menu', class: 'pixel-select-menu', role: 'listbox', 'aria-label': 'DISK 指标来源' });
    menu.hidden = true;
    const root = el('div', { class: 'pixel-select' }, [trigger, menu]);
    const update = () => {
        label.textContent = options.find(option => String(option.value) === value)?.label || '';
        [...menu.children].forEach((item, index) => item.setAttribute('aria-selected', String(String(options[index].value) === value)));
    };
    const close = (focus = false) => { menu.hidden = true; root.classList.remove('open', 'drop-up'); trigger.setAttribute('aria-expanded', 'false'); if (focus) trigger.focus(); };
    const open = () => {
        menu.hidden = false; root.classList.add('open'); trigger.setAttribute('aria-expanded', 'true');
        const rect = trigger.getBoundingClientRect();
        root.classList.toggle('drop-up', rect.bottom + menu.offsetHeight + 10 > innerHeight && rect.top > menu.offsetHeight + 10);
        menu.children[Math.max(0, options.findIndex(option => String(option.value) === value))].focus();
    };
    options.forEach(option => {
        const item = el('button', { type: 'button', class: 'pixel-select-option', role: 'option', text: option.label });
        item.addEventListener('click', () => { if (value !== String(option.value)) { value = String(option.value); update(); root.dispatchEvent(new Event('change', { bubbles: true })); } close(true); });
        menu.append(item);
    });
    trigger.addEventListener('click', () => menu.hidden ? open() : close(true));
    trigger.addEventListener('keydown', event => { if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); open(); } });
    menu.addEventListener('keydown', event => {
        const index = [...menu.children].indexOf(document.activeElement);
        let next;
        if (event.key === 'ArrowDown') next = (index + 1) % options.length;
        else if (event.key === 'ArrowUp') next = (index - 1 + options.length) % options.length;
        else if (event.key === 'Home') next = 0;
        else if (event.key === 'End') next = options.length - 1;
        else if (event.key === 'Escape') { event.preventDefault(); close(true); return; }
        if (next !== undefined) { event.preventDefault(); menu.children[next].focus(); }
    });
    root.addEventListener('focusout', event => { if (!root.contains(event.relatedTarget)) close(); });
    update();
    return { root, get value() { return value; } };
}
function panelForm(data) {
    const d = data.device;
    const checkbox = el('input', { type: 'checkbox', id: 'panel-enabled' });
    checkbox.checked = d.panel_enabled;
    const [nameField, name] = field('面板名称（1–8 ASCII 字符）', 'text', 'panel-name', 'off', d.display_name);
    name.maxLength = 8;
    name.required = true;
    const options = [{ value: 0, label: '不展示磁盘容量' }];
    const inv=data.profile?.inventory||{};
    const disks=new Map((inv.drives||[]).map(d=>[d.id,d]));
    for(const object of inv.objects||[])if(object.kind==='drive'&&!disks.has(object.id))disks.set(object.id,object);
    for (const disk of disks.values())
        options.push({ value: disk.id, label: objectTitle(inv,'drive',disk.id) });
    const select = pixelSelect('panel-disk', options, d.disk_id);
    const form = el('form', { class: 'panel-form' }, [el('label', { class: 'checkbox-label' }, [checkbox, '在 ESP32 面板展示']), el('p', { class: 'hint', text: '最多 4 台设备。磁盘必须明确选择物理磁盘。' }), nameField, el('div', { class: 'field' }, [el('label', { for: 'panel-disk', text: 'DISK 指标来源' }), select.root]), el('div', { class: 'actions' }, el('button', { class: 'btn primary', type: 'submit', text: '保存面板设置' }))]);
    busyForm(form, async () => { await api('/api/devices/' + selected + '/panel', 'PUT', { enabled: checkbox.checked, name: name.value, disk_id: Number(select.value) }); form.closest('section').dataset.dirty = 'false'; await refreshDevices(); notice('面板设置已保存'); });
    return form;
}
function chart(samples, field, label) {
    const width = 800, base = 150, unit = 1.4;
    const first = samples[0].t, last = samples[samples.length - 1].t, span = last - first;
    const deltas = [];
    for (let i = 1; i < samples.length; i++)
        if (samples[i].t > samples[i - 1].t)
            deltas.push(samples[i].t - samples[i - 1].t);
    deltas.sort((a, b) => a - b);
    const gap = Math.max(60, (deltas.length ? deltas[Math.floor(deltas.length / 2)] : 0) * 6);
    const metric = field === 'c' ? 'cpu.usage' : 'memory.usage';
    const percent = v => Math.max(0, Math.min(100, v / 10));
    const canvas = el('canvas', { class: 'sample-chart', width, height: 160, role: 'img', 'aria-label': label + ' 历史使用率，' + date(first) + ' 至 ' + date(last) + '，最新 ' + format(metric, samples[samples.length - 1][field]) });
    requestAnimationFrame(() => {
        const ctx = canvas.getContext('2d');
        if (!ctx)
            return;
        ctx.strokeStyle = '#c7c7c0';
        ctx.lineWidth = 1;
        [0, 50, 100].forEach(y => { ctx.beginPath(); ctx.moveTo(0, base - y * unit); ctx.lineTo(width, base - y * unit); ctx.stroke(); });
        ctx.strokeStyle = '#222';
        ctx.lineWidth = 2;
        ctx.beginPath();
        let previous = null;
        samples.forEach(sample => {
            const x = span > 0 ? (sample.t - first) / span * width : width / 2;
            const y = base - percent(sample[field]) * unit;
            if (previous === null || sample.t - previous > gap)
                ctx.moveTo(x, y);
            else
                ctx.lineTo(x, y);
            previous = sample.t;
        });
        ctx.stroke();
        if (samples.length === 1) {
            ctx.fillStyle = '#222';
            ctx.fillRect(width / 2 - 3, base - percent(samples[0][field]) * unit - 3, 6, 6);
        }
    });
    const rows = samples.slice().reverse().map(sample => el('tr', {}, [el('td', { text: date(sample.t) }), el('td', { text: format(metric, sample[field]) })]));
    const table = el('table', {}, [el('caption', { text: label + ' 使用率历史数据' }), el('thead', {}, el('tr', {}, [el('th', { text: '时间' }), el('th', { text: '使用率' })])), el('tbody', {}, rows)]);
    return el('div', {}, [canvas, el('div', { class: 'sr-only' }, table)]);
}
async function refreshMetrics() {
    if (!selected || !devices.some(d => d.id === selected))
        return;
    const id = selected, gen = generation;
    const data = await api('/api/devices/' + id + '/metrics');
    const history = await api('/api/devices/' + id + '/history');
    if (page !== 'metrics' || selected !== id || generation !== gen)
        return;
    const area = $('#metric-detail');
    if (!area)
        return;
    let current = area.querySelector('#panel-settings');
    const active = current?.contains(document.activeElement) || current?.dataset.dirty === 'true';
    const blocks = [el('div', { class: 'metric-status muted', text: statusLabel(data.device) + ' · 档案版本 ' + data.device.revision + ' · 最近采样 ' + date(data.device.last_seen_at) }), el('div', { class: 'grid metric-list' }, metricRows(data))];
    const samples = history.batches.flatMap(b => b.batch.s).sort((a, b) => a.t - b.t).slice(-100);
    blocks.push(el('h2', { class: 'section-title', text: '最近 100 轮采样' }));
    if (samples.length)
        blocks.push(el('div', { class: 'grid' }, [card('CPU 使用率 · 0–100%', [chart(samples, 'c', 'CPU')], 'span6'), card('内存使用率 · 0–100%', [chart(samples, 'm', '内存')], 'span6')]), el('p', { class: 'hint', text: date(samples[0].t) + ' — ' + date(samples.at(-1).t) }));
    else
        blocks.push(card('采样历史', [el('p', { class: 'empty', text: '尚未收到样本。' })]));

    if (!active) {
        current = card('ESP32 面板设置', [panelForm(data)]);
        current.id = 'panel-settings';
        current.addEventListener('input', () => { current.dataset.dirty = 'true'; });
        current.addEventListener('change', () => { current.dataset.dirty = 'true'; });
    }
    let values = area.querySelector('#metric-values');
    if (!values) {
        values = el('div', { id: 'metric-values' });
        area.append(values);
    }
    values.replaceChildren(...blocks);
    const previous = area.querySelector('#panel-settings');
    if (previous !== current) {
        if (previous)
            previous.replaceWith(current);
        else
            area.append(current);
    }
}
let polling = false, repoll = false;
async function poll() { clearTimeout(timer); if (polling) {
    repoll = true;
    return;
} ; if (!authenticated || forced || page === 'password' || page === 'token' || document.hidden)
    return; polling = true; try {
    await refreshDevices();
    if (page === 'metrics')
        await refreshMetrics();
}
catch (e) {
    if (authenticated)
        notice(e.message);
}
finally {
    polling = false;
    if (authenticated && !forced && !document.hidden)
        timer = setTimeout(poll, repoll ? 0 : 5000);
    repoll = false;
} }
function render() { clearTimeout(timer); generation++; if (!authenticated) {
    renderLogin();
    return;
} if (forced || page === 'password') {
    passwordPage();
    return;
} if (page === 'token') {
    tokenPage();
    return;
} if (page === 'metrics')
    renderMetrics();
else
    renderDevices(); void poll(); }
document.querySelectorAll('[data-page]').forEach(b => b.addEventListener('click', () => { page = b.dataset.page; render(); }));
for(const logout of [$('#logout'),$('#logout-mobile')])logout.addEventListener('click', async () => { try {
    await api('/api/auth/logout', 'POST', {});
    authenticated = false;
    csrf = '';
    renderLogin();
}
catch (e) {
    notice(e.message);
} });
document.addEventListener('visibilitychange', () => { if (document.hidden)
    clearTimeout(timer);
else
    void poll(); });
(async () => { try {
    acceptState(await api('/api/auth/state'));
    render();
}
catch {
    renderLogin();
} })();
