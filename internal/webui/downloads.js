import { reconcileChildren, withFocus, readPreference, savePreference } from './ui-core.js';
import { $, element, empty, button, setMessage, api, post, valueText, firstNonEmpty, dramaTitle, watchLabel, historySuffix, normalizeSource, sourceKey, sourceLabel, categoryName, episodeCount, coverURL, tagsText, dramaSearchText, rebuildOptions, number, formatBytes, formatTime, statusText, releaseText, phaseText, progressText, progressBar, episodeLabel, groupStats } from './ui-core.js';

export function createDownloads(app) {
  let tasks = [], visibleGroups = [], mergeStates = {}, taskRequest = false, ffmpegStatus = '';
  const selectedTasks = new Set(), selectedMerges = new Set(), exportingMerges = new Set(), verifyingGroups = new Set(), openGroups = new Set(), groupsEl = $('groups');
  let pollTimer, pollPromise, batch = false, taskSignature = '', ffmpegSignature = '';
  let hasActiveTasks = false, polling = false, scheduledDelay = 0;
  const qualityOptions = [0, 2160, 1440, 1080, 720, 540, 480, 360];
  const storedQuality = number(readPreference('downloadQuality', 0));
  let downloadQuality = qualityOptions.includes(storedQuality) ? storedQuality : 0;
  const qualityLabel = value => value ? value + 'p 优先' : '最高可用';

  function setupQualitySelect(select) {
    select.dataset.downloadQuality = '';
    for (const quality of qualityOptions) {
      const option = element('option', '', qualityLabel(quality));
      option.value = String(quality);
      select.appendChild(option);
    }
    select.value = String(downloadQuality);
    select.title = '用于新加入的下载任务；缺少所选档位时取不高于该档位的最高画质，全部更高时取最低可用画质。保留源文件，不压缩。';
    select.addEventListener('change', () => {
      downloadQuality = number(select.value);
      savePreference('downloadQuality', downloadQuality);
      document.querySelectorAll('[data-download-quality]').forEach(node => {node.value = String(downloadQuality);});
    });
  }

  function qualityControl() {
    const label = element('label', 'download-quality', '下载画质');
    const select = element('select');
    select.setAttribute('aria-label', '本次下载清晰度');
    select.dataset.focusKey = 'detail-download-quality';
    setupQualitySelect(select);
    label.appendChild(select);
    return label;
  }
function hasMergeRecord(id){return Boolean(mergeStates[id]?.outputPath || mergeStates[id]?.playbackTaskId);}

function buildGroups(){const byID=new Map();tasks.forEach(task=>{const id=task.dramaId||task.dramaTitle||task.id;let group=byID.get(id);if(!group){group={id,title:task.dramaTitle||'短剧',tasks:[],release:'unknown'};byID.set(id,group);}group.tasks.push(task);if(task.releaseStatus)group.release=task.releaseStatus;});for(const [id,merge] of Object.entries(mergeStates)){if(hasMergeRecord(id)&&!byID.has(id))byID.set(id,{id,title:merge.dramaTitle||'短剧',tasks:[],release:'unknown'});}return Array.from(byID.values());}

// long: 只保留全集记录时不能用 0/0 分集判断“已完成”；删除意图尚未清理完也应归入未完成和失败。
function groupMatches(group){const keyword=$('taskSearch').value.trim().toLowerCase();if(keyword&&!group.title.toLowerCase().includes(keyword))return false;const release=$('releaseStatus').value;if(release&&group.release!==release)return false;const status=$('taskStatus').value;if(mergeStates[group.id]?.deleteRequested){if(status==='completed')return false;if(status==='unfinished'||status==='failed')return true;}const stats=groupStats(group);if(!group.tasks.length){const merged=mergeStates[group.id]?.status;if(status==='completed')return merged==='success';if(status==='unfinished')return merged!=='success';if(status==='failed')return merged==='failed';if(status==='running')return merged==='queued'||merged==='running';return !status;}const unavailable=group.tasks.some(task=>task.localFileUnavailable);if(status==='completed')return stats.success===group.tasks.length&&!unavailable;if(status==='unfinished')return stats.success!==group.tasks.length||unavailable;if(status==='failed')return stats.failed>0;if(status==='running')return stats.running+stats.parsing+stats.queued>0;if(status==='paused')return stats.paused>0;if(status==='canceled')return stats.canceled>0;return true;}

function selectGroup(group, checked){group.tasks.forEach(task=>{if(checked)selectedTasks.add(task.id);else selectedTasks.delete(task.id);});if(hasMergeRecord(group.id)){if(checked)selectedMerges.add(group.id);else selectedMerges.delete(group.id);}renderTasks(tasks);}

function selectedTaskList(){return tasks.filter(task=>selectedTasks.has(task.id));}

function selectedGroupIDs(){return Array.from(new Set(selectedTaskList().map(task=>task.dramaId)));}

function selectedMergeGroupIDs(){return Array.from(new Set(selectedTaskList().filter(task=>task.status==='success' && task.dramaId && !['queued','running'].includes(mergeStates[task.dramaId]?.status) && !mergeStates[task.dramaId]?.cleanupPending?.length).map(task=>task.dramaId)));}

function selectedActionTasks(action){
  const statuses={pause:['running','queued','parsing'],resume:['paused'],cancel:['running','queued','parsing','paused'],retry:['failed','canceled']}[action]||[];
  return selectedTaskList().filter(task=>(statuses.includes(task.status)||action==='retry'&&task.localFileRetryable) && (action!=='pause'||!task.pauseRequested) && (action!=='cancel'||!task.cancelRequested));
}

function updateTaskSelection(){
  const selected=selectedTaskList(), visible=new Set(visibleGroups.flatMap(group=>group.tasks.map(task=>task.id)));
  const hidden=selected.filter(task=>!visible.has(task.id)).length;
  $('taskSelection').textContent='已选 '+selectedTasks.size+' 集 / '+selectedMerges.size+' 个全集'+(hidden?'（隐藏 '+hidden+' 集）':'');
  // long: 批量按钮只在至少一个选中任务真正符合动作状态时启用，避免成功任务也能点暂停或重试。
  for(const [id,action] of [['pauseSelectedBtn','pause'],['resumeSelectedBtn','resume'],['cancelSelectedBtn','cancel'],['retrySelectedBtn','retry']])$(id).disabled=!selectedActionTasks(action).length;
  $('updateSelectedBtn').disabled=!selectedGroupIDs().length;
  $('mergeSelectedBtn').disabled=!selectedMergeGroupIDs().length;
  $('clearTasksBtn').disabled=!selected.length;
  $('deleteSelectedBtn').disabled=!selected.length&&!selectedMerges.size;
  for(const id of ['prioritySelectedBtn','deferSelectedBtn'])$(id).disabled=!selected.some(task=>task.status==='queued');
}

function renderTasks(next) {
  if (app.viewer?.onlineOnly) return;
  if (tasks !== next) taskSignature = '';
  tasks = next;
  if (selectedTasks.size) {
    const existing = new Set(tasks.map(task => task.id));
    selectedTasks.forEach(id => {if (!existing.has(id)) selectedTasks.delete(id);});
  }
  selectedMerges.forEach(id => {if (!hasMergeRecord(id)) selectedMerges.delete(id);});
  let active = 0, completed = 0, written = 0;
  for (const task of tasks) {
    if (['running', 'queued', 'parsing'].includes(task.status)) active++;
    if (task.status === 'success') completed++;
    written += number(task.downloadedBytes);
  }
  const setText = (node, value) => {if (node.textContent !== String(value)) node.textContent = value;};
  setText($('downloadActiveCount'), active);
  setText($('downloadCompleteCount'), completed);
  setText($('downloadWrittenBytes'), formatBytes(written));
  document.querySelectorAll('[data-download-count]').forEach(node => {setText(node, active); if (node.hidden !== !active) node.hidden = !active;});
  hasActiveTasks = active > 0;
  const showActivity = !$('activityPanel').hidden && !$('libraryPage').hidden;
  if ($('downloadsPage').hidden && !showActivity) return;
  const all = buildGroups();
  if (!$('downloadsPage').hidden) {
    visibleGroups = all.filter(groupMatches);
    setText($('taskCount'), all.length ? visibleGroups.length + ' 部' : '');
    const previous = new Map(Array.from(groupsEl.querySelectorAll('.group'), node => [node.dataset.dramaId, node]));
    withFocus(groupsEl, () => {
      const nodes = [];
      for (const group of visibleGroups) {
        const old = previous.get(group.id);
        const key = JSON.stringify([group, mergeStates[group.id], openGroups.has(group.id), batch, selectedMerges.has(group.id), exportingMerges.has(group.id), verifyingGroups.has(group.id), group.tasks.map(task => selectedTasks.has(task.id)), historySuffix(group.id)]);
        // long: 进度轮询只替换变化的分集行，保留同一合集的其余分集 DOM、焦点和内部滚动位置。
        const node = old || element('article', 'group');
        node.dataset.dramaId = group.id;
        node.classList.toggle('open', openGroups.has(group.id));
        if (!old) node.append(renderGroupHead(group), element('div', 'episode-list'));
        else if (node.headerKey !== key) {
          const menuOpen = node.querySelector('.action-menu')?.open;
          const head = renderGroupHead(group);
          if (menuOpen) head.querySelector('.action-menu').open = true;
          node.querySelector('.group-head').replaceWith(head);
        }
        node.headerKey = key;
        const list = node.querySelector('.episode-list');
        const previousEpisodes = new Map(Array.from(list.children, row => [row.dataset.taskId, row]));
        const episodeNodes = openGroups.has(group.id) ? group.tasks.map(task => {
          const prior = previousEpisodes.get(task.id);
          const rowKey = JSON.stringify([task, selectedTasks.has(task.id)]);
          if (prior?.renderKey === rowKey) return prior;
          const row = renderEpisode(task);
          row.dataset.taskId = task.id;
          row.renderKey = rowKey;
          return row;
        }) : [];
        reconcileChildren(list, episodeNodes);
        nodes.push(node);
      }
      if (!visibleGroups.length) {
        const empty = element('div', 'empty');
        empty.append(element('strong', '', tasks.length ? '没有匹配的下载合集' : '还没有下载任务'), element('p', '', tasks.length ? '试试其他筛选条件。' : '在剧集详情加入下载，也可以在剧库批量选择。'));
        if (!tasks.length) empty.appendChild(button('去剧库看看', () => window.DuanjuDialogs.navigate('library'), false, 'secondary'));
        nodes.push(empty);
      }
      reconcileChildren(groupsEl, nodes);
    });
    updateTaskSelection();
  }
  if (showActivity) renderActivity(all);

}

function renderGroupHead(group) {
  const stats = groupStats(group);
  const merge = mergeStates[group.id];
  if (!group.tasks.length && merge?.playbackTaskId) stats.percent = 100;
  const head = element('div', 'group-head');
  const heading = element('div', 'group-heading');
  const checkbox = element('input');
  checkbox.type = 'checkbox';
  checkbox.setAttribute('aria-label', '选择合集 ' + group.title);
  checkbox.dataset.focusKey = 'group-select-' + group.id;
  const selectedCount = group.tasks.filter(task => selectedTasks.has(task.id)).length;
  const hasMerged = hasMergeRecord(group.id), mergeSelected = hasMerged && selectedMerges.has(group.id);
  checkbox.checked = (group.tasks.length > 0 || hasMerged) && selectedCount === group.tasks.length && (!hasMerged || mergeSelected);
  checkbox.disabled = !group.tasks.length && !hasMerged;
  checkbox.indeterminate = (selectedCount > 0 || mergeSelected) && !checkbox.checked;
  checkbox.addEventListener('change', () => selectGroup(group, checkbox.checked));
  const info = element('div');
  info.appendChild(element('h2', 'group-title', group.title));
  // long: 删除已落盘但文件清理未完成时，明确提示可重试，避免把暂不可播放的全集误报为文件丢失。
  const statuses = group.tasks.length ? [releaseText(group.release), '共 ' + group.tasks.length + ' 集', '完成 ' + stats.success] : [merge?.deleteRequested ? '全集删除未完成，可重试' : merge?.playbackTaskId ? '本地合并视频' : '全集文件已丢失，可清理记录'];
  // long: 任务完成记录不等于当前能读取本地文件；共享存储离线时在合集摘要中单独提示。
  const unavailableCount = group.tasks.filter(task => task.localFileUnavailable).length;
  if (unavailableCount) statuses.push('本地不可访问 ' + unavailableCount);
  const qualities = [...new Set(group.tasks.map(task => number(task.downloadQuality)))];
  if (qualities.length) statuses.push('画质：' + (qualities.length === 1 ? qualityLabel(qualities[0]) : '按分集设置'));
  for (const [key, label] of [['running','下载中'],['queued','排队'],['parsing','解析中'],['paused','暂停'],['failed','失败'],['canceled','已取消']]) if (stats[key]) statuses.push(label + ' ' + stats[key]);
  const meta = element('p', 'small', statuses.join(' · '));
  meta.appendChild(element('span', 'group-watch-progress', historySuffix(group.id)));
  info.appendChild(meta);
  const selection = element('label', 'selection-target');
  selection.appendChild(checkbox);
  heading.append(selection, info);
  head.append(heading, progressBar(stats.percent), element('p', 'small', stats.percent + '% · 已写入 ' + formatBytes(stats.bytes) + (stats.speed > 0 ? ' · ' + formatBytes(stats.speed) + '/s' : '')));
  if (merge) {
    // long: 全集文件生成后若记录保存或分集清理失败，页面需提示“部分完成”，避免用户误以为整项操作已成功收尾。
    const mergeStatus = merge.status === 'success' && merge.error ? '部分完成' : ({queued:'排队中', running:'进行中', success:'完成', failed:'失败'}[merge.status] || merge.status);
    head.appendChild(element('p', merge.error ? 'error notice' : 'small notice', '合并：' + mergeStatus + ' ' + (merge.progress || 0) + '%' + (merge.detail ? ' · ' + merge.detail : '') + (merge.error ? ' · ' + merge.error : '')));
  }
  const actions = element('div', 'group-actions');
  function action(label, key, callback, disabled = false, className = 'secondary') {
    const node = button(label, callback, disabled, className);
    node.dataset.focusKey = group.id + '-' + key;
    return node;
  }
  const playable = group.tasks.filter(task => task.playable && !task.localFileUnavailable).sort((left, right) => number(left.index) - number(right.index))[0];
  if (merge?.playbackTaskId) actions.appendChild(action('播放全集（' + merge.startEpisode + '–' + merge.endEpisode + '集）', 'play-merged', () => window.dramaPlayer.openCollection(merge.playbackTaskId, group.title, true), false, 'collection-play-button merged-play-button'));
  if (group.tasks.length) actions.appendChild(action(merge?.playbackTaskId ? '分集播放' : watchLabel(group.id, '播放'), 'play', () => window.dramaPlayer.openCollection(playable.id, group.title, true), !playable, 'secondary collection-play-button'));
  const toggle = action(openGroups.has(group.id) ? '收起分集' : '查看分集', 'episodes', () => {
    if (openGroups.has(group.id)) openGroups.delete(group.id); else openGroups.add(group.id);
    renderTasks(tasks);
  });
  toggle.setAttribute('aria-expanded', String(openGroups.has(group.id)));
  toggle.disabled = !group.tasks.length;
  actions.appendChild(toggle);
  if (stats.running + stats.queued + stats.parsing) actions.appendChild(action('暂停', 'pause', () => taskAction('pause', [], [group.id])));
  if (stats.paused) actions.appendChild(action('继续', 'resume', () => taskAction('resume', [], [group.id])));
  if (stats.failed + stats.canceled + group.tasks.filter(task => task.localFileRetryable).length) actions.appendChild(action('重试失败或不可用分集', 'retry', () => taskAction('retry', [], [group.id])));
  if (merge && ['queued', 'running'].includes(merge.status)) actions.appendChild(action('取消合并', 'cancel-merge', async () => {try {await post('/api/ui/merge/cancel', {dramaIds: [group.id]}); await pollTasks();} catch (error) {setMessage(error.message, true);}}));
  if (merge?.cleanupPending?.length) actions.appendChild(action('重试清理 ' + merge.cleanupPending.length + ' 个分集文件', 'retry-merge-cleanup', () => retryMergeCleanup(group.id)));
  const menu = element('details', 'action-menu');
  const summary = element('summary', '', '更多操作');
  summary.dataset.focusKey = group.id + '-more';
  const popover = element('div', 'action-popover');
  popover.append(action('更新本剧', 'update', () => updateGroups([group.id])), action('合并已完成分集', 'merge', () => mergeGroups([group.id]), !stats.success || ['queued', 'running'].includes(merge?.status)), action('取消本剧下载', 'cancel', () => taskAction('cancel', [], [group.id]), !stats.running && !stats.queued && !stats.parsing && !stats.paused));
  if (stats.success) popover.appendChild(action(verifyingGroups.has(group.id) ? '校验中…' : '校验本剧文件', 'verify-files', () => verifyGroupFiles(group), verifyingGroups.has(group.id)));
  if (stats.queued) popover.append(action('优先下载', 'priority-front', () => prioritizeTasks([], [group.id], 'front')), action('稍后下载', 'priority-back', () => prioritizeTasks([], [group.id], 'back')));
  if (merge?.playbackTaskId) popover.appendChild(action(exportingMerges.has(group.id) ? '导出中…' : '导出 Emby 全集', 'export-merged', () => exportMergedEmby(group), exportingMerges.has(group.id)));
  if (hasMerged) popover.appendChild(action(merge?.deleteRequested ? '重试删除全集文件与记录' : '删除全集文件与记录', 'delete-merged', () => deleteResources([], [group.id]), false, 'danger'));
  if (group.tasks.length || hasMerged) popover.appendChild(action('删除本剧全部资源', 'delete-group', () => deleteResources(group.tasks.map(task => task.id), hasMerged ? [group.id] : []), false, 'danger'));
  menu.append(summary, popover);
  actions.appendChild(menu);
  head.appendChild(actions);
  return head;
}

function renderEpisode(task){
  const row = element('div', 'episode');
  const checkbox = element('input');
  checkbox.type = 'checkbox';
  checkbox.checked = selectedTasks.has(task.id);
  checkbox.setAttribute('aria-label', '选择 ' + episodeLabel(task));
  checkbox.dataset.focusKey = 'task-' + task.id + '-select';
  checkbox.addEventListener('change', () => {
    if (checkbox.checked) selectedTasks.add(task.id); else selectedTasks.delete(task.id);
    renderTasks(tasks);
  });
  const selection = element('label', 'selection-target');
  selection.appendChild(checkbox);
  row.appendChild(selection);
  const content = element('div', 'episode-main');
  const heading = element('div', 'episode-heading');
  heading.appendChild(element('span', 'pill ' + task.status, phaseText(task)));
  heading.appendChild(element('strong', '', task.status === 'parsing' || task.title === '章节获取失败' ? task.title : episodeLabel(task)));
  content.appendChild(heading);
  content.appendChild(element('div', 'small episode-path', task.path || ''));
  if (task.error) content.appendChild(element('div', 'error', task.error));
  // long: NAS 离线和文件丢失不能凭状态推断；提示先恢复存储，确认丢失后才由用户主动重下。
  if (task.localFileUnavailable) content.appendChild(element('div', 'error', '本地文件当前不可访问，请先检查存储连接；确认文件丢失后可手动重试下载。'));
  content.appendChild(progressBar(task.progress));
  content.appendChild(element('div', 'small', progressText(task) + ' · 已写入 ' + formatBytes(task.downloadedBytes) + (number(task.totalBytes) ? ' / ' + formatBytes(task.totalBytes) : '') + ' · ' + (number(task.speedBytesPerSecond) > 0 ? formatBytes(task.speedBytesPerSecond) + '/s' : '速度 —')));
  content.appendChild(element('div', 'small', '已用 ' + formatTime(task.elapsedSeconds) + ' · 剩余 ' + formatTime(task.remainingSeconds) + ' · 尝试 ' + (task.attempt || 0)));
  const actions = element('div', 'episode-actions');
  function action(label, key, callback, disabled = false, className = 'secondary') {
    const control = button(label, callback, disabled, className);
    // long: 分集动作随状态增减时仍按业务动作恢复焦点，不能按按钮位置把焦点移到另一种操作。
    control.dataset.focusKey = 'task-' + task.id + '-' + key;
    return control;
  }
  if (task.playable && !task.localFileUnavailable) actions.appendChild(action('播放', 'play', () => window.dramaPlayer.openCollection(task.id, task.dramaTitle), false, 'secondary episode-play-button'));
  if (['running', 'queued', 'parsing'].includes(task.status)) actions.appendChild(action('暂停', 'pause', () => taskAction('pause', [task.id]), task.pauseRequested));
  if (task.status === 'paused') actions.appendChild(action('继续', 'resume', () => taskAction('resume', [task.id])));
  if (['running', 'queued', 'parsing', 'paused'].includes(task.status)) actions.appendChild(action('取消', 'cancel', () => taskAction('cancel', [task.id]), task.cancelRequested));
  if (['failed', 'canceled'].includes(task.status) || task.localFileRetryable) actions.appendChild(action('重试', 'retry', () => taskAction('retry', [task.id])));
  if (task.status === 'queued') actions.append(action('优先下载', 'priority-front', () => prioritizeTasks([task.id], [], 'front')), action('稍后下载', 'priority-back', () => prioritizeTasks([task.id], [], 'back')));
  actions.appendChild(action(task.deleteFilesRequested ? '正在删除…' : '删除分集文件', 'delete', () => deleteResources([task.id], []), task.deleteFilesRequested, 'danger'));
  content.appendChild(actions);
  row.appendChild(content);
  return row;
}

async function retryFFmpeg(){try{await post('/api/ui/ffmpeg',{});setMessage('已重新检查 FFmpeg，准备进度会自动更新');await pollTasks();}catch(error){setMessage(error.message,true);}}

async function exportMergedEmby(group) {
  if (exportingMerges.has(group.id)) return;
  exportingMerges.add(group.id);
  renderTasks(tasks);
  try {
    // long: 全集导出只打包 STRM 与元数据，由服务器持续提供本地 MP4 的 Range 播放。
    const response = await fetch('/api/emby/export', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({dramaId: group.id, baseUrl: location.origin, merged: true})});
    if (!response.ok) {const result = await response.json(); throw new Error(result.error || '导出失败');}
    const address = URL.createObjectURL(await response.blob());
    const link = document.createElement('a');
    link.href = address;
    link.download = group.title + '-Emby-全集.zip';
    document.body.appendChild(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(address), 60000);
    setMessage('已导出 Emby 全集；解压到电视剧媒体库后扫描即可。');
  } catch (error) {setMessage('Emby 全集导出失败：' + error.message, true);}
  finally {exportingMerges.delete(group.id); renderTasks(tasks);}
}

