import { api, post, setMessage, icon } from './ui-core.js';
import { createShell } from './shell.js';
import { createLibrary } from './library.js';
import { createDownloads } from './downloads.js';
import { createSettings } from './settings.js';
import { createFollowing } from './following.js';
import { createDetails } from './details.js';
import { initializeViewer } from './viewer.js';
import { createEmby } from './emby.js';

const app = {api, post};
app.play = (id, title) => {
  window.dramaPlayer.open(id, title);
  app.following.acknowledge(id);
  app.library.refreshDrama(id);
};
app.shell = createShell(app);
app.library = createLibrary(app);
app.downloads = createDownloads(app);
app.settings = createSettings(app);
app.following = createFollowing(app);
app.details = createDetails(app);
app.emby = createEmby(app);
app.rankings = window.DuanjuRankings.init({
  api, post, icon,
  getDrama: id => app.library.get(id),
  openDetails: id => app.details.open(id),
  onDownloadsChanged: () => app.downloads.refresh?.(),
  getDownloadQuality: app.downloads.quality,
  play: app.play
});

app.shell.init();
window.addEventListener('duanjuviewerchange', () => {
  document.getElementById('viewerNotice').textContent = '浏览器身份已变化，正在重新加载…';
  document.getElementById('viewerNotice').hidden = false;
  window.location.reload();
});

async function initialize() {
  try {
    app.viewer = await initializeViewer();
  } catch (error) {
    const notice = document.getElementById('viewerNotice');
    const retry = document.createElement('button');
    retry.type = 'button';
    retry.className = 'secondary';
    retry.textContent = '重试';
    retry.addEventListener('click', () => window.location.reload());
    notice.replaceChildren(document.createTextNode(error.message), retry);
    notice.hidden = false;
    return;
  }
app.emby.init();
app.shell.access();

Promise.allSettled([app.settings.init(), app.library.init(), app.downloads.init(), app.following.init()]).then(results => {
  const failure = results.find(result => result.status === 'rejected');
  if (failure) setMessage('部分功能未能初始化：' + (failure.reason?.message || '请刷新页面重试'), true);
  document.documentElement.dataset.ready = 'true';
});

}

initialize();
