let identity = "";
let changed = false;

export function viewerHeaders() {
  return identity ? {'X-Duanju-Viewer': identity} : {};
}

export function checkViewerResponse(result) {
  if (!identity || changed || !['viewer_required', 'viewer_changed'].includes(result?.code)) return;
  changed = true;
  window.dispatchEvent(new Event('duanjuviewerchange'));
}

export async function initializeViewer() {
  async function read(path) {
    const response = await fetch(path, {credentials: 'same-origin', cache: 'no-store', headers: {'Accept': 'application/json'}});
    let result;
    try {result = await response.json();} catch (_) {throw new Error('无法建立独立观看记录，请确认已启动新版程序');}
    if (!response.ok) throw new Error(result.error || '无法建立独立观看记录');
    return result;
  }
  let result = await read('/api/ui/viewer');
  if (!result.ready) result = await read('/api/ui/viewer?confirm=1');
  if (!result.ready || !/^[a-f0-9]{64}$/.test(result.id)) throw new Error('无法确认浏览器身份，请刷新页面重试');
  identity = result.id;
  changed = false;
  return result;
}

window.DuanjuViewer = {headers: viewerHeaders, checkResponse: checkViewerResponse};