function renderFFmpeg(state){if(!state)return;const retryCovers=ffmpegStatus&&ffmpegStatus!=='ready'&&state.status==='ready';ffmpegStatus=state.status;let text=state.detail||'首次使用时自动准备 FFmpeg';if(state.status==='downloading'&&number(state.totalBytes)>0)text+=' · '+formatBytes(state.downloadedBytes)+' / '+formatBytes(state.totalBytes);if(state.error)text+='：'+state.error;for(const id of ['ffmpegNotice','ffmpegSettingsStatus']){const target=$(id);empty(target);target.appendChild(element('span',state.status==='failed'?'error':'',text));if(state.status==='failed'&&app.viewer?.account?.admin)target.appendChild(button('重新检查 FFmpeg',retryFFmpeg));if(id==='ffmpegNotice')target.hidden=!['downloading','verifying','failed'].includes(state.status);}if(retryCovers)app.library.retryCovers();window.dramaPlayer?.updateDependency(state,text);}

async function pollTasks() {
  if (app.viewer?.onlineOnly) return;
  if (taskRequest) return pollPromise;
  taskRequest = true;
  pollPromise = (async () => {
    try {
      const result = await api('/api/ui/tasks');
      const signature = JSON.stringify([result.data || [], result.merges || {}]);
      if (signature !== taskSignature) {
        mergeStates = result.merges || {};
        renderTasks(result.data || []);
        taskSignature = signature;
      }
      const nextFFmpeg = JSON.stringify(result.ffmpeg || null);
      if (nextFFmpeg !== ffmpegSignature) {ffmpegSignature = nextFFmpeg; renderFFmpeg(result.ffmpeg);}
      if ($('taskError').textContent.startsWith('任务刷新失败：')) $('taskError').textContent = '';
    } catch (error) {
      $('taskError').textContent = '任务刷新失败：' + error.message;
    } finally {taskRequest = false; schedule();}
  })();
  return pollPromise;
}

