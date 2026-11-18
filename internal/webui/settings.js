import { $, api, post, number, icon } from './ui-core.js';

import { createPlaybackSettings } from './playback-settings.js';

export function createSettings(app) {
  let config = {};
function refreshConfigFields(){ $('downloadLocation').textContent=config.outputDir||'尚未设置';$('downloadLocation').title=config.outputDir||''; $('downloadConcurrency').value=config.concurrency||2;$('requestConcurrency').value=config.requestConcurrency||2;$('requestInterval').value=config.requestIntervalMs||500;$('proxyMode').value=config.proxyMode||'auto';$('proxyURL').value=config.proxyURL||'';$('proxyUsername').value='';$('proxyPassword').value='';$('proxyPassword').placeholder=config.proxyHasAuth?'已保存，留空保留认证':'';updateProxyFields();$('configText').textContent='当前输出：'+(config.outputDir||'')+' · 下载并发 '+(config.concurrency||2)+' · '+(config.network||'')+' · FFmpeg '+(config.ffmpeg||''); }

function updateProxyFields(){const disabled=$('proxyMode').value!=='manual';for(const name of ['proxyURL','proxyUsername','proxyPassword'])$(name).disabled=disabled;}

async function loadConfig(){try{config=await api('/api/ui/config');refreshConfigFields();}catch(error){$('configText').textContent='配置读取失败：'+error.message;}}

async function saveSettings(){
    // long: 下载目录改由部署配置管理，设置页只提交运行时并发与代理，避免远程网页误改服务路径。
    const settings={concurrency:number($('downloadConcurrency').value),requestConcurrency:number($('requestConcurrency').value),requestIntervalMs:number($('requestInterval').value)};
    try{
      const mode=$('proxyMode').value;if(mode!=='manual')settings.proxyURL=mode;else{const raw=$('proxyURL').value.trim();const username=$('proxyUsername').value;const password=$('proxyPassword').value;if(!(config.proxyHasAuth&&config.proxyMode==='manual'&&raw===config.proxyURL&&!username&&!password)){const endpoint=new URL(raw);if(!['http:','https:','socks5:','socks5h:'].includes(endpoint.protocol))throw new Error('仅支持 HTTP/HTTPS/SOCKS5 代理');if(username||password){endpoint.username=username;endpoint.password=password;}settings.proxyURL=endpoint.toString();}}
      $('saveSettingsBtn').disabled=true;config=await post('/api/ui/config',settings);refreshConfigFields();$('settingsStatus').textContent='设置已保存并应用';
    }catch(error){$('settingsStatus').textContent='保存失败：'+error.message;}finally{$('saveSettingsBtn').disabled=false;}
  }

async function checkNetwork(){ $('checkNetworkBtn').disabled=true;$('settingsStatus').textContent='正在检测已保存的网络配置和入口，最多约一分钟…';try{const result=await post('/api/ui/network/check',{});$('settingsStatus').textContent=(result.data||[]).map(item=>item.name+'：'+(item.error||[item.status?'HTTP '+item.status:'',item.detail||''].filter(Boolean).join(' · '))).join('；');}catch(error){$('settingsStatus').textContent='检测失败：'+error.message;}finally{$('checkNetworkBtn').disabled=false;} }

function setAppVersion(info){const version=info.currentVersion||info.version||'未知';$('appVersionText').textContent='当前 '+version;$('appVersionText').title='当前 '+version+(info.currentCommit?' · commit '+info.currentCommit:'')+(info.buildTime?' · 构建 '+info.buildTime:'');}
async function loadAppVersion(){try{const info=await api('/healthz');setAppVersion(info);}catch(error){$('appVersionText').textContent='版本读取失败';$('appVersionText').title=error.message;}}

function init() {
  $('proxyMode').addEventListener('change', updateProxyFields);
  $('saveSettingsBtn').addEventListener('click', saveSettings);
  $('checkNetworkBtn').addEventListener('click', checkNetwork);
  const themeButton = $('themeToggleBtn');
  const syncThemeButton = () => {
    const dark = document.documentElement.dataset.theme === 'dark';
    const label = dark ? '切换到浅色主题' : '切换到深色主题';
    themeButton.replaceChildren(icon(dark ? 'sun' : 'moon'));
    themeButton.setAttribute('aria-label', label);
    themeButton.title = label;
  };
  themeButton.addEventListener('click', () => {
    window.DuanjuTheme.set(document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark');
    syncThemeButton();
  });
  document.addEventListener('themechange', syncThemeButton);
  syncThemeButton();
  return Promise.all([loadConfig(), createPlaybackSettings().init(), loadAppVersion()]);
}
return {init, config: () => config};

}
