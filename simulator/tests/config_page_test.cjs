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
const document = {
    getElementById: id => elements.get(id), addEventListener() {},
    querySelectorAll: selector => {
        const key = selector.match(/^\[([^\]]+)\]$/)?.[1];
        return key ? attributed.filter(el => el.getAttribute(key)) : [];
    },
};
let calls = [], mode = 'load';
const ctx = vm.createContext({document, console, window: {addEventListener() {}},
    localStorage: {setItem() {}}, setTimeout() {}, clearTimeout() {},
    fetch: async (url, options) => {
        calls.push({url, options});
        const result = mode === 'load' ? {devices_api_url: 'https://baseurl.com:8443', devices_api_token_configured: true, qweather_key_configured: true} :
            mode === 'invalid' ? {success: false, error: 'invalid_devices_api_url'} : {success: true};
        return {ok: true, json: async () => result};
    }});
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
    await vm.runInContext('loadAdvancedConfig()', ctx);
    assert.equal(elements.get('devices_api_url').value, 'https://baseurl.com:8443');
    assert.equal(elements.get('devices_api_token').value, '');
    assert.match(elements.get('devices_api_token').placeholder, /已配置/);
    elements.get('language').value = 'en-US';
    vm.runInContext('changeLanguage()', ctx);
    assert.match(elements.get('devices_api_token').placeholder, /Configured/);
    mode = 'submit'; calls = [];
    await vm.runInContext('submitAdvancedForm({preventDefault() {}})', ctx);
    const config = JSON.parse(calls[0].options.body);
    assert.equal(config.devices_api_url, 'https://baseurl.com:8443');
    assert.equal(config.devices_api_token, '');
    assert.equal(config.devices_api_token_clear, false);
    assert.equal(calls[1].url, '/exit');
    mode = 'invalid'; calls = [];
    elements.get('language').value = 'zh-CN';
    await vm.runInContext('submitAdvancedForm({preventDefault() {}})', ctx);
    assert.match(elements.get('advanced_error').textContent, /端口/);
    assert.equal(calls.length, 1, 'invalid settings must keep the configuration session open');
    mode = 'submit'; calls = [];
    elements.get('devices_api_token_clear').checked = true;
    await vm.runInContext('submitAdvancedForm({preventDefault() {}})', ctx);
    assert.equal(JSON.parse(calls[0].options.body).devices_api_token_clear, true);
    console.log(`Configuration page checks passed (${langs.length} locales, custom port, token state, localized errors, clear token)`);
})().catch(error => {console.error(error); process.exitCode = 1;});