async function updateGroups(ids){if(!ids.length){setMessage('请先勾选下载合集',true);return;}try{await post('/api/ui/update',{ids});setMessage('已提交 '+ids.length+' 部更新检查，已有文件保留，只补充新增或缺失分集');await pollTasks();}catch(error){$('taskError').textContent='更新失败：'+error.message;}}

async function taskAction(action,ids,dramaIds=[]){const count=dramaIds.length||ids.length;const unit=dramaIds.length?' 部合集':' 个任务';if(!count){setMessage('所选任务中没有可执行此操作的项',true);return;}if(action==='cancel'&&!confirm('取消所选 '+count+unit+'？已完成文件保留，可稍后重试。'))return;
  // long: 成功任务的文件不可访问可能只是共享存储离线；明确确认后才重新写入原下载目录。
  if(action==='retry'&&tasks.some(task=>task.localFileRetryable&&(ids.includes(task.id)||dramaIds.includes(task.dramaId)))&&!confirm('本地文件可能只是存储暂时离线。请先确认存储已连接、原文件确实丢失；重试将重新下载到原目录。确定继续？'))return;
  try{const result=await post('/api/ui/tasks/'+action,dramaIds.length?{dramaIds}:{ids});$('taskError').textContent='';if(result.data)renderTasks(result.data);setMessage('已提交'+({pause:'暂停',resume:'继续',cancel:'取消',retry:'重试'}[action]||action)+'：'+count+unit);await pollTasks();}catch(error){$('taskError').textContent='操作失败：'+error.message;}}

