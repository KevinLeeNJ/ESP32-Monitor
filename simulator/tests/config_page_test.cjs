// Run the embedded configuration script against a small DOM/fetch harness.
const {readFileSync} = require('node:fs');
const {resolve} = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const html = readFileSync(resolve(__dirname, '../../firmware/components/esp_wifi_connect/assets/wifi_configuration.html'), 'utf8');
const script = html.match(/<script[^>]*>([\s\S]*?)<\/script>/)[1];
const elements = new Map();
const attributed = [];
for (const tag of html.matchAll(/<[^!\/][^>]*>/g)) {
    const attributes = Object.fromEntries([...tag[0].matchAll(/([\w-]+)="([^"]*)"/g)].map(m => [m[1], m[2]]));
    const el = {value: '', checked: false, disabled: false, textContent: '', placeholder: '', dataset: {}, style: {},
        getAttribute: key => attributes[key], addEventListener() {}, classList: {add() {}, remove() {}}};
    if (attributes.id) elements.set(attributes.id, el);
    attributed.push(el);
}
function scanElement() {
    return {children: [], events: {}, textContent: '', html: '',
        get innerHTML() { return this.html; },
        set innerHTML(value) { this.html = value; this.children = []; },
        appendChild(child) { this.children.push(child); },
        querySelector() { return this.children[0] || null; },
        addEventListener(name, callback) { this.events[name] = callback; }, blur() {}};
}
elements.set('ap_list', scanElement());
elements.get('ssid').blur = () => {};
const document = {
    createElement: scanElement,
    getElementById: id => elements.get(id), addEventListener() {},
    querySelectorAll: selector => {
        const key = selector.match(/^\[([^\]]+)\]$/)?.[1];
        return key ? attributed.filter(el => el.getAttribute(key)) : [];
    },
};
let calls = [], mode = 'load';
let timers = new Map(), timerId = 0, fakeNow = 0;
const listeners = new Map();
document.addEventListener = (name, fn) => listeners.set(name, fn);
const ctx = vm.createContext({document, console, window: {addEventListener() {}},
    localStorage: {setItem() {}}, setTimeout(fn) { timers.set(++timerId, fn); return timerId; }, clearTimeout(id) { timers.delete(id); },
    fetch: async (url, options) => {
        calls.push({url, options});
        const result = mode === 'scan' ? {support_5g: false, aps: [{ssid: 'Lab "2.4G"\\AP', rssi: -42, authmode: 3}]} :
            mode === 'empty_scan' ? {support_5g: false, aps: []} : mode === 'load' ? {devices_api_url: 'https://baseurl.com:8443', devices_api_token_configured: true, qweather_key_configured: true} :
            mode === 'invalid' ? {success: false, error: 'invalid_devices_api_url'} : {success: true};
        return {ok: true, json: async () => result};
    }});
ctx.Date = {now: () => fakeNow};
vm.runInContext(script, ctx);
(async () => {
    const langs = vm.runInContext('Object.keys(translations)', ctx);
    for (const lang of langs) {
        elements.get('language').value = lang;
        vm.runInContext('changeLanguage()', ctx);
        for (const el of attributed) {
            if (el.getAttribute('data-lang')) assert.ok(el.textContent && el.textContent !== 'undefined', lang);
            if (el.getAttribute('data-lang-placeholder')) assert.ok(el.placeholder, lang);
        }
    }
    elements.get('language').value = 'zh-CN';
    vm.runInContext('changeLanguage()', ctx);
    calls = [];
    await vm.runInContext('submitForm({preventDefault() {}})', ctx);
    assert.equal(calls.length, 0, 'save waits for advanced config to load, preventing defaults from overwriting saved fields');
    await vm.runInContext('loadAdvancedConfig()', ctx);
    assert.equal(elements.get('devices_api_url').value, 'https://baseurl.com:8443');
    assert.equal(elements.get('devices_api_token').value, '');
    assert.match(elements.get('devices_api_token').placeholder, /已配置/);
    elements.get('language').value = 'en-US';
    vm.runInContext('changeLanguage()', ctx);
    assert.match(elements.get('devices_api_token').placeholder, /Configured/);
    elements.get('ssid').value = 'test-network';
    elements.get('password').value = 'test-password';
    mode = 'submit'; calls = [];
    await vm.runInContext('submitAdvancedForm({preventDefault() {}})', ctx);
    const config = JSON.parse(calls[0].options.body);
    assert.equal(config.devices_api_url, 'https://baseurl.com:8443');
    assert.equal(config.ssid, 'test-network');
    assert.equal(config.password, 'test-password');
    assert.equal(config.devices_api_token, '');
    assert.equal(config.devices_api_token_clear, false);
    assert.equal(calls.length, 1, 'one save request includes all fields and server exits without a second request');
    assert.equal(elements.get('advanced_button').disabled, true);
    vm.runInContext('saving = false', ctx);
    mode = 'invalid'; calls = [];
    elements.get('language').value = 'zh-CN';
    await vm.runInContext('submitAdvancedForm({preventDefault() {}})', ctx);
    assert.match(elements.get('advanced_error').textContent, /端口/);
    assert.equal(calls.length, 1, 'invalid settings must keep the configuration session open');
    mode = 'submit'; calls = [];
    elements.get('devices_api_token_clear').checked = true;
    await vm.runInContext('submitAdvancedForm({preventDefault() {}})', ctx);
    assert.equal(JSON.parse(calls[0].options.body).devices_api_token_clear, true);
    vm.runInContext('saving = false', ctx);
    calls = []; timers.clear();
    listeners.get('input')(); listeners.get('input')(); listeners.get('change')();
    assert.equal(timers.size, 1, 'typing is coalesced into one edit notification');
    assert.equal(calls.length, 1, 'first edit reports activity immediately');
    fakeNow += 1000;
    listeners.get('input')();
    assert.equal(calls.length, 2, 'continuous typing also renews activity before debounce ends');
    [...timers.values()][0]();
    assert.equal(calls.length, 3, 'final edit has a trailing notification');
    assert.ok(calls.every(call => call.url === '/activity'));
    calls = [];
    elements.get('ssid').value = '';
    elements.get('password').value = '';
    await vm.runInContext('submitForm({preventDefault() {}})', ctx);
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, '/advanced/submit', 'Wi-Fi tab uses same unified save');
    assert.equal(JSON.parse(calls[0].options.body).ssid, '', 'advanced-only save preserves saved Wi-Fi');
    vm.runInContext('saving = false', ctx);
    mode = 'scan'; calls = []; timers.clear();
    await vm.runInContext('loadAPList()', ctx);
    assert.equal(calls.length, 1, 'scan polling must not count as editing');
    assert.equal(calls[0].url, '/scan');
    const links = elements.get('ap_list').children;
    assert.equal(links.length, 1);
    assert.match(links[0].textContent, /2.4G/);
    links[0].events.click({preventDefault() {}});
    assert.equal(elements.get('ssid').value, 'Lab "2.4G"\\AP');
    mode = 'empty_scan';
    await vm.runInContext('loadAPList()', ctx);
    assert.match(elements.get('ap_list').innerHTML, /scanning/, 'empty scans show progress while waiting for results');
    console.log(`Configuration page checks passed (${langs.length} locales, custom port, token state, localized errors, clear token)`);
})().catch(error => {console.error(error); process.exitCode = 1;});
