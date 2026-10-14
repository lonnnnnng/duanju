import { $, element, empty, button, setMessage, api, post, valueText, firstNonEmpty, dramaTitle, watchLabel, historySuffix, normalizeSource, sourceKey, sourceLabel, categoryName, episodeCount, coverURL, tagsText, dramaSearchText, rebuildOptions, number, formatBytes, formatTime, statusText, releaseText, phaseText, progressText, progressBar, episodeLabel, groupStats } from './ui-core.js';

import { createPlaybackSettings } from './playback-settings.js';

export function createSettings(app) {
  let config = {};
function refreshConfigFields(){ $('downloadLocation').textContent=config.outputDir||'尚未设置';$('downloadLocation').title=config.outputDir||''; $('downloadDirectory').value=config.outputDirSetting||config.outputDir||'./短剧下载';$('downloadDirectoryHint').textContent=(config.restartRequired?'已保存新目录，重启后生效。':'')+'选择程序所在电脑的文件夹，或输入相对/绝对路径。重启后新合集使用新目录，原任务和文件保留。';$('downloadConcurrency').value=config.concurrency||2;$('requestConcurrency').value=config.requestConcurrency||2;$('requestInterval').value=config.requestIntervalMs||500;$('proxyMode').value=config.proxyMode||'auto';$('proxyURL').value=config.proxyURL||'';$('proxyUsername').value='';$('proxyPassword').value='';$('proxyPassword').placeholder=config.proxyHasAuth?'已保存，留空保留认证':'';updateProxyFields();$('configText').textContent='当前输出：'+(config.outputDir||'')+' · 下载并发 '+(config.concurrency||2)+' · '+(config.network||'')+' · FFmpeg '+(config.ffmpeg||''); }

function updateProxyFields(){const disabled=$('proxyMode').value!=='manual';for(const name of ['proxyURL','proxyUsername','proxyPassword'])$(name).disabled=disabled;}

async function loadConfig(){try{config=await api('/api/ui/config');refreshConfigFields();}catch(error){$('configText').textContent='配置读取失败：'+error.message;}}

async function saveSettings(){
    const settings={outputDir:$('downloadDirectory').value.trim(),concurrency:number($('downloadConcurrency').value),requestConcurrency:number($('requestConcurrency').value),requestIntervalMs:number($('requestInterval').value)};
    try{
      const mode=$('proxyMode').value;if(mode!=='manual')settings.proxyURL=mode;else{const raw=$('proxyURL').value.trim();const username=$('proxyUsername').value;const password=$('proxyPassword').value;if(!(config.proxyHasAuth&&config.proxyMode==='manual'&&raw===config.proxyURL&&!username&&!password)){const endpoint=new URL(raw);if(!['http:','https:','socks5:','socks5h:'].includes(endpoint.protocol))throw new Error('仅支持 HTTP/HTTPS/SOCKS5 代理');if(username||password){endpoint.username=username;endpoint.password=password;}settings.proxyURL=endpoint.toString();}}
      $('saveSettingsBtn').disabled=true;config=await post('/api/ui/config',settings);refreshConfigFields();$('settingsStatus').textContent=config.restartRequired?'设置已保存；下载目录重启后生效，当前任务保持原路径':'设置已保存并应用';
    }catch(error){$('settingsStatus').textContent='保存失败：'+error.message;}finally{$('saveSettingsBtn').disabled=false;}
  }

async function checkNetwork(){ $('checkNetworkBtn').disabled=true;$('settingsStatus').textContent='正在检测已保存的网络配置和入口，最多约一分钟…';try{const result=await post('/api/ui/network/check',{});$('settingsStatus').textContent=(result.data||[]).map(item=>item.name+'：'+(item.error||[item.status?'HTTP '+item.status:'',item.detail||''].filter(Boolean).join(' · '))).join('；');}catch(error){$('settingsStatus').textContent='检测失败：'+error.message;}finally{$('checkNetworkBtn').disabled=false;} }

let updateInfo = null;
function renderUpdateInfo(info){updateInfo=info;$('appVersionText').textContent='当前 '+(info.currentVersion||'未知')+' · commit '+(info.currentCommit||'未知')+' · 构建 '+(info.buildTime||'未知');$('applyAppUpdateBtn').hidden=!info.updateReady||!info.updateAvailable||info.updateApplied;$('appUpdateStatus').textContent=info.message||(info.latestVersion?'最新正式版 '+info.latestVersion+' · '+(info.assetName||'当前平台暂无安装包'):'尚未检查更新');}
async function loadAppVersion(){try{const info=await api('/healthz');$('appVersionText').textContent='当前 '+(info.version||'未知')+' · commit '+(info.commit||'未知');}catch(error){$('appVersionText').textContent='版本信息读取失败：'+error.message;}}
async function checkAppUpdate(){const button=$('checkAppUpdateBtn');button.disabled=true;$('appUpdateStatus').textContent='正在查询 GitHub Release…';try{renderUpdateInfo(await api('/api/ui/admin/update'));}catch(error){$('appUpdateStatus').textContent='检查失败：'+error.message;}finally{button.disabled=false;}}
async function waitForUpdatedHealth(version){for(let attempt=0;attempt<60;attempt+=1){try{const health=await api('/healthz');if(health.version===version){$('appVersionText').textContent='当前 '+(health.version||'未知')+' · commit '+(health.commit||'未知');$('appUpdateStatus').textContent='服务已重启并运行新版本';return true;}}catch(error){/* long: systemd 重启期间连接会短暂失败，下一轮继续确认服务是否已恢复。 */}await new Promise(resolve=>setTimeout(resolve,1000));}return false;}
async function applyAppUpdate(){if(!updateInfo?.updateReady)return;const button=$('applyAppUpdateBtn');button.disabled=true;$('appUpdateStatus').textContent='正在下载并校验程序…';try{const result=await post('/api/ui/admin/update',{});renderUpdateInfo(result);if(result.autoRestart&&result.latestVersion){$('appUpdateStatus').textContent='服务正在重启…';if(!await waitForUpdatedHealth(result.latestVersion))$('appUpdateStatus').textContent='程序已替换，但尚未确认新服务健康，请查看 systemd 状态';}else $('appUpdateStatus').textContent=result.message||'更新完成';}catch(error){$('appUpdateStatus').textContent='更新失败：'+error.message;}finally{button.disabled=false;}}

async function chooseDownloadDirectory(){const button=$('browseDirectoryBtn');if(button.disabled)return;button.disabled=true;$('settingsStatus').textContent='请在系统窗口中选择下载文件夹…';try{const result=await post('/api/ui/directory/pick',{initialPath:$('downloadDirectory').value});if(result.canceled){$('settingsStatus').textContent='已取消选择，原目录未修改';return;}$('downloadDirectory').value=result.selectionPath||result.path;$('settingsStatus').textContent='已选择文件夹，请点击“保存并应用”';}catch(error){$('settingsStatus').textContent='选择文件夹失败：'+error.message;}finally{button.disabled=false;}}

async function importLegacyRecords() {
  const control = $('legacyImportBtn');
  control.disabled = true;
  $('viewerImportStatus').textContent = '正在接回旧版记录…';
  try {
    await post('/api/ui/viewer/legacy', {code: $('legacyImportCode').value.trim()});
    $('legacyImportCode').value = '';
    $('legacyImportSection').hidden = true;
    app.viewer.legacyAvailable = false;
    await Promise.all([window.DuanjuHistory.refresh(), app.following.refresh()]);
    $('viewerImportStatus').textContent = app.viewer?.account ? '旧版记录已接入当前账号，原始文件仍保留。' : '旧版记录已接回当前浏览器，原始文件仍保留。';
  } catch (error) {
    $('viewerImportStatus').textContent = error.message;
  } finally {
    control.disabled = false;
  }
}

function init() {
  const admin = Boolean(app.viewer?.account?.admin && !app.viewer.account.requirePasswordChange);
  $('serverSettings').hidden = !admin;
  $('serverSettingsNotice').hidden = admin;
  $('downloadLocationBtn').hidden = !admin;
  if (!admin) $('downloadLocation').textContent = '服务端下载目录';
  $('legacyImportSection').hidden = !app.viewer?.legacyAvailable;
  $('viewerImportStatus').textContent = app.viewer?.legacyError || '';
  $('legacyImportBtn').addEventListener('click', importLegacyRecords);
  $('browseDirectoryBtn').addEventListener('click', chooseDownloadDirectory);
  $('proxyMode').addEventListener('change', updateProxyFields);
  $('saveSettingsBtn').addEventListener('click', saveSettings);
  $('checkNetworkBtn').addEventListener('click', checkNetwork);
  $('checkAppUpdateBtn').addEventListener('click', checkAppUpdate);
  $('applyAppUpdateBtn').addEventListener('click', applyAppUpdate);
  $('openSettingsBtn').addEventListener('click', () => window.DuanjuDialogs.open('settingsPanel'));
  $('closeSettingsBtn').addEventListener('click', () => $('settingsPanel').close());
  $('themeSelect').value = window.DuanjuTheme.get();
  $('themeSelect').addEventListener('change', () => window.DuanjuTheme.set($('themeSelect').value));
  document.addEventListener('themechange', event => {$('themeSelect').value = event.detail;});
  return admin ? Promise.all([loadConfig(), createPlaybackSettings().init(), loadAppVersion(), checkAppUpdate()]) : Promise.resolve();
}
return {init, config: () => config};

}