async function prioritizeTasks(ids, dramaIds, position) {
  if (!ids.length && !dramaIds.length) {setMessage('请先选择排队中的任务', true); return;}
  try {
    const result = await post('/api/ui/tasks/priority', {ids, dramaIds, position});
    if (result.data) renderTasks(result.data);
    setMessage(position === 'front' ? '已调整为优先下载；正在下载的任务会继续完成。' : '已调整为稍后下载。');
    await pollTasks();
  } catch (error) {$('taskError').textContent = '调整下载顺序失败：' + error.message;}
}

async function retryMergeCleanup(dramaId) {
  try {
    const result = await post('/api/ui/merge/cleanup', {dramaIds: [dramaId]});
    setMessage(result.pending ? '仍有 ' + result.pending + ' 个分集文件未能清理，请检查文件权限、共享引用和下载状态' : '分集文件已清理完成', Boolean(result.pending));
    await pollTasks();
  } catch (error) {$('taskError').textContent = '重试清理失败：' + error.message;}
}

async function verifyGroupFiles(group) {
  if (verifyingGroups.has(group.id)) return;
  verifyingGroups.add(group.id);
  renderTasks(tasks);
  const ids = group.tasks.filter(task => task.status === 'success').map(task => task.id);
  let checked = 0, damaged = 0;
  try {
    // long: 每批只校验少量分集，避免单次请求因长剧或低性能服务器持续占用连接。
    for (let offset = 0; offset < ids.length; offset += 10) {
      const result = await post('/api/ui/tasks/verify', {ids: ids.slice(offset, offset + 10)});
      checked += result.checked || 0;
      damaged += result.damaged || 0;
      setMessage('正在校验 ' + group.title + '：' + Math.min(offset + 10, ids.length) + ' / ' + ids.length + ' 集');
    }
    setMessage('已校验 ' + checked + ' 集' + (damaged ? '；发现 ' + damaged + ' 集损坏或缺失，可点击重试重新下载' : '，未发现损坏文件'), Boolean(damaged));
    await pollTasks();
  } catch (error) {$('taskError').textContent = '校验本剧文件失败：' + error.message; await pollTasks();}
  finally {verifyingGroups.delete(group.id); renderTasks(tasks);}
}

async function clearTasks(){const ids=selectedTaskList().map(task=>task.id);if(!ids.length){setMessage('请先勾选需要清理的任务',true);return;}if(!confirm('清理勾选的 '+ids.length+' 个任务记录？进行中的任务会先停止；已下载视频不会删除。'))return;try{const result=await post('/api/ui/tasks/clear',{ids});ids.forEach(id=>selectedTasks.delete(id));renderTasks(result.data||[]);setMessage('已清理 '+(result.removed||0)+' 个任务'+(result.pending?'，另有 '+result.pending+' 个正在停止后清理':''));}catch(error){$('taskError').textContent='清理失败：'+error.message;}}

async function deleteResources(ids, mergedDramaIds) {
  if (!ids.length && !mergedDramaIds.length) {setMessage('请先选择要删除的分集或全集', true); return;}
  // long: 二次确认明确包含磁盘文件，避免把“清理记录”和永久删除误认为同一操作。
  const scope = [ids.length && ids.length + ' 集分集', mergedDramaIds.length && mergedDramaIds.length + ' 个全集'].filter(Boolean).join('、');
  if (!confirm('永久删除 ' + scope + ' 的任务记录和本地视频文件？此操作不可恢复。正在下载的任务会先停止；正在合并的剧需先取消合并。')) return;
  try {
    const result = await post('/api/ui/tasks/delete', {ids, mergedDramaIds});
    ids.forEach(id => selectedTasks.delete(id));
    mergedDramaIds.forEach(id => selectedMerges.delete(id));
    renderTasks(result.data || []);
    setMessage('已删除 ' + (result.removed || 0) + ' 集、' + (result.mergedRemoved || 0) + ' 个全集，共移除 ' + (result.deletedFiles || 0) + ' 个磁盘文件' + (result.pending ? '；' + result.pending + ' 个下载任务将在停止后删除' : '') + (result.failed ? '；' + result.failed + ' 项失败，请查看提示' : ''), Boolean(result.failed));
    await pollTasks();
    $('taskError').textContent = (result.errors || []).join('；');
  } catch (error) {$('taskError').textContent = '删除失败：' + error.message;}
}

async function mergeGroups(ids) {
  if (!ids.length) {setMessage('请先勾选下载合集', true); return;}
  const deleteEpisodes = $('deleteEpisodesAfterMerge').checked;
  if (deleteEpisodes && !confirm('合并成功后删除 ' + ids.length + ' 个合集已合并的分集文件，确认继续？')) return;
  try {
    const result = await post('/api/ui/merge', {dramaIds: ids, deleteEpisodes, async: true});
    setMessage('已提交后台合并 ' + result.accepted + ' 部，可关闭或刷新页面，进度在下载区查看');
    await pollTasks();
  } catch (error) {$('taskError').textContent = '提交合并失败：' + error.message;}
}


function renderActivity(all) {
  const ordered = all.slice().sort((left, right) => {
    const a = groupStats(left), b = groupStats(right);
    return Boolean(b.running + b.queued + b.parsing) - Boolean(a.running + a.queued + a.parsing);
  });
  const fragment = document.createDocumentFragment();
  for (const group of ordered.slice(0, 3)) {
    const stats = groupStats(group), row = element('div', 'activity-item');
    row.append(element('h3', '', group.title), element('p', 'small', stats.success + ' / ' + group.tasks.length + ' 集已完成'), progressBar(stats.percent));
    fragment.appendChild(row);
  }
  if (!ordered.length) fragment.appendChild(element('p', 'small', '暂无下载任务，可在剧集卡片上直接加入下载。'));
  $('activityTasks').replaceChildren(fragment);
}

async function enqueue(ids) {
  if (app.viewer?.onlineOnly) {setMessage('当前账号仅可在线观看', true); return false;}
  if (!ids.length) return false;
  try {
    const quality = downloadQuality;
    await post('/api/ui/download', {ids, quality});
    setMessage('已加入下载队列 · ' + ids.length + ' 部 · ' + qualityLabel(quality), false, {label: '查看下载', run: () => window.DuanjuDialogs.navigate('downloads')});
    await pollTasks();
    return true;
  } catch (error) {setMessage('加入下载失败：' + error.message, true); return false;}
}

function persistFilters() {
  savePreference('downloadFilters', {search: $('taskSearch').value, status: $('taskStatus').value, release: $('releaseStatus').value});
}

function toggleBatch() {
  batch = !batch;
  if (!batch) {selectedTasks.clear(); selectedMerges.clear();}
  document.body.classList.toggle('download-batch', batch);
  $('downloadBatchControls').hidden = !batch;
  $('downloadBatchBtn').setAttribute('aria-pressed', String(batch));
  $('downloadBatchBtn').textContent = batch ? '退出批量' : '批量管理';
  renderTasks(tasks);
}

function schedule(restart = true) {
  if (app.viewer?.onlineOnly) return;
  const active = hasActiveTasks || ['idle', 'downloading', 'verifying'].includes(ffmpegStatus) || Object.values(mergeStates).some(state => ['queued', 'running'].includes(state.status));
  // long: 闲置下载页主要是成功任务；降低轮询频率可减少共享存储目录检查，操作和窗口恢复仍会立即刷新。
  const delay = document.hidden ? 30000 : active ? 2000 : 15000;
  if (!restart && polling && pollTimer && delay === scheduledDelay) return;
  clearTimeout(pollTimer);
  pollTimer = 0;
  if (!polling) return;
  scheduledDelay = delay;
  pollTimer = setTimeout(() => {pollTimer = 0; pollTasks();}, delay);
}

function init() {
  if (app.viewer?.onlineOnly) return;
  document.querySelectorAll('[data-download-quality]').forEach(setupQualitySelect);
  const stored = readPreference('downloadFilters', {}), saved = stored && typeof stored === 'object' ? stored : {};
  $('taskSearch').value = typeof saved.search === 'string' ? saved.search.slice(0, 80) : '';
  for (const [id, value] of [['taskStatus', saved.status], ['releaseStatus', saved.release]]) {
    $(id).value = Array.from($(id).options).some(option => option.value === value) ? value : '';
  }
  for (const id of ['taskStatus', 'releaseStatus']) $(id).addEventListener('change', () => {persistFilters(); renderTasks(tasks);});
  $('taskSearch').addEventListener('input', () => {persistFilters(); renderTasks(tasks);});
  $('downloadBatchBtn').addEventListener('click', toggleBatch);
  $('selectGroupsBtn').addEventListener('click', () => {visibleGroups.forEach(group => {group.tasks.forEach(task => selectedTasks.add(task.id)); if (hasMergeRecord(group.id)) selectedMerges.add(group.id);}); renderTasks(tasks);});
  $('invertGroupsBtn').addEventListener('click', () => {visibleGroups.forEach(group => {const hasMerged = hasMergeRecord(group.id); const checked = !(group.tasks.every(task => selectedTasks.has(task.id)) && (!hasMerged || selectedMerges.has(group.id))); group.tasks.forEach(task => checked ? selectedTasks.add(task.id) : selectedTasks.delete(task.id)); if (hasMerged) {if (checked) selectedMerges.add(group.id); else selectedMerges.delete(group.id);}}); renderTasks(tasks);});
  $('deselectGroupsBtn').addEventListener('click', () => {selectedTasks.clear(); selectedMerges.clear(); renderTasks(tasks);});
  for (const [id, action] of [['pauseSelectedBtn','pause'],['resumeSelectedBtn','resume'],['cancelSelectedBtn','cancel'],['retrySelectedBtn','retry']]) $(id).addEventListener('click', () => taskAction(action, selectedActionTasks(action).map(task => task.id)));
  $('updateSelectedBtn').addEventListener('click', () => updateGroups(selectedGroupIDs()));
  $('mergeSelectedBtn').addEventListener('click', () => mergeGroups(selectedMergeGroupIDs()));
  $('clearTasksBtn').addEventListener('click', clearTasks);
  $('prioritySelectedBtn').addEventListener('click', () => prioritizeTasks(selectedTaskList().filter(task => task.status === 'queued').map(task => task.id), [], 'front'));
  $('deferSelectedBtn').addEventListener('click', () => prioritizeTasks(selectedTaskList().filter(task => task.status === 'queued').map(task => task.id), [], 'back'));
  $('deleteSelectedBtn').addEventListener('click', () => deleteResources(selectedTaskList().map(task => task.id), [...selectedMerges]));
  window.addEventListener('downloadsChanged', pollTasks);
  document.addEventListener('visibilitychange', () => {if (!document.hidden) pollTasks(); schedule();});
  window.addEventListener('pagehide', () => {polling = false; clearTimeout(pollTimer);});
  window.addEventListener('pageshow', event => {polling = true; if (event.persisted) pollTasks(); else schedule();});
  polling = true;
  return pollTasks();
}

return {init, refresh: pollTasks, render: () => {renderTasks(tasks); schedule(false);}, enqueue, qualityControl, quality: () => downloadQuality, all: () => tasks};

}
